package main

import (
	"context"
	"strings"
	"testing"

	"github.com/flanksource/clicky"
	depgraph "github.com/flanksource/repomap/deps"
	"github.com/spf13/cobra"
)

func TestDepsPositionalPathHonored(t *testing.T) {
	var gotPath string
	root := &cobra.Command{Use: "test"}
	clicky.BindAllFlags(root.PersistentFlags(), "tasks", "format")
	clicky.AddNamedCommandWithContext("deps", root, DepsOptions{}, func(_ context.Context, opts DepsOptions) (*depgraph.Export, error) {
		gotPath = opts.Path
		return &depgraph.Export{}, nil
	})

	root.SetArgs([]string{"deps", "/tmp/some/scan/path", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/tmp/some/scan/path" {
		t.Fatalf("positional path not bound: opts.Path = %q, want /tmp/some/scan/path", gotPath)
	}
}

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

// deps update must declare its own --filter, otherwise the flag silently binds
// to clicky's persistent format flag (`--filter string`, a CEL output filter)
// and the MatchItem patterns never reach depgraph.Update.
func TestDepsUpdateFilterFlagShadowsGlobalCELFilter(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"deps", "update"})
	if err != nil {
		t.Fatal(err)
	}
	flag := cmd.Flags().Lookup("filter")
	if flag == nil {
		t.Fatal("filter flag not registered on deps update")
	}
	if got := flag.Value.Type(); got != "stringSlice" {
		t.Fatalf("filter flag type = %q, want stringSlice (clicky's global CEL filter is a string)", got)
	}
	if !strings.Contains(flag.Usage, "MatchItem syntax") {
		t.Fatalf("filter help should document MatchItem syntax, got %q", flag.Usage)
	}
}

func TestDepsUpdateFilterFlagBindsToOptions(t *testing.T) {
	var got DepsUpdateOptions
	root := &cobra.Command{Use: "test"}
	clicky.BindAllFlags(root.PersistentFlags(), "tasks", "format")
	deps := clicky.AddNamedCommandWithContext("deps", root, DepsOptions{}, func(_ context.Context, _ DepsOptions) (*depgraph.Export, error) {
		return &depgraph.Export{}, nil
	})
	clicky.AddNamedCommandWithContext("update", deps, DepsUpdateOptions{}, func(_ context.Context, opts DepsUpdateOptions) (any, error) {
		got = opts
		return nil, nil
	})

	root.SetArgs([]string{"deps", "update", "--filter", "*flanksource*,!*test*", "--filter", "left-pad", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	// cobra's stringSlice comma-splits at parse time; depgraph.Update splits
	// again for the positional expr, which arrives as one unsplit string.
	want := []string{"*flanksource*", "!*test*", "left-pad"}
	if len(got.Filter) != len(want) {
		t.Fatalf("Filter = %#v, want %#v", got.Filter, want)
	}
	for i := range want {
		if got.Filter[i] != want[i] {
			t.Fatalf("Filter[%d] = %q, want %q", i, got.Filter[i], want[i])
		}
	}
}

func TestUpdateFiltersCombinesFlagAndPositionalExpr(t *testing.T) {
	cases := []struct {
		name   string
		filter []string
		expr   string
		want   []string
	}{
		{name: "neither", want: nil},
		{name: "flag only", filter: []string{"npm:@scope/*"}, want: []string{"npm:@scope/*"}},
		{name: "expr only", expr: "left-pad", want: []string{"left-pad"}},
		{
			name:   "flag and expr combined",
			filter: []string{"*flanksource*", "!*test*"},
			expr:   "path:apps/*/package.json",
			want:   []string{"*flanksource*", "!*test*", "path:apps/*/package.json"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := updateFilters(tc.filter, tc.expr)
			if len(got) != len(tc.want) {
				t.Fatalf("updateFilters(%#v, %q) = %#v, want %#v", tc.filter, tc.expr, got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("pattern[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestParseDepsUpdateArgs(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name     string
		args     []string
		wantExpr string
		wantPath string
		wantErr  bool
	}{
		{name: "no args defaults to cwd", wantPath: "."},
		{name: "existing dir is the path", args: []string{dir}, wantPath: dir},
		{name: "non-dir is the expression", args: []string{"left-pad"}, wantExpr: "left-pad", wantPath: "."},
		{name: "expr then path", args: []string{"left-pad", dir}, wantExpr: "left-pad", wantPath: dir},
		{name: "too many args", args: []string{"a", "b", "c"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, path, err := parseDepsUpdateArgs(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %#v", tc.args)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if expr != tc.wantExpr || path != tc.wantPath {
				t.Fatalf("parseDepsUpdateArgs(%#v) = (%q, %q), want (%q, %q)", tc.args, expr, path, tc.wantExpr, tc.wantPath)
			}
		})
	}
}
