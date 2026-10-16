package deps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/flanksource/repomap/deps/internal/actions"
)

func applyActionBatch(ctx context.Context, batch updateBatch, requests []updateRequest, plans []UpdatePlan, opts UpdateOptions) {
	file := filepath.Join(batch.Dir, filepath.Base(batch.File))
	content, err := os.ReadFile(file)
	if err != nil {
		failActionBatch(batch, plans, fmt.Errorf("read %s: %w", file, err))
		return
	}
	edits, err := actionBatchEdits(ctx, batch, requests, plans, opts)
	if err != nil {
		failActionBatch(batch, plans, err)
		return
	}
	if len(edits) == 0 {
		return
	}
	updated, err := actions.Apply(content, edits)
	if err != nil {
		failActionBatch(batch, plans, fmt.Errorf("edit %s: %w", file, err))
		return
	}
	if !opts.DryRun {
		info, statErr := os.Stat(file)
		if statErr == nil {
			statErr = os.WriteFile(file, updated, info.Mode())
		}
		if statErr != nil {
			failActionBatch(batch, plans, fmt.Errorf("write %s: %w", file, statErr))
			return
		}
	}
	for _, i := range batch.Indexes {
		if plans[i].Skipped == "" {
			plans[i].DryRun = opts.DryRun
			plans[i].Written = !opts.DryRun
		}
	}
}

func actionBatchEdits(ctx context.Context, batch updateBatch, requests []updateRequest, plans []UpdatePlan, opts UpdateOptions) ([]actions.Edit, error) {
	var edits []actions.Edit
	for _, i := range batch.Indexes {
		request := requests[i]
		plans[i] = planFromCandidate(request.Candidate)
		plans[i].NewVersion = request.Version
		ref := request.Candidate.action
		if ref == nil {
			return nil, fmt.Errorf("%s has no GitHub Actions reference", request.Candidate.Name)
		}
		tags, err := opts.actionResolver.Load(ctx, ref.Repository)
		if err != nil {
			return nil, err
		}
		sha, err := tags.Resolve(request.Version)
		if err != nil {
			return nil, err
		}
		newRef := request.Version
		if ref.Pinned {
			newRef = sha
			plans[i].ResolvedRef = sha
		}
		if newRef == ref.Ref && (!ref.Pinned || ref.VersionComment == "" || ref.VersionComment == request.Version) {
			plans[i].Skipped = "already at selected version"
			continue
		}
		edits = append(edits, actions.Edit{Line: ref.Line, OldValue: ref.Name + "@" + ref.Ref, NewValue: ref.Name + "@" + newRef, VersionTag: actionVersionComment(ref, request.Version)})
	}
	return edits, nil
}

func actionVersionComment(ref *actions.Reference, version string) string {
	if ref.Pinned && ref.VersionComment != "" {
		return version
	}
	return ""
}

func failActionBatch(batch updateBatch, plans []UpdatePlan, err error) {
	for _, i := range batch.Indexes {
		plans[i].Written = false
		plans[i].DryRun = false
		plans[i].Error = err.Error()
	}
}
