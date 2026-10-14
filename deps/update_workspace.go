package deps

import (
	"fmt"
	"path/filepath"
	"slices"

	"github.com/flanksource/repomap/deps/internal/pnpm"
)

func discoverUpdateProjects(root string, managers []Manager) ([]Project, error) {
	selected := managerSet(managers)
	lookup := slices.Clone(managers)
	if selected[ManagerPNPM] && !selected[ManagerNPM] {
		lookup = append(lookup, ManagerNPM)
	}
	projects, _, err := Discover(root, lookup)
	if err != nil {
		return nil, err
	}
	var out []Project
	seen := map[string]bool{}
	for _, project := range projects {
		if project.Manager == ManagerNPM && selected[ManagerPNPM] {
			workspace, err := pnpm.Find(project.Dir)
			if err != nil {
				return nil, err
			}
			if workspace != nil {
				project.Manager = ManagerPNPM
			}
		}
		key := string(project.Manager) + "\x00" + project.Dir
		if selected[project.Manager] && !seen[key] {
			seen[key] = true
			out = append(out, project)
		}
	}
	return out, nil
}

func validateCatalogSelections(requests []updateRequest) error {
	versions := map[string]string{}
	for _, request := range requests {
		if request.Candidate.catalog == nil {
			continue
		}
		key := request.Candidate.catalog.Key()
		if version, ok := versions[key]; ok && version != request.Version {
			return fmt.Errorf("conflicting versions %s and %s selected for %s in %s", version, request.Version, request.Candidate.Name, request.Candidate.catalog.Workspace.File)
		}
		versions[key] = request.Version
	}
	return nil
}

func includeCatalogConsumers(candidates []UpdateCandidate, requests []updateRequest) []updateRequest {
	versions := map[string]string{}
	selected := map[string]bool{}
	for _, request := range requests {
		selected[request.Candidate.key()] = true
		if request.Candidate.catalog != nil {
			versions[request.Candidate.catalog.Key()] = request.Version
		}
	}
	for _, candidate := range candidates {
		if candidate.catalog == nil || selected[candidate.key()] {
			continue
		}
		if version, ok := versions[candidate.catalog.Key()]; ok {
			requests = append(requests, updateRequest{Candidate: candidate, Version: version})
		}
	}
	return requests
}

func workspaceFiles(c UpdateCandidate) []string {
	var files []string
	for _, file := range updatedRepoFiles(c) {
		files = append(files, filepath.Join(c.Dir, filepath.FromSlash(file)))
	}
	if c.workspace != nil {
		files = append(files, c.workspace.File, filepath.Join(c.workspace.Root, "pnpm-lock.yaml"))
	}
	return files
}
