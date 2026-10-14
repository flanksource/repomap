package deps

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

	got, err := DiscoverUpdateCandidates(dir, nil, DiscoverFilter{})
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

	got, err := DiscoverUpdateCandidates(".", nil, DiscoverFilter{})
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

func TestUpdateCandidateMatchesMatchItemFilter(t *testing.T) {
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
	plans, err := updateInTestRepo(context.Background(), dir, UpdateOptions{Managers: []Manager{ManagerNPM}, Filters: []string{"left-pad"}, DryRun: true, Runner: runner, SelectCandidates: selectAllAt("1.4.0")})

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
	plans, err := updateInTestRepo(context.Background(), dir, UpdateOptions{Managers: []Manager{ManagerNPM}, Filters: []string{"left-pad"}, DryRun: true, Runner: runner, SelectCandidates: func([]UpdateChoice) ([]UpdateSelection, bool) {
		t.Fatal("no-change candidates must not be shown for selection")
		return nil, false
	}})

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
	plans, err := updateInTestRepo(context.Background(), dir, UpdateOptions{Managers: []Manager{ManagerNPM}, Filters: []string{"left-pad"}, Check: true, Runner: runner, SelectCandidates: func([]UpdateChoice) ([]UpdateSelection, bool) {
		t.Fatal("--check must not prompt for dependency selection")
		return nil, false
	}})

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

	got, err := DiscoverUpdateCandidates(".", []Manager{ManagerImage, ManagerHelm}, DiscoverFilter{})
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

func TestUpdateHelmReleaseWithMissingSourceReportsClearError(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	runGit(t, dir, "init")
	writeFile(t, filepath.Join(dir, "apps", "helmrelease.yaml"), helmReleaseMissingSourceFixture)
	runGit(t, dir, "add", ".")

	resolver := &recordingImageVersionResolver{}
	plans, err := updateInTestRepo(context.Background(), ".", UpdateOptions{Managers: []Manager{ManagerHelm}, Check: true, ImageResolver: resolver})

	if err == nil {
		t.Fatal("expected a missing-source failure")
	}
	if len(plans) != 1 {
		t.Fatalf("plans = %#v, want 1", plans)
	}
	if !strings.Contains(plans[0].Error, "HelmRepository flux-system/mission-control-oipa which was not found in the scanned manifests") {
		t.Fatalf("failure = %q, want the actionable HelmRepository-not-found error", plans[0].Error)
	}
	if resolver.calls.Load() != 0 {
		t.Fatalf("resolver was queried %d times; an unresolved chart source must short-circuit before any lookup", resolver.calls.Load())
	}
}

// recordingImageVersionResolver counts version lookups so a test can assert an
// unresolved chart source short-circuits before any network query.
type recordingImageVersionResolver struct{ calls atomic.Int64 }

func (r *recordingImageVersionResolver) Available(context.Context, imageupdate.UpdateTarget) ([]string, error) {
	r.calls.Add(1)
	return nil, nil
}

func (r *recordingImageVersionResolver) ResolveLatestVersions(context.Context, imageupdate.UpdateTarget) (imageupdate.LatestVersions, error) {
	r.calls.Add(1)
	return imageupdate.LatestVersions{}, nil
}

func (r *recordingImageVersionResolver) NewImageValue(context.Context, imageupdate.UpdateTarget, string) (string, error) {
	r.calls.Add(1)
	return "", nil
}

func TestUpdateImageDryRunUsesImageVersionResolver(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	runGit(t, dir, "init")
	writeFile(t, filepath.Join(dir, "apps", "workloads.yaml"), deploymentUpdateFixture)
	runGit(t, dir, "add", ".")

	plans, err := updateInTestRepo(context.Background(), ".", UpdateOptions{Managers: []Manager{ManagerImage}, Filters: []string{"*nginx*"}, DryRun: true, ImageResolver: fakeImageVersionResolver{"nginx": []string{"1.27.0", "1.25.3"}}, SelectCandidates: func(choices []UpdateChoice) ([]UpdateSelection, bool) {
		if len(choices) != 1 {
			t.Fatalf("choices = %#v, want exactly nginx", choices)
		}
		return selectAllAt("1.27.0")(choices)
	}})

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

func TestUpdateStagesChangedFilesAfterWrite(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "package.json"), `{
  "name": "web",
  "dependencies": {"left-pad": "^1.3.0"}
}`)
	writeFile(t, filepath.Join(dir, "package-lock.json"), `{"lockfileVersion": 3}`)
	runner := &updateFakeRunner{
		responses: map[string]CommandResult{
			"npm view left-pad versions --json":                                           {Stdout: `["1.4.0","1.3.0"]`},
			"npm install --package-lock-only --ignore-scripts --save-prod left-pad@1.4.0": {},
		},
		beforeRun: func(cmd Command) {
			if cmd.Name == "npm" && cmd.Args[0] == "install" {
				writeFile(t, filepath.Join(dir, "package.json"), `{"name":"web","dependencies":{"left-pad":"^1.4.0"}}`)
				writeFile(t, filepath.Join(dir, "package-lock.json"), `{"lockfileVersion":3,"version":"1.4.0"}`)
			}
		},
	}
	plans, err := updateInTestRepo(context.Background(), dir, UpdateOptions{Managers: []Manager{ManagerNPM}, Filters: []string{"left-pad"}, Runner: runner, SelectCandidates: selectAllAt("1.4.0")})

	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || !plans[0].Written {
		t.Fatalf("plans = %#v, want one written plan", plans)
	}
	if strings.Join(plans[0].Staged, ",") != "package-lock.json,package.json" {
		t.Fatalf("staged = %#v, want package-lock.json,package.json", plans[0].Staged)
	}
	if plans[0].StageError != "" {
		t.Fatalf("unexpected stage error: %q", plans[0].StageError)
	}
	staged, err := (ExecRunner{}).Run(context.Background(), Command{Dir: dir, Name: "git", Args: []string{"show", ":package.json"}})
	if err != nil || !strings.Contains(staged.Stdout, "1.4.0") {
		t.Fatalf("staged manifest = %q, error %v", staged.Stdout, err)
	}
}

// Dependencies sharing a manifest rewrite the same files, so they stage once
// together rather than each running an identical `git add`.
func TestUpdateStagesOncePerFileSet(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "package.json"), `{
  "name": "web",
  "dependencies": {"left-pad": "^1.3.0", "right-pad": "^2.0.0", "mid-pad": "^3.0.0"}
}`)
	writeFile(t, filepath.Join(dir, "package-lock.json"), `{"lockfileVersion": 3}`)
	runner := &updateFakeRunner{
		succeedByDefault: true,
		responses: map[string]CommandResult{
			"npm view left-pad versions --json":  {Stdout: `["1.4.0","1.3.0"]`},
			"npm view right-pad versions --json": {Stdout: `["2.1.0","2.0.0"]`},
			"npm view mid-pad versions --json":   {Stdout: `["3.1.0","3.0.0"]`},
		},
	}
	plans, err := updateInTestRepo(context.Background(), dir, UpdateOptions{Managers: []Manager{ManagerNPM}, Runner: runner, SelectCandidates: selectAll(func(choice UpdateChoice) string { return choice.Versions[0] })})

	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 3 {
		t.Fatalf("plans = %d, want one per dependency: %#v", len(plans), plans)
	}
	for _, plan := range plans {
		if !plan.Written {
			t.Fatalf("plan = %#v, want written", plan)
		}
		if len(plan.Staged) != 0 {
			t.Fatalf("staged = %#v, a package manager that changed no bytes must stage nothing", plan.Staged)
		}
	}
}

// Dependencies in one manifest section update in a single package-manager run.
func TestUpdateBatchesSameManifestIntoOneCommand(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), `module example.com/app

go 1.22

require (
	github.com/acme/lib v1.0.0
	github.com/acme/tool v1.5.0
)
`)
	runner := &updateFakeRunner{
		succeedByDefault: true,
		responses: map[string]CommandResult{
			"go list -m -versions -json github.com/acme/lib":  {Stdout: `{"Versions":["v1.0.0","v1.1.0"]}`},
			"go list -m -versions -json github.com/acme/tool": {Stdout: `{"Versions":["v1.5.0","v1.6.0"]}`},
		},
	}
	plans, err := updateInTestRepo(context.Background(), dir, UpdateOptions{Managers: []Manager{ManagerGo}, Runner: runner, SelectCandidates: selectAll(func(choice UpdateChoice) string { return choice.Versions[0] })})

	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 2 {
		t.Fatalf("plans = %d, want 2: %#v", len(plans), plans)
	}
	var gets []string
	for _, cmd := range runner.commands {
		if cmd.Name == "go" && len(cmd.Args) > 0 && cmd.Args[0] == "get" {
			gets = append(gets, cmd.String())
		}
	}
	if len(gets) != 1 {
		t.Fatalf("go get invocations = %d, want 1 batched run: %#v", len(gets), gets)
	}
	if !strings.Contains(gets[0], "github.com/acme/lib@v1.1.0") || !strings.Contains(gets[0], "github.com/acme/tool@v1.6.0") {
		t.Fatalf("batched command = %q, want both modules", gets[0])
	}
	for _, plan := range plans {
		if !plan.Written {
			t.Fatalf("plan = %#v, want written", plan)
		}
	}
}

// The --save-* flag applies to the whole invocation, so sections never merge.
func TestUpdateSeparatesBatchesByScope(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package.json"), `{
  "name": "web",
  "dependencies": {"left-pad": "^1.3.0"},
  "devDependencies": {"right-pad": "^2.0.0"}
}`)
	writeFile(t, filepath.Join(dir, "package-lock.json"), `{"lockfileVersion": 3}`)
	runner := &updateFakeRunner{
		succeedByDefault: true,
		responses: map[string]CommandResult{
			"npm view left-pad versions --json":  {Stdout: `["1.4.0","1.3.0"]`},
			"npm view right-pad versions --json": {Stdout: `["2.1.0","2.0.0"]`},
		},
	}
	if _, err := updateInTestRepo(context.Background(), dir, UpdateOptions{Managers: []Manager{ManagerNPM}, Runner: runner, SelectCandidates: selectAll(func(choice UpdateChoice) string { return choice.Versions[0] })}); err != nil {
		t.Fatal(err)
	}
	var installs []string
	for _, cmd := range runner.commands {
		if cmd.Name == "npm" && len(cmd.Args) > 0 && cmd.Args[0] == "install" {
			installs = append(installs, cmd.String())
		}
	}
	sort.Strings(installs)
	want := []string{
		"npm install --package-lock-only --ignore-scripts --save-dev right-pad@2.1.0",
		"npm install --package-lock-only --ignore-scripts --save-prod left-pad@1.4.0",
	}
	if strings.Join(installs, "\n") != strings.Join(want, "\n") {
		t.Fatalf("installs =\n%s\nwant\n%s", strings.Join(installs, "\n"), strings.Join(want, "\n"))
	}
}

// A failed batch cannot say which dependency broke, so it retries individually.
func TestUpdateBatchFailureDoesNotRetryMutatingCommands(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), `module example.com/app

go 1.22

require (
	github.com/acme/lib v1.0.0
	github.com/acme/tool v1.5.0
)
`)
	runner := &updateFakeRunner{
		succeedByDefault: true,
		responses: map[string]CommandResult{
			"go list -m -versions -json github.com/acme/lib":  {Stdout: `{"Versions":["v1.0.0","v1.1.0"]}`},
			"go list -m -versions -json github.com/acme/tool": {Stdout: `{"Versions":["v1.5.0","v1.6.0"]}`},
		},
		errors: map[string]error{
			"go get github.com/acme/lib@v1.1.0 github.com/acme/tool@v1.6.0": errors.New("batch failed"),
			"go get github.com/acme/tool@v1.6.0":                            errors.New("module tool is broken"),
		},
	}
	plans, err := updateInTestRepo(context.Background(), dir, UpdateOptions{Managers: []Manager{ManagerGo}, Runner: runner, SelectCandidates: selectAll(func(choice UpdateChoice) string { return choice.Versions[0] })})

	if err == nil {
		t.Fatal("expected a batch failure")
	}
	for _, plan := range plans {
		if plan.Written || !strings.Contains(plan.Error, "batch failed") || len(plan.Staged) != 0 {
			t.Fatalf("plan = %#v, want an unstaged batch failure", plan)
		}
	}
	var writes int
	for _, cmd := range runner.commands {
		if cmd.Name == "go" && cmd.Args[0] == "get" {
			writes++
		}
	}
	if writes != 1 {
		t.Fatalf("mutating commands = %d, want one without retries", writes)
	}
}

// Updates to one manifest race on its lockfile, so a directory runs serially
// while separate directories overlap.
func TestUpdateSerializesPerDirectoryAndParallelizesAcrossDirectories(t *testing.T) {
	dir := t.TempDir()
	projects := []string{"api", "web", "cli"}
	for _, project := range projects {
		writeFile(t, filepath.Join(dir, project, "package.json"), `{
  "name": "`+project+`",
  "dependencies": {"left-pad": "^1.3.0"},
  "devDependencies": {"right-pad": "^2.0.0"}
}`)
		writeFile(t, filepath.Join(dir, project, "package-lock.json"), `{"lockfileVersion": 3}`)
	}

	tracker := newConcurrencyTracker(len(projects))
	runner := &updateFakeRunner{
		succeedByDefault: true,
		responses: map[string]CommandResult{
			"npm view left-pad versions --json":  {Stdout: `["1.4.0","1.3.0"]`},
			"npm view right-pad versions --json": {Stdout: `["2.1.0","2.0.0"]`},
		},
		beforeRun: func(cmd Command) {
			if cmd.Name == "npm" && len(cmd.Args) > 0 && cmd.Args[0] == "install" {
				tracker.enter(t, cmd.Dir)
			}
		},
		afterRun: func(cmd Command) {
			if cmd.Name == "npm" && len(cmd.Args) > 0 && cmd.Args[0] == "install" {
				tracker.leave(cmd.Dir)
			}
		},
	}

	if _, err := updateInTestRepo(context.Background(), dir, UpdateOptions{Managers: []Manager{ManagerNPM}, Runner: runner, SelectCandidates: selectAll(func(choice UpdateChoice) string { return choice.Versions[0] })}); err != nil {
		t.Fatal(err)
	}
	if got := tracker.maxConcurrentDirs(); got < 2 {
		t.Fatalf("max overlapping directories = %d, want at least 2 running in parallel", got)
	}
}

// concurrencyTracker fails the test if one directory ever has two commands in
// flight, and records how many distinct directories overlapped.
type concurrencyTracker struct {
	mu       sync.Mutex
	inFlight map[string]int
	maxDirs  int
	arrived  chan struct{}
	want     int
}

func newConcurrencyTracker(want int) *concurrencyTracker {
	return &concurrencyTracker{inFlight: map[string]int{}, arrived: make(chan struct{}), want: want}
}

func (c *concurrencyTracker) enter(t *testing.T, dir string) {
	t.Helper()
	c.mu.Lock()
	c.inFlight[dir]++
	if c.inFlight[dir] > 1 {
		c.mu.Unlock()
		t.Errorf("two commands ran concurrently in %q; a manifest must be updated serially", dir)
		return
	}
	if len(c.inFlight) > c.maxDirs {
		c.maxDirs = len(c.inFlight)
	}
	reached := len(c.inFlight) >= min(c.want, updateApplyConcurrency)
	c.mu.Unlock()

	if reached {
		// Release everyone once enough lanes are in flight to prove overlap.
		select {
		case <-c.arrived:
		default:
			close(c.arrived)
		}
		return
	}
	select {
	case <-c.arrived:
	case <-time.After(2 * time.Second):
	}
}

func (c *concurrencyTracker) leave(dir string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inFlight[dir]--
	if c.inFlight[dir] <= 0 {
		delete(c.inFlight, dir)
	}
}

func (c *concurrencyTracker) maxConcurrentDirs() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.maxDirs
}

func TestUpdateDryRunDoesNotStage(t *testing.T) {
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
	plans, err := updateInTestRepo(context.Background(), dir, UpdateOptions{Managers: []Manager{ManagerNPM}, Filters: []string{"left-pad"}, DryRun: true, Runner: runner, SelectCandidates: selectAllAt("1.4.0")})

	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || len(plans[0].Staged) != 0 {
		t.Fatalf("dry-run must not stage files: %#v", plans)
	}
	for _, cmd := range runner.commands {
		if cmd.Name == "git" {
			t.Fatalf("dry-run must not run git add, got %#v", cmd)
		}
	}
}

func TestUpdatedRepoFilesByManager(t *testing.T) {
	cases := []struct {
		candidate UpdateCandidate
		want      string
	}{
		{UpdateCandidate{Manager: ManagerGo}, "go.mod,go.sum"},
		{UpdateCandidate{Manager: ManagerNPM}, "package.json,package-lock.json"},
		{UpdateCandidate{Manager: ManagerPNPM}, "package.json,pnpm-lock.yaml"},
		{UpdateCandidate{Manager: ManagerImage, Target: &imageupdate.UpdateTarget{File: "apps/workloads.yaml"}}, "apps/workloads.yaml"},
		{UpdateCandidate{Manager: ManagerImage}, ""},
	}
	for _, tc := range cases {
		if got := strings.Join(updatedRepoFiles(tc.candidate), ","); got != tc.want {
			t.Fatalf("updatedRepoFiles(%s) = %q, want %q", tc.candidate.Manager, got, tc.want)
		}
	}
}

func TestUpdateImageStagesEditedManifest(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	runGit(t, dir, "init")
	writeFile(t, filepath.Join(dir, "apps", "workloads.yaml"), deploymentUpdateFixture)
	runGit(t, dir, "add", ".")
	runGit(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "init")

	plans, err := updateInTestRepo(context.Background(), ".", UpdateOptions{Managers: []Manager{ManagerImage}, Filters: []string{"*nginx*"}, ImageResolver: fakeImageVersionResolver{"nginx": []string{"1.27.0", "1.25.3"}}, SelectCandidates: selectAllAt("1.27.0")})

	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || !plans[0].Written {
		t.Fatalf("plans = %#v, want one written image plan", plans)
	}
	if strings.Join(plans[0].Staged, ",") != "apps/workloads.yaml" || plans[0].StageError != "" {
		t.Fatalf("unexpected staging result: staged=%#v err=%q", plans[0].Staged, plans[0].StageError)
	}
	staged, err := runGitCmd(context.Background(), dir, "diff", "--name-only", "--cached")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(staged) != "apps/workloads.yaml" {
		t.Fatalf("git staged files = %q, want apps/workloads.yaml", strings.TrimSpace(staged))
	}
}

// The tree groups rows under their manifest and sorts within it — nested
// directories first, then files at that level — so selections come back in a
// stable, readable order regardless of discovery order.
func TestUpdateTreeGroupsAndSortsRowsByFile(t *testing.T) {
	model := newUpdateChoiceTreeModel([]UpdateChoice{
		{Candidate: UpdateCandidate{Manager: ManagerNPM, Name: "zeta", File: "repo/web/package.json", Scope: "dependencies"}},
		{Candidate: UpdateCandidate{Manager: ManagerGo, Name: "github.com/acme/lib", File: "repo/go.mod", Scope: "require"}},
		{Candidate: UpdateCandidate{Manager: ManagerNPM, Name: "alpha", File: "repo/web/package.json", Scope: "dependencies"}},
	})
	selectUpdateTreeStable(model.root)

	var got []string
	for _, selection := range model.selections() {
		got = append(got, selection.Candidate.File+":"+selection.Candidate.Name)
	}
	want := []string{
		"repo/web/package.json:alpha",
		"repo/web/package.json:zeta",
		"repo/go.mod:github.com/acme/lib",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("selection order:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
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
	_, err := updateInTestRepo(context.Background(), t.TempDir(), UpdateOptions{Managers: []Manager{ManagerMaven}, Filters: []string{"*"}})

	if err == nil || !strings.Contains(err.Error(), "unsupported manager") {
		t.Fatalf("expected unsupported manager error, got %v", err)
	}
}

type updateFakeRunner struct {
	mu        sync.Mutex
	responses map[string]CommandResult
	errors    map[string]error
	commands  []Command
	// succeedByDefault makes an unlisted command succeed with empty output,
	// for callers that assert on which commands ran rather than what they
	// printed. Left false, an unlisted command is an error.
	succeedByDefault bool
	// beforeRun and afterRun bracket the command body outside the runner's own
	// lock, so tests can observe which commands overlap.
	beforeRun func(Command)
	afterRun  func(Command)
}

func (r *updateFakeRunner) Run(_ context.Context, cmd Command) (CommandResult, error) {
	r.mu.Lock()
	r.commands = append(r.commands, cmd)
	key := strings.Join(append([]string{cmd.Name}, cmd.Args...), " ")
	result, hasResult := CommandResult{}, false
	if r.responses != nil {
		result, hasResult = r.responses[key]
	}
	var err error
	switch {
	case r.errors != nil && r.errors[key] != nil:
		err = r.errors[key]
	case hasResult, r.succeedByDefault:
	default:
		err = errors.New("unexpected command: " + key)
	}
	before, after := r.beforeRun, r.afterRun
	r.mu.Unlock()

	if before != nil {
		before(cmd)
	}
	if after != nil {
		defer after(cmd)
	}
	return result, err
}

// ran returns each command as it would have been typed, in the order received.
func (r *updateFakeRunner) ran() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.commands))
	for _, cmd := range r.commands {
		out = append(out, cmd.String())
	}
	return out
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

// selectAll answers the picker by taking every choice at the version pick
// returns, standing in for the tree's per-row version mode.
func selectAll(pick func(UpdateChoice) string) CandidateSelector {
	return func(choices []UpdateChoice) ([]UpdateSelection, bool) {
		out := make([]UpdateSelection, 0, len(choices))
		for _, choice := range choices {
			out = append(out, UpdateSelection{UpdateChoice: choice, Version: pick(choice)})
		}
		return out, true
	}
}

// selectAllStable mirrors the tree's default: every row at its highest stable.
func selectAllStable() CandidateSelector {
	return selectAll(func(choice UpdateChoice) string { return choice.LatestStable })
}

// selectAllAt mirrors picking one explicit version for every row.
func selectAllAt(version string) CandidateSelector {
	return selectAll(func(UpdateChoice) string { return version })
}

// stubDefaultImageResolver swaps the factory Update uses when no ImageResolver
// is supplied, so tests can count how many resolvers a single run builds.
func stubDefaultImageResolver(t *testing.T, factory func() ImageVersionResolver) func() {
	t.Helper()
	previous := newDefaultImageResolver
	newDefaultImageResolver = factory
	return func() { newDefaultImageResolver = previous }
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

// countingImageVersionResolver records how many times each target's published
// versions are looked up, so tests can assert duplicate occurrences share a
// single network round-trip.
type countingImageVersionResolver struct {
	versions map[string][]string
	mu       sync.Mutex
	calls    map[string]int
}

func newCountingImageVersionResolver(versions map[string][]string) *countingImageVersionResolver {
	return &countingImageVersionResolver{versions: versions, calls: map[string]int{}}
}

func (r *countingImageVersionResolver) Available(_ context.Context, target imageupdate.UpdateTarget) ([]string, error) {
	r.mu.Lock()
	r.calls[updateTargetName(target)]++
	r.mu.Unlock()
	return sortDependencyVersions(r.versions[updateTargetName(target)]), nil
}

func (r *countingImageVersionResolver) ResolveLatestVersions(_ context.Context, target imageupdate.UpdateTarget) (imageupdate.LatestVersions, error) {
	versions := sortDependencyVersions(r.versions[updateTargetName(target)])
	return imageupdate.LatestVersions{
		Stable:     latestStableVersion(versions),
		Prerelease: latestPrereleaseVersion(versions),
	}, nil
}

func (*countingImageVersionResolver) NewImageValue(_ context.Context, target imageupdate.UpdateTarget, version string) (string, error) {
	return stripImageVersion(target.CurrentValue) + ":" + version, nil
}

func (r *countingImageVersionResolver) callCount(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[name]
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

// helmReleaseMissingSourceFixture references a HelmRepository that is absent from
// the scanned manifests, mirroring the real mission-control-oipa breakage.
const helmReleaseMissingSourceFixture = `apiVersion: helm.toolkit.fluxcd.io/v2beta1
kind: HelmRelease
metadata:
  name: mission-control-oipa
spec:
  chart:
    spec:
      chart: mission-control-oipa-chart
      version: 0.0.9
      sourceRef:
        kind: HelmRepository
        name: mission-control-oipa
        namespace: flux-system
`
