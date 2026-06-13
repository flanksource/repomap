package deps

// collapseDuplicates rewrites package-manager roots in place so each dependency
// renders once at its resolved (shallowest) location instead of repeating under
// every parent. Image/Helm roots are left untouched: repeated images are real
// distinct deployments and the kubernetes display drops the root that would
// carry a collapse marker.
func collapseDuplicates(roots []*Node) {
	for _, root := range roots {
		if root == nil || !isPackageManager(root.Manager) {
			continue
		}
		collapseRoot(root)
	}
}

func isPackageManager(manager Manager) bool {
	switch manager {
	case ManagerGo, ManagerMaven, ManagerGradle, ManagerNPM, ManagerPNPM:
		return true
	}
	return false
}

// collapseRoot keeps the first BFS sighting (shallowest, then sorted) of each
// node ID within a single root, drops later sightings, and records the counts:
// the resolved node's OtherParents and each parent's HiddenDuplicates.
func collapseRoot(root *Node) {
	refs := map[string]int{}
	countRefs(root, refs)
	sortTree(root)

	seen := map[string]bool{}
	queue := []*Node{root}
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		kept := parent.Children[:0]
		hidden := 0
		for _, child := range parent.Children {
			if seen[child.ID] {
				hidden++
				continue
			}
			seen[child.ID] = true
			if refs[child.ID] > 1 {
				child.OtherParents = refs[child.ID] - 1
			}
			kept = append(kept, child)
			queue = append(queue, child)
		}
		parent.Children = kept
		parent.HiddenDuplicates = hidden
	}
}

func countRefs(node *Node, refs map[string]int) {
	if node == nil {
		return
	}
	for _, child := range node.Children {
		refs[child.ID]++
		countRefs(child, refs)
	}
}

func sortTree(node *Node) {
	if node == nil {
		return
	}
	sortChildren(node)
	for _, child := range node.Children {
		sortTree(child)
	}
}
