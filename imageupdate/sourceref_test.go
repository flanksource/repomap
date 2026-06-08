package imageupdate

import (
	"strings"
	"testing"
)

func TestSourceIndex_ResolveHTTP(t *testing.T) {
	content := readManifest(t, "helmrelease.yaml")
	idx := NewSourceIndex(nil)
	if err := idx.IndexHelmRepositories("helmrelease.yaml", content); err != nil {
		t.Fatal(err)
	}
	targets, err := ExtractTargets("helmrelease.yaml", content)
	if err != nil {
		t.Fatal(err)
	}
	tg := targets[0]
	if err := idx.Resolve(&tg); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if tg.RepoURL != "https://stefanprodan.github.io/podinfo" {
		t.Errorf("repo url = %q", tg.RepoURL)
	}
	if tg.IsOCI {
		t.Error("HTTP repo flagged as OCI")
	}
}

func TestSourceIndex_ResolveOCI(t *testing.T) {
	content := readManifest(t, "helmrelease-oci.yaml")
	idx := NewSourceIndex(nil)
	if err := idx.IndexHelmRepositories("helmrelease-oci.yaml", content); err != nil {
		t.Fatal(err)
	}
	targets, _ := ExtractTargets("helmrelease-oci.yaml", content)
	tg := targets[0]
	if tg.SourceRefNamespace != "apps" {
		t.Fatalf("sourceRef namespace = %q, want apps", tg.SourceRefNamespace)
	}
	if err := idx.Resolve(&tg); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !tg.IsOCI || tg.RepoURL != "oci://ghcr.io/acme/charts" {
		t.Errorf("oci resolve mismatch: oci=%v url=%q", tg.IsOCI, tg.RepoURL)
	}
}

func TestSourceIndex_MissingSourceFailsLoud(t *testing.T) {
	idx := NewSourceIndex(nil)
	tg := UpdateTarget{
		Kind:               TargetChart,
		SourceRefName:      "ghost",
		SourceRefNamespace: "flux-system",
	}
	tg.Ref.Namespace = "default"
	tg.Ref.Name = "podinfo"
	err := idx.Resolve(&tg)
	if err == nil {
		t.Fatal("expected error for missing HelmRepository, got nil")
	}
	if !strings.Contains(err.Error(), "flux-system/ghost") {
		t.Errorf("error should name the missing source: %v", err)
	}
}

// TestSourceIndex_TreeDerivedNamespace is the mission-control-app regression:
// neither the HelmRelease nor the HelmRepository pins a namespace; both get
// flux-system from the Flux Kustomization targetNamespace, and resolution
// succeeds via the kustomize tree.
func TestSourceIndex_TreeDerivedNamespace(t *testing.T) {
	files := loadKustomizeFixture(t)
	kt := BuildKustomizeTree(files)
	idx := NewSourceIndex(kt)
	for f, content := range files {
		if err := idx.IndexHelmRepositories(f, content); err != nil {
			t.Fatal(err)
		}
	}

	hrFile := "apps/mission-control/helmrelease.yaml"
	targets, err := ExtractTargets(hrFile, files[hrFile])
	if err != nil {
		t.Fatal(err)
	}
	tg := targets[0]
	if tg.SourceRefNamespace != "" {
		t.Fatalf("expected empty sourceRef namespace (derived from tree), got %q", tg.SourceRefNamespace)
	}
	if err := idx.Resolve(&tg); err != nil {
		t.Fatalf("resolve via tree failed: %v", err)
	}
	if !tg.IsOCI || tg.RepoURL != "oci://ghcr.io/flanksource/charts" {
		t.Errorf("resolved url=%q oci=%v", tg.RepoURL, tg.IsOCI)
	}
}

// TestSourceIndex_ControllerNamespaceFallback covers a HelmRepository living in
// the controller namespace (flux-system) while the HelmRelease resolves to an
// app namespace; matching falls back to name across namespaces.
func TestSourceIndex_ControllerNamespaceFallback(t *testing.T) {
	idx := NewSourceIndex(nil)
	// Two sources with the SAME name in different namespaces.
	repo := "apiVersion: source.toolkit.fluxcd.io/v1\nkind: HelmRepository\n" +
		"metadata:\n  name: shared\n  namespace: flux-system\nspec:\n  url: oci://ghcr.io/acme/shared\n"
	if err := idx.IndexHelmRepositories("sources.yaml", repo); err != nil {
		t.Fatal(err)
	}
	tg := UpdateTarget{Kind: TargetChart, SourceRefName: "shared", SourceRefNamespace: "my-app"}
	tg.Ref.Namespace = "my-app"
	tg.Ref.Name = "app"
	if err := idx.Resolve(&tg); err != nil {
		t.Fatalf("controller-namespace fallback failed: %v", err)
	}
	if tg.RepoURL != "oci://ghcr.io/acme/shared" {
		t.Errorf("url = %q", tg.RepoURL)
	}
}

// TestSourceIndex_CommentOnlyDocDoesNotDropTrailing guards a goccy multi-doc
// parsing bug: a comment-only document (with a commented-out `# ---`) made the
// AST parser drop every document after it, hiding trailing HelmRepositories.
// Indexing must use the line-based splitter so the HelmRepository is found.
func TestSourceIndex_CommentOnlyDocDoesNotDropTrailing(t *testing.T) {
	content := readManifest(t, "flux-multidoc.yaml")
	idx := NewSourceIndex(nil)
	if err := idx.IndexHelmRepositories("flux.yaml", content); err != nil {
		t.Fatal(err)
	}
	tg := UpdateTarget{Kind: TargetChart, SourceRefName: "flanksource-ecr", SourceRefNamespace: "flux-system"}
	tg.Ref.Namespace = "flux-system"
	tg.Ref.Name = "app"
	if err := idx.Resolve(&tg); err != nil {
		t.Fatalf("HelmRepository after a comment-only doc must still index: %v", err)
	}
	if tg.RepoURL != "oci://public.ecr.aws/flanksource" {
		t.Errorf("url = %q", tg.RepoURL)
	}
}

func TestSourceIndex_AmbiguousNameFailsLoud(t *testing.T) {
	idx := NewSourceIndex(nil)
	a := "apiVersion: source.toolkit.fluxcd.io/v1\nkind: HelmRepository\n" +
		"metadata:\n  name: dup\n  namespace: team-a\nspec:\n  url: https://a.example.com\n"
	b := "apiVersion: source.toolkit.fluxcd.io/v1\nkind: HelmRepository\n" +
		"metadata:\n  name: dup\n  namespace: team-b\nspec:\n  url: https://b.example.com\n"
	_ = idx.IndexHelmRepositories("a.yaml", a)
	_ = idx.IndexHelmRepositories("b.yaml", b)

	tg := UpdateTarget{Kind: TargetChart, SourceRefName: "dup", SourceRefNamespace: "team-c"}
	tg.Ref.Namespace = "team-c"
	tg.Ref.Name = "app"
	err := idx.Resolve(&tg)
	if err == nil {
		t.Fatal("expected ambiguity error")
	}
	if !strings.Contains(err.Error(), "team-a") || !strings.Contains(err.Error(), "team-b") {
		t.Errorf("error should name both namespaces: %v", err)
	}
}
