package deps

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/flanksource/repomap/deps/internal/actions"
)

func resolveActionsManifest(project Project) (*Node, []Warning, error) {
	content, err := os.ReadFile(project.File)
	if err != nil {
		return nil, nil, toolError{fmt.Errorf("read %s: %w", project.File, err)}
	}
	result, err := actions.Parse(project.File, content)
	if err != nil {
		return nil, nil, toolError{err}
	}
	root := NewNode(ManagerActions, project.Name, "")
	root.ID = "actions:file:" + filepath.ToSlash(project.File)
	root.Path = filepath.ToSlash(project.File)
	root.Source = "GitHub Actions manifest"
	for _, ref := range result.References {
		node := NewNode(ManagerActions, ref.Name, ref.Ref)
		node.ID = fmt.Sprintf("actions:%s:%d", filepath.ToSlash(project.File), ref.Line)
		node.Scope = ref.Scope
		node.Source = ref.Name + "@" + ref.Ref
		node.Path = filepath.ToSlash(project.File)
		node.Direct = true
		node.Depth = 1
		root.Children = append(root.Children, node)
	}
	var warnings []Warning
	for _, skipped := range result.Skipped {
		warnings = append(warnings, Warning{Manager: ManagerActions, Project: project.File, Message: fmt.Sprintf("%s:%d: %s: %s", project.File, skipped.Line, skipped.Value, skipped.Reason)})
	}
	return root, warnings, nil
}

func discoverActionsUpdateCandidates(project Project) ([]UpdateCandidate, error) {
	content, err := os.ReadFile(project.File)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", project.File, err)
	}
	result, err := actions.Parse(project.File, content)
	if err != nil {
		return nil, err
	}
	var candidates []UpdateCandidate
	for _, ref := range result.References {
		ref := ref
		candidates = append(candidates, UpdateCandidate{
			Manager: ManagerActions,
			Name:    ref.Name,
			Current: ref.Ref,
			Scope:   ref.Scope,
			File:    project.File,
			Dir:     project.Dir,
			action:  &ref,
		})
	}
	for _, skipped := range result.Skipped {
		candidates = append(candidates, UpdateCandidate{
			Manager:    ManagerActions,
			Name:       skipped.Value,
			Scope:      skipped.Scope,
			File:       project.File,
			Dir:        project.Dir,
			actionSkip: skipped.Reason,
		})
	}
	return candidates, nil
}
