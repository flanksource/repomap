package imageupdate

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/flanksource/commons/collections"

	"github.com/flanksource/repomap"
)

// TargetWarning is a non-fatal problem encountered while discovering a target —
// typically a chart whose Flux source (HelmRepository/OCIRepository/HelmChart)
// could not be resolved. Discovery keeps the target so it still surfaces.
type TargetWarning struct {
	File    string
	Message string
}

// DiscoverResult bundles the discovered targets, the source index used to resolve
// them, and any per-target resolution warnings.
type DiscoverResult struct {
	Targets  []UpdateTarget
	Index    *SourceIndex
	Warnings []TargetWarning
}

// DiscoverTargets runs the full image/chart discovery over a set of repo-relative
// YAML file contents: it builds the kustomize/Flux tree, indexes every chart
// source, extracts image/chart targets under scanPrefix, and resolves each chart
// target onto its source (URL + current version + edit anchor). Source-resolution
// failures become warnings (the target is kept), so a single bad HelmRelease
// never aborts discovery. scanPrefix is a repo-relative POSIX directory prefix
// ("" scans everything); sources are indexed repo-wide regardless of prefix so a
// HelmRelease under the prefix can resolve a source defined elsewhere.
func DiscoverTargets(contents map[string]string, scanPrefix string) DiscoverResult {
	tree := BuildKustomizeTree(contents)
	idx := NewSourceIndex(tree)

	files := make([]string, 0, len(contents))
	for f := range contents {
		files = append(files, f)
	}
	sort.Strings(files)

	for _, f := range files {
		_ = idx.IndexSources(f, contents[f])
	}

	var targets []UpdateTarget
	var warnings []TargetWarning
	for _, f := range files {
		if scanPrefix != "" && !strings.HasPrefix(f, scanPrefix) {
			continue
		}
		fileTargets, err := ExtractTargets(f, contents[f])
		if err != nil {
			continue
		}
		for i := range fileTargets {
			t := fileTargets[i]
			if t.Kind == TargetChart {
				if err := idx.Resolve(&t); err != nil {
					warnings = append(warnings, TargetWarning{File: f, Message: err.Error()})
				}
			}
			targets = append(targets, t)
		}
	}
	sortTargets(targets)
	return DiscoverResult{Targets: targets, Index: idx, Warnings: warnings}
}

// DiscoverRepoTargets is the convenience entry point used by the CLI: it reads
// every tracked YAML file under conf and discovers targets, scoping extraction to
// scanPath (which must live under the repo).
func DiscoverRepoTargets(conf *repomap.ArchConf, scanPath string) (DiscoverResult, error) {
	contents, err := conf.TrackedYAMLContents()
	if err != nil {
		return DiscoverResult{}, err
	}
	return DiscoverTargets(contents, scanPrefix(conf.RepoPath(), scanPath)), nil
}

// scanPrefix returns the repo-relative POSIX directory prefix for scanPath, or ""
// when scanPath is the repo root.
func scanPrefix(repoPath, scanPath string) string {
	rel, err := filepath.Rel(repoPath, scanPath)
	if err != nil || rel == "." || rel == "" {
		return ""
	}
	return filepath.ToSlash(rel) + "/"
}

func sortTargets(targets []UpdateTarget) {
	sort.SliceStable(targets, func(i, j int) bool {
		if targets[i].File != targets[j].File {
			return targets[i].File < targets[j].File
		}
		if targets[i].FieldLine != targets[j].FieldLine {
			return targets[i].FieldLine < targets[j].FieldLine
		}
		return targetName(targets[i]) < targetName(targets[j])
	})
}

func targetName(t UpdateTarget) string {
	if t.Kind == TargetImage && t.Image != nil {
		return t.Image.GetFullNameWithoutTag()
	}
	return t.ChartName
}

// Filter applies a resource matcher plus image/chart name patterns to a target
// set. Empty matcher and empty patterns return the targets unchanged.
func Filter(targets []UpdateTarget, matcher repomap.ResourceMatcher, imagePatterns, chartPatterns []string) []UpdateTarget {
	var out []UpdateTarget
	for _, t := range targets {
		if !matcher.MatchesRef(t.Ref) {
			continue
		}
		if t.Kind == TargetImage && len(imagePatterns) > 0 {
			name := ""
			if t.Image != nil {
				name = t.Image.GetFullNameWithoutTag()
			}
			if matched, _ := collections.MatchItem(name, imagePatterns...); !matched {
				continue
			}
		}
		if t.Kind == TargetChart && len(chartPatterns) > 0 {
			if matched, _ := collections.MatchItem(t.ChartName, chartPatterns...); !matched {
				continue
			}
		}
		out = append(out, t)
	}
	return out
}
