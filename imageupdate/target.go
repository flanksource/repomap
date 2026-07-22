package imageupdate

import (
	"github.com/argoproj-labs/argocd-image-updater/registry-scanner/pkg/image"

	"github.com/flanksource/repomap/kubernetes"
)

// TargetKind distinguishes a container image update from a Helm chart version update.
type TargetKind string

const (
	// TargetImage is an apps/v1 workload container image (registry/name:tag[@digest]).
	TargetImage TargetKind = "image"
	// TargetChart is a Flux HelmRelease chart version (spec.chart.spec.version).
	TargetChart TargetKind = "chart"
)

// UpdateTarget is a single editable version field located in a working-tree
// manifest file. FieldLine is the 1-based absolute line of the value to edit and
// is the anchor the editor uses for surgical, comment-preserving replacement.
type UpdateTarget struct {
	Ref           kubernetes.KubernetesRef `json:"ref"`
	Kind          TargetKind               `json:"kind"`
	File          string                   `json:"file"`
	FieldLine     int                      `json:"field_line"`
	FieldJSONPath string                   `json:"field_path"`
	CurrentValue  string                   `json:"current_value"`

	// Image is set when Kind == TargetImage; parsed from CurrentValue via
	// image.NewFromIdentifier so registry/name/tag/digest are decomposed.
	Image *image.ContainerImage `json:"-"`

	// ContainerName is the name of the container the image belongs to, used to
	// disambiguate multi-container workloads in output and the --image filter.
	ContainerName string `json:"container,omitempty"`

	// Chart fields are set when Kind == TargetChart.
	ChartName          string `json:"chart,omitempty"`
	SourceRefName      string `json:"source_ref,omitempty"`
	SourceRefNamespace string `json:"source_ref_namespace,omitempty"`
	RepoURL            string `json:"repo_url,omitempty"`
	IsOCI              bool   `json:"oci,omitempty"`

	// ChartRef* are set when a HelmRelease selects its chart via spec.chartRef
	// (Flux v2) instead of the inline spec.chart.spec template. They name the
	// referenced OCIRepository/HelmChart source; SourceIndex.Resolve redirects
	// File/FieldLine/CurrentValue/RepoURL onto that object (where the version
	// literal actually lives) so the edit lands in the right place.
	ChartRefKind      string `json:"chart_ref_kind,omitempty"`
	ChartRefName      string `json:"chart_ref_name,omitempty"`
	ChartRefNamespace string `json:"chart_ref_namespace,omitempty"`

	// SourceErr records why a chart target's Flux source could not be resolved
	// (for example, the referenced HelmRepository is absent from the scanned
	// manifests). When set, RepoURL is empty and version resolution must surface
	// this actionable message instead of querying an empty URL.
	SourceErr string `json:"source_error,omitempty"`
}
