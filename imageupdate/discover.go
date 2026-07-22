package imageupdate

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/flanksource/commons/collections"

	"github.com/flanksource/repomap"
	"github.com/flanksource/repomap/kubernetes"
)

// TargetWarning is a non-fatal problem encountered while discovering a target —
// typically a chart whose Flux source (HelmRepository/OCIRepository/HelmChart)
// could not be resolved. Discovery keeps the target so it still surfaces.
type TargetWarning struct {
	File    string
	Message string
}

// DiscoverResult bundles the discovered targets, the source index used to resolve
// them, and any per-target resolution warnings. UntrackedTarget names a single
// scanned file that is a YAML manifest but is not git-tracked (so discovery could
// not see it); it is empty when the scan target is a directory or a tracked file.
type DiscoverResult struct {
	Targets         []UpdateTarget
	Index           *SourceIndex
	Warnings        []TargetWarning
	UntrackedTarget string
}

// DiscoverTargets runs the full image/chart discovery over a set of repo-relative
// YAML file contents: it builds the kustomize/Flux tree, indexes every chart
// source, extracts image/chart targets under scope, and resolves each chart
// target onto its source (URL + current version + edit anchor). Source-resolution
// failures become warnings (the target is kept), so a single bad HelmRelease
// never aborts discovery. scope is a repo-relative POSIX match scope ("" scans
// everything, a "/"-terminated value is a directory prefix, anything else is an
// exact file path); sources are indexed repo-wide regardless of scope so a
// HelmRelease under the scope can resolve a source defined elsewhere.
func DiscoverTargets(contents map[string]string, scope string) DiscoverResult {
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
		if !inScanScope(f, scope) {
			continue
		}
		fileTargets, err := ExtractTargets(f, contents[f])
		if err != nil {
			continue
		}
		for i := range fileTargets {
			t := fileTargets[i]
			if effNS := tree.EffectiveNamespace(t.File); effNS != "" {
				t.Ref.Namespace = effNS
			}
			if t.Kind == TargetChart {
				if err := idx.Resolve(&t); err != nil {
					t.SourceErr = err.Error()
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
// scanPath (which must live under the repo). When scanPath is a single YAML file
// that is not git-tracked, the result's UntrackedTarget is set so the caller can
// report the reason instead of silently finding nothing.
func DiscoverRepoTargets(conf *repomap.ArchConf, scanPath string) (DiscoverResult, error) {
	contents, err := conf.TrackedYAMLContents()
	if err != nil {
		return DiscoverResult{}, err
	}
	scope := scanScope(conf.RepoPath(), scanPath)
	result := DiscoverTargets(contents, scope)
	if untracked := untrackedFileTarget(scanPath, scope, contents); untracked != "" {
		result.UntrackedTarget = untracked
	}
	return result, nil
}

// untrackedFileTarget returns the repo-relative path of scanPath when it is a
// single YAML file (an exact-file scope) that is absent from the git-tracked
// contents, otherwise "". Directory scopes and tracked files return "".
func untrackedFileTarget(scanPath, scope string, contents map[string]string) string {
	if scope == "" || strings.HasSuffix(scope, "/") || !kubernetes.IsYaml(scanPath) {
		return ""
	}
	if _, tracked := contents[scope]; tracked {
		return ""
	}
	return scope
}

// scanScope returns the repo-relative POSIX match scope for scanPath: "" for the
// repo root (match everything), a directory prefix ending in "/" for a directory,
// or the exact repo-relative file path for a single file.
func scanScope(repoPath, scanPath string) string {
	rel, err := filepath.Rel(repoPath, scanPath)
	if err != nil || rel == "." || rel == "" {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if info, err := os.Stat(scanPath); err == nil && !info.IsDir() {
		return rel
	}
	return rel + "/"
}

// inScanScope reports whether repo-relative file f falls under scope: an empty
// scope matches everything, a "/"-terminated scope is a directory prefix, and any
// other scope matches that exact file.
func inScanScope(f, scope string) bool {
	switch {
	case scope == "":
		return true
	case strings.HasSuffix(scope, "/"):
		return strings.HasPrefix(f, scope)
	default:
		return f == scope
	}
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
