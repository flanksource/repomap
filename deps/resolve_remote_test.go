package deps

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// memCache is an in-memory RemoteCache for recursion tests: no network, no disk
// reads beyond the git fixture dirs the test writes.
type memCache struct {
	blobs   map[string][]byte
	images  map[string]ImageConfig
	gitDirs map[string]string // keyed by url, any ref
}

func (m *memCache) Fetch(_ context.Context, url string, _ time.Duration) ([]byte, error) {
	if b, ok := m.blobs[url]; ok {
		return b, nil
	}
	return nil, notFoundError{url}
}

func (m *memCache) GitRepo(_ context.Context, url, _ string) (string, error) {
	if d, ok := m.gitDirs[url]; ok {
		return d, nil
	}
	return "", notFoundError{url}
}

func (m *memCache) ImageConfig(_ context.Context, ref string) (ImageConfig, error) {
	if c, ok := m.images[ref]; ok {
		return c, nil
	}
	return ImageConfig{}, notFoundError{ref}
}

func buildChartTgz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// chartRootWithSubchart builds an offline chart root carrying a single subchart
// leaf, as the offline scan would produce it.
func chartRootWithSubchart(name, version, repo string) *Node {
	root := NewNode(ManagerHelm, "parent", "1.0.0")
	root.Source = "Chart.yaml"
	root.Depth = 0
	sub := NewNode(ManagerHelm, name, version)
	sub.Source = repo
	sub.Scope = "dependencies"
	sub.Direct = true
	sub.Depth = 1
	root.Children = []*Node{sub}
	return root
}

func TestResolveRemoteChartRecursion(t *testing.T) {
	const repo = "https://charts.example.com"
	tgz := buildChartTgz(t, map[string]string{
		"flanksource-ui/Chart.yaml": "apiVersion: v2\nname: flanksource-ui\nversion: 1.4.212\n" +
			"dependencies:\n  - name: common\n    version: 2.0.0\n    repository: https://other.example.com\n",
		"flanksource-ui/values.yaml": "image:\n  repository: flanksource/ui\n  tag: \"3.2.1\"\n",
	})
	cache := &memCache{
		blobs: map[string][]byte{
			repo + "/index.yaml":                 []byte("entries:\n  flanksource-ui:\n    - version: 1.4.212\n      urls:\n        - " + repo + "/flanksource-ui-1.4.212.tgz\n"),
			repo + "/flanksource-ui-1.4.212.tgz": tgz,
		},
	}

	root := chartRootWithSubchart("flanksource-ui", "1.4.212", repo)
	opts := Options{MaxDepth: 0, remote: remoteDepsFromCache(cache)}
	if _, err := resolveRemote(context.Background(), []*Node{root}, opts); err != nil {
		t.Fatal(err)
	}

	ui := findChild(root, "flanksource-ui")
	if ui == nil {
		t.Fatalf("subchart node missing")
	}
	if common := findChild(ui, "common"); common == nil || common.Manager != ManagerHelm || common.Depth != 2 {
		t.Fatalf("nested subchart 'common' not resolved: %#v (children: %v)", common, childNames(ui))
	}
	img := findChild(ui, "flanksource/ui")
	if img == nil || img.Manager != ManagerImage || img.Version != "3.2.1" || img.Depth != 2 {
		t.Fatalf("subchart values image not harvested: %#v (children: %v)", img, childNames(ui))
	}
}

func TestResolveRemoteImageBaseViaLabel(t *testing.T) {
	cache := &memCache{images: map[string]ImageConfig{
		"app:1.0":   {Labels: map[string]string{labelBaseName: "debian:12"}},
		"debian:12": {Labels: map[string]string{}},
	}}
	root := imageRootWithImage("app:1.0")
	opts := Options{MaxDepth: 0, remote: remoteDepsFromCache(cache)}
	if _, err := resolveRemote(context.Background(), []*Node{root}, opts); err != nil {
		t.Fatal(err)
	}
	app := findChild(root, "app")
	base := findChild(app, "debian")
	if base == nil || base.Version != "12" || base.Depth != 2 {
		t.Fatalf("base image from OCI label not attached: %#v (children: %v)", base, childNames(app))
	}
}

func TestResolveRemoteImageBaseViaDockerfile(t *testing.T) {
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "Dockerfile"),
		[]byte("FROM golang:1.22 AS build\nFROM gcr.io/distroless/static:nonroot\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := &memCache{
		images:  map[string]ImageConfig{"ghcr.io/acme/app:1.0": {Labels: map[string]string{labelSource: "https://github.com/acme/app"}}},
		gitDirs: map[string]string{"https://github.com/acme/app": repoDir},
	}
	root := imageRootWithImage("ghcr.io/acme/app:1.0")
	opts := Options{MaxDepth: 2, remote: remoteDepsFromCache(cache)}
	if _, err := resolveRemote(context.Background(), []*Node{root}, opts); err != nil {
		t.Fatal(err)
	}
	app := findChild(root, "ghcr.io/acme/app")
	if app == nil {
		t.Fatalf("app node missing (children: %v)", childNames(root))
	}
	// Both FROM lines reference external images (golang is the builder's base);
	// only a bare `FROM <stage>` reference would be excluded as internal.
	if base := findChild(app, "gcr.io/distroless/static"); base == nil || base.Version != "nonroot" {
		t.Fatalf("runtime FROM base not attached: %#v (children: %v)", base, childNames(app))
	}
	if builder := findChild(app, "golang"); builder == nil || builder.Version != "1.22" {
		t.Fatalf("builder-stage FROM base not attached: %#v (children: %v)", builder, childNames(app))
	}
}

func TestResolveRemoteDepthLimit(t *testing.T) {
	cache := &memCache{images: map[string]ImageConfig{
		"a:1": {Labels: map[string]string{labelBaseName: "b:1"}},
		"b:1": {Labels: map[string]string{labelBaseName: "c:1"}},
		"c:1": {Labels: map[string]string{labelBaseName: "d:1"}},
	}}
	root := imageRootWithImage("a:1") // a is Depth 1
	opts := Options{MaxDepth: 2, remote: remoteDepsFromCache(cache)}
	if _, err := resolveRemote(context.Background(), []*Node{root}, opts); err != nil {
		t.Fatal(err)
	}
	a := findChild(root, "a")
	b := findChild(a, "b")
	if b == nil || b.Depth != 2 {
		t.Fatalf("expected b at depth 2, got %#v", b)
	}
	if c := findChild(b, "c"); c != nil {
		t.Fatalf("depth 2 limit should stop fetching past b, but found c: %#v", c)
	}
}

func imageRootWithImage(ref string) *Node {
	root := NewNode(ManagerImage, "container images", "")
	root.Source = "kubernetes manifests"
	root.Depth = 0
	img := imageNodeFromRef(ref)
	img.Depth = 1
	img.Direct = true
	root.Children = []*Node{img}
	return root
}

func imageNodeFromRef(ref string) *Node {
	r := parseImageRef(ref)
	n := NewNode(ManagerImage, r.Name, r.Version)
	n.Source = ref
	return n
}
