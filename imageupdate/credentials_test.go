package imageupdate

import (
	"context"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
)

// fakeKeychain returns configured creds for known hosts and anonymous otherwise.
type fakeKeychain struct {
	creds map[string]authn.AuthConfig
}

func (f fakeKeychain) Resolve(r authn.Resource) (authn.Authenticator, error) {
	if c, ok := f.creds[r.RegistryStr()]; ok {
		return authn.FromConfig(c), nil
	}
	return authn.Anonymous, nil
}

func TestRegistryHost(t *testing.T) {
	cases := map[string]string{
		"oci://public.ecr.aws/flanksource": "public.ecr.aws",
		"https://charts.example.com/foo":   "charts.example.com",
		"ghcr.io/org/repo":                 "ghcr.io",
		"public.ecr.aws":                   "public.ecr.aws",
		"":                                 "",
	}
	for in, want := range cases {
		if got := registryHost(in); got != want {
			t.Errorf("registryHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestKeychainResolver_KnownHost(t *testing.T) {
	r := &KeychainResolver{keychain: fakeKeychain{creds: map[string]authn.AuthConfig{
		"public.ecr.aws": {Username: "AWS", Password: "tok123"},
	}}}
	user, pass, err := r.Resolve(context.Background(), "oci://public.ecr.aws/flanksource")
	if err != nil {
		t.Fatal(err)
	}
	if user != "AWS" || pass != "tok123" {
		t.Errorf("got %q/%q, want AWS/tok123", user, pass)
	}
}

func TestKeychainResolver_UnknownHostAnonymous(t *testing.T) {
	r := &KeychainResolver{keychain: fakeKeychain{}}
	user, pass, err := r.Resolve(context.Background(), "ghcr.io")
	if err != nil {
		t.Fatal(err)
	}
	if user != "" || pass != "" {
		t.Errorf("unknown host should be anonymous, got %q/%q", user, pass)
	}
}

func TestKeychainResolver_EmptyHost(t *testing.T) {
	r := &KeychainResolver{keychain: fakeKeychain{}}
	user, pass, err := r.Resolve(context.Background(), "")
	if err != nil || user != "" || pass != "" {
		t.Errorf("empty host should yield empty creds, no error; got %q/%q err=%v", user, pass, err)
	}
}
