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

	infos, err := buildImageInfos(context.Background(), nil, targets, imageupdate.NewSourceIndex(nil), false)
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
	if info.Checked || info.Latest != "" {
		t.Errorf("offline row should not be checked or have latest: %+v", info)
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

	resolver := fakeImageResolver([]string{"1.25.3", "1.27.0"})
	infos, err := buildImageInfos(context.Background(), resolver, targets, imageupdate.NewSourceIndex(nil), true)
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
	if !info.UpdateAvailable {
		t.Error("expected update available (1.25.3 -> 1.27.0)")
	}
	if len(info.Columns()) != 6 {
		t.Errorf("checked columns = %d, want 6", len(info.Columns()))
	}
}

func TestBuildImageInfos_CheckNoUpdateWhenCurrentIsLatest(t *testing.T) {
	conf, rel := writeRepo(t)
	content, _ := os.ReadFile(filepath.Join(conf.RepoPath(), rel))
	targets, _ := imageupdate.ExtractTargets(rel, string(content))

	resolver := fakeImageResolver([]string{"1.25.3", "1.24.0"})
	infos, _ := buildImageInfos(context.Background(), resolver, targets, imageupdate.NewSourceIndex(nil), true)
	if infos[0].UpdateAvailable {
		t.Errorf("no update expected when 1.25.3 is already newest; got latest=%q", infos[0].Latest)
	}
}
