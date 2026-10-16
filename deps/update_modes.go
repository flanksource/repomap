package deps

import (
	"context"
	"fmt"

	"github.com/Masterminds/semver/v3"
)

// applyExplicitVersionUpdates applies opts.Version to every candidate. For
// image/helm targets it first validates the version is published; package-manager
// updates pass the version straight to the manager, which validates it.
func applyExplicitVersionUpdates(ctx context.Context, candidates []UpdateCandidate, opts UpdateOptions) []UpdatePlan {
	plansByKey := make(map[string]UpdatePlan, len(candidates))
	var requests []updateRequest
	for _, candidate := range candidates {
		if skipped, ok := explicitVersionSkip(ctx, candidate, opts); ok {
			plansByKey[candidate.key()] = skipped
			continue
		}
		requests = append(requests, updateRequest{Candidate: candidate, Version: opts.Version})
	}
	if plans, err := finishUpdate(orderedUpdatePlans(candidates, plansByKey)); err != nil {
		return plans
	}
	for key, plan := range applyUpdateRequests(ctx, requests, opts) {
		plansByKey[key] = plan
	}
	return orderedUpdatePlans(candidates, plansByKey)
}

// explicitVersionSkip reports the plan for a candidate --version cannot be
// applied to. Image and Helm targets are validated against their published
// versions here; package managers validate the version themselves.
func explicitVersionSkip(ctx context.Context, candidate UpdateCandidate, opts UpdateOptions) (UpdatePlan, bool) {
	if candidate.actionSkip != "" {
		return skippedUpdatePlan(candidate, candidate.actionSkip), true
	}
	if selectedVersionIsCurrent(candidate.Current, opts.Version) {
		return skippedUpdatePlan(candidate, "already at selected version"), true
	}
	if candidate.Manager == ManagerActions {
		if !candidate.action.Pinned {
			if _, err := semver.NewVersion(candidate.action.Ref); err != nil {
				return skippedUpdatePlan(candidate, fmt.Sprintf("reference %q is not a version tag", candidate.action.Ref)), true
			}
		}
		tags, err := opts.actionResolver.Load(ctx, candidate.action.Repository)
		if err != nil {
			return failedUpdatePlan(candidate, err.Error()), true
		}
		if _, err = tags.Resolve(opts.Version); err != nil {
			return failedUpdatePlan(candidate, err.Error()), true
		}
		if candidate.action.Pinned && candidate.action.VersionComment != "" {
			if _, err = tags.CurrentVersion(candidate.action.Ref, candidate.action.VersionComment); err != nil {
				return failedUpdatePlan(candidate, err.Error()), true
			}
		}
		return UpdatePlan{}, false
	}
	if candidate.Manager != ManagerImage && candidate.Manager != ManagerHelm {
		return UpdatePlan{}, false
	}
	available, _, _, err := availableImageTargetVersions(ctx, opts.ImageResolver, candidate)
	if err != nil {
		return failedUpdatePlan(candidate, err.Error()), true
	}
	if !containsString(available, opts.Version) {
		return failedUpdatePlan(candidate, fmt.Sprintf("version %q is not available", opts.Version)), true
	}
	return UpdatePlan{}, false
}

// applyLatestUpdates fills plansByKey with the highest-stable update for each
// candidate that has one, recording "already up to date" for the rest. Candidates
// whose version lookup already failed (a skipped plan exists) are left untouched.
func applyLatestUpdates(ctx context.Context, candidates []UpdateCandidate, choices []UpdateChoice, plansByKey map[string]UpdatePlan, opts UpdateOptions) {
	choiceByKey := make(map[string]UpdateChoice, len(choices))
	for _, choice := range choices {
		choiceByKey[choice.Candidate.key()] = choice
	}
	var requests []updateRequest
	for _, candidate := range candidates {
		key := candidate.key()
		if _, done := plansByKey[key]; done {
			continue
		}
		choice, ok := choiceByKey[key]
		if !ok {
			plansByKey[key] = skippedUpdatePlan(candidate, "already up to date")
			continue
		}
		if choice.LatestStable == "" {
			plansByKey[key] = skippedUpdatePlan(candidate, "no stable version available")
			continue
		}
		requests = append(requests, updateRequest{Candidate: candidate, Version: choice.LatestStable})
	}
	for key, plan := range applyUpdateRequests(ctx, requests, opts) {
		plansByKey[key] = plan
	}
}

func containsString(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}
