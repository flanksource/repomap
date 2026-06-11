package deps

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/flanksource/clicky/task"
	flanksourceContext "github.com/flanksource/commons/context"
)

func Scan(ctx context.Context, path string, opts Options) (*Export, error) {
	if opts.Mode == "" {
		opts.Mode = ModeManifest
	}
	if opts.Mode != ModeManifest {
		return nil, fmt.Errorf("unsupported dependency resolution mode %q", opts.Mode)
	}
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	packageManagers := packageScanManagers(opts.Managers)
	scanPackages := len(opts.Managers) == 0 || len(packageManagers) > 0
	scanImages := includesImageScanManagers(opts.Managers)

	var projects []Project
	var warnings []Warning
	var packageErr error
	if scanPackages {
		projects, warnings, packageErr = discoverOffline(absPath, packageManagers)
		if packageErr != nil && !scanImages {
			return nil, packageErr
		}
	}

	if opts.Runner == nil {
		opts.Runner = ExecRunner{}
	}

	var roots []*Node
	if len(projects) > 0 {
		projectRoots, projectWarnings, err := resolveProjectsWithTasks(ctx, projects, opts)
		warnings = append(warnings, projectWarnings...)
		if err != nil {
			return nil, err
		}
		roots = append(roots, projectRoots...)
	}

	if scanImages {
		imageRoots, imageWarnings, err := discoverImageDependencyRoots(absPath, imageScanManagers(opts.Managers))
		warnings = append(warnings, imageWarnings...)
		if err != nil {
			if len(roots) == 0 && packageErr != nil {
				return nil, packageErr
			}
			if len(roots) == 0 {
				return nil, err
			}
			warnings = append(warnings, Warning{Message: err.Error()})
		}
		roots = append(roots, imageRoots...)
	}

	if packageErr != nil && len(roots) == 0 {
		return nil, packageErr
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("no dependency graphs resolved")
	}
	projectsScanned := len(projects) + imageRootCount(roots)

	filteredRoots := make([]*Node, 0, len(roots))
	for _, root := range roots {
		if filtered := filterAndPrune(root, opts.Filters, opts.MaxDepth); filtered != nil {
			filteredRoots = append(filteredRoots, filtered)
		}
	}
	dups := analyzeDuplicates(filteredRoots)
	applyDuplicateRefs(filteredRoots, dups)
	nodes, edges, stats := flatten(filteredRoots, dups)
	stats.Projects = projectsScanned

	sortWarnings(warnings)
	export := &Export{
		Metadata: Metadata{
			ExportedAt:      now(),
			Version:         "1.0",
			Path:            absPath,
			Managers:        opts.Managers,
			Mode:            opts.Mode,
			Filter:          opts.Filters,
			MaxDepth:        opts.MaxDepth,
			Flat:            opts.Flat,
			ProjectsScanned: projectsScanned,
		},
		Statistics: stats,
		Duplicates: duplicatesList(dups),
		Warnings:   warnings,
	}
	if opts.Flat {
		export.Nodes = nodes
		export.Edges = edges
	} else {
		export.Roots = filteredRoots
	}
	return export, nil
}

type projectResolution struct {
	Index    int
	Project  Project
	Root     *Node
	Warnings []Warning
	Err      error
}

func resolveProjectsWithTasks(ctx context.Context, projects []Project, opts Options) ([]*Node, []Warning, error) {
	group := task.StartGroup[projectResolution](
		"Resolving dependency graphs",
		task.WithKind("repomap-deps"),
		task.WithLabels(map[string]string{
			"mode":     string(opts.Mode),
			"projects": fmt.Sprintf("%d", len(projects)),
		}),
	)
	for i, project := range projects {
		index := i
		project := project
		group.Add(projectTaskName(project), func(_ flanksourceContext.Context, tk *task.Task) (projectResolution, error) {
			tk.SetProgress(0, 1)
			tk.Infof("resolving %s dependencies in %s", project.Manager, project.Dir)
			root, warnings, err := resolveProject(ctx, project, opts)
			result := projectResolution{
				Index:    index,
				Project:  project,
				Root:     root,
				Warnings: warnings,
				Err:      err,
			}
			for _, warning := range warnings {
				tk.Warnf("%s", warning.Message)
			}
			if err != nil {
				tk.Warnf("%s", err.Error())
				tk.Warning()
				return result, nil
			}
			tk.SetProgress(1, 1)
			if len(warnings) > 0 {
				tk.Warning()
			} else {
				tk.Success()
			}
			return result, nil
		})
	}

	results, err := group.GetResults()
	if err != nil {
		return nil, nil, err
	}
	ordered := make([]projectResolution, 0, len(results))
	for _, result := range results {
		ordered = append(ordered, result)
	}
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].Index < ordered[j].Index
	})
	roots := make([]*Node, 0, len(ordered))
	var warnings []Warning
	for _, result := range ordered {
		warnings = append(warnings, result.Warnings...)
		if result.Err != nil {
			var te toolError
			if errors.As(result.Err, &te) {
				return nil, nil, result.Err
			}
			warnings = append(warnings, Warning{Manager: result.Project.Manager, Project: result.Project.Dir, Message: result.Err.Error()})
			continue
		}
		if result.Root != nil {
			roots = append(roots, result.Root)
		}
	}
	return roots, warnings, nil
}

func projectTaskName(project Project) string {
	return fmt.Sprintf("%s %s", project.Manager, project.Dir)
}

func resolveProject(ctx context.Context, project Project, opts Options) (*Node, []Warning, error) {
	root, warnings, err := resolveManifest(ctx, project, opts)
	if err != nil {
		var te toolError
		if errors.As(err, &te) {
			return nil, warnings, err
		}
		return nil, warnings, fmt.Errorf("%s manifest resolver failed for %s: %w", project.Manager, project.Dir, err)
	}
	return root, warnings, nil
}

func resolveManifest(ctx context.Context, project Project, opts Options) (*Node, []Warning, error) {
	transitive := opts.MaxDepth != 1
	switch project.Manager {
	case ManagerGo:
		if transitive {
			return resolveGoGraph(ctx, project, opts)
		}
		return resolveGoManifest(project, opts)
	case ManagerMaven:
		if transitive {
			return resolveMavenTree(ctx, project, opts)
		}
		return resolveMavenManifest(project)
	case ManagerGradle:
		if transitive {
			return resolveGradleTree(ctx, project, opts)
		}
		return resolveGradleManifest(project)
	case ManagerNPM:
		return resolveNPMManifest(project)
	case ManagerPNPM:
		return resolvePNPMManifest(project)
	default:
		return nil, nil, fmt.Errorf("unsupported manager %q", project.Manager)
	}
}

// toolError marks a failure that must abort the whole scan rather than degrade
// to a per-project warning. It wraps errors from package-manager shell-outs
// (go mod graph, mvn dependency:tree, gradle dependencies) so a missing or
// failing tool surfaces loudly with a suggestion to rerun with --depth 1.
type toolError struct {
	err error
}

func (e toolError) Error() string { return e.err.Error() }

func (e toolError) Unwrap() error { return e.err }

func packageScanManagers(managers []Manager) []Manager {
	if len(managers) == 0 {
		return nil
	}
	out := make([]Manager, 0, len(managers))
	for _, manager := range managers {
		switch manager {
		case ManagerGo, ManagerMaven, ManagerGradle, ManagerNPM, ManagerPNPM:
			out = append(out, manager)
		}
	}
	return out
}

func imageScanManagers(managers []Manager) []Manager {
	if len(managers) == 0 {
		return nil
	}
	out := make([]Manager, 0, len(managers))
	for _, manager := range managers {
		switch manager {
		case ManagerImage, ManagerHelm:
			out = append(out, manager)
		}
	}
	return out
}

func includesImageScanManagers(managers []Manager) bool {
	if len(managers) == 0 {
		return true
	}
	return len(imageScanManagers(managers)) > 0
}

func imageRootCount(roots []*Node) int {
	count := 0
	for _, root := range roots {
		if root != nil && (root.Manager == ManagerImage || root.Manager == ManagerHelm) && root.Source == "kubernetes manifests" {
			count++
		}
	}
	return count
}

func sortWarnings(warnings []Warning) {
	sort.Slice(warnings, func(i, j int) bool {
		if warnings[i].Manager != warnings[j].Manager {
			return warnings[i].Manager < warnings[j].Manager
		}
		if warnings[i].Project != warnings[j].Project {
			return warnings[i].Project < warnings[j].Project
		}
		return warnings[i].Message < warnings[j].Message
	})
}
