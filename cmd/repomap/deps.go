package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	depgraph "github.com/flanksource/repomap/deps"
)

type DepsOptions struct {
	Path          string   `json:"path" args:"true" help:"Path to scan" default:"."`
	Manager       []string `json:"manager,omitempty" flag:"manager" help:"Dependency manager to include: go, maven, gradle, npm, pnpm (repeatable or comma-separated)"`
	Mode          string   `json:"mode,omitempty" flag:"mode" default:"auto" help:"Resolution mode: auto, native, or manifest"`
	Depth         int      `json:"depth,omitempty" flag:"depth" default:"1" help:"Maximum dependency depth (1 = direct only, 0 = unlimited)"`
	Filter        []string `json:"filter,omitempty" flag:"filter" help:"Dependency filter patterns matched against id, name, version, manager, source, or path; supports comma-separated values and !exclusions"`
	Configuration []string `json:"configuration,omitempty" flag:"configuration" help:"Gradle configuration to resolve (repeatable or comma-separated); default is all resolvable configurations"`
	Strict        bool     `json:"strict,omitempty" flag:"strict" help:"Fail if native resolution is unavailable or fallback resolution is degraded"`
}

func (opts DepsOptions) GetName() string { return "deps" }

func (opts DepsOptions) Help() api.Text {
	return clicky.Text(`Generate dependency graphs for Go, Maven, Gradle, npm, and pnpm projects.

Repomap auto-detects supported manifests below the selected path, resolves
transitive dependency graphs with native tools when available, and falls back to
manifest or lockfile parsing with warnings when native resolution is unavailable.

The command uses the normal Clicky output flow. Use --json to write structured
JSON to stdout, for example:

  repomap deps --json > out.json

EXAMPLES:
  repomap deps
  repomap deps ./service --manager go
  repomap deps --manager npm,pnpm --depth 0
  repomap deps --filter 'github.com/flanksource/*,!*test*'
  repomap deps --mode manifest --json > deps.json`)
}

func init() {
	cmd := clicky.AddNamedCommandWithContext("deps", rootCmd, DepsOptions{}, runDeps)
	cmd.Short = "Generate dependency graphs for Go, Maven, Gradle, npm, and pnpm projects"
}

func runDeps(ctx context.Context, opts DepsOptions) (*depgraph.Export, error) {
	if opts.Path == "" {
		opts.Path = "."
	}
	path, err := resolvePath(opts.Path)
	if err != nil {
		return nil, err
	}
	managers, err := parseManagers(opts.Manager)
	if err != nil {
		return nil, err
	}
	mode, err := parseDepsMode(opts.Mode)
	if err != nil {
		return nil, err
	}
	return depgraph.Scan(ctx, path, depgraph.Options{
		Managers:       managers,
		Mode:           mode,
		MaxDepth:       opts.Depth,
		Filters:        splitCommaArgs(opts.Filter),
		Configurations: splitCommaArgs(opts.Configuration),
		Strict:         opts.Strict,
	})
}

func parseDepsMode(value string) (depgraph.Mode, error) {
	switch depgraph.Mode(strings.TrimSpace(value)) {
	case "", depgraph.ModeAuto:
		return depgraph.ModeAuto, nil
	case depgraph.ModeNative:
		return depgraph.ModeNative, nil
	case depgraph.ModeManifest:
		return depgraph.ModeManifest, nil
	default:
		return "", fmt.Errorf("unsupported deps mode %q (expected auto, native, or manifest)", value)
	}
}

func parseManagers(values []string) ([]depgraph.Manager, error) {
	parts := splitCommaArgs(values)
	if len(parts) == 0 {
		return nil, nil
	}
	out := make([]depgraph.Manager, 0, len(parts))
	for _, part := range parts {
		manager := depgraph.Manager(strings.ToLower(part))
		switch manager {
		case depgraph.ManagerGo, depgraph.ManagerMaven, depgraph.ManagerGradle, depgraph.ManagerNPM, depgraph.ManagerPNPM:
			out = append(out, manager)
		default:
			return nil, fmt.Errorf("unsupported dependency manager %q (expected go, maven, gradle, npm, or pnpm)", part)
		}
	}
	return out, nil
}

func splitCommaArgs(values []string) []string {
	var out []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}
