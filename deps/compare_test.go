package deps

import (
	"path/filepath"
	"testing"
)

func goDep(name, version, scope string) *Node {
	n := NewNode(ManagerGo, name, version)
	n.Scope = scope
	n.Depth = 1
	n.Direct = true
	return n
}

func goProject(metaPath, modRel string, children ...*Node) *Node {
	root := NewNode(ManagerGo, "github.com/acme/"+modRel, "")
	root.Path = filepath.Join(metaPath, modRel, "go.mod")
	root.Children = children
	return root
}

func exportWith(metaPath string, roots ...*Node) *Export {
	return &Export{Metadata: Metadata{Path: metaPath}, Roots: roots}
}

func totalChanges(c *Comparison) int {
	return len(c.Added) + len(c.Removed) + len(c.Updated)
}

func TestCompareIdenticalIsEmpty(t *testing.T) {
	base := exportWith("/repo", goProject("/repo", "svc", goDep("a", "1.0", "require")))
	head := exportWith("/repo", goProject("/repo", "svc", goDep("a", "1.0", "require")))
	got := Compare(base, head)
	if totalChanges(got) != 0 {
		t.Fatalf("identical graphs should produce no changes, got %+v", got)
	}
}

func TestCompareAddRemoveUpdate(t *testing.T) {
	base := exportWith("/repo", goProject("/repo", "svc",
		goDep("keep", "1.0", "require"),
		goDep("bump", "1.0", "require"),
		goDep("gone", "1.0", "require"),
	))
	head := exportWith("/repo", goProject("/repo", "svc",
		goDep("keep", "1.0", "require"),
		goDep("bump", "2.0", "require"),
		goDep("fresh", "0.1", "require"),
	))
	got := Compare(base, head)
	if len(got.Added) != 1 || got.Added[0].Name != "fresh" || got.Added[0].NewVersion != "0.1" {
		t.Fatalf("added = %+v", got.Added)
	}
	if len(got.Removed) != 1 || got.Removed[0].Name != "gone" {
		t.Fatalf("removed = %+v", got.Removed)
	}
	if len(got.Updated) != 1 || got.Updated[0].OldVersion != "1.0" || got.Updated[0].NewVersion != "2.0" {
		t.Fatalf("updated = %+v", got.Updated)
	}
	if got.Updated[0].Project != "svc/go.mod" {
		t.Fatalf("project key = %q, want svc/go.mod", got.Updated[0].Project)
	}
}

func TestCompareScopeOnlyChangeIsUpdate(t *testing.T) {
	base := exportWith("/repo", goProject("/repo", "svc", goDep("a", "1.0", "require")))
	head := exportWith("/repo", goProject("/repo", "svc", goDep("a", "1.0", "indirect")))
	got := Compare(base, head)
	if len(got.Updated) != 1 || got.Updated[0].OldScope != "require" || got.Updated[0].NewScope != "indirect" {
		t.Fatalf("scope-only update not detected: %+v", got.Updated)
	}
}

func TestComparePerProjectKeying(t *testing.T) {
	base := exportWith("/repo",
		goProject("/repo", "a", goDep("lib", "1.0", "require")),
		goProject("/repo", "b", goDep("lib", "1.0", "require")),
	)
	head := exportWith("/repo",
		goProject("/repo", "a", goDep("lib", "2.0", "require")),
		goProject("/repo", "b", goDep("lib", "1.0", "require")),
	)
	got := Compare(base, head)
	if len(got.Updated) != 1 || got.Updated[0].Project != "a/go.mod" {
		t.Fatalf("expected only project a to change, got %+v", got.Updated)
	}
}

func TestCompareDuplicateVersionSets(t *testing.T) {
	dupA := goDep("lib", "1.0", "require")
	dupB := goDep("lib", "2.0", "require")
	base := exportWith("/repo", goProject("/repo", "svc", dupA, dupB))
	head := exportWith("/repo", goProject("/repo", "svc", goDep("lib", "2.0", "require")))
	got := Compare(base, head)
	if len(got.Updated) != 1 || got.Updated[0].OldVersion != "1.0, 2.0" || got.Updated[0].NewVersion != "2.0" {
		t.Fatalf("duplicate version set not joined: %+v", got.Updated)
	}
}

func TestComparePathNormalization(t *testing.T) {
	base := exportWith("/repo/base", goProject("/repo/base", "svc", goDep("a", "1.0", "require")))
	head := exportWith("/repo/head", goProject("/repo/head", "svc", goDep("a", "2.0", "require")))
	got := Compare(base, head)
	if len(got.Updated) != 1 || got.Updated[0].Project != "svc/go.mod" {
		t.Fatalf("absolute worktree paths not normalized to relative project key: %+v", got.Updated)
	}
}

func TestCompareWholeProjectAddRemove(t *testing.T) {
	base := exportWith("/repo", goProject("/repo", "old", goDep("a", "1.0", "require")))
	head := exportWith("/repo", goProject("/repo", "new", goDep("b", "1.0", "require")))
	got := Compare(base, head)
	if len(got.Removed) != 1 || got.Removed[0].Project != "old/go.mod" {
		t.Fatalf("removed project deps = %+v", got.Removed)
	}
	if len(got.Added) != 1 || got.Added[0].Project != "new/go.mod" {
		t.Fatalf("added project deps = %+v", got.Added)
	}
}

func TestCompareDeterministicOrdering(t *testing.T) {
	base := exportWith("/repo", goProject("/repo", "svc"))
	head := exportWith("/repo", goProject("/repo", "svc",
		goDep("zebra", "1.0", "require"),
		goDep("alpha", "1.0", "require"),
		goDep("mango", "1.0", "require"),
	))
	got := Compare(base, head)
	want := []string{"alpha", "mango", "zebra"}
	if len(got.Added) != 3 {
		t.Fatalf("added = %d, want 3", len(got.Added))
	}
	for i, name := range want {
		if got.Added[i].Name != name {
			t.Fatalf("added[%d] = %q, want %q (sorted)", i, got.Added[i].Name, name)
		}
	}
}
