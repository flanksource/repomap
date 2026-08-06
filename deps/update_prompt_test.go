package deps

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// A dependency declared in several manifests is one decision, so the version is
// confirmed once and the answer written to every occurrence.
func TestUpdatePromptsOnceForDependencyRepeatedAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	for _, project := range []string{"api", "web"} {
		writeFile(t, filepath.Join(dir, project, "package.json"), `{
  "name": "`+project+`",
  "dependencies": {"left-pad": "^1.3.0"}
}`)
		writeFile(t, filepath.Join(dir, project, "package-lock.json"), `{"lockfileVersion": 3}`)
	}
	runner := &updateFakeRunner{
		responses: map[string]CommandResult{
			"npm view left-pad versions --json": {Stdout: `["1.3.0","1.4.0"]`},
		},
	}

	var (
		mu      sync.Mutex
		prompts []UpdateVersionPrompt
	)
	plans, err := Update(context.Background(), dir, UpdateOptions{
		Managers: []Manager{ManagerNPM},
		Filters:  []string{"left-pad"},
		DryRun:   true,
		Runner:   runner,
		SelectCandidates: func(choices []UpdateChoice) ([]UpdateChoice, bool) {
			if len(choices) != 2 {
				t.Fatalf("both occurrences should be selectable, got %d", len(choices))
			}
			return choices, true
		},
		SelectVersion: func(prompt UpdateVersionPrompt) (string, bool) {
			mu.Lock()
			defer mu.Unlock()
			prompts = append(prompts, prompt)
			return "1.4.0", true
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 {
		t.Fatalf("version prompts = %d, want 1: %#v", len(prompts), prompts)
	}
	// UpdateCandidate.File is cwd-relative, so assert on the manifest suffixes.
	gotFiles := prompts[0].Files
	wantSuffixes := []string{"api/package.json", "web/package.json"}
	if len(gotFiles) != len(wantSuffixes) {
		t.Fatalf("prompt files = %#v, want one entry per manifest %#v", gotFiles, wantSuffixes)
	}
	for i, suffix := range wantSuffixes {
		if !strings.HasSuffix(filepath.ToSlash(gotFiles[i]), suffix) {
			t.Fatalf("prompt file[%d] = %q, want a path ending in %q", i, gotFiles[i], suffix)
		}
	}
	if len(plans) != 2 {
		t.Fatalf("plans = %d, want 2: %#v", len(plans), plans)
	}
	for _, plan := range plans {
		if plan.NewVersion != "1.4.0" || plan.Skipped != "" {
			t.Fatalf("single answer should apply to every occurrence, got %#v", plan)
		}
	}
}

// Declining the shared prompt skips every occurrence it covered.
func TestUpdateSharedPromptCancellationSkipsAllOccurrences(t *testing.T) {
	dir := t.TempDir()
	for _, project := range []string{"api", "web"} {
		writeFile(t, filepath.Join(dir, project, "package.json"), `{
  "name": "`+project+`",
  "dependencies": {"left-pad": "^1.3.0"}
}`)
		writeFile(t, filepath.Join(dir, project, "package-lock.json"), `{"lockfileVersion": 3}`)
	}
	runner := &updateFakeRunner{
		responses: map[string]CommandResult{
			"npm view left-pad versions --json": {Stdout: `["1.3.0","1.4.0"]`},
		},
	}

	plans, err := Update(context.Background(), dir, UpdateOptions{
		Managers: []Manager{ManagerNPM},
		Filters:  []string{"left-pad"},
		DryRun:   true,
		Runner:   runner,
		SelectCandidates: func(choices []UpdateChoice) ([]UpdateChoice, bool) {
			return choices, true
		},
		SelectVersion: func(UpdateVersionPrompt) (string, bool) {
			return "", false
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 2 {
		t.Fatalf("plans = %d, want 2: %#v", len(plans), plans)
	}
	for _, plan := range plans {
		if plan.Skipped != "no version selected" || plan.Written {
			t.Fatalf("declined prompt should skip every occurrence, got %#v", plan)
		}
	}
}

func TestGroupChoicesForVersionPrompt(t *testing.T) {
	choice := func(name, current, file string, versions ...string) UpdateChoice {
		return UpdateChoice{
			Candidate: UpdateCandidate{Manager: ManagerNPM, Name: name, Current: current, File: file},
			Versions:  versions,
		}
	}
	cases := []struct {
		name    string
		choices []UpdateChoice
		want    [][]string
	}{
		{
			name: "same dependency in different files shares one prompt",
			choices: []UpdateChoice{
				choice("left-pad", "^1.3.0", "api/package.json", "1.4.0"),
				choice("left-pad", "^1.3.0", "web/package.json", "1.4.0"),
			},
			want: [][]string{{"api/package.json", "web/package.json"}},
		},
		{
			name: "different current versions are different decisions",
			choices: []UpdateChoice{
				choice("left-pad", "^1.3.0", "api/package.json", "1.4.0"),
				choice("left-pad", "^1.2.0", "web/package.json", "1.4.0"),
			},
			want: [][]string{{"api/package.json"}, {"web/package.json"}},
		},
		{
			name: "different available versions are different decisions",
			choices: []UpdateChoice{
				choice("left-pad", "^1.3.0", "api/package.json", "1.4.0"),
				choice("left-pad", "^1.3.0", "web/package.json", "1.4.0", "1.5.0"),
			},
			want: [][]string{{"api/package.json"}, {"web/package.json"}},
		},
		{
			name: "different dependencies never merge",
			choices: []UpdateChoice{
				choice("left-pad", "^1.3.0", "api/package.json", "1.4.0"),
				choice("right-pad", "^1.3.0", "api/package.json", "1.4.0"),
			},
			want: [][]string{{"api/package.json"}, {"api/package.json"}},
		},
		{
			name: "two occurrences in one file collapse to a single file entry",
			choices: []UpdateChoice{
				choice("left-pad", "^1.3.0", "api/package.json", "1.4.0"),
				choice("left-pad", "^1.3.0", "api/package.json", "1.4.0"),
			},
			want: [][]string{{"api/package.json"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			groups := groupChoicesForVersionPrompt(tc.choices)
			if len(groups) != len(tc.want) {
				t.Fatalf("groups = %d, want %d", len(groups), len(tc.want))
			}
			for i, group := range groups {
				got := newUpdateVersionPrompt(group).Files
				if strings.Join(got, ",") != strings.Join(tc.want[i], ",") {
					t.Fatalf("group[%d] files = %#v, want %#v", i, got, tc.want[i])
				}
			}
		})
	}
}
