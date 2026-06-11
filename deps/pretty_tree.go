package deps

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/clicky/api/icons"
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

func (d *displayNode) Pretty() api.Text            { return d.text }
func (d *displayNode) GetChildren() []api.TreeNode { return d.children }

// dependencyDisplayTree renders package roots with their manager prefix and
// promotes kubernetes (image/helm) dependencies into a unified Namespace → Kind
// → resource grouping, dropping the synthetic "container images"/"helm charts"
// root wrappers.
func dependencyDisplayTree(roots []*Node, scanPath string) (api.TextTree, bool) {
	var top []api.TreeNode
	var k8sLeaves []*Node
	for _, root := range roots {
		if root == nil {
			continue
		}
		switch root.Manager {
		case ManagerImage, ManagerHelm:
			k8sLeaves = append(k8sLeaves, root.Children...)
		default:
			top = append(top, rootDisplayNode(root, scanPath))
		}
	}
	top = append(top, namespaceNodes(k8sLeaves)...)
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
	return &displayNode{text: label, children: packageDisplayNodes(root.Children)}
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

// namespaceNodes is the top level of the kubernetes grouping.
func namespaceNodes(leaves []*Node) []api.TreeNode {
	keys, groups := groupLeaves(sortedNodes(leaves), func(n *Node) string {
		return orDefault(n.prop(propNamespace), "(none)")
	})
	out := make([]api.TreeNode, 0, len(keys))
	for _, ns := range keys {
		label := clicky.Text("").AddIcon(icons.Kubernetes).Space().Append(ns, "font-bold")
		out = append(out, &displayNode{text: label, children: kindNodes(groups[ns])})
	}
	return out
}

func kindNodes(leaves []*Node) []api.TreeNode {
	keys, groups := groupLeaves(leaves, func(n *Node) string {
		return orDefault(n.prop(propKind), "(unknown)")
	})
	out := make([]api.TreeNode, 0, len(keys))
	for _, kind := range keys {
		out = append(out, &displayNode{
			text:     clicky.Text(kind, "text-white"),
			children: resourceNodes(groups[kind]),
		})
	}
	return out
}

// resourceNodes groups leaves by resource name and source file, labelling each
// with the kind's resource name and a shortened path.
func resourceNodes(leaves []*Node) []api.TreeNode {
	keys, groups := groupLeaves(leaves, func(n *Node) string {
		return orDefault(n.prop(propResource), "(unnamed)") + "\x00" + n.Path
	})
	out := make([]api.TreeNode, 0, len(keys))
	for _, key := range keys {
		members := groups[key]
		label := clicky.Text(orDefault(members[0].prop(propResource), "(unnamed)"), "font-bold text-cyan-600")
		if sp := shortPath(members[0].Path); sp != "" {
			label = label.Space().Append("("+sp+")", "font-mono text-muted")
		}
		node := &displayNode{text: label}
		for _, leaf := range sortedNodes(members) {
			node.children = append(node.children, &displayNode{text: k8sLeafText(leaf)})
		}
		out = append(out, node)
	}
	return out
}

func k8sLeafText(node *Node) api.Text {
	t := clicky.Text(node.Name, "font-bold text-cyan-600")
	if node.Version != "" {
		t = t.Append("@"+node.Version, "font-mono text-muted")
	}
	if container := node.prop(propContainer); container != "" {
		t = t.Space().Append("("+container+")", "text-muted")
	}
	return appendTags(t, statusTags(node))
}

// groupLeaves buckets leaves by key, returning the keys in sorted order.
func groupLeaves(leaves []*Node, key func(*Node) string) ([]string, map[string][]*Node) {
	groups := map[string][]*Node{}
	var keys []string
	for _, leaf := range leaves {
		k := key(leaf)
		if _, seen := groups[k]; !seen {
			keys = append(keys, k)
		}
		groups[k] = append(groups[k], leaf)
	}
	sort.Strings(keys)
	return keys, groups
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func shortPath(path string) string {
	if path == "" {
		return ""
	}
	path = filepath.ToSlash(path)
	parts := strings.Split(path, "/")
	if len(parts) <= 2 {
		return path
	}
	return strings.Join(parts[len(parts)-2:], "/")
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
