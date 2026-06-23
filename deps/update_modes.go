package deps

import (
	"context"
	"fmt"
)

// applyExplicitVersionUpdates applies opts.Version to every candidate. For
// image/helm targets it first validates the version is published; package-manager
// updates pass the version straight to the manager, which validates it.
func applyExplicitVersionUpdates(ctx context.Context, candidates []UpdateCandidate, opts UpdateOptions) []UpdatePlan {
	plansByKey := make(map[string]UpdatePlan, len(candidates))
	for _, candidate := range candidates {
		plansByKey[candidate.key()] = applyExplicitVersion(ctx, candidate, opts)
	}
	return orderedUpdatePlans(candidates, plansByKey)
}

func applyExplicitVersion(ctx context.Context, candidate UpdateCandidate, opts UpdateOptions) UpdatePlan {
	if selectedVersionIsCurrent(candidate.Current, opts.Version) {
		return skippedUpdatePlan(candidate, "already at selected version")
	}
	if candidate.Manager == ManagerImage || candidate.Manager == ManagerHelm {
		available, _, _, err := availableImageTargetVersions(ctx, opts.ImageResolver, candidate)
		if err != nil {
			return skippedUpdatePlan(candidate, err.Error())
		}
		if !containsString(available, opts.Version) {
			return skippedUpdatePlan(candidate, fmt.Sprintf("version %q is not available", opts.Version))
		}
	}
	return applyDependencyUpdate(ctx, candidate, opts.Version, opts)
}

// applyLatestUpdates fills plansByKey with the highest-stable update for each
// candidate that has one, recording "already up to date" for the rest. Candidates
// whose version lookup already failed (a skipped plan exists) are left untouched.
func applyLatestUpdates(ctx context.Context, candidates []UpdateCandidate, choices []UpdateChoice, plansByKey map[string]UpdatePlan, opts UpdateOptions) {
	choiceByKey := make(map[string]UpdateChoice, len(choices))
	for _, choice := range choices {
		choiceByKey[choice.Candidate.key()] = choice
	}
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
		plansByKey[key] = applyDependencyUpdate(ctx, candidate, choice.LatestStable, opts)
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
