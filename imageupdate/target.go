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
}
