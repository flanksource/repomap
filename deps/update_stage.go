package deps

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/flanksource/repomap/deps/internal/gitstage"
)

type updateStage struct {
	session *gitstage.Session
	indexes []int
}

func beginUpdateStages(ctx context.Context, requests []updateRequest, opts UpdateOptions) ([]updateStage, error) {
	if opts.DryRun {
		return nil, nil
	}
	groups := map[string][]int{}
	paths := map[string][]string{}
	for i, request := range requests {
		if selectedVersionIsCurrent(request.Candidate.Current, request.Version) {
			continue
		}
		result, err := (ExecRunner{}).Run(ctx, Command{Dir: request.Candidate.Dir, Name: "git", Args: []string{"rev-parse", "--show-toplevel"}})
		if err != nil {
			return nil, fmt.Errorf("snapshot %s: %w: %s", request.Candidate.File, err, result.Stderr)
		}
		root := strings.TrimSpace(result.Stdout)
		groups[root] = append(groups[root], i)
		paths[root] = append(paths[root], workspaceFiles(request.Candidate)...)
	}
	var stages []updateStage
	roots := make([]string, 0, len(groups))
	for root := range groups {
		roots = append(roots, root)
	}
	sort.Strings(roots)
	for _, root := range roots {
		session, err := gitstage.Begin(ctx, root, paths[root])
		if err != nil {
			for _, stage := range stages {
				err = errors.Join(err, stage.session.Close())
			}
			return nil, err
		}
		stages = append(stages, updateStage{session: session, indexes: groups[root]})
	}
	return stages, nil
}

func finishUpdateStages(ctx context.Context, plans []UpdatePlan, stages []updateStage) {
	for _, stage := range stages {
		changed, err := stage.session.Changed()
		failed := err != nil
		for _, i := range stage.indexes {
			if plans[i].Error != "" {
				failed = true
			}
		}
		var staged []string
		if !failed {
			staged, err = stage.session.Stage(ctx)
		}
		err = errors.Join(err, stage.session.Close())
		for _, i := range stage.indexes {
			for _, path := range changed {
				plans[i].ChangedFiles = append(plans[i].ChangedFiles, cwdRelativePath(filepath.Join(stage.session.Root, path)))
			}
			for _, path := range staged {
				plans[i].Staged = append(plans[i].Staged, cwdRelativePath(filepath.Join(stage.session.Root, path)))
			}
			if err != nil {
				plans[i].StageError = err.Error()
			}
		}
	}
}

// updatedRepoFiles lists the files a manager rewrites during an update, relative
// to candidate.Dir using forward slashes.
func updatedRepoFiles(candidate UpdateCandidate) []string {
	switch candidate.Manager {
	case ManagerGo:
		return []string{"go.mod", "go.sum"}
	case ManagerNPM:
		return []string{"package.json", "package-lock.json"}
	case ManagerPNPM:
		return []string{"package.json", "pnpm-lock.yaml"}
	case ManagerImage, ManagerHelm:
		if candidate.Target != nil && candidate.Target.File != "" {
			return []string{candidate.Target.File}
		}
		return nil
	default:
		return nil
	}
}
