package kubernetes

import (
	"strings"

	"github.com/Masterminds/semver/v3"
)

type SourceChangeType string

const (
	SourceChangeTypeAdded      SourceChangeType = "added"
	SourceChangeTypeModified   SourceChangeType = "modified"
	SourceChangeTypeDeleted    SourceChangeType = "deleted"
	SourceChangeTypeRenamed    SourceChangeType = "renamed"
	SourceChangeTypeDocumented SourceChangeType = "documented"
	SourceChangeTypeTested     SourceChangeType = "tested"
	SourceChangeTypeRefactored SourceChangeType = "refactored"
	SourceChangeTypeConfigured SourceChangeType = "configured"
	SourceChangeTypeOptimized  SourceChangeType = "optimized"
	SourceChangeTypeFixed      SourceChangeType = "fixed"
	SourceChangeTypeUpgraded   SourceChangeType = "upgraded"
	SourceChangeTypeScaled     SourceChangeType = "scaled"
)

type KubernetesSourceType string

const (
	Kustomize KubernetesSourceType = "kustomize"
	Helm      KubernetesSourceType = "helm"
	YAML      KubernetesSourceType = "yaml"
	Flux      KubernetesSourceType = "flux"
	ArgoCD    KubernetesSourceType = "argocd"
)

type KubernetesRef struct {
	APIVersion  string            `json:"apiVersion,omitempty"`
	Kind        string            `json:"kind,omitempty"`
	Namespace   string            `json:"namespace,omitempty"`
	Name        string            `json:"name,omitempty"`
	JSONPath    string            `json:"path,omitempty"`
	StartLine   int               `json:"start_line,omitempty"`
	EndLine     int               `json:"end_line,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

type ChangeSeverity string

const (
	ChangeSeverityCritical ChangeSeverity = "critical"
	ChangeSeverityHigh     ChangeSeverity = "high"
	ChangeSeverityMedium   ChangeSeverity = "medium"
	ChangeSeverityLow      ChangeSeverity = "low"
	ChangeSeverityInfo     ChangeSeverity = "info"
)

type ExtendedPatch struct {
	Operation string      `json:"op"`
	Path      string      `json:"path"`
	Value     interface{} `json:"value,omitempty"`
	OldValue  interface{} `json:"oldValue,omitempty"`
}

type KubernetesChange struct {
	KubernetesRef `json:",inline"`
	ChangeType    SourceChangeType     `json:"change_type,omitempty"`
	SourceType    KubernetesSourceType `json:"source_type,omitempty"`
	Patches       []ExtendedPatch      `json:"patches,omitempty"`
	// Structured change detection
	Scaling           *Scaling           `json:"scaling,omitempty"`
	VersionChanges    []VersionChange    `json:"version_changes,omitempty"`
	EnvironmentChange *EnvironmentChange `json:"env,omitempty"`
	// Summary and metadata
	Severity         ChangeSeverity `json:"severity,omitempty"`
	Before           map[string]any `json:"before,omitempty"`
	After            map[string]any `json:"after,omitempty"`
	FieldsChanged    []string       `json:"fields_changed,omitempty"`
	FieldChangeCount int            `json:"field_change_count,omitempty"`
}

type EnvironmentChange struct {
	Old map[string]string `json:"old,omitempty"`
	New map[string]string `json:"new,omitempty"`
}

type Scaling struct {
	OldCPU      string `json:"old_cpu,omitempty"`
	NewCPU      string `json:"new_cpu,omitempty"`
	OldMemory   string `json:"old_memory,omitempty"`
	NewMemory   string `json:"new_memory,omitempty"`
	Replicas    *int   `json:"replicas,omitempty"`
	NewReplicas *int   `json:"new_replicas,omitempty"`
}

type VersionChangeType string

const (
	VersionChangeMajor   VersionChangeType = "major"
	VersionChangeMinor   VersionChangeType = "minor"
	VersionChangePatch   VersionChangeType = "patch"
	VersionChangeUnknown VersionChangeType = "unknown"
)

type VersionChange struct {
	OldVersion string            `json:"old_version,omitempty"`
	NewVersion string            `json:"new_version,omitempty"`
	ChangeType VersionChangeType `json:"change_type,omitempty"`
	FieldPath  string            `json:"field_path,omitempty"`
	ValueType  string            `json:"value_type,omitempty"`
	Digest     string            `json:"digest,omitempty"`
}

func AnalyzeVersionChange(oldVer, newVer string) VersionChange {
	vc := VersionChange{
		OldVersion: oldVer,
		NewVersion: newVer,
		ChangeType: VersionChangeUnknown,
	}

	oldSemver, oldErr := semver.NewVersion(strings.TrimPrefix(oldVer, "v"))
	newSemver, newErr := semver.NewVersion(strings.TrimPrefix(newVer, "v"))

	if oldErr != nil || newErr != nil {
		return vc
	}

	if newSemver.GreaterThan(oldSemver) {
		if newSemver.Major() > oldSemver.Major() {
			vc.ChangeType = VersionChangeMajor
		} else if newSemver.Minor() > oldSemver.Minor() {
			vc.ChangeType = VersionChangeMinor
		} else if newSemver.Patch() > oldSemver.Patch() {
			vc.ChangeType = VersionChangePatch
		}
	}

	return vc
}

type YAMLDocument struct {
	StartLine int
	EndLine   int
	Content   map[string]interface{}
}
