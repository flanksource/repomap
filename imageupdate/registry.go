package imageupdate

import (
	"context"
	"fmt"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// RegistryClient is the registry capability needed by image version resolution.
type RegistryClient interface {
	Tags(context.Context) ([]string, error)
	Digest(context.Context, string) (string, error)
}

// RegistryClientFactory builds a client scoped to one image repository.
type RegistryClientFactory func(context.Context, *ContainerImage) (RegistryClient, error)

type remoteRegistryClient struct {
	repository name.Repository
	auth       authn.Authenticator
}

func newRemoteRegistryClient(ctx context.Context, img *ContainerImage, creds CredentialResolver) (RegistryClient, error) {
	repository, err := name.NewRepository(img.GetFullNameWithoutTag())
	if err != nil {
		return nil, fmt.Errorf("parse repository %s: %w", img.GetFullNameWithoutTag(), err)
	}
	user, pass, err := creds.Resolve(ctx, repository.RegistryStr())
	if err != nil {
		return nil, err
	}
	var auth authn.Authenticator = authn.Anonymous
	if user != "" || pass != "" {
		auth = authn.FromConfig(authn.AuthConfig{Username: user, Password: pass})
	}
	return &remoteRegistryClient{repository: repository, auth: auth}, nil
}

func (c *remoteRegistryClient) Tags(ctx context.Context) ([]string, error) {
	return remote.List(c.repository, remote.WithContext(ctx), remote.WithAuth(c.auth))
}

func (c *remoteRegistryClient) Digest(ctx context.Context, tag string) (string, error) {
	ref, err := name.NewTag(c.repository.Name() + ":" + tag)
	if err != nil {
		return "", err
	}
	descriptor, err := remote.Head(ref, remote.WithContext(ctx), remote.WithAuth(c.auth))
	if err != nil {
		return "", err
	}
	return descriptor.Digest.String(), nil
}
