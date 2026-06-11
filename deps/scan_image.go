package deps

import (
	"path/filepath"
	"sort"

	"github.com/flanksource/repomap"
	"github.com/flanksource/repomap/imageupdate"
)

func discoverImageDependencyRoots(root string, managers []Manager) ([]*Node, []Warning, error) {
	selected := managerSet(managers)
	conf, err := repomap.GetConf(root)
	if err != nil {
		return nil, nil, err
	}
	targets, sourceIndex, err := discoverImageTargets(conf, root)
	if err != nil {
		return nil, nil, err
	}

	var imageChildren []*Node
	var helmChildren []*Node
	var warnings []Warning
	for _, target := range targets {
		manager := managerForUpdateTarget(target)
		if manager == "" || (len(selected) > 0 && !selected[manager]) {
			continue
		}
		if target.Kind == imageupdate.TargetChart {
			if err := sourceIndex.Resolve(&target); err != nil {
				warnings = append(warnings, Warning{Manager: ManagerHelm, Project: target.File, Message: err.Error()})
			}
		}
		node := nodeForImageTarget(target, manager)
		switch manager {
		case ManagerImage:
			imageChildren = append(imageChildren, node)
		case ManagerHelm:
			helmChildren = append(helmChildren, node)
		}
	}

	var roots []*Node
	if len(imageChildren) > 0 {
		roots = append(roots, imageDependencyRoot(ManagerImage, "container images", root, imageChildren))
	}
	if len(helmChildren) > 0 {
		roots = append(roots, imageDependencyRoot(ManagerHelm, "helm charts", root, helmChildren))
	}
	return roots, warnings, nil
}

func imageDependencyRoot(manager Manager, name, path string, children []*Node) *Node {
	sort.SliceStable(children, func(i, j int) bool {
		return dependencyLess(children[i], children[j])
	})
	root := NewNode(manager, name, "")
	root.Source = "kubernetes manifests"
	root.Path = filepath.ToSlash(path)
	root.Depth = 0
	root.Children = children
	return root
}

func nodeForImageTarget(target imageupdate.UpdateTarget, manager Manager) *Node {
	node := NewNode(manager, updateTargetName(target), updateTargetCurrentVersion(target))
	node.Direct = true
	node.Depth = 1
	node.Scope = updateTargetScope(target)
	node.Source = imageTargetSource(target)
	node.Path = filepath.ToSlash(target.File)
	return node
}

func imageTargetSource(target imageupdate.UpdateTarget) string {
	switch target.Kind {
	case imageupdate.TargetImage:
		return target.CurrentValue
	case imageupdate.TargetChart:
		if target.RepoURL != "" {
			return target.RepoURL
		}
		if target.SourceRefName != "" {
			if target.SourceRefNamespace != "" {
				return target.SourceRefNamespace + "/" + target.SourceRefName
			}
			return target.SourceRefName
		}
	}
	return ""
}
