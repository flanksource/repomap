package cel

import (
	"testing"

	"github.com/flanksource/repomap"
	"github.com/flanksource/repomap/kubernetes"
)

func TestBuildContextNilInputs(t *testing.T) {
	ctx := BuildContext(nil, nil, nil)

	commit := ctx["commit"].(map[string]any)
	if commit["hash"] != "" {
		t.Errorf("commit.hash = %v, want empty string", commit["hash"])
	}
	if commit["line_changes"] != 0 {
		t.Errorf("commit.line_changes = %v, want 0", commit["line_changes"])
	}

	change := ctx["change"].(map[string]any)
	if change["type"] != "" {
		t.Errorf("change.type = %v, want empty string", change["type"])
	}

	k8s := ctx["kubernetes"].(map[string]any)
	if k8s["is_kubernetes"] != false {
		t.Errorf("kubernetes.is_kubernetes = %v, want false", k8s["is_kubernetes"])
	}

	file := ctx["file"].(map[string]any)
	if file["extension"] != "" {
		t.Errorf("file.extension = %v, want empty string", file["extension"])
	}
}

func TestBuildContextWithData(t *testing.T) {
	commit := &repomap.CommitAnalysis{
		Commit: repomap.Commit{
			Hash:       "abc123",
			CommitType: repomap.CommitTypeFeat,
			Subject:    "add feature",
		},
		TotalLineChanges:   150,
		TotalResourceCount: 3,
		Changes: repomap.Changes{
			{File: "a.go"},
			{File: "b.go"},
		},
	}

	change := &repomap.CommitChange{
		File:  "deploy.yaml",
		Type:  repomap.SourceChangeTypeModified,
		Adds:  10,
		Dels:  5,
		Scope: repomap.Scopes{repomap.ScopeType("kubernetes")},
	}

	replicas := 3
	newReplicas := 5
	k8sChange := &kubernetes.KubernetesChange{
		KubernetesRef: kubernetes.KubernetesRef{
			Kind:      "Deployment",
			Name:      "my-app",
			Namespace: "prod",
		},
		Scaling: &kubernetes.Scaling{
			Replicas:    &replicas,
			NewReplicas: &newReplicas,
		},
	}

	ctx := BuildContext(commit, change, k8sChange)

	commitCtx := ctx["commit"].(map[string]any)
	if commitCtx["hash"] != "abc123" {
		t.Errorf("commit.hash = %v, want 'abc123'", commitCtx["hash"])
	}
	if commitCtx["line_changes"] != 150 {
		t.Errorf("commit.line_changes = %v, want 150", commitCtx["line_changes"])
	}
	if commitCtx["file_count"] != 2 {
		t.Errorf("commit.file_count = %v, want 2", commitCtx["file_count"])
	}

	k8sCtx := ctx["kubernetes"].(map[string]any)
	if k8sCtx["is_kubernetes"] != true {
		t.Errorf("kubernetes.is_kubernetes = %v, want true", k8sCtx["is_kubernetes"])
	}
	if k8sCtx["replica_delta"] != 2 {
		t.Errorf("kubernetes.replica_delta = %v, want 2", k8sCtx["replica_delta"])
	}

	fileCtx := ctx["file"].(map[string]any)
	if fileCtx["extension"] != ".yaml" {
		t.Errorf("file.extension = %v, want '.yaml'", fileCtx["extension"])
	}
	if fileCtx["tech"] != "kubernetes" {
		t.Errorf("file.tech = %v, want 'kubernetes'", fileCtx["tech"])
	}
}

func TestBuildFileContextTestDetection(t *testing.T) {
	change := &repomap.CommitChange{File: "main_test.go"}
	ctx := buildFileContext(change)
	if ctx["is_test"] != true {
		t.Errorf("is_test = %v for main_test.go, want true", ctx["is_test"])
	}

	change = &repomap.CommitChange{File: "app.spec.ts"}
	ctx = buildFileContext(change)
	if ctx["is_test"] != true {
		t.Errorf("is_test = %v for app.spec.ts, want true", ctx["is_test"])
	}

	change = &repomap.CommitChange{File: "main.go"}
	ctx = buildFileContext(change)
	if ctx["is_test"] != false {
		t.Errorf("is_test = %v for main.go, want false", ctx["is_test"])
	}
}

func TestBuildFileContextConfigDetection(t *testing.T) {
	change := &repomap.CommitChange{File: "config.yaml"}
	ctx := buildFileContext(change)
	if ctx["is_config"] != true {
		t.Errorf("is_config = %v for config.yaml, want true", ctx["is_config"])
	}

	change = &repomap.CommitChange{File: ".env.production"}
	ctx = buildFileContext(change)
	if ctx["is_config"] != true {
		t.Errorf("is_config = %v for .env.production, want true", ctx["is_config"])
	}
}
