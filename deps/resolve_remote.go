package deps

import (
	"context"
	"time"

	"github.com/flanksource/repomap/imageupdate"
)

// defaultRemoteDepth bounds recursive fetching when MaxDepth is 0 (unlimited),
// so the network walk always terminates.
const defaultRemoteDepth = 10

// remoteDeps bundles the remote cache-backed resolvers used to recurse into
// chart subcharts and image base images.
type remoteDeps struct {
	charts *chartResolver
	images *imageResolver
}

func remoteDepsFromCache(cache RemoteCache) *remoteDeps {
	return &remoteDeps{
		charts: &chartResolver{helm: &helmClient{cache: cache}},
		images: newImageResolver(cache),
	}
}

func newRemoteDeps(opts Options) (*remoteDeps, error) {
	if opts.remote != nil {
		return opts.remote, nil
	}
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	runner := opts.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	cache, err := newDiskCache(now, runner, imageupdate.NewLabelResolver())
	if err != nil {
		return nil, err
	}
	return remoteDepsFromCache(cache), nil
}

// resolveRemote recursively expands chart-subchart and image-base children in
// place. It is gated by the caller to MaxDepth != 1. Remote failures degrade to
// warnings; only cache initialization can fail hard.
func resolveRemote(ctx context.Context, roots []*Node, opts Options) ([]Warning, error) {
	rd, err := newRemoteDeps(opts)
	if err != nil {
		return nil, err
	}
	limit := opts.MaxDepth
	if limit == 0 {
		limit = defaultRemoteDepth
	}
	visited := map[string]bool{}
	var warnings []Warning
	for _, root := range roots {
		warnings = append(warnings, rd.expandNode(ctx, root, limit, visited)...)
	}
	return warnings, nil
}

// expandNode fetches a node's remote children (if it is a fetchable chart or
// image and within depth/visited bounds), then recurses into all children
// (existing offline ones plus the newly fetched).
func (rd *remoteDeps) expandNode(ctx context.Context, node *Node, limit int, visited map[string]bool) []Warning {
	if node == nil {
		return nil
	}
	var warnings []Warning
	if node.Depth < limit {
		if key := remoteKey(node); key != "" && !visited[key] {
			visited[key] = true
			warnings = append(warnings, rd.fetchChildren(ctx, node)...)
		}
	}
	for _, child := range node.Children {
		warnings = append(warnings, rd.expandNode(ctx, child, limit, visited)...)
	}
	return warnings
}

func (rd *remoteDeps) fetchChildren(ctx context.Context, node *Node) []Warning {
	switch {
	case isFetchableChart(node):
		children, version, warns := rd.charts.expand(ctx, chartDepEntry{
			Name:       node.Name,
			Version:    node.Version,
			Repository: node.Source,
		}, node.Depth)
		if version != "" {
			node.Version = version
			node.ID = NodeID(ManagerHelm, node.Name, version)
		}
		node.Children = append(node.Children, children...)
		return nodeWarnings(node, warns)
	case isFetchableImage(node):
		bases, warns := rd.images.baseImages(ctx, imageRefOf(node))
		for _, b := range bases {
			node.Children = append(node.Children, baseImageNode(b, node.Depth+1))
		}
		return nodeWarnings(node, warns)
	}
	return nil
}

// isFetchableChart matches a Helm dependency that can be resolved from a remote
// repository — excludes the local Chart.yaml root and the synthetic k8s wrapper.
func isFetchableChart(n *Node) bool {
	return n.Manager == ManagerHelm && n.Source != "" &&
		n.Source != "Chart.yaml" && n.Source != "kubernetes manifests"
}

// isFetchableImage matches a real image node — excludes the synthetic
// "container images" k8s wrapper root.
func isFetchableImage(n *Node) bool {
	return n.Manager == ManagerImage && n.Source != "" && n.Source != "kubernetes manifests"
}

func remoteKey(n *Node) string {
	switch {
	case isFetchableChart(n):
		return "helm:" + n.Name + "@" + n.Version
	case isFetchableImage(n):
		return "image:" + imageRefOf(n)
	}
	return ""
}

// imageRefOf returns the original image reference to resolve; Source holds the
// full ref captured at discovery (e.g. "ghcr.io/acme/app:1.2.3").
func imageRefOf(n *Node) string {
	if n.Source != "" && n.Source != "kubernetes manifests" {
		return n.Source
	}
	if n.Version != "" {
		return n.Name + ":" + n.Version
	}
	return n.Name
}

func baseImageNode(b imageRef, depth int) *Node {
	ref := b.Name
	if b.Version != "" {
		ref += ":" + b.Version
	}
	if b.Digest != "" {
		ref += "@" + b.Digest
	}
	n := NewNode(ManagerImage, b.Name, b.Version)
	n.Depth = depth
	n.Direct = false
	n.Scope = "base"
	n.Source = ref // makes the base itself fetchable (base-of-base recursion)
	return n
}

func nodeWarnings(node *Node, msgs []string) []Warning {
	out := make([]Warning, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, Warning{Manager: node.Manager, Project: node.Name, Message: m})
	}
	return out
}
