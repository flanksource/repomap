package deps

import "testing"

// diamondRoot builds a go project where a shared dependency is reachable under
// three direct parents, the classic diamond that motivates collapsing.
//
//	app
//	├── a ── shared@v1
//	├── b ── shared@v1
//	└── c ── shared@v1
func diamondRoot() *Node {
	root := NewNode(ManagerGo, "github.com/acme/app", "")
	root.Source = "go.mod"
	shared := func() *Node {
		n := NewNode(ManagerGo, "github.com/acme/shared", "v1.0.0")
		n.Depth = 2
		return n
	}
	for _, name := range []string{"a", "b", "c"} {
		parent := NewNode(ManagerGo, "github.com/acme/"+name, "v1.0.0")
		parent.Direct = true
		parent.Depth = 1
		parent.Children = []*Node{shared()}
		root.Children = append(root.Children, parent)
	}
	return root
}

func TestCollapseKeepsSharedDependencyOnce(t *testing.T) {
	root := diamondRoot()
	collapseDuplicates([]*Node{root})

	var kept int
	for _, parent := range root.Children {
		if findChild(parent, "github.com/acme/shared") != nil {
			kept++
		}
	}
	if kept != 1 {
		t.Fatalf("expected shared dependency to survive under exactly one parent, got %d", kept)
	}
}

func TestCollapseMarksOtherParentsAndHiddenCounts(t *testing.T) {
	root := diamondRoot()
	collapseDuplicates([]*Node{root})

	a := findChild(root, "github.com/acme/a")
	shared := findChild(a, "github.com/acme/shared")
	if shared == nil {
		t.Fatalf("shared should be retained under the first sorted parent 'a'")
	}
	if shared.OtherParents != 2 {
		t.Fatalf("resolved shared node should record 2 other parents, got %d", shared.OtherParents)
	}

	for _, name := range []string{"github.com/acme/b", "github.com/acme/c"} {
		parent := findChild(root, name)
		if parent.HiddenDuplicates != 1 {
			t.Fatalf("parent %s should hide 1 duplicate, got %d", name, parent.HiddenDuplicates)
		}
		if findChild(parent, "github.com/acme/shared") != nil {
			t.Fatalf("parent %s should no longer carry the shared dependency", name)
		}
	}
}

func TestCollapseLeavesImageRootsUntouched(t *testing.T) {
	root := NewNode(ManagerImage, "container images", "")
	dup := func() *Node {
		n := NewNode(ManagerImage, "nginx", "1.25")
		n.Depth = 1
		return n
	}
	root.Children = []*Node{dup(), dup()}
	collapseDuplicates([]*Node{root})

	if len(root.Children) != 2 {
		t.Fatalf("image roots must keep every occurrence, got %d children", len(root.Children))
	}
	if root.HiddenDuplicates != 0 {
		t.Fatalf("image root should not record hidden duplicates, got %d", root.HiddenDuplicates)
	}
}

func TestCollapseIsolatesPerRoot(t *testing.T) {
	first := diamondRoot()
	second := diamondRoot()
	collapseDuplicates([]*Node{first, second})

	for _, root := range []*Node{first, second} {
		a := findChild(root, "github.com/acme/a")
		if shared := findChild(a, "github.com/acme/shared"); shared == nil || shared.OtherParents != 2 {
			t.Fatalf("each root should dedup independently with OtherParents=2")
		}
	}
}

func TestCollapseKeepsConflictingVersions(t *testing.T) {
	root := NewNode(ManagerGo, "github.com/acme/app", "")
	v1 := NewNode(ManagerGo, "github.com/acme/lib", "v1.0.0")
	v1.Direct = true
	v1.Depth = 1
	v2 := NewNode(ManagerGo, "github.com/acme/lib", "v2.0.0")
	v2.Direct = true
	v2.Depth = 1
	root.Children = []*Node{v1, v2}
	collapseDuplicates([]*Node{root})

	if len(root.Children) != 2 {
		t.Fatalf("conflicting versions must each survive, got %d", len(root.Children))
	}
}
