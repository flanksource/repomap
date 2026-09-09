package deps

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// A dependency declared in several manifests is one decision, so picking a
// version on any of its rows answers all of them.
func TestUpdateTreeVersionPickAppliesToEveryOccurrence(t *testing.T) {
	choices := []UpdateChoice{
		npmTreeChoice("left-pad", "^1.3.0", "api/package.json", "1.5.0", "1.4.0"),
		npmTreeChoice("left-pad", "^1.3.0", "web/package.json", "1.5.0", "1.4.0"),
		npmTreeChoice("right-pad", "^2.0.0", "api/package.json", "2.1.0"),
	}
	model := newUpdateChoiceTreeModel(choices)

	var prompts []UpdateVersionPrompt
	restore := stubUpdateVersionPrompt(t, func(prompt UpdateVersionPrompt) (string, bool) {
		prompts = append(prompts, prompt)
		return "1.4.0", true
	})
	defer restore()

	node := findUpdateTreeLeaf(t, model.root, "left-pad", "api/package.json")
	promptUpdateTreeVersion(model.root, node)

	if len(prompts) != 1 {
		t.Fatalf("version prompts = %d, want 1 shared question: %#v", len(prompts), prompts)
	}
	if strings.Join(prompts[0].Files, ",") != "api/package.json,web/package.json" {
		t.Fatalf("prompt files = %#v, want both manifests", prompts[0].Files)
	}

	selections := model.selections()
	if len(selections) != 2 {
		t.Fatalf("selections = %#v, want both left-pad rows selected", selections)
	}
	for _, selection := range selections {
		if selection.Candidate.Name != "left-pad" || selection.Version != "1.4.0" {
			t.Fatalf("selection = %#v, want left-pad at the picked 1.4.0", selection)
		}
	}
}

// Declining the picker leaves the rows on whatever they already targeted.
func TestUpdateTreeDeclinedVersionPickLeavesRowsUnchanged(t *testing.T) {
	model := newUpdateChoiceTreeModel([]UpdateChoice{
		npmTreeChoice("left-pad", "^1.3.0", "api/package.json", "1.5.0", "1.4.0"),
	})
	restore := stubUpdateVersionPrompt(t, func(UpdateVersionPrompt) (string, bool) {
		return "", false
	})
	defer restore()

	node := findUpdateTreeLeaf(t, model.root, "left-pad", "api/package.json")
	promptUpdateTreeVersion(model.root, node)

	if node.Selected {
		t.Fatal("declining the picker must not select the row")
	}
	if node.Mode != updateVersionModeStable || node.Picked != "" {
		t.Fatalf("node = %#v, want the stable default untouched", node)
	}
}

// Rows selected without a stable version to move to report why, rather than
// silently updating to a pre-release.
func TestUpdateSkipsSelectionWithNoStableVersion(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package.json"), `{
  "name": "web",
  "dependencies": {"left-pad": "^1.3.0"}
}`)
	writeFile(t, filepath.Join(dir, "package-lock.json"), `{"lockfileVersion": 3}`)
	runner := &updateFakeRunner{
		responses: map[string]CommandResult{
			"npm view left-pad versions --json": {Stdout: `["1.3.0","1.4.0-rc.1"]`},
		},
	}

	plans, err := Update(context.Background(), dir, UpdateOptions{
		Managers:         []Manager{ManagerNPM},
		Runner:           runner,
		SelectCandidates: selectAllStable(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 {
		t.Fatalf("plans = %d, want 1: %#v", len(plans), plans)
	}
	if plans[0].Skipped != "no stable version available" || plans[0].Written {
		t.Fatalf("plan = %#v, want a no-stable-version skip", plans[0])
	}
}

func npmTreeChoice(name, current, file string, versions ...string) UpdateChoice {
	return UpdateChoice{
		Candidate:        UpdateCandidate{Manager: ManagerNPM, Name: name, Current: current, File: file, Scope: "dependencies"},
		Versions:         versions,
		LatestStable:     latestStableVersion(versions),
		LatestPrerelease: latestPrereleaseVersion(versions),
	}
}

// stubUpdateVersionPrompt replaces the tree's `v` picker so tests can answer it
// without a terminal.
func stubUpdateVersionPrompt(t *testing.T, answer VersionSelector) func() {
	t.Helper()
	previous := selectUpdateVersion
	selectUpdateVersion = answer
	return func() { selectUpdateVersion = previous }
}

func findUpdateTreeLeaf(t *testing.T, root *updateChoiceTreeNode, name, file string) *updateChoiceTreeNode {
	t.Helper()
	var found *updateChoiceTreeNode
	var walk func(n *updateChoiceTreeNode)
	walk = func(n *updateChoiceTreeNode) {
		if !n.IsDir {
			if n.Choice != nil && n.Choice.Candidate.Name == name && n.Choice.Candidate.File == file {
				found = n
			}
			return
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(root)
	if found == nil {
		t.Fatalf("no leaf for %s in %s", name, file)
	}
	return found
}

// Applying between prompts froze the terminal for seconds at a time while a
// package manager ran, so every question must be asked before anything is
// applied.
func TestUpdateAppliesOnlyAfterEveryPromptIsAnswered(t *testing.T) {
	dir := t.TempDir()
	for _, project := range []string{"api", "web"} {
		writeFile(t, filepath.Join(dir, project, "package.json"), `{
  "name": "`+project+`",
  "dependencies": {"left-pad": "^1.3.0", "right-pad": "^2.0.0"}
}`)
		writeFile(t, filepath.Join(dir, project, "package-lock.json"), `{"lockfileVersion": 3}`)
	}
	runner := &updateFakeRunner{
		succeedByDefault: true,
		responses: map[string]CommandResult{
			"npm view left-pad versions --json":  {Stdout: `["1.3.0","1.4.0"]`},
			"npm view right-pad versions --json": {Stdout: `["2.0.0","2.1.0"]`},
		},
	}

	var ranDuringSelection []string
	if _, err := Update(context.Background(), dir, UpdateOptions{
		Managers: []Manager{ManagerNPM},
		Runner:   runner,
		SelectCandidates: func(choices []UpdateChoice) ([]UpdateSelection, bool) {
			ranDuringSelection = runner.ran()
			return selectAllStable()(choices)
		},
	}); err != nil {
		t.Fatal(err)
	}

	for _, cmd := range ranDuringSelection {
		if strings.Contains(cmd, "npm view") {
			continue // version lookups legitimately precede selection
		}
		t.Fatalf("command %q ran before selection finished; applies must not block the picker", cmd)
	}
	if !containsString(runner.ran(), "npm install --package-lock-only --ignore-scripts --save-prod left-pad@1.4.0 right-pad@2.1.0") {
		t.Fatalf("expected the update to be applied after selection, got %#v", runner.ran())
	}
}

// Pressing v answers every row posing the identical question, and only those.
func TestUpdateTreeRowsSharingVersionQuestion(t *testing.T) {
	choice := func(name, current, file string, versions ...string) UpdateChoice {
		return UpdateChoice{
			Candidate: UpdateCandidate{Manager: ManagerNPM, Name: name, Current: current, File: file},
			Versions:  versions,
		}
	}
	cases := []struct {
		name    string
		choices []UpdateChoice
		// pick identifies the row v is pressed on; want lists the manifests the
		// answer reaches.
		pickName string
		pickFile string
		want     []string
	}{
		{
			name: "same dependency in different files shares one question",
			choices: []UpdateChoice{
				choice("left-pad", "^1.3.0", "api/package.json", "1.4.0"),
				choice("left-pad", "^1.3.0", "web/package.json", "1.4.0"),
			},
			pickName: "left-pad", pickFile: "api/package.json",
			want: []string{"api/package.json", "web/package.json"},
		},
		{
			name: "different current versions are different decisions",
			choices: []UpdateChoice{
				choice("left-pad", "^1.3.0", "api/package.json", "1.4.0"),
				choice("left-pad", "^1.2.0", "web/package.json", "1.4.0"),
			},
			pickName: "left-pad", pickFile: "api/package.json",
			want: []string{"api/package.json"},
		},
		{
			name: "different available versions are different decisions",
			choices: []UpdateChoice{
				choice("left-pad", "^1.3.0", "api/package.json", "1.4.0"),
				choice("left-pad", "^1.3.0", "web/package.json", "1.4.0", "1.5.0"),
			},
			pickName: "left-pad", pickFile: "api/package.json",
			want: []string{"api/package.json"},
		},
		{
			name: "different dependencies never merge",
			choices: []UpdateChoice{
				choice("left-pad", "^1.3.0", "api/package.json", "1.4.0"),
				choice("right-pad", "^1.3.0", "api/package.json", "1.4.0"),
			},
			pickName: "left-pad", pickFile: "api/package.json",
			want: []string{"api/package.json"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model := newUpdateChoiceTreeModel(tc.choices)
			node := findUpdateTreeLeaf(t, model.root, tc.pickName, tc.pickFile)
			rows := updateTreeRowsSharingVersionQuestion(model.root, node)
			got := make([]string, 0, len(rows))
			for _, row := range rows {
				got = append(got, row.Choice.Candidate.File)
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("shared rows = %#v, want %#v", got, tc.want)
			}
		})
	}
}
