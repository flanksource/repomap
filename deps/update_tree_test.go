package deps

import (
	"sort"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestUpdateChoiceTreeGroupsDependenciesUnderFiles(t *testing.T) {
	m := newUpdateChoiceTreeModel([]UpdateChoice{
		{Candidate: UpdateCandidate{Manager: ManagerNPM, Name: "zeta", Current: "1.0.0", Scope: "dependencies", File: "web/package.json"}},
		{Candidate: UpdateCandidate{Manager: ManagerGo, Name: "github.com/acme/lib", Current: "v1.0.0", Scope: "require", File: "go.mod"}},
		{Candidate: UpdateCandidate{Manager: ManagerNPM, Name: "alpha", Current: "1.0.0", Scope: "dependencies", File: "web/package.json"}},
	})

	goMod := updateTreeNodeAtPath(m, "go.mod")
	if goMod == nil || !goMod.FileGroup {
		t.Fatalf("go.mod file group not found: %#v", updateTreeVisiblePaths(m))
	}
	webPackage := updateTreeNodeAtPath(m, "web/package.json")
	if webPackage == nil || !webPackage.FileGroup {
		t.Fatalf("web/package.json file group not found: %#v", updateTreeVisiblePaths(m))
	}
	got := updateTreeChildNames(webPackage)
	if strings.Join(got, ",") != "alpha,zeta" {
		t.Fatalf("web package choices = %#v, want alpha,zeta", got)
	}
}

func TestUpdateChoiceTreeCtrlASelectsAndClearsAll(t *testing.T) {
	m := newUpdateChoiceTreeModel([]UpdateChoice{
		{Candidate: UpdateCandidate{Manager: ManagerGo, Name: "github.com/acme/lib", File: "go.mod"}},
		{Candidate: UpdateCandidate{Manager: ManagerNPM, Name: "left-pad", File: "web/package.json"}},
		{Candidate: UpdateCandidate{Manager: ManagerNPM, Name: "typescript", File: "web/package.json"}},
	})

	m = updateTreeTestKey(m, tea.KeyMsg{Type: tea.KeyCtrlA})
	got := selectedUpdateTreeNames(m)
	if strings.Join(got, ",") != "github.com/acme/lib,left-pad,typescript" {
		t.Fatalf("selected after ctrl+a = %#v", got)
	}

	m = updateTreeTestKey(m, tea.KeyMsg{Type: tea.KeyCtrlA})
	if got := selectedUpdateTreeNames(m); len(got) != 0 {
		t.Fatalf("selected after second ctrl+a = %#v, want none", got)
	}
}

func TestUpdateChoiceTreeFilterMatchesDependencyAndFile(t *testing.T) {
	m := newUpdateChoiceTreeModel([]UpdateChoice{
		{Candidate: UpdateCandidate{Manager: ManagerGo, Name: "github.com/acme/lib", File: "go.mod"}},
		{Candidate: UpdateCandidate{Manager: ManagerNPM, Name: "@flanksource/ui", File: "web/package.json"}},
		{Candidate: UpdateCandidate{Manager: ManagerNPM, Name: "typescript", File: "web/package.json"}},
	})

	m = updateTreeTestKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updateTreeTestKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("flanksource")})
	if got := updateTreeVisibleLeafNames(m); strings.Join(got, ",") != "@flanksource/ui" {
		t.Fatalf("visible leaves for dependency filter = %#v", got)
	}

	m = updateTreeTestKey(m, tea.KeyMsg{Type: tea.KeyCtrlU})
	m = updateTreeTestKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("go.mod")})
	if got := updateTreeVisibleLeafNames(m); strings.Join(got, ",") != "github.com/acme/lib" {
		t.Fatalf("visible leaves for file filter = %#v", got)
	}
}

func updateTreeTestKey(m updateChoiceTreeModel, msg tea.KeyMsg) updateChoiceTreeModel {
	updated, _ := m.handleKey(msg)
	return updated.(updateChoiceTreeModel)
}

func updateTreeNodeAtPath(m updateChoiceTreeModel, path string) *updateChoiceTreeNode {
	var found *updateChoiceTreeNode
	var walk func(*updateChoiceTreeNode)
	walk = func(node *updateChoiceTreeNode) {
		if found != nil {
			return
		}
		if node.Path == path {
			found = node
			return
		}
		for _, child := range node.Children {
			walk(child)
		}
	}
	walk(m.root)
	return found
}

func updateTreeChildNames(node *updateChoiceTreeNode) []string {
	out := make([]string, 0, len(node.Children))
	for _, child := range node.Children {
		out = append(out, child.Name)
	}
	return out
}

func selectedUpdateTreeNames(m updateChoiceTreeModel) []string {
	choices := m.selectedChoices()
	out := make([]string, 0, len(choices))
	for _, choice := range choices {
		out = append(out, choice.Candidate.Name)
	}
	sort.Strings(out)
	return out
}

func updateTreeVisibleLeafNames(m updateChoiceTreeModel) []string {
	var out []string
	for _, node := range m.visible {
		if node.IsDir || node.Choice == nil {
			continue
		}
		out = append(out, node.Choice.Candidate.Name)
	}
	sort.Strings(out)
	return out
}

func updateTreeVisiblePaths(m updateChoiceTreeModel) []string {
	out := make([]string, 0, len(m.visible))
	for _, node := range m.visible {
		out = append(out, node.Path)
	}
	return out
}
