package deps

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"github.com/flanksource/clicky/task"
	flanksourceContext "github.com/flanksource/commons/context"
)

// updateRequest pairs a candidate with the version the caller settled on, so
// every mode — the interactive tree, --latest, and --version — hands the apply
// phase the same shape.
type updateRequest struct {
	Candidate UpdateCandidate
	Version   string
}

// updateBatch is a set of requests one command can satisfy: package-manager
// updates written to the same manifest under the same dependency section.
// Image and Helm requests are never batched — each rewrites its own manifest.
type updateBatch struct {
	Manager Manager
	Dir     string
	File    string
	Scope   string
	Indexes []int
}

// applyUpdateRequests applies every request and returns the resulting plans
// keyed by UpdateCandidate.key. Applying and staging are separate phases:
// `git add` takes .git/index.lock exclusively, so staging runs once per
// distinct file set after every update has been written.
func applyUpdateRequests(ctx context.Context, requests []updateRequest, opts UpdateOptions) map[string]UpdatePlan {
	plans := make([]UpdatePlan, len(requests))
	runUpdateLanes(ctx, groupUpdateBatchesIntoLanes(groupUpdateRequestsIntoBatches(requests)), requests, plans, opts)
	stageUpdatePlans(ctx, requests, plans, opts.Runner)

	plansByKey := make(map[string]UpdatePlan, len(requests))
	for i, request := range requests {
		plansByKey[request.Candidate.key()] = plans[i]
	}
	return plansByKey
}

// groupUpdateRequestsIntoBatches collapses package-manager requests that share a
// manifest and dependency section into one command. Scope is part of the key
// because the --save-* flag applies to the whole invocation and would otherwise
// move packages between sections.
func groupUpdateRequestsIntoBatches(requests []updateRequest) []updateBatch {
	indexByKey := map[string]int{}
	var batches []updateBatch
	for i, request := range requests {
		candidate := request.Candidate
		key := updateBatchKey(request)
		if j, ok := indexByKey[key]; ok {
			batches[j].Indexes = append(batches[j].Indexes, i)
			continue
		}
		indexByKey[key] = len(batches)
		batches = append(batches, updateBatch{
			Manager: candidate.Manager,
			Dir:     candidate.Dir,
			File:    candidate.File,
			Scope:   candidate.Scope,
			Indexes: []int{i},
		})
	}
	return batches
}

// updateLane is a sequence of batches that must not run concurrently with each
// other. Lanes run in parallel; the batches inside one lane run in order.
type updateLane struct {
	Name    string
	Batches []updateBatch
}

// groupUpdateBatchesIntoLanes serializes batches that write the same files.
// Package managers rewrite their manifest and lockfile, so a directory is one
// lane regardless of manager — npm and pnpm share package.json. Image and Helm
// edits are whole-file rewrites, so each manifest is its own lane, which still
// lets separate manifests resolve digests in parallel.
func groupUpdateBatchesIntoLanes(batches []updateBatch) []updateLane {
	indexByKey := map[string]int{}
	var lanes []updateLane
	for _, batch := range batches {
		key := batch.laneKey()
		if i, ok := indexByKey[key]; ok {
			lanes[i].Batches = append(lanes[i].Batches, batch)
			continue
		}
		indexByKey[key] = len(lanes)
		lanes = append(lanes, updateLane{Name: key, Batches: []updateBatch{batch}})
	}
	return lanes
}

func (b updateBatch) laneKey() string {
	if b.Manager == ManagerImage || b.Manager == ManagerHelm {
		return "file:" + filepath.Join(b.Dir, b.File)
	}
	return "pkg:" + b.Dir
}

// runUpdateLanes applies every lane, rendering progress so a slow package
// manager reports what it is doing instead of freezing the terminal.
func runUpdateLanes(ctx context.Context, lanes []updateLane, requests []updateRequest, plans []UpdatePlan, opts UpdateOptions) {
	if len(lanes) == 0 {
		return
	}
	if len(lanes) == 1 {
		for _, batch := range lanes[0].Batches {
			applyUpdateBatch(ctx, batch, requests, plans, opts)
		}
		return
	}
	group := task.StartGroup[int]("Applying dependency updates", task.WithConcurrency(updateApplyConcurrency))
	for i, lane := range lanes {
		idx, batches := i, lane.Batches
		group.Add(updateLaneTaskName(batches), func(_ flanksourceContext.Context, tk *task.Task) (int, error) {
			for _, batch := range batches {
				tk.Infof("updating %d %s %s", len(batch.Indexes), batch.Manager, pluralizeDependency(len(batch.Indexes)))
				applyUpdateBatch(ctx, batch, requests, plans, opts)
			}
			tk.Success()
			return idx, nil
		})
	}
	_, _ = group.GetResults()
}

func updateLaneTaskName(batches []updateBatch) string {
	if len(batches) == 0 {
		return "update"
	}
	if file := batches[0].File; file != "" {
		return file
	}
	if dir := batches[0].Dir; dir != "" {
		return dir
	}
	return string(batches[0].Manager)
}

func pluralizeDependency(n int) string {
	if n == 1 {
		return "dependency"
	}
	return "dependencies"
}

func updateBatchKey(request updateRequest) string {
	candidate := request.Candidate
	if candidate.Manager == ManagerImage || candidate.Manager == ManagerHelm {
		return "single\x00" + candidate.key()
	}
	return strings.Join([]string{string(candidate.Manager), candidate.Dir, candidate.Scope}, "\x00")
}

// applyUpdateBatch writes every plan the batch covers. A batch that fails as a
// whole is retried one dependency at a time so a single bad version reports its
// own error instead of failing its manifest siblings.
func applyUpdateBatch(ctx context.Context, batch updateBatch, requests []updateRequest, plans []UpdatePlan, opts UpdateOptions) {
	if batch.Manager == ManagerImage || batch.Manager == ManagerHelm {
		for _, i := range batch.Indexes {
			plans[i] = applyDependencyUpdate(ctx, requests[i].Candidate, requests[i].Version, opts)
		}
		return
	}

	// Requests that need no command, or whose manager rejects them, never enter
	// the batch.
	var runnable []int
	for _, i := range batch.Indexes {
		plans[i] = planFromCandidate(requests[i].Candidate)
		plans[i].NewVersion = requests[i].Version
		if selectedVersionIsCurrent(requests[i].Candidate.Current, requests[i].Version) {
			plans[i].Skipped = "already at selected version"
			continue
		}
		runnable = append(runnable, i)
	}
	if len(runnable) == 0 {
		return
	}

	sort.SliceStable(runnable, func(a, b int) bool {
		return requests[runnable[a]].Candidate.less(requests[runnable[b]].Candidate)
	})
	targets := make([]string, 0, len(runnable))
	for _, i := range runnable {
		targets = append(targets, requests[i].Candidate.Name+"@"+requests[i].Version)
	}

	cmd, err := updateBatchCommand(batch.Manager, batch.Dir, batch.Scope, targets)
	if err != nil {
		for _, i := range runnable {
			plans[i].Skipped = err.Error()
		}
		return
	}
	for _, i := range runnable {
		plans[i].Command = append([]string{cmd.Name}, cmd.Args...)
	}
	if opts.DryRun {
		for _, i := range runnable {
			plans[i].DryRun = true
		}
		return
	}

	runner := opts.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	_, err = runner.Run(ctx, cmd)
	if err == nil {
		for _, i := range runnable {
			plans[i].Written = true
		}
		return
	}
	if len(runnable) == 1 {
		// A one-target batch is already the per-dependency command, so its error
		// needs no attribution.
		plans[runnable[0]].Skipped = err.Error()
		return
	}
	// One shared error cannot say which dependency failed, so retry them
	// individually to keep the per-dependency reporting the serial path had.
	for _, i := range runnable {
		plans[i] = applyDependencyUpdate(ctx, requests[i].Candidate, requests[i].Version, opts)
	}
}

// stageUpdatePlans runs `git add` once per distinct set of written files and
// copies the outcome onto every plan that shares it. Candidates in one manifest
// rewrite the same files, so staging them individually would run identical
// commands and contend on the git index lock.
func stageUpdatePlans(ctx context.Context, requests []updateRequest, plans []UpdatePlan, runner CommandRunner) {
	type stageResult struct {
		staged []string
		err    string
	}
	results := map[string]stageResult{}
	for i, request := range requests {
		if !plans[i].Written {
			continue
		}
		key := stageGroupKey(request.Candidate)
		if _, done := results[key]; !done {
			staged, err := stageUpdatedFiles(ctx, runner, request.Candidate)
			result := stageResult{staged: staged}
			if err != nil {
				result.err = err.Error()
			}
			results[key] = result
		}
		plans[i].Staged = results[key].staged
		plans[i].StageError = results[key].err
	}
}

// stageGroupKey identifies candidates whose successful update stages exactly
// the same paths.
func stageGroupKey(candidate UpdateCandidate) string {
	return candidate.Dir + "\x00" + strings.Join(updatedRepoFiles(candidate), "\x00")
}
