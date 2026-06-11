package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/spf13/cobra"
	depgraph "github.com/flanksource/repomap/deps"
)

type DepsDiffOptions struct {
	Args    []string `json:"args" args:"true" required:"true" help:"<ref> or <ref1>..<ref2>, optionally followed by a path"`
	Manager []string `json:"manager,omitempty" flag:"manager" help:"Dependency manager to include: go, maven, gradle, npm, pnpm, image/docker, helm (repeatable or comma-separated)"`
	Depth   int      `json:"depth,omitempty" flag:"depth" default:"1" help:"Maximum dependency depth to compare (1 = direct only, 0 = unlimited)"`
	Filter  []string `json:"filter,omitempty" flag:"filter" help:"Dependency filter patterns applied to both sides; supports comma-separated values and !exclusions"`
}

func (opts DepsDiffOptions) GetName() string { return "diff <ref|ref1..ref2> [path]" }

func (opts DepsDiffOptions) Help() api.Text {
	return clicky.Text(`Compare dependency graphs across git revisions.

Resolves dependencies at a base revision and compares them against the working
tree (default) or a second revision with ref1..ref2. Each non-working-tree side
is checked out into a temporary git worktree so image and Helm discovery work
unchanged. A dirty working tree is compared as-is.

Pass --depth 0 to compare full transitive graphs (requires the package manager
tools, e.g. go, mvn, gradle); the default --depth 1 compares direct dependencies
offline.

EXAMPLES:
  repomap deps diff HEAD~3
  repomap deps diff main
  repomap deps diff v1.0.0..v1.1.0 ./service
  repomap deps diff HEAD~5 --manager go --depth 0`)
}

func registerDepsDiff(parent *cobra.Command) {
	diffCmd := clicky.AddNamedCommandWithContext("diff", parent, DepsDiffOptions{}, runDepsDiff)
	diffCmd.Short = "Compare dependency graphs across git revisions"
}

func runDepsDiff(ctx context.Context, opts DepsDiffOptions) (*depgraph.Comparison, error) {
	if len(opts.Args) == 0 {
		return nil, fmt.Errorf("a git ref or ref1..ref2 range is required")
	}
	if len(opts.Args) > 2 {
		return nil, fmt.Errorf("expected <ref|ref1..ref2> [path], got %d arguments", len(opts.Args))
	}
	baseRef, headRef, err := parseRefRange(opts.Args[0])
	if err != nil {
		return nil, err
	}
	path := "."
	if len(opts.Args) == 2 {
		path = opts.Args[1]
	}
	path, err = resolvePath(path)
	if err != nil {
		return nil, err
	}
	managers, err := parseManagers(opts.Manager)
	if err != nil {
		return nil, err
	}
	return depgraph.CompareScan(ctx, path, depgraph.CompareOptions{
		Options: depgraph.Options{
			Managers: managers,
			MaxDepth: opts.Depth,
			Filters:  splitCommaArgs(opts.Filter),
		},
		BaseRef: baseRef,
		HeadRef: headRef,
	})
}

// parseRefRange splits a "ref1..ref2" range or returns a single ref with an
// empty head (meaning the working tree). Both sides of a range are required.
func parseRefRange(arg string) (baseRef, headRef string, err error) {
	if !strings.Contains(arg, "..") {
		return arg, "", nil
	}
	left, right, _ := strings.Cut(arg, "..")
	if left == "" || right == "" {
		return "", "", fmt.Errorf("invalid ref range %q: both sides of .. are required", arg)
	}
	return left, right, nil
}
