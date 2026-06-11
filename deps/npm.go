package deps

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type packageJSON struct {
	Name                 string            `json:"name"`
	Version              string            `json:"version"`
	Dependencies         map[string]string `json:"dependencies"`
	DevDependencies      map[string]string `json:"devDependencies"`
	OptionalDependencies map[string]string `json:"optionalDependencies"`
	PeerDependencies     map[string]string `json:"peerDependencies"`
}

type packageLock struct {
	Name         string                     `json:"name"`
	Version      string                     `json:"version"`
	Lockfile     int                        `json:"lockfileVersion"`
	Packages     map[string]packageLockItem `json:"packages"`
	Dependencies map[string]json.RawMessage `json:"dependencies"`
}

type packageLockItem struct {
	Name                 string            `json:"name"`
	Version              string            `json:"version"`
	Resolved             string            `json:"resolved"`
	Link                 bool              `json:"link"`
	Dev                  bool              `json:"dev"`
	Optional             bool              `json:"optional"`
	Dependencies         map[string]string `json:"dependencies"`
	DevDependencies      map[string]string `json:"devDependencies"`
	OptionalDependencies map[string]string `json:"optionalDependencies"`
	PeerDependencies     map[string]string `json:"peerDependencies"`
}

type packageLockV1Item struct {
	Version      string                       `json:"version"`
	Resolved     string                       `json:"resolved"`
	Dev          bool                         `json:"dev"`
	Optional     bool                         `json:"optional"`
	Dependencies map[string]packageLockV1Item `json:"dependencies"`
}

func resolveNPMManifest(project Project) (*Node, []Warning, error) {
	if filepath.Base(project.File) == "package-lock.json" || filepath.Base(project.File) == "npm-shrinkwrap.json" {
		root, err := parsePackageLock(project.File)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse %s: %w", project.File, err)
		}
		return root, nil, nil
	}
	root, err := parsePackageJSON(filepath.Join(project.Dir, "package.json"))
	if err != nil {
		return nil, nil, err
	}
	return root, []Warning{{Manager: ManagerNPM, Project: project.Dir, Message: "offline package.json parsing includes direct dependencies only; install tree may be unavailable"}}, nil
}

func parsePackageJSON(path string) (*Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, err
	}
	if pkg.Name == "" {
		pkg.Name = filepath.Base(filepath.Dir(path))
	}
	root := NewNode(ManagerNPM, pkg.Name, pkg.Version)
	root.Path = path
	root.Source = "package.json"
	addPackageJSONDeps(root, pkg.Dependencies, "dependencies", false, false)
	addPackageJSONDeps(root, pkg.DevDependencies, "devDependencies", true, false)
	addPackageJSONDeps(root, pkg.OptionalDependencies, "optionalDependencies", false, true)
	addPackageJSONDeps(root, pkg.PeerDependencies, "peerDependencies", false, false)
	sortChildren(root)
	return root, nil
}

func addPackageJSONDeps(root *Node, deps map[string]string, scope string, dev, optional bool) {
	keys := make([]string, 0, len(deps))
	for key := range deps {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, name := range keys {
		child := NewNode(ManagerNPM, name, deps[name])
		child.Depth = 1
		child.Direct = true
		child.Scope = scope
		child.Dev = dev
		child.Optional = optional
		child.Local = isLocalRef(deps[name])
		root.Children = append(root.Children, child)
	}
}

func parsePackageLock(path string) (*Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lock packageLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, err
	}
	if len(lock.Packages) > 0 {
		return packageLockV2Tree(path, lock), nil
	}
	return packageLockV1Tree(path, lock), nil
}

func packageLockV2Tree(path string, lock packageLock) *Node {
	rootPkg := lock.Packages[""]
	name := firstNonEmpty(lock.Name, rootPkg.Name, filepath.Base(filepath.Dir(path)))
	version := firstNonEmpty(lock.Version, rootPkg.Version)
	root := NewNode(ManagerNPM, name, version)
	root.Path = path
	root.Source = filepath.Base(path)
	root.Children = packageLockChildren("", rootPkg, lock.Packages, 1, map[string]bool{})
	sortChildren(root)
	return root
}

func packageLockChildren(parentPath string, item packageLockItem, packages map[string]packageLockItem, depth int, seen map[string]bool) []*Node {
	depScopes := []struct {
		scope    string
		deps     map[string]string
		dev      bool
		optional bool
	}{
		{"dependencies", item.Dependencies, item.Dev, item.Optional},
		{"devDependencies", item.DevDependencies, true, item.Optional},
		{"optionalDependencies", item.OptionalDependencies, item.Dev, true},
		{"peerDependencies", item.PeerDependencies, item.Dev, item.Optional},
	}
	var children []*Node
	for _, depScope := range depScopes {
		keys := make([]string, 0, len(depScope.deps))
		for key := range depScope.deps {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, depName := range keys {
			childPath, childItem, ok := findPackageLockChild(parentPath, depName, packages)
			version := depScope.deps[depName]
			if ok && childItem.Version != "" {
				version = childItem.Version
			}
			child := NewNode(ManagerNPM, depName, version)
			child.Depth = depth
			child.Direct = depth == 1
			child.Scope = depScope.scope
			child.Dev = depScope.dev || childItem.Dev
			child.Optional = depScope.optional || childItem.Optional
			child.Local = childItem.Link || isLocalRef(childItem.Resolved)
			child.Source = childItem.Resolved
			if ok && !seen[childPath] {
				nextSeen := cloneBoolMap(seen)
				nextSeen[childPath] = true
				child.Children = packageLockChildren(childPath, childItem, packages, depth+1, nextSeen)
			} else if ok {
				child.Circular = true
			}
			children = append(children, child)
		}
	}
	return children
}

func findPackageLockChild(parentPath, name string, packages map[string]packageLockItem) (string, packageLockItem, bool) {
	candidates := []string{}
	if parentPath != "" {
		candidates = append(candidates, parentPath+"/node_modules/"+name)
	}
	candidates = append(candidates, "node_modules/"+name)
	for _, candidate := range candidates {
		if item, ok := packages[candidate]; ok {
			return candidate, item, true
		}
	}
	return "", packageLockItem{}, false
}

func packageLockV1Tree(path string, lock packageLock) *Node {
	name := firstNonEmpty(lock.Name, filepath.Base(filepath.Dir(path)))
	root := NewNode(ManagerNPM, name, lock.Version)
	root.Path = path
	root.Source = filepath.Base(path)
	keys := make([]string, 0, len(lock.Dependencies))
	for key := range lock.Dependencies {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		var item packageLockV1Item
		if err := json.Unmarshal(lock.Dependencies[key], &item); err == nil {
			root.Children = append(root.Children, convertPackageLockV1(key, item, 1))
		}
	}
	return root
}

func convertPackageLockV1(name string, item packageLockV1Item, depth int) *Node {
	node := NewNode(ManagerNPM, name, item.Version)
	node.Depth = depth
	node.Direct = depth == 1
	node.Dev = item.Dev
	node.Optional = item.Optional
	node.Source = item.Resolved
	keys := make([]string, 0, len(item.Dependencies))
	for key := range item.Dependencies {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		node.Children = append(node.Children, convertPackageLockV1(key, item.Dependencies[key], depth+1))
	}
	return node
}
