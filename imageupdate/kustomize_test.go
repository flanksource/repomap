package imageupdate

import (
	"os"
	"path/filepath"
	"testing"
)

// loadKustomizeFixture reads testdata/kustomize/<sub> into a repo-relative file
// map (paths relative to the fixture root, POSIX-separated).
func loadKustomizeFixture(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join("testdata", "kustomize")
	files := map[string]string{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		files[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestResolveIncludePath(t *testing.T) {
	cases := []struct{ dir, rel, want string }{
		{"apps/mission-control", "helmrelease.yaml", "apps/mission-control/helmrelease.yaml"},
		{"apps/mission-control", "../../base/sources", "base/sources"},
		{"apps/mc", "./base", "apps/mc/base"},
		{"apps/mc", "base/", "apps/mc/base"},
		{"a/b/c", "../../common", "a/common"},
	}
	for _, tc := range cases {
		if got := resolveIncludePath(tc.dir, tc.rel); got != tc.want {
			t.Errorf("resolveIncludePath(%q,%q) = %q, want %q", tc.dir, tc.rel, got, tc.want)
		}
	}
}

func TestBuildKustomizeTree_Includes(t *testing.T) {
	kt := BuildKustomizeTree(loadKustomizeFixture(t))

	flux := kt.byDirOrFile(t, "flux/mission-control.yaml")
	if !flux.IsFlux || flux.Namespace != "flux-system" {
		t.Errorf("flux node: %+v", flux)
	}
	if len(flux.Includes) != 1 || flux.Includes[0] != "apps/mission-control" {
		t.Errorf("flux includes = %v, want [apps/mission-control]", flux.Includes)
	}

	app := kt.byDir["apps/mission-control"]
	if app == nil {
		t.Fatal("missing apps/mission-control kustomization node")
	}
	wantInc := map[string]bool{
		"apps/mission-control/helmrelease.yaml": true,
		"base/sources":                          true,
	}
	for _, inc := range app.Includes {
		if !wantInc[inc] {
			t.Errorf("unexpected include %q", inc)
		}
	}
}

func TestEffectiveNamespace_FluxTargetNamespace(t *testing.T) {
	kt := BuildKustomizeTree(loadKustomizeFixture(t))

	hr := "apps/mission-control/helmrelease.yaml"
	if ns := kt.EffectiveNamespace(hr); ns != "flux-system" {
		t.Errorf("HelmRelease effective ns = %q, want flux-system", ns)
	}
	repo := "base/sources/helmrepository.yaml"
	if ns := kt.EffectiveNamespace(repo); ns != "flux-system" {
		t.Errorf("HelmRepository (in base) effective ns = %q, want flux-system", ns)
	}
}

func TestEffectiveNamespace_KustomizeTopmostWins(t *testing.T) {
	files := map[string]string{
		"overlay/kustomization.yaml": "namespace: prod\nresources:\n  - ../base\n",
		"base/kustomization.yaml":    "namespace: base-ns\nresources:\n  - app.yaml\n",
		"base/app.yaml":              "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: x\n",
	}
	kt := BuildKustomizeTree(files)
	if ns := kt.EffectiveNamespace("base/app.yaml"); ns != "prod" {
		t.Errorf("topmost overlay namespace should win: got %q, want prod", ns)
	}
}

func TestEffectiveNamespace_NoImposer(t *testing.T) {
	files := map[string]string{
		"base/kustomization.yaml": "resources:\n  - app.yaml\n",
		"base/app.yaml":           "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: x\n",
	}
	kt := BuildKustomizeTree(files)
	if ns := kt.EffectiveNamespace("base/app.yaml"); ns != "" {
		t.Errorf("no imposer should yield empty ns, got %q", ns)
	}
}

// byDirOrFile finds a node by its File path (helper for Flux nodes not in byDir).
func (kt *KustomizeTree) byDirOrFile(t *testing.T, file string) *KustomizeNode {
	t.Helper()
	for _, n := range kt.nodes {
		if n.File == file {
			return n
		}
	}
	t.Fatalf("no node for %q", file)
	return nil
}
