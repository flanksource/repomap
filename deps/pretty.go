package deps

import (
	"fmt"
	"sort"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
)

func (e *Export) Pretty() api.Text {
	if e == nil {
		return clicky.Text("")
	}
	t := clicky.Text("Dependency Graph", "font-bold")
	t = t.Append(fmt.Sprintf("  projects=%d nodes=%d edges=%d", e.Statistics.Projects, e.Statistics.Total, e.Statistics.Edges), "text-muted")
	if tree, ok := dependencyTree(e.Roots); ok {
		t = t.NewLine().Add(tree)
	} else if len(e.Nodes) > 0 {
		t = t.NewLine().Add(flatNodesText(e.Nodes))
	}
	if len(e.Warnings) > 0 {
		t = t.NewLine().Append("Warnings", "font-bold text-yellow-600")
		for _, w := range e.Warnings {
			msg := w.Message
			if w.Project != "" {
				msg = w.Project + ": " + msg
			}
			if w.Manager != "" {
				msg = "[" + string(w.Manager) + "] " + msg
			}
			t = t.NewLine().Append("- "+msg, "text-yellow-600")
		}
	}
	if len(e.Duplicates) > 0 {
		t = t.NewLine().Append("Duplicates", "font-bold")
		for _, d := range e.Duplicates {
			label := fmt.Sprintf("- %s [%s] count=%d", d.Name, d.Manager, d.Count)
			if d.Conflicts {
				label += " conflicts"
			}
			t = t.NewLine().Append(label, "text-muted")
		}
	}
	return t
}

var _ api.TreeNode = (*Node)(nil)

func (n *Node) Pretty() api.Text {
	if n == nil {
		return clicky.Text("")
	}
	return nodeText(n)
}

func (n *Node) GetChildren() []api.TreeNode {
	if n == nil || len(n.Children) == 0 {
		return nil
	}
	nodeChildren := make([]*Node, 0, len(n.Children))
	for _, child := range n.Children {
		if child != nil {
			nodeChildren = append(nodeChildren, child)
		}
	}
	sort.SliceStable(nodeChildren, func(i, j int) bool {
		return dependencyLess(nodeChildren[i], nodeChildren[j])
	})
	children := make([]api.TreeNode, 0, len(nodeChildren))
	for _, child := range nodeChildren {
		children = append(children, child)
	}
	return children
}

func dependencyTree(roots []*Node) (api.TextTree, bool) {
	if len(roots) == 0 {
		return api.TextTree{}, false
	}
	nodes := make([]api.TreeNode, 0, len(roots))
	for _, root := range roots {
		if root != nil {
			nodes = append(nodes, root)
		}
	}
	if len(nodes) == 0 {
		return api.TextTree{}, false
	}
	return api.NewTree(nodes...), true
}

type dependencyTag struct {
	label string
	style string
}

func flatNodesText(nodes []FlatNode) api.Text {
	t := clicky.Text("")
	for i, node := range nodes {
		if i > 0 {
			t = t.NewLine()
		}
		t = t.Append("[", "text-muted").
			Append(string(node.Manager), managerStyle(node.Manager)).
			Append("] ", "text-muted").
			Append(node.Name, "font-bold text-cyan-600")
		if node.Version != "" {
			t = t.Append("@"+node.Version, "font-mono text-muted")
		}
		tags := flatNodeTags(node)
		if len(tags) > 0 {
			t = t.Space().Append("(", "text-muted")
			for j, tag := range tags {
				if j > 0 {
					t = t.Append(", ", "text-muted")
				}
				t = t.Append(tag.label, tag.style)
			}
			t = t.Append(")", "text-muted")
		}
		t = t.Space().Append(fmt.Sprintf("depth=%d", node.Depth), "text-muted")
	}
	return t
}

func flatNodeTags(node FlatNode) []dependencyTag {
	var tags []dependencyTag
	if node.Scope != "" {
		tags = append(tags, dependencyTag{label: node.Scope, style: scopeStyle(node.Scope)})
	}
	if node.Direct {
		tags = append(tags, dependencyTag{label: "direct", style: "text-green-600"})
	}
	if node.Dev {
		tags = append(tags, dependencyTag{label: "dev", style: "text-yellow-600"})
	}
	if node.Optional {
		tags = append(tags, dependencyTag{label: "optional", style: "text-purple-600"})
	}
	if node.Local {
		tags = append(tags, dependencyTag{label: "local", style: "text-cyan-600"})
	}
	sort.Slice(tags, func(i, j int) bool {
		return tags[i].label < tags[j].label
	})
	return tags
}

func nodeText(node *Node) api.Text {
	t := clicky.Text("")
	t = t.Append("[", "text-muted").
		Append(string(node.Manager), managerStyle(node.Manager)).
		Append("] ", "text-muted").
		Append(node.Name, "font-bold text-cyan-600")
	if node.Version != "" {
		t = t.Append("@"+node.Version, "font-mono text-muted")
	}
	tags := nodeTags(node)
	if len(tags) > 0 {
		t = t.Space().Append("(", "text-muted")
		for i, tag := range tags {
			if i > 0 {
				t = t.Append(", ", "text-muted")
			}
			t = t.Append(tag.label, tag.style)
		}
		t = t.Append(")", "text-muted")
	}
	if node.Path != "" {
		t = t.Space().Append(node.Path, "font-mono text-muted")
	}
	return t
}

func nodeTags(node *Node) []dependencyTag {
	var tags []dependencyTag
	if node.Scope != "" {
		tags = append(tags, dependencyTag{label: node.Scope, style: scopeStyle(node.Scope)})
	}
	if node.Direct {
		tags = append(tags, dependencyTag{label: "direct", style: "text-green-600"})
	}
	if node.Dev {
		tags = append(tags, dependencyTag{label: "dev", style: "text-yellow-600"})
	}
	if node.Optional {
		tags = append(tags, dependencyTag{label: "optional", style: "text-purple-600"})
	}
	if node.Local {
		tags = append(tags, dependencyTag{label: "local", style: "text-cyan-600"})
	}
	if node.Circular {
		tags = append(tags, dependencyTag{label: "circular", style: "font-bold text-red-600"})
	}
	if node.Duplicate != nil {
		tag := fmt.Sprintf("dup:%d", node.Duplicate.Count)
		style := "text-orange-500"
		if node.Duplicate.Conflicts {
			tag += ":conflict"
			style = "font-bold text-red-600"
		}
		tags = append(tags, dependencyTag{label: tag, style: style})
	}
	sort.Slice(tags, func(i, j int) bool {
		return tags[i].label < tags[j].label
	})
	return tags
}

func managerStyle(manager Manager) string {
	switch manager {
	case ManagerPNPM:
		return "font-bold text-orange-500"
	case ManagerNPM:
		return "font-bold text-red-500"
	case ManagerGo:
		return "font-bold text-cyan-600"
	case ManagerMaven:
		return "font-bold text-purple-600"
	case ManagerGradle:
		return "font-bold text-green-600"
	case ManagerImage:
		return "font-bold text-blue-600"
	case ManagerHelm:
		return "font-bold text-indigo-600"
	default:
		return "font-bold text-muted"
	}
}

func scopeStyle(scope string) string {
	switch strings.ToLower(scope) {
	case "dependencies", "dependency", "require", "compile":
		return "text-green-600"
	case "devdependencies", "dev", "test", "testdependencies":
		return "text-yellow-600"
	case "optionaldependencies", "optional":
		return "text-purple-600"
	case "peerdependencies", "peer":
		return "text-blue-600"
	case "runtime", "runtimeclasspath", "implementation":
		return "text-cyan-600"
	default:
		return "text-muted"
	}
}
