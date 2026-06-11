package deps

import (
	"path/filepath"
	"sort"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
)

// displayNode is a presentation-only tree node: it carries a pre-rendered label
// and arbitrary children, letting the renderer group/relabel the underlying
// dependency graph (manager grouping, kubernetes namespace/kind, relative paths)
// without mutating the model.
type displayNode struct {
	text     api.Text
	children []api.TreeNode
}

var _ api.TreeNode = (*displayNode)(nil)

func (d *displayNode) Pretty() api.Text          { return d.text }
func (d *displayNode) GetChildren() []api.TreeNode { return d.children }

func dependencyDisplayTree(roots []*Node, scanPath string) (api.TextTree, bool) {
	top := make([]api.TreeNode, 0, len(roots))
	for _, root := range roots {
		if root != nil {
			top = append(top, rootDisplayNode(root, scanPath))
		}
	}
	if len(top) == 0 {
		return api.TextTree{}, false
	}
	return api.NewTree(top...), true
}

func rootDisplayNode(root *Node, scanPath string) *displayNode {
	label := clicky.Text("[", "text-muted").
		Append(string(root.Manager), managerStyle(root.Manager)).
		Append("] ", "text-muted").
		Append(root.Name, "font-bold text-cyan-600")
	if rel := relDisplayPath(scanPath, root.Path); rel != "" {
		label = label.Space().Append(rel, "font-mono text-muted")
	}

	node := &displayNode{text: label}
	switch root.Manager {
	case ManagerImage, ManagerHelm:
		node.children = k8sGroupNodes(root.Children)
	default:
		node.children = packageDisplayNodes(root.Children)
	}
	return node
}

// packageDisplayNodes renders dependency children without repeating the manager
// prefix (the root already carries it).
func packageDisplayNodes(nodes []*Node) []api.TreeNode {
	sorted := sortedNodes(nodes)
	out := make([]api.TreeNode, 0, len(sorted))
	for _, n := range sorted {
		out = append(out, &displayNode{
			text:     nodeText(n, false),
			children: packageDisplayNodes(n.Children),
		})
	}
	return out
}

// k8sGroupNodes groups image/helm leaves under namespace → kind headers.
func k8sGroupNodes(leaves []*Node) []api.TreeNode {
	byNamespace := map[string][]*Node{}
	var namespaces []string
	for _, leaf := range sortedNodes(leaves) {
		ns := leaf.prop(propNamespace)
		if ns == "" {
			ns = "(none)"
		}
		if _, seen := byNamespace[ns]; !seen {
			namespaces = append(namespaces, ns)
		}
		byNamespace[ns] = append(byNamespace[ns], leaf)
	}
	sort.Strings(namespaces)

	out := make([]api.TreeNode, 0, len(namespaces))
	for _, ns := range namespaces {
		out = append(out, &displayNode{
			text:     clicky.Text(ns, "font-bold"),
			children: k8sKindNodes(byNamespace[ns]),
		})
	}
	return out
}

func k8sKindNodes(leaves []*Node) []api.TreeNode {
	byKind := map[string][]*Node{}
	var kinds []string
	for _, leaf := range leaves {
		kind := leaf.prop(propKind)
		if kind == "" {
			kind = "(unknown)"
		}
		if _, seen := byKind[kind]; !seen {
			kinds = append(kinds, kind)
		}
		byKind[kind] = append(byKind[kind], leaf)
	}
	sort.Strings(kinds)

	out := make([]api.TreeNode, 0, len(kinds))
	for _, kind := range kinds {
		kindNode := &displayNode{text: clicky.Text(kind, "text-muted")}
		for _, leaf := range byKind[kind] {
			kindNode.children = append(kindNode.children, &displayNode{text: k8sLeafText(leaf)})
		}
		out = append(out, kindNode)
	}
	return out
}

func k8sLeafText(node *Node) api.Text {
	t := clicky.Text(node.Name, "font-bold text-cyan-600")
	if node.Version != "" {
		t = t.Append("@"+node.Version, "font-mono text-muted")
	}
	if loc := k8sLeafLocation(node); loc != "" {
		t = t.Space().Append("("+loc+")", "text-muted")
	}
	t = appendTags(t, statusTags(node))
	if node.Path != "" {
		t = t.Space().Append(node.Path, "font-mono text-muted")
	}
	return t
}

func k8sLeafLocation(node *Node) string {
	resource := node.prop(propResource)
	container := node.prop(propContainer)
	switch {
	case resource != "" && container != "":
		return resource + "/" + container
	case resource != "":
		return resource
	default:
		return container
	}
}

func relDisplayPath(base, path string) string {
	if path == "" {
		return ""
	}
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return ""
	}
	return rel
}
