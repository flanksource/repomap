package main

import (
	"context"
	"fmt"
	"os"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	depgraph "github.com/flanksource/repomap/deps"
)

// UpdateImageOptions are the flags for the deprecated `images update` command,
// which now delegates to `deps update --manager image,helm`.
type UpdateImageOptions struct {
	imageFilterOptions
	Latest  bool   `json:"latest" flag:"latest" help:"Resolve each target to the highest stable semver"`
	Version string `json:"version" flag:"version" help:"Apply this concrete version/tag to all matched targets"`
	DryRun  bool   `json:"dry_run" flag:"dry-run" help:"Show planned edits without writing"`
}

func (opts UpdateImageOptions) GetName() string { return "update" }

func (opts UpdateImageOptions) Help() api.Text {
	return clicky.Text(`DEPRECATED: use 'repomap deps update --manager image,helm' instead.

Update container image tags and Helm chart versions in tracked manifests. This
command now delegates to 'deps update', which additionally stages applied edits
with git and resolves HelmRelease spec.chartRef (OCIRepository/HelmChart) charts.

EXAMPLES:
  repomap deps update --manager helm -n default -k HelmRelease --latest
  repomap deps update --manager image -k Deployment --image nginx --version 1.27.0`)
}

func init() {
	cmd := clicky.AddNamedCommandWithContext("update", imagesCmd, UpdateImageOptions{}, runUpdateImage)
	cmd.Short = "(deprecated) Update image tags and Helm chart versions; use 'deps update'"
}

// runUpdateImage maps the legacy image-update flags onto deps.Update with the
// image and helm managers, so the two commands share one resolution/apply path.
func runUpdateImage(ctx context.Context, opts UpdateImageOptions) (any, error) {
	fmt.Fprintln(os.Stderr, "warning: 'images update' is deprecated; use 'repomap deps update --manager image,helm'")

	path, err := resolvePath(opts.Path)
	if err != nil {
		return nil, err
	}
	plans, err := depgraph.Update(ctx, path, depgraph.UpdateOptions{
		Managers:  []depgraph.Manager{depgraph.ManagerImage, depgraph.ManagerHelm},
		Kind:      opts.Kind,
		Namespace: opts.Namespace,
		Name:      opts.Name,
		Selector:  opts.Selector,
		Image:     opts.Image,
		Chart:     opts.Chart,
		Latest:    opts.Latest,
		Version:   opts.Version,
		DryRun:    opts.DryRun,
	})
	if err != nil {
		return nil, err
	}
	return api.NewTableFrom(plans), nil
}
