package imageupdate

import (
	"context"
	"fmt"

	"github.com/argoproj-labs/argocd-image-updater/registry-scanner/pkg/image"
	"github.com/argoproj-labs/argocd-image-updater/registry-scanner/pkg/options"
)

// LabelResolver reads an image's config digest and OCI labels from its registry,
// authenticating via the local Docker credential store. It is used by the deps
// recursion to discover an image's source repository
// (org.opencontainers.image.source) and declared base image
// (org.opencontainers.image.base.name/base.digest).
type LabelResolver struct {
	newClient RegistryClientFactory
}

// NewLabelResolver returns a LabelResolver wired to the live registry client.
func NewLabelResolver() *LabelResolver {
	return &LabelResolver{newClient: liveRegistryClientFactory(NewKeychainResolver())}
}

// Labels resolves the config digest and OCI labels for an image reference of the
// form registry/repo:tag[@digest].
func (l *LabelResolver) Labels(ctx context.Context, ref string) (digest string, labels map[string]string, err error) {
	img := image.NewFromIdentifier(ref)
	client, err := l.newClient(ctx, img)
	if err != nil {
		return "", nil, err
	}
	tagName := "latest"
	if img.ImageTag != nil && img.ImageTag.TagName != "" {
		tagName = img.ImageTag.TagName
	}
	manifest, err := client.ManifestForTag(ctx, tagName)
	if err != nil {
		return "", nil, fmt.Errorf("manifest for %s:%s: %w", img.GetFullNameWithoutTag(), tagName, err)
	}
	info, err := client.TagMetadata(ctx, manifest, options.NewManifestOptions())
	if err != nil {
		return "", nil, fmt.Errorf("metadata for %s:%s: %w", img.GetFullNameWithoutTag(), tagName, err)
	}
	return info.EncodedDigest(), info.Labels, nil
}
