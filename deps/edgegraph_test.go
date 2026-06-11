package deps

import "testing"

// diamondGraph: root -> a, b; a -> shared; b -> shared.
func diamondGraph() edgeGraph {
	mk := func(name string) *Node { return NewNode(ManagerGo, name, "v1") }
	keyA := mk("a").ID
	keyB := mk("b").ID
	keyShared := mk("shared").ID
	root := mk("root")
	return edgeGraph{
		RootKey: root.ID,
		Nodes: map[string]*Node{
			root.ID:   root,
			keyA:      mk("a"),
			keyB:      mk("b"),
			keyShared: mk("shared"),
		},
		Edges: map[string][]string{
			root.ID: {keyA, keyB},
			keyA:    {keyShared},
			keyB:    {keyShared},
		},
	}
}

func TestEdgeGraphDepthsDiamond(t *testing.T) {
	graph := diamondGraph()
	depths := edgeGraphDepths(graph)
	if depths[NewNode(ManagerGo, "shared", "v1").ID] != 2 {
		t.Fatalf("shared depth = %d, want 2", depths[NewNode(ManagerGo, "shared", "v1").ID])
	}
	if depths[NewNode(ManagerGo, "a", "v1").ID] != 1 {
		t.Fatalf("a depth = %d, want 1", depths[NewNode(ManagerGo, "a", "v1").ID])
	}
}

func TestBuildTreeFirstOccurrenceExpandsOnceDuplicateLeaf(t *testing.T) {
	root := buildTreeFromEdgeGraph(edgeTreeOptions{Graph: diamondGraph()})
	if root == nil || len(root.Children) != 2 {
		t.Fatalf("root children = %#v", root)
	}
	a := findChild(root, "a")
	b := findChild(root, "b")
	occurrences := 0
	for _, branch := range []*Node{a, b} {
		shared := findChild(branch, "shared")
		if shared == nil {
			continue
		}
		occurrences++
		if len(shared.Children) != 0 {
			t.Fatalf("shared should be a childless leaf at re-occurrence, got %#v", shared)
		}
	}
	if occurrences != 2 {
		t.Fatalf("shared should appear under both branches, got %d", occurrences)
	}
}

func TestBuildTreeTerminatesOnCycle(t *testing.T) {
	mk := func(name string) *Node { return NewNode(ManagerGo, name, "v1") }
	rootKey := mk("root").ID
	aKey := mk("a").ID
	graph := edgeGraph{
		RootKey: rootKey,
		Nodes:   map[string]*Node{rootKey: mk("root"), aKey: mk("a")},
		Edges: map[string][]string{
			rootKey: {aKey},
			aKey:    {rootKey}, // cycle a -> root
		},
	}
	root := buildTreeFromEdgeGraph(edgeTreeOptions{Graph: graph})
	a := findChild(root, "a")
	if a == nil {
		t.Fatalf("expected child a, got %#v", root)
	}
	// root re-occurs as a leaf under a (deeper than its BFS depth 0), not expanded.
	rootLeaf := findChild(a, "root")
	if rootLeaf == nil || len(rootLeaf.Children) != 0 {
		t.Fatalf("cycle back-edge should be a leaf, got %#v", rootLeaf)
	}
	// filterAndPrune marks the ancestor-path repeat as circular.
	pruned := filterAndPrune(root, nil, 0)
	if leaf := findChild(findChild(pruned, "a"), "root"); leaf == nil || !leaf.Circular {
		t.Fatalf("expected circular marker on back-edge, got %#v", leaf)
	}
}

func TestBuildTreeMaxDepthPruning(t *testing.T) {
	root := buildTreeFromEdgeGraph(edgeTreeOptions{Graph: diamondGraph(), MaxDepth: 1})
	if root == nil || len(root.Children) != 2 {
		t.Fatalf("root children = %#v", root)
	}
	if a := findChild(root, "a"); a == nil || len(a.Children) != 0 {
		t.Fatalf("depth-1 child should have no children at MaxDepth 1, got %#v", a)
	}
}
