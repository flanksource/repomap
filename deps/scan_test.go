package deps

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/flanksource/clicky/api"
)

func TestScanGoManifestFallback(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), `module github.com/acme/app

go 1.22

require (
	github.com/acme/lib v1.2.3
	github.com/acme/indirect v0.1.0 // indirect
)

replace github.com/acme/lib => ../lib
`)

	got, err := Scan(context.Background(), dir, Options{
		Mode:     ModeManifest,
		MaxDepth: 1,
		Now:      func() time.Time { return time.Unix(1, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Metadata.ProjectsScanned != 1 {
		t.Fatalf("projects scanned = %d, want 1", got.Metadata.ProjectsScanned)
	}
	if len(got.Roots) != 1 || got.Roots[0].Name != "github.com/acme/app" {
		t.Fatalf("unexpected roots: %#v", got.Roots)
	}
	if len(got.Roots[0].Children) != 1 {
		t.Fatalf("children = %d, want 1 (indirect excluded by default)", len(got.Roots[0].Children))
	}
	lib := findChild(got.Roots[0], "github.com/acme/lib")
	if lib == nil || lib.Name != "github.com/acme/lib" || !lib.Local || lib.Source != "../lib" {
		t.Fatalf("replace/local metadata not captured: %#v", lib)
	}
	if len(got.Nodes) != 0 || len(got.Edges) != 0 {
		t.Fatalf("default export should omit flat nodes/edges, got nodes=%d edges=%d", len(got.Nodes), len(got.Edges))
	}
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"roots"`) {
		t.Fatalf("default json export missing roots: %s", data)
	}
}

func TestScanFlatExport(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), `module github.com/acme/app

go 1.22

require github.com/acme/lib v1.2.3
`)

	got, err := Scan(context.Background(), dir, Options{
		Mode:     ModeManifest,
		MaxDepth: 1,
		Flat:     true,
		Now:      func() time.Time { return time.Unix(1, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Roots) != 0 {
		t.Fatalf("flat export should omit roots, got %d", len(got.Roots))
	}
	if len(got.Nodes) != 2 || len(got.Edges) != 1 {
		t.Fatalf("flat export nodes=%d edges=%d, want nodes=2 edges=1", len(got.Nodes), len(got.Edges))
	}
	if !got.Metadata.Flat {
		t.Fatal("metadata.flat should be true")
	}
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"nodes"`) {
		t.Fatalf("flat json export missing nodes: %s", data)
	}
	if strings.Contains(string(data), `"roots"`) {
		t.Fatalf("flat json export should omit roots: %s", data)
	}
}

func TestFlatExportPretty(t *testing.T) {
	export := &Export{
		Nodes: []FlatNode{
			{ID: "go:github.com/acme/app", Name: "github.com/acme/app", Manager: ManagerGo, Depth: 0},
			{ID: "go:github.com/acme/lib@v1.2.3", Name: "github.com/acme/lib", Version: "v1.2.3", Manager: ManagerGo, Scope: "require", Direct: true, Depth: 1},
		},
	}
	got := export.Pretty().String()
	if !strings.Contains(got, "github.com/acme/lib@v1.2.3") {
		t.Fatalf("flat pretty missing node label: %q", got)
	}
	if !strings.Contains(got, "depth=1") {
		t.Fatalf("flat pretty missing depth marker: %q", got)
	}
	if !strings.Contains(got, "direct") {
		t.Fatalf("flat pretty missing direct tag: %q", got)
	}
}

func TestScanDefaultsToOfflineManifestResolution(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), `module github.com/acme/app

go 1.22

require github.com/acme/lib v1.2.3
`)

	got, err := Scan(context.Background(), dir, Options{
		MaxDepth: 1,
		Now:      func() time.Time { return time.Unix(1, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Metadata.Mode != ModeManifest {
		t.Fatalf("mode = %q, want %q", got.Metadata.Mode, ModeManifest)
	}
	if len(got.Roots) != 1 || got.Roots[0].Source != "go.mod" {
		t.Fatalf("expected go.mod manifest root, got %#v", got.Roots)
	}
	if child := findChild(got.Roots[0], "github.com/acme/lib"); child == nil || !child.Direct {
		t.Fatalf("expected direct manifest dependency, got %#v", got.Roots[0].Children)
	}
}

func TestScanImageAndHelmManifestTargets(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	runGit(t, dir, "init")
	writeFile(t, filepath.Join(dir, "apps", "workloads.yaml"), deploymentUpdateFixture)
	writeFile(t, filepath.Join(dir, "apps", "helmrelease.yaml"), helmReleaseUpdateFixture)
	runGit(t, dir, "add", ".")

	got, err := Scan(context.Background(), ".", Options{
		Managers: []Manager{ManagerImage, ManagerHelm},
		Now:      func() time.Time { return time.Unix(1, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Metadata.ProjectsScanned != 2 {
		t.Fatalf("projects scanned = %d, want 2 image/helm roots", got.Metadata.ProjectsScanned)
	}
	if len(got.Roots) != 2 {
		t.Fatalf("roots = %d, want image and helm roots: %#v", len(got.Roots), got.Roots)
	}
	imageRoot := findRoot(got.Roots, ManagerImage)
	if imageRoot == nil || imageRoot.Name != "container images" || len(imageRoot.Children) != 2 {
		t.Fatalf("image root not resolved: %#v", imageRoot)
	}
	nginx := findChild(imageRoot, "nginx")
	if nginx == nil || nginx.Version != "1.25.3" || nginx.Path != "apps/workloads.yaml" || nginx.Source != "nginx:1.25.3" {
		t.Fatalf("nginx image node mismatch: %#v", nginx)
	}
	helmRoot := findRoot(got.Roots, ManagerHelm)
	if helmRoot == nil || helmRoot.Name != "helm charts" || len(helmRoot.Children) != 1 {
		t.Fatalf("helm root not resolved: %#v", helmRoot)
	}
	podinfo := findChild(helmRoot, "podinfo")
	if podinfo == nil || podinfo.Version != "6.5.0" || podinfo.Source != "https://stefanprodan.github.io/podinfo" {
		t.Fatalf("helm node mismatch: %#v", podinfo)
	}
	if got.Statistics.ByManager[ManagerImage] != 3 || got.Statistics.ByManager[ManagerHelm] != 2 {
		t.Fatalf("manager stats = %#v, want image=3 helm=2", got.Statistics.ByManager)
	}
}

func TestNodeImplementsClickyTreeNode(t *testing.T) {
	var _ api.TreeNode = (*Node)(nil)

	root := NewNode(ManagerGo, "root", "")
	child := NewNode(ManagerGo, "github.com/acme/lib", "v1.0.0")
	root.Children = []*Node{child, nil}

	children := root.GetChildren()
	if len(children) != 1 {
		t.Fatalf("children = %d, want 1", len(children))
	}
	if got := children[0].Pretty().String(); !strings.Contains(got, "github.com/acme/lib@v1.0.0") {
		t.Fatalf("unexpected child label: %q", got)
	}
	if ansi := children[0].Pretty().ANSI(); !strings.Contains(ansi, "\x1b[") {
		t.Fatalf("expected styled dependency label to emit ANSI color, got %q", ansi)
	}
}

func TestTreeChildrenSortByTypeThenName(t *testing.T) {
	root := NewNode(ManagerGo, "root", "")
	replacement := NewNode(ManagerGo, "z-replacement", "v1.0.0")
	replacement.Local = true
	replacement.Direct = true
	directB := NewNode(ManagerGo, "b-direct", "v1.0.0")
	directB.Direct = true
	directA := NewNode(ManagerGo, "a-direct", "v1.0.0")
	directA.Direct = true
	indirectB := NewNode(ManagerGo, "b-indirect", "v1.0.0")
	indirectA := NewNode(ManagerGo, "a-indirect", "v1.0.0")

	root.Children = []*Node{indirectB, directB, replacement, indirectA, directA}
	got := treeChildNames(root.GetChildren())
	want := []string{"z-replacement", "a-direct", "b-direct", "a-indirect", "b-indirect"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("tree child order = %#v, want %#v", got, want)
	}

	sortChildren(root)
	got = nodeChildNames(root.Children)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("stored child order = %#v, want %#v", got, want)
	}
}

func TestFilterAndDepthPreserveAncestors(t *testing.T) {
	root := NewNode(ManagerGo, "root", "")
	child := NewNode(ManagerGo, "github.com/acme/lib", "v1.0.0")
	child.Depth = 1
	grandchild := NewNode(ManagerGo, "github.com/acme/target", "v2.0.0")
	grandchild.Depth = 2
	other := NewNode(ManagerGo, "github.com/acme/other", "v1.0.0")
	other.Depth = 1
	child.Children = []*Node{grandchild}
	root.Children = []*Node{child, other}

	filtered := filterAndPrune(root, []string{"*target*"}, 0)
	if filtered == nil || len(filtered.Children) != 1 {
		t.Fatalf("expected only matching branch, got %#v", filtered)
	}
	if filtered.Children[0].Name != "github.com/acme/lib" || len(filtered.Children[0].Children) != 1 {
		t.Fatalf("expected ancestor plus target child, got %#v", filtered.Children[0])
	}
	directOnly := filterAndPrune(root, nil, 1)
	if directOnly == nil || len(directOnly.Children) != 2 || len(directOnly.Children[0].Children) != 0 {
		t.Fatalf("depth=1 should keep direct deps only, got %#v", directOnly)
	}
}

func TestParsePackageLockV3(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "package-lock.json")
	writeFile(t, path, `{
  "name": "app",
  "version": "1.0.0",
  "lockfileVersion": 3,
  "packages": {
    "": {
      "name": "app",
      "version": "1.0.0",
      "dependencies": {
        "left-pad": "^1.3.0",
        "@scope/pkg": "2.0.0"
      }
    },
    "node_modules/left-pad": {
      "version": "1.3.0",
      "dependencies": {
        "repeat-string": "1.6.1"
      }
    },
    "node_modules/repeat-string": {
      "version": "1.6.1"
    },
    "node_modules/@scope/pkg": {
      "version": "2.0.0",
      "dev": true
    }
  }
}`)

	root, err := parsePackageLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if root.Name != "app" || len(root.Children) != 2 {
		t.Fatalf("unexpected root: %#v", root)
	}
	leftPad := findChild(root, "left-pad")
	if leftPad == nil || leftPad.Version != "1.3.0" || len(leftPad.Children) != 1 {
		t.Fatalf("left-pad tree not resolved: %#v", leftPad)
	}
	scoped := findChild(root, "@scope/pkg")
	if scoped == nil || !scoped.Dev {
		t.Fatalf("scoped dev package metadata missing: %#v", scoped)
	}
}

func TestParsePNPMLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pnpm-lock.yaml")
	writeFile(t, path, `lockfileVersion: '9.0'
importers:
  .:
    dependencies:
      left-pad:
        specifier: ^1.3.0
        version: 1.3.0
    devDependencies:
      local-tool:
        specifier: file:../tool
        version: file:../tool
packages:
  left-pad@1.3.0:
    dependencies:
      repeat-string: 1.6.1
  repeat-string@1.6.1: {}
  "local-tool@file:../tool": {}
`)

	root, err := parsePNPMLock(path)
	if err != nil {
		t.Fatal(err)
	}
	importer := findChild(root, ".")
	if importer == nil {
		t.Fatalf("importer not found: %#v", root)
	}
	leftPad := findChild(importer, "left-pad")
	if leftPad == nil || len(leftPad.Children) != 1 || leftPad.Children[0].Name != "repeat-string" {
		t.Fatalf("pnpm dependency tree not resolved: %#v", leftPad)
	}
	local := findChild(importer, "local-tool")
	if local == nil || !local.Local || !local.Dev {
		t.Fatalf("pnpm local dev dependency metadata missing: %#v", local)
	}
}

func TestDiscoverRespectsGitignore(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	writeFile(t, filepath.Join(dir, ".gitignore"), `ignored-dir/
ignored-package.json
`)
	writeFile(t, filepath.Join(dir, "go.mod"), `module github.com/acme/app

go 1.22
`)
	writeFile(t, filepath.Join(dir, "visible", "package.json"), `{"name":"visible"}`)
	writeFile(t, filepath.Join(dir, "ignored-dir", "package.json"), `{"name":"ignored-dir"}`)
	writeFile(t, filepath.Join(dir, "ignored-package.json"), `{"name":"ignored-file"}`)

	projects, _, err := Discover(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := projectFiles(projects)
	want := []string{
		filepath.Join(dir, "go.mod"),
		filepath.Join(dir, "visible", "package.json"),
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("projects:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func findChild(root *Node, name string) *Node {
	for _, child := range root.Children {
		if child.Name == name {
			return child
		}
	}
	return nil
}

func findRoot(roots []*Node, manager Manager) *Node {
	for _, root := range roots {
		if root.Manager == manager {
			return root
		}
	}
	return nil
}

func projectFiles(projects []Project) []string {
	out := make([]string, 0, len(projects))
	for _, project := range projects {
		out = append(out, project.File)
	}
	return out
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func treeChildNames(children []api.TreeNode) []string {
	names := make([]string, 0, len(children))
	for _, child := range children {
		if node, ok := child.(*Node); ok {
			names = append(names, node.Name)
		}
	}
	return names
}

func nodeChildNames(children []*Node) []string {
	names := make([]string, 0, len(children))
	for _, child := range children {
		if child != nil {
			names = append(names, child.Name)
		}
	}
	return names
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
