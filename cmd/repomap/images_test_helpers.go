package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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

// writeRepo creates a temp git repo with one manifest and a repomap conf rooted
// there. The manifest is committed so git ls-files discovers it.
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
	if out, err := exec.Command("git", "-C", dir, "add", rel).CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, out)
	}
	conf, err := repomap.GetConf(dir)
	if err != nil {
		t.Fatal(err)
	}
	return conf, rel
}
