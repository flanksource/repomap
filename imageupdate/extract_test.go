package imageupdate

import (
	"os"
	"path/filepath"
	"testing"
)

func readManifest(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "manifests", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

func TestExtractTargets_Deployment(t *testing.T) {
	targets, err := ExtractTargets("deployment.yaml", readManifest(t, "deployment.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 {
		t.Fatalf("want 2 image targets, got %d", len(targets))
	}

	web := targets[0]
	if web.Kind != TargetImage {
		t.Errorf("kind = %q, want image", web.Kind)
	}
	if web.CurrentValue != "nginx:1.25.3" {
		t.Errorf("current = %q, want nginx:1.25.3", web.CurrentValue)
	}
	if web.FieldLine != 12 {
		t.Errorf("field line = %d, want 12", web.FieldLine)
	}
	if web.ContainerName != "web" {
		t.Errorf("container = %q, want web", web.ContainerName)
	}
	if web.Image == nil || web.Image.ImageTag.TagName != "1.25.3" {
		t.Errorf("parsed image tag mismatch: %+v", web.Image)
	}
	if web.Ref.Kind != "Deployment" || web.Ref.Name != "web" || web.Ref.Namespace != "default" {
		t.Errorf("ref mismatch: %+v", web.Ref)
	}

	sidecar := targets[1]
	if sidecar.CurrentValue != "ghcr.io/flanksource/proxy:v0.4.1" {
		t.Errorf("sidecar current = %q", sidecar.CurrentValue)
	}
	if sidecar.FieldLine != 14 {
		t.Errorf("sidecar field line = %d, want 14", sidecar.FieldLine)
	}
}

func TestExtractTargets_DigestPinnedStatefulSet(t *testing.T) {
	targets, err := ExtractTargets("statefulset.yaml", readManifest(t, "statefulset.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Fatalf("want 1 target, got %d", len(targets))
	}
	tg := targets[0]
	if tg.Image.ImageTag.TagName != "15.4" {
		t.Errorf("tag = %q, want 15.4", tg.Image.ImageTag.TagName)
	}
	wantDigest := "sha256:1eeb4c7316bacb1d4c8ead65571cd92dd21e27359f0d4917f1a5822a73b75db1"
	if tg.Image.ImageTag.TagDigest != wantDigest {
		t.Errorf("digest = %q, want %q", tg.Image.ImageTag.TagDigest, wantDigest)
	}
	if tg.FieldLine != 11 {
		t.Errorf("field line = %d, want 11", tg.FieldLine)
	}
}

func TestExtractTargets_HelmReleaseMultiDoc(t *testing.T) {
	targets, err := ExtractTargets("helmrelease.yaml", readManifest(t, "helmrelease.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Fatalf("want 1 chart target (HelmRepository is not a target), got %d", len(targets))
	}
	tg := targets[0]
	if tg.Kind != TargetChart {
		t.Errorf("kind = %q, want chart", tg.Kind)
	}
	if tg.CurrentValue != "6.5.0" {
		t.Errorf("current = %q, want 6.5.0", tg.CurrentValue)
	}
	if tg.ChartName != "podinfo" {
		t.Errorf("chart = %q, want podinfo", tg.ChartName)
	}
	if tg.SourceRefName != "podinfo" {
		t.Errorf("sourceRef = %q, want podinfo", tg.SourceRefName)
	}
	if tg.FieldLine != 10 {
		t.Errorf("field line = %d, want 10", tg.FieldLine)
	}
}
