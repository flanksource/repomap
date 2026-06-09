package deps

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/flanksource/clicky/task"
	flanksourceContext "github.com/flanksource/commons/context"
)

func Scan(ctx context.Context, path string, opts Options) (*Export, error) {
	if opts.Mode == "" {
		opts.Mode = ModeAuto
	}
	if opts.Runner == nil {
		opts.Runner = ExecRunner{}
	}
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	projects, warnings, err := Discover(absPath, opts.Managers)
	if err != nil {
		return nil, err
	}

	roots, projectWarnings, err := resolveProjectsWithTasks(ctx, projects, opts)
	warnings = append(warnings, projectWarnings...)
	if err != nil {
		return nil, err
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("no dependency graphs resolved")
	}

	filteredRoots := make([]*Node, 0, len(roots))
	for _, root := range roots {
		if filtered := filterAndPrune(root, opts.Filters, opts.MaxDepth); filtered != nil {
			filteredRoots = append(filteredRoots, filtered)
		}
	}
	dups := analyzeDuplicates(filteredRoots)
	applyDuplicateRefs(filteredRoots, dups)
	nodes, edges, stats := flatten(filteredRoots, dups)
	stats.Projects = len(projects)

	sortWarnings(warnings)
	return &Export{
		Metadata: Metadata{
			ExportedAt:      now(),
			Version:         "1.0",
			Path:            absPath,
			Managers:        opts.Managers,
			Mode:            opts.Mode,
			Filter:          opts.Filters,
			MaxDepth:        opts.MaxDepth,
			Configurations:  opts.Configurations,
			ProjectsScanned: len(projects),
		},
		Roots:      filteredRoots,
		Nodes:      nodes,
		Edges:      edges,
		Statistics: stats,
		Duplicates: duplicatesList(dups),
		Warnings:   warnings,
	}, nil
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
			taskOpts := opts
			taskOpts.Runner = taskCommandRunner{base: opts.Runner, task: tk}
			root, warnings, err := resolveProject(ctx, project, taskOpts)
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
				if opts.Strict || opts.Mode == ModeNative {
					tk.FailedWithError(err)
					return result, err
				}
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
			if opts.Strict || opts.Mode == ModeNative {
				return nil, warnings, result.Err
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

type taskCommandRunner struct {
	base CommandRunner
	task *task.Task
}

func (r taskCommandRunner) Run(ctx context.Context, cmd Command) (CommandResult, error) {
	if r.task != nil {
		r.task.Infof("running %s %s", cmd.Name, strings.Join(cmd.Args, " "))
	}
	result, err := r.base.Run(ctx, cmd)
	if err != nil && r.task != nil {
		r.task.Warnf("%s failed: %v", cmd.Name, err)
	}
	return result, err
}

func resolveProject(ctx context.Context, project Project, opts Options) (*Node, []Warning, error) {
	var root *Node
	var warnings []Warning
	var err error
	if opts.Mode != ModeManifest {
		root, warnings, err = resolveNative(ctx, project, opts)
		if err == nil && root != nil {
			return root, warnings, nil
		}
		if opts.Mode == ModeNative {
			return nil, warnings, fmt.Errorf("%s native resolver failed for %s: %w", project.Manager, project.Dir, err)
		}
		warnings = append(warnings, Warning{
			Manager: project.Manager,
			Project: project.Dir,
			Message: fmt.Sprintf("native resolver failed; using manifest fallback: %v", err),
		})
	}
	root, fallbackWarnings, err := resolveManifest(project, opts)
	warnings = append(warnings, fallbackWarnings...)
	if err != nil {
		return nil, warnings, fmt.Errorf("%s manifest resolver failed for %s: %w", project.Manager, project.Dir, err)
	}
	return root, warnings, nil
}

func resolveNative(ctx context.Context, project Project, opts Options) (*Node, []Warning, error) {
	switch project.Manager {
	case ManagerGo:
		return resolveGoNative(ctx, project, opts)
	case ManagerMaven:
		return resolveMavenNative(ctx, project, opts)
	case ManagerGradle:
		return resolveGradleNative(ctx, project, opts)
	case ManagerNPM:
		return resolveNPMNative(ctx, project, opts)
	case ManagerPNPM:
		return resolvePNPMNative(ctx, project, opts)
	default:
		return nil, nil, fmt.Errorf("unsupported manager %q", project.Manager)
	}
}

func resolveManifest(project Project, opts Options) (*Node, []Warning, error) {
	switch project.Manager {
	case ManagerGo:
		return resolveGoManifest(project)
	case ManagerMaven:
		return resolveMavenManifest(project)
	case ManagerGradle:
		return resolveGradleManifest(project)
	case ManagerNPM:
		return resolveNPMManifest(project)
	case ManagerPNPM:
		return resolvePNPMManifest(project)
	default:
		return nil, nil, fmt.Errorf("unsupported manager %q", project.Manager)
	}
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
