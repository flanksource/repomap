package deps

import (
	"fmt"
	"sort"
	"strings"

	"github.com/flanksource/commons/collections"
)

func filterAndPrune(root *Node, filters []string, maxDepth int) *Node {
	return filterAndPruneAt(root, filters, maxDepth, nil)
}

func filterAndPruneAt(node *Node, filters []string, maxDepth int, path map[string]bool) *Node {
	if node == nil {
		return nil
	}
	if maxDepth > 0 && node.Depth > maxDepth {
		return nil
	}
	if path == nil {
		path = map[string]bool{}
	}
	cp := node.cloneShallow()
	key := node.ID
	if path[key] {
		cp.Circular = true
		return cp
	}
	path[key] = true
	for _, child := range node.Children {
		if filtered := filterAndPruneAt(child, filters, maxDepth, cloneBoolMap(path)); filtered != nil {
			cp.Children = append(cp.Children, filtered)
		}
	}
	if len(filters) == 0 || nodeMatches(cp, filters) || len(cp.Children) > 0 || cp.Depth == 0 {
		return cp
	}
	return nil
}

func nodeMatches(node *Node, filters []string) bool {
	if len(filters) == 0 {
		return true
	}
	candidates := []string{
		node.ID,
		node.Name,
		node.Version,
		string(node.Manager),
		node.Scope,
		node.Source,
		node.Path,
		fmt.Sprintf("%s:%s", node.Manager, node.Name),
		fmt.Sprintf("%s:%s@%s", node.Manager, node.Name, node.Version),
	}
	for _, candidate := range candidates {
		if candidate != "" && collections.MatchItems(candidate, filters...) {
			return true
		}
	}
	return false
}

func flatten(roots []*Node, duplicates map[string]*Duplicate) ([]FlatNode, []Edge, Statistics) {
	nodeMap := map[string]FlatNode{}
	edgeMap := map[string]Edge{}
	stats := Statistics{ByManager: map[Manager]int{}}
	var circular int
	var visit func(*Node)
	visit = func(node *Node) {
		if node == nil {
			return
		}
		if _, exists := nodeMap[node.ID]; !exists {
			nodeMap[node.ID] = FlatNode{
				ID:       node.ID,
				Name:     node.Name,
				Version:  node.Version,
				Manager:  node.Manager,
				Scope:    node.Scope,
				Source:   node.Source,
				Path:     node.Path,
				Direct:   node.Direct,
				Dev:      node.Dev,
				Optional: node.Optional,
				Local:    node.Local,
				Depth:    node.Depth,
			}
			stats.ByManager[node.Manager]++
			if node.Depth > stats.MaxDepth {
				stats.MaxDepth = node.Depth
			}
			if node.Circular {
				circular++
			}
		}
		for _, child := range node.Children {
			edgeKey := node.ID + ">" + child.ID + ">" + child.Scope
			edgeMap[edgeKey] = Edge{
				From:     node.ID,
				To:       child.ID,
				Manager:  child.Manager,
				Scope:    child.Scope,
				Dev:      child.Dev,
				Optional: child.Optional,
			}
			visit(child)
		}
	}
	for _, root := range roots {
		visit(root)
	}
	nodes := make([]FlatNode, 0, len(nodeMap))
	for _, node := range nodeMap {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Manager != nodes[j].Manager {
			return nodes[i].Manager < nodes[j].Manager
		}
		if nodes[i].Depth != nodes[j].Depth {
			return nodes[i].Depth < nodes[j].Depth
		}
		return nodes[i].ID < nodes[j].ID
	})
	edges := make([]Edge, 0, len(edgeMap))
	for _, edge := range edgeMap {
		edges = append(edges, edge)
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].To != edges[j].To {
			return edges[i].To < edges[j].To
		}
		return edges[i].Scope < edges[j].Scope
	})
	stats.Total = len(nodes)
	stats.Edges = len(edges)
	stats.Circular = circular
	for _, dup := range duplicates {
		if dup.Count > 1 {
			stats.Duplicates++
			if dup.Conflicts {
				stats.Conflicts++
			}
		}
	}
	return nodes, edges, stats
}

func analyzeDuplicates(roots []*Node) map[string]*Duplicate {
	out := map[string]*Duplicate{}
	var walk func(*Node, string)
	walk = func(node *Node, parentPath string) {
		if node == nil {
			return
		}
		currentPath := node.Name
		if parentPath != "" {
			currentPath = parentPath + " > " + node.Name
		}
		key := string(node.Manager) + ":" + node.Name
		dup := out[key]
		if dup == nil {
			dup = &Duplicate{
				Name:     node.Name,
				Manager:  node.Manager,
				Versions: map[string][]string{},
			}
			out[key] = dup
		}
		dup.Count++
		version := node.Version
		if version == "" {
			version = "(none)"
		}
		dup.Versions[version] = append(dup.Versions[version], currentPath)
		for _, child := range node.Children {
			walk(child, currentPath)
		}
	}
	for _, root := range roots {
		walk(root, "")
	}
	for _, dup := range out {
		if len(dup.Versions) > 1 {
			dup.Conflicts = true
		}
	}
	return out
}

func applyDuplicateRefs(roots []*Node, duplicates map[string]*Duplicate) {
	var walk func(*Node)
	walk = func(node *Node) {
		if node == nil {
			return
		}
		key := string(node.Manager) + ":" + node.Name
		if dup := duplicates[key]; dup != nil && dup.Count > 1 {
			node.Duplicate = &DupRef{Count: dup.Count, Conflicts: dup.Conflicts}
		}
		for _, child := range node.Children {
			walk(child)
		}
	}
	for _, root := range roots {
		walk(root)
	}
}

func duplicatesList(duplicates map[string]*Duplicate) []Duplicate {
	out := make([]Duplicate, 0)
	for _, dup := range duplicates {
		if dup.Count > 1 {
			out = append(out, *dup)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Manager != out[j].Manager {
			return out[i].Manager < out[j].Manager
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func cloneBoolMap(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func splitPatterns(values []string) []string {
	var out []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}
