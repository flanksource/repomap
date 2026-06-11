package deps

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

const gradleTreeFixture = `
runtimeClasspath - Runtime classpath of source set 'main'.
+--- org.foo:bar:1.0
|    \--- org.baz:qux:2.0
+--- org.abc:def:1.0 -> 1.2
\--- project :submodule
     \--- org.x:y:3.0 (*)

(*) - Indicates repeated occurrences of a transitive dependency subtree.
`

func TestParseGradleDependencyTree(t *testing.T) {
	graph, err := parseGradleDependencyTree(gradleTreeFixture, "app")
	if err != nil {
		t.Fatal(err)
	}
	root := buildTreeFromEdgeGraph(edgeTreeOptions{Graph: graph})
	applyGradleScope(root)

	bar := findChild(root, "org.foo:bar")
	if bar == nil || bar.Version != "1.0" {
		t.Fatalf("bar not parsed: %#v", bar)
	}
	qux := findChild(bar, "org.baz:qux")
	if qux == nil || qux.Depth != 2 {
		t.Fatalf("transitive qux not at depth 2: %#v", qux)
	}
	def := findChild(root, "org.abc:def")
	if def == nil || def.Version != "1.2" {
		t.Fatalf("resolved-version override not applied: %#v", def)
	}
	sub := findChild(root, ":submodule")
	if sub == nil || !sub.Local {
		t.Fatalf("project dependency not marked local: %#v", sub)
	}
	if root.Children[0].Scope != gradleDefaultConfiguration {
		t.Fatalf("scope not applied: %#v", root.Children[0])
	}
}

func TestGradleCommandPrefersWrapper(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "gradlew"), "#!/bin/sh\n")
	sub := filepath.Join(root, "service")
	writeFile(t, filepath.Join(sub, "build.gradle"), "")

	got := gradleCommand(sub)
	if got != filepath.Join(root, "gradlew") {
		t.Fatalf("gradleCommand = %q, want wrapper at repo root", got)
	}

	bare := t.TempDir()
	if gradleCommand(bare) != "gradle" {
		t.Fatalf("expected fallback to gradle binary when no wrapper found")
	}
}

func TestResolveGradleTreeFailureIsFatal(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeRunner{err: fmt.Errorf("exit status 1"), result: CommandResult{Stderr: "Configuration 'runtimeClasspath' not found"}}

	_, _, err := resolveGradleTree(context.Background(), Project{Manager: ManagerGradle, Dir: dir, File: filepath.Join(dir, "build.gradle")}, Options{Runner: runner, MaxDepth: 0})
	if err == nil {
		t.Fatal("expected fatal tool error")
	}
	var te toolError
	if !errors.As(err, &te) {
		t.Fatalf("expected toolError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), gradleDefaultConfiguration) {
		t.Fatalf("error should name the configuration, got %v", err)
	}
}
