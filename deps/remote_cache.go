package deps

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"
)

// Differentiated TTLs (decision 3). Immutable-per-ref artifacts (git clones at a
// ref, chart .tgz, image config by digest) are kept long; mutable lookups
// (helm index.yaml, image tag→config) refresh daily; negatives retry soon.
const (
	ttlImmutable = 365 * 24 * time.Hour
	ttlIndex     = 24 * time.Hour
	ttlNegative  = 1 * time.Hour
)

// ImageConfig is the resolved OCI config metadata for an image reference.
type ImageConfig struct {
	Digest string
	Labels map[string]string
}

// notFoundError marks a definitively-absent remote resource (404, missing repo)
// so it can be negatively cached and degraded to a warning, distinct from a
// transient network failure which is never cached.
type notFoundError struct{ what string }

func (e notFoundError) Error() string { return e.what + ": not found" }

func isNotFound(err error) bool {
	var nf notFoundError
	return errors.As(err, &nf)
}

// labelResolver reads an image's config digest and OCI labels from a registry.
// Production is backed by imageupdate; tests inject a fake.
type labelResolver interface {
	Labels(ctx context.Context, ref string) (digest string, labels map[string]string, err error)
}

// RemoteCache provides cached access to the remote artifacts recursion needs:
// arbitrary HTTP blobs (helm index/chart), git checkouts, and image configs.
type RemoteCache interface {
	Fetch(ctx context.Context, url string, ttl time.Duration) ([]byte, error)
	GitRepo(ctx context.Context, url, ref string) (dir string, err error)
	ImageConfig(ctx context.Context, ref string) (ImageConfig, error)
}

type diskCache struct {
	root   string
	now    func() time.Time
	get    func(ctx context.Context, url string) ([]byte, error)
	runner CommandRunner
	labels labelResolver
	group  singleflight.Group
}

// newDiskCache builds the production cache rooted at os.UserCacheDir()/repomap.
func newDiskCache(now func() time.Time, runner CommandRunner, labels labelResolver) (*diskCache, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("locating user cache dir: %w", err)
	}
	root := filepath.Join(base, "repomap", "deps")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("creating cache dir %s: %w", root, err)
	}
	c := &diskCache{root: root, now: now, runner: runner, labels: labels}
	c.get = c.httpGet
	return c, nil
}

type cacheEntry[T any] struct {
	FetchedAt time.Time
	NotFound  bool
	Value     T
}

func (c *diskCache) Fetch(ctx context.Context, url string, ttl time.Duration) ([]byte, error) {
	v, err, _ := c.group.Do("blob:"+url, func() (any, error) {
		path := c.entryPath("blobs", url)
		if e, ok := readEntry[[]byte](path); ok && !c.expiredAt(e.FetchedAt, e.NotFound, ttl) {
			if e.NotFound {
				return nil, notFoundError{url}
			}
			return e.Value, nil
		}
		data, err := c.get(ctx, url)
		if isNotFound(err) {
			writeEntry(path, cacheEntry[[]byte]{FetchedAt: c.now(), NotFound: true})
			return nil, notFoundError{url}
		}
		if err != nil {
			return nil, err // transient: do not cache
		}
		writeEntry(path, cacheEntry[[]byte]{FetchedAt: c.now(), Value: data})
		return data, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]byte), nil
}

func (c *diskCache) ImageConfig(ctx context.Context, ref string) (ImageConfig, error) {
	v, err, _ := c.group.Do("img:"+ref, func() (any, error) {
		path := c.entryPath("imageconfig", ref)
		if e, ok := readEntry[ImageConfig](path); ok && !c.expiredAt(e.FetchedAt, e.NotFound, ttlIndex) {
			if e.NotFound {
				return ImageConfig{}, notFoundError{ref}
			}
			return e.Value, nil
		}
		digest, labels, err := c.labels.Labels(ctx, ref)
		if isNotFound(err) {
			writeEntry(path, cacheEntry[ImageConfig]{FetchedAt: c.now(), NotFound: true})
			return ImageConfig{}, notFoundError{ref}
		}
		if err != nil {
			return ImageConfig{}, err
		}
		cfg := ImageConfig{Digest: digest, Labels: labels}
		writeEntry(path, cacheEntry[ImageConfig]{FetchedAt: c.now(), Value: cfg})
		return cfg, nil
	})
	if err != nil {
		return ImageConfig{}, err
	}
	return v.(ImageConfig), nil
}

// GitRepo clones url at ref into the cache (kept for ttlImmutable) and returns
// the checkout directory. A ref that is not a branch/tag falls back to the
// default branch.
func (c *diskCache) GitRepo(ctx context.Context, url, ref string) (string, error) {
	v, err, _ := c.group.Do("git:"+url+"@"+ref, func() (any, error) {
		dir := filepath.Join(c.root, "git", hashKey(url), sanitizeRef(ref))
		marker := filepath.Join(dir, ".repomap-fetched")
		if data, err := os.ReadFile(marker); err == nil {
			if at, perr := time.Parse(time.RFC3339, strings.TrimSpace(string(data))); perr == nil &&
				c.now().Sub(at) < ttlImmutable {
				return dir, nil
			}
		}
		if err := c.cloneInto(ctx, url, ref, dir); err != nil {
			return "", err
		}
		_ = os.WriteFile(marker, []byte(c.now().Format(time.RFC3339)), 0o644)
		return dir, nil
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

func (c *diskCache) cloneInto(ctx context.Context, url, ref, dir string) error {
	tmp, err := os.MkdirTemp(c.root, "clone-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	args := []string{"clone", "--depth", "1", "--single-branch", url, tmp}
	if ref != "" {
		args = []string{"clone", "--depth", "1", "--single-branch", "--branch", ref, url, tmp}
	}
	if _, err := c.runner.Run(ctx, Command{Name: "git", Args: args}); err != nil {
		// Ref may not be a branch/tag; fall back to the default branch.
		if ref == "" {
			return notFoundError{url}
		}
		if _, err2 := c.runner.Run(ctx, Command{Name: "git", Args: []string{"clone", "--depth", "1", url, tmp}}); err2 != nil {
			return notFoundError{url}
		}
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	_ = os.RemoveAll(dir)
	return os.Rename(tmp, dir)
}

func (c *diskCache) httpGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, notFoundError{url}
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func (c *diskCache) expiredAt(at time.Time, notFound bool, ttl time.Duration) bool {
	if notFound {
		ttl = ttlNegative
	}
	return c.now().Sub(at) >= ttl
}

func (c *diskCache) entryPath(kind, key string) string {
	return filepath.Join(c.root, kind, hashKey(key)+".gob")
}

func hashKey(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func sanitizeRef(ref string) string {
	if ref == "" {
		return "_default"
	}
	return strings.NewReplacer("/", "_", ":", "_", " ", "_").Replace(ref)
}

func writeEntry[T any](path string, e cacheEntry[T]) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(e); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

func readEntry[T any](path string) (cacheEntry[T], bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return cacheEntry[T]{}, false
	}
	var e cacheEntry[T]
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&e); err != nil {
		return cacheEntry[T]{}, false
	}
	return e, true
}
