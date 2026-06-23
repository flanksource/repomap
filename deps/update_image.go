package deps

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/flanksource/repomap"
	"github.com/flanksource/repomap/imageupdate"
)

func discoverImageUpdateCandidates(root string, managers []Manager, filter DiscoverFilter) ([]UpdateCandidate, error) {
	selected := managerSet(managers)
	conf, err := repomap.GetConf(root)
	if err != nil {
		return nil, err
	}
	res, err := imageupdate.DiscoverRepoTargets(conf, root)
	if err != nil {
		return nil, err
	}
	targets := imageupdate.Filter(res.Targets, filter.Matcher, filter.ImagePatterns, filter.ChartPatterns)

	var out []UpdateCandidate
	for _, target := range targets {
		manager := managerForUpdateTarget(target)
		if manager == "" || !selected[manager] {
			continue
		}
		targetCopy := target
		out = append(out, UpdateCandidate{
			Manager: manager,
			Name:    updateTargetName(target),
			Current: updateTargetCurrentVersion(target),
			Scope:   updateTargetScope(target),
			File:    filepath.Join(conf.RepoPath(), filepath.FromSlash(target.File)),
			Dir:     conf.RepoPath(),
			Target:  &targetCopy,
		})
	}
	return out, nil
}

func managerForUpdateTarget(target imageupdate.UpdateTarget) Manager {
	switch target.Kind {
	case imageupdate.TargetImage:
		return ManagerImage
	case imageupdate.TargetChart:
		return ManagerHelm
	default:
		return ""
	}
}

func updateTargetName(target imageupdate.UpdateTarget) string {
	switch target.Kind {
	case imageupdate.TargetImage:
		if target.Image != nil {
			return target.Image.GetFullNameWithoutTag()
		}
		return stripImageVersion(target.CurrentValue)
	case imageupdate.TargetChart:
		if target.ChartName != "" {
			return target.ChartName
		}
		// Unresolved chartRef: fall back to the referenced source name so the
		// candidate still has an identity and its lookup fails loudly by name.
		return target.ChartRefName
	default:
		return target.CurrentValue
	}
}

func updateTargetCurrentVersion(target imageupdate.UpdateTarget) string {
	if target.Kind == imageupdate.TargetImage {
		return imageVersionOnly(target.CurrentValue)
	}
	return target.CurrentValue
}

func updateTargetScope(target imageupdate.UpdateTarget) string {
	ref := target.Ref.Kind
	if target.Ref.Namespace != "" {
		ref += "/" + target.Ref.Namespace
	}
	if target.Ref.Name != "" {
		ref += "/" + target.Ref.Name
	}
	switch target.Kind {
	case imageupdate.TargetImage:
		if target.ContainerName != "" {
			return ref + " container/" + target.ContainerName
		}
	case imageupdate.TargetChart:
		return ref + " chart"
	}
	return ref
}

func stripImageVersion(value string) string {
	if at := strings.Index(value, "@"); at >= 0 {
		value = value[:at]
	}
	if i := imageTagSeparator(value); i >= 0 {
		return value[:i]
	}
	return value
}

func imageVersionOnly(value string) string {
	if i := imageTagSeparator(value); i >= 0 {
		version := value[i+1:]
		if at := strings.Index(version, "@"); at >= 0 {
			version = version[:at]
		}
		return version
	}
	return value
}

func imageTagSeparator(value string) int {
	colon := strings.LastIndex(value, ":")
	if colon < 0 {
		return -1
	}
	if slash := strings.LastIndex(value, "/"); slash > colon {
		return -1
	}
	return colon
}

func availableImageTargetVersions(ctx context.Context, resolver ImageVersionResolver, candidate UpdateCandidate) ([]string, string, string, error) {
	if candidate.Target == nil {
		return nil, "", "", fmt.Errorf("%s has no image or Helm target metadata", candidate.Name)
	}
	if resolver == nil {
		resolver = imageupdate.NewResolver()
	}
	target := *candidate.Target
	latest, err := resolver.ResolveLatestVersions(ctx, target)
	if err != nil {
		return nil, "", "", err
	}
	versions, err := resolver.Available(ctx, target)
	if err != nil {
		return nil, "", "", err
	}
	return versions, latest.Stable, latest.Prerelease, nil
}

func applyImageTargetUpdate(ctx context.Context, candidate UpdateCandidate, version string, opts UpdateOptions) UpdatePlan {
	plan := planFromCandidate(candidate)
	plan.NewVersion = version
	plan.DryRun = opts.DryRun
	if selectedVersionIsCurrent(candidate.Current, version) {
		plan.Skipped = "already at selected version"
		return plan
	}
	if candidate.Target == nil {
		plan.Skipped = "missing image or Helm target metadata"
		return plan
	}
	resolver := opts.ImageResolver
	if resolver == nil {
		resolver = imageupdate.NewResolver()
	}
	target := *candidate.Target
	newValue := version
	if target.Kind == imageupdate.TargetImage {
		resolved, err := resolver.NewImageValue(ctx, target, version)
		if err != nil {
			plan.Skipped = err.Error()
			return plan
		}
		newValue = resolved
	}
	if newValue == target.CurrentValue {
		plan.Skipped = "already at selected version"
		return plan
	}
	absFile := filepath.Join(candidate.Dir, filepath.FromSlash(target.File))
	if _, err := imageupdate.ApplyEdit(absFile, target, newValue, opts.DryRun); err != nil {
		plan.Skipped = err.Error()
		return plan
	}
	plan.Written = !opts.DryRun
	if plan.Written {
		stageUpdatePlan(ctx, &plan, candidate, opts.Runner)
	}
	return plan
}
