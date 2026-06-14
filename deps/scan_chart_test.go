package deps

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

const chartYAMLFixture = `apiVersion: v2
name: web
version: 1.4.0
dependencies:
  - name: postgresql
    version: "12.x.x"
    repository: https://charts.bitnami.com/bitnami
  - name: redis
    version: 18.1.2
    repository: oci://registry-1.docker.io/bitnamicharts
`

const chartValuesFixture = `image:
  registry: docker.io
  repository: bitnami/nginx
  tag: "1.25.3"
sidecar:
  image: busybox:1.36
postgresql:
  image:
    repository: postgres
    tag: 15.5
ui:
  image:
    name: ghcr.io/acme/ui
    tag: "3.1.0"
`

const chartTemplateFixture = `apiVersion: apps/v1
kind: Deployment
spec:
  template:
    spec:
      containers:
        - name: app
          image: ghcr.io/acme/app:2.0.1
        - name: tools
          image: alpine
        - image: quay.io/prometheus/node-exporter:1.7.0
          name: dash-first
        - name: templated
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
`

func writeChartFixture(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "charts", "web", "Chart.yaml"), chartYAMLFixture)
	writeFile(t, filepath.Join(dir, "charts", "web", "values.yaml"), chartValuesFixture)
	writeFile(t, filepath.Join(dir, "charts", "web", "templates", "deployment.yaml"), chartTemplateFixture)
}

func TestScanChartSubchartDependencies(t *testing.T) {
	dir := t.TempDir()
	writeChartFixture(t, dir)

	got, err := Scan(context.Background(), dir, Options{
		Managers: []Manager{ManagerHelm},
		Now:      func() time.Time { return time.Unix(1, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Roots) != 1 {
		t.Fatalf("expected one chart root, got %d: %#v", len(got.Roots), got.Roots)
	}
	root := got.Roots[0]
	if root.Manager != ManagerHelm || root.Name != "web" || root.Version != "1.4.0" {
		t.Fatalf("chart root metadata wrong: %#v", root)
	}
	if root.Source != "Chart.yaml" {
		t.Fatalf("chart root should be sourced from Chart.yaml, got %q", root.Source)
	}

	pg := findChild(root, "postgresql")
	if pg == nil || pg.Version != "12.x.x" || pg.Manager != ManagerHelm {
		t.Fatalf("postgresql subchart not resolved with declared range: %#v", pg)
	}
	if pg.Source != "https://charts.bitnami.com/bitnami" {
		t.Fatalf("subchart should carry its repository as source: %#v", pg)
	}
	redis := findChild(root, "redis")
	if redis == nil || redis.Version != "18.1.2" {
		t.Fatalf("redis subchart not resolved: %#v", redis)
	}
}

func TestScanChartHelmOnlyExcludesImages(t *testing.T) {
	dir := t.TempDir()
	writeChartFixture(t, dir)

	got, err := Scan(context.Background(), dir, Options{
		Managers: []Manager{ManagerHelm},
		Now:      func() time.Time { return time.Unix(1, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, child := range got.Roots[0].Children {
		if child.Manager == ManagerImage {
			t.Fatalf("helm-only scan should not include image children: %#v", child)
		}
	}
}

func TestScanChartExtractsImagesFromValuesAndTemplates(t *testing.T) {
	dir := t.TempDir()
	writeChartFixture(t, dir)

	got, err := Scan(context.Background(), dir, Options{
		Managers: []Manager{ManagerImage},
		Now:      func() time.Time { return time.Unix(1, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Roots) != 1 {
		t.Fatalf("expected one chart root, got %d", len(got.Roots))
	}
	root := got.Roots[0]

	want := map[string]string{
		"docker.io/bitnami/nginx":          "1.25.3",
		"busybox":                          "1.36",
		"postgres":                         "15.5",
		"ghcr.io/acme/ui":                  "3.1.0",
		"ghcr.io/acme/app":                 "2.0.1",
		"alpine":                           "",      // bare image from a template, no tag
		"quay.io/prometheus/node-exporter": "1.7.0", // list-item `- image:` form
	}
	for name, version := range want {
		img := findChild(root, name)
		if img == nil {
			t.Fatalf("expected image %q in chart, got children %#v", name, childNames(root))
		}
		if img.Version != version {
			t.Fatalf("image %q version = %q, want %q", name, img.Version, version)
		}
		if img.Manager != ManagerImage {
			t.Fatalf("image %q should use the image manager: %#v", name, img)
		}
	}
	if templated := findChild(root, "{{ .Values.image.repository }}"); templated != nil {
		t.Fatalf("templated image refs must be skipped, got %#v", templated)
	}
}

func TestScanChartSkipsVendoredSubcharts(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Chart.yaml"), "apiVersion: v2\nname: parent\nversion: 1.0.0\ndependencies:\n  - name: child\n    version: 1.0.0\n")
	// A vendored copy of the dependency under charts/ must not become its own root.
	writeFile(t, filepath.Join(dir, "charts", "child", "Chart.yaml"), "apiVersion: v2\nname: child\nversion: 1.0.0\n")

	got, err := Scan(context.Background(), dir, Options{
		Managers: []Manager{ManagerHelm},
		Now:      func() time.Time { return time.Unix(1, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Roots) != 1 || got.Roots[0].Name != "parent" {
		t.Fatalf("vendored subchart should not be a separate root, got %#v", childRootNames(got.Roots))
	}
}

func childNames(node *Node) []string {
	var out []string
	for _, c := range node.Children {
		out = append(out, c.Name)
	}
	return out
}

func childRootNames(roots []*Node) []string {
	var out []string
	for _, r := range roots {
		out = append(out, r.Name)
	}
	return out
}
