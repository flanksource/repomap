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
	for _, lookup := range groupVersionLookups(keys, rep) {
		indexes, candidates := lookup.indexes, lookup.candidates
		group.Add(lookup.name, func(_ flanksourceContext.Context, tk *task.Task) (int, error) {
			tk.Infof("looking up published versions")
			var failed bool
			for i, candidate := range candidates {
				versions, err := lookup.resolve(ctx, opts, candidate)
				results[indexes[i]] = rawVersionResult{versions: versions, err: err}
				if err != nil {
					tk.Warnf("%s: %s", candidate.Name, err.Error())
					failed = true
				}
			}
			if failed {
				tk.Warning()
			} else {
				tk.Success()
			}
			return indexes[0], nil
		})
	}
	_, _ = group.GetResults()

	byKey := make(map[string]rawVersionResult, len(keys))
	for i, key := range keys {
		byKey[key] = results[i]
	}
	return byKey
}

// versionLookup is one unit of version resolution: usually a single dependency,
// but Go modules sharing a directory resolve together in one `go list` run.
type versionLookup struct {
	name       string
	indexes    []int
	candidates []UpdateCandidate
	resolve    func(context.Context, UpdateOptions, UpdateCandidate) ([]string, error)
}

// groupVersionLookups collapses the Go modules of one directory into a single
// lookup. `go list -m -versions` accepts several modules per invocation, and one
// toolchain start-up beats one per dependency.
func groupVersionLookups(keys []string, rep map[string]UpdateCandidate) []versionLookup {
	var lookups []versionLookup
	goLookupByDir := map[string]int{}
	for i, key := range keys {
		candidate := rep[key]
		if candidate.Manager != ManagerGo {
			lookups = append(lookups, versionLookup{
				name:       updateTaskName(candidate),
				indexes:    []int{i},
				candidates: []UpdateCandidate{candidate},
				resolve:    resolveCandidateRawVersions,
			})
			continue
		}
		if j, ok := goLookupByDir[candidate.Dir]; ok {
			lookups[j].indexes = append(lookups[j].indexes, i)
			lookups[j].candidates = append(lookups[j].candidates, candidate)
			continue
		}
		goLookupByDir[candidate.Dir] = len(lookups)
		lookups = append(lookups, versionLookup{
			name:       updateTaskName(candidate),
			indexes:    []int{i},
			candidates: []UpdateCandidate{candidate},
		})
	}
	for i := range lookups {
		if lookups[i].resolve != nil {
			continue
		}
		lookups[i].resolve = newGoModuleVersionLookup(lookups[i].candidates)
		if len(lookups[i].candidates) > 1 {
			lookups[i].name = fmt.Sprintf("go %d modules", len(lookups[i].candidates))
		}
	}
	return lookups
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
