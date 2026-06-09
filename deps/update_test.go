package deps

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/flanksource/repomap/imageupdate"
)

func TestDiscoverUpdateCandidates_DirectOnly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), `module github.com/acme/app

go 1.22

require (
	github.com/acme/direct v1.2.3
	github.com/acme/indirect v0.1.0 // indirect
	github.com/acme/local v0.2.0
)

replace github.com/acme/local => ../local
`)
	writeFile(t, filepath.Join(dir, "web", "package.json"), `{
  "name": "web",
  "dependencies": {"left-pad": "^1.3.0", "local-tool": "file:../tool"},
  "devDependencies": {"typescript": "~5.5.0"}
}`)
	writeFile(t, filepath.Join(dir, "web", "package-lock.json"), `{"lockfileVersion": 3}`)
	writeFile(t, filepath.Join(dir, "pnpm-app", "package.json"), `{
  "name": "pnpm-app",
  "dependencies": {"@scope/pkg": "2.0.0"},
  "devDependencies": {"workspace-tool": "workspace:*"}
}`)
	writeFile(t, filepath.Join(dir, "pnpm-app", "pnpm-lock.yaml"), `lockfileVersion: '9.0'`)

	got, err := DiscoverUpdateCandidates(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := updateCandidateLabels(got)
	want := []string{
		"go:github.com/acme/direct:require:v1.2.3",
		"npm:left-pad:dependencies:^1.3.0",
		"npm:typescript:devDependencies:~5.5.0",
		"pnpm:@scope/pkg:dependencies:2.0.0",
	}
	if strings.Join(names, "\n") != strings.Join(want, "\n") {
		t.Fatalf("candidates:\n%s\nwant:\n%s", strings.Join(names, "\n"), strings.Join(want, "\n"))
	}
}

func TestDiscoverUpdateCandidates_FilePathsRelativeToCWD(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "go.mod"), `module github.com/acme/app

go 1.22

require github.com/acme/direct v1.2.3
`)
	writeFile(t, filepath.Join(dir, "web", "package.json"), `{
  "name": "web",
  "dependencies": {"left-pad": "^1.3.0"}
}`)
	writeFile(t, filepath.Join(dir, "web", "package-lock.json"), `{"lockfileVersion": 3}`)

	got, err := DiscoverUpdateCandidates(".", nil)
	if err != nil {
		t.Fatal(err)
	}
	files := updateCandidateFiles(got)
	want := []string{"go.mod", "web/package.json"}
	if strings.Join(files, "\n") != strings.Join(want, "\n") {
		t.Fatalf("candidate files:\n%s\nwant:\n%s", strings.Join(files, "\n"), strings.Join(want, "\n"))
	}
	for _, file := range files {
		if filepath.IsAbs(file) {
			t.Fatalf("candidate file should be relative to cwd, got %q", file)
		}
	}
}

func TestUpdateCandidateMatchesMatchItemExpression(t *testing.T) {
	candidates := []UpdateCandidate{
		{Manager: ManagerGo, Name: "github.com/acme/lib", Current: "v1.0.0", File: "/work/flanksource/app/go.mod"},
		{Manager: ManagerGo, Name: "github.com/flanksource/lib", Current: "v1.0.0", File: "/work/other/go.mod"},
		{Manager: ManagerNPM, Name: "@scope/pkg", Current: "^2.0.0", File: "/work/flanksource/app/package.json"},
	}
	got := filterUpdateCandidates(candidates, []string{"go:github.com/acme/*"})
	if len(got) != 1 || got[0].Name != "github.com/acme/lib" {
		t.Fatalf("unexpected go match: %#v", got)
	}
	got = filterUpdateCandidates(candidates, []string{"*", "!npm:*"})
	if len(got) != 2 || got[0].Manager != ManagerGo || got[1].Manager != ManagerGo {
		t.Fatalf("negated manager match failed: %#v", got)
	}
	got = filterUpdateCandidates(candidates, []string{"*flanksource*"})
	if len(got) != 1 || got[0].Name != "github.com/flanksource/lib" {
		t.Fatalf("unqualified match should not match file paths: %#v", got)
	}
	got = filterUpdateCandidates(candidates, []string{"path:*flanksource*"})
	if len(got) != 2 || got[0].Name != "github.com/acme/lib" || got[1].Name != "@scope/pkg" {
		t.Fatalf("explicit path match failed: %#v", got)
	}
	got = filterUpdateCandidates(candidates, []string{"file:*package.json"})
	if len(got) != 1 || got[0].Name != "@scope/pkg" {
		t.Fatalf("explicit file match failed: %#v", got)
	}
}

func TestAvailableDependencyVersionsParsesAndSorts(t *testing.T) {
	runner := &updateFakeRunner{
		responses: map[string]CommandResult{
			"go list -m -versions -json github.com/acme/lib": {
				Stdout: `{"Path":"github.com/acme/lib","Versions":["v1.0.0","v1.2.0-beta.1","v1.1.0","not-semver"]}`,
			},
			"npm view left-pad versions --json": {
				Stdout: `["1.0.0","1.3.0","1.3.0-beta.1","latest"]`,
			},
		},
	}
	goVersions, err := AvailableDependencyVersions(context.Background(), runner, UpdateCandidate{
		Manager: ManagerGo,
		Name:    "github.com/acme/lib",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(goVersions, ",") != "v1.2.0-beta.1,v1.1.0,v1.0.0" {
		t.Fatalf("go versions = %#v", goVersions)
	}
	npmVersions, err := AvailableDependencyVersions(context.Background(), runner, UpdateCandidate{
		Manager: ManagerNPM,
		Name:    "left-pad",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(npmVersions, ",") != "1.3.0,1.3.0-beta.1,1.0.0" {
		t.Fatalf("npm versions = %#v", npmVersions)
	}
}

func TestUpdateDryRunBuildsPackageManagerCommand(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package.json"), `{
  "name": "web",
  "dependencies": {"left-pad": "^1.3.0"}
}`)
	writeFile(t, filepath.Join(dir, "package-lock.json"), `{"lockfileVersion": 3}`)
	runner := &updateFakeRunner{
		responses: map[string]CommandResult{
			"npm view left-pad versions --json": {Stdout: `["1.3.0","1.4.0"]`},
		},
	}
	plans, err := Update(context.Background(), dir, UpdateOptions{
		Managers:   []Manager{ManagerNPM},
		Expression: []string{"left-pad"},
		DryRun:     true,
		Runner:     runner,
		SelectCandidates: func(choices []UpdateChoice) ([]UpdateChoice, bool) {
			return choices, true
		},
		SelectVersion: func(choice UpdateChoice) (string, bool) {
			return "1.4.0", true
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 {
		t.Fatalf("plans = %d, want 1: %#v", len(plans), plans)
	}
	plan := plans[0]
	if !plan.DryRun || plan.Written || plan.Skipped != "" {
		t.Fatalf("unexpected plan status: %#v", plan)
	}
	wantCommand := "npm install --package-lock-only --ignore-scripts --save-prod left-pad@1.4.0"
	if strings.Join(plan.Command, " ") != wantCommand {
		t.Fatalf("command = %q, want %q", strings.Join(plan.Command, " "), wantCommand)
	}
	if len(runner.commands) != 1 || runner.commands[0].Name != "npm" || runner.commands[0].Args[0] != "view" {
		t.Fatalf("dry-run should only run version lookup, got %#v", runner.commands)
	}
}

func TestUpdateSkipsCandidatesWithoutChangesBeforePrompt(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package.json"), `{
  "name": "web",
  "dependencies": {"left-pad": "^1.3.0"}
}`)
	writeFile(t, filepath.Join(dir, "package-lock.json"), `{"lockfileVersion": 3}`)
	runner := &updateFakeRunner{
		responses: map[string]CommandResult{
			"npm view left-pad versions --json": {Stdout: `["1.3.0"]`},
		},
	}
	plans, err := Update(context.Background(), dir, UpdateOptions{
		Managers:   []Manager{ManagerNPM},
		Expression: []string{"left-pad"},
		DryRun:     true,
		Runner:     runner,
		SelectCandidates: func([]UpdateChoice) ([]UpdateChoice, bool) {
			t.Fatal("no-change candidates must not be shown for selection")
			return nil, false
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 0 {
		t.Fatalf("plans = %#v, want no rows for already-current dependencies", plans)
	}
}

func TestUpdateCheckListsUpdatesWithoutPrompting(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package.json"), `{
  "name": "web",
  "dependencies": {"left-pad": "^1.3.0"}
}`)
	writeFile(t, filepath.Join(dir, "package-lock.json"), `{"lockfileVersion": 3}`)
	runner := &updateFakeRunner{
		responses: map[string]CommandResult{
			"npm view left-pad versions --json": {Stdout: `["1.4.0","1.3.0"]`},
		},
	}
	plans, err := Update(context.Background(), dir, UpdateOptions{
		Managers:   []Manager{ManagerNPM},
		Expression: []string{"left-pad"},
		Check:      true,
		Runner:     runner,
		SelectCandidates: func([]UpdateChoice) ([]UpdateChoice, bool) {
			t.Fatal("--check must not prompt for dependency selection")
			return nil, false
		},
		SelectVersion: func(UpdateChoice) (string, bool) {
			t.Fatal("--check must not prompt for a version")
			return "", false
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 {
		t.Fatalf("plans = %#v, want 1 checked update", plans)
	}
	plan := plans[0]
	if !plan.Checked || plan.DryRun || plan.Written || plan.Skipped != "" {
		t.Fatalf("unexpected check plan status: %#v", plan)
	}
	if plan.OldVersion != "^1.3.0" || plan.NewVersion != "1.4.0" {
		t.Fatalf("unexpected check version change: %#v", plan)
	}
	if len(runner.commands) != 1 || runner.commands[0].Name != "npm" || runner.commands[0].Args[0] != "view" {
		t.Fatalf("--check should only run version lookup, got %#v", runner.commands)
	}
}

func TestDiscoverUpdateCandidates_ImageAndHelmTargets(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	runGit(t, dir, "init")
	writeFile(t, filepath.Join(dir, "apps", "workloads.yaml"), deploymentUpdateFixture)
	writeFile(t, filepath.Join(dir, "apps", "helmrelease.yaml"), helmReleaseUpdateFixture)
	runGit(t, dir, "add", ".")

	got, err := DiscoverUpdateCandidates(".", []Manager{ManagerImage, ManagerHelm})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("candidates = %d, want 3: %#v", len(got), got)
	}
	byManager := map[Manager]int{}
	for _, candidate := range got {
		byManager[candidate.Manager]++
		if filepath.IsAbs(candidate.File) {
			t.Fatalf("candidate file should be relative to cwd, got %q", candidate.File)
		}
		if candidate.Target == nil {
			t.Fatalf("candidate missing target metadata: %#v", candidate)
		}
	}
	if byManager[ManagerImage] != 2 || byManager[ManagerHelm] != 1 {
		t.Fatalf("manager counts = %#v, want image=2 helm=1", byManager)
	}
	helm := findUpdateCandidate(got, ManagerHelm, "podinfo")
	if helm == nil || helm.Current != "6.5.0" || helm.Target.RepoURL == "" {
		t.Fatalf("helm candidate not resolved correctly: %#v", helm)
	}
}

func TestUpdateImageDryRunUsesImageVersionResolver(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	runGit(t, dir, "init")
	writeFile(t, filepath.Join(dir, "apps", "workloads.yaml"), deploymentUpdateFixture)
	runGit(t, dir, "add", ".")

	plans, err := Update(context.Background(), ".", UpdateOptions{
		Managers:      []Manager{ManagerImage},
		Expression:    []string{"*nginx*"},
		DryRun:        true,
		ImageResolver: fakeImageVersionResolver{"nginx": []string{"1.27.0", "1.25.3"}},
		SelectCandidates: func(choices []UpdateChoice) ([]UpdateChoice, bool) {
			if len(choices) != 1 {
				t.Fatalf("choices = %#v, want exactly nginx", choices)
			}
			return choices, true
		},
		SelectVersion: func(choice UpdateChoice) (string, bool) {
			return "1.27.0", true
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 {
		t.Fatalf("plans = %#v, want 1", plans)
	}
	plan := plans[0]
	if plan.Manager != ManagerImage || !plan.DryRun || plan.Written || plan.Skipped != "" {
		t.Fatalf("unexpected image plan: %#v", plan)
	}
	if plan.File != "apps/workloads.yaml" || plan.OldVersion != "1.25.3" || plan.NewVersion != "1.27.0" {
		t.Fatalf("unexpected image change: %#v", plan)
	}
}

func TestGroupUpdateChoicesByFile(t *testing.T) {
	choices := []UpdateChoice{
		{Candidate: UpdateCandidate{Manager: ManagerNPM, Name: "zeta", File: "/repo/web/package.json", Scope: "dependencies"}},
		{Candidate: UpdateCandidate{Manager: ManagerGo, Name: "github.com/acme/lib", File: "/repo/go.mod", Scope: "require"}},
		{Candidate: UpdateCandidate{Manager: ManagerNPM, Name: "alpha", File: "/repo/web/package.json", Scope: "dependencies"}},
	}
	groups := groupUpdateChoicesByFile(choices)
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2: %#v", len(groups), groups)
	}
	if groups[0].File != "/repo/go.mod" || groups[1].File != "/repo/web/package.json" {
		t.Fatalf("unexpected file order: %#v", groups)
	}
	webChoices := groups[1].Choices
	if len(webChoices) != 2 || webChoices[0].Candidate.Name != "alpha" || webChoices[1].Candidate.Name != "zeta" {
		t.Fatalf("web choices not sorted within file: %#v", webChoices)
	}
}

func TestUpdateCommandForManagers(t *testing.T) {
	cases := []struct {
		candidate UpdateCandidate
		version   string
		want      string
	}{
		{
			candidate: UpdateCandidate{Manager: ManagerGo, Name: "github.com/acme/lib"},
			version:   "v1.2.0",
			want:      "go get github.com/acme/lib@v1.2.0",
		},
		{
			candidate: UpdateCandidate{Manager: ManagerNPM, Name: "typescript", Scope: "devDependencies"},
			version:   "5.6.0",
			want:      "npm install --package-lock-only --ignore-scripts --save-dev typescript@5.6.0",
		},
		{
			candidate: UpdateCandidate{Manager: ManagerPNPM, Name: "@scope/pkg", Scope: "peerDependencies"},
			version:   "2.1.0",
			want:      "pnpm add --lockfile-only --ignore-scripts --save-peer @scope/pkg@2.1.0",
		},
	}
	for _, tc := range cases {
		cmd, err := updateCommand(tc.candidate, tc.version)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(append([]string{cmd.Name}, cmd.Args...), " "); got != tc.want {
			t.Fatalf("command = %q, want %q", got, tc.want)
		}
	}
}

func TestUpdateRejectsUnsupportedManagers(t *testing.T) {
	_, err := Update(context.Background(), t.TempDir(), UpdateOptions{
		Managers:   []Manager{ManagerMaven},
		Expression: []string{"*"},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported manager") {
		t.Fatalf("expected unsupported manager error, got %v", err)
	}
}

type updateFakeRunner struct {
	mu        sync.Mutex
	responses map[string]CommandResult
	errors    map[string]error
	commands  []Command
}

func (r *updateFakeRunner) Run(_ context.Context, cmd Command) (CommandResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands = append(r.commands, cmd)
	key := strings.Join(append([]string{cmd.Name}, cmd.Args...), " ")
	if r.errors != nil && r.errors[key] != nil {
		return r.responses[key], r.errors[key]
	}
	if r.responses != nil {
		if result, ok := r.responses[key]; ok {
			return result, nil
		}
	}
	return CommandResult{}, errors.New("unexpected command: " + key)
}

func updateCandidateLabels(candidates []UpdateCandidate) []string {
	labels := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		labels = append(labels, strings.Join([]string{
			string(candidate.Manager),
			candidate.Name,
			candidate.Scope,
			candidate.Current,
		}, ":"))
	}
	return labels
}

func updateCandidateFiles(candidates []UpdateCandidate) []string {
	files := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		files = append(files, candidate.File)
	}
	return files
}

func findUpdateCandidate(candidates []UpdateCandidate, manager Manager, name string) *UpdateCandidate {
	for i := range candidates {
		if candidates[i].Manager == manager && candidates[i].Name == name {
			return &candidates[i]
		}
	}
	return nil
}

type fakeImageVersionResolver map[string][]string

func (r fakeImageVersionResolver) Available(_ context.Context, target imageupdate.UpdateTarget) ([]string, error) {
	return sortDependencyVersions(r[updateTargetName(target)]), nil
}

func (r fakeImageVersionResolver) ResolveLatestVersions(_ context.Context, target imageupdate.UpdateTarget) (imageupdate.LatestVersions, error) {
	versions := sortDependencyVersions(r[updateTargetName(target)])
	return imageupdate.LatestVersions{
		Stable:     latestStableVersion(versions),
		Prerelease: latestPrereleaseVersion(versions),
	}, nil
}

func (fakeImageVersionResolver) NewImageValue(_ context.Context, target imageupdate.UpdateTarget, version string) (string, error) {
	return stripImageVersion(target.CurrentValue) + ":" + version, nil
}

const deploymentUpdateFixture = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  template:
    spec:
      containers:
        - name: web
          image: nginx:1.25.3
        - name: sidecar
          image: ghcr.io/flanksource/proxy:v0.4.1
`

const helmReleaseUpdateFixture = `apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: podinfo
  namespace: default
spec:
  chart:
    spec:
      chart: podinfo
      version: 6.5.0
      sourceRef:
        kind: HelmRepository
        name: podinfo
        namespace: flux-system
---
apiVersion: source.toolkit.fluxcd.io/v1
kind: HelmRepository
metadata:
  name: podinfo
  namespace: flux-system
spec:
  url: https://stefanprodan.github.io/podinfo
`
