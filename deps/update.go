package deps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/clicky/task"
	"github.com/flanksource/commons/collections"
	flanksourceContext "github.com/flanksource/commons/context"
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
type VersionSelector func(UpdateChoice) (string, bool)

type ImageVersionResolver interface {
	Available(context.Context, imageupdate.UpdateTarget) ([]string, error)
	ResolveLatestVersions(context.Context, imageupdate.UpdateTarget) (imageupdate.LatestVersions, error)
	NewImageValue(context.Context, imageupdate.UpdateTarget, string) (string, error)
}

type UpdateOptions struct {
	Managers         []Manager
	Expression       []string
	Check            bool
	DryRun           bool
	Runner           CommandRunner
	ImageResolver    ImageVersionResolver
	SelectCandidates CandidateSelector
	SelectVersion    VersionSelector
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
	managers, err := updateManagers(opts.Managers)
	if err != nil {
		return nil, err
	}
	patterns := splitUpdatePatterns(opts.Expression)
	if len(patterns) == 0 {
		return nil, fmt.Errorf("dependency update expression is required")
	}
	if opts.Runner == nil {
		opts.Runner = ExecRunner{}
	}

	candidates, err := DiscoverUpdateCandidates(path, managers)
	if err != nil {
		return nil, err
	}
	candidates = filterUpdateCandidates(candidates, patterns)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no direct dependencies matched %q", strings.Join(patterns, ","))
	}

	choices, plansByKey := resolveUpdateChoices(ctx, candidates, opts)
	if len(choices) == 0 {
		return orderedUpdatePlans(candidates, plansByKey), nil
	}
	if opts.Check {
		for _, choice := range choices {
			plansByKey[choice.Candidate.key()] = checkUpdatePlan(choice)
		}
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
	for _, choice := range sortSelectedUpdateChoicesByFile(selected) {
		version, ok := selectVersion(choice)
		if !ok || strings.TrimSpace(version) == "" {
			plansByKey[choice.Candidate.key()] = skippedUpdatePlan(choice.Candidate, "no version selected")
			continue
		}
		plansByKey[choice.Candidate.key()] = applyDependencyUpdate(ctx, choice.Candidate, version, opts)
	}
	return orderedUpdatePlans(candidates, plansByKey), nil
}

func DiscoverUpdateCandidates(path string, managers []Manager) ([]UpdateCandidate, error) {
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
		candidates, err := discoverImageUpdateCandidates(absPath, imageManagers)
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

func resolveUpdateChoices(ctx context.Context, candidates []UpdateCandidate, opts UpdateOptions) ([]UpdateChoice, map[string]UpdatePlan) {
	type result struct {
		versions         []string
		latestStable     string
		latestPrerelease string
		err              error
	}
	results := make([]result, len(candidates))
	group := task.StartGroup[int]("Resolving dependency versions", task.WithConcurrency(updateResolveConcurrency))
	for i, candidate := range candidates {
		idx, c := i, candidate
		group.Add(updateTaskName(c), func(_ flanksourceContext.Context, tk *task.Task) (int, error) {
			tk.Infof("looking up published versions")
			versions, latestStable, latestPrerelease, err := resolveCandidateVersions(ctx, opts, c)
			results[idx] = result{versions: versions, latestStable: latestStable, latestPrerelease: latestPrerelease, err: err}
			if err != nil {
				tk.Warnf("%s", err.Error())
				tk.Warning()
			} else if len(versions) == 0 {
				tk.Infof("already up to date")
				tk.Success()
			} else {
				tk.Success()
			}
			return idx, nil
		})
	}
	_, _ = group.GetResults()

	plansByKey := map[string]UpdatePlan{}
	choices := make([]UpdateChoice, 0, len(candidates))
	for i, candidate := range candidates {
		result := results[i]
		if result.err != nil {
			plansByKey[candidate.key()] = skippedUpdatePlan(candidate, result.err.Error())
			continue
		}
		if len(result.versions) == 0 {
			continue
		}
		choices = append(choices, UpdateChoice{
			Candidate:        candidate,
			Versions:         result.versions,
			LatestStable:     result.latestStable,
			LatestPrerelease: result.latestPrerelease,
		})
	}
	return choices, plansByKey
}

func resolveCandidateVersions(ctx context.Context, opts UpdateOptions, candidate UpdateCandidate) ([]string, string, string, error) {
	var (
		versions []string
		err      error
	)
	switch candidate.Manager {
	case ManagerImage, ManagerHelm:
		versions, _, _, err = availableImageTargetVersions(ctx, opts.ImageResolver, candidate)
	default:
		versions, err = AvailableDependencyVersions(ctx, opts.Runner, candidate)
	}
	if err != nil {
		return nil, "", "", err
	}
	versions = updateableVersions(candidate.Current, versions)
	return versions, latestStableVersion(versions), latestPrereleaseVersion(versions), nil
}

func AvailableDependencyVersions(ctx context.Context, runner CommandRunner, candidate UpdateCandidate) ([]string, error) {
	if runner == nil {
		runner = ExecRunner{}
	}
	var (
		result CommandResult
		err    error
	)
	switch candidate.Manager {
	case ManagerGo:
		result, err = runner.Run(ctx, Command{
			Dir:  candidate.Dir,
			Name: "go",
			Args: []string{"list", "-m", "-versions", "-json", candidate.Name},
			Env:  []string{"GOFLAGS=-mod=readonly"},
		})
	case ManagerNPM:
		result, err = runner.Run(ctx, Command{
			Dir:  candidate.Dir,
			Name: "npm",
			Args: []string{"view", candidate.Name, "versions", "--json"},
		})
	case ManagerPNPM:
		result, err = runner.Run(ctx, Command{
			Dir:  candidate.Dir,
			Name: "pnpm",
			Args: []string{"view", candidate.Name, "versions", "--json"},
		})
	default:
		return nil, fmt.Errorf("dependency version lookup does not support manager %q", candidate.Manager)
	}
	if err != nil {
		if result.Stderr != "" {
			return nil, fmt.Errorf("%s: %w", strings.TrimSpace(result.Stderr), err)
		}
		return nil, err
	}
	versions, err := parseAvailableVersions(candidate.Manager, result.Stdout)
	if err != nil {
		return nil, err
	}
	return sortDependencyVersions(versions), nil
}

func parseAvailableVersions(manager Manager, stdout string) ([]string, error) {
	stdout = strings.TrimSpace(stdout)
	if stdout == "" {
		return nil, nil
	}
	if manager == ManagerGo {
		var payload struct {
			Versions []string `json:"Versions"`
		}
		if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
			return nil, err
		}
		return payload.Versions, nil
	}
	var list []string
	if err := json.Unmarshal([]byte(stdout), &list); err == nil {
		return list, nil
	}
	var single string
	if err := json.Unmarshal([]byte(stdout), &single); err == nil && single != "" {
		return []string{single}, nil
	}
	return nil, fmt.Errorf("expected JSON version array")
}

func applyDependencyUpdate(ctx context.Context, candidate UpdateCandidate, version string, opts UpdateOptions) UpdatePlan {
	if candidate.Manager == ManagerImage || candidate.Manager == ManagerHelm {
		return applyImageTargetUpdate(ctx, candidate, version, opts)
	}
	plan := planFromCandidate(candidate)
	plan.NewVersion = version
	if selectedVersionIsCurrent(candidate.Current, version) {
		plan.Skipped = "already at selected version"
		return plan
	}
	cmd, err := updateCommand(candidate, version)
	if err != nil {
		plan.Skipped = err.Error()
		return plan
	}
	plan.Command = append([]string{cmd.Name}, cmd.Args...)
	if opts.DryRun {
		plan.DryRun = true
		return plan
	}
	runner := opts.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	if _, err := runner.Run(ctx, cmd); err != nil {
		plan.Skipped = err.Error()
		return plan
	}
	plan.Written = true
	stageUpdatePlan(ctx, &plan, candidate, runner)
	return plan
}

func stageUpdatePlan(ctx context.Context, plan *UpdatePlan, candidate UpdateCandidate, runner CommandRunner) {
	staged, err := stageUpdatedFiles(ctx, runner, candidate)
	plan.Staged = staged
	if err != nil {
		plan.StageError = err.Error()
	}
}

func updateCommand(candidate UpdateCandidate, version string) (Command, error) {
	target := candidate.Name + "@" + version
	switch candidate.Manager {
	case ManagerGo:
		return Command{Dir: candidate.Dir, Name: "go", Args: []string{"get", target}}, nil
	case ManagerNPM:
		args := []string{"install", "--package-lock-only", "--ignore-scripts"}
		if flag := packageSaveFlag(candidate.Scope); flag != "" {
			args = append(args, flag)
		}
		args = append(args, target)
		return Command{Dir: candidate.Dir, Name: "npm", Args: args}, nil
	case ManagerPNPM:
		args := []string{"add", "--lockfile-only", "--ignore-scripts"}
		if flag := packageSaveFlag(candidate.Scope); flag != "" {
			args = append(args, flag)
		}
		args = append(args, target)
		return Command{Dir: candidate.Dir, Name: "pnpm", Args: args}, nil
	default:
		return Command{}, fmt.Errorf("package-manager updates do not support manager %q", candidate.Manager)
	}
}

func packageSaveFlag(scope string) string {
	switch scope {
	case "dependencies":
		return "--save-prod"
	case "devDependencies":
		return "--save-dev"
	case "optionalDependencies":
		return "--save-optional"
	case "peerDependencies":
		return "--save-peer"
	default:
		return ""
	}
}

func promptUpdateCandidates(choices []UpdateChoice) ([]UpdateChoice, bool) {
	return runUpdateChoiceTreePicker(choices)
}

func promptUpdateVersion(choice UpdateChoice) (string, bool) {
	candidate := choice.Candidate
	title := fmt.Sprintf("Select version for %s in %s (current %s)", candidate.Name, candidate.File, candidate.Current)
	return clicky.PromptSelect(choice.Versions, clicky.PromptSelectOptions[string]{
		Title:    title,
		PageSize: 12,
		Render: func(version string) api.Textable {
			text := clicky.Text(version, "font-mono")
			var tags []string
			if selectedVersionIsCurrent(candidate.Current, version) {
				tags = append(tags, "current")
			}
			if version == choice.LatestStable {
				tags = append(tags, "latest stable")
			}
			if version == choice.LatestPrerelease {
				tags = append(tags, "latest pre-release")
			}
			if isPrerelease(version) {
				tags = append(tags, "pre-release")
			}
			if len(tags) > 0 {
				text = text.Space().Append("("+strings.Join(tags, ", ")+")", "text-muted")
			}
			return text
		},
	})
}

type updateChoiceFileGroup struct {
	File    string
	Choices []UpdateChoice
}

func groupUpdateChoicesByFile(choices []UpdateChoice) []updateChoiceFileGroup {
	byFile := map[string][]UpdateChoice{}
	for _, choice := range choices {
		file := choice.Candidate.File
		byFile[file] = append(byFile[file], choice)
	}
	files := make([]string, 0, len(byFile))
	for file := range byFile {
		files = append(files, file)
	}
	sort.Strings(files)
	groups := make([]updateChoiceFileGroup, 0, len(files))
	for _, file := range files {
		groupChoices := append([]UpdateChoice(nil), byFile[file]...)
		sort.SliceStable(groupChoices, func(i, j int) bool {
			return groupChoices[i].Candidate.less(groupChoices[j].Candidate)
		})
		groups = append(groups, updateChoiceFileGroup{File: file, Choices: groupChoices})
	}
	return groups
}

func sortSelectedUpdateChoicesByFile(choices []UpdateChoice) []UpdateChoice {
	var out []UpdateChoice
	for _, group := range groupUpdateChoicesByFile(choices) {
		out = append(out, group.Choices...)
	}
	return out
}

func (p UpdatePlan) Pretty() api.Text {
	t := clicky.Text(fmt.Sprintf("[%s] %s", p.Manager, p.Name), managerStyle(p.Manager))
	if p.OldVersion != "" || p.NewVersion != "" {
		t = t.Space().Append(p.OldVersion, "font-mono text-muted")
		if p.NewVersion != "" {
			t = t.Append(" -> ", "text-muted").Append(p.NewVersion, "font-mono text-green-600")
		}
	}
	switch {
	case p.Skipped != "":
		t = t.Space().Append("skipped: "+p.Skipped, "text-muted")
	case p.Checked:
		t = t.Space().Append("update available", "text-green-600")
	case p.DryRun:
		t = t.Space().Append("(dry-run)", "text-yellow-600")
	case p.Written:
		t = t.Space().Append("written", "text-green-600")
	}
	if len(p.Staged) > 0 {
		t = t.Space().Append("staged "+strings.Join(p.Staged, ", "), "text-muted")
	}
	if p.StageError != "" {
		t = t.Space().Append("stage failed: "+p.StageError, "text-yellow-600")
	}
	return t
}

func (UpdatePlan) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		api.Column("manager").Label("Manager").Build(),
		api.Column("dependency").Label("Dependency").Build(),
		api.Column("file").Label("File").Build(),
		api.Column("scope").Label("Scope").Build(),
		api.Column("change").Label("Change").Build(),
		api.Column("status").Label("Status").Build(),
	}
}

func (p UpdatePlan) Row() map[string]any {
	row := map[string]any{
		"manager":    clicky.Text(string(p.Manager), managerStyle(p.Manager)),
		"dependency": clicky.Text(p.Name, "font-bold text-cyan-600"),
		"file":       clicky.Text(p.File, "font-mono"),
		"scope":      clicky.Text(p.Scope, "text-muted"),
		"change":     updateChangeText(p.OldVersion, p.NewVersion),
	}
	switch {
	case p.Skipped != "":
		row["status"] = clicky.Text(p.Skipped, "text-muted")
	case p.Checked:
		row["status"] = clicky.Text("update available", "text-green-600")
	case p.DryRun:
		row["status"] = clicky.Text("dry-run", "text-yellow-600")
	case p.Written:
		status := clicky.Text("written", "text-green-600")
		if len(p.Staged) > 0 {
			status = status.Append(" + staged "+strings.Join(p.Staged, ", "), "text-muted")
		}
		if p.StageError != "" {
			status = status.Append(" (stage failed: "+p.StageError+")", "text-yellow-600")
		}
		row["status"] = status
	default:
		row["status"] = clicky.Text("")
	}
	return row
}

func updateChangeText(oldVersion, newVersion string) api.Text {
	text := clicky.Text(oldVersion, "font-mono text-muted")
	if newVersion != "" {
		text = text.Append(" -> ", "text-muted").Append(newVersion, "font-mono text-green-600")
	}
	return text
}

func planFromCandidate(candidate UpdateCandidate) UpdatePlan {
	return UpdatePlan{
		Manager:    candidate.Manager,
		Name:       candidate.Name,
		File:       candidate.File,
		Scope:      candidate.Scope,
		OldVersion: candidate.Current,
	}
}

func skippedUpdatePlan(candidate UpdateCandidate, reason string) UpdatePlan {
	plan := planFromCandidate(candidate)
	plan.Skipped = reason
	return plan
}

func checkUpdatePlan(choice UpdateChoice) UpdatePlan {
	plan := planFromCandidate(choice.Candidate)
	plan.NewVersion = checkUpdateVersion(choice)
	plan.Checked = true
	return plan
}

func checkUpdateVersion(choice UpdateChoice) string {
	if choice.LatestStable != "" {
		return choice.LatestStable
	}
	if choice.LatestPrerelease != "" {
		return choice.LatestPrerelease
	}
	if len(choice.Versions) > 0 {
		return choice.Versions[0]
	}
	return ""
}

func orderedUpdatePlans(candidates []UpdateCandidate, plansByKey map[string]UpdatePlan) []UpdatePlan {
	plans := make([]UpdatePlan, 0, len(plansByKey))
	for _, candidate := range candidates {
		if plan, ok := plansByKey[candidate.key()]; ok {
			plans = append(plans, plan)
		}
	}
	return plans
}

func updateManagers(managers []Manager) ([]Manager, error) {
	if len(managers) == 0 {
		return []Manager{ManagerGo, ManagerNPM, ManagerPNPM, ManagerImage, ManagerHelm}, nil
	}
	out := make([]Manager, 0, len(managers))
	var unsupported []string
	for _, manager := range managers {
		if !supportedUpdateManagers[manager] {
			unsupported = append(unsupported, string(manager))
			continue
		}
		out = append(out, manager)
	}
	if len(unsupported) > 0 {
		sort.Strings(unsupported)
		return nil, fmt.Errorf("dependency updates currently support go, npm, pnpm, image, and helm; unsupported manager(s): %s", strings.Join(unsupported, ", "))
	}
	return out, nil
}

func packageUpdateManagers(managers []Manager) []Manager {
	var out []Manager
	for _, manager := range managers {
		switch manager {
		case ManagerGo, ManagerNPM, ManagerPNPM:
			out = append(out, manager)
		}
	}
	return out
}

func imageUpdateManagers(managers []Manager) []Manager {
	var out []Manager
	for _, manager := range managers {
		switch manager {
		case ManagerImage, ManagerHelm:
			out = append(out, manager)
		}
	}
	return out
}

func splitUpdatePatterns(values []string) []string {
	var out []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func (c UpdateCandidate) matches(patterns []string) bool {
	identityPatterns, pathPatterns := splitExplicitPathPatterns(patterns)
	hasPositivePattern := identityPatterns.hasPositive || pathPatterns.hasPositive
	matchedPositive := false

	for _, value := range c.matchValues() {
		if value == "" {
			continue
		}
		ok, negated := collections.MatchItem(value, identityPatterns.values...)
		if negated {
			return false
		}
		if ok && identityPatterns.hasPositive {
			matchedPositive = true
		}
	}
	if len(pathPatterns.values) > 0 {
		ok, negated := collections.MatchItem(c.File, pathPatterns.values...)
		if negated {
			return false
		}
		if ok && pathPatterns.hasPositive {
			matchedPositive = true
		}
	}
	if hasPositivePattern {
		return matchedPositive
	}
	return true
}

func (c UpdateCandidate) matchValues() []string {
	return []string{
		c.Name,
		string(c.Manager),
		fmt.Sprintf("%s:%s", c.Manager, c.Name),
		fmt.Sprintf("%s:%s@%s", c.Manager, c.Name, c.Current),
		c.Scope,
		c.Current,
	}
}

type updatePatternSet struct {
	values      []string
	hasPositive bool
}

func splitExplicitPathPatterns(patterns []string) (identity updatePatternSet, path updatePatternSet) {
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		negated := strings.HasPrefix(pattern, "!")
		body := strings.TrimPrefix(pattern, "!")
		field, value, explicit := strings.Cut(body, ":")
		if explicit && (field == "path" || field == "file") {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if strings.HasPrefix(value, "!") {
				negated = true
				value = strings.TrimPrefix(value, "!")
			}
			if negated {
				value = "!" + value
			} else {
				path.hasPositive = true
			}
			path.values = append(path.values, value)
			continue
		}
		if !negated {
			identity.hasPositive = true
		}
		identity.values = append(identity.values, pattern)
	}
	return identity, path
}

func (c UpdateCandidate) key() string {
	return strings.Join([]string{string(c.Manager), c.Dir, c.File, c.Scope, c.Name}, "\x00")
}

func (c UpdateCandidate) less(other UpdateCandidate) bool {
	if c.Manager != other.Manager {
		return c.Manager < other.Manager
	}
	if c.File != other.File {
		return c.File < other.File
	}
	if c.Scope != other.Scope {
		return c.Scope < other.Scope
	}
	return c.Name < other.Name
}

func updateTaskName(candidate UpdateCandidate) string {
	return fmt.Sprintf("%s %s", candidate.Manager, candidate.Name)
}

func isLocalUpdateSpec(spec string) bool {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return true
	}
	if strings.HasPrefix(spec, "workspace:") {
		return true
	}
	return isLocalRef(spec)
}

func sortDependencyVersions(versions []string) []string {
	type parsed struct {
		orig string
		ver  *semver.Version
	}
	seen := map[string]bool{}
	var parsedVersions []parsed
	for _, version := range versions {
		version = strings.TrimSpace(version)
		if version == "" || seen[version] {
			continue
		}
		seen[version] = true
		sv, err := semver.NewVersion(version)
		if err != nil {
			continue
		}
		parsedVersions = append(parsedVersions, parsed{orig: version, ver: sv})
	}
	sort.Slice(parsedVersions, func(i, j int) bool {
		return parsedVersions[i].ver.GreaterThan(parsedVersions[j].ver)
	})
	out := make([]string, len(parsedVersions))
	for i, item := range parsedVersions {
		out[i] = item.orig
	}
	return out
}

func latestStableVersion(versions []string) string {
	for _, version := range versions {
		if !isPrerelease(version) {
			return version
		}
	}
	return ""
}

func latestPrereleaseVersion(versions []string) string {
	for _, version := range versions {
		if isPrerelease(version) {
			return version
		}
	}
	return ""
}

func isPrerelease(version string) bool {
	sv, err := semver.NewVersion(version)
	return err == nil && strings.TrimSpace(sv.Prerelease()) != ""
}

func updateableVersions(current string, versions []string) []string {
	current = normalizeCurrentVersion(current)
	currentSemver, currentErr := semver.NewVersion(current)
	out := make([]string, 0, len(versions))
	for _, version := range versions {
		if version == "" || version == current {
			continue
		}
		versionSemver, versionErr := semver.NewVersion(version)
		if currentErr != nil || versionErr != nil {
			out = append(out, version)
			continue
		}
		if versionSemver.GreaterThan(currentSemver) {
			out = append(out, version)
		}
	}
	return out
}

func selectedVersionIsCurrent(current, selected string) bool {
	return normalizeCurrentVersion(current) == selected
}

func normalizeCurrentVersion(current string) string {
	current = strings.TrimSpace(current)
	current = strings.TrimPrefix(current, "npm:")
	current = strings.TrimLeft(current, "^~<>= ")
	if idx := strings.IndexAny(current, " |,"); idx >= 0 {
		current = current[:idx]
	}
	return strings.TrimSpace(current)
}
