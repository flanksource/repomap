package deps

import (
	"context"
	"sort"
)

func updateLaneFailed(batches []updateBatch, plans []UpdatePlan) bool {
	for _, batch := range batches {
		for _, i := range batch.Indexes {
			if plans[i].Error != "" {
				return true
			}
		}
	}
	return false
}

func markBlockedBatch(batch updateBatch, plans []UpdatePlan) {
	for _, i := range batch.Indexes {
		plans[i].Skipped = "not executed after lane failure"
	}
}

func refreshUpdateWorkspaces(ctx context.Context, requests []updateRequest, plans []UpdatePlan, opts UpdateOptions) {
	if opts.DryRun {
		return
	}
	roots := map[string][]int{}
	failedDirs := map[string]bool{}
	for i, request := range requests {
		if request.Candidate.workspace == nil {
			continue
		}
		root := request.Candidate.workspace.Root
		if plans[i].Error != "" {
			failedDirs[root] = true
		}
		if plans[i].Written {
			roots[root] = append(roots[root], i)
		}
	}
	ordered := make([]string, 0, len(roots))
	for root := range roots {
		ordered = append(ordered, root)
	}
	sort.Strings(ordered)
	for _, root := range ordered {
		indexes := roots[root]
		if failedDirs[root] {
			continue
		}
		cmd := Command{Dir: root, Name: "pnpm", Args: []string{"install", "--lockfile-only", "--ignore-scripts", "--no-frozen-lockfile"}}
		result, err := opts.Runner.Run(ctx, cmd)
		if err != nil {
			for _, i := range indexes {
				plans[i].Error = commandFailure(cmd, result, err)
			}
		}
	}
}
