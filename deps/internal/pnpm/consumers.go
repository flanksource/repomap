package pnpm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

func (r *Reference) Consumers() ([]string, error) {
	paths := map[string]bool{r.Workspace.Root: true}
	for _, pattern := range r.Workspace.Packages {
		if strings.HasPrefix(pattern, "!") {
			continue
		}
		matches, err := doublestar.FilepathGlob(filepath.Join(r.Workspace.Root, pattern))
		if err != nil {
			return nil, err
		}
		for _, path := range matches {
			if !r.Workspace.Contains(path) {
				continue
			}
			info, err := os.Stat(path)
			if err != nil {
				return nil, err
			}
			if info.IsDir() {
				paths[path] = true
			}
		}
	}
	var consumers []string
	for path := range paths {
		file := filepath.Join(path, "package.json")
		data, err := os.ReadFile(file)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var pkg map[string]json.RawMessage
		if err := json.Unmarshal(data, &pkg); err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		for _, scope := range []string{"dependencies", "devDependencies", "peerDependencies", "optionalDependencies"} {
			if pkg[scope] == nil {
				continue
			}
			var deps map[string]string
			if err := json.Unmarshal(pkg[scope], &deps); err != nil {
				return nil, fmt.Errorf("%s: %s: %w", file, scope, err)
			}
			spec := deps[r.Name]
			if !strings.HasPrefix(spec, "catalog:") {
				continue
			}
			catalog := strings.TrimSpace(strings.TrimPrefix(spec, "catalog:"))
			if catalog == "default" {
				catalog = ""
			}
			if catalog == r.Catalog {
				consumers = append(consumers, file)
				break
			}
		}
	}
	slices.Sort(consumers)
	return consumers, nil
}
