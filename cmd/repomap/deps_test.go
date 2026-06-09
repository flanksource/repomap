package main

import (
	"strings"
	"testing"

	depgraph "github.com/flanksource/repomap/deps"
)

func TestParseManagers(t *testing.T) {
	got, err := parseManagers([]string{"go,npm", "pnpm"})
	if err != nil {
		t.Fatal(err)
	}
	want := []depgraph.Manager{depgraph.ManagerGo, depgraph.ManagerNPM, depgraph.ManagerPNPM}
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

func TestParseDepsMode(t *testing.T) {
	for _, mode := range []string{"", "auto", "native", "manifest"} {
		if _, err := parseDepsMode(mode); err != nil {
			t.Fatalf("parseDepsMode(%q): %v", mode, err)
		}
	}
	if _, err := parseDepsMode("lockfile"); err == nil {
		t.Fatal("expected unsupported mode error")
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
