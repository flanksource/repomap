package deps

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// pnpm nests dependencies under a synthetic importer node, so counting
// root.Children would report the importer instead of the packages, and the
// version lookup would miss the target entirely.
func TestWalkWarmedSkipsSyntheticNodes(t *testing.T) {
	leftPad := NewNode(ManagerPNPM, "left-pad", "1.3.0")
	nested := NewNode(ManagerPNPM, "big-int", "1.0.2")
	leftPad.Children = []*Node{nested}
	importer := NewNode(ManagerPNPM, ".", "")
	importer.Source = "importer"
	importer.Children = []*Node{leftPad}
	root := NewNode(ManagerPNPM, "scratch", "")
	root.Children = []*Node{importer}

	count, version := walkWarmed(root, "left-pad")
	if count != 2 {
		t.Errorf("count = %d, want 2: the importer carries no version and is not a package", count)
	}
	if version != "1.3.0" {
		t.Errorf("version = %q, want 1.3.0: the target sits below the importer", version)
	}
}

func TestWalkWarmedFindsFlatGoRequires(t *testing.T) {
	root := NewNode(ManagerGo, "repomap.local/cachewarm", "")
	root.Children = []*Node{
		NewNode(ManagerGo, "rsc.io/quote", "v1.5.2"),
		NewNode(ManagerGo, "rsc.io/sampler", "v1.3.0"),
	}
	count, version := walkWarmed(root, "rsc.io/quote")
	if count != 2 || version != "v1.5.2" {
		t.Fatalf("walkWarmed = (%d, %q), want (2, v1.5.2)", count, version)
	}
}

func TestWarmCacheRejectsUnsupportedManager(t *testing.T) {
	for _, manager := range []Manager{ManagerMaven, ManagerGradle, ManagerHelm, ManagerImage, Manager("cargo")} {
		_, err := WarmCache(context.Background(), WarmOptions{
			Manager: manager,
			Specs:   []string{"something@1.0.0"},
			Runner:  &updateFakeRunner{},
		})
		if err == nil {
			t.Errorf("manager %q: expected an error", manager)
			continue
		}
		if !strings.Contains(err.Error(), "go, npm, or pnpm") {
			t.Errorf("manager %q: error should list the supported managers, got %v", manager, err)
		}
	}
}

func TestWarmCacheRequiresSpecs(t *testing.T) {
	if _, err := WarmCache(context.Background(), WarmOptions{
		Manager: ManagerGo,
		Runner:  &updateFakeRunner{},
	}); err == nil {
		t.Fatal("expected an error when no specs are given")
	}
}

func TestWarmCacheRunsTheGoStepsInOrder(t *testing.T) {
	runner := &updateFakeRunner{succeedByDefault: true}
	results, err := WarmCache(context.Background(), WarmOptions{
		Manager: ManagerGo,
		Specs:   []string{"github.com/acme/lib@v1.2.3"},
		Build:   true,
		Verify:  true,
		Runner:  runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	result := results[0]
	if result.Error != "" {
		t.Fatalf("unexpected warm error: %s", result.Error)
	}
	if result.Name != "github.com/acme/lib" || result.Spec != "github.com/acme/lib@v1.2.3" {
		t.Errorf("result identity = %q / %q", result.Name, result.Spec)
	}
	if !result.Built || !result.Verified {
		t.Errorf("Built = %v, Verified = %v, want both true", result.Built, result.Verified)
	}
	steps := []string{
		"go mod init repomap.local/cachewarm",
		"go get github.com/acme/lib/...@v1.2.3",
		"go mod download all",
		"go build github.com/acme/lib/...",
		// The verify replay is the same build with GOPROXY=off, which the argv
		// alone does not show; deps/manager/gomod pins the env.
		"go build github.com/acme/lib/...",
	}
	// Reporting where the cache lives runs after the steps, not as one of them.
	want := append(append([]string{}, steps...), "go env GOMODCACHE")
	if got := runner.ran(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands mismatch\n got: %v\nwant: %v", got, want)
	}
	if len(result.Steps) != len(steps) {
		t.Errorf("recorded %d steps, want %d", len(result.Steps), len(steps))
	}
}

// The scratch directory is deleted before the error surfaces, so the message is
// the only handle the user has for reproducing the failure.
func TestWarmCacheFailureAbortsRemainingStepsAndNamesTheCommand(t *testing.T) {
	runner := &updateFakeRunner{
		succeedByDefault: true,
		errors: map[string]error{
			"go get github.com/acme/lib/...@v1.2.3": errors.New("exit status 1"),
		},
		responses: map[string]CommandResult{
			"go get github.com/acme/lib/...@v1.2.3": {Stderr: "module github.com/acme/lib: not found"},
		},
	}
	results, err := WarmCache(context.Background(), WarmOptions{
		Manager: ManagerGo,
		Specs:   []string{"github.com/acme/lib@v1.2.3"},
		Build:   true,
		Runner:  runner,
	})
	if err == nil {
		t.Fatal("WarmCache should report an error when a spec fails, so the CLI exits non-zero")
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	for _, want := range []string{"go get github.com/acme/lib/...@v1.2.3", "not found"} {
		if !strings.Contains(results[0].Error, want) {
			t.Errorf("result error %q should contain %q", results[0].Error, want)
		}
		// The returned error is what a caller that only prints err sees, and the
		// scratch dir is gone by then, so the detail has to survive into it too.
		if !strings.Contains(err.Error(), want) {
			t.Errorf("returned error %q should contain %q", err, want)
		}
	}
	if results[0].Built {
		t.Error("Built should stay false when the warm failed")
	}
	// init ran, get failed, and download/build must not have been attempted.
	want := []string{"go mod init repomap.local/cachewarm", "go get github.com/acme/lib/...@v1.2.3"}
	if got := runner.ran(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands mismatch\n got: %v\nwant: %v", got, want)
	}
}

// pnpm's probe output decides the build flags, so the orchestrator has to run it
// and feed the result into Steps.
func TestWarmCacheFeedsTheProbeIntoSteps(t *testing.T) {
	runner := &updateFakeRunner{
		succeedByDefault: true,
		responses: map[string]CommandResult{
			"pnpm --version": {Stdout: "10.7.0\n"},
		},
	}
	if _, err := WarmCache(context.Background(), WarmOptions{
		Manager: ManagerPNPM,
		Specs:   []string{"left-pad@1.3.0"},
		Build:   true,
		Runner:  runner,
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"pnpm --version",
		"pnpm install --config.dangerouslyAllowAllBuilds=true",
		"pnpm store path",
	}
	if got := runner.ran(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestWarmCacheWarmsEverySpec(t *testing.T) {
	runner := &updateFakeRunner{succeedByDefault: true}
	results, err := WarmCache(context.Background(), WarmOptions{
		Manager: ManagerGo,
		Specs:   []string{"github.com/acme/one@v1.0.0", "github.com/acme/two@v2.0.0"},
		Runner:  runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	// Specs are warmed concurrently, so assert identity by position rather than
	// relying on command ordering.
	if results[0].Spec != "github.com/acme/one@v1.0.0" || results[1].Spec != "github.com/acme/two@v2.0.0" {
		t.Fatalf("results should stay in spec order, got %q and %q", results[0].Spec, results[1].Spec)
	}
	for _, result := range results {
		if result.Error != "" {
			t.Errorf("spec %q failed: %s", result.Spec, result.Error)
		}
	}
}

// A GitHub slug and a repository URL are what a user has to hand, and both used
// to reach `go get` verbatim and fail with the toolchain's "malformed module
// path".
func TestWarmCacheNormalisesGoRepoShorthand(t *testing.T) {
	for _, spec := range []string{
		"flanksource/commons",
		"https://github.com/flanksource/commons",
		"git@github.com:flanksource/commons.git",
	} {
		t.Run(spec, func(t *testing.T) {
			runner := &updateFakeRunner{succeedByDefault: true}
			results, err := WarmCache(context.Background(), WarmOptions{
				Manager: ManagerGo,
				Specs:   []string{spec},
				Runner:  runner,
			})
			if err != nil {
				t.Fatal(err)
			}
			if results[0].Name != "github.com/flanksource/commons" {
				t.Errorf("Name = %q, want the canonical module path", results[0].Name)
			}
			if results[0].Spec != spec {
				t.Errorf("Spec = %q, should stay the spec the user typed", results[0].Spec)
			}
			want := []string{
				"go mod init repomap.local/cachewarm",
				"go get github.com/flanksource/commons/...@latest",
				"go mod download all",
				"go env GOMODCACHE",
			}
			if got := runner.ran(); strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Fatalf("commands mismatch\n got: %v\nwant: %v", got, want)
			}
		})
	}
}

// A spec with no host to infer is repomap's to reject, before a scratch project
// is created or the go toolchain is asked anything.
func TestWarmCacheRejectsAGoSpecThatIsNotAModulePath(t *testing.T) {
	runner := &updateFakeRunner{succeedByDefault: true}
	results, err := WarmCache(context.Background(), WarmOptions{
		Manager: ManagerGo,
		Specs:   []string{"commons"},
		Runner:  runner,
	})
	if err == nil {
		t.Fatal("expected an error: \"commons\" names no host and cannot become a module path")
	}
	if !strings.Contains(results[0].Error, "github.com/owner/repo") {
		t.Errorf("error %q should say what a module path looks like", results[0].Error)
	}
	if got := runner.ran(); len(got) != 0 {
		t.Errorf("no command should run for an unusable spec, got %v", got)
	}
}

// npm names are already registry names, and a scoped one must not be mistaken
// for a repo shorthand.
func TestWarmCacheLeavesScopedNPMNamesAlone(t *testing.T) {
	runner := &updateFakeRunner{succeedByDefault: true}
	results, err := WarmCache(context.Background(), WarmOptions{
		Manager: ManagerNPM,
		Specs:   []string{"@scope/pkg@1.0.0"},
		Runner:  runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Name != "@scope/pkg" || results[0].RequestedVersion != "1.0.0" {
		t.Fatalf("result identity = %q@%q, want @scope/pkg@1.0.0", results[0].Name, results[0].RequestedVersion)
	}
}

// A bad spec must be reported against that spec rather than aborting the run
// before the others are warmed.
func TestWarmCacheReportsABadSpecWithoutSkippingTheRest(t *testing.T) {
	runner := &updateFakeRunner{succeedByDefault: true}
	results, err := WarmCache(context.Background(), WarmOptions{
		Manager: ManagerGo,
		Specs:   []string{"left-pad@", "github.com/acme/lib@v1.0.0"},
		Runner:  runner,
	})
	if err == nil {
		t.Fatal("expected a non-nil error because one spec is invalid")
	}
	if results[0].Error == "" {
		t.Error("the invalid spec should carry an error")
	}
	if results[1].Error != "" {
		t.Errorf("the valid spec should still have been warmed, got %q", results[1].Error)
	}
}
