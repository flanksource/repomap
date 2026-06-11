package deps

// edgeGraph is a manager-agnostic adjacency representation of a resolved
// dependency graph. Keys identify nodes (typically Node.ID); Nodes holds a
// childless template per key and Edges lists each node's direct children in a
// stable order. Transitive resolvers (go mod graph, maven dependency:tree,
// gradle dependencies) build an edgeGraph and hand it to buildTreeFromEdgeGraph.
type edgeGraph struct {
	RootKey string
	Nodes   map[string]*Node
	Edges   map[string][]string
}

type edgeTreeOptions struct {
	Graph    edgeGraph
	MaxDepth int
}

// buildTreeFromEdgeGraph expands an edgeGraph into a Node tree. Each node is
// expanded (its children materialized) only at its shallowest BFS occurrence;
// deeper re-occurrences become childless leaves so existing analyzeDuplicates
// can tag them and filterAndPruneAt can mark ancestor-path repeats Circular.
// Children beyond MaxDepth (>0) are pruned during construction.
func buildTreeFromEdgeGraph(opts edgeTreeOptions) *Node {
	graph := opts.Graph
	if graph.RootKey == "" || graph.Nodes[graph.RootKey] == nil {
		return nil
	}
	depths := edgeGraphDepths(graph)
	expanded := map[string]bool{}

	var build func(key string, depth int) *Node
	build = func(key string, depth int) *Node {
		template := graph.Nodes[key]
		if template == nil {
			return nil
		}
		node := template.cloneShallow()
		node.Depth = depth
		if depth == depths[key] && !expanded[key] {
			expanded[key] = true
			for _, childKey := range graph.Edges[key] {
				if opts.MaxDepth > 0 && depth+1 > opts.MaxDepth {
					break
				}
				if child := build(childKey, depth+1); child != nil {
					node.Children = append(node.Children, child)
				}
			}
		}
		return node
	}

	root := build(graph.RootKey, 0)
	sortChildren(root)
	return root
}

// markDirectByDepth flags depth-1 nodes as direct dependencies, matching the
// offline manifest resolvers that mark declared dependencies as direct.
func markDirectByDepth(root *Node) {
	if root == nil {
		return
	}
	for _, child := range root.Children {
		if child != nil {
			child.Direct = child.Depth == 1
		}
	}
}

// edgeGraphDepths returns the shortest-path depth of every reachable node from
// RootKey via breadth-first traversal.
func edgeGraphDepths(graph edgeGraph) map[string]int {
	depths := map[string]int{graph.RootKey: 0}
	queue := []string{graph.RootKey}
	for len(queue) > 0 {
		key := queue[0]
		queue = queue[1:]
		for _, child := range graph.Edges[key] {
			if _, seen := depths[child]; seen {
				continue
			}
			depths[child] = depths[key] + 1
			queue = append(queue, child)
		}
	}
	return depths
}
