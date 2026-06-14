package deps

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testClock(t time.Time) (func() time.Time, *time.Time) {
	cur := t
	return func() time.Time { return cur }, &cur
}

func newTestCache(t *testing.T, now func() time.Time) *diskCache {
	t.Helper()
	return &diskCache{root: t.TempDir(), now: now}
}

func TestCacheFetchHitAndTTL(t *testing.T) {
	now, clock := testClock(time.Unix(1000, 0))
	c := newTestCache(t, now)
	var calls int64
	c.get = func(_ context.Context, url string) ([]byte, error) {
		atomic.AddInt64(&calls, 1)
		return []byte("payload"), nil
	}

	for i := 0; i < 3; i++ {
		if _, err := c.Fetch(context.Background(), "http://x/index.yaml", ttlIndex); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("expected 1 remote fetch within TTL, got %d", calls)
	}

	*clock = clock.Add(ttlIndex + time.Minute) // expire
	if _, err := c.Fetch(context.Background(), "http://x/index.yaml", ttlIndex); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("expected re-fetch after TTL expiry, got %d calls", calls)
	}
}

func TestCacheNegativeCaching(t *testing.T) {
	now, clock := testClock(time.Unix(1000, 0))
	c := newTestCache(t, now)
	var calls int64
	c.get = func(_ context.Context, url string) ([]byte, error) {
		atomic.AddInt64(&calls, 1)
		return nil, notFoundError{url}
	}

	if _, err := c.Fetch(context.Background(), "http://x/missing", ttlImmutable); !isNotFound(err) {
		t.Fatalf("want notFound, got %v", err)
	}
	if _, err := c.Fetch(context.Background(), "http://x/missing", ttlImmutable); !isNotFound(err) {
		t.Fatalf("want cached notFound, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("negative result should be cached (1 call), got %d", calls)
	}

	*clock = clock.Add(ttlNegative + time.Minute) // negatives expire on the short TTL
	if _, err := c.Fetch(context.Background(), "http://x/missing", ttlImmutable); !isNotFound(err) {
		t.Fatalf("want notFound after negative expiry, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("negative entry should retry after ttlNegative, got %d calls", calls)
	}
}

func TestCacheTransientErrorNotCached(t *testing.T) {
	now, _ := testClock(time.Unix(1000, 0))
	c := newTestCache(t, now)
	var calls int64
	c.get = func(_ context.Context, url string) ([]byte, error) {
		atomic.AddInt64(&calls, 1)
		return nil, context.DeadlineExceeded
	}
	for i := 0; i < 2; i++ {
		if _, err := c.Fetch(context.Background(), "http://x/flaky", ttlIndex); err == nil {
			t.Fatal("expected transient error")
		}
	}
	if calls != 2 {
		t.Fatalf("transient errors must not be cached, want 2 calls got %d", calls)
	}
}

func TestCacheSingleflightCollapsesConcurrentFetches(t *testing.T) {
	now, _ := testClock(time.Unix(1000, 0))
	c := newTestCache(t, now)
	var calls int64
	release := make(chan struct{})
	c.get = func(_ context.Context, url string) ([]byte, error) {
		atomic.AddInt64(&calls, 1)
		<-release // hold so all goroutines pile onto the same in-flight call
		return []byte("v"), nil
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = c.Fetch(context.Background(), "http://x/same", ttlIndex)
		}()
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if calls != 1 {
		t.Fatalf("singleflight should collapse concurrent fetches to 1, got %d", calls)
	}
}

func TestCacheImageConfig(t *testing.T) {
	now, _ := testClock(time.Unix(1000, 0))
	c := newTestCache(t, now)
	var calls int64
	c.labels = fakeLabels{fn: func(ref string) (string, map[string]string, error) {
		atomic.AddInt64(&calls, 1)
		return "sha256:deadbeef", map[string]string{"org.opencontainers.image.source": "https://github.com/acme/app"}, nil
	}}

	cfg, err := c.ImageConfig(context.Background(), "ghcr.io/acme/app:1.0")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Digest != "sha256:deadbeef" || cfg.Labels["org.opencontainers.image.source"] != "https://github.com/acme/app" {
		t.Fatalf("unexpected config %#v", cfg)
	}
	if _, err := c.ImageConfig(context.Background(), "ghcr.io/acme/app:1.0"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("image config should be cached, got %d calls", calls)
	}
}

type fakeLabels struct {
	fn func(ref string) (string, map[string]string, error)
}

func (f fakeLabels) Labels(_ context.Context, ref string) (string, map[string]string, error) {
	return f.fn(ref)
}
