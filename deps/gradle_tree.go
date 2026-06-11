package deps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const gradleDefaultConfiguration = "runtimeClasspath"

// resolveGradleTree resolves the transitive Gradle dependency graph by running
// the `dependencies` task for the runtimeClasspath configuration. It fails fast
// with a toolError when gradle is missing, the command fails, or the
// configuration is absent, suggesting --depth 1 for offline output.
func resolveGradleTree(ctx context.Context, project Project, opts Options) (*Node, []Warning, error) {
	command := gradleCommand(project.Dir)
	result, err := opts.Runner.Run(ctx, Command{
		Dir:  project.Dir,
		Name: command,
		Args: []string{"-q", "dependencies", "--configuration", gradleDefaultConfiguration},
	})
	if err != nil {
		detail := strings.TrimSpace(result.Stderr)
		if detail == "" {
			detail = err.Error()
		}
		return nil, nil, toolError{fmt.Errorf("gradle dependencies --configuration %s failed in %s (rerun with --depth 1 for offline direct-only output): %s", gradleDefaultConfiguration, project.Dir, detail)}
	}

	graph, err := parseGradleDependencyTree(result.Stdout, filepath.Base(filepath.Dir(project.File)))
	if err != nil {
		return nil, nil, toolError{fmt.Errorf("gradle dependencies parse failed in %s: %w", project.Dir, err)}
	}
	root := buildTreeFromEdgeGraph(edgeTreeOptions{Graph: graph, MaxDepth: opts.MaxDepth})
	if root == nil {
		return nil, nil, toolError{fmt.Errorf("gradle dependencies produced no nodes for %s", project.Dir)}
	}
	root.Path = project.File
	root.Source = filepath.Base(project.File)
	applyGradleScope(root)
	markDirectByDepth(root)
	return root, nil, nil
}

// gradleCommand prefers a ./gradlew wrapper found by walking up from dir, and
// falls back to a gradle binary on PATH.
func gradleCommand(dir string) string {
	current := dir
	for {
		wrapper := filepath.Join(current, "gradlew")
		if info, err := os.Stat(wrapper); err == nil && !info.IsDir() {
			return wrapper
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return "gradle"
}

// parseGradleDependencyTree parses the indented ASCII tree printed by the gradle
// `dependencies` task into an edgeGraph rooted at a synthetic project node.
// It resolves "-> version" overrides, treats "(*)" repeated subtrees as leaves,
// skips "(c)" constraint and "(n)" unresolved entries, and marks "project :x"
// nodes as local.
func parseGradleDependencyTree(output, rootName string) (edgeGraph, error) {
	rootKey := NodeID(ManagerGradle, rootName, "")
	graph := edgeGraph{
		RootKey: rootKey,
		Nodes:   map[string]*Node{rootKey: NewNode(ManagerGradle, rootName, "")},
		Edges:   map[string][]string{},
	}
	type frame struct {
		depth int
		key   string
	}
	stack := []frame{}
	seenEdge := map[string]bool{}

	for _, raw := range strings.Split(output, "\n") {
		col, ok := gradleMarkerColumn(raw)
		if !ok {
			continue
		}
		depth := col/gradleIndentWidth + 1
		entry := strings.TrimSpace(raw[col+gradleMarkerWidth:])
		node, skip := gradleNodeFromEntry(entry)
		if skip {
			continue
		}
		key := node.ID
		if _, exists := graph.Nodes[key]; !exists {
			graph.Nodes[key] = node
		}
		for len(stack) > 0 && stack[len(stack)-1].depth >= depth {
			stack = stack[:len(stack)-1]
		}
		parentKey := rootKey
		if len(stack) > 0 {
			parentKey = stack[len(stack)-1].key
		}
		edgeKey := parentKey + ">" + key
		if !seenEdge[edgeKey] {
			seenEdge[edgeKey] = true
			graph.Edges[parentKey] = append(graph.Edges[parentKey], key)
		}
		stack = append(stack, frame{depth: depth, key: key})
	}
	return graph, nil
}

const (
	gradleIndentWidth = 5
	gradleMarkerWidth = 5 // "+--- " or "\--- "
)

func gradleMarkerColumn(line string) (int, bool) {
	for _, marker := range []string{"+--- ", "\\--- "} {
		idx := strings.Index(line, marker)
		if idx < 0 {
			continue
		}
		if gradleIsIndent(line[:idx]) {
			return idx, true
		}
	}
	return 0, false
}

func gradleIsIndent(prefix string) bool {
	for _, r := range prefix {
		if r != ' ' && r != '|' {
			return false
		}
	}
	return true
}

func gradleNodeFromEntry(entry string) (*Node, bool) {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return nil, true
	}
	if strings.HasSuffix(entry, "(c)") || strings.HasSuffix(entry, "(n)") {
		return nil, true
	}
	entry = strings.TrimSpace(strings.TrimSuffix(entry, "(*)"))
	if strings.HasPrefix(entry, "project ") {
		name := strings.TrimSpace(strings.TrimPrefix(entry, "project"))
		node := NewNode(ManagerGradle, name, "")
		node.Local = true
		return node, false
	}
	left, right, hasArrow := strings.Cut(entry, " -> ")
	name, version := gradleCoordinate(left)
	if hasArrow {
		version = gradleResolvedVersion(strings.TrimSpace(right))
	}
	return NewNode(ManagerGradle, name, version), false
}

func gradleCoordinate(text string) (name, version string) {
	parts := strings.Split(strings.TrimSpace(text), ":")
	switch len(parts) {
	case 3:
		return parts[0] + ":" + parts[1], parts[2]
	case 2:
		return parts[0] + ":" + parts[1], ""
	default:
		return strings.TrimSpace(text), ""
	}
}

func gradleResolvedVersion(right string) string {
	if strings.Contains(right, ":") {
		parts := strings.Split(right, ":")
		return parts[len(parts)-1]
	}
	return right
}

func applyGradleScope(root *Node) {
	var walk func(n *Node)
	walk = func(n *Node) {
		if n == nil {
			return
		}
		if n.Scope == "" {
			n.Scope = gradleDefaultConfiguration
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	for _, child := range root.Children {
		walk(child)
	}
}
