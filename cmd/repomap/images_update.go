package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/clicky/task"
	flanksourceContext "github.com/flanksource/commons/context"

	"github.com/flanksource/repomap"
	"github.com/flanksource/repomap/imageupdate"
	"github.com/flanksource/repomap/kubernetes"
)

type UpdateImageOptions struct {
	imageFilterOptions
	Latest  bool   `json:"latest" flag:"latest" help:"Resolve each target to the highest stable semver"`
	Version string `json:"version" flag:"version" help:"Apply this concrete version/tag to all matched targets"`
	DryRun  bool   `json:"dry_run" flag:"dry-run" help:"Show planned edits without writing"`
}

func (opts UpdateImageOptions) GetName() string { return "update" }

func (opts UpdateImageOptions) Help() api.Text {
	return clicky.Text(`Update container image tags and Helm chart versions in tracked manifests.

Discovers apps/v1 workload images and Flux HelmRelease chart versions in
git-tracked YAML, resolves the target version from the container registry or
Helm repository, and edits the manifest in place (preserving comments).

With --latest the highest stable version is chosen. With --version a specific
version is applied. With neither, an interactive picker lists available versions.

EXAMPLES:
  repomap images update -n default -k HelmRelease --latest
  repomap images update -k Deployment --image nginx --version 1.27.0
  repomap images update -k HelmRelease --dry-run`)
}

func init() {
	cmd := clicky.AddCommand(imagesCmd, UpdateImageOptions{}, runUpdateImage)
	cmd.Short = "Update image tags and Helm chart versions in tracked manifests"
}

// UpdatePlan is one resolved (and possibly applied) version change.
type UpdatePlan struct {
	Ref      kubernetes.KubernetesRef `json:"ref"`
	Kind     imageupdate.TargetKind   `json:"kind"`
	File     string                   `json:"file"`
	Field    string                   `json:"field"`
	OldValue string                   `json:"old_value"`
	NewValue string                   `json:"new_value"`
	Written  bool                     `json:"written"`
	DryRun   bool                     `json:"dry_run"`
	Skipped  string                   `json:"skipped,omitempty"`
}

func (p UpdatePlan) Pretty() api.Text {
	t := p.Ref.Pretty().Space()
	if p.Skipped != "" {
		return t.Append("skipped: "+p.Skipped, "text-muted")
	}
	t = t.Append(kubernetes.VersionChange{OldVersion: p.OldValue, NewVersion: p.NewValue}.Pretty())
	switch {
	case p.DryRun:
		t = t.Space().Append("(dry-run)", "text-yellow-600")
	case p.Written:
		t = t.Space().Append("written", "text-green-600")
	}
	return t
}

func (UpdatePlan) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		api.Column("resource").Label("Resource").Build(),
		api.Column("file").Label("File").Build(),
		api.Column("change").Label("Change").Build(),
		api.Column("status").Label("Status").Build(),
	}
}

func (p UpdatePlan) Row() map[string]any {
	row := map[string]any{
		"resource": p.Ref.Pretty(),
		"file":     clicky.Text(p.File, "font-mono"),
		"change":   kubernetes.VersionChange{OldVersion: p.OldValue, NewVersion: p.NewValue}.Pretty(),
	}
	switch {
	case p.Skipped != "":
		row["status"] = clicky.Text(p.Skipped, "text-muted")
	case p.DryRun:
		row["status"] = clicky.Text("dry-run", "text-yellow-600")
	case p.Written:
		row["status"] = clicky.Text("written", "text-green-600")
	}
	return row
}

func runUpdateImage(opts UpdateImageOptions) (any, error) {
	if opts.Latest && opts.Version != "" {
		return nil, fmt.Errorf("--latest and --version are mutually exclusive")
	}

	targets, sourceIndex, conf, err := discoverAndFilter(opts.imageFilterOptions)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no matching image or chart targets found")
	}

	resolver := imageupdate.NewResolver()
	ctx := context.Background()

	// Resolve chart sources up front (cheap, no network) so the concurrent
	// version lookups have a repo URL to query.
	for i := range targets {
		if targets[i].Kind == imageupdate.TargetChart {
			if err := sourceIndex.Resolve(&targets[i]); err != nil {
				return nil, err
			}
		}
	}

	var plans []UpdatePlan
	if opts.Latest || opts.Version != "" {
		plans = resolveConcurrently(ctx, resolver, conf, targets, opts)
	} else {
		// Interactive picker must run serially (it prompts the user per target).
		for _, t := range targets {
			plans = append(plans, planTarget(ctx, resolver, conf, t, opts, nil))
		}
	}
	return api.NewTableFrom(plans), nil
}

// resolveConcurrently runs each target's version lookup as its own clicky task,
// then applies the resulting edits serially (concurrent writes to the same file
// would race; resolution is the slow, parallelisable part).
func resolveConcurrently(ctx context.Context, resolver *imageupdate.Resolver, conf *repomap.ArchConf, targets []imageupdate.UpdateTarget, opts UpdateImageOptions) []UpdatePlan {
	type resolved struct {
		newValue string
		skipped  string
		err      error
	}
	results := make([]resolved, len(targets))
	group := task.StartGroup[int]("Resolving image versions", task.WithConcurrency(resolveConcurrency))
	for i, t := range targets {
		idx, target := i, t
		group.Add(taskName(target), func(ctx flanksourceContext.Context, tk *task.Task) (int, error) {
			newValue, skipped, err := resolveNewValue(ctx, resolver, target, opts, tk)
			results[idx] = resolved{newValue, skipped, err}
			return idx, nil
		})
	}
	_, _ = group.GetResults()

	plans := make([]UpdatePlan, len(targets))
	for i, t := range targets {
		plans[i] = applyResolved(conf, t, results[i].newValue, results[i].skipped, results[i].err, opts)
	}
	return plans
}

// planTarget resolves the new version for a target and applies the edit. tk may
// be nil when not running inside a task.
func planTarget(ctx context.Context, resolver *imageupdate.Resolver, conf *repomap.ArchConf, t imageupdate.UpdateTarget, opts UpdateImageOptions, tk *task.Task) UpdatePlan {
	newValue, skipped, err := resolveNewValue(ctx, resolver, t, opts, tk)
	return applyResolved(conf, t, newValue, skipped, err, opts)
}

// resolveNewValue determines the replacement value for a target without writing.
// It returns a skip reason instead of a value when no update applies.
func resolveNewValue(ctx context.Context, resolver *imageupdate.Resolver, t imageupdate.UpdateTarget, opts UpdateImageOptions, tk *task.Task) (newValue, skipped string, err error) {
	logf(tk, "resolving version")
	newVersion, err := chooseVersion(ctx, resolver, t, opts)
	if err != nil {
		return "", "", err
	}
	if newVersion == "" {
		return "", "no version selected", nil
	}
	newValue = newVersion
	if t.Kind == imageupdate.TargetImage {
		newValue, err = resolver.NewImageValue(ctx, t, newVersion)
		if err != nil {
			return "", "", err
		}
	}
	if newValue == t.CurrentValue {
		return "", "already up to date", nil
	}
	return newValue, "", nil
}

// applyResolved builds the plan and applies the edit (unless dry-run, skipped,
// or errored).
func applyResolved(conf *repomap.ArchConf, t imageupdate.UpdateTarget, newValue, skipped string, resErr error, opts UpdateImageOptions) UpdatePlan {
	plan := UpdatePlan{
		Ref:      t.Ref,
		Kind:     t.Kind,
		File:     t.File,
		Field:    t.FieldJSONPath,
		OldValue: t.CurrentValue,
		DryRun:   opts.DryRun,
	}
	if resErr != nil {
		plan.Skipped = resErr.Error()
		return plan
	}
	if skipped != "" {
		plan.Skipped = skipped
		return plan
	}
	plan.NewValue = newValue

	absFile := filepath.Join(conf.RepoPath(), t.File)
	if _, err := imageupdate.ApplyEdit(absFile, t, newValue, opts.DryRun); err != nil {
		plan.Skipped = err.Error()
		return plan
	}
	plan.Written = !opts.DryRun
	return plan
}

func logf(tk *task.Task, format string, args ...any) {
	if tk != nil {
		tk.Infof(format, args...)
	}
}

// chooseVersion returns the target version per the CLI mode: explicit --version,
// resolved --latest, or an interactive pick from available candidates.
func chooseVersion(ctx context.Context, resolver *imageupdate.Resolver, t imageupdate.UpdateTarget, opts UpdateImageOptions) (string, error) {
	if opts.Version != "" {
		available, err := resolver.Available(ctx, t)
		if err != nil {
			return "", err
		}
		if !contains(available, opts.Version) {
			return "", fmt.Errorf("%s: version %q is not available (have: %s)",
				t.CurrentValue, opts.Version, strings.Join(available, ", "))
		}
		return opts.Version, nil
	}
	if opts.Latest {
		return resolver.ResolveLatest(ctx, t)
	}

	available, err := resolver.Available(ctx, t)
	if err != nil {
		return "", err
	}
	if len(available) == 0 {
		return "", fmt.Errorf("no available versions for %s", t.CurrentValue)
	}
	return pickVersion(t, available), nil
}

func pickVersion(t imageupdate.UpdateTarget, available []string) string {
	title := fmt.Sprintf("Select version for %s/%s (current %s)", t.Ref.Kind, t.Ref.Name, t.CurrentValue)
	choice, ok := clicky.PromptSelect(available, clicky.PromptSelectOptions[string]{
		Title: title,
		Render: func(v string) api.Textable {
			text := clicky.Text(v)
			if v == versionOnly(t.CurrentValue) {
				text = text.Space().Append("(current)", "text-muted")
			}
			return text
		},
	})
	if !ok {
		return ""
	}
	return choice
}

func versionOnly(currentValue string) string {
	if i := strings.LastIndex(currentValue, ":"); i >= 0 {
		v := currentValue[i+1:]
		if at := strings.Index(v, "@"); at >= 0 {
			v = v[:at]
		}
		return v
	}
	return currentValue
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}
