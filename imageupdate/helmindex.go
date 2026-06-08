package imageupdate

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/flanksource/commons/http"
	"github.com/goccy/go-yaml"
)

// HelmIndexClient lists available chart versions from a classic HTTP Helm
// repository. OCI Helm charts are resolved through the registry path instead.
type HelmIndexClient interface {
	Versions(ctx context.Context, repoURL, chart string) ([]string, error)
}

// helmIndex is the subset of a Helm repository index.yaml we consume.
type helmIndex struct {
	Entries map[string][]struct {
		Version string `yaml:"version"`
		Name    string `yaml:"name"`
	} `yaml:"entries"`
}

// HTTPHelmIndexClient fetches and parses <repoURL>/index.yaml over HTTP,
// authenticating private repos with credentials from the local Docker store.
type HTTPHelmIndexClient struct {
	client *http.Client
	creds  CredentialResolver
}

// NewHTTPHelmIndexClient returns a client with a sane default timeout. creds may
// be nil, in which case all requests are anonymous.
func NewHTTPHelmIndexClient(creds CredentialResolver) *HTTPHelmIndexClient {
	return &HTTPHelmIndexClient{
		client: http.NewClient().Timeout(30 * time.Second),
		creds:  creds,
	}
}

// Versions fetches the repository index and returns every published version for
// chart. It fails loud when the index is unreachable or the chart is absent.
func (c *HTTPHelmIndexClient) Versions(ctx context.Context, repoURL, chart string) ([]string, error) {
	indexURL := strings.TrimSuffix(repoURL, "/") + "/index.yaml"
	req := c.client.R(ctx)
	if header, err := c.authHeader(ctx, repoURL); err != nil {
		return nil, err
	} else if header != "" {
		req = req.Header("Authorization", header)
	}
	resp, err := req.Get(indexURL)
	url := indexURL
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	if !resp.IsOK() {
		return nil, fmt.Errorf("fetch %s: status %d", url, resp.StatusCode)
	}
	body, err := resp.AsString()
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
	}

	var idx helmIndex
	if err := yaml.Unmarshal([]byte(body), &idx); err != nil {
		return nil, fmt.Errorf("parse %s: %w", url, err)
	}
	entries, ok := idx.Entries[chart]
	if !ok {
		return nil, fmt.Errorf("chart %q not found in %s", chart, url)
	}
	versions := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Version != "" {
			versions = append(versions, e.Version)
		}
	}
	return versions, nil
}

// authHeader returns a Basic auth header for repoURL's host when the credential
// store has credentials for it, else an empty string (anonymous).
func (c *HTTPHelmIndexClient) authHeader(ctx context.Context, repoURL string) (string, error) {
	if c.creds == nil {
		return "", nil
	}
	u, err := url.Parse(repoURL)
	if err != nil || u.Host == "" {
		return "", nil
	}
	user, pass, err := c.creds.Resolve(ctx, u.Host)
	if err != nil {
		return "", err
	}
	if user == "" && pass == "" {
		return "", nil
	}
	token := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
	return "Basic " + token, nil
}
