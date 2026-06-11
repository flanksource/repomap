package deps

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// resolveMavenTree resolves the transitive Maven dependency graph by running
// `mvn dependency:tree` with TGF output. It fails fast with a toolError when mvn
// is missing or the command fails, suggesting --depth 1 for offline output.
func resolveMavenTree(ctx context.Context, project Project, opts Options) (*Node, []Warning, error) {
	tmp, err := os.CreateTemp("", "repomap-mvn-tree-*.tgf")
	if err != nil {
		return nil, nil, err
	}
	outFile := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(outFile) }()

	result, err := opts.Runner.Run(ctx, Command{
		Dir:  project.Dir,
		Name: "mvn",
		Args: []string{"-B", "-N", "dependency:tree", "-DoutputType=tgf", "-DoutputFile=" + outFile},
	})
	if err != nil {
		detail := strings.TrimSpace(result.Stderr)
		if detail == "" {
			detail = err.Error()
		}
		return nil, nil, toolError{fmt.Errorf("mvn dependency:tree failed in %s (rerun with --depth 1 for offline direct-only output): %s", project.Dir, detail)}
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		return nil, nil, toolError{fmt.Errorf("mvn dependency:tree produced no output file for %s: %w", project.Dir, err)}
	}
	graph, err := parseMavenTGF(string(data))
	if err != nil {
		return nil, nil, toolError{fmt.Errorf("mvn dependency:tree parse failed in %s: %w", project.Dir, err)}
	}

	root := buildTreeFromEdgeGraph(edgeTreeOptions{Graph: graph, MaxDepth: opts.MaxDepth})
	if root == nil {
		return nil, nil, toolError{fmt.Errorf("mvn dependency:tree produced no nodes for %s", project.Dir)}
	}
	root.Path = project.File
	root.Source = "pom.xml"
	markDirectByDepth(root)
	return root, nil, nil
}

// parseMavenTGF parses Trivial Graph Format produced by dependency:tree. The
// node section lists "<id> <label>" where label is
// groupId:artifactId:type[:classifier]:version[:scope]; the edge section after
// the "#" separator lists "<fromId> <toId> [edge-label]".
func parseMavenTGF(output string) (edgeGraph, error) {
	lines := strings.Split(output, "\n")
	graph := edgeGraph{Nodes: map[string]*Node{}, Edges: map[string][]string{}}
	idToKey := map[string]string{}
	inEdges := false
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if line == "#" {
			inEdges = true
			continue
		}
		if !inEdges {
			id, label, ok := strings.Cut(line, " ")
			if !ok {
				return edgeGraph{}, fmt.Errorf("malformed TGF node line: %q", line)
			}
			node, err := mavenNodeFromLabel(strings.TrimSpace(label))
			if err != nil {
				return edgeGraph{}, err
			}
			idToKey[id] = node.ID
			graph.Nodes[node.ID] = node
			if graph.RootKey == "" {
				graph.RootKey = node.ID
			}
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return edgeGraph{}, fmt.Errorf("malformed TGF edge line: %q", line)
		}
		fromKey, fromOK := idToKey[fields[0]]
		toKey, toOK := idToKey[fields[1]]
		if !fromOK || !toOK {
			return edgeGraph{}, fmt.Errorf("TGF edge references unknown node: %q", line)
		}
		graph.Edges[fromKey] = append(graph.Edges[fromKey], toKey)
	}
	if graph.RootKey == "" {
		return edgeGraph{}, fmt.Errorf("TGF output contained no nodes")
	}
	return graph, nil
}

func mavenNodeFromLabel(label string) (*Node, error) {
	parts := strings.Split(label, ":")
	if len(parts) < 4 {
		return nil, fmt.Errorf("malformed maven coordinate %q", label)
	}
	group, artifact := parts[0], parts[1]
	var version, scope string
	switch len(parts) {
	case 4: // group:artifact:type:version
		version = parts[3]
	case 5: // group:artifact:type:version:scope
		version, scope = parts[3], parts[4]
	default: // group:artifact:type:classifier:version:scope (classifier ignored)
		version, scope = parts[len(parts)-2], parts[len(parts)-1]
	}
	node := NewNode(ManagerMaven, group+":"+artifact, version)
	node.Scope = scope
	return node, nil
}
