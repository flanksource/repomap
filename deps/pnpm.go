package deps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
)

type pnpmNativeNode struct {
	Name         string         `json:"name"`
	From         string         `json:"from"`
	Version      string         `json:"version"`
	Path         string         `json:"path"`
	Private      bool           `json:"private"`
	Dependencies pnpmNativeDeps `json:"dependencies"`
}

type pnpmNativeDeps []pnpmNativeNode

func (d *pnpmNativeDeps) UnmarshalJSON(data []byte) error {
	if strings.TrimSpace(string(data)) == "null" {
		*d = nil
		return nil
	}
	var list []pnpmNativeNode
	if err := json.Unmarshal(data, &list); err == nil {
		*d = list
		return nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]pnpmNativeNode, 0, len(keys))
	for _, key := range keys {
		var node pnpmNativeNode
		if err := json.Unmarshal(object[key], &node); err != nil {
			var version string
			if err2 := json.Unmarshal(object[key], &version); err2 != nil {
				return fmt.Errorf("dependency %s: %w", key, err)
			}
			node.Version = version
		}
		node.Name = firstNonEmpty(node.Name, node.From, key)
		out = append(out, node)
	}
	*d = out
	return nil
}

func resolvePNPMNative(ctx context.Context, project Project, opts Options) (*Node, []Warning, error) {
	args := []string{"list", "--json", "--depth", "Infinity", "--lockfile-only"}
	result, err := opts.Runner.Run(ctx, Command{Dir: project.Dir, Name: "pnpm", Args: args})
	if err != nil || strings.TrimSpace(result.Stdout) == "" {
		args = []string{"list", "--json", "--depth", "Infinity"}
		result, err = opts.Runner.Run(ctx, Command{Dir: project.Dir, Name: "pnpm", Args: args})
	}
	if err != nil && strings.TrimSpace(result.Stdout) == "" {
		return nil, nil, err
	}
	root, parseErr := parsePNPMNative([]byte(result.Stdout), project)
	if parseErr != nil {
		if err != nil {
			return nil, nil, fmt.Errorf("%v; also failed to parse stdout: %w", err, parseErr)
		}
		return nil, nil, parseErr
	}
	root.Path = project.File
	root.Source = "pnpm list"
	if err != nil {
		return root, []Warning{{Manager: ManagerPNPM, Project: project.Dir, Message: "pnpm list exited non-zero but produced parseable JSON: " + err.Error()}}, nil
	}
	return root, nil, nil
}

func resolvePNPMManifest(project Project) (*Node, []Warning, error) {
	root, err := parsePNPMLock(project.File)
	if err != nil {
		return nil, nil, err
	}
	return root, []Warning{{Manager: ManagerPNPM, Project: project.Dir, Message: "manifest fallback parsed pnpm-lock.yaml; peer and hoisting semantics may be approximate"}}, nil
}

func parsePNPMNative(data []byte, project Project) (*Node, error) {
	var roots []pnpmNativeNode
	if err := json.Unmarshal(data, &roots); err != nil {
		var root pnpmNativeNode
		if err2 := json.Unmarshal(data, &root); err2 != nil {
			return nil, err
		}
		roots = []pnpmNativeNode{root}
	}
	root := NewNode(ManagerPNPM, filepath.Base(project.Dir), "")
	root.Source = "pnpm list"
	for _, nativeRoot := range roots {
		child := convertPNPMNative(nativeRoot, 1)
		child.Direct = true
		root.Children = append(root.Children, child)
	}
	sortChildren(root)
	return root, nil
}

func convertPNPMNative(input pnpmNativeNode, depth int) *Node {
	name := input.Name
	if name == "" {
		name = input.Path
	}
	node := NewNode(ManagerPNPM, name, input.Version)
	node.Depth = depth
	node.Source = input.Path
	for _, dep := range input.Dependencies {
		node.Children = append(node.Children, convertPNPMNative(dep, depth+1))
	}
	sortChildren(node)
	return node
}

func parsePNPMLock(path string) (*Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	root := NewNode(ManagerPNPM, filepath.Base(filepath.Dir(path)), "")
	root.Path = path
	root.Source = filepath.Base(path)
	packages := pnpmPackageIndex(raw)
	importers := asMap(raw["importers"])
	if len(importers) == 0 {
		importers = map[string]any{".": raw}
	}
	importerKeys := make([]string, 0, len(importers))
	for key := range importers {
		importerKeys = append(importerKeys, key)
	}
	sort.Strings(importerKeys)
	for _, importerKey := range importerKeys {
		importerNode := NewNode(ManagerPNPM, importerKey, "")
		importerNode.Depth = 1
		importerNode.Direct = true
		importerNode.Source = "importer"
		importerData := asMap(importers[importerKey])
		addPNPMImporterDeps(importerNode, importerData, packages)
		sortChildren(importerNode)
		root.Children = append(root.Children, importerNode)
	}
	sortChildren(root)
	return root, nil
}

type pnpmPackage struct {
	Name     string
	Version  string
	Key      string
	Data     map[string]any
	Children map[string]string
}

func pnpmPackageIndex(raw map[string]any) map[string]pnpmPackage {
	out := map[string]pnpmPackage{}
	for _, section := range []string{"packages", "snapshots"} {
		for key, value := range asMap(raw[section]) {
			name, version := splitPNPMPackageKey(key)
			if name == "" {
				continue
			}
			data := asMap(value)
			pkg := pnpmPackage{Name: name, Version: version, Key: key, Data: data, Children: pnpmDeps(data)}
			out[pnpmPackageID(name, version)] = pkg
			if _, ok := out[name]; !ok {
				out[name] = pkg
			}
		}
	}
	return out
}

func addPNPMImporterDeps(parent *Node, importer map[string]any, packages map[string]pnpmPackage) {
	sections := []struct {
		name     string
		dev      bool
		optional bool
	}{
		{"dependencies", false, false},
		{"devDependencies", true, false},
		{"optionalDependencies", false, true},
	}
	for _, section := range sections {
		deps := asMap(importer[section.name])
		keys := make([]string, 0, len(deps))
		for key := range deps {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, depName := range keys {
			version := pnpmDependencyVersion(deps[depName])
			child := buildPNPMNode(depName, version, section.name, 2, section.dev, section.optional, packages, map[string]bool{})
			child.Direct = true
			parent.Children = append(parent.Children, child)
		}
	}
}

func buildPNPMNode(name, version, scope string, depth int, dev, optional bool, packages map[string]pnpmPackage, seen map[string]bool) *Node {
	version = stripPNPMPeerSuffix(version)
	node := NewNode(ManagerPNPM, name, version)
	node.Depth = depth
	node.Scope = scope
	node.Dev = dev
	node.Optional = optional
	node.Local = isLocalRef(version)
	pkg := packages[pnpmPackageID(name, version)]
	if pkg.Name == "" {
		pkg = packages[name]
	}
	if pkg.Key != "" {
		node.Source = pkg.Key
	}
	seenKey := pnpmPackageID(name, version)
	if seen[seenKey] {
		node.Circular = true
		return node
	}
	seen[seenKey] = true
	keys := make([]string, 0, len(pkg.Children))
	for key := range pkg.Children {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, childName := range keys {
		childVersion := pkg.Children[childName]
		node.Children = append(node.Children, buildPNPMNode(childName, childVersion, "dependencies", depth+1, dev, optional, packages, cloneBoolMap(seen)))
	}
	sortChildren(node)
	return node
}

func pnpmDeps(data map[string]any) map[string]string {
	out := map[string]string{}
	for _, section := range []string{"dependencies", "optionalDependencies", "peerDependencies"} {
		for key, value := range asMap(data[section]) {
			out[key] = pnpmDependencyVersion(value)
		}
	}
	return out
}

func pnpmDependencyVersion(value any) string {
	switch v := value.(type) {
	case string:
		return stripPNPMPeerSuffix(v)
	case map[string]any:
		if version := stringValue(v["version"]); version != "" {
			return stripPNPMPeerSuffix(version)
		}
		if specifier := stringValue(v["specifier"]); specifier != "" {
			return specifier
		}
	case map[any]any:
		if version := stringValue(v["version"]); version != "" {
			return stripPNPMPeerSuffix(version)
		}
	}
	return ""
}

func splitPNPMPackageKey(key string) (string, string) {
	key = strings.TrimPrefix(key, "/")
	key = stripPNPMPeerSuffix(key)
	if idx := strings.Index(key, "("); idx >= 0 {
		key = key[:idx]
	}
	if strings.HasPrefix(key, "@") {
		idx := strings.LastIndex(key, "@")
		if idx > 0 {
			return key[:idx], key[idx+1:]
		}
		return key, ""
	}
	name, version, ok := strings.Cut(key, "@")
	if !ok {
		return key, ""
	}
	return name, version
}

func stripPNPMPeerSuffix(value string) string {
	if idx := strings.Index(value, "("); idx >= 0 {
		return value[:idx]
	}
	return value
}

func pnpmPackageID(name, version string) string {
	if version == "" {
		return name
	}
	return name + "@" + stripPNPMPeerSuffix(version)
}

func asMap(value any) map[string]any {
	out := map[string]any{}
	switch v := value.(type) {
	case map[string]any:
		return v
	case map[any]any:
		for key, item := range v {
			out[fmt.Sprint(key)] = item
		}
	}
	return out
}

func stringValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}
