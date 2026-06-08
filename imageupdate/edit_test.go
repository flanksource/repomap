package imageupdate

import (
	"os"
	"path/filepath"
	"testing"
)

func readGolden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "golden", name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return string(b)
}

func TestEdit_BareValuePreservesComment(t *testing.T) {
	content := readManifest(t, "deployment.yaml")
	targets, _ := ExtractTargets("deployment.yaml", content)
	res, err := Edit(content, targets[0], "nginx:1.27.0")
	if err != nil {
		t.Fatal(err)
	}
	if res.NewContent != readGolden(t, "deployment-web-1.27.0.yaml") {
		t.Errorf("content mismatch:\n--- got ---\n%s", res.NewContent)
	}
	if res.Before != "          image: nginx:1.25.3 # pin nginx" {
		t.Errorf("before = %q", res.Before)
	}
}

func TestEdit_QuotedDigestPinnedValue(t *testing.T) {
	content := readManifest(t, "statefulset.yaml")
	targets, _ := ExtractTargets("statefulset.yaml", content)
	newValue := "registry.k8s.io/postgres:15.5@sha256:aaaa4c7316bacb1d4c8ead65571cd92dd21e27359f0d4917f1a5822a73b75dbff"
	res, err := Edit(content, targets[0], newValue)
	if err != nil {
		t.Fatal(err)
	}
	if res.NewContent != readGolden(t, "statefulset-15.5.yaml") {
		t.Errorf("content mismatch:\n--- got ---\n%s", res.NewContent)
	}
}

func TestEdit_ChartVersionInMultiDoc(t *testing.T) {
	content := readManifest(t, "helmrelease.yaml")
	targets, _ := ExtractTargets("helmrelease.yaml", content)
	res, err := Edit(content, targets[0], "6.6.0")
	if err != nil {
		t.Fatal(err)
	}
	if res.NewContent != readGolden(t, "helmrelease-6.6.0.yaml") {
		t.Errorf("content mismatch:\n--- got ---\n%s", res.NewContent)
	}
}

func TestEdit_OutOfRangeFails(t *testing.T) {
	_, err := Edit("a: b\n", UpdateTarget{File: "x", FieldLine: 99}, "z")
	if err == nil {
		t.Fatal("expected out-of-range error")
	}
}

func TestApplyEdit_WritesAndDryRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deployment.yaml")
	orig := readManifest(t, "deployment.yaml")
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	targets, _ := ExtractTargets(path, orig)

	// dry-run must not modify the file
	if _, err := ApplyEdit(path, targets[0], "nginx:1.27.0", true); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != orig {
		t.Error("dry-run modified the file")
	}

	// real write must change exactly the one line
	if _, err := ApplyEdit(path, targets[0], "nginx:1.27.0", false); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != readGolden(t, "deployment-web-1.27.0.yaml") {
		t.Errorf("written content mismatch:\n%s", got)
	}
}
