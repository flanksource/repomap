package main

import (
	"fmt"
	"strings"

	"github.com/flanksource/commons/collections"
	"github.com/spf13/cobra"

	"github.com/flanksource/repomap"
	"github.com/flanksource/repomap/imageupdate"
)

// versionOnly strips an image/chart current value down to its tag/version
// (dropping any registry/repo prefix and digest suffix).
func versionOnly(currentValue string) string {
	if i := strings.LastIndex(currentValue, ":"); i >= 0 {
		v := currentValue[i+1:]
		if at := strings.Index(v, "@"); at >= 0 {
			v = v[:at]
		}
		return v
	}
	return currentValue
}

// resolveConcurrency bounds how many registry/Helm version lookups run at once.
const resolveConcurrency = 8

// imagesCmd groups the `images list` and `images update` subcommands. It has no
// run function of its own.
var imagesCmd = &cobra.Command{
	Use:   "images",
	Short: "List and update container images and Helm chart versions",
}

func init() {
	rootCmd.AddCommand(imagesCmd)
}

// imageFilterOptions are the discovery filters shared by `images list` and
// `images update`. The first four mirror `scan`'s resource filters; --image and
// --chart further narrow by image repo / chart name.
type imageFilterOptions struct {
	Path      string   `json:"path" args:"true" help:"Path to scan" default:"."`
	Kind      []string `json:"kind" flag:"kind,k" help:"Filter by kind, e.g. HelmRelease,Deployment (MatchItem syntax)"`
	Namespace []string `json:"namespace" flag:"namespace,n" help:"Filter by namespace (MatchItem syntax)"`
	Name      []string `json:"name" flag:"name" help:"Filter by resource name (MatchItem syntax)"`
	Selector  []string `json:"selector" flag:"selector,l" help:"Filter by label selector, e.g. app=nginx"`
	Image     []string `json:"image" flag:"image" help:"Only container images matching this repo pattern (MatchItem syntax)"`
	Chart     []string `json:"chart" flag:"chart" help:"Only HelmRelease charts matching this name (MatchItem syntax)"`
}

// discoverAndFilter resolves the scan path, discovers every image/chart target
// in the repo (via the shared imageupdate discovery, which also resolves chart
// sources), and applies the shared resource + image/chart filters. It returns the
// matching targets and the source index for chart resolution.
func discoverAndFilter(opts imageFilterOptions) ([]imageupdate.UpdateTarget, *imageupdate.SourceIndex, *repomap.ArchConf, error) {
	path, err := resolvePath(opts.Path)
	if err != nil {
		return nil, nil, nil, err
	}
	conf, err := repomap.GetConf(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to load config: %w", err)
	}

	res, err := imageupdate.DiscoverRepoTargets(conf, path)
	if err != nil {
		return nil, nil, nil, err
	}

	matcher := repomap.NewResourceMatcher(opts.Kind, opts.Namespace, opts.Name, opts.Selector)
	targets := imageupdate.Filter(res.Targets, matcher, opts.Image, opts.Chart)
	return targets, res.Index, conf, nil
}
