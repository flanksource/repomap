package deps

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"
	"strings"

	"github.com/goccy/go-yaml"
	"helm.sh/helm/v3/pkg/helmpath"
)

// helmRepoCreds mirrors one entry of Helm's repositories.yaml, carrying the
// authentication a private chart repository needs.
type helmRepoCreds struct {
	Name                  string `yaml:"name"`
	URL                   string `yaml:"url"`
	Username              string `yaml:"username"`
	Password              string `yaml:"password"`
	CertFile              string `yaml:"certFile"`
	KeyFile               string `yaml:"keyFile"`
	CAFile                string `yaml:"caFile"`
	InsecureSkipTLSVerify bool   `yaml:"insecure_skip_tls_verify"`
}

type helmRepoFile struct {
	Repositories []helmRepoCreds `yaml:"repositories"`
}

// helmCredentials reuses the user's `helm repo add` logins so chart discovery
// authenticates to private repositories exactly like the helm CLI.
type helmCredentials struct {
	repos []helmRepoCreds
}

// loadHelmCredentials reads repositories.yaml from $HELM_REPOSITORY_CONFIG or
// Helm's default config path. A missing or unreadable file yields no
// credentials (public repositories keep working).
func loadHelmCredentials() *helmCredentials {
	path := os.Getenv("HELM_REPOSITORY_CONFIG")
	if path == "" {
		path = helmpath.ConfigPath("repositories.yaml")
	}
	return newHelmCredentials(path)
}

func newHelmCredentials(path string) *helmCredentials {
	data, err := os.ReadFile(path)
	if err != nil {
		return &helmCredentials{}
	}
	var f helmRepoFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return &helmCredentials{}
	}
	return &helmCredentials{repos: f.Repositories}
}

// match returns the credentials for the repository whose URL is the longest
// prefix of url (so index.yaml/.tgz fetches under a repo inherit its login).
func (h *helmCredentials) match(url string) (helmRepoCreds, bool) {
	best := -1
	var found helmRepoCreds
	for _, r := range h.repos {
		base := strings.TrimSuffix(r.URL, "/")
		if base == "" || (url != base && !strings.HasPrefix(url, base+"/")) {
			continue
		}
		if len(base) > best {
			best, found = len(base), r
		}
	}
	return found, best >= 0
}

// authorize applies the matched repository's basic-auth and TLS settings to req,
// returning the http client to use (a custom one only when TLS options apply).
func (h *helmCredentials) authorize(req *http.Request) *http.Client {
	if h == nil {
		return http.DefaultClient
	}
	cred, ok := h.match(req.URL.String())
	if !ok {
		return http.DefaultClient
	}
	if cred.Username != "" {
		req.SetBasicAuth(cred.Username, cred.Password)
	}
	return httpClientForCreds(cred)
}

func httpClientForCreds(cred helmRepoCreds) *http.Client {
	if cred.CAFile == "" && cred.CertFile == "" && !cred.InsecureSkipTLSVerify {
		return http.DefaultClient
	}
	tlsCfg := &tls.Config{InsecureSkipVerify: cred.InsecureSkipTLSVerify} //nolint:gosec // honors the user's helm repo setting
	if cred.CAFile != "" {
		if pem, err := os.ReadFile(cred.CAFile); err == nil {
			pool := x509.NewCertPool()
			if pool.AppendCertsFromPEM(pem) {
				tlsCfg.RootCAs = pool
			}
		}
	}
	if cred.CertFile != "" && cred.KeyFile != "" {
		if cert, err := tls.LoadX509KeyPair(cred.CertFile, cred.KeyFile); err == nil {
			tlsCfg.Certificates = []tls.Certificate{cert}
		}
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: tlsCfg}}
}
