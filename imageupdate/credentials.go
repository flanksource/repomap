package imageupdate

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
)

// CredentialResolver resolves a username/password for a registry host from the
// local environment (Docker config + credential helpers). An empty user and
// password means anonymous access, which is correct for public registries.
type CredentialResolver interface {
	Resolve(ctx context.Context, host string) (user, pass string, err error)
}

// KeychainResolver resolves credentials via go-containerregistry's keychain,
// which reads ~/.docker/config.json and runs credential helpers
// (docker-credential-ecr-login, osxkeychain, ...) exactly like docker/crane.
type KeychainResolver struct {
	keychain authn.Keychain
}

// NewKeychainResolver returns a resolver backed by the default Docker keychain.
func NewKeychainResolver() *KeychainResolver {
	return &KeychainResolver{keychain: authn.DefaultKeychain}
}

// Resolve returns the username/password the keychain holds for host. Unknown
// hosts resolve to anonymous (empty user/pass, nil error) so public access is
// unaffected.
func (r *KeychainResolver) Resolve(ctx context.Context, host string) (string, string, error) {
	host = registryHost(host)
	if host == "" {
		return "", "", nil
	}
	reg, err := name.NewRegistry(host)
	if err != nil {
		return "", "", fmt.Errorf("invalid registry host %q: %w", host, err)
	}
	authenticator, err := r.keychain.Resolve(reg)
	if err != nil {
		return "", "", fmt.Errorf("resolve credentials for %q: %w", host, err)
	}
	cfg, err := authenticator.Authorization()
	if err != nil {
		return "", "", fmt.Errorf("authorization for %q: %w", host, err)
	}
	if cfg == nil {
		return "", "", nil
	}
	return cfg.Username, cfg.Password, nil
}

// registryHost normalises a registry reference to a bare host: it strips any
// scheme (oci://, https://) and trailing path so the keychain can key on it.
func registryHost(ref string) string {
	ref = strings.TrimSpace(ref)
	if i := strings.Index(ref, "://"); i >= 0 {
		ref = ref[i+3:]
	}
	if i := strings.Index(ref, "/"); i >= 0 {
		ref = ref[:i]
	}
	return ref
}
