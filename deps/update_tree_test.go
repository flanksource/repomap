package deps

import (
	"regexp"
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

// n and s select a row and target its highest stable version; on a directory
// they apply to every row beneath it.
func TestUpdateChoiceTreeStableKeySelectsAndTargetsStable(t *testing.T) {
	for _, key := range []string{"n", "s"} {
		t.Run(key, func(t *testing.T) {
			m := newUpdateChoiceTreeModel([]UpdateChoice{
				npmTreeChoice("left-pad", "^1.3.0", "web/package.json", "1.5.0-rc.1", "1.4.0"),
				npmTreeChoice("typescript", "~5.5.0", "web/package.json", "5.6.0"),
			})
			// The cursor starts on the web/package.json file group, so the key
			// recurses into both dependencies.
			m = updateTreeTestKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})

			selections := m.selections()
			if len(selections) != 2 {
				t.Fatalf("selections = %#v, want both rows selected", selections)
			}
			got := map[string]string{}
			for _, selection := range selections {
				got[selection.Candidate.Name] = selection.Version
			}
			if got["left-pad"] != "1.4.0" {
				t.Fatalf("left-pad = %q, want the highest stable 1.4.0 rather than the 1.5.0-rc.1 pre-release", got["left-pad"])
			}
			if got["typescript"] != "5.6.0" {
				t.Fatalf("typescript = %q, want 5.6.0", got["typescript"])
			}
		})
	}
}

// n reverts a row that had an explicit version picked back to stable.
func TestUpdateChoiceTreeStableKeyClearsExplicitPick(t *testing.T) {
	m := newUpdateChoiceTreeModel([]UpdateChoice{
		npmTreeChoice("left-pad", "^1.3.0", "web/package.json", "1.5.0-rc.1", "1.4.0"),
	})
	node := findUpdateTreeLeaf(t, m.root, "left-pad", "web/package.json")
	node.Selected = true
	node.Mode = updateVersionModeExplicit
	node.Picked = "1.5.0-rc.1"

	m.cursor = updateTreeCursorFor(t, m, node)
	m = updateTreeTestKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})

	if node.Mode != updateVersionModeStable || node.Picked != "" {
		t.Fatalf("node = %#v, want the explicit pick cleared", node)
	}
	if got := m.selections(); len(got) != 1 || got[0].Version != "1.4.0" {
		t.Fatalf("selections = %#v, want left-pad back on the stable 1.4.0", got)
	}
}

// Vim navigation must survive the new version keys.
func TestUpdateChoiceTreeVimNavigationStillExpandsAndCollapses(t *testing.T) {
	m := newUpdateChoiceTreeModel([]UpdateChoice{
		npmTreeChoice("left-pad", "^1.3.0", "web/package.json", "1.4.0"),
	})
	webPackage := updateTreeNodeAtPath(m, "web/package.json")
	if webPackage == nil {
		t.Fatal("web/package.json file group not found")
	}
	m.cursor = updateTreeCursorFor(t, m, webPackage)

	m = updateTreeTestKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	if webPackage.Expanded {
		t.Fatal("h should collapse the file group")
	}
	m = updateTreeTestKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	if !webPackage.Expanded {
		t.Fatal("l should expand the file group")
	}
	if got := m.selections(); len(got) != 0 {
		t.Fatalf("navigation must not select rows, got %#v", got)
	}
}

// v hands the row to the caller rather than prompting from inside the program,
// because bubbletea owns the terminal while it runs.
func TestUpdateChoiceTreeVKeyRequestsVersionPick(t *testing.T) {
	m := newUpdateChoiceTreeModel([]UpdateChoice{
		npmTreeChoice("left-pad", "^1.3.0", "web/package.json", "1.4.0"),
	})
	leaf := findUpdateTreeLeaf(t, m.root, "left-pad", "web/package.json")
	m.cursor = updateTreeCursorFor(t, m, leaf)

	m = updateTreeTestKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	if m.pendingPick != leaf {
		t.Fatalf("pendingPick = %#v, want the left-pad row", m.pendingPick)
	}

	// On a directory row v does nothing — there is no single version to pick.
	m2 := newUpdateChoiceTreeModel([]UpdateChoice{npmTreeChoice("left-pad", "^1.3.0", "web/package.json", "1.4.0")})
	m2.cursor = updateTreeCursorFor(t, m2, updateTreeNodeAtPath(m2, "web/package.json"))
	m2 = updateTreeTestKey(m2, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	if m2.pendingPick != nil {
		t.Fatalf("pendingPick = %#v, want none for a directory row", m2.pendingPick)
	}
}

// The row shows the version it will move to, and the help line advertises the
// version keys.
func TestUpdateChoiceTreeViewShowsTargetVersionAndKeys(t *testing.T) {
	m := newUpdateChoiceTreeModel([]UpdateChoice{
		npmTreeChoice("left-pad", "^1.3.0", "web/package.json", "1.5.0-rc.1", "1.4.0"),
		npmTreeChoice("only-pre", "^2.0.0", "web/package.json", "2.1.0-rc.1"),
	})
	view := stripANSI(m.View())

	if !strings.Contains(view, "n/s=stable") || !strings.Contains(view, "v=pick version") {
		t.Fatalf("help line missing the version keys:\n%s", view)
	}
	if !strings.Contains(view, "→ 1.4.0") {
		t.Fatalf("left-pad row should show its stable target:\n%s", view)
	}
	if !strings.Contains(view, "no stable version - press v") {
		t.Fatalf("pre-release-only row should say so:\n%s", view)
	}

	findUpdateTreeLeaf(t, m.root, "left-pad", "web/package.json").Mode = updateVersionModeExplicit
	findUpdateTreeLeaf(t, m.root, "left-pad", "web/package.json").Picked = "1.5.0-rc.1"
	if view := stripANSI(m.View()); !strings.Contains(view, "→ 1.5.0-rc.1") {
		t.Fatalf("explicit pick should be shown as the target:\n%s", view)
	}
}

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string { return ansiEscape.ReplaceAllString(s, "") }

func updateTreeCursorFor(t *testing.T, m updateChoiceTreeModel, target *updateChoiceTreeNode) int {
	t.Helper()
	for i, node := range m.visible {
		if node == target {
			return i
		}
	}
	t.Fatalf("node %q is not visible", target.Path)
	return 0
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
	selections := m.selections()
	out := make([]string, 0, len(selections))
	for _, selection := range selections {
		out = append(out, selection.Candidate.Name)
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
