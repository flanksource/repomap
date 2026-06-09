package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/flanksource/repomap/imageupdate"
)

func TestBuildImageInfos_OfflineHasNoLatest(t *testing.T) {
	conf, rel := writeRepo(t)
	content, _ := os.ReadFile(filepath.Join(conf.RepoPath(), rel))
	targets, _ := imageupdate.ExtractTargets(rel, string(content))

	infos, err := buildImageInfos(context.Background(), nil, targets, imageupdate.NewSourceIndex(nil), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 {
		t.Fatalf("want 1 info, got %d", len(infos))
	}
	info := infos[0]
	if info.Current != "nginx:1.25.3" {
		t.Errorf("current = %q, want nginx:1.25.3", info.Current)
	}
	if info.Checked || info.Latest != "" || info.LatestPrerelease != "" {
		t.Errorf("offline row should not be checked or have latest versions: %+v", info)
	}
	// offline Columns omit latest/update
	if len(info.Columns()) != 4 {
		t.Errorf("offline columns = %d, want 4", len(info.Columns()))
	}
}

func TestBuildImageInfos_CheckFlagsUpdateAvailable(t *testing.T) {
	conf, rel := writeRepo(t)
	content, _ := os.ReadFile(filepath.Join(conf.RepoPath(), rel))
	targets, _ := imageupdate.ExtractTargets(rel, string(content))

	resolver := fakeImageResolver([]string{"1.25.3", "1.27.0", "1.28.0-beta.1"})
	infos, err := buildImageInfos(context.Background(), resolver, targets, imageupdate.NewSourceIndex(nil), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	info := infos[0]
	if !info.Checked {
		t.Fatal("expected checked")
	}
	if info.Latest != "1.27.0" {
		t.Errorf("latest = %q, want 1.27.0", info.Latest)
	}
	if info.LatestPrerelease != "1.28.0-beta.1" {
		t.Errorf("latest prerelease = %q, want 1.28.0-beta.1", info.LatestPrerelease)
	}
	if !info.UpdateAvailable {
		t.Error("expected update available (1.25.3 -> 1.27.0)")
	}
	if !info.PrereleaseUpdateAvailable {
		t.Error("expected prerelease update available (1.25.3 -> 1.28.0-beta.1)")
	}
	if len(info.Columns()) != 8 {
		t.Errorf("checked columns = %d, want 8", len(info.Columns()))
	}
}

func TestBuildImageInfos_CheckNoUpdateWhenCurrentIsLatest(t *testing.T) {
	conf, rel := writeRepo(t)
	content, _ := os.ReadFile(filepath.Join(conf.RepoPath(), rel))
	targets, _ := imageupdate.ExtractTargets(rel, string(content))

	resolver := fakeImageResolver([]string{"1.25.3", "1.24.0"})
	infos, _ := buildImageInfos(context.Background(), resolver, targets, imageupdate.NewSourceIndex(nil), true, nil)
	if infos[0].UpdateAvailable {
		t.Errorf("no update expected when 1.25.3 is already newest; got latest=%q", infos[0].Latest)
	}
}

func TestBuildImageInfos_CheckDoesNotFlagOlderPrerelease(t *testing.T) {
	conf, rel := writeRepo(t)
	content, _ := os.ReadFile(filepath.Join(conf.RepoPath(), rel))
	targets, _ := imageupdate.ExtractTargets(rel, string(content))

	resolver := fakeImageResolver([]string{"1.25.3", "1.24.0-rc.1"})
	infos, err := buildImageInfos(context.Background(), resolver, targets, imageupdate.NewSourceIndex(nil), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	info := infos[0]
	if info.LatestPrerelease != "1.24.0-rc.1" {
		t.Errorf("latest prerelease = %q, want 1.24.0-rc.1", info.LatestPrerelease)
	}
	if info.PrereleaseUpdateAvailable {
		t.Errorf("older prerelease should not be an available update: %+v", info)
	}
}

func TestDisplayPathForRepoFileRelativeToWorkingDir(t *testing.T) {
	conf, _ := writeRepo(t)
	oldWorkingDir := workingDir
	t.Cleanup(func() { workingDir = oldWorkingDir })

	workingDir = filepath.Join(conf.RepoPath(), "apps")
	if err := os.MkdirAll(workingDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if got := displayPathForRepoFile(conf, "apps/api/deploy.yaml"); got != "api/deploy.yaml" {
		t.Errorf("inside cwd = %q, want api/deploy.yaml", got)
	}
	if got := displayPathForRepoFile(conf, "clusters/prod/deploy.yaml"); got != "../clusters/prod/deploy.yaml" {
		t.Errorf("outside cwd = %q, want ../clusters/prod/deploy.yaml", got)
	}
}
