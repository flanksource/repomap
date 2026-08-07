package main

import (
	"context"
	"strings"
	"testing"

	"github.com/flanksource/clicky"
	depgraph "github.com/flanksource/repomap/deps"
	"github.com/spf13/cobra"
)

// runCacheWarmArgs parses argv through a real cobra tree with a stub handler, so
// the assertions cover clicky's struct-tag binding rather than a hand-built
// options struct.
func runCacheWarmArgs(t *testing.T, argv ...string) (CacheWarmOptions, error) {
	t.Helper()
	var got CacheWarmOptions
	root := &cobra.Command{Use: "test"}
	clicky.BindAllFlags(root.PersistentFlags(), "tasks", "format")
	clicky.AddNamedCommandWithContext("cache-warm", root, CacheWarmOptions{}, func(_ context.Context, opts CacheWarmOptions) (any, error) {
		got = opts
		return nil, nil
	})
	root.SetArgs(argv)
	return got, root.Execute()
}

// The positional args field must carry no default: tag, or clicky silently drops
// the positional values.
func TestCacheWarmBindsPositionalArgsAndFlags(t *testing.T) {
	got, err := runCacheWarmArgs(t, "cache-warm", "go", "github.com/acme/lib@v1.2.3", "left-pad@1.3.0", "--build", "--verify")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"go", "github.com/acme/lib@v1.2.3", "left-pad@1.3.0"}
	if strings.Join(got.Args, ",") != strings.Join(want, ",") {
		t.Fatalf("Args = %v, want %v", got.Args, want)
	}
	if !got.Build || !got.Verify {
		t.Fatalf("Build = %v, Verify = %v, want both true", got.Build, got.Verify)
	}
}

func TestCacheWarmFlagsDefaultOff(t *testing.T) {
	got, err := runCacheWarmArgs(t, "cache-warm", "go", "github.com/acme/lib@v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if got.Build || got.Verify {
		t.Fatalf("Build = %v, Verify = %v, want both false", got.Build, got.Verify)
	}
}

func TestParseCacheWarmArgs(t *testing.T) {
	cases := []struct {
		name        string
		args        []string
		wantManager depgraph.Manager
		wantSpecs   []string
		wantErr     string
	}{
		{
			name: "single spec", args: []string{"go", "github.com/acme/lib@v1.2.3"},
			wantManager: depgraph.ManagerGo, wantSpecs: []string{"github.com/acme/lib@v1.2.3"},
		},
		{
			name: "several specs", args: []string{"pnpm", "left-pad@1.3.0", "@scope/pkg@2.0.0"},
			wantManager: depgraph.ManagerPNPM, wantSpecs: []string{"left-pad@1.3.0", "@scope/pkg@2.0.0"},
		},
		{
			name: "manager casing is normalised", args: []string{"NPM", "left-pad@1.3.0"},
			wantManager: depgraph.ManagerNPM, wantSpecs: []string{"left-pad@1.3.0"},
		},
		{name: "no args", args: nil, wantErr: "manager"},
		{name: "manager but no spec", args: []string{"go"}, wantErr: "spec"},
		// Managers repomap can scan but cannot warm must be rejected by name.
		{name: "unwarmable manager", args: []string{"maven", "org.acme:lib@1.0.0"}, wantErr: "maven"},
		{name: "unknown manager", args: []string{"cargo", "serde@1.0.0"}, wantErr: "cargo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manager, specs, err := parseCacheWarmArgs(tc.args)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected an error mentioning %q", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q should mention %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if manager != tc.wantManager {
				t.Errorf("manager = %q, want %q", manager, tc.wantManager)
			}
			if strings.Join(specs, ",") != strings.Join(tc.wantSpecs, ",") {
				t.Errorf("specs = %v, want %v", specs, tc.wantSpecs)
			}
		})
	}
}

// defaultToScan rewrites anything it does not recognise into `scan ...`, so a
// misregistered name would turn this command into a silent repo scan.
func TestCacheWarmIsNotRewrittenToScan(t *testing.T) {
	argv := []string{"cache-warm", "go", "github.com/acme/lib@v1.2.3"}
	got := defaultToScan(argv)
	if strings.Join(got, " ") != strings.Join(argv, " ") {
		t.Fatalf("defaultToScan(%v) = %v, want it unchanged", argv, got)
	}
}
