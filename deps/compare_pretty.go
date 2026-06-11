package deps

import (
	"fmt"
	"sort"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
)

// diffNode adapts a comparison group (project header or change leaf) to the
// clicky tree renderer.
type diffNode struct {
	text     api.Text
	children []api.TreeNode
}

var _ api.TreeNode = (*diffNode)(nil)

func (d *diffNode) Pretty() api.Text { return d.text }

func (d *diffNode) GetChildren() []api.TreeNode { return d.children }

func (c *Comparison) Pretty() api.Text {
	if c == nil {
		return clicky.Text("")
	}
	t := clicky.Text("Dependency Diff", "font-bold")
	t = t.Append(summaryText(c.Statistics))
	if c.Metadata.BaseRef != "" {
		t = t.Append(fmt.Sprintf("  %s..%s", c.Metadata.BaseRef, headRefLabel(c.Metadata.HeadRef)), "text-muted")
	}

	nodes := diffProjectNodes(c)
	if len(nodes) == 0 {
		return t.NewLine().Append("No dependency changes", "text-muted")
	}
	t = t.NewLine().Add(api.NewTree(nodes...))

	if len(c.Warnings) > 0 {
		t = t.NewLine().Append("Warnings", "font-bold text-yellow-600")
		for _, w := range c.Warnings {
			t = t.NewLine().Append("- "+w.Message, "text-yellow-600")
		}
	}
	return t
}

func summaryText(stats ComparisonStatistics) api.Text {
	return clicky.Text("  +", "text-muted").
		Append(fmt.Sprintf("%d", stats.Added), "text-green-600").
		Append(" -", "text-muted").
		Append(fmt.Sprintf("%d", stats.Removed), "text-red-600").
		Append(" ~", "text-muted").
		Append(fmt.Sprintf("%d", stats.Updated), "text-yellow-600")
}

func headRefLabel(headRef string) string {
	if headRef == "" {
		return "working tree"
	}
	return headRef
}

func diffProjectNodes(c *Comparison) []api.TreeNode {
	byProject := map[string][]Change{}
	for _, change := range allChanges(c) {
		byProject[change.Project] = append(byProject[change.Project], change)
	}
	projects := make([]string, 0, len(byProject))
	for project := range byProject {
		projects = append(projects, project)
	}
	sort.Strings(projects)

	nodes := make([]api.TreeNode, 0, len(projects))
	for _, project := range projects {
		changes := byProject[project]
		sort.SliceStable(changes, func(i, j int) bool {
			if changes[i].Manager != changes[j].Manager {
				return changes[i].Manager < changes[j].Manager
			}
			return changes[i].Name < changes[j].Name
		})
		projectNode := &diffNode{text: clicky.Text(project, "font-bold")}
		for _, change := range changes {
			projectNode.children = append(projectNode.children, &diffNode{text: changeText(change)})
		}
		nodes = append(nodes, projectNode)
	}
	return nodes
}

func allChanges(c *Comparison) []Change {
	out := make([]Change, 0, len(c.Added)+len(c.Removed)+len(c.Updated))
	out = append(out, c.Added...)
	out = append(out, c.Removed...)
	out = append(out, c.Updated...)
	return out
}

func changeText(change Change) api.Text {
	t := clicky.Text("[", "text-muted").
		Append(string(change.Manager), managerStyle(change.Manager)).
		Append("] ", "text-muted")
	switch change.Type {
	case ChangeAdded:
		t = t.Append(change.Name, "font-bold text-green-600")
		if change.NewVersion != "" {
			t = t.Append("@"+change.NewVersion, "font-mono text-green-600")
		}
		t = t.Space().Append("(new)", "text-green-600")
	case ChangeRemoved:
		t = t.Append(change.Name, "font-bold line-through text-red-600")
		if change.OldVersion != "" {
			t = t.Append("@"+change.OldVersion, "font-mono line-through text-red-600")
		}
		t = t.Space().Append("(removed)", "text-red-600")
	case ChangeUpdated:
		t = t.Append(change.Name, "font-bold text-cyan-600").
			Space().Append(versionOrNone(change.OldVersion), "font-mono text-muted").
			Append(" → ", "text-yellow-600").
			Append(versionOrNone(change.NewVersion), "font-mono text-yellow-600")
		if change.OldScope != change.NewScope {
			t = t.Space().Append(fmt.Sprintf("(%s → %s)", change.OldScope, change.NewScope), "text-muted")
		}
	}
	return t
}

func versionOrNone(version string) string {
	if version == "" {
		return "(none)"
	}
	return version
}

// Columns implements the clicky TableProvider interface for flat change tables.
func (Change) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		api.Column("type").Label("Type").Build(),
		api.Column("manager").Label("Manager").Build(),
		api.Column("dependency").Label("Dependency").Build(),
		api.Column("project").Label("Project").Build(),
		api.Column("change").Label("Change").Build(),
	}
}

func (change Change) Row() map[string]any {
	return map[string]any{
		"type":       clicky.Text(string(change.Type), changeTypeStyle(change.Type)),
		"manager":    clicky.Text(string(change.Manager), managerStyle(change.Manager)),
		"dependency": clicky.Text(change.Name, "font-bold text-cyan-600"),
		"project":    clicky.Text(change.Project, "font-mono"),
		"change":     changeText(change),
	}
}

func changeTypeStyle(changeType ChangeType) string {
	switch changeType {
	case ChangeAdded:
		return "text-green-600"
	case ChangeRemoved:
		return "text-red-600"
	default:
		return "text-yellow-600"
	}
}
