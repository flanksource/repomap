package main

import (
	"strings"
	"testing"

	depgraph "github.com/flanksource/repomap/deps"
)

func TestParseManagers(t *testing.T) {
	got, err := parseManagers([]string{"go,npm", "pnpm", "image", "docker", "helm"})
	if err != nil {
		t.Fatal(err)
	}
	want := []depgraph.Manager{
		depgraph.ManagerGo,
		depgraph.ManagerNPM,
		depgraph.ManagerPNPM,
		depgraph.ManagerImage,
		depgraph.ManagerImage,
		depgraph.ManagerHelm,
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("manager[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if _, err := parseManagers([]string{"ruby"}); err == nil {
		t.Fatal("expected unsupported manager error")
	}
}

func TestParseUpdateManagers(t *testing.T) {
	got, err := parseUpdateManagers([]string{"go,image", "docker", "helm"})
	if err != nil {
		t.Fatal(err)
	}
	want := []depgraph.Manager{depgraph.ManagerGo, depgraph.ManagerImage, depgraph.ManagerImage, depgraph.ManagerHelm}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("manager[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if _, err := parseUpdateManagers([]string{"maven"}); err == nil {
		t.Fatal("expected unsupported update manager error")
	}
}

func TestDepsNativeResolutionFlagsRemoved(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"deps"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"mode", "configuration", "strict"} {
		if flag := cmd.Flags().Lookup(name); flag != nil {
			t.Fatalf("%s flag should be removed from deps listing", name)
		}
	}
}

func TestDepsDepthDefault(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"deps"})
	if err != nil {
		t.Fatal(err)
	}
	flag := cmd.Flags().Lookup("depth")
	if flag == nil {
		t.Fatal("depth flag not registered")
	}
	if flag.DefValue != "1" {
		t.Fatalf("depth default = %q, want 1", flag.DefValue)
	}
	if !strings.Contains(flag.Usage, "0 = unlimited") {
		t.Fatalf("depth help should document unlimited mode, got %q", flag.Usage)
	}
}

func TestDepsFlatAndIncludeIndirectFlags(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"deps"})
	if err != nil {
		t.Fatal(err)
	}
	if flag := cmd.Flags().Lookup("flat"); flag == nil {
		t.Fatal("flat flag not registered")
	}
	if flag := cmd.Flags().Lookup("include-indirect"); flag == nil {
		t.Fatal("include-indirect flag not registered")
	}
}

func TestDepsUpdateCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"deps", "update", "github.com/flanksource/*"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd == nil || !strings.HasPrefix(cmd.Use, "update") {
		t.Fatalf("expected deps update command, got %#v", cmd)
	}
	if flag := cmd.Flags().Lookup("dry-run"); flag == nil {
		t.Fatal("dry-run flag not registered")
	}
	if flag := cmd.Flags().Lookup("check"); flag == nil {
		t.Fatal("check flag not registered")
	}
	manager := cmd.Flags().Lookup("manager")
	if manager == nil {
		t.Fatal("manager flag not registered")
	}
	if !strings.Contains(manager.Usage, "go, npm, pnpm, image/docker, helm") {
		t.Fatalf("manager help should document update-supported managers, got %q", manager.Usage)
	}
}
