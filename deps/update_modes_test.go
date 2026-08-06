package deps

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const chartRefOCIFixture = `apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: podinfo
  namespace: apps
spec:
  chartRef:
    kind: OCIRepository
    name: podinfo
---
apiVersion: source.toolkit.fluxcd.io/v1beta2
kind: OCIRepository
metadata:
  name: podinfo
  namespace: apps
spec:
  url: oci://ghcr.io/stefanprodan/charts/podinfo
  ref:
    tag: 6.5.0
`

func setupImageRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	runGit(t, dir, "init")
	for rel, content := range files {
		writeFile(t, filepath.Join(dir, rel), content)
	}
	runGit(t, dir, "add", ".")
	return dir
}

func TestUpdate_LatestSelectsHighestStable(t *testing.T) {
	setupImageRepo(t, map[string]string{"apps/workloads.yaml": deploymentUpdateFixture})

	plans, err := Update(context.Background(), ".", UpdateOptions{
		Managers: []Manager{ManagerImage},
		Latest:   true,
		DryRun:   true,
		ImageResolver: fakeImageVersionResolver{
			"nginx":                     {"1.28.0-rc.1", "1.27.0", "1.25.3"},
			"ghcr.io/flanksource/proxy": {"v0.4.1"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	nginx := findPlan(plans, "nginx")
	if nginx == nil || nginx.NewVersion != "1.27.0" || !nginx.DryRun || nginx.Written {
		t.Fatalf("nginx plan = %#v, want 1.27.0 dry-run (skipping the rc prerelease)", nginx)
	}
	proxy := findPlan(plans, "ghcr.io/flanksource/proxy")
	if proxy == nil || proxy.Skipped != "already up to date" {
		t.Fatalf("proxy plan = %#v, want skipped already up to date", proxy)
	}
}

func TestUpdate_ExplicitVersionWritesEdit(t *testing.T) {
	dir := setupImageRepo(t, map[string]string{"apps/workloads.yaml": deploymentUpdateFixture})

	plans, err := Update(context.Background(), ".", UpdateOptions{
		Managers:      []Manager{ManagerImage},
		Image:         []string{"nginx"},
		Version:       "1.27.0",
		ImageResolver: fakeImageVersionResolver{"nginx": {"1.27.0", "1.25.3"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 {
		t.Fatalf("plans = %#v, want only the nginx candidate (image filter)", plans)
	}
	if !plans[0].Written || plans[0].NewVersion != "1.27.0" {
		t.Fatalf("plan = %#v, want written 1.27.0", plans[0])
	}
	got := readFileString(t, filepath.Join(dir, "apps", "workloads.yaml"))
	if !strings.Contains(got, "image: nginx:1.27.0") {
		t.Fatalf("manifest not updated:\n%s", got)
	}
	if !strings.Contains(got, "image: ghcr.io/flanksource/proxy:v0.4.1") {
		t.Fatalf("image filter must leave the sidecar untouched:\n%s", got)
	}
}

func TestUpdate_ExplicitVersionRejectsUnavailable(t *testing.T) {
	setupImageRepo(t, map[string]string{"apps/workloads.yaml": deploymentUpdateFixture})

	plans, err := Update(context.Background(), ".", UpdateOptions{
		Managers:      []Manager{ManagerImage},
		Image:         []string{"nginx"},
		Version:       "9.9.9",
		ImageResolver: fakeImageVersionResolver{"nginx": {"1.27.0", "1.25.3"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || plans[0].Written {
		t.Fatalf("plans = %#v, want one unwritten plan", plans)
	}
	if !strings.Contains(plans[0].Skipped, "not available") {
		t.Fatalf("skip = %q, want 'not available'", plans[0].Skipped)
	}
}

func TestUpdate_ResourceFilterKindNarrowsToHelm(t *testing.T) {
	setupImageRepo(t, map[string]string{
		"apps/workloads.yaml":   deploymentUpdateFixture,
		"apps/helmrelease.yaml": helmReleaseUpdateFixture,
	})

	plans, err := Update(context.Background(), ".", UpdateOptions{
		Managers:      []Manager{ManagerImage, ManagerHelm},
		Kind:          []string{"HelmRelease"},
		Check:         true,
		ImageResolver: fakeImageVersionResolver{"podinfo": {"6.6.0", "6.5.0"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || plans[0].Manager != ManagerHelm || plans[0].Name != "podinfo" {
		t.Fatalf("plans = %#v, want only the HelmRelease chart", plans)
	}
	if plans[0].NewVersion != "6.6.0" || !plans[0].Checked {
		t.Fatalf("plan = %#v, want checked 6.6.0", plans[0])
	}
}

func TestUpdate_DedupesVersionLookupsAcrossDuplicates(t *testing.T) {
	setupImageRepo(t, map[string]string{
		"apps/a.yaml": deploymentUpdateFixture,
		"apps/b.yaml": deploymentUpdateFixture, // the same nginx + proxy in a second file
	})
	resolver := newCountingImageVersionResolver(map[string][]string{
		"nginx":                     {"1.27.0", "1.25.3"},
		"ghcr.io/flanksource/proxy": {"v0.5.0", "v0.4.1"},
	})

	plans, err := Update(context.Background(), ".", UpdateOptions{
		Managers:      []Manager{ManagerImage},
		Check:         true,
		ImageResolver: resolver,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Two images each appear in two files, so four updates are planned but each
	// image's published versions must be looked up exactly once.
	if len(plans) != 4 {
		t.Fatalf("plans = %d, want 4 (two images × two files): %#v", len(plans), plans)
	}
	if got := resolver.callCount("nginx"); got != 1 {
		t.Fatalf("nginx version lookups = %d, want 1 deduped lookup", got)
	}
	if got := resolver.callCount("ghcr.io/flanksource/proxy"); got != 1 {
		t.Fatalf("proxy version lookups = %d, want 1 deduped lookup", got)
	}
}

func TestUpdate_FilterThenChecksOnlyFilteredSet(t *testing.T) {
	setupImageRepo(t, map[string]string{
		"apps/workloads.yaml":   deploymentUpdateFixture,
		"apps/helmrelease.yaml": helmReleaseUpdateFixture,
	})
	resolver := newCountingImageVersionResolver(map[string][]string{
		"nginx":                     {"1.27.0", "1.25.3"},
		"ghcr.io/flanksource/proxy": {"v0.5.0", "v0.4.1"},
		"podinfo":                   {"6.6.0", "6.5.0"},
	})

	plans, err := Update(context.Background(), ".", UpdateOptions{
		Managers:      []Manager{ManagerImage, ManagerHelm},
		Kind:          []string{"HelmRelease"},
		Check:         true,
		ImageResolver: resolver,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || plans[0].Manager != ManagerHelm || plans[0].Name != "podinfo" {
		t.Fatalf("plans = %#v, want only the filtered HelmRelease chart", plans)
	}
	// The kind filter must restrict the lookups: Deployment images are never queried.
	if got := resolver.callCount("nginx") + resolver.callCount("ghcr.io/flanksource/proxy"); got != 0 {
		t.Fatalf("filtered-out image lookups = %d, want 0 (filter before check)", got)
	}
	if got := resolver.callCount("podinfo"); got != 1 {
		t.Fatalf("podinfo lookups = %d, want 1", got)
	}
}

func TestUpdate_NoFiltersMatchesAll(t *testing.T) {
	setupImageRepo(t, map[string]string{"apps/helmrelease.yaml": helmReleaseUpdateFixture})

	plans, err := Update(context.Background(), ".", UpdateOptions{
		Managers:      []Manager{ManagerHelm},
		Check:         true,
		ImageResolver: fakeImageVersionResolver{"podinfo": {"6.6.0", "6.5.0"}},
	})
	if err != nil {
		t.Fatalf("empty expression should match all, got %v", err)
	}
	if len(plans) != 1 || plans[0].Name != "podinfo" {
		t.Fatalf("plans = %#v, want podinfo", plans)
	}
}

func TestDiscoverUpdateCandidates_ChartRefOCI(t *testing.T) {
	setupImageRepo(t, map[string]string{"clusters/app.yaml": chartRefOCIFixture})

	got, err := DiscoverUpdateCandidates(".", []Manager{ManagerHelm}, DiscoverFilter{})
	if err != nil {
		t.Fatal(err)
	}
	helm := findUpdateCandidate(got, ManagerHelm, "podinfo")
	if helm == nil {
		t.Fatalf("chartRef OCIRepository not discovered: %#v", got)
	}
	if helm.Current != "6.5.0" {
		t.Errorf("current = %q, want 6.5.0 (from OCIRepository spec.ref.tag)", helm.Current)
	}
	if helm.Target == nil || helm.Target.RepoURL != "oci://ghcr.io/stefanprodan/charts" || !helm.Target.IsOCI {
		t.Errorf("target repo not resolved: %#v", helm.Target)
	}
	// The edit anchor must be the OCIRepository file, not the HelmRelease.
	if helm.Target.File != "clusters/app.yaml" || helm.Target.FieldJSONPath != "$.spec.ref.tag" {
		t.Errorf("anchor = %s %s, want clusters/app.yaml $.spec.ref.tag", helm.Target.File, helm.Target.FieldJSONPath)
	}
}

func findPlan(plans []UpdatePlan, name string) *UpdatePlan {
	for i := range plans {
		if plans[i].Name == name {
			return &plans[i]
		}
	}
	return nil
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
