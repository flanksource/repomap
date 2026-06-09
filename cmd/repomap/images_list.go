package main

import (
	"context"
	"fmt"

	"github.com/Masterminds/semver/v3"
	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/clicky/task"
	flanksourceContext "github.com/flanksource/commons/context"

	"github.com/flanksource/repomap/imageupdate"
	"github.com/flanksource/repomap/kubernetes"
)

type ListImagesOptions struct {
	imageFilterOptions
	Check bool `json:"check" flag:"check" help:"Resolve the latest stable and pre-release versions from the registry/Helm repo"`
}

func (opts ListImagesOptions) GetName() string { return "list" }

func (opts ListImagesOptions) Help() api.Text {
	return clicky.Text(`List container images and Helm chart versions in tracked manifests.

Discovers apps/v1 workload images and Flux HelmRelease chart versions in
git-tracked YAML and lists each with its current version, file, and resource.
Offline by default. With --check, the latest stable and pre-release versions are
resolved from the container registry / Helm repository using semantic versioning.

EXAMPLES:
  repomap images list                  # all images and charts (offline)
  repomap images list -k Deployment    # only Deployment images
  repomap images list -k HelmRelease --check  # show latest stable and pre-release chart versions`)
}

func init() {
	cmd := clicky.AddCommand(imagesCmd, ListImagesOptions{}, runListImages)
	cmd.Short = "List image tags and Helm chart versions in tracked manifests"
}

// ImageInfo is one discovered image/chart row. When Checked, Latest,
// LatestPrerelease, and their update flags are populated from the registry/Helm repo.
type ImageInfo struct {
	Ref                       kubernetes.KubernetesRef `json:"ref"`
	Kind                      imageupdate.TargetKind   `json:"kind"`
	File                      string                   `json:"file"`
	Current                   string                   `json:"current"`
	Latest                    string                   `json:"latest,omitempty"`
	LatestPrerelease          string                   `json:"latest_prerelease,omitempty"`
	UpdateAvailable           bool                     `json:"update_available"`
	PrereleaseUpdateAvailable bool                     `json:"prerelease_update_available"`
	Checked                   bool                     `json:"-"`
	Error                     string                   `json:"error,omitempty"`
}

func (i ImageInfo) Pretty() api.Text {
	t := i.Ref.Pretty().Space().Append(i.Current, "font-mono")
	if i.Checked && i.Latest != "" && i.UpdateAvailable {
		t = t.Space().Add(kubernetes.VersionChange{OldVersion: i.Current, NewVersion: i.Latest}.Pretty())
	}
	return t
}

func (i ImageInfo) Columns() []api.ColumnDef {
	cols := []api.ColumnDef{
		api.Column("resource").Label("Resource").Build(),
		api.Column("kind").Label("Type").Build(),
		api.Column("file").Label("File").Build(),
		api.Column("current").Label("Current").Build(),
	}
	if i.Checked {
		cols = append(cols,
			api.Column("latest").Label("Latest Stable").Build(),
			api.Column("stable_update").Label("Stable Update").Build(),
			api.Column("latest_prerelease").Label("Latest Pre-release").Build(),
			api.Column("prerelease_update").Label("Pre-release Update").Build(),
		)
	}
	return cols
}

func (i ImageInfo) Row() map[string]any {
	row := map[string]any{
		"resource": i.Ref.Pretty(),
		"kind":     string(i.Kind),
		"file":     clicky.Text(i.File, "font-mono"),
		"current":  clicky.Text(i.Current, "font-mono"),
	}
	if i.Checked {
		if i.Error != "" {
			row["latest"] = clicky.Text(i.Error, "text-red-600")
			row["stable_update"] = clicky.Text("?", "text-muted")
			row["latest_prerelease"] = clicky.Text(i.Error, "text-red-600")
			row["prerelease_update"] = clicky.Text("?", "text-muted")
		} else {
			row["latest"] = versionCell(i.Latest, i.UpdateAvailable)
			row["stable_update"] = updateCell(i.UpdateAvailable)
			row["latest_prerelease"] = versionCell(i.LatestPrerelease, i.PrereleaseUpdateAvailable)
			row["prerelease_update"] = updateCell(i.PrereleaseUpdateAvailable)
		}
	}
	return row
}

func runListImages(opts ListImagesOptions) (any, error) {
	targets, sourceIndex, conf, err := discoverAndFilter(opts.imageFilterOptions)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no matching image or chart targets found")
	}

	var resolver *imageupdate.Resolver
	if opts.Check {
		resolver = imageupdate.NewResolver()
	}
	infos, err := buildImageInfos(context.Background(), resolver, targets, sourceIndex, opts.Check, displayPathFuncForConf(conf))
	if err != nil {
		return nil, err
	}
	return api.NewTableFrom(infos), nil
}

// buildImageInfos turns targets into list rows. Without --check it is a pure
// offline mapping. With --check each target's version lookup runs as its own
// clicky task (concurrently, with live progress); a per-target resolution error
// is recorded on the row rather than aborting the whole listing.
func buildImageInfos(ctx context.Context, resolver *imageupdate.Resolver, targets []imageupdate.UpdateTarget, sourceIndex *imageupdate.SourceIndex, check bool, displayPath displayPathFunc) ([]ImageInfo, error) {
	if displayPath == nil {
		displayPath = func(path string) string { return path }
	}
	if !check {
		infos := make([]ImageInfo, len(targets))
		for i, t := range targets {
			infos[i] = baseInfo(t, false, displayPath(t.File))
		}
		return infos, nil
	}

	infos := make([]ImageInfo, len(targets))
	group := task.StartGroup[int]("Resolving image versions", task.WithConcurrency(resolveConcurrency))
	for i, t := range targets {
		idx, target := i, t
		displayFile := displayPath(target.File)
		group.Add(taskName(target, displayFile), func(ctx flanksourceContext.Context, tk *task.Task) (int, error) {
			infos[idx] = checkInfo(ctx, resolver, sourceIndex, target, displayFile, tk)
			return idx, nil
		})
	}
	if _, err := group.GetResults(); err != nil {
		return nil, err
	}
	return infos, nil
}

func baseInfo(t imageupdate.UpdateTarget, checked bool, displayFile string) ImageInfo {
	return ImageInfo{Ref: t.Ref, Kind: t.Kind, File: displayFile, Current: t.CurrentValue, Checked: checked}
}

// checkInfo resolves a single target's latest stable and pre-release versions,
// recording any failure on the row. Resolution errors do not fail the task; the
// listing reports them.
func checkInfo(ctx context.Context, resolver *imageupdate.Resolver, sourceIndex *imageupdate.SourceIndex, t imageupdate.UpdateTarget, displayFile string, tk *task.Task) ImageInfo {
	info := baseInfo(t, true, displayFile)
	if t.Kind == imageupdate.TargetChart {
		if err := sourceIndex.Resolve(&t); err != nil {
			tk.Warnf("source unresolved: %v", err)
			info.Error = err.Error()
			return info
		}
	}
	tk.Infof("looking up latest stable and pre-release versions")
	latest, err := resolver.ResolveLatestVersions(ctx, t)
	if err != nil {
		tk.Errorf("%v", err)
		info.Error = err.Error()
		return info
	}
	if latest.Stable == "" && latest.Prerelease == "" {
		info.Error = fmt.Sprintf("no semver-matching version found for %s", versionSourceLabel(t))
		tk.Errorf("%s", info.Error)
		return info
	}
	info.Latest = latest.Stable
	info.LatestPrerelease = latest.Prerelease
	info.UpdateAvailable = semverUpdateAvailable(t.CurrentValue, info.Latest)
	info.PrereleaseUpdateAvailable = semverUpdateAvailable(t.CurrentValue, info.LatestPrerelease)
	return info
}

func versionCell(version string, update bool) api.Text {
	if version == "" {
		return clicky.Text("-", "text-muted")
	}
	if update {
		return clicky.Text(version, "font-mono text-green-600")
	}
	return clicky.Text(version, "font-mono text-muted")
}

func updateCell(update bool) api.Text {
	if update {
		return clicky.Text("✓", "text-green-600")
	}
	return clicky.Text("—", "text-muted")
}

func semverUpdateAvailable(currentValue, latest string) bool {
	if latest == "" {
		return false
	}
	current := versionOnly(currentValue)
	currentSemver, currentErr := semver.NewVersion(current)
	latestSemver, latestErr := semver.NewVersion(latest)
	if currentErr != nil || latestErr != nil {
		return latest != current
	}
	return latestSemver.GreaterThan(currentSemver)
}

func versionSourceLabel(t imageupdate.UpdateTarget) string {
	if t.Kind == imageupdate.TargetChart && !t.IsOCI {
		return fmt.Sprintf("chart %q in %s", t.ChartName, t.RepoURL)
	}
	if t.Kind == imageupdate.TargetImage && t.Image != nil {
		return fmt.Sprintf("image %s", t.Image.GetFullNameWithoutTag())
	}
	return fmt.Sprintf("%s %q", t.Kind, t.CurrentValue)
}

// taskName builds a descriptive, unique label for a target's resolution task:
// the chart/image, the file it lives in, and the namespace when one is known
// (the namespace is often imposed by Flux/kustomize and left empty in the file).
func taskName(t imageupdate.UpdateTarget, displayFile string) string {
	var subject string
	if t.Kind == imageupdate.TargetChart {
		subject = "chart " + t.ChartName
	} else {
		subject = "image " + t.CurrentValue
		if t.ContainerName != "" {
			subject += " [" + t.ContainerName + "]"
		}
	}
	if displayFile == "" {
		displayFile = t.File
	}
	subject += " in " + displayFile
	if ns := targetNamespace(t); ns != "" {
		subject += " (ns " + ns + ")"
	}
	return subject
}

// targetNamespace returns the resource's namespace when determined: the manifest
// value, else (for charts) the resolved sourceRef namespace.
func targetNamespace(t imageupdate.UpdateTarget) string {
	if t.Ref.Namespace != "" {
		return t.Ref.Namespace
	}
	if t.Kind == imageupdate.TargetChart {
		return t.SourceRefNamespace
	}
	return ""
}
