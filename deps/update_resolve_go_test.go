package deps

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

const twoModuleGoMod = `module example.com/app

go 1.22

require (
	github.com/acme/lib v1.0.0
	github.com/acme/tool v1.5.0
)
`

// goListBatchKey is the command the batched lookup issues for the fixture.
const goListBatchKey = "go list -m -versions -json -e github.com/acme/lib github.com/acme/tool"

func goListRunner(stdout string) *updateFakeRunner {
	return &updateFakeRunner{
		succeedByDefault: true,
		responses:        map[string]CommandResult{goListBatchKey: {Stdout: stdout}},
	}
}

func checkPlansFor(t *testing.T, dir string, runner CommandRunner) map[string]UpdatePlan {
	t.Helper()
	plans, err := Update(context.Background(), dir, UpdateOptions{
		Managers: []Manager{ManagerGo},
		Check:    true,
		Runner:   runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]UpdatePlan{}
	for _, plan := range plans {
		byName[plan.Name] = plan
	}
	return byName
}

// The toolchain loads the module graph once per invocation, so a directory's
// modules resolve in one run rather than one each.
func TestGoVersionLookupBatchesPerDirectory(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), twoModuleGoMod)
	runner := goListRunner(`{"Path":"github.com/acme/lib","Versions":["v1.0.0","v1.1.0"]}
{"Path":"github.com/acme/tool","Versions":["v1.5.0","v1.6.0"]}`)

	plans := checkPlansFor(t, dir, runner)

	var lists []string
	for _, cmd := range runner.commands {
		if cmd.Name == "go" && len(cmd.Args) > 0 && cmd.Args[0] == "list" {
			lists = append(lists, cmd.String())
		}
	}
	if len(lists) != 1 {
		t.Fatalf("go list invocations = %d, want 1 batched run: %#v", len(lists), lists)
	}
	if plans["github.com/acme/lib"].NewVersion != "v1.1.0" {
		t.Fatalf("lib plan = %#v, want v1.1.0", plans["github.com/acme/lib"])
	}
	if plans["github.com/acme/tool"].NewVersion != "v1.6.0" {
		t.Fatalf("tool plan = %#v, want v1.6.0", plans["github.com/acme/tool"])
	}
}

// -e reports a broken module inline, so its siblings still resolve.
func TestGoVersionLookupBatchAttributesPerModuleErrors(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), twoModuleGoMod)
	runner := goListRunner(`{"Path":"github.com/acme/lib","Versions":["v1.0.0","v1.1.0"]}
{"Path":"github.com/acme/tool","Error":{"Err":"module github.com/acme/tool: not found"}}`)

	results, err := Update(context.Background(), dir, UpdateOptions{Managers: []Manager{ManagerGo}, Check: true, Runner: runner})
	if err == nil {
		t.Fatal("expected a module-resolution failure")
	}
	plans := map[string]UpdatePlan{}
	for _, plan := range results {
		plans[plan.Name] = plan
	}

	if plans["github.com/acme/lib"].NewVersion != "v1.1.0" {
		t.Fatalf("lib plan = %#v, want the healthy sibling resolved", plans["github.com/acme/lib"])
	}
	if got := plans["github.com/acme/tool"].Error; got != "module github.com/acme/tool: not found" {
		t.Fatalf("tool skip reason = %q, want the module's own error", got)
	}
}

// A module the batch never mentions falls back to its own lookup rather than
// silently disappearing.
func TestGoVersionLookupBatchFallsBackForUnreportedModules(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), twoModuleGoMod)
	runner := goListRunner(`{"Path":"github.com/acme/lib","Versions":["v1.0.0","v1.1.0"]}`)
	runner.responses["go list -m -versions -json github.com/acme/tool"] = CommandResult{
		Stdout: `{"Path":"github.com/acme/tool","Versions":["v1.5.0","v1.6.0"]}`,
	}

	plans := checkPlansFor(t, dir, runner)

	if plans["github.com/acme/tool"].NewVersion != "v1.6.0" {
		t.Fatalf("tool plan = %#v, want the per-module fallback to resolve it", plans["github.com/acme/tool"])
	}
	var fellBack bool
	for _, cmd := range runner.commands {
		if strings.Contains(cmd.String(), "go list -m -versions -json github.com/acme/tool") {
			fellBack = true
		}
	}
	if !fellBack {
		t.Fatalf("expected a per-module fallback lookup, got %#v", runner.ran())
	}
}

// Unparseable output tells us nothing about any module, so every one falls back.
func TestGoVersionLookupBatchFallsBackOnUnparseableOutput(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), twoModuleGoMod)
	runner := goListRunner("this is not json")
	runner.responses["go list -m -versions -json github.com/acme/lib"] = CommandResult{
		Stdout: `{"Path":"github.com/acme/lib","Versions":["v1.0.0","v1.1.0"]}`,
	}
	runner.responses["go list -m -versions -json github.com/acme/tool"] = CommandResult{
		Stdout: `{"Path":"github.com/acme/tool","Versions":["v1.5.0","v1.6.0"]}`,
	}

	plans := checkPlansFor(t, dir, runner)

	if plans["github.com/acme/lib"].NewVersion != "v1.1.0" || plans["github.com/acme/tool"].NewVersion != "v1.6.0" {
		t.Fatalf("plans = %#v, want both resolved via per-module fallback", plans)
	}
}

// Modules in different directories resolve against different module graphs, so
// they never share an invocation.
func TestGoVersionLookupDoesNotBatchAcrossDirectories(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a", "go.mod"), "module example.com/a\n\ngo 1.22\n\nrequire github.com/acme/lib v1.0.0\n")
	writeFile(t, filepath.Join(dir, "b", "go.mod"), "module example.com/b\n\ngo 1.22\n\nrequire github.com/acme/tool v1.5.0\n")
	runner := &updateFakeRunner{
		responses: map[string]CommandResult{
			"go list -m -versions -json github.com/acme/lib":  {Stdout: `{"Versions":["v1.0.0","v1.1.0"]}`},
			"go list -m -versions -json github.com/acme/tool": {Stdout: `{"Versions":["v1.5.0","v1.6.0"]}`},
		},
	}

	plans := checkPlansFor(t, dir, runner)

	if len(plans) != 2 {
		t.Fatalf("plans = %#v, want one per module", plans)
	}
	dirs := map[string]bool{}
	for _, cmd := range runner.commands {
		if cmd.Name == "go" {
			dirs[cmd.Dir] = true
		}
	}
	if len(dirs) != 2 {
		t.Fatalf("go list directories = %#v, want one invocation per module directory", dirs)
	}
}
