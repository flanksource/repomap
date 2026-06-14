package deps

import (
	"fmt"
	"io/fs"
	"os"
	osexec "os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
)

// chartFile is the subset of Chart.yaml repomap reads: the chart's own identity
// and its declared subchart dependencies.
type chartFile struct {
	Name         string          `yaml:"name"`
	Version      string          `yaml:"version"`
	Dependencies []chartDepEntry `yaml:"dependencies"`
}

type chartDepEntry struct {
	Name       string `yaml:"name"`
	Version    string `yaml:"version"`
	Repository string `yaml:"repository"`
}

// discoverChartDependencyRoots walks for Helm Chart.yaml files and returns one
// dependency root per chart. Each root carries the subchart dependencies
// declared in Chart.yaml (helm) and the container images referenced in the
// chart's values.yaml and templates/ (image). Versions are recorded verbatim
// from the declarations; nothing is resolved over the network.
func discoverChartDependencyRoots(root string, managers []Manager) ([]*Node, []Warning, error) {
	selected := managerSet(managers)
	wantHelm := len(selected) == 0 || selected[ManagerHelm]
	wantImage := len(selected) == 0 || selected[ManagerImage]
	if !wantHelm && !wantImage {
		return nil, nil, nil
	}

	charts, err := discoverChartFiles(root)
	if err != nil {
		return nil, nil, err
	}

	var roots []*Node
	var warnings []Warning
	for _, chartPath := range charts {
		node, chartWarnings := chartDependencyRoot(root, chartPath, wantHelm, wantImage)
		warnings = append(warnings, chartWarnings...)
		if node != nil {
			roots = append(roots, node)
		}
	}
	sort.SliceStable(roots, func(i, j int) bool { return roots[i].Path < roots[j].Path })
	return roots, warnings, nil
}

func chartDependencyRoot(scanRoot, chartPath string, wantHelm, wantImage bool) (*Node, []Warning) {
	rel := chartRelPath(scanRoot, chartPath)
	data, err := os.ReadFile(chartPath)
	if err != nil {
		return nil, []Warning{{Manager: ManagerHelm, Project: rel, Message: err.Error()}}
	}
	var chart chartFile
	if err := yaml.Unmarshal(data, &chart); err != nil {
		return nil, []Warning{{Manager: ManagerHelm, Project: rel, Message: fmt.Sprintf("parse Chart.yaml: %s", err)}}
	}

	chartDir := filepath.Dir(chartPath)
	name := chart.Name
	if name == "" {
		name = filepath.Base(chartDir)
	}
	root := NewNode(ManagerHelm, name, chart.Version)
	root.Source = "Chart.yaml"
	root.Path = filepath.ToSlash(chartPath)

	var children []*Node
	var warnings []Warning
	if wantHelm {
		children = append(children, chartSubchartNodes(chart, rel)...)
	}
	if wantImage {
		imageNodes, imgWarnings := chartImageNodes(chartDir, scanRoot)
		children = append(children, imageNodes...)
		warnings = append(warnings, imgWarnings...)
	}
	if len(children) == 0 {
		return nil, warnings
	}
	root.Children = children
	return root, warnings
}

func chartSubchartNodes(chart chartFile, chartRel string) []*Node {
	var nodes []*Node
	for _, dep := range chart.Dependencies {
		if dep.Name == "" {
			continue
		}
		node := NewNode(ManagerHelm, dep.Name, dep.Version)
		node.Direct = true
		node.Depth = 1
		node.Scope = "dependencies"
		node.Source = dep.Repository
		node.Path = chartRel
		nodes = append(nodes, node)
	}
	return nodes
}

// chartImageNodes collects container images referenced in a chart's values.yaml
// and templates/, deduplicated by image ref.
func chartImageNodes(chartDir, scanRoot string) ([]*Node, []Warning) {
	seen := map[string]*Node{}
	var order []string
	add := func(ref, source string) {
		ref = strings.TrimSpace(strings.Trim(ref, `"'`))
		if !looksLikeImage(ref) {
			return
		}
		node := chartImageNode(ref, source)
		if _, ok := seen[node.ID]; ok {
			return
		}
		seen[node.ID] = node
		order = append(order, node.ID)
	}

	var warnings []Warning
	valuesPath := filepath.Join(chartDir, "values.yaml")
	if data, err := os.ReadFile(valuesPath); err == nil {
		var values map[string]interface{}
		if err := yaml.Unmarshal(data, &values); err != nil {
			warnings = append(warnings, Warning{Manager: ManagerImage, Project: chartRelPath(scanRoot, valuesPath), Message: fmt.Sprintf("parse values.yaml: %s", err)})
		} else {
			source := chartRelPath(scanRoot, valuesPath)
			collectValuesImages(values, func(ref string) { add(ref, source) })
		}
	}
	collectTemplateImages(chartDir, scanRoot, add)

	nodes := make([]*Node, 0, len(order))
	for _, id := range order {
		nodes = append(nodes, seen[id])
	}
	return nodes, warnings
}

func chartImageNode(ref, source string) *Node {
	name, version := splitImageRef(ref)
	node := NewNode(ManagerImage, name, version)
	node.Direct = true
	node.Depth = 1
	node.Source = ref
	node.Path = source
	return node
}

// collectValuesImages walks a parsed values.yaml tree and reports every image
// reference it finds, covering both the `image: "repo:tag"` string form and the
// structured `image: {registry, repository, tag}` map form at any nesting depth.
func collectValuesImages(node interface{}, report func(string)) {
	switch v := node.(type) {
	case map[string]interface{}:
		if s, ok := v["image"].(string); ok {
			report(s)
		}
		if img := imageFromMap(v); img != "" {
			report(img)
		}
		for _, val := range v {
			collectValuesImages(val, report)
		}
	case []interface{}:
		for _, item := range v {
			collectValuesImages(item, report)
		}
	}
}

func imageFromMap(m map[string]interface{}) string {
	if spec, ok := m["image"].(map[string]interface{}); ok {
		if img := repoTagImage(spec); img != "" {
			return img
		}
	}
	return repoTagImage(m)
}

func repoTagImage(m map[string]interface{}) string {
	repo := scalarString(m["repository"])
	if repo == "" {
		// `name` is a less common alias for `repository`; only trust it when a tag
		// sits alongside, so non-image maps that merely have a name are ignored.
		if _, hasTag := m["tag"]; hasTag {
			repo = scalarString(m["name"])
		}
	}
	if repo == "" {
		return ""
	}
	if registry := scalarString(m["registry"]); registry != "" {
		repo = registry + "/" + repo
	}
	if tag := scalarString(m["tag"]); tag != "" {
		return repo + ":" + tag
	}
	return repo
}

func scalarString(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		return fmt.Sprintf("%v", t)
	}
}

// collectTemplateImages scans a chart's templates/ for literal `image:` values,
// skipping Go-templated values that cannot be resolved offline.
func collectTemplateImages(chartDir, scanRoot string, add func(ref, source string)) {
	templatesDir := filepath.Join(chartDir, "templates")
	_ = filepath.WalkDir(templatesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !isTemplateFile(d.Name()) {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		source := chartRelPath(scanRoot, path)
		for _, ref := range imageValuesInTemplate(string(data)) {
			add(ref, source)
		}
		return nil
	})
}

func imageValuesInTemplate(content string) []string {
	var refs []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		// Accept both `image:` and the list-item form `- image:`.
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
		const key = "image:"
		if !strings.HasPrefix(trimmed, key) {
			continue
		}
		value := strings.TrimSpace(trimmed[len(key):])
		if value == "" || strings.Contains(value, "{{") {
			continue
		}
		refs = append(refs, value)
	}
	return refs
}

func isTemplateFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".yaml", ".yml", ".tpl":
		return true
	}
	return false
}

// looksLikeImage rejects values that are clearly not container image references:
// empty, whitespace, Go-templated, or not starting with an image-ref character.
// Bare official images (e.g. busybox) are accepted since the value came from an
// explicit image field.
func looksLikeImage(ref string) bool {
	if ref == "" || strings.ContainsAny(ref, " \t{}|<>\"'`") || strings.Contains(ref, "{{") {
		return false
	}
	c := ref[0]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func splitImageRef(ref string) (name, version string) {
	name = stripImageVersion(ref)
	if i := imageTagSeparator(ref); i >= 0 {
		version = ref[i+1:]
		if at := strings.Index(version, "@"); at >= 0 {
			version = version[:at]
		}
	}
	return name, version
}

func discoverChartFiles(root string) ([]string, error) {
	files, ok := gitChartFiles(root)
	if !ok {
		var err error
		files, err = walkChartFiles(root)
		if err != nil {
			return nil, err
		}
	}
	return filterVendoredCharts(files), nil
}

func gitChartFiles(root string) ([]string, bool) {
	cmd := osexec.Command("git", "-C", root, "ls-files", "--cached", "--others", "--exclude-standard", "-z", "--", ".")
	out, err := cmd.Output()
	if err != nil {
		return nil, false
	}
	var files []string
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel == "" || !strings.EqualFold(filepath.Base(filepath.FromSlash(rel)), "chart.yaml") {
			continue
		}
		files = append(files, filepath.Join(root, filepath.FromSlash(rel)))
	}
	sort.Strings(files)
	return files, true
}

func walkChartFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && ignoredDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(d.Name(), "Chart.yaml") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// filterVendoredCharts drops Chart.yaml files that live in a parent chart's
// charts/ directory (downloaded dependencies), keeping standalone charts and
// monorepo charts/<name> layouts.
func filterVendoredCharts(files []string) []string {
	present := make(map[string]bool, len(files))
	for _, f := range files {
		present[filepath.Dir(f)] = true
	}
	out := files[:0]
	for _, f := range files {
		dir := filepath.Dir(f)
		parent := filepath.Dir(dir)
		if filepath.Base(parent) == "charts" && present[filepath.Dir(parent)] {
			continue
		}
		out = append(out, f)
	}
	return out
}

func chartRelPath(scanRoot, path string) string {
	if rel, err := filepath.Rel(scanRoot, path); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(path)
}

func chartRootCount(roots []*Node) int {
	count := 0
	for _, root := range roots {
		if root != nil && root.Manager == ManagerHelm && root.Source == "Chart.yaml" {
			count++
		}
	}
	return count
}
