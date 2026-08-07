package imageupdate

import (
	"context"
	"fmt"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// LabelResolver reads an image's manifest digest and OCI labels from its
// registry, authenticating via the local Docker credential store. It is used by
// the deps recursion to discover an image's source repository
// (org.opencontainers.image.source) and declared base image
// (org.opencontainers.image.base.name/base.digest).
type LabelResolver struct {
	creds CredentialResolver
}

// NewLabelResolver returns a LabelResolver wired to the local Docker keychain.
func NewLabelResolver() *LabelResolver {
	return &LabelResolver{creds: NewKeychainResolver()}
}

// Labels resolves the manifest digest and OCI labels for an image reference of
// the form registry/repo:tag[@digest].
func (l *LabelResolver) Labels(ctx context.Context, ref string) (digest string, labels map[string]string, err error) {
	img := NewContainerImage(ref)
	tagName := "latest"
	if img.ImageTag != nil && img.ImageTag.TagName != "" {
		tagName = img.ImageTag.TagName
	}
	tag, err := name.NewTag(img.GetFullNameWithoutTag() + ":" + tagName)
	if err != nil {
		return "", nil, fmt.Errorf("parse reference %s:%s: %w", img.GetFullNameWithoutTag(), tagName, err)
	}

	user, pass, err := l.creds.Resolve(ctx, tag.RegistryStr())
	if err != nil {
		return "", nil, err
	}
	var auth authn.Authenticator = authn.Anonymous
	if user != "" || pass != "" {
		auth = authn.FromConfig(authn.AuthConfig{Username: user, Password: pass})
	}

	remoteImage, err := remote.Image(tag, remote.WithContext(ctx), remote.WithAuth(auth))
	if err != nil {
		return "", nil, fmt.Errorf("manifest for %s: %w", tag, err)
	}
	manifestDigest, err := remoteImage.Digest()
	if err != nil {
		return "", nil, fmt.Errorf("digest for %s: %w", tag, err)
	}
	config, err := remoteImage.ConfigFile()
	if err != nil {
		return "", nil, fmt.Errorf("config for %s: %w", tag, err)
	}
	return manifestDigest.String(), config.Config.Labels, nil
}
