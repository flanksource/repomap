package imageupdate

import (
	"strings"
	"testing"
)

// resolveSingleTarget indexes sources and extracts the one chart target from a
// fixture, resolving it through the index. It is the chartRef equivalent of the
// inline helpers in sourceref_test.go.
func resolveSingleTarget(t *testing.T, name string) UpdateTarget {
	t.Helper()
	content := readManifest(t, name)
	idx := NewSourceIndex(nil)
	if err := idx.IndexSources(name, content); err != nil {
		t.Fatalf("index: %v", err)
	}
	targets, err := ExtractTargets(name, content)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("want 1 target, got %d: %#v", len(targets), targets)
	}
	tg := targets[0]
	if err := idx.Resolve(&tg); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return tg
}

func TestExtractTargets_ChartRefDeferred(t *testing.T) {
	content := readManifest(t, "helmrelease-chartref-oci.yaml")
	targets, err := ExtractTargets("helmrelease-chartref-oci.yaml", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Fatalf("want 1 chart target (OCIRepository is a source, not a target), got %d", len(targets))
	}
	tg := targets[0]
	// Before Resolve the target is deferred: it names the chartRef but has no
	// version or edit anchor yet.
	if tg.ChartRefKind != "OCIRepository" || tg.ChartRefName != "podinfo" {
		t.Errorf("chartRef = %q/%q, want OCIRepository/podinfo", tg.ChartRefKind, tg.ChartRefName)
	}
	if tg.CurrentValue != "" || tg.FieldLine != 0 {
		t.Errorf("deferred target should have no version/line yet: %+v", tg)
	}
	if tg.Ref.Kind != "HelmRelease" || tg.Ref.Name != "podinfo" {
		t.Errorf("ref should stay the HelmRelease: %+v", tg.Ref)
	}
}

func TestResolveChartRef_OCIRepository(t *testing.T) {
	tg := resolveSingleTarget(t, "helmrelease-chartref-oci.yaml")
	if !tg.IsOCI || tg.RepoURL != "oci://ghcr.io/stefanprodan/charts" {
		t.Errorf("repo = %q oci=%v, want oci://ghcr.io/stefanprodan/charts true", tg.RepoURL, tg.IsOCI)
	}
	if tg.ChartName != "podinfo" {
		t.Errorf("chart = %q, want podinfo", tg.ChartName)
	}
	if tg.CurrentValue != "6.5.0" {
		t.Errorf("current = %q, want 6.5.0", tg.CurrentValue)
	}
	// The edit anchor must move onto the OCIRepository's spec.ref.tag (line 19).
	if tg.FieldJSONPath != "$.spec.ref.tag" || tg.FieldLine != 19 {
		t.Errorf("anchor = %s:%d, want $.spec.ref.tag:19", tg.FieldJSONPath, tg.FieldLine)
	}
	if tg.File != "helmrelease-chartref-oci.yaml" {
		t.Errorf("file = %q, want the OCIRepository file", tg.File)
	}
}

func TestResolveChartRef_HelmChart(t *testing.T) {
	tg := resolveSingleTarget(t, "helmrelease-chartref-helmchart.yaml")
	if tg.IsOCI || tg.RepoURL != "https://charts.example.com/app" {
		t.Errorf("repo = %q oci=%v, want https://charts.example.com/app false", tg.RepoURL, tg.IsOCI)
	}
	if tg.ChartName != "app" {
		t.Errorf("chart = %q, want app", tg.ChartName)
	}
	if tg.CurrentValue != "1.2.0" {
		t.Errorf("current = %q, want 1.2.0", tg.CurrentValue)
	}
	// Anchor moves onto the HelmChart's spec.version (line 18).
	if tg.FieldJSONPath != "$.spec.version" || tg.FieldLine != 18 {
		t.Errorf("anchor = %s:%d, want $.spec.version:18", tg.FieldJSONPath, tg.FieldLine)
	}
}

func TestResolveChartRef_MissingSourceFailsLoud(t *testing.T) {
	idx := NewSourceIndex(nil)
	tg := UpdateTarget{Kind: TargetChart, ChartRefKind: "OCIRepository", ChartRefName: "ghost"}
	tg.Ref.Namespace = "apps"
	tg.Ref.Name = "podinfo"
	err := idx.Resolve(&tg)
	if err == nil || !strings.Contains(err.Error(), "OCIRepository") {
		t.Fatalf("expected loud error naming OCIRepository, got %v", err)
	}
}

func TestSplitOCIChartURL(t *testing.T) {
	repo, chart := splitOCIChartURL("oci://ghcr.io/stefanprodan/charts/podinfo")
	if repo != "oci://ghcr.io/stefanprodan/charts" || chart != "podinfo" {
		t.Fatalf("split = %q/%q", repo, chart)
	}
}
