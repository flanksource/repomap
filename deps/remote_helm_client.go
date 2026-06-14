package deps

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/goccy/go-yaml"
	"helm.sh/helm/v3/pkg/chart/loader"
)

// helmClient resolves Helm chart dependencies remotely. It uses the Helm SDK's
// chart loader (helm.sh/helm/v3/pkg/chart/loader) to parse fetched charts, and
// parses the repository index.yaml directly over HTTP — the SDK's pkg/repo is
// avoided because it transitively requires a k8s API version removed in the
// k8s release this module pins, and pulls oras/containerd.
//
// Pin note: helm.sh/helm/v3 is held at v3.17.x; v3.18+ bumps
// distribution/distribution/v3 past the version registry-scanner requires.
type helmClient struct {
	cache RemoteCache
}

type helmIndex struct {
	Entries map[string][]helmIndexEntry `yaml:"entries"`
}

type helmIndexEntry struct {
	Version string   `yaml:"version"`
	URLs    []string `yaml:"urls"`
}

type fetchedChart struct {
	Dir          string
	Dependencies []chartDepEntry
	Version      string
}

// fetchChart resolves a Chart.yaml dependency to an extracted chart directory
// plus its own declared dependencies. The returned cleanup removes the
// extraction dir and must be called by the caller.
func (h *helmClient) fetchChart(ctx context.Context, dep chartDepEntry) (*fetchedChart, func(), error) {
	repoURL := strings.TrimSuffix(dep.Repository, "/")
	if repoURL == "" {
		return nil, nil, notFoundError{dep.Name + " (no repository)"}
	}
	if strings.HasPrefix(repoURL, "oci://") {
		return nil, nil, fmt.Errorf("oci helm repositories are not yet supported")
	}

	version, tgzURL, err := h.resolveChart(ctx, repoURL, dep.Name, dep.Version)
	if err != nil {
		return nil, nil, err
	}
	tgz, err := h.cache.Fetch(ctx, tgzURL, ttlImmutable)
	if err != nil {
		return nil, nil, err
	}

	dir, err := os.MkdirTemp("", "repomap-chart-*")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	if err := extractTarGz(tgz, dir); err != nil {
		cleanup()
		return nil, nil, err
	}
	chartDir, err := findChartDir(dir)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	ch, err := loader.Load(chartDir)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("loading chart %s: %w", dep.Name, err)
	}

	deps := make([]chartDepEntry, 0, len(ch.Metadata.Dependencies))
	for _, d := range ch.Metadata.Dependencies {
		deps = append(deps, chartDepEntry{Name: d.Name, Version: d.Version, Repository: d.Repository})
	}
	return &fetchedChart{Dir: chartDir, Dependencies: deps, Version: version}, cleanup, nil
}

// resolveChart picks the newest index version satisfying the dependency's
// version constraint and returns its absolute .tgz URL.
func (h *helmClient) resolveChart(ctx context.Context, repoURL, name, constraint string) (version, tgzURL string, err error) {
	data, err := h.cache.Fetch(ctx, repoURL+"/index.yaml", ttlIndex)
	if err != nil {
		return "", "", err
	}
	var idx helmIndex
	if err := yaml.Unmarshal(data, &idx); err != nil {
		return "", "", fmt.Errorf("parsing index.yaml for %s: %w", repoURL, err)
	}
	entries := idx.Entries[name]
	if len(entries) == 0 {
		return "", "", notFoundError{name + " in " + repoURL}
	}
	version, url := pickChartVersion(entries, constraint)
	if version == "" {
		return "", "", notFoundError{name + "@" + constraint}
	}
	if strings.HasPrefix(url, "oci://") {
		return "", "", fmt.Errorf("chart %s@%s is published as an OCI artifact (%s), which is not yet supported", name, version, url)
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = repoURL + "/" + strings.TrimPrefix(url, "/")
	}
	return version, url, nil
}

// pickChartVersion selects the highest entry satisfying constraint (a SemVer
// range). If constraint is not a valid range it falls back to an exact match.
func pickChartVersion(entries []helmIndexEntry, constraint string) (version, url string) {
	cons, consErr := semver.NewConstraint(constraint)
	var best *semver.Version
	for _, e := range entries {
		if consErr != nil {
			if e.Version == constraint && len(e.URLs) > 0 {
				return e.Version, e.URLs[0]
			}
			continue
		}
		v, err := semver.NewVersion(e.Version)
		if err != nil || !cons.Check(v) || len(e.URLs) == 0 {
			continue
		}
		if best == nil || v.GreaterThan(best) {
			best, version, url = v, e.Version, e.URLs[0]
		}
	}
	return version, url
}

// extractTarGz unpacks a gzipped tar archive into dest, guarding against path
// traversal.
func extractTarGz(data []byte, dest string) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.Clean(hdr.Name))
		if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("tar entry escapes destination: %s", hdr.Name)
		}
		if hdr.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		f, err := os.Create(target)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, tr); err != nil { //nolint:gosec // chart archives are size-bounded
			_ = f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
}

// findChartDir returns the directory containing Chart.yaml within an extracted
// chart archive (charts unpack to a single top-level dir named for the chart).
func findChartDir(root string) (string, error) {
	if _, err := os.Stat(filepath.Join(root, "Chart.yaml")); err == nil {
		return root, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.IsDir() {
			candidate := filepath.Join(root, e.Name())
			if _, err := os.Stat(filepath.Join(candidate, "Chart.yaml")); err == nil {
				return candidate, nil
			}
		}
	}
	return "", fmt.Errorf("no Chart.yaml found under %s", root)
}
