package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	depgraph "github.com/flanksource/repomap/deps"
)

type CacheWarmOptions struct {
	// No default: tag — a positional field carrying one has its positional values
	// silently discarded. Emptiness is validated in runCacheWarm instead.
	Args   []string `json:"args" args:"true" help:"Package manager (go, npm, pnpm) followed by one or more name@version specs"`
	Build  bool     `json:"build,omitempty" flag:"build" help:"Compile every package after downloading (Go) or run dependency lifecycle and native builds (npm, pnpm)"`
	Verify bool     `json:"verify,omitempty" flag:"verify" help:"Replay the warm with the network disabled to prove the cache is complete"`
}

func (opts CacheWarmOptions) GetName() string { return "cache-warm <manager> <name@version>..." }

func (opts CacheWarmOptions) Help() api.Text {
	return clicky.Text(`Prime the local package caches for dependencies this machine has not checked out.

For each name@version spec, repomap creates a throwaway single-dependency
project in a temporary directory, drives the real package manager to download
the dependency's full transitive closure into the machine's shared cache, then
deletes the project. Nothing in the working tree is touched; what persists is
the warmed cache (GOMODCACHE, the pnpm store, or the npm cache).

Omit the version to take whatever the manager considers current. The concrete
resolved version is reported back, including Go pseudo-versions.

A Go dependency can be given as a module path, as a GitHub owner/repo slug, or as
a repository URL — all three are canonicalised to the module path before the go
toolchain sees them.

Use --build to go further than downloading. For Go it compiles every package in
the module so GOCACHE holds the build artifacts, not just the source. For npm and
pnpm it lets dependency lifecycle scripts run so native addons are compiled. It
does not run the package's own build script.

Use --verify to prove the result rather than assume it: the work is replayed with
the network disabled (GOPROXY=off, or an --offline install against a frozen
lockfile), so a cache that could not actually build offline fails loudly.

This is aimed at CI images, sandboxes, and air-gapped builds, where a later build
must succeed with no network access.

EXAMPLES:
  repomap cache-warm go github.com/flanksource/clicky@v1.21.14
  repomap cache-warm go github.com/flanksource/commons --build --verify
  repomap cache-warm go flanksource/commons
  repomap cache-warm go https://github.com/flanksource/commons
  repomap cache-warm pnpm react@18.2.0 react-dom@18.2.0
  repomap cache-warm npm @flanksource/icons@1.0.0 --verify
  repomap cache-warm go github.com/flanksource/clicky@v1.21.14 --json`)
}

func init() {
	cmd := clicky.AddNamedCommandWithContext("cache-warm", rootCmd, CacheWarmOptions{}, runCacheWarm)
	cmd.Short = "Warm the Go, npm, or pnpm cache for a dependency and optionally build it"
}

func runCacheWarm(ctx context.Context, opts CacheWarmOptions) (any, error) {
	manager, specs, err := parseCacheWarmArgs(opts.Args)
	if err != nil {
		return nil, err
	}
	results, err := depgraph.WarmCache(ctx, depgraph.WarmOptions{
		Manager: manager,
		Specs:   specs,
		Build:   opts.Build,
		Verify:  opts.Verify,
	})
	// Returned as a slice rather than api.NewTableFrom so --json keeps the full
	// WarmResult — per-step commands, durations, and errors, which is what a CI
	// debugging session needs. Pretty output still renders as a table via
	// WarmResult's Columns/Row. The error is returned alongside the results so a
	// partial run still reports which specs succeeded.
	return results, err
}

// parseCacheWarmArgs splits the positional arguments into the manager and its
// specs. Only managers repomap can actually warm are accepted; maven and gradle
// are scan-only, and image/helm are not package caches.
func parseCacheWarmArgs(args []string) (depgraph.Manager, []string, error) {
	if len(args) == 0 {
		return "", nil, fmt.Errorf("expected a package manager (go, npm, or pnpm) followed by one or more name@version specs")
	}
	manager := depgraph.Manager(strings.ToLower(strings.TrimSpace(args[0])))
	switch manager {
	case depgraph.ManagerGo, depgraph.ManagerNPM, depgraph.ManagerPNPM:
	default:
		return "", nil, fmt.Errorf("cache warming does not support %q (expected go, npm, or pnpm)", args[0])
	}
	specs := splitCommaArgs(args[1:])
	if len(specs) == 0 {
		return "", nil, fmt.Errorf("expected at least one name@version spec to warm for %s", manager)
	}
	return manager, specs, nil
}
