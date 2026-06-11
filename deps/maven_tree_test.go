package deps

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

const mavenTGFFixture = `1 com.example:app:jar:1.0
2 org.foo:bar:jar:2.0:compile
3 org.baz:qux:jar:3.0:runtime
#
1 2 compile
2 3 runtime
`

func TestParseMavenTGF(t *testing.T) {
	graph, err := parseMavenTGF(mavenTGFFixture)
	if err != nil {
		t.Fatal(err)
	}
	rootKey := NodeID(ManagerMaven, "com.example:app", "1.0")
	if graph.RootKey != rootKey {
		t.Fatalf("root key = %q, want %q", graph.RootKey, rootKey)
	}
	bar := graph.Nodes[NodeID(ManagerMaven, "org.foo:bar", "2.0")]
	if bar == nil || bar.Scope != "compile" {
		t.Fatalf("bar scope not parsed: %#v", bar)
	}
	root := buildTreeFromEdgeGraph(edgeTreeOptions{Graph: graph})
	bar = findChild(root, "org.foo:bar")
	if bar == nil {
		t.Fatalf("bar not a direct child: %#v", root)
	}
	qux := findChild(bar, "org.baz:qux")
	if qux == nil || qux.Depth != 2 || qux.Scope != "runtime" {
		t.Fatalf("qux transitive node not at depth 2: %#v", qux)
	}
}

func TestResolveMavenTreeCommandArgs(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeRunner{onRun: func(cmd Command) (CommandResult, error) {
		outFile := mavenOutputFile(cmd.Args)
		if outFile == "" {
			return CommandResult{}, fmt.Errorf("missing -DoutputFile arg")
		}
		if err := os.WriteFile(outFile, []byte(mavenTGFFixture), 0o644); err != nil {
			return CommandResult{}, err
		}
		return CommandResult{}, nil
	}}

	root, _, err := resolveMavenTree(context.Background(), Project{Manager: ManagerMaven, Dir: dir, File: dir + "/pom.xml"}, Options{Runner: runner, MaxDepth: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("expected 1 mvn call, got %d", len(runner.calls))
	}
	args := strings.Join(runner.calls[0].Args, " ")
	for _, want := range []string{"-B", "-N", "dependency:tree", "-DoutputType=tgf", "-DoutputFile="} {
		if !strings.Contains(args, want) {
			t.Fatalf("mvn args missing %q, got %q", want, args)
		}
	}
	if root.Children[0].Name != "org.foo:bar" || !root.Children[0].Direct {
		t.Fatalf("direct dependency not marked: %#v", root.Children)
	}
}

func TestResolveMavenTreeFailureIsFatal(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeRunner{err: fmt.Errorf("exit status 1"), result: CommandResult{Stderr: "BUILD FAILURE"}}

	_, _, err := resolveMavenTree(context.Background(), Project{Manager: ManagerMaven, Dir: dir, File: dir + "/pom.xml"}, Options{Runner: runner, MaxDepth: 0})
	if err == nil {
		t.Fatal("expected fatal tool error")
	}
	var te toolError
	if !errors.As(err, &te) {
		t.Fatalf("expected toolError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "--depth 1") {
		t.Fatalf("error should suggest --depth 1, got %v", err)
	}
}

func mavenOutputFile(args []string) string {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-DoutputFile=") {
			return strings.TrimPrefix(arg, "-DoutputFile=")
		}
	}
	return ""
}
