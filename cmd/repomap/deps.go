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

type DepsUpdateOptions struct {
	Args    []string `json:"args" args:"true" required:"true" help:"Dependency MatchItem expression followed by optional path"`
	Manager []string `json:"manager,omitempty" flag:"manager" help:"Dependency manager to update: go, npm, pnpm, image/docker, helm (repeatable or comma-separated)"`
	Check   bool     `json:"check" flag:"check" help:"Resolve and list available updates without prompting or writing"`
	DryRun  bool     `json:"dry_run" flag:"dry-run" help:"Show planned dependency updates without running package-manager commands"`
}

func (opts DepsOptions) GetName() string { return "deps" }

func (opts DepsUpdateOptions) GetName() string { return "update <expr> [path]" }

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

func (opts DepsUpdateOptions) Help() api.Text {
	return clicky.Text(`Update direct package, image, and Helm chart dependencies.

The required expr argument uses commons MatchItem syntax and is matched against
dependency names, manager-qualified names, versions, and scopes. Manifest path
matching is explicit with path:<pattern> or file:<pattern>. Matched direct
dependencies are resolved to published versions, then repomap prompts for which
dependencies and versions to apply.

Use --check to list updateable dependencies without prompting or writing.

EXAMPLES:
  repomap deps update 'github.com/flanksource/*'
  repomap deps update '*' --check
  repomap deps update 'path:apps/*/package.json'
  repomap deps update 'image:ghcr.io/flanksource/*' --manager image
  repomap deps update 'helm:mission-control' --manager helm
  repomap deps update 'npm:@flanksource/*' ./web --manager npm
  repomap deps update 'left-pad,!*beta*' --dry-run`)
}

func init() {
	cmd := clicky.AddNamedCommandWithContext("deps", rootCmd, DepsOptions{}, runDeps)
	cmd.Short = "Generate dependency graphs for Go, Maven, Gradle, npm, and pnpm projects"

	updateCmd := clicky.AddNamedCommandWithContext("update", cmd, DepsUpdateOptions{}, runDepsUpdate)
	updateCmd.Short = "Update direct package, image, and Helm chart dependencies"
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

func runDepsUpdate(ctx context.Context, opts DepsUpdateOptions) (any, error) {
	if len(opts.Args) == 0 {
		return nil, fmt.Errorf("dependency update expression is required")
	}
	if len(opts.Args) > 2 {
		return nil, fmt.Errorf("expected <expr> [path], got %d arguments", len(opts.Args))
	}
	path := "."
	if len(opts.Args) == 2 {
		path = opts.Args[1]
	}
	path, err := resolvePath(path)
	if err != nil {
		return nil, err
	}
	managers, err := parseUpdateManagers(opts.Manager)
	if err != nil {
		return nil, err
	}
	plans, err := depgraph.Update(ctx, path, depgraph.UpdateOptions{
		Managers:   managers,
		Expression: []string{opts.Args[0]},
		Check:      opts.Check,
		DryRun:     opts.DryRun,
	})
	if err != nil {
		return nil, err
	}
	return api.NewTableFrom(plans), nil
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

func parseUpdateManagers(values []string) ([]depgraph.Manager, error) {
	parts := splitCommaArgs(values)
	if len(parts) == 0 {
		return nil, nil
	}
	out := make([]depgraph.Manager, 0, len(parts))
	for _, part := range parts {
		manager := depgraph.Manager(strings.ToLower(part))
		switch manager {
		case "docker":
			out = append(out, depgraph.ManagerImage)
		case depgraph.ManagerGo, depgraph.ManagerNPM, depgraph.ManagerPNPM, depgraph.ManagerImage, depgraph.ManagerHelm:
			out = append(out, manager)
		default:
			return nil, fmt.Errorf("unsupported dependency update manager %q (expected go, npm, pnpm, image/docker, or helm)", part)
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
