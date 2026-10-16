package deps

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/task"
)

const (
	updateTreeStyleHeader       = "font-bold"
	updateTreeStyleHelp         = "text-muted"
	updateTreeStyleMuted        = "text-muted"
	updateTreeStyleCursor       = "text-cyan-600 font-bold"
	updateTreeStyleCheckOn      = "text-green-500 font-bold"
	updateTreeStyleCheckPartial = "text-muted"
)

func updateTreeStyled(s, style string) string {
	return clicky.Text(s, style).ANSI()
}

// updateVersionMode is how a row's target version is decided. Rows default to
// the highest stable version; picking one explicitly is the only other case.
type updateVersionMode int

const (
	updateVersionModeStable updateVersionMode = iota
	updateVersionModeExplicit
)

type updateChoiceTreeNode struct {
	Name      string
	Path      string
	Depth     int
	IsDir     bool
	FileGroup bool
	Expanded  bool
	Selected  bool
	Mode      updateVersionMode
	Picked    string
	Children  []*updateChoiceTreeNode
	Choice    *UpdateChoice
	Parent    *updateChoiceTreeNode
}

// resolvedVersion is the version this row will be updated to, or "" when the
// row has only pre-releases available and none has been picked.
func (n *updateChoiceTreeNode) resolvedVersion() string {
	if n.Choice == nil {
		return ""
	}
	if n.Mode == updateVersionModeExplicit {
		return n.Picked
	}
	return n.Choice.LatestStable
}

type updateChoiceTreeModel struct {
	root        *updateChoiceTreeNode
	visible     []*updateChoiceTreeNode
	cursor      int
	width       int
	height      int
	filtering   bool
	filterQuery string
	cancelled   bool
	submitted   bool
	// pendingPick asks the caller to run the version picker for this row and
	// resume; bubbletea owns the terminal while it runs, so the prompt cannot
	// be opened from inside the model.
	pendingPick *updateChoiceTreeNode
}

func newUpdateChoiceTreeModel(choices []UpdateChoice) updateChoiceTreeModel {
	root := &updateChoiceTreeNode{Name: "", Path: "", IsDir: true, Expanded: true}
	for _, choice := range choices {
		insertUpdateChoice(root, choice)
	}
	sortUpdateChoiceTree(root)
	m := updateChoiceTreeModel{root: root, height: 20, width: 80}
	m.rebuildVisible()
	return m
}

func insertUpdateChoice(root *updateChoiceTreeNode, choice UpdateChoice) {
	filePath := cleanUpdateTreePath(choice.Candidate.File)
	parts := strings.Split(filePath, "/")
	current := root
	for i, segment := range parts {
		if segment == "" {
			continue
		}
		isLast := i == len(parts)-1
		child := findUpdateTreeChild(current, segment, isLast)
		if child == nil {
			path := strings.Join(parts[:i+1], "/")
			child = &updateChoiceTreeNode{
				Name:      segment,
				Path:      path,
				Depth:     current.Depth + 1,
				IsDir:     true,
				FileGroup: isLast,
				Expanded:  true,
				Parent:    current,
			}
			current.Children = append(current.Children, child)
		}
		current = child
	}
	if current == root {
		current = &updateChoiceTreeNode{
			Name:      "(unknown file)",
			Path:      "(unknown file)",
			Depth:     1,
			IsDir:     true,
			FileGroup: true,
			Expanded:  true,
			Parent:    root,
		}
		root.Children = append(root.Children, current)
	}

	choiceCopy := choice
	leaf := &updateChoiceTreeNode{
		Name:   choice.Candidate.Name,
		Path:   choice.Candidate.key(),
		Depth:  current.Depth + 1,
		IsDir:  false,
		Choice: &choiceCopy,
		Parent: current,
	}
	current.Children = append(current.Children, leaf)
}

func cleanUpdateTreePath(file string) string {
	file = filepath.ToSlash(strings.TrimSpace(file))
	file = strings.TrimPrefix(file, "./")
	file = strings.TrimPrefix(file, "/")
	if file == "" {
		return "(unknown file)"
	}
	return file
}

func findUpdateTreeChild(n *updateChoiceTreeNode, name string, fileGroup bool) *updateChoiceTreeNode {
	for _, child := range n.Children {
		if child.Name == name && child.IsDir && child.FileGroup == fileGroup {
			return child
		}
	}
	return nil
}

func sortUpdateChoiceTree(n *updateChoiceTreeNode) {
	sort.SliceStable(n.Children, func(i, j int) bool {
		a, b := n.Children[i], n.Children[j]
		if a.IsDir != b.IsDir {
			return a.IsDir && !b.IsDir
		}
		if a.FileGroup != b.FileGroup {
			return !a.FileGroup && b.FileGroup
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.Choice != nil && b.Choice != nil {
			return a.Choice.Candidate.less(b.Choice.Candidate)
		}
		return a.Path < b.Path
	})
	for _, child := range n.Children {
		if child.IsDir {
			sortUpdateChoiceTree(child)
		}
	}
}

func (m *updateChoiceTreeModel) rebuildVisible() {
	m.visible = m.visible[:0]
	query := normalizedUpdateTreeFilter(m.filterQuery)
	if query != "" {
		for _, child := range m.root.Children {
			appendUpdateTreeVisibleFiltered(&m.visible, child, query)
		}
		if len(m.visible) == 0 {
			m.cursor = 0
			return
		}
		if m.cursor >= len(m.visible) {
			m.cursor = len(m.visible) - 1
		}
		return
	}
	for _, child := range m.root.Children {
		appendUpdateTreeVisible(&m.visible, child)
	}
	if m.cursor >= len(m.visible) {
		m.cursor = max(0, len(m.visible)-1)
	}
}

func appendUpdateTreeVisible(out *[]*updateChoiceTreeNode, n *updateChoiceTreeNode) {
	*out = append(*out, n)
	if n.IsDir && n.Expanded {
		for _, child := range n.Children {
			appendUpdateTreeVisible(out, child)
		}
	}
}

func appendUpdateTreeVisibleFiltered(out *[]*updateChoiceTreeNode, n *updateChoiceTreeNode, query string) bool {
	if updateTreeNodeMatchesFilter(n, query) {
		*out = append(*out, n)
		if n.IsDir {
			for _, child := range n.Children {
				appendUpdateTreeVisibleAll(out, child)
			}
		}
		return true
	}
	if !n.IsDir {
		return false
	}

	var childMatches []*updateChoiceTreeNode
	for _, child := range n.Children {
		var visibleChild []*updateChoiceTreeNode
		if appendUpdateTreeVisibleFiltered(&visibleChild, child, query) {
			childMatches = append(childMatches, visibleChild...)
		}
	}
	if len(childMatches) == 0 {
		return false
	}
	*out = append(*out, n)
	*out = append(*out, childMatches...)
	return true
}

func appendUpdateTreeVisibleAll(out *[]*updateChoiceTreeNode, n *updateChoiceTreeNode) {
	*out = append(*out, n)
	if n.IsDir {
		for _, child := range n.Children {
			appendUpdateTreeVisibleAll(out, child)
		}
	}
}

func normalizedUpdateTreeFilter(query string) string {
	return strings.ToLower(strings.TrimSpace(query))
}

func updateTreeNodeMatchesFilter(n *updateChoiceTreeNode, query string) bool {
	if query == "" {
		return true
	}
	haystack := strings.ToLower(n.Path + " " + n.Name)
	if n.Choice != nil {
		choice := *n.Choice
		candidate := choice.Candidate
		haystack += " " + strings.ToLower(strings.Join([]string{
			candidate.Name,
			string(candidate.Manager),
			string(candidate.Manager) + ":" + candidate.Name,
			candidate.Current,
			candidate.Scope,
			candidate.File,
			choice.LatestStable,
			n.resolvedVersion(),
			strings.Join(choice.Versions, " "),
		}, " "))
	}
	return strings.Contains(haystack, query)
}

func (m updateChoiceTreeModel) Init() tea.Cmd { return nil }

func (m updateChoiceTreeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m updateChoiceTreeModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filtering {
		return m.handleFilterKey(msg)
	}
	switch msg.String() {
	case "ctrl+c", "esc", "q":
		m.cancelled = true
		return m, tea.Quit
	case "/":
		m.filtering = true
	case "enter":
		m.submitted = true
		return m, tea.Quit
	case "down", "j":
		if m.cursor < len(m.visible)-1 {
			m.cursor++
		}
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "right", "l":
		if n := m.currentNode(); n != nil && n.IsDir && !n.Expanded {
			n.Expanded = true
			m.rebuildVisible()
		}
	case "left", "h":
		if n := m.currentNode(); n != nil && n.IsDir && n.Expanded {
			n.Expanded = false
			m.rebuildVisible()
		} else if n != nil && n.Parent != nil && n.Parent != m.root {
			m.moveCursorTo(n.Parent)
		}
	case " ":
		if n := m.currentNode(); n != nil {
			toggleUpdateTreeNode(n)
		}
	case "a":
		if n := m.currentNode(); n != nil {
			toggleUpdateTreeNode(n.containerOrSelf())
		}
	case "ctrl+a":
		toggleUpdateTreeNode(m.root)
	case "n", "s":
		// Select the row (or every row under a directory) and target the
		// highest stable version, discarding any explicit pick.
		if n := m.currentNode(); n != nil {
			selectUpdateTreeStable(n)
		}
	case "v":
		if n := m.currentNode(); n != nil && !n.IsDir && n.Choice != nil && (!n.Choice.UpToDate || n.Choice.Candidate.Manager == ManagerGo || n.Choice.Candidate.git != nil) {
			m.pendingPick = n
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m updateChoiceTreeModel) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		m.cancelled = true
		return m, tea.Quit
	case tea.KeyEsc:
		m.filtering = false
		m.filterQuery = ""
		m.rebuildVisible()
		return m, nil
	case tea.KeyEnter:
		m.filtering = false
		return m, nil
	case tea.KeyBackspace:
		m.filterQuery = trimLastUpdateTreeRune(m.filterQuery)
		m.rebuildVisible()
		return m, nil
	case tea.KeyCtrlU:
		m.filterQuery = ""
		m.rebuildVisible()
		return m, nil
	}
	if len(msg.Runes) > 0 {
		m.filterQuery += string(msg.Runes)
		m.rebuildVisible()
	}
	return m, nil
}

func trimLastUpdateTreeRune(s string) string {
	if s == "" {
		return ""
	}
	runes := []rune(s)
	return string(runes[:len(runes)-1])
}

func (m updateChoiceTreeModel) currentNode() *updateChoiceTreeNode {
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return nil
	}
	return m.visible[m.cursor]
}

func (m *updateChoiceTreeModel) moveCursorTo(target *updateChoiceTreeNode) {
	for i, node := range m.visible {
		if node == target {
			m.cursor = i
			return
		}
	}
}

func (n *updateChoiceTreeNode) containerOrSelf() *updateChoiceTreeNode {
	if n.IsDir {
		return n
	}
	if n.Parent != nil {
		return n.Parent
	}
	return n
}

func toggleUpdateTreeNode(n *updateChoiceTreeNode) {
	target := !allUpdateTreeLeavesSelected(n)
	setUpdateTreeLeavesSelected(n, target)
}

func allUpdateTreeLeavesSelected(n *updateChoiceTreeNode) bool {
	if !n.IsDir {
		return n.Selected
	}
	if len(n.Children) == 0 {
		return false
	}
	for _, child := range n.Children {
		if !allUpdateTreeLeavesSelected(child) {
			return false
		}
	}
	return true
}

func anyUpdateTreeLeafSelected(n *updateChoiceTreeNode) bool {
	if !n.IsDir {
		return n.Selected
	}
	for _, child := range n.Children {
		if anyUpdateTreeLeafSelected(child) {
			return true
		}
	}
	return false
}

func setUpdateTreeLeavesSelected(n *updateChoiceTreeNode, selected bool) {
	if !n.IsDir {
		n.Selected = selected
		return
	}
	for _, child := range n.Children {
		setUpdateTreeLeavesSelected(child, selected)
	}
}

// selectUpdateTreeStable marks every leaf under n for update at its highest
// stable version.
func selectUpdateTreeStable(n *updateChoiceTreeNode) {
	if !n.IsDir {
		n.Selected = true
		n.Mode = updateVersionModeStable
		n.Picked = ""
		return
	}
	for _, child := range n.Children {
		selectUpdateTreeStable(child)
	}
}

func (m updateChoiceTreeModel) selections() []UpdateSelection {
	var out []UpdateSelection
	var walk func(n *updateChoiceTreeNode)
	walk = func(n *updateChoiceTreeNode) {
		if !n.IsDir {
			if n.Selected && n.Choice != nil {
				out = append(out, UpdateSelection{UpdateChoice: *n.Choice, Version: n.resolvedVersion()})
			}
			return
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(m.root)
	return out
}

func (m updateChoiceTreeModel) View() string {
	var b strings.Builder
	selectedCount := len(m.selections())
	totalLeaves := countUpdateTreeLeaves(m.root)
	visibleLeaves := countVisibleUpdateTreeLeaves(m.visible)
	filterActive := normalizedUpdateTreeFilter(m.filterQuery) != ""

	b.WriteString(updateTreeStyled("Select dependencies to update", updateTreeStyleHeader))
	b.WriteString("  ")
	b.WriteString(updateTreeStyled(fmt.Sprintf("(%d / %d selected)", selectedCount, totalLeaves), updateTreeStyleMuted))
	if filterActive {
		b.WriteString("  ")
		b.WriteString(updateTreeStyled(fmt.Sprintf("filter=%q (%d dependencies)", m.filterQuery, visibleLeaves), updateTreeStyleMuted))
	}
	b.WriteByte('\n')
	if m.filtering {
		b.WriteString(updateTreeStyled(
			fmt.Sprintf("  filter: %s  type=search  backspace=delete  ctrl+u=clear  enter=keep  esc=clear", m.filterQuery),
			updateTreeStyleHelp,
		))
	} else {
		b.WriteString(updateTreeStyled(
			"  /=filter  space=toggle  a=toggle file  ctrl+a=all  n/s=stable  v=pick version  enter=confirm  esc=cancel",
			updateTreeStyleHelp,
		))
	}
	b.WriteString("\n\n")

	pageSize := max(m.height-5, 5)
	start := 0
	if m.cursor >= pageSize {
		start = m.cursor - pageSize + 1
	}
	end := min(start+pageSize, len(m.visible))
	for i := start; i < end; i++ {
		b.WriteString(renderUpdateTreeRow(m.visible[i], i == m.cursor, filterActive))
		b.WriteByte('\n')
	}
	if len(m.visible) == 0 {
		b.WriteString(updateTreeStyled("  No dependencies match the current filter", updateTreeStyleHelp))
		b.WriteByte('\n')
	}
	return b.String()
}

func renderUpdateTreeRow(n *updateChoiceTreeNode, isCursor bool, forceExpanded bool) string {
	cursor := "  "
	if isCursor {
		cursor = updateTreeStyled("> ", updateTreeStyleCursor)
	}
	indent := strings.Repeat("  ", max(0, n.Depth-1))
	check := updateTreeCheckbox(n)
	name := n.Name
	if n.IsDir {
		expand := "v "
		if !forceExpanded && !n.Expanded {
			expand = "> "
		}
		suffix := "/"
		if n.FileGroup {
			suffix = ""
		}
		name = expand + name + suffix
	}
	if isCursor {
		name = updateTreeStyled(name, updateTreeStyleHeader)
	}
	row := fmt.Sprintf("%s%s%s %s", cursor, indent, check, name)
	if !n.IsDir && n.Choice != nil {
		row += "  " + updateTreeChoiceChips(n)
	}
	return row
}

func updateTreeCheckbox(n *updateChoiceTreeNode) string {
	if !n.IsDir {
		if n.Selected {
			return updateTreeStyled("[x]", updateTreeStyleCheckOn)
		}
		return "[ ]"
	}
	switch {
	case allUpdateTreeLeavesSelected(n):
		return updateTreeStyled("[x]", updateTreeStyleCheckOn)
	case anyUpdateTreeLeafSelected(n):
		return updateTreeStyled("[~]", updateTreeStyleCheckPartial)
	default:
		return "[ ]"
	}
}

func updateTreeChoiceChips(n *updateChoiceTreeNode) string {
	choice := *n.Choice
	candidate := choice.Candidate
	parts := []string{
		updateTreeStyled(string(candidate.Manager), managerStyle(candidate.Manager)),
		updateTreeStyled("@"+candidate.Current, "font-mono text-muted"),
	}
	if candidate.catalog != nil {
		parts = append(parts, updateTreeStyled("catalog: "+cwdRelativePath(candidate.catalog.Workspace.File), "text-muted"))
	}
	switch version := n.resolvedVersion(); {
	case version == "" && candidate.git != nil:
		parts = append(parts, updateTreeStyled("press v to choose commit", "text-yellow-600"))
	case version == "" && candidate.Manager == ManagerGo && len(choice.Versions) == 0:
		parts = append(parts, updateTreeStyled("no newer release - press v for commit", "text-yellow-600"))
	case version == "" && choice.UpToDate:
		parts = append(parts, updateTreeStyled("up to date", "text-muted"))
	case version == "":
		parts = append(parts, updateTreeStyled("no stable version - press v", "text-yellow-600"))
	case n.Mode == updateVersionModeExplicit:
		parts = append(parts, updateTreeStyled("→ "+version, "font-mono text-cyan-600 font-bold"))
	default:
		parts = append(parts, updateTreeStyled("→ "+version, "font-mono text-green-600"))
	}
	if choice.LatestPrerelease != "" && choice.LatestPrerelease != n.resolvedVersion() {
		parts = append(parts, updateTreeStyled("pre "+choice.LatestPrerelease, "text-yellow-600"))
	}
	if candidate.Scope != "" {
		parts = append(parts, updateTreeStyled(candidate.Scope, "text-muted"))
	}
	return strings.Join(parts, updateTreeStyled(" * ", updateTreeStyleMuted))
}

func countUpdateTreeLeaves(n *updateChoiceTreeNode) int {
	if !n.IsDir {
		return 1
	}
	total := 0
	for _, child := range n.Children {
		total += countUpdateTreeLeaves(child)
	}
	return total
}

func countVisibleUpdateTreeLeaves(nodes []*updateChoiceTreeNode) int {
	total := 0
	for _, node := range nodes {
		if !node.IsDir {
			total++
		}
	}
	return total
}

// runUpdateChoiceTreePicker collects both which dependencies to update and the
// version for each in one screen. Picking an explicit version (`v`) needs the
// terminal bubbletea is holding, so the model quits, the prompt runs, and the
// tree resumes over the same node pointers with its state intact.
func runUpdateChoiceTreePicker(ctx context.Context, runner CommandRunner, choices []UpdateChoice) ([]UpdateSelection, bool, error) {
	if len(choices) == 0 {
		return nil, false, nil
	}
	model := newUpdateChoiceTreeModel(choices)
	if len(model.visible) == 0 {
		return nil, false, nil
	}

	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, false, fmt.Errorf("open dependency picker terminal: %w", err)
	}
	defer func() { _ = tty.Close() }()

	for {
		finished, err := runUpdateChoiceTree(model, tty)
		if errors.Is(err, tea.ErrInterrupted) || finished.cancelled {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, fmt.Errorf("run dependency selection tree: %w", err)
		}
		if node := finished.pendingPick; node != nil {
			var pickErr error
			pick := func(prompt UpdateVersionPrompt) (string, bool) {
				version, chosen, err := promptUpdateVersionWithGit(ctx, runner, prompt)
				pickErr = err
				return version, chosen
			}
			promptUpdateTreeVersion(finished.root, node, pick)
			if pickErr != nil {
				return nil, false, pickErr
			}
			model = finished
			model.pendingPick = nil
			model.submitted = false
			model.rebuildVisible()
			continue
		}
		if !finished.submitted {
			return nil, false, nil
		}
		return finished.selections(), true, nil
	}
}

// runUpdateChoiceTree renders one pass of the tree, holding the ANSI terminal
// only for as long as the program runs so a follow-up prompt can claim it.
func runUpdateChoiceTree(model updateChoiceTreeModel, tty *os.File) (updateChoiceTreeModel, error) {
	releaseTerminal, _ := task.AcquirePromptTerminal()
	if releaseTerminal != nil {
		defer releaseTerminal()
	}

	final, err := tea.NewProgram(model, tea.WithInput(tty), tea.WithOutput(tty), tea.WithAltScreen()).Run()
	if err != nil {
		return updateChoiceTreeModel{}, err
	}
	finished, ok := final.(updateChoiceTreeModel)
	if !ok {
		return updateChoiceTreeModel{}, fmt.Errorf("unexpected dependency selection tree result %T", final)
	}
	return finished, nil
}

// promptUpdateTreeVersion asks the version question node poses and records the
// answer on every row posing the identical question — the same dependency at the
// same current version is one decision however many manifests declare it.
// Declining leaves those rows on whatever they were already targeting.
func promptUpdateTreeVersion(root, node *updateChoiceTreeNode, pick VersionSelector) {
	group := updateTreeRowsSharingVersionQuestion(root, node)
	choices := make([]UpdateChoice, 0, len(group))
	for _, row := range group {
		choices = append(choices, *row.Choice)
	}
	version, ok := pick(newUpdateVersionPrompt(choices))
	if !ok || strings.TrimSpace(version) == "" {
		return
	}
	for _, row := range group {
		row.Selected = true
		row.Mode = updateVersionModeExplicit
		row.Picked = strings.TrimSpace(version)
	}
}

// updateTreeRowsSharingVersionQuestion returns every leaf posing node's version
// question, in tree order so the prompt lists manifests deterministically.
func updateTreeRowsSharingVersionQuestion(root, node *updateChoiceTreeNode) []*updateChoiceTreeNode {
	want := node.Choice.versionPromptKey()
	var out []*updateChoiceTreeNode
	var walk func(n *updateChoiceTreeNode)
	walk = func(n *updateChoiceTreeNode) {
		if !n.IsDir {
			if n.Choice != nil && n.Choice.versionPromptKey() == want {
				out = append(out, n)
			}
			return
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(root)
	return out
}

// selectUpdateVersion is a var so tree tests can answer the picker without a tty.
var selectUpdateVersion = promptUpdateVersion
