package deps

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	labelBaseName   = "org.opencontainers.image.base.name"
	labelBaseDigest = "org.opencontainers.image.base.digest"
	labelSource     = "org.opencontainers.image.source"
)

// imageResolver resolves the base image(s) of a container image by combining the
// OCI base-image label with the Dockerfile FROM directives in the image's source
// repository.
type imageResolver struct {
	cache  RemoteCache
	parser func(content string) ([]imageRef, []string)
}

func newImageResolver(cache RemoteCache) *imageResolver {
	return &imageResolver{cache: cache, parser: parseDockerfileFrom}
}

// baseImages returns the distinct base images of ref. It never returns a hard
// error: remote failures are reported as warnings so recursion degrades per
// branch.
func (r *imageResolver) baseImages(ctx context.Context, ref string) (bases []imageRef, warnings []string) {
	cfg, err := r.cache.ImageConfig(ctx, ref)
	if err != nil {
		return nil, []string{fmt.Sprintf("image %s: %s", ref, err)}
	}

	seen := map[string]bool{}
	add := func(b imageRef) {
		key := b.Name + ":" + b.Version + "@" + b.Digest
		if b.Name == "" || seen[key] {
			return
		}
		seen[key] = true
		bases = append(bases, b)
	}

	// Signal A: explicit OCI base-image label.
	if name := cfg.Labels[labelBaseName]; name != "" {
		b := parseImageRef(name)
		if b.Digest == "" {
			b.Digest = cfg.Labels[labelBaseDigest]
		}
		add(b)
	}

	// Signal B: source repo Dockerfile FROM.
	source := cfg.Labels[labelSource]
	if source == "" {
		source = sourceRepoHeuristic(ref)
	}
	if source != "" {
		dbases, dwarn := r.dockerfileBases(ctx, ref, source)
		warnings = append(warnings, dwarn...)
		for _, b := range dbases {
			add(b)
		}
	}

	if len(bases) == 0 && len(warnings) == 0 {
		warnings = append(warnings, fmt.Sprintf("image %s: no base image resolvable (no base/source label, no known registry heuristic)", ref))
	}
	return bases, warnings
}

func (r *imageResolver) dockerfileBases(ctx context.Context, ref, source string) ([]imageRef, []string) {
	url := normalizeGitURL(source)
	if url == "" {
		return nil, []string{fmt.Sprintf("image %s: unrecognized source %q", ref, source)}
	}
	dir, err := r.cache.GitRepo(ctx, url, parseImageRef(ref).Version)
	if err != nil {
		return nil, []string{fmt.Sprintf("image %s: clone %s: %s", ref, url, err)}
	}
	path, warnings := findDockerfile(dir)
	if path == "" {
		return nil, []string{fmt.Sprintf("image %s: no Dockerfile in %s", ref, source)}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, append(warnings, fmt.Sprintf("image %s: reading Dockerfile: %s", ref, err))
	}
	bases, pwarn := r.parser(string(data))
	return bases, append(warnings, pwarn...)
}

// sourceRepoHeuristic maps a registry path to a probable git source URL for
// registries that mirror their org/repo layout from GitHub.
func sourceRepoHeuristic(ref string) string {
	name := stripImageVersion(ref)
	for _, host := range []string{"ghcr.io/", "quay.io/"} {
		if strings.HasPrefix(name, host) {
			parts := strings.Split(strings.TrimPrefix(name, host), "/")
			if len(parts) >= 2 {
				return "https://github.com/" + parts[0] + "/" + parts[1]
			}
		}
	}
	return ""
}

// normalizeGitURL turns an OCI source label into a cloneable URL.
func normalizeGitURL(source string) string {
	source = strings.TrimSpace(source)
	source = strings.TrimPrefix(source, "git+")
	switch {
	case source == "":
		return ""
	case strings.HasPrefix(source, "http://"), strings.HasPrefix(source, "https://"), strings.HasPrefix(source, "git@"):
		return strings.TrimSuffix(source, ".git")
	case strings.HasPrefix(source, "github.com/"), strings.HasPrefix(source, "gitlab.com/"):
		return "https://" + strings.TrimSuffix(source, ".git")
	default:
		return ""
	}
}

// findDockerfile returns the repo-root Dockerfile if present, else the first
// Dockerfile found in a shallow walk, warning when several exist.
func findDockerfile(root string) (string, []string) {
	if p := filepath.Join(root, "Dockerfile"); fileExists(p) {
		return p, nil
	}
	var found []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" || ignoredDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "Dockerfile" || strings.HasPrefix(d.Name(), "Dockerfile.") {
			found = append(found, path)
		}
		return nil
	})
	if len(found) == 0 {
		return "", nil
	}
	if len(found) > 1 {
		rels := make([]string, len(found))
		for i, f := range found {
			rels[i], _ = filepath.Rel(root, f)
		}
		return found[0], []string{fmt.Sprintf("multiple Dockerfiles found, using %s (others: %s)", rels[0], strings.Join(rels[1:], ", "))}
	}
	return found[0], nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
