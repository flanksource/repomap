package imageupdate

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/argoproj-labs/argocd-image-updater/registry-scanner/pkg/image"
	"github.com/argoproj-labs/argocd-image-updater/registry-scanner/pkg/options"
	"github.com/argoproj-labs/argocd-image-updater/registry-scanner/pkg/registry"
	"github.com/argoproj-labs/argocd-image-updater/registry-scanner/pkg/tag"
	"golang.org/x/sync/singleflight"
)

// RegistryClientFactory builds a ready-to-query registry client for an image
// (with the repository already selected). Production wires registry-scanner;
// tests inject a mock.
type RegistryClientFactory func(ctx context.Context, img *image.ContainerImage) (registry.RegistryClient, error)

// Resolver resolves available versions and the newest version for update
// targets, dispatching images and OCI charts to a container registry and HTTP
// charts to a Helm repository index. Lookups are deduplicated by source identity
// so the same registry/repo or Helm chart is queried only once per run, even
// when many manifests reference it concurrently.
type Resolver struct {
	NewRegistryClient RegistryClientFactory
	HelmIndex         HelmIndexClient

	flight singleflight.Group
	mu     sync.Mutex
	cache  map[string]versionsResult
}

type versionsResult struct {
	versions []string
	err      error
}

// LatestVersions is the newest stable and pre-release semver version published
// by a target source. Empty fields mean that class was not present.
type LatestVersions struct {
	Stable     string
	Prerelease string
}

// NewResolver returns a Resolver wired to live registry-scanner and HTTP Helm
// index clients, authenticating against the local Docker credential store.
func NewResolver() *Resolver {
	creds := NewKeychainResolver()
	return &Resolver{
		NewRegistryClient: liveRegistryClientFactory(creds),
		HelmIndex:         NewHTTPHelmIndexClient(creds),
	}
}

// liveRegistryClientFactory builds a registry client factory that resolves
// credentials for each image's registry host via creds before connecting.
func liveRegistryClientFactory(creds CredentialResolver) RegistryClientFactory {
	return func(ctx context.Context, img *image.ContainerImage) (registry.RegistryClient, error) {
		ep, err := registry.GetRegistryEndpoint(ctx, img)
		if err != nil {
			return nil, err
		}
		user, pass, err := creds.Resolve(ctx, registryHostForImage(img, ep))
		if err != nil {
			return nil, err
		}
		client, err := registry.NewClient(ep, user, pass)
		if err != nil {
			return nil, err
		}
		if err := client.NewRepository(img.ImageName); err != nil {
			return nil, fmt.Errorf("select repository %s: %w", img.ImageName, err)
		}
		return client, nil
	}
}

// registryHostForImage picks the host key used for credential lookup: the
// image's registry URL when set, else the endpoint's prefix (which carries the
// inferred host, e.g. docker.io).
func registryHostForImage(img *image.ContainerImage, ep *registry.RegistryEndpoint) string {
	if img.RegistryURL != "" {
		return img.RegistryURL
	}
	return ep.RegistryPrefix
}

// Available returns every candidate version for a target, sorted newest-first.
func (r *Resolver) Available(ctx context.Context, t UpdateTarget) ([]string, error) {
	versions, err := r.sourceVersions(ctx, t)
	if err != nil {
		return nil, err
	}
	return sortSemverDesc(versions), nil
}

// ResolveLatest returns the highest stable (non-prerelease) version for a target.
func (r *Resolver) ResolveLatest(ctx context.Context, t UpdateTarget) (string, error) {
	latest, err := r.ResolveLatestVersions(ctx, t)
	if err != nil {
		return "", err
	}
	if latest.Stable == "" {
		return "", fmt.Errorf("no stable version found for %s", targetVersionSource(t))
	}
	return latest.Stable, nil
}

// ResolveLatestVersions returns both the highest stable and highest pre-release
// semver versions for a target. It uses the shared source version cache, so a
// caller can ask for both classes without a second registry or Helm lookup.
func (r *Resolver) ResolveLatestVersions(ctx context.Context, t UpdateTarget) (LatestVersions, error) {
	versions, err := r.sourceVersions(ctx, t)
	if err != nil {
		return LatestVersions{}, err
	}
	return latestSemverVersions(versions), nil
}

// sourceVersionKey identifies the upstream a target's versions come from, so two
// manifests referencing the same registry/repo or Helm chart share one lookup.
func sourceVersionKey(t UpdateTarget) string {
	if t.Kind == TargetChart && !t.IsOCI {
		return "helm|" + strings.TrimSuffix(t.RepoURL, "/") + "|" + t.ChartName
	}
	return "oci|" + registryImage(t).GetFullNameWithoutTag()
}

// sourceVersions returns the raw available versions for a target's source,
// deduplicated: concurrent identical lookups collapse to one network call via
// singleflight, and the result (including errors) is cached for the run.
func (r *Resolver) sourceVersions(ctx context.Context, t UpdateTarget) ([]string, error) {
	key := sourceVersionKey(t)

	r.mu.Lock()
	if r.cache != nil {
		if hit, ok := r.cache[key]; ok {
			r.mu.Unlock()
			return hit.versions, hit.err
		}
	}
	r.mu.Unlock()

	v, _, _ := r.flight.Do(key, func() (interface{}, error) {
		versions, err := r.fetchVersions(ctx, t)
		r.mu.Lock()
		if r.cache == nil {
			r.cache = map[string]versionsResult{}
		}
		r.cache[key] = versionsResult{versions: versions, err: err}
		r.mu.Unlock()
		return versionsResult{versions: versions, err: err}, nil
	})
	res := v.(versionsResult)
	return res.versions, res.err
}

// fetchVersions performs the actual upstream query for a target.
func (r *Resolver) fetchVersions(ctx context.Context, t UpdateTarget) ([]string, error) {
	if t.Kind == TargetChart && !t.IsOCI {
		return r.HelmIndex.Versions(ctx, t.RepoURL, t.ChartName)
	}
	img := registryImage(t)
	client, err := r.NewRegistryClient(ctx, img)
	if err != nil {
		return nil, err
	}
	tags, err := client.Tags(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tags for %s: %w", img.GetFullNameWithoutTag(), err)
	}
	return tags, nil
}

// registryImage returns the ContainerImage to query for a target's tags. For an
// apps/v1 image it is the parsed image; for an OCI chart it is built from the
// repository URL (oci://host/path) and chart name, since the chart's current
// value is a version string, not an image reference.
func registryImage(t UpdateTarget) *image.ContainerImage {
	if t.Kind == TargetChart {
		repo := strings.TrimPrefix(t.RepoURL, "oci://")
		repo = strings.TrimSuffix(repo, "/")
		return image.NewFromIdentifier(repo + "/" + t.ChartName)
	}
	if t.Image != nil {
		return t.Image
	}
	return image.NewFromIdentifier(t.CurrentValue)
}

// NewImageValue composes the replacement string for an image target updated to
// newTag. When the current image is digest-pinned, the re-resolve policy fetches
// newTag's digest and writes repo:newtag@sha256:<new>; otherwise it writes
// repo:newtag.
func (r *Resolver) NewImageValue(ctx context.Context, t UpdateTarget, newTag string) (string, error) {
	img := t.Image
	if img == nil {
		img = image.NewFromIdentifier(t.CurrentValue)
	}
	newImageTag := tag.NewImageTag(newTag, time.Time{}, "")
	if img.ImageTag != nil && img.ImageTag.TagDigest != "" {
		digest, err := r.ResolveDigest(ctx, t, newTag)
		if err != nil {
			return "", err
		}
		newImageTag.TagDigest = digest
	}
	return img.WithTag(newImageTag).GetFullNameWithTag(), nil
}

// ResolveDigest returns the registry digest (sha256:...) of newTag for the
// target's image. Used by the re-resolve digest policy so a digest-pinned image
// is rewritten as repo:newtag@sha256:<new>.
func (r *Resolver) ResolveDigest(ctx context.Context, t UpdateTarget, newTag string) (string, error) {
	img := t.Image
	if img == nil {
		img = image.NewFromIdentifier(t.CurrentValue)
	}
	client, err := r.NewRegistryClient(ctx, img)
	if err != nil {
		return "", err
	}
	manifest, err := client.ManifestForTag(ctx, newTag)
	if err != nil {
		return "", fmt.Errorf("manifest for %s:%s: %w", img.GetFullNameWithoutTag(), newTag, err)
	}
	info, err := client.TagMetadata(ctx, manifest, options.NewManifestOptions())
	if err != nil {
		return "", fmt.Errorf("metadata for %s:%s: %w", img.GetFullNameWithoutTag(), newTag, err)
	}
	return info.EncodedDigest(), nil
}

// sortSemverDesc returns versions that parse as semver, newest first. Tags that
// are not valid semver are dropped (they cannot be ordered meaningfully).
func sortSemverDesc(versions []string) []string {
	type parsed struct {
		orig string
		ver  *semver.Version
	}
	var ps []parsed
	for _, v := range versions {
		sv, err := semver.NewVersion(v)
		if err != nil {
			continue
		}
		ps = append(ps, parsed{orig: v, ver: sv})
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].ver.GreaterThan(ps[j].ver) })
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.orig
	}
	return out
}

func latestSemverVersions(versions []string) LatestVersions {
	var latest LatestVersions
	for _, v := range sortSemverDesc(versions) {
		sv, err := semver.NewVersion(v)
		if err != nil {
			continue
		}
		if strings.TrimSpace(sv.Prerelease()) == "" {
			if latest.Stable == "" {
				latest.Stable = v
			}
		} else if latest.Prerelease == "" {
			latest.Prerelease = v
		}
		if latest.Stable != "" && latest.Prerelease != "" {
			return latest
		}
	}
	return latest
}

func targetVersionSource(t UpdateTarget) string {
	if t.Kind == TargetChart && !t.IsOCI {
		return fmt.Sprintf("chart %q in %s", t.ChartName, t.RepoURL)
	}
	return fmt.Sprintf("image %s", registryImage(t).GetFullNameWithoutTag())
}
