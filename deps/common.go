package deps

import (
	"path/filepath"
	"sort"
	"strings"
)

func sortChildren(node *Node) {
	if node == nil {
		return
	}
	sort.SliceStable(node.Children, func(i, j int) bool {
		return dependencyLess(node.Children[i], node.Children[j])
	})
}

func dependencyLess(a, b *Node) bool {
	if a == nil {
		return b != nil
	}
	if b == nil {
		return false
	}
	if ar, br := dependencySortRank(a), dependencySortRank(b); ar != br {
		return ar < br
	}
	if an, bn := strings.ToLower(a.Name), strings.ToLower(b.Name); an != bn {
		return an < bn
	}
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	if a.Version != b.Version {
		return a.Version < b.Version
	}
	if a.Manager != b.Manager {
		return a.Manager < b.Manager
	}
	return a.ID < b.ID
}

func dependencySortRank(node *Node) int {
	if isReplacementDependency(node) {
		return 0
	}
	if node != nil && node.Direct {
		return 1
	}
	return 2
}

func isReplacementDependency(node *Node) bool {
	if node == nil {
		return false
	}
	if node.Local {
		return true
	}
	return node.Manager == ManagerGo && node.Source != "" && node.Source != "go.mod" && node.Source != "go mod graph"
}

func sortStrings(values []string) {
	sort.Strings(values)
}

func isLocalRef(ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return false
	}
	if strings.HasPrefix(ref, "file:") || strings.HasPrefix(ref, "link:") || strings.HasPrefix(ref, "portal:") {
		return true
	}
	return strings.HasPrefix(ref, ".") || strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, ".."+string(filepath.Separator)) || strings.HasPrefix(ref, "../")
}
