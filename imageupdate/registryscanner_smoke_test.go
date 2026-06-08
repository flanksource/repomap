package imageupdate

import (
	"testing"

	"github.com/argoproj-labs/argocd-image-updater/registry-scanner/pkg/image"
)

// TestRegistryScannerWires confirms the registry-scanner dependency resolves and
// links against repomap's module graph (k8s.io v0.36, distribution/v3, logrus).
// It exercises the image-ref parser we rely on for tag/digest decomposition.
func TestRegistryScannerWires(t *testing.T) {
	cases := []struct {
		identifier string
		wantName   string
		wantTag    string
		wantDigest string
	}{
		{"nginx:1.25.3", "nginx", "1.25.3", ""},
		{"ghcr.io/flanksource/repomap:v0.4.1", "flanksource/repomap", "v0.4.1", ""},
		{
			"registry.k8s.io/coredns/coredns:v1.11.1@sha256:1eeb4c7316bacb1d4c8ead65571cd92dd21e27359f0d4917f1a5822a73b75db1",
			"coredns/coredns",
			"v1.11.1",
			"sha256:1eeb4c7316bacb1d4c8ead65571cd92dd21e27359f0d4917f1a5822a73b75db1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.identifier, func(t *testing.T) {
			img := image.NewFromIdentifier(tc.identifier)
			if img.ImageName != tc.wantName {
				t.Errorf("ImageName = %q, want %q", img.ImageName, tc.wantName)
			}
			if img.ImageTag == nil {
				t.Fatalf("ImageTag is nil for %q", tc.identifier)
			}
			if img.ImageTag.TagName != tc.wantTag {
				t.Errorf("TagName = %q, want %q", img.ImageTag.TagName, tc.wantTag)
			}
			if img.ImageTag.TagDigest != tc.wantDigest {
				t.Errorf("TagDigest = %q, want %q", img.ImageTag.TagDigest, tc.wantDigest)
			}
		})
	}
}
