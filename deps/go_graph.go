package deps

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
)

// resolveGoGraph resolves the transitive Go dependency graph by shelling out to
// `go mod graph`. It fails fast with a toolError when the go binary is missing
// or the command fails, suggesting --depth 1 for offline direct-only output.
func resolveGoGraph(ctx context.Context, project Project, opts Options) (*Node, []Warning, error) {
	if _, err := exec.LookPath("go"); err != nil {
		return nil, nil, toolError{fmt.Errorf("go binary not found on PATH; rerun with --depth 1 for offline direct-only output: %w", err)}
	}
	file, err := loadGoModFile(project.Dir)
	if err != nil {
		return nil, nil, err
	}
	mainModule := file.Module.Mod.Path

	result, err := opts.Runner.Run(ctx, Command{
		Dir:  project.Dir,
		Name: "go",
		Args: []string{"mod", "graph"},
		Env:  []string{"GOFLAGS=-mod=mod"},
	})
	if err != nil {
		detail := strings.TrimSpace(result.Stderr)
		if detail == "" {
			detail = err.Error()
		}
		return nil, nil, toolError{fmt.Errorf("go mod graph failed in %s (rerun with --depth 1 for offline direct-only output): %s", project.Dir, detail)}
	}

	graph, err := parseGoModGraph(result.Stdout, mainModule)
	if err != nil {
		return nil, nil, toolError{fmt.Errorf("go mod graph parse failed in %s: %w", project.Dir, err)}
	}

	root := buildTreeFromEdgeGraph(edgeTreeOptions{Graph: graph, MaxDepth: opts.MaxDepth})
	if root == nil {
		return nil, nil, toolError{fmt.Errorf("go mod graph produced no nodes for %s", mainModule)}
	}
	root.Path = filepath.Join(project.Dir, "go.mod")
	root.Source = "go.mod"
	applyGoModMetadata(root, file)

	warning := Warning{
		Manager: ManagerGo,
		Project: project.Dir,
		Message: "go mod graph reports the module requirement graph (MVS inputs); it may list module versions that are not selected in the final build list",
	}
	return root, []Warning{warning}, nil
}

// parseGoModGraph converts `go mod graph` output into an edgeGraph. Each line is
// "<from> <to>" where a node is "path@version" (the main module appears without
// a version). The root key is the main module.
func parseGoModGraph(output, mainModule string) (edgeGraph, error) {
	graph := edgeGraph{
		RootKey: NodeID(ManagerGo, mainModule, ""),
		Nodes:   map[string]*Node{},
		Edges:   map[string][]string{},
	}
	graph.Nodes[graph.RootKey] = NewNode(ManagerGo, mainModule, "")

	seenEdge := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return edgeGraph{}, fmt.Errorf("malformed go mod graph line: %q", line)
		}
		fromKey := goModNodeKey(&graph, fields[0])
		toKey := goModNodeKey(&graph, fields[1])
		edgeKey := fromKey + ">" + toKey
		if seenEdge[edgeKey] {
			continue
		}
		seenEdge[edgeKey] = true
		graph.Edges[fromKey] = append(graph.Edges[fromKey], toKey)
	}
	return graph, nil
}

func goModNodeKey(graph *edgeGraph, field string) string {
	path, version, _ := strings.Cut(field, "@")
	key := NodeID(ManagerGo, path, version)
	if _, ok := graph.Nodes[key]; !ok {
		graph.Nodes[key] = NewNode(ManagerGo, path, version)
	}
	return key
}

// applyGoModMetadata overlays go.mod metadata onto the resolved graph: direct vs
// indirect requirement scope and replace directives (Source/Local).
func applyGoModMetadata(root *Node, file *modfile.File) {
	requires := map[string]*modfile.Require{}
	for _, req := range file.Require {
		requires[req.Mod.Path] = req
	}
	var walk func(n *Node)
	walk = func(n *Node) {
		if n == nil {
			return
		}
		if req, ok := requires[n.Name]; ok {
			n.Direct = !req.Indirect
			if req.Indirect {
				n.Scope = "indirect"
			} else {
				n.Scope = "require"
			}
		}
		if rep := goReplaceFor(file, n.Name, n.Version); rep != nil {
			n.Source = goReplaceSource(rep.New.Path, rep.New.Version)
			n.Local = isLocalRef(rep.New.Path)
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	for _, child := range root.Children {
		walk(child)
	}
}
