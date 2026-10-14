package pnpm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/bmatcuk/doublestar/v4"
	"github.com/flanksource/repomap/imageupdate"
	"gopkg.in/yaml.v3"
)

type Workspace struct {
	Root     string
	File     string
	Packages []string
	Catalog  map[string]string
	Catalogs map[string]map[string]string
}

type Reference struct {
	Workspace *Workspace
	Catalog   string
	Name      string
	Current   string
	Line      int
}

func Find(dir string) (*Workspace, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	for root := dir; ; root = filepath.Dir(root) {
		file := filepath.Join(root, "pnpm-workspace.yaml")
		data, err := os.ReadFile(file)
		if err == nil {
			w := &Workspace{Root: root, File: file}
			if err := yaml.Unmarshal(data, w); err != nil {
				return nil, fmt.Errorf("%s: %w", file, err)
			}
			w.Root, w.File = root, file
			for _, pattern := range w.Packages {
				if !doublestar.ValidatePattern(strings.TrimPrefix(pattern, "!")) {
					return nil, fmt.Errorf("%s: invalid package pattern %q", file, pattern)
				}
			}
			if !w.Contains(dir) {
				return nil, fmt.Errorf("%s is excluded from pnpm workspace %s", dir, file)
			}
			return w, nil
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
		if filepath.Dir(root) == root {
			return nil, nil
		}
	}
}

func (w *Workspace) Contains(dir string) bool {
	rel, err := filepath.Rel(w.Root, dir)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return true
	}
	for _, part := range strings.Split(rel, "/") {
		if part == "node_modules" || part == ".git" {
			return false
		}
	}
	included := false
	for _, pattern := range w.Packages {
		match, err := doublestar.Match(filepath.ToSlash(filepath.Clean(strings.TrimPrefix(pattern, "!"))), rel)
		if err != nil || !match {
			continue
		}
		if strings.HasPrefix(pattern, "!") {
			return false
		}
		included = true
	}
	return included
}

func (w *Workspace) Resolve(name, spec string) (*Reference, error) {
	if !strings.HasPrefix(spec, "catalog:") {
		return nil, nil
	}
	catalog := strings.TrimSpace(strings.TrimPrefix(spec, "catalog:"))
	if catalog == "default" {
		catalog = ""
	}
	entries := w.Catalog
	keys := []string{"catalog", name}
	if catalog == "" && entries == nil {
		entries = w.Catalogs["default"]
		keys = []string{"catalogs", "default", name}
	}
	if catalog != "" {
		entries = w.Catalogs[catalog]
		keys = []string{"catalogs", catalog, name}
	}
	current, ok := entries[name]
	if !ok {
		return nil, fmt.Errorf("%s: missing %s entry for %s", w.File, spec, name)
	}
	if _, err := semver.NewConstraint(current); err != nil {
		return nil, fmt.Errorf("%s: invalid %s version for %s: %w", w.File, spec, name, err)
	}
	data, err := os.ReadFile(w.File)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	node := doc.Content[0]
	for _, key := range keys {
		var found *yaml.Node
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == key {
				found = node.Content[i+1]
				break
			}
		}
		if found == nil {
			return nil, fmt.Errorf("%s: cannot locate catalog entry %s", w.File, name)
		}
		node = found
	}
	if node.Kind != yaml.ScalarNode {
		return nil, fmt.Errorf("%s: catalog entry %s must be a scalar", w.File, name)
	}
	return &Reference{Workspace: w, Catalog: catalog, Name: name, Current: current, Line: node.Line}, nil
}

func (r *Reference) Key() string { return r.Workspace.File + "\x00" + r.Catalog + "\x00" + r.Name }

func (r *Reference) Apply(version string, dryRun bool) error {
	if _, err := semver.NewVersion(version); err != nil {
		return err
	}
	current := r.Current
	prefix := ""
	if strings.HasPrefix(current, "^") || strings.HasPrefix(current, "~") {
		prefix, current = current[:1], current[1:]
	}
	if _, err := semver.NewVersion(current); err != nil {
		return fmt.Errorf("%s: cannot preserve complex catalog range %q", r.Workspace.File, r.Current)
	}
	_, err := imageupdate.ApplyEdit(r.Workspace.File, imageupdate.UpdateTarget{File: r.Workspace.File, FieldLine: r.Line}, prefix+version, dryRun)
	return err
}
