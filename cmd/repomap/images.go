package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/flanksource/commons/collections"
	"github.com/spf13/cobra"

	"github.com/flanksource/repomap"
	"github.com/flanksource/repomap/imageupdate"
	"github.com/flanksource/repomap/kubernetes"
)

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
// in the repo, and applies the shared resource + image/chart filters. It returns
// the matching targets and the HelmRepository source index for chart resolution.
func discoverAndFilter(opts imageFilterOptions) ([]imageupdate.UpdateTarget, *imageupdate.SourceIndex, *repomap.ArchConf, error) {
	path, err := resolvePath(opts.Path)
	if err != nil {
		return nil, nil, nil, err
	}
	conf, err := repomap.GetConf(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to load config: %w", err)
	}

	targets, sourceIndex, err := discoverTargets(conf, path)
	if err != nil {
		return nil, nil, nil, err
	}

	matcher := repomap.NewResourceMatcher(opts.Kind, opts.Namespace, opts.Name, opts.Selector)
	targets = filterTargets(targets, matcher, opts)
	return targets, sourceIndex, conf, nil
}

// discoverTargets reads every tracked YAML file, builds the kustomize/Flux tree
// so HelmRepository sources can be resolved through Kustomization namespace
// transformers, indexes the HelmRepositories, and extracts update targets from
// files under the scan prefix.
func discoverTargets(conf *repomap.ArchConf, scanPath string) ([]imageupdate.UpdateTarget, *imageupdate.SourceIndex, error) {
	files, err := gitListFiles(conf.RepoPath())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list files: %w", err)
	}

	var prefix string
	if rel, err := filepath.Rel(conf.RepoPath(), scanPath); err == nil && rel != "." {
		prefix = rel + string(filepath.Separator)
	}

	// Pass 0: read all tracked YAML (paths are repo-relative POSIX from git).
	contents := map[string]string{}
	for _, f := range files {
		if !kubernetes.IsYaml(f) {
			continue
		}
		content, err := conf.ReadFileWithFallback(f, "")
		if err != nil {
			continue
		}
		contents[f] = content
	}

	// Pass 1: build the kustomize/Flux tree and the HelmRepository index.
	tree := imageupdate.BuildKustomizeTree(contents)
	sourceIndex := imageupdate.NewSourceIndex(tree)

	// Pass 2: index sources repo-wide; extract targets only under the scan prefix.
	var targets []imageupdate.UpdateTarget
	for f, content := range contents {
		_ = sourceIndex.IndexHelmRepositories(f, content)

		if prefix != "" && !strings.HasPrefix(f, prefix) {
			continue
		}
		fileTargets, err := imageupdate.ExtractTargets(f, content)
		if err != nil {
			continue
		}
		targets = append(targets, fileTargets...)
	}
	return targets, sourceIndex, nil
}

func filterTargets(targets []imageupdate.UpdateTarget, matcher repomap.ResourceMatcher, opts imageFilterOptions) []imageupdate.UpdateTarget {
	var out []imageupdate.UpdateTarget
	for _, t := range targets {
		if !matcher.MatchesRef(t.Ref) {
			continue
		}
		if t.Kind == imageupdate.TargetImage && len(opts.Image) > 0 {
			if matched, _ := collections.MatchItem(t.Image.GetFullNameWithoutTag(), opts.Image...); !matched {
				continue
			}
		}
		if t.Kind == imageupdate.TargetChart && len(opts.Chart) > 0 {
			if matched, _ := collections.MatchItem(t.ChartName, opts.Chart...); !matched {
				continue
			}
		}
		out = append(out, t)
	}
	return out
}
