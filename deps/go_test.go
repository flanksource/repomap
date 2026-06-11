package deps

import (
	"path/filepath"
	"testing"
)

const goModWithIndirect = `module github.com/acme/app

go 1.22

require (
	github.com/acme/lib v1.2.3
	github.com/acme/indirect v0.1.0 // indirect
)
`

func TestGoManifestExcludesIndirectByDefault(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), goModWithIndirect)

	root, _, err := resolveGoManifest(Project{Manager: ManagerGo, Dir: dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(root.Children) != 1 {
		t.Fatalf("children = %d, want 1 (indirect excluded)", len(root.Children))
	}
	if root.Children[0].Name != "github.com/acme/lib" {
		t.Fatalf("unexpected direct child: %#v", root.Children[0])
	}
}

func TestGoManifestIncludeIndirect(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), goModWithIndirect)

	root, _, err := resolveGoManifest(Project{Manager: ManagerGo, Dir: dir}, Options{IncludeIndirect: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(root.Children) != 2 {
		t.Fatalf("children = %d, want 2 (indirect included)", len(root.Children))
	}
	indirect := findChild(root, "github.com/acme/indirect")
	if indirect == nil || indirect.Direct || indirect.Scope != "indirect" {
		t.Fatalf("indirect metadata not captured: %#v", indirect)
	}
}
