package deps

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	result CommandResult
	err    error
	calls  []Command
	onRun  func(Command) (CommandResult, error)
}

func (f *fakeRunner) Run(_ context.Context, cmd Command) (CommandResult, error) {
	f.calls = append(f.calls, cmd)
	if f.onRun != nil {
		return f.onRun(cmd)
	}
	return f.result, f.err
}

func TestParseGoModGraph(t *testing.T) {
	output := strings.Join([]string{
		"github.com/acme/app github.com/acme/lib@v1.2.3",
		"github.com/acme/app github.com/acme/other@v0.5.0",
		"github.com/acme/lib@v1.2.3 github.com/acme/dep@v0.1.0",
		"github.com/acme/other@v0.5.0 github.com/acme/dep@v0.1.0",
		"",
	}, "\n")

	graph, err := parseGoModGraph(output, "github.com/acme/app")
	if err != nil {
		t.Fatal(err)
	}
	rootKey := NodeID(ManagerGo, "github.com/acme/app", "")
	if graph.RootKey != rootKey {
		t.Fatalf("root key = %q, want %q", graph.RootKey, rootKey)
	}
	if len(graph.Edges[rootKey]) != 2 {
		t.Fatalf("root edges = %d, want 2", len(graph.Edges[rootKey]))
	}
	depKey := NodeID(ManagerGo, "github.com/acme/dep", "v0.1.0")
	depths := edgeGraphDepths(graph)
	if depths[depKey] != 2 {
		t.Fatalf("dep depth = %d, want 2 (diamond shortest path)", depths[depKey])
	}
}

func TestResolveGoGraphMergesGoModMetadata(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), `module github.com/acme/app

go 1.22

require (
	github.com/acme/lib v1.2.3
	github.com/acme/indirect v0.1.0 // indirect
)

replace github.com/acme/lib => ../lib
`)
	runner := &fakeRunner{result: CommandResult{Stdout: strings.Join([]string{
		"github.com/acme/app github.com/acme/lib@v1.2.3",
		"github.com/acme/app github.com/acme/indirect@v0.1.0",
		"github.com/acme/lib@v1.2.3 github.com/acme/dep@v0.3.0",
		"",
	}, "\n")}}

	root, warnings, err := resolveGoGraph(context.Background(), Project{Manager: ManagerGo, Dir: dir}, Options{Runner: runner, MaxDepth: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0].Message, "MVS") {
		t.Fatalf("expected MVS warning, got %#v", warnings)
	}
	lib := findChild(root, "github.com/acme/lib")
	if lib == nil || !lib.Direct || lib.Scope != "require" || !lib.Local || lib.Source != "../lib" {
		t.Fatalf("lib metadata not merged: %#v", lib)
	}
	indirect := findChild(root, "github.com/acme/indirect")
	if indirect == nil || indirect.Direct || indirect.Scope != "indirect" {
		t.Fatalf("indirect metadata not merged: %#v", indirect)
	}
	dep := findChild(lib, "github.com/acme/dep")
	if dep == nil || dep.Depth != 2 {
		t.Fatalf("transitive dep not resolved at depth 2: %#v", dep)
	}
}

func TestResolveGoGraphToolFailureIsFatal(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), `module github.com/acme/app

go 1.22

require github.com/acme/lib v1.2.3
`)
	runner := &fakeRunner{err: fmt.Errorf("exit status 1"), result: CommandResult{Stderr: "go: updates to go.mod needed"}}

	_, err := Scan(context.Background(), dir, Options{
		Managers: []Manager{ManagerGo},
		MaxDepth: 0,
		Runner:   runner,
		Now:      func() time.Time { return time.Unix(1, 0).UTC() },
	})
	if err == nil {
		t.Fatal("expected fatal tool error to abort scan")
	}
	if !strings.Contains(err.Error(), "--depth 1") {
		t.Fatalf("error should suggest --depth 1, got %v", err)
	}
}

func TestScanGoDepthOneStaysOffline(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), `module github.com/acme/app

go 1.22

require github.com/acme/lib v1.2.3
`)
	runner := &fakeRunner{}

	got, err := Scan(context.Background(), dir, Options{
		Managers: []Manager{ManagerGo},
		MaxDepth: 1,
		Runner:   runner,
		Now:      func() time.Time { return time.Unix(1, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner should not be called at depth 1, got %d calls", len(runner.calls))
	}
	if len(got.Roots) != 1 {
		t.Fatalf("roots = %d, want 1", len(got.Roots))
	}
}
