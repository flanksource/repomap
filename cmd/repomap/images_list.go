package main

import (
	"context"
	"fmt"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/clicky/task"
	flanksourceContext "github.com/flanksource/commons/context"

	"github.com/flanksource/repomap/imageupdate"
	"github.com/flanksource/repomap/kubernetes"
)

type ListImagesOptions struct {
	imageFilterOptions
	Check bool `json:"check" flag:"check" help:"Resolve the latest available version from the registry/Helm repo"`
}

func (opts ListImagesOptions) GetName() string { return "list" }

func (opts ListImagesOptions) Help() api.Text {
	return clicky.Text(`List container images and Helm chart versions in tracked manifests.

Discovers apps/v1 workload images and Flux HelmRelease chart versions in
git-tracked YAML and lists each with its current version, file, and resource.
Offline by default. With --check, the latest available version is resolved from
the container registry / Helm repository and an update-available column is shown.

EXAMPLES:
  repomap images list                  # all images and charts (offline)
  repomap images list -k Deployment    # only Deployment images
  repomap images list -k HelmRelease --check  # show latest available chart versions`)
}

func init() {
	cmd := clicky.AddCommand(imagesCmd, ListImagesOptions{}, runListImages)
	cmd.Short = "List image tags and Helm chart versions in tracked manifests"
}

// ImageInfo is one discovered image/chart row. When Checked, Latest and
// UpdateAvailable are populated from the registry/Helm repo.
type ImageInfo struct {
	Ref             kubernetes.KubernetesRef `json:"ref"`
	Kind            imageupdate.TargetKind   `json:"kind"`
	File            string                   `json:"file"`
	Current         string                   `json:"current"`
	Latest          string                   `json:"latest,omitempty"`
	UpdateAvailable bool                     `json:"update_available"`
	Checked         bool                     `json:"-"`
	Error           string                   `json:"error,omitempty"`
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
			api.Column("latest").Label("Latest").Build(),
			api.Column("update").Label("Update").Build(),
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
		switch {
		case i.Error != "":
			row["latest"] = clicky.Text(i.Error, "text-red-600")
			row["update"] = clicky.Text("?", "text-muted")
		case i.UpdateAvailable:
			row["latest"] = clicky.Text(i.Latest, "font-mono text-green-600")
			row["update"] = clicky.Text("✓", "text-green-600")
		default:
			row["latest"] = clicky.Text(i.Latest, "font-mono text-muted")
			row["update"] = clicky.Text("—", "text-muted")
		}
	}
	return row
}

func runListImages(opts ListImagesOptions) (any, error) {
	targets, sourceIndex, _, err := discoverAndFilter(opts.imageFilterOptions)
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
	infos, err := buildImageInfos(context.Background(), resolver, targets, sourceIndex, opts.Check)
	if err != nil {
		return nil, err
	}
	return api.NewTableFrom(infos), nil
}

// buildImageInfos turns targets into list rows. Without --check it is a pure
// offline mapping. With --check each target's version lookup runs as its own
// clicky task (concurrently, with live progress); a per-target resolution error
// is recorded on the row rather than aborting the whole listing.
func buildImageInfos(ctx context.Context, resolver *imageupdate.Resolver, targets []imageupdate.UpdateTarget, sourceIndex *imageupdate.SourceIndex, check bool) ([]ImageInfo, error) {
	if !check {
		infos := make([]ImageInfo, len(targets))
		for i, t := range targets {
			infos[i] = baseInfo(t, false)
		}
		return infos, nil
	}

	infos := make([]ImageInfo, len(targets))
	group := task.StartGroup[int]("Resolving image versions", task.WithConcurrency(resolveConcurrency))
	for i, t := range targets {
		idx, target := i, t
		group.Add(taskName(target), func(ctx flanksourceContext.Context, tk *task.Task) (int, error) {
			infos[idx] = checkInfo(ctx, resolver, sourceIndex, target, tk)
			return idx, nil
		})
	}
	if _, err := group.GetResults(); err != nil {
		return nil, err
	}
	return infos, nil
}

func baseInfo(t imageupdate.UpdateTarget, checked bool) ImageInfo {
	return ImageInfo{Ref: t.Ref, Kind: t.Kind, File: t.File, Current: t.CurrentValue, Checked: checked}
}

// checkInfo resolves a single target's latest version, recording any failure on
// the row. Resolution errors do not fail the task — the listing reports them.
func checkInfo(ctx context.Context, resolver *imageupdate.Resolver, sourceIndex *imageupdate.SourceIndex, t imageupdate.UpdateTarget, tk *task.Task) ImageInfo {
	info := baseInfo(t, true)
	if t.Kind == imageupdate.TargetChart {
		if err := sourceIndex.Resolve(&t); err != nil {
			tk.Warnf("source unresolved: %v", err)
			info.Error = err.Error()
			return info
		}
	}
	tk.Infof("looking up latest version")
	latest, err := resolver.ResolveLatest(ctx, t)
	if err != nil {
		tk.Errorf("%v", err)
		info.Error = err.Error()
		return info
	}
	info.Latest = latest
	info.UpdateAvailable = latest != "" && latest != versionOnly(t.CurrentValue)
	return info
}

// taskName builds a descriptive, unique label for a target's resolution task:
// the chart/image, the file it lives in, and the namespace when one is known
// (the namespace is often imposed by Flux/kustomize and left empty in the file).
func taskName(t imageupdate.UpdateTarget) string {
	var subject string
	if t.Kind == imageupdate.TargetChart {
		subject = "chart " + t.ChartName
	} else {
		subject = "image " + t.CurrentValue
		if t.ContainerName != "" {
			subject += " [" + t.ContainerName + "]"
		}
	}
	subject += " in " + t.File
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
