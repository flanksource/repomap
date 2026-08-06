package deps

// collapseDuplicates rewrites package-manager roots in place so each dependency
// renders once at its resolved (shallowest) location instead of repeating under
// every parent. Later occurrences are dropped silently with no marker. Image/Helm
// roots are left untouched: repeated images are real distinct deployments.
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
// node ID within a single root and silently drops later sightings.
func collapseRoot(root *Node) {
	sortTree(root)

	seen := map[string]bool{}
	queue := []*Node{root}
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		kept := parent.Children[:0]
		for _, child := range parent.Children {
			if seen[child.ID] {
				continue
			}
			seen[child.ID] = true
			kept = append(kept, child)
			queue = append(queue, child)
		}
		parent.Children = kept
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
