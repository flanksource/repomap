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
	type result struct {
		versions         []string
		latestStable     string
		latestPrerelease string
		err              error
	}
	results := make([]result, len(candidates))
	group := task.StartGroup[int]("Resolving dependency versions", task.WithConcurrency(updateResolveConcurrency))
	for i, candidate := range candidates {
		idx, c := i, candidate
		group.Add(updateTaskName(c), func(_ flanksourceContext.Context, tk *task.Task) (int, error) {
			tk.Infof("looking up published versions")
			versions, latestStable, latestPrerelease, err := resolveCandidateVersions(ctx, opts, c)
			results[idx] = result{versions: versions, latestStable: latestStable, latestPrerelease: latestPrerelease, err: err}
			if err != nil {
				tk.Warnf("%s", err.Error())
				tk.Warning()
			} else if len(versions) == 0 {
				tk.Infof("already up to date")
				tk.Success()
			} else {
				tk.Success()
			}
			return idx, nil
		})
	}
	_, _ = group.GetResults()

	plansByKey := map[string]UpdatePlan{}
	choices := make([]UpdateChoice, 0, len(candidates))
	for i, candidate := range candidates {
		result := results[i]
		if result.err != nil {
			plansByKey[candidate.key()] = skippedUpdatePlan(candidate, result.err.Error())
			continue
		}
		if len(result.versions) == 0 {
			continue
		}
		choices = append(choices, UpdateChoice{
			Candidate:        candidate,
			Versions:         result.versions,
			LatestStable:     result.latestStable,
			LatestPrerelease: result.latestPrerelease,
		})
	}
	return choices, plansByKey
}

func resolveCandidateVersions(ctx context.Context, opts UpdateOptions, candidate UpdateCandidate) ([]string, string, string, error) {
	var (
		versions []string
		err      error
	)
	switch candidate.Manager {
	case ManagerImage, ManagerHelm:
		versions, _, _, err = availableImageTargetVersions(ctx, opts.ImageResolver, candidate)
	default:
		versions, err = AvailableDependencyVersions(ctx, opts.Runner, candidate)
	}
	if err != nil {
		return nil, "", "", err
	}
	versions = updateableVersions(candidate.Current, versions)
	return versions, latestStableVersion(versions), latestPrereleaseVersion(versions), nil
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
