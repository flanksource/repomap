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
	if tree, ok := dependencyDisplayTree(e.Roots, e.Metadata.Path); ok {
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
	return nodeText(n, true)
}

func (n *Node) GetChildren() []api.TreeNode {
	if n == nil || len(n.Children) == 0 {
		return nil
	}
	sorted := sortedNodes(n.Children)
	children := make([]api.TreeNode, 0, len(sorted))
	for _, child := range sorted {
		children = append(children, child)
	}
	return children
}

func sortedNodes(nodes []*Node) []*Node {
	out := make([]*Node, 0, len(nodes))
	for _, child := range nodes {
		if child != nil {
			out = append(out, child)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return dependencyLess(out[i], out[j])
	})
	return out
}

type dependencyTag struct {
	label string
	style string
}

func appendTags(t api.Text, tags []dependencyTag) api.Text {
	if len(tags) == 0 {
		return t
	}
	t = t.Space().Append("(", "text-muted")
	for i, tag := range tags {
		if i > 0 {
			t = t.Append(", ", "text-muted")
		}
		t = t.Append(tag.label, tag.style)
	}
	return t.Append(")", "text-muted")
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
		t = appendTags(t, flatNodeTags(node))
		t = t.Space().Append(fmt.Sprintf("depth=%d", node.Depth), "text-muted")
	}
	return t
}

func flatNodeTags(node FlatNode) []dependencyTag {
	var tags []dependencyTag
	if tag, ok := scopeTag(node.Scope); ok {
		tags = append(tags, tag)
	}
	tags = append(tags, flagTags(node.Dev, node.Optional, node.Local)...)
	if tag, ok := replacedTag(node.Manager, node.Source, node.Local, node.Depth); ok {
		tags = append(tags, tag)
	}
	return sortTags(tags)
}

func nodeText(node *Node, showManager bool) api.Text {
	t := clicky.Text("")
	if showManager {
		t = t.Append("[", "text-muted").
			Append(string(node.Manager), managerStyle(node.Manager)).
			Append("] ", "text-muted")
	}
	t = t.Append(node.Name, "font-bold text-cyan-600")
	if node.Version != "" {
		t = t.Append("@"+node.Version, "font-mono text-muted")
	}
	t = appendTags(t, nodeTags(node))
	if node.Path != "" {
		t = t.Space().Append(node.Path, "font-mono text-muted")
	}
	return t
}

func nodeTags(node *Node) []dependencyTag {
	var tags []dependencyTag
	if tag, ok := scopeTag(node.Scope); ok {
		tags = append(tags, tag)
	}
	tags = append(tags, flagTags(node.Dev, node.Optional, node.Local)...)
	if tag, ok := replacedTag(node.Manager, node.Source, node.Local, node.Depth); ok {
		tags = append(tags, tag)
	}
	tags = append(tags, statusTags(node)...)
	return sortTags(tags)
}

// scopeTag renders a scope as a tag unless it is the manager's default
// (require/compile/dependencies), which carries no information.
func scopeTag(scope string) (dependencyTag, bool) {
	if scope == "" || isDefaultScope(scope) {
		return dependencyTag{}, false
	}
	return dependencyTag{label: scope, style: scopeStyle(scope)}, true
}

func isDefaultScope(scope string) bool {
	switch strings.ToLower(scope) {
	case "require", "compile", "dependencies", "dependency":
		return true
	}
	return false
}

// replacedTag marks a Go dependency redirected by a non-local replace directive.
func replacedTag(manager Manager, source string, local bool, depth int) (dependencyTag, bool) {
	if local || depth == 0 || manager != ManagerGo {
		return dependencyTag{}, false
	}
	if source != "" && source != "go.mod" {
		return dependencyTag{label: "replaced", style: "text-purple-600"}, true
	}
	return dependencyTag{}, false
}

func flagTags(dev, optional, local bool) []dependencyTag {
	var tags []dependencyTag
	if dev {
		tags = append(tags, dependencyTag{label: "dev", style: "text-yellow-600"})
	}
	if optional {
		tags = append(tags, dependencyTag{label: "optional", style: "text-purple-600"})
	}
	if local {
		tags = append(tags, dependencyTag{label: "local", style: "text-cyan-600"})
	}
	return tags
}

func statusTags(node *Node) []dependencyTag {
	var tags []dependencyTag
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
	return tags
}

func sortTags(tags []dependencyTag) []dependencyTag {
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
