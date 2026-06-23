package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	depgraph "github.com/flanksource/repomap/deps"
)

type DepsOptions struct {
	Path            string   `json:"path" args:"true" help:"Path to scan" default:"."`
	Manager         []string `json:"manager,omitempty" flag:"manager" help:"Dependency manager to include: go, maven, gradle, npm, pnpm, image/docker, helm (repeatable or comma-separated)"`
	Depth           int      `json:"depth,omitempty" flag:"depth" default:"1" help:"Maximum dependency depth (1 = direct only, 0 = unlimited)"`
	Filter          []string `json:"filter,omitempty" flag:"filter" help:"Dependency filter patterns matched against id, name, version, manager, source, or path; supports comma-separated values and !exclusions"`
	Flat            bool     `json:"flat,omitempty" flag:"flat" help:"Export a flat node list with edges instead of the dependency tree"`
	IncludeIndirect bool     `json:"include_indirect,omitempty" flag:"include-indirect" help:"Include Go indirect requirements in --depth 1 listings (ignored at other depths)"`
	ShowDuplicates  bool     `json:"show_duplicates,omitempty" flag:"show-duplicates" help:"Render every occurrence of duplicated dependencies instead of collapsing them to the resolved node"`
}

type DepsUpdateOptions struct {
	Args      []string `json:"args" args:"true" help:"Optional dependency MatchItem expression followed by optional path"`
	Manager   []string `json:"manager,omitempty" flag:"manager" help:"Dependency manager to update: go, npm, pnpm, image/docker, helm (repeatable or comma-separated)"`
	Kind      []string `json:"kind,omitempty" flag:"kind,k" help:"Filter image/helm targets by kind, e.g. HelmRelease,Deployment (MatchItem syntax)"`
	Namespace []string `json:"namespace,omitempty" flag:"namespace,n" help:"Filter image/helm targets by namespace (MatchItem syntax)"`
	Name      []string `json:"name,omitempty" flag:"name" help:"Filter image/helm targets by resource name (MatchItem syntax)"`
	Selector  []string `json:"selector,omitempty" flag:"selector,l" help:"Filter image/helm targets by label selector, e.g. app=nginx"`
	Image     []string `json:"image,omitempty" flag:"image" help:"Only container images matching this repo pattern (MatchItem syntax)"`
	Chart     []string `json:"chart,omitempty" flag:"chart" help:"Only Helm charts matching this name (MatchItem syntax)"`
	Latest    bool     `json:"latest,omitempty" flag:"latest" help:"Resolve each matched dependency to its highest stable version"`
	Version   string   `json:"version,omitempty" flag:"version" help:"Apply this concrete version to all matched dependencies"`
	Check     bool     `json:"check" flag:"check" help:"Resolve and list available updates without prompting or writing"`
	DryRun    bool     `json:"dry_run" flag:"dry-run" help:"Show planned dependency updates without running package-manager commands"`
}

func (opts DepsOptions) GetName() string { return "deps" }

func (opts DepsUpdateOptions) GetName() string { return "update [expr] [path]" }

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

For Helm charts and container images, --depth other than 1 additionally recurses
into remote dependencies: subcharts are fetched from their Helm repositories and
image base images are resolved from registry labels and Dockerfile FROM
directives (cloning source repos). Fetches are cached under the user cache dir;
failures degrade to warnings. Use --depth 1 to stay fully offline.

The command uses the normal Clicky output flow. Use --json to write structured
JSON to stdout, for example:

  repomap deps --json > out.json

By default the JSON export contains the dependency tree under "roots". Use --flat
to export a flat "nodes" list plus "edges" instead of the tree.

Shared dependencies are collapsed to their resolved (shallowest) occurrence: the
resolved node is tagged with the number of other parents and each parent that
hid a duplicate shows a trailing count. Use --show-duplicates to render every
occurrence instead.

EXAMPLES:
  repomap deps
  repomap deps ./service --manager go
  repomap deps --manager npm,pnpm --depth 0
  repomap deps --manager go --depth 0 --flat --json
  repomap deps --manager go --include-indirect
  repomap deps --depth 0 --manager go --show-duplicates
  repomap deps --manager image,helm ./clusters/prod
  repomap deps --filter 'github.com/flanksource/*,!*test*'`)
}

func (opts DepsUpdateOptions) Help() api.Text {
	return clicky.Text(`Update direct package, image, and Helm chart dependencies.

The optional expr argument uses commons MatchItem syntax and is matched against
dependency names, manager-qualified names, versions, and scopes. Manifest path
matching is explicit with path:<pattern> or file:<pattern>. With no expr, every
matched dependency is considered. Image and Helm targets (from git-tracked
Kubernetes/Flux manifests, including HelmRelease spec.chartRef OCIRepository and
HelmChart sources) can be further narrowed with --kind/--namespace/--name/
--selector and the --image/--chart name patterns.

By default repomap prompts for which dependencies and versions to apply. Use
--latest to resolve each to its highest stable version, or --version to apply a
concrete version, both non-interactively. Applied updates are staged with git add
(manifests plus lockfiles); --dry-run and --check never stage.

Use --check to list updateable dependencies without prompting or writing.

EXAMPLES:
  repomap deps update 'github.com/flanksource/*'
  repomap deps update '*' --check
  repomap deps update --manager helm -k HelmRelease --latest
  repomap deps update --manager image -n default --version 1.27.0
  repomap deps update 'path:apps/*/package.json'
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
		ShowDuplicates:  opts.ShowDuplicates,
	})
}

func runDepsUpdate(ctx context.Context, opts DepsUpdateOptions) (any, error) {
	expr, rawPath, err := parseDepsUpdateArgs(opts.Args)
	if err != nil {
		return nil, err
	}
	path, err := resolvePath(rawPath)
	if err != nil {
		return nil, err
	}
	managers, err := parseUpdateManagers(opts.Manager)
	if err != nil {
		return nil, err
	}
	var expression []string
	if expr != "" {
		expression = []string{expr}
	}
	plans, err := depgraph.Update(ctx, path, depgraph.UpdateOptions{
		Managers:   managers,
		Expression: expression,
		Kind:       opts.Kind,
		Namespace:  opts.Namespace,
		Name:       opts.Name,
		Selector:   opts.Selector,
		Image:      opts.Image,
		Chart:      opts.Chart,
		Latest:     opts.Latest,
		Version:    opts.Version,
		Check:      opts.Check,
		DryRun:     opts.DryRun,
	})
	if err != nil {
		return nil, err
	}
	return api.NewTableFrom(plans), nil
}

// parseDepsUpdateArgs interprets the optional positional [expr] [path]. With one
// argument, an existing directory is treated as the path and anything else as the
// expression, so `deps update ./clusters` and `deps update 'left-pad'` both work.
func parseDepsUpdateArgs(args []string) (expr, path string, err error) {
	switch len(args) {
	case 0:
		return "", ".", nil
	case 1:
		if isExistingDir(args[0]) {
			return "", args[0], nil
		}
		return args[0], ".", nil
	case 2:
		return args[0], args[1], nil
	default:
		return "", "", fmt.Errorf("expected [expr] [path], got %d arguments", len(args))
	}
}

func isExistingDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
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
