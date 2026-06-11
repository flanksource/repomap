package deps

import (
	"path/filepath"
	"sort"
	"strings"
)

type ChangeType string

const (
	ChangeAdded   ChangeType = "added"
	ChangeRemoved ChangeType = "removed"
	ChangeUpdated ChangeType = "updated"
)

// Change describes a single dependency difference between two scans, scoped to a
// project (manifest) within the scanned tree.
type Change struct {
	Type       ChangeType `json:"type"`
	Manager    Manager    `json:"manager"`
	Name       string     `json:"name"`
	Project    string     `json:"project"`
	OldVersion string     `json:"old_version,omitempty"`
	NewVersion string     `json:"new_version,omitempty"`
	OldScope   string     `json:"old_scope,omitempty"`
	NewScope   string     `json:"new_scope,omitempty"`
	Direct     bool       `json:"direct,omitempty"`
	Depth      int        `json:"depth,omitempty"`
}

type ComparisonMetadata struct {
	Path    string `json:"path"`
	BaseRef string `json:"base_ref"`
	HeadRef string `json:"head_ref"`
}

type ComparisonStatistics struct {
	Added   int `json:"added"`
	Removed int `json:"removed"`
	Updated int `json:"updated"`
}

// Comparison is the result of diffing two dependency graphs.
type Comparison struct {
	Metadata   ComparisonMetadata   `json:"metadata"`
	Added      []Change             `json:"added,omitempty"`
	Removed    []Change             `json:"removed,omitempty"`
	Updated    []Change             `json:"updated,omitempty"`
	Statistics ComparisonStatistics `json:"statistics"`
	Warnings   []Warning            `json:"warnings,omitempty"`
}

type depEntry struct {
	manager  Manager
	name     string
	versions map[string]bool
	scope    string
	direct   bool
	depth    int
}

// Compare diffs the dependency graphs of two exports. Dependencies are keyed by
// manager+name within each project (manifest), so the same package in different
// projects is compared independently. Duplicate occurrences of a package within
// a project are collapsed into a joined version set.
func Compare(base, head *Export) *Comparison {
	baseIndex := indexExport(base)
	headIndex := indexExport(head)

	comparison := &Comparison{}
	for _, project := range sortedKeys(unionKeys(baseIndex, headIndex)) {
		baseDeps := baseIndex[project]
		headDeps := headIndex[project]
		for _, key := range sortedKeys(unionKeys(baseDeps, headDeps)) {
			before, hasBefore := baseDeps[key]
			after, hasAfter := headDeps[key]
			switch {
			case hasBefore && !hasAfter:
				comparison.Removed = append(comparison.Removed, removedChange(project, before))
			case !hasBefore && hasAfter:
				comparison.Added = append(comparison.Added, addedChange(project, after))
			default:
				if change, ok := updatedChange(project, before, after); ok {
					comparison.Updated = append(comparison.Updated, change)
				}
			}
		}
	}
	comparison.Statistics = ComparisonStatistics{
		Added:   len(comparison.Added),
		Removed: len(comparison.Removed),
		Updated: len(comparison.Updated),
	}
	return comparison
}

func addedChange(project string, e *depEntry) Change {
	return Change{
		Type:       ChangeAdded,
		Manager:    e.manager,
		Name:       e.name,
		Project:    project,
		NewVersion: versionSetString(e.versions),
		NewScope:   e.scope,
		Direct:     e.direct,
		Depth:      e.depth,
	}
}

func removedChange(project string, e *depEntry) Change {
	return Change{
		Type:       ChangeRemoved,
		Manager:    e.manager,
		Name:       e.name,
		Project:    project,
		OldVersion: versionSetString(e.versions),
		OldScope:   e.scope,
		Direct:     e.direct,
		Depth:      e.depth,
	}
}

func updatedChange(project string, before, after *depEntry) (Change, bool) {
	oldVersion := versionSetString(before.versions)
	newVersion := versionSetString(after.versions)
	if oldVersion == newVersion && before.scope == after.scope {
		return Change{}, false
	}
	return Change{
		Type:       ChangeUpdated,
		Manager:    after.manager,
		Name:       after.name,
		Project:    project,
		OldVersion: oldVersion,
		NewVersion: newVersion,
		OldScope:   before.scope,
		NewScope:   after.scope,
		Direct:     after.direct,
		Depth:      after.depth,
	}, true
}

func indexExport(export *Export) map[string]map[string]*depEntry {
	out := map[string]map[string]*depEntry{}
	if export == nil {
		return out
	}
	for _, root := range export.Roots {
		project := normalizeProject(export.Metadata.Path, root)
		deps := out[project]
		if deps == nil {
			deps = map[string]*depEntry{}
			out[project] = deps
		}
		for _, child := range root.Children {
			collectEntries(child, deps)
		}
	}
	return out
}

func collectEntries(node *Node, deps map[string]*depEntry) {
	if node == nil {
		return
	}
	key := string(node.Manager) + ":" + node.Name
	entry := deps[key]
	if entry == nil {
		entry = &depEntry{
			manager:  node.Manager,
			name:     node.Name,
			versions: map[string]bool{},
			scope:    node.Scope,
			direct:   node.Direct,
			depth:    node.Depth,
		}
		deps[key] = entry
	} else {
		if node.Depth < entry.depth {
			entry.depth = node.Depth
		}
		if node.Direct {
			entry.direct = true
		}
		if entry.scope == "" {
			entry.scope = node.Scope
		}
	}
	if node.Version != "" {
		entry.versions[node.Version] = true
	}
	for _, child := range node.Children {
		collectEntries(child, deps)
	}
}

func normalizeProject(basePath string, root *Node) string {
	target := root.Path
	if target == "" {
		return root.Name
	}
	if basePath == "" {
		return filepath.ToSlash(target)
	}
	rel, err := filepath.Rel(basePath, target)
	if err != nil {
		return filepath.ToSlash(target)
	}
	return filepath.ToSlash(rel)
}

func versionSetString(versions map[string]bool) string {
	if len(versions) == 0 {
		return ""
	}
	list := make([]string, 0, len(versions))
	for version := range versions {
		list = append(list, version)
	}
	sort.Strings(list)
	return strings.Join(list, ", ")
}

func unionKeys[V any](a, b map[string]V) map[string]bool {
	keys := map[string]bool{}
	for key := range a {
		keys[key] = true
	}
	for key := range b {
		keys[key] = true
	}
	return keys
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
