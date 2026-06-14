package deps

import (
	"context"
	"fmt"
)

// chartResolver fetches a Helm chart dependency and produces its direct children:
// nested subchart dependency nodes and image nodes harvested from the fetched
// chart's values.yaml and templates.
type chartResolver struct {
	helm *helmClient
}

func (c *chartResolver) expand(ctx context.Context, dep chartDepEntry, parentDepth int) (children []*Node, resolvedVersion string, warnings []string) {
	fc, cleanup, err := c.helm.fetchChart(ctx, dep)
	if err != nil {
		return nil, "", []string{fmt.Sprintf("chart %s@%s: %s", dep.Name, dep.Version, err)}
	}
	defer cleanup()

	for _, sub := range fc.Dependencies {
		if sub.Name == "" {
			continue
		}
		n := NewNode(ManagerHelm, sub.Name, sub.Version)
		n.Depth = parentDepth + 1
		n.Scope = "dependencies"
		n.Source = sub.Repository
		children = append(children, n)
	}

	imgNodes, imgWarns := chartImageNodes(fc.Dir, fc.Dir)
	for _, img := range imgNodes {
		img.Depth = parentDepth + 1
		img.Direct = false
		children = append(children, img)
	}
	for _, w := range imgWarns {
		warnings = append(warnings, w.Message)
	}
	return children, fc.Version, warnings
}
