package kubernetes

import "testing"

func TestIsYaml(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"deploy.yaml", true},
		{"deploy.yml", true},
		{"deploy.YAML", true},
		{"deploy.go", false},
		{"deploy.json", false},
	}

	for _, tt := range tests {
		if got := IsYaml(tt.path); got != tt.expected {
			t.Errorf("IsYaml(%q) = %v, want %v", tt.path, got, tt.expected)
		}
	}
}

func TestParseYAMLDocuments(t *testing.T) {
	content := `apiVersion: v1
kind: Service
metadata:
  name: my-svc
  namespace: default
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-deploy
`
	docs, err := ParseYAMLDocuments(content)
	if err != nil {
		t.Fatalf("ParseYAMLDocuments() error: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 documents, got %d", len(docs))
	}
	if docs[0].Content["kind"] != "Service" {
		t.Errorf("doc[0].kind = %v, want Service", docs[0].Content["kind"])
	}
	if docs[1].Content["kind"] != "Deployment" {
		t.Errorf("doc[1].kind = %v, want Deployment", docs[1].Content["kind"])
	}
}

func TestIsKubernetesResource(t *testing.T) {
	k8s := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Service",
	}
	if !IsKubernetesResource(k8s) {
		t.Error("expected k8s resource to be detected")
	}

	notK8s := map[string]interface{}{
		"name": "test",
	}
	if IsKubernetesResource(notK8s) {
		t.Error("expected non-k8s resource to NOT be detected")
	}
}

func TestExtractKubernetesRefsFromContent(t *testing.T) {
	content := `apiVersion: v1
kind: Service
metadata:
  name: my-svc
  namespace: production
  labels:
    app: web
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-deploy
---
# Non-k8s document
name: config
value: test
`
	refs, err := ExtractKubernetesRefsFromContent(content)
	if err != nil {
		t.Fatalf("ExtractKubernetesRefsFromContent() error: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs, got %d", len(refs))
	}

	if refs[0].Kind != "Service" || refs[0].Name != "my-svc" || refs[0].Namespace != "production" {
		t.Errorf("ref[0] = %+v, want Service/my-svc/production", refs[0])
	}
	if refs[0].Labels["app"] != "web" {
		t.Errorf("ref[0].Labels[app] = %q, want 'web'", refs[0].Labels["app"])
	}
	if refs[1].Kind != "Deployment" || refs[1].Name != "my-deploy" {
		t.Errorf("ref[1] = %+v, want Deployment/my-deploy", refs[1])
	}
}

func TestAnalyzeVersionChange(t *testing.T) {
	tests := []struct {
		oldVer   string
		newVer   string
		expected VersionChangeType
	}{
		{"1.0.0", "2.0.0", VersionChangeMajor},
		{"1.0.0", "1.1.0", VersionChangeMinor},
		{"1.0.0", "1.0.1", VersionChangePatch},
		{"v1.0.0", "v2.0.0", VersionChangeMajor},
		{"notversion", "alsonotversion", VersionChangeUnknown},
		{"2.0.0", "1.0.0", VersionChangeUnknown}, // downgrade
	}

	for _, tt := range tests {
		vc := AnalyzeVersionChange(tt.oldVer, tt.newVer)
		if vc.ChangeType != tt.expected {
			t.Errorf("AnalyzeVersionChange(%q, %q).ChangeType = %q, want %q",
				tt.oldVer, tt.newVer, vc.ChangeType, tt.expected)
		}
	}
}
