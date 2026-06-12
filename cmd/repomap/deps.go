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
	Path    string   `json:"path" args:"true" help:"Path to scan" default:"."`
	Manager []string `json:"manager,omitempty" flag:"manager" help:"Dependency manager to include: go, maven, gradle, npm, pnpm, image/docker, helm (repeatable or comma-separated)"`
	Depth          int      `json:"depth,omitempty" flag:"depth" default:"1" help:"Maximum dependency depth (1 = direct only, 0 = unlimited)"`
	Filter         []string `json:"filter,omitempty" flag:"filter" help:"Dependency filter patterns matched against id, name, version, manager, source, or path; supports comma-separated values and !exclusions"`
	Flat           bool     `json:"flat,omitempty" flag:"flat" help:"Export a flat node list with edges instead of the dependency tree"`
	IncludeIndirect bool    `json:"include_indirect,omitempty" flag:"include-indirect" help:"Include Go indirect requirements in --depth 1 listings (ignored at other depths)"`
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
	return clicky.Text(`Generate dependency graphs for Go, Maven, Gradle, npm, pnpm, image, and Helm dependencies.

Repomap auto-detects supported manifests below the selected path and resolves
dependency graphs from local manifest and lockfile content without running
package-manager commands. Image and Helm dependencies are discovered from
git-tracked Kubernetes manifests.

For Go, Maven, and Gradle, --depth other than 1 resolves transitive dependencies
by shelling out to the package manager (go mod graph, mvn dependency:tree,
gradle dependencies). The tool must be installed; rerun with --depth 1 for
offline direct-only output.

The command uses the normal Clicky output flow. Use --json to write structured
JSON to stdout, for example:

  repomap deps --json > out.json

By default the JSON export contains the dependency tree under "roots". Use --flat
to export a flat "nodes" list plus "edges" instead of the tree.

EXAMPLES:
  repomap deps
  repomap deps ./service --manager go
  repomap deps --manager npm,pnpm --depth 0
  repomap deps --manager go --depth 0 --flat --json
  repomap deps --manager go --include-indirect
  repomap deps --manager image,helm ./clusters/prod
  repomap deps --filter 'github.com/flanksource/*,!*test*'`)
}

func (opts DepsUpdateOptions) Help() api.Text {
	return clicky.Text(`Update direct package, image, and Helm chart dependencies.

The required expr argument uses commons MatchItem syntax and is matched against
dependency names, manager-qualified names, versions, and scopes. Manifest path
matching is explicit with path:<pattern> or file:<pattern>. Matched direct
dependencies are resolved to published versions, then repomap prompts for which
dependencies and versions to apply. Applied updates are staged with git add
(manifests plus lockfiles); --dry-run and --check never stage.

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

	registerDepsDiff(cmd)
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
	return depgraph.Scan(ctx, path, depgraph.Options{
		Managers:        managers,
		MaxDepth:        opts.Depth,
		Filters:         splitCommaArgs(opts.Filter),
		Flat:            opts.Flat,
		IncludeIndirect: opts.IncludeIndirect,
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

func parseManagers(values []string) ([]depgraph.Manager, error) {
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
		case depgraph.ManagerGo, depgraph.ManagerMaven, depgraph.ManagerGradle, depgraph.ManagerNPM, depgraph.ManagerPNPM, depgraph.ManagerImage, depgraph.ManagerHelm:
			out = append(out, manager)
		default:
			return nil, fmt.Errorf("unsupported dependency manager %q (expected go, maven, gradle, npm, pnpm, image/docker, or helm)", part)
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
