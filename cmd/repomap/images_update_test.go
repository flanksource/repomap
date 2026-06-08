package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/argoproj-labs/argocd-image-updater/registry-scanner/pkg/image"
	"github.com/argoproj-labs/argocd-image-updater/registry-scanner/pkg/registry"
	"github.com/argoproj-labs/argocd-image-updater/registry-scanner/pkg/registry/mocks"
	"github.com/stretchr/testify/mock"

	"github.com/flanksource/repomap"
	"github.com/flanksource/repomap/imageupdate"
)

const deploymentManifest = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  template:
    spec:
      containers:
        - name: web
          image: nginx:1.25.3 # keep me
`

func fakeImageResolver(tags []string) *imageupdate.Resolver {
	return &imageupdate.Resolver{
		NewRegistryClient: func(ctx context.Context, img *image.ContainerImage) (registry.RegistryClient, error) {
			m := &mocks.RegistryClient{}
			m.On("Tags", mock.Anything).Return(tags, nil)
			return m, nil
		},
	}
}

// writeRepo creates a temp dir with one manifest and a repomap conf rooted there.
func writeRepo(t *testing.T) (*repomap.ArchConf, string) {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	rel := "deploy.yaml"
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(deploymentManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	conf, err := repomap.GetConf(dir)
	if err != nil {
		t.Fatal(err)
	}
	return conf, rel
}

func TestPlanTarget_LatestDryRun(t *testing.T) {
	conf, rel := writeRepo(t)
	content, _ := os.ReadFile(filepath.Join(conf.RepoPath(), rel))
	targets, err := imageupdate.ExtractTargets(rel, string(content))
	if err != nil || len(targets) != 1 {
		t.Fatalf("extract: %v (%d targets)", err, len(targets))
	}

	resolver := fakeImageResolver([]string{"1.25.3", "1.27.0", "1.28.0-rc.1"})
	plan := planTarget(context.Background(), resolver, conf, targets[0],
		UpdateImageOptions{Latest: true, DryRun: true}, nil)
	if plan.Skipped != "" {
		t.Fatalf("unexpected skip: %s", plan.Skipped)
	}
	if plan.NewValue != "nginx:1.27.0" {
		t.Errorf("new value = %q, want nginx:1.27.0", plan.NewValue)
	}
	if plan.Written {
		t.Error("dry-run must not write")
	}
	// file must be untouched
	after, _ := os.ReadFile(filepath.Join(conf.RepoPath(), rel))
	if string(after) != deploymentManifest {
		t.Error("dry-run modified the manifest")
	}
}

func TestPlanTarget_VersionWritesAndPreservesComment(t *testing.T) {
	conf, rel := writeRepo(t)
	content, _ := os.ReadFile(filepath.Join(conf.RepoPath(), rel))
	targets, _ := imageupdate.ExtractTargets(rel, string(content))

	resolver := fakeImageResolver([]string{"1.25.3", "1.27.0"})
	plan := planTarget(context.Background(), resolver, conf, targets[0],
		UpdateImageOptions{Version: "1.27.0"}, nil)
	if !plan.Written {
		t.Fatalf("expected written, skipped=%q", plan.Skipped)
	}
	after, _ := os.ReadFile(filepath.Join(conf.RepoPath(), rel))
	got := splitLine(string(after), 11)
	want := "          image: nginx:1.27.0 # keep me"
	if got != want {
		t.Errorf("line 11 = %q, want %q", got, want)
	}
}

func TestPlanTarget_RejectsUnavailableVersion(t *testing.T) {
	conf, rel := writeRepo(t)
	content, _ := os.ReadFile(filepath.Join(conf.RepoPath(), rel))
	targets, _ := imageupdate.ExtractTargets(rel, string(content))

	resolver := fakeImageResolver([]string{"1.25.3", "1.27.0"})
	plan := planTarget(context.Background(), resolver, conf, targets[0],
		UpdateImageOptions{Version: "9.9.9"}, nil)
	if plan.Skipped == "" || !strings.Contains(plan.Skipped, "not available") {
		t.Fatalf("expected skip for unavailable version, got skipped=%q written=%v", plan.Skipped, plan.Written)
	}
}

func splitLine(content string, n int) string {
	lines := splitLines(content)
	if n < 1 || n > len(lines) {
		return ""
	}
	return lines[n-1]
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
