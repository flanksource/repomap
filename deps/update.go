package deps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/flanksource/repomap"
	"github.com/flanksource/repomap/imageupdate"
	"golang.org/x/mod/modfile"
)

const updateResolveConcurrency = 8

var supportedUpdateManagers = map[Manager]bool{
	ManagerGo:    true,
	ManagerNPM:   true,
	ManagerPNPM:  true,
	ManagerImage: true,
	ManagerHelm:  true,
}

type CandidateSelector func([]UpdateChoice) ([]UpdateChoice, bool)
type VersionSelector func(UpdateVersionPrompt) (string, bool)

type ImageVersionResolver interface {
	Available(context.Context, imageupdate.UpdateTarget) ([]string, error)
	ResolveLatestVersions(context.Context, imageupdate.UpdateTarget) (imageupdate.LatestVersions, error)
	NewImageValue(context.Context, imageupdate.UpdateTarget, string) (string, error)
}

type UpdateOptions struct {
	Managers []Manager
	Filters  []string

	// Resource filters for image/helm targets (ignored by package managers).
	Kind      []string
	Namespace []string
	Name      []string
	Selector  []string
	Image     []string
	Chart     []string

	// Latest resolves each matched dependency to its highest stable version;
	// Version applies an explicit version to all matched dependencies. They are
	// mutually exclusive and both bypass the interactive pickers.
	Latest  bool
	Version string

	Check            bool
	DryRun           bool
	Runner           CommandRunner
	ImageResolver    ImageVersionResolver
	SelectCandidates CandidateSelector
	SelectVersion    VersionSelector
}

// DiscoverFilter narrows image/helm targets during discovery by Kubernetes
// resource metadata and image/chart name patterns. It has no effect on package
// managers, whose candidates come from manifest/lockfile parsing.
type DiscoverFilter struct {
	Matcher       repomap.ResourceMatcher
	ImagePatterns []string
	ChartPatterns []string
}

type UpdateCandidate struct {
	Manager Manager                   `json:"manager"`
	Name    string                    `json:"name"`
	Current string                    `json:"current"`
	Scope   string                    `json:"scope,omitempty"`
	File    string                    `json:"file"`
	Dir     string                    `json:"dir"`
	Target  *imageupdate.UpdateTarget `json:"-"`
}

type UpdateChoice struct {
	Candidate        UpdateCandidate `json:"candidate"`
	Versions         []string        `json:"versions"`
	LatestStable     string          `json:"latest_stable,omitempty"`
	LatestPrerelease string          `json:"latest_prerelease,omitempty"`
}

// UpdateVersionPrompt is a single version question standing in for every
// selected occurrence of the same dependency. Candidate is the first
// occurrence; Files lists each distinct manifest the answer will be written to.
type UpdateVersionPrompt struct {
	UpdateChoice
	Files []string `json:"files"`
}

type UpdatePlan struct {
	Manager    Manager  `json:"manager"`
	Name       string   `json:"name"`
	File       string   `json:"file"`
	Scope      string   `json:"scope,omitempty"`
	OldVersion string   `json:"old_version"`
	NewVersion string   `json:"new_version,omitempty"`
	Command    []string `json:"command,omitempty"`
	Written    bool     `json:"written"`
	DryRun     bool     `json:"dry_run"`
	Checked    bool     `json:"checked,omitempty"`
	Skipped    string   `json:"skipped,omitempty"`
	Staged     []string `json:"staged,omitempty"`
	StageError string   `json:"stage_error,omitempty"`
}

func Update(ctx context.Context, path string, opts UpdateOptions) ([]UpdatePlan, error) {
	if path == "" {
		path = "."
	}
	if opts.Latest && opts.Version != "" {
		return nil, fmt.Errorf("--latest and --version are mutually exclusive")
	}
	managers, err := updateManagers(opts.Managers)
	if err != nil {
		return nil, err
	}
	patterns := splitUpdatePatterns(opts.Filters)
	if opts.Runner == nil {
		opts.Runner = ExecRunner{}
	}

	filter := DiscoverFilter{
		Matcher:       repomap.NewResourceMatcher(opts.Kind, opts.Namespace, opts.Name, opts.Selector),
		ImagePatterns: splitUpdatePatterns(opts.Image),
		ChartPatterns: splitUpdatePatterns(opts.Chart),
	}
	candidates, err := DiscoverUpdateCandidates(path, managers, filter)
	if err != nil {
		return nil, err
	}
	candidates = filterUpdateCandidates(candidates, patterns)
	if len(candidates) == 0 {
		if len(patterns) > 0 {
			return nil, fmt.Errorf("no direct dependencies matched %q", strings.Join(patterns, ","))
		}
		return nil, fmt.Errorf("no updatable dependencies found")
	}

	// --check lists available updates without writing; it takes precedence over
	// the write modes so `--check --version`/`--check --latest` never mutate files.
	if opts.Check {
		choices, plansByKey := resolveUpdateChoices(ctx, candidates, opts)
		for _, choice := range choices {
			plansByKey[choice.Candidate.key()] = checkUpdatePlan(choice)
		}
		return orderedUpdatePlans(candidates, plansByKey), nil
	}

	// --version applies an explicit version to every matched dependency without
	// listing available versions (it only validates image/helm availability).
	if opts.Version != "" {
		return applyExplicitVersionUpdates(ctx, candidates, opts), nil
	}

	choices, plansByKey := resolveUpdateChoices(ctx, candidates, opts)

	// --latest resolves each candidate to its highest stable version.
	if opts.Latest {
		applyLatestUpdates(ctx, candidates, choices, plansByKey, opts)
		return orderedUpdatePlans(candidates, plansByKey), nil
	}

	if len(choices) == 0 {
		return orderedUpdatePlans(candidates, plansByKey), nil
	}

	selectCandidates := opts.SelectCandidates
	if selectCandidates == nil {
		selectCandidates = promptUpdateCandidates
	}
	selected, ok := selectCandidates(choices)
	if !ok {
		for _, choice := range choices {
			plansByKey[choice.Candidate.key()] = skippedUpdatePlan(choice.Candidate, "selection cancelled")
		}
		return orderedUpdatePlans(candidates, plansByKey), nil
	}
	selectedKeys := map[string]UpdateChoice{}
	for _, choice := range selected {
		selectedKeys[choice.Candidate.key()] = choice
	}
	for _, choice := range choices {
		if _, ok := selectedKeys[choice.Candidate.key()]; !ok {
			plansByKey[choice.Candidate.key()] = skippedUpdatePlan(choice.Candidate, "not selected")
		}
	}

	selectVersion := opts.SelectVersion
	if selectVersion == nil {
		selectVersion = promptUpdateVersion
	}
	// A dependency repeated across manifests poses one question, so occurrences
	// sharing a version prompt are confirmed once and the answer applied to all.
	for _, group := range groupChoicesForVersionPrompt(sortSelectedUpdateChoicesByFile(selected)) {
		version, ok := selectVersion(newUpdateVersionPrompt(group))
		for _, choice := range group {
			if !ok || strings.TrimSpace(version) == "" {
				plansByKey[choice.Candidate.key()] = skippedUpdatePlan(choice.Candidate, "no version selected")
				continue
			}
			plansByKey[choice.Candidate.key()] = applyDependencyUpdate(ctx, choice.Candidate, version, opts)
		}
	}
	return orderedUpdatePlans(candidates, plansByKey), nil
}

func DiscoverUpdateCandidates(path string, managers []Manager, filter DiscoverFilter) ([]UpdateCandidate, error) {
	defaultManagers := len(managers) == 0
	managers, err := updateManagers(managers)
	if err != nil {
		return nil, err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	var out []UpdateCandidate
	var packageErr error
	if packageManagers := packageUpdateManagers(managers); len(packageManagers) > 0 {
		projects, _, err := Discover(absPath, packageManagers)
		if err != nil {
			packageErr = err
		} else {
			for _, project := range projects {
				switch project.Manager {
				case ManagerGo:
					candidates, err := discoverGoUpdateCandidates(project)
					if err != nil {
						return nil, err
					}
					out = append(out, candidates...)
				case ManagerNPM, ManagerPNPM:
					candidates, err := discoverPackageJSONUpdateCandidates(project)
					if err != nil {
						return nil, err
					}
					out = append(out, candidates...)
				}
			}
		}
	}
	if imageManagers := imageUpdateManagers(managers); len(imageManagers) > 0 {
		candidates, err := discoverImageUpdateCandidates(absPath, imageManagers, filter)
		if err != nil {
			if !defaultManagers || len(out) == 0 {
				if packageErr != nil && defaultManagers {
					return nil, packageErr
				}
				return nil, err
			}
		} else {
			out = append(out, candidates...)
		}
	}
	if len(out) == 0 && packageErr != nil {
		return nil, packageErr
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no supported dependency manifests or image/chart targets found under %s", absPath)
	}
	relativizeUpdateCandidateFiles(out)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].less(out[j])
	})
	return out, nil
}

func relativizeUpdateCandidateFiles(candidates []UpdateCandidate) {
	for i := range candidates {
		candidates[i].File = cwdRelativePath(candidates[i].File)
	}
}

func cwdRelativePath(path string) string {
	if strings.TrimSpace(path) == "" {
		return path
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return filepath.ToSlash(absPath)
	}
	rel, err := filepath.Rel(cwd, absPath)
	if err != nil {
		return filepath.ToSlash(absPath)
	}
	if rel == "." {
		return filepath.ToSlash(filepath.Base(absPath))
	}
	return filepath.ToSlash(rel)
}

func discoverGoUpdateCandidates(project Project) ([]UpdateCandidate, error) {
	data, err := os.ReadFile(filepath.Join(project.Dir, "go.mod"))
	if err != nil {
		return nil, err
	}
	file, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return nil, err
	}
	var out []UpdateCandidate
	for _, req := range file.Require {
		if req.Indirect {
			continue
		}
		if rep := goReplaceFor(file, req.Mod.Path, req.Mod.Version); rep != nil && isLocalRef(rep.New.Path) {
			continue
		}
		out = append(out, UpdateCandidate{
			Manager: ManagerGo,
			Name:    req.Mod.Path,
			Current: req.Mod.Version,
			Scope:   "require",
			File:    filepath.Join(project.Dir, "go.mod"),
			Dir:     project.Dir,
		})
	}
	return out, nil
}

func discoverPackageJSONUpdateCandidates(project Project) ([]UpdateCandidate, error) {
	path := filepath.Join(project.Dir, "package.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, err
	}
	sections := []struct {
		scope string
		deps  map[string]string
	}{
		{"dependencies", pkg.Dependencies},
		{"devDependencies", pkg.DevDependencies},
		{"optionalDependencies", pkg.OptionalDependencies},
		{"peerDependencies", pkg.PeerDependencies},
	}
	var out []UpdateCandidate
	for _, section := range sections {
		keys := make([]string, 0, len(section.deps))
		for name := range section.deps {
			keys = append(keys, name)
		}
		sort.Strings(keys)
		for _, name := range keys {
			current := strings.TrimSpace(section.deps[name])
			if isLocalUpdateSpec(current) {
				continue
			}
			out = append(out, UpdateCandidate{
				Manager: project.Manager,
				Name:    name,
				Current: current,
				Scope:   section.scope,
				File:    path,
				Dir:     project.Dir,
			})
		}
	}
	return out, nil
}

func filterUpdateCandidates(candidates []UpdateCandidate, patterns []string) []UpdateCandidate {
	out := make([]UpdateCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.matches(patterns) {
			out = append(out, candidate)
		}
	}
	return out
}

// version math, candidate matching, manager helpers, plan rendering, version
// resolution, apply, and prompts live in update_version.go, update_match.go,
// update_plan.go, update_resolve.go, update_apply.go, and update_prompt.go.
