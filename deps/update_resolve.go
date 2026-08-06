package deps

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/flanksource/clicky/task"
	flanksourceContext "github.com/flanksource/commons/context"
)

func resolveUpdateChoices(ctx context.Context, candidates []UpdateCandidate, opts UpdateOptions) ([]UpdateChoice, map[string]UpdatePlan) {
	rawByKey := resolveRawVersionsByKey(ctx, candidates, opts)

	plansByKey := map[string]UpdatePlan{}
	choices := make([]UpdateChoice, 0, len(candidates))
	for _, candidate := range candidates {
		raw := rawByKey[candidate.resolutionKey()]
		if raw.err != nil {
			plansByKey[candidate.key()] = skippedUpdatePlan(candidate, raw.err.Error())
			continue
		}
		// The "newer than current" filtering is per occurrence: duplicates of the
		// same dependency may sit at different current versions even though they
		// share one published-version list.
		versions := updateableVersions(candidate.Current, raw.versions)
		if len(versions) == 0 {
			continue
		}
		choices = append(choices, UpdateChoice{
			Candidate:        candidate,
			Versions:         versions,
			LatestStable:     latestStableVersion(versions),
			LatestPrerelease: latestPrereleaseVersion(versions),
		})
	}
	return choices, plansByKey
}

type rawVersionResult struct {
	versions []string
	err      error
}

// resolveRawVersionsByKey looks up the published versions for every distinct
// dependency source among candidates, running one lookup per source and sharing
// it across duplicate occurrences. The same image repository, chart+repo, or
// package resolves identically wherever it is referenced, so collapsing them
// avoids redundant registry/proxy round-trips. The result is keyed by
// UpdateCandidate.resolutionKey.
func resolveRawVersionsByKey(ctx context.Context, candidates []UpdateCandidate, opts UpdateOptions) map[string]rawVersionResult {
	keys := make([]string, 0, len(candidates))
	rep := map[string]UpdateCandidate{}
	for _, candidate := range candidates {
		key := candidate.resolutionKey()
		if _, ok := rep[key]; !ok {
			rep[key] = candidate
			keys = append(keys, key)
		}
	}

	results := make([]rawVersionResult, len(keys))
	group := task.StartGroup[int]("Resolving dependency versions", task.WithConcurrency(updateResolveConcurrency))
	for i, key := range keys {
		idx, candidate := i, rep[key]
		group.Add(updateTaskName(candidate), func(_ flanksourceContext.Context, tk *task.Task) (int, error) {
			tk.Infof("looking up published versions")
			versions, err := resolveCandidateRawVersions(ctx, opts, candidate)
			results[idx] = rawVersionResult{versions: versions, err: err}
			if err != nil {
				tk.Warnf("%s", err.Error())
				tk.Warning()
			} else {
				tk.Success()
			}
			return idx, nil
		})
	}
	_, _ = group.GetResults()

	byKey := make(map[string]rawVersionResult, len(keys))
	for i, key := range keys {
		byKey[key] = results[i]
	}
	return byKey
}

// resolveCandidateRawVersions returns a candidate source's published versions
// without the per-occurrence "newer than current" filtering, so the result can
// be cached and reused across duplicate occurrences.
func resolveCandidateRawVersions(ctx context.Context, opts UpdateOptions, candidate UpdateCandidate) ([]string, error) {
	switch candidate.Manager {
	case ManagerImage, ManagerHelm:
		versions, _, _, err := availableImageTargetVersions(ctx, opts.ImageResolver, candidate)
		return versions, err
	default:
		return AvailableDependencyVersions(ctx, opts.Runner, candidate)
	}
}

func AvailableDependencyVersions(ctx context.Context, runner CommandRunner, candidate UpdateCandidate) ([]string, error) {
	if runner == nil {
		runner = ExecRunner{}
	}
	var (
		result CommandResult
		err    error
	)
	switch candidate.Manager {
	case ManagerGo:
		result, err = runner.Run(ctx, Command{
			Dir:  candidate.Dir,
			Name: "go",
			Args: []string{"list", "-m", "-versions", "-json", candidate.Name},
			Env:  []string{"GOFLAGS=-mod=readonly"},
		})
	case ManagerNPM:
		result, err = runner.Run(ctx, Command{
			Dir:  candidate.Dir,
			Name: "npm",
			Args: []string{"view", candidate.Name, "versions", "--json"},
		})
	case ManagerPNPM:
		result, err = runner.Run(ctx, Command{
			Dir:  candidate.Dir,
			Name: "pnpm",
			Args: []string{"view", candidate.Name, "versions", "--json"},
		})
	default:
		return nil, fmt.Errorf("dependency version lookup does not support manager %q", candidate.Manager)
	}
	if err != nil {
		if result.Stderr != "" {
			return nil, fmt.Errorf("%s: %w", strings.TrimSpace(result.Stderr), err)
		}
		return nil, err
	}
	versions, err := parseAvailableVersions(candidate.Manager, result.Stdout)
	if err != nil {
		return nil, err
	}
	return sortDependencyVersions(versions), nil
}

func parseAvailableVersions(manager Manager, stdout string) ([]string, error) {
	stdout = strings.TrimSpace(stdout)
	if stdout == "" {
		return nil, nil
	}
	if manager == ManagerGo {
		var payload struct {
			Versions []string `json:"Versions"`
		}
		if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
			return nil, err
		}
		return payload.Versions, nil
	}
	var list []string
	if err := json.Unmarshal([]byte(stdout), &list); err == nil {
		return list, nil
	}
	var single string
	if err := json.Unmarshal([]byte(stdout), &single); err == nil && single != "" {
		return []string{single}, nil
	}
	return nil, fmt.Errorf("expected JSON version array")
}
