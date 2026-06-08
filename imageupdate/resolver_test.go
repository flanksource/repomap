package imageupdate

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/argoproj-labs/argocd-image-updater/registry-scanner/pkg/image"
	"github.com/argoproj-labs/argocd-image-updater/registry-scanner/pkg/registry"
	"github.com/argoproj-labs/argocd-image-updater/registry-scanner/pkg/registry/mocks"
	"github.com/argoproj-labs/argocd-image-updater/registry-scanner/pkg/tag"
	"github.com/stretchr/testify/mock"
)

func mockRegistryResolver(tags []string) *Resolver {
	return &Resolver{
		NewRegistryClient: func(ctx context.Context, img *image.ContainerImage) (registry.RegistryClient, error) {
			m := &mocks.RegistryClient{}
			m.On("Tags", mock.Anything).Return(tags, nil)
			return m, nil
		},
	}
}

type fakeHelmIndex struct{ versions []string }

func (f fakeHelmIndex) Versions(ctx context.Context, repoURL, chart string) ([]string, error) {
	return f.versions, nil
}

func TestResolver_DedupesSharedSource(t *testing.T) {
	var tagsCalls, factoryCalls int32
	r := &Resolver{
		NewRegistryClient: func(ctx context.Context, img *image.ContainerImage) (registry.RegistryClient, error) {
			atomic.AddInt32(&factoryCalls, 1)
			m := &mocks.RegistryClient{}
			m.On("Tags", mock.Anything).Run(func(mock.Arguments) {
				atomic.AddInt32(&tagsCalls, 1)
			}).Return([]string{"1.0.0", "1.1.0"}, nil)
			return m, nil
		},
	}

	// Same image (nginx) referenced by two targets in different files.
	mk := func(file string) UpdateTarget {
		return UpdateTarget{Kind: TargetImage, File: file, CurrentValue: "nginx:1.0.0", Image: image.NewFromIdentifier("nginx:1.0.0")}
	}
	for _, f := range []string{"a.yaml", "b.yaml", "a.yaml"} {
		if _, err := r.Available(context.Background(), mk(f)); err != nil {
			t.Fatal(err)
		}
	}
	if got := atomic.LoadInt32(&tagsCalls); got != 1 {
		t.Errorf("Tags() called %d times, want 1 (deduped)", got)
	}
	if got := atomic.LoadInt32(&factoryCalls); got != 1 {
		t.Errorf("registry client built %d times, want 1", got)
	}
}

func TestResolver_DistinctSourcesNotDeduped(t *testing.T) {
	var tagsCalls int32
	r := &Resolver{
		NewRegistryClient: func(ctx context.Context, img *image.ContainerImage) (registry.RegistryClient, error) {
			m := &mocks.RegistryClient{}
			m.On("Tags", mock.Anything).Run(func(mock.Arguments) {
				atomic.AddInt32(&tagsCalls, 1)
			}).Return([]string{"1.0.0"}, nil)
			return m, nil
		},
	}
	a := UpdateTarget{Kind: TargetImage, CurrentValue: "nginx:1.0.0", Image: image.NewFromIdentifier("nginx:1.0.0")}
	b := UpdateTarget{Kind: TargetImage, CurrentValue: "redis:1.0.0", Image: image.NewFromIdentifier("redis:1.0.0")}
	_, _ = r.Available(context.Background(), a)
	_, _ = r.Available(context.Background(), b)
	if got := atomic.LoadInt32(&tagsCalls); got != 2 {
		t.Errorf("Tags() called %d times, want 2 (distinct sources)", got)
	}
}

func TestRegistryImage_OCIChartUsesRepoAndChart(t *testing.T) {
	// An OCI chart's CurrentValue is a version string; the registry image must be
	// built from RepoURL + ChartName, not the version.
	tg := UpdateTarget{
		Kind:         TargetChart,
		IsOCI:        true,
		RepoURL:      "oci://registry.example.com/charts",
		ChartName:    "sybrin",
		CurrentValue: "v1.0.51",
	}
	img := registryImage(tg)
	if got := img.GetFullNameWithoutTag(); got != "registry.example.com/charts/sybrin" {
		t.Errorf("OCI chart image = %q", got)
	}
	if img.RegistryURL != "registry.example.com" {
		t.Errorf("registry host = %q", img.RegistryURL)
	}
}

func TestRegistryImage_AppsV1UsesParsedImage(t *testing.T) {
	tg := UpdateTarget{
		Kind:         TargetImage,
		CurrentValue: "nginx:1.25.3",
		Image:        image.NewFromIdentifier("nginx:1.25.3"),
	}
	if got := registryImage(tg).GetFullNameWithoutTag(); got != "nginx" {
		t.Errorf("image = %q, want nginx", got)
	}
}

func TestResolver_AvailableImage_SortedDesc(t *testing.T) {
	r := mockRegistryResolver([]string{"1.25.3", "1.27.0", "latest", "1.26.1", "1.27.0-rc.1"})
	tg := UpdateTarget{Kind: TargetImage, CurrentValue: "nginx:1.25.3", Image: image.NewFromIdentifier("nginx:1.25.3")}
	got, err := r.Available(context.Background(), tg)
	if err != nil {
		t.Fatal(err)
	}
	// "latest" is dropped (not semver); rc sorts below its release.
	want := []string{"1.27.0", "1.27.0-rc.1", "1.26.1", "1.25.3"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d]=%q want %q", i, got[i], want[i])
		}
	}
}

func TestResolver_LatestImage_ExcludesPrerelease(t *testing.T) {
	r := mockRegistryResolver([]string{"1.25.3", "1.27.0", "1.28.0-beta.1", "1.26.1"})
	tg := UpdateTarget{Kind: TargetImage, CurrentValue: "nginx:1.25.3", Image: image.NewFromIdentifier("nginx:1.25.3")}
	latest, err := r.ResolveLatest(context.Background(), tg)
	if err != nil {
		t.Fatal(err)
	}
	if latest != "1.27.0" {
		t.Errorf("latest = %q, want 1.27.0 (1.28.0-beta.1 excluded)", latest)
	}
}

func TestResolver_LatestChart_HTTP(t *testing.T) {
	r := &Resolver{HelmIndex: fakeHelmIndex{versions: []string{"6.5.0", "6.6.0", "6.7.0-rc.1", "6.5.4"}}}
	tg := UpdateTarget{Kind: TargetChart, ChartName: "podinfo", RepoURL: "https://example.com"}
	latest, err := r.ResolveLatest(context.Background(), tg)
	if err != nil {
		t.Fatal(err)
	}
	if latest != "6.6.0" {
		t.Errorf("latest = %q, want 6.6.0", latest)
	}
}

func TestNewImageValue_PlainTagDropsNothing(t *testing.T) {
	r := &Resolver{}
	tg := UpdateTarget{Kind: TargetImage, CurrentValue: "nginx:1.25.3", Image: image.NewFromIdentifier("nginx:1.25.3")}
	got, err := r.NewImageValue(context.Background(), tg, "1.27.0")
	if err != nil {
		t.Fatal(err)
	}
	if got != "nginx:1.27.0" {
		t.Errorf("got %q, want nginx:1.27.0", got)
	}
}

func TestNewImageValue_DigestPinnedReResolves(t *testing.T) {
	var want [32]byte
	for i := range want {
		want[i] = 0xab
	}
	r := &Resolver{
		NewRegistryClient: func(ctx context.Context, img *image.ContainerImage) (registry.RegistryClient, error) {
			m := &mocks.RegistryClient{}
			m.On("ManifestForTag", mock.Anything, "15.5").Return(nil, nil)
			m.On("TagMetadata", mock.Anything, mock.Anything, mock.Anything).
				Return(&tag.TagInfo{Digest: want}, nil)
			return m, nil
		},
	}
	cur := "registry.k8s.io/postgres:15.4@sha256:1eeb4c7316bacb1d4c8ead65571cd92dd21e27359f0d4917f1a5822a73b75db1"
	tg := UpdateTarget{Kind: TargetImage, CurrentValue: cur, Image: image.NewFromIdentifier(cur)}
	got, err := r.NewImageValue(context.Background(), tg, "15.5")
	if err != nil {
		t.Fatal(err)
	}
	wantStr := "registry.k8s.io/postgres:15.5@sha256:abababababababababababababababababababababababababababababababab"
	if got != wantStr {
		t.Errorf("got %q\nwant %q", got, wantStr)
	}
}

func TestResolver_AvailableChart_HTTP(t *testing.T) {
	r := &Resolver{HelmIndex: fakeHelmIndex{versions: []string{"6.5.0", "6.6.0", "6.5.4"}}}
	tg := UpdateTarget{Kind: TargetChart, ChartName: "podinfo", RepoURL: "https://example.com"}
	got, err := r.Available(context.Background(), tg)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"6.6.0", "6.5.4", "6.5.0"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d]=%q want %q", i, got[i], want[i])
		}
	}
}
