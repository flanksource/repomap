package deps

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/flanksource/clicky/task"
	flanksourceContext "github.com/flanksource/commons/context"
	"github.com/flanksource/repomap/deps/internal/pnpm"
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
	Manager   Manager
	Dir       string
	File      string
	Scope     string
	Indexes   []int
	workspace *pnpm.Workspace
}

// applyUpdateRequests applies every request and returns the resulting plans
// keyed by UpdateCandidate.key. Repository snapshots isolate updater changes
// from existing staged and unstaged work.
func applyUpdateRequests(ctx context.Context, requests []updateRequest, opts UpdateOptions) map[string]UpdatePlan {
	plans := make([]UpdatePlan, len(requests))
	for i, request := range requests {
		plans[i] = planFromCandidate(request.Candidate)
		plans[i].NewVersion = request.Version
	}
	if err := validateCatalogSelections(requests); err != nil {
		for i := range plans {
			plans[i].Error = err.Error()
		}
	} else {
		stages, err := beginUpdateStages(ctx, requests, opts)
		if err != nil {
			for i := range plans {
				plans[i].Error = err.Error()
			}
		} else {
			runUpdateLanes(ctx, groupUpdateBatchesIntoLanes(groupUpdateRequestsIntoBatches(requests)), requests, plans, opts)
			refreshUpdateWorkspaces(ctx, requests, plans, opts)
			verifyGitPins(ctx, requests, plans, opts)
			finishUpdateStages(ctx, plans, stages)
		}
	}

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
			Manager:   candidate.Manager,
			Dir:       candidate.Dir,
			File:      candidate.File,
			Scope:     candidate.Scope,
			Indexes:   []int{i},
			workspace: candidate.workspace,
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
	parents := make([]int, len(batches))
	for i := range parents {
		parents[i] = i
	}
	root := func(i int) int {
		for parents[i] != i {
			i = parents[i]
		}
		return i
	}
	for i, batch := range batches {
		for j := 0; j < i; j++ {
			if batch.overlaps(batches[j]) {
				parents[root(i)] = root(j)
			}
		}
	}
	indexByRoot := map[int]int{}
	var lanes []updateLane
	for i, batch := range batches {
		key := root(i)
		if j, ok := indexByRoot[key]; ok {
			lanes[j].Batches = append(lanes[j].Batches, batch)
		} else {
			indexByRoot[key] = len(lanes)
			lanes = append(lanes, updateLane{Name: batch.laneKey(), Batches: []updateBatch{batch}})
		}
	}
	return lanes
}

func (b updateBatch) overlaps(other updateBatch) bool {
	return b.laneKey() == other.laneKey() || b.workspace != nil && b.workspace.Contains(other.Dir) || other.workspace != nil && other.workspace.Contains(b.Dir)
}

func (b updateBatch) laneKey() string {
	if b.Manager == ManagerActions {
		return "file:" + filepath.Join(b.Dir, filepath.Base(b.File))
	}
	if b.Manager == ManagerImage || b.Manager == ManagerHelm {
		return "file:" + filepath.Join(b.Dir, b.File)
	}
	if b.workspace != nil {
		return "pnpm:" + b.workspace.Root
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
			if updateLaneFailed(lanes[0].Batches, plans) {
				markBlockedBatch(batch, plans)
				continue
			}
			applyUpdateBatch(ctx, batch, requests, plans, opts)
		}
		return
	}
	group := task.StartGroup[int]("Applying dependency updates", task.WithConcurrency(updateApplyConcurrency))
	for i, lane := range lanes {
		idx, batches := i, lane.Batches
		group.Add(updateLaneTaskName(batches), func(_ flanksourceContext.Context, tk *task.Task) (int, error) {
			for _, batch := range batches {
				if updateLaneFailed(batches, plans) {
					markBlockedBatch(batch, plans)
					continue
				}
				tk.Infof("updating %d %s %s", len(batch.Indexes), batch.Manager, pluralizeDependency(len(batch.Indexes)))
				applyUpdateBatch(ctx, batch, requests, plans, opts)
			}
			if updateLaneFailed(batches, plans) {
				tk.Failed()
				return idx, fmt.Errorf("updates failed in %s", updateLaneTaskName(batches))
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
	if candidate.Manager == ManagerActions {
		return "actions:" + filepath.Join(candidate.Dir, filepath.Base(candidate.File))
	}
	if candidate.catalog != nil {
		return "catalog:" + candidate.catalog.Key()
	}
	if candidate.Manager == ManagerImage || candidate.Manager == ManagerHelm {
		return "single\x00" + candidate.key()
	}
	return strings.Join([]string{string(candidate.Manager), candidate.Dir, candidate.Scope}, "\x00")
}

// applyUpdateBatch records a failed mutating command without retrying it.
func applyUpdateBatch(ctx context.Context, batch updateBatch, requests []updateRequest, plans []UpdatePlan, opts UpdateOptions) {
	if batch.Manager == ManagerActions {
		applyActionBatch(ctx, batch, requests, plans, opts)
		return
	}
	if batch.Manager == ManagerImage || batch.Manager == ManagerHelm {
		for _, i := range batch.Indexes {
			plans[i] = applyImageTargetUpdate(ctx, requests[i].Candidate, requests[i].Version, opts)
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
	if ref := requests[runnable[0]].Candidate.catalog; ref != nil {
		for _, i := range runnable {
			plans[i].Command = []string{"pnpm", "install", "--lockfile-only", "--ignore-scripts", "--no-frozen-lockfile"}
		}
		err := ref.Apply(requests[runnable[0]].Version, opts.DryRun)
		for _, i := range runnable {
			if err != nil {
				plans[i].Error = err.Error()
			} else if opts.DryRun {
				plans[i].DryRun = true
			} else {
				plans[i].Written = true
			}
		}
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
			plans[i].Error = err.Error()
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
	result, err := runner.Run(ctx, cmd)
	if err == nil {
		for _, i := range runnable {
			plans[i].Written = true
		}
		return
	}
	for _, i := range runnable {
		plans[i].Error = commandFailure(cmd, result, err)
	}
}
