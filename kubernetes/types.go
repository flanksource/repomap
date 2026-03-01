package kubernetes

import (
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/clicky/api/icons"
	"github.com/flanksource/repomap/textutil"
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

func (c SourceChangeType) Pretty() api.Text {
	t := clicky.Text("")
	switch c {
	case SourceChangeTypeAdded:
		return t.Append(icons.Add, "text-green-500").Space().Append(string(c), "text-green-500")
	case SourceChangeTypeDeleted:
		return t.Append(icons.Delete, "text-red-500").Space().Append(string(c), "text-red-500")
	case SourceChangeTypeModified:
		return t.Append(icons.Edit, "text-yellow-500").Space().Append(string(c), "text-yellow-500")
	case SourceChangeTypeRenamed:
		return t.Append(icons.Rename, "text-blue-500").Space().Append(string(c), "text-blue-500")
	default:
		return t.Append(string(c))
	}
}

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

func (ref KubernetesRef) Pretty() api.Text {
	t := api.Text{}.Append(icons.Kubernetes).Space()

	t = t.Append(ref.Kind)
	t = t.Append("/", "text-muted").Append(ref.Name, "font-bold")
	if ref.Namespace != "" {
		t = t.Append(" (", "text-muted").Append(ref.Namespace, "text-muted").Append(")", "text-muted")
	}

	return t
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

func (ec EnvironmentChange) Pretty() api.Text {
	t := clicky.Text("env ", "text-muted")
	t = t.Append(clicky.Map(ec.Old)).Space().Add(icons.ArrowDoubleRight).Space().Add(clicky.Map(ec.New))
	return t
}

type Scaling struct {
	OldCPU      string `json:"old_cpu,omitempty"`
	NewCPU      string `json:"new_cpu,omitempty"`
	OldMemory   string `json:"old_memory,omitempty"`
	NewMemory   string `json:"new_memory,omitempty"`
	Replicas    *int   `json:"replicas,omitempty"`
	NewReplicas *int   `json:"new_replicas,omitempty"`
}

func (s Scaling) Pretty() api.Text {
	t := clicky.Text("")
	if s.Replicas != nil && s.NewReplicas != nil {
		t = t.Append("replicas: ", "text-muted").Append(*s.Replicas, "text-blue-500").Append(icons.ArrowDoubleRight, "text-muted").Space().Append(s.NewReplicas, "font-bold text-blue-500")
	}
	if s.OldCPU != "" && s.NewCPU != "" {
		if t.String() != "" {
			t = t.Append(", ")
		}
		t = t.Append("cpu: ", "text-muted").Append(s.OldCPU, "text-blue-500").Append(icons.ArrowDoubleRight, "text-muted").Space().Append(s.NewCPU, "font-bold text-blue-500")
	}
	if s.OldMemory != "" && s.NewMemory != "" {
		if t.String() != "" {
			t = t.Append(", ")
		}
		t = t.Append("memory: ", "text-muted").Append(s.OldMemory, "text-blue-500").Append(icons.ArrowDoubleRight, "text-muted").Space().Append(s.NewMemory, "font-bold text-blue-500")
	}
	return t
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

func (vc VersionChange) Pretty() api.Text {
	t := textutil.HumanDiff(vc.OldVersion, vc.NewVersion)

	switch vc.ChangeType {
	case VersionChangeMajor:
		t = t.Space().Append("major", "text-orange-500")
	case VersionChangeMinor:
		t = t.Space().Append("minor", "text-blue-500")
	case VersionChangePatch:
		t = t.Space().Append("patch", "text-green-500")
	}

	return t
}

func (kc KubernetesChange) Pretty() api.Text {
	t := api.Text{}
	if kc.Severity != "" {
		badge, style := getSeverityBadge(kc.Severity)
		t = t.Append("["+badge+"]", style).Space()
	}

	ref := kc.KubernetesRef.Pretty()
	if kc.ChangeType == SourceChangeTypeDeleted {
		ref = ref.Styles("strikethrough text-red-500")
	} else {
		ref = kc.ChangeType.Pretty().Space().Add(ref)
	}
	t = t.Append(ref)

	if kc.Scaling != nil {
		t = t.Space().Append(kc.Scaling.Pretty())
	}
	if len(kc.VersionChanges) > 0 {
		for _, vc := range kc.VersionChanges {
			t = t.Space().Append(vc.Pretty())
		}
	}
	if kc.EnvironmentChange != nil {
		t = t.Space().Append(kc.EnvironmentChange.Pretty())
	}

	if len(kc.Before) > 0 && len(kc.After) > 0 {
		t = t.Space().Append(kc.ChangeType.Pretty())
		beforeMap := textutil.DiffMap[any](kc.Before)
		afterMap := textutil.DiffMap[any](kc.After)
		t = t.NewLine().Append(beforeMap.Diff(afterMap))
	}
	return t
}

func getSeverityBadge(severity ChangeSeverity) (string, string) {
	switch severity {
	case ChangeSeverityCritical:
		return "CRITICAL", "font-bold text-red-600 bg-red-100"
	case ChangeSeverityHigh:
		return "HIGH", "font-bold text-orange-600 bg-orange-100"
	case ChangeSeverityMedium:
		return "MEDIUM", "text-yellow-600 bg-yellow-100"
	case ChangeSeverityLow:
		return "LOW", "text-blue-600 bg-blue-100"
	case ChangeSeverityInfo:
		return "INFO", "text-gray-600 bg-gray-100"
	default:
		return string(severity), "text-gray-600"
	}
}

type YAMLDocument struct {
	StartLine int
	EndLine   int
	Content   map[string]interface{}
}
