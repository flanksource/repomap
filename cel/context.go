package cel

import (
	"path/filepath"
	"strings"

	"github.com/flanksource/repomap"
	"github.com/flanksource/repomap/kubernetes"
)

func BuildContext(
	commit *repomap.CommitAnalysis,
	change *repomap.CommitChange,
	k8sChange *kubernetes.KubernetesChange,
) map[string]any {
	return map[string]any{
		"commit":     buildCommitContext(commit),
		"change":     buildChangeContext(change),
		"kubernetes": buildKubernetesContext(k8sChange),
		"file":       buildFileContext(change),
	}
}

func buildCommitContext(commit *repomap.CommitAnalysis) map[string]any {
	if commit == nil {
		return map[string]any{
			"hash":           "",
			"author":         "",
			"author_email":   "",
			"subject":        "",
			"body":           "",
			"type":           "",
			"scope":          "",
			"file_count":     0,
			"line_changes":   0,
			"resource_count": 0,
		}
	}

	return map[string]any{
		"hash":           commit.Hash,
		"author":         commit.Author.Name,
		"author_email":   commit.Author.Email,
		"subject":        commit.Subject,
		"body":           commit.Body,
		"type":           string(commit.CommitType),
		"scope":          string(commit.Scope),
		"file_count":     len(commit.Changes),
		"line_changes":   commit.TotalLineChanges,
		"resource_count": commit.TotalResourceCount,
	}
}

func buildChangeContext(change *repomap.CommitChange) map[string]any {
	if change == nil {
		return map[string]any{
			"type":           "",
			"file":           "",
			"adds":           0,
			"dels":           0,
			"fields_changed": []string{},
			"field_count":    0,
		}
	}

	fieldsChanged := []string{}
	fieldCount := 0

	if len(change.KubernetesChanges) > 0 {
		fieldSet := make(map[string]bool, len(change.KubernetesChanges))
		for _, kc := range change.KubernetesChanges {
			for _, field := range kc.FieldsChanged {
				fieldSet[field] = true
			}
			fieldCount += kc.FieldChangeCount
		}
		for field := range fieldSet {
			fieldsChanged = append(fieldsChanged, field)
		}
	}

	return map[string]any{
		"type":           string(change.Type),
		"file":           change.File,
		"adds":           change.Adds,
		"dels":           change.Dels,
		"fields_changed": fieldsChanged,
		"field_count":    fieldCount,
	}
}

func buildKubernetesContext(k8sChange *kubernetes.KubernetesChange) map[string]any {
	if k8sChange == nil {
		return map[string]any{
			"is_kubernetes":       false,
			"kind":                "",
			"api_version":         "",
			"namespace":           "",
			"name":                "",
			"version_upgrade":     "",
			"version_downgrade":   "",
			"has_sha_change":      false,
			"replica_delta":       0,
			"has_env_change":      false,
			"has_resource_change": false,
		}
	}

	versionUpgrade := ""
	versionDowngrade := ""
	hasSHAChange := false

	for _, vc := range k8sChange.VersionChanges {
		changeType := strings.ToLower(string(vc.ChangeType))

		if changeType == "sha256" || changeType == "git_sha" || changeType == "combined" {
			hasSHAChange = true
		}

		if strings.Contains(changeType, "upgrade") || changeType == "major" || changeType == "minor" || changeType == "patch" {
			if changeType == "major" || strings.Contains(changeType, "major") {
				versionUpgrade = "major"
			} else if (changeType == "minor" || strings.Contains(changeType, "minor")) && versionUpgrade != "major" {
				versionUpgrade = "minor"
			} else if (changeType == "patch" || strings.Contains(changeType, "patch")) && versionUpgrade == "" {
				versionUpgrade = "patch"
			}
		} else if strings.Contains(changeType, "downgrade") {
			if strings.Contains(changeType, "major") {
				versionDowngrade = "major"
			} else if strings.Contains(changeType, "minor") && versionDowngrade != "major" {
				versionDowngrade = "minor"
			} else if strings.Contains(changeType, "patch") && versionDowngrade == "" {
				versionDowngrade = "patch"
			}
		}
	}

	replicaDelta := 0
	if k8sChange.Scaling != nil && k8sChange.Scaling.Replicas != nil && k8sChange.Scaling.NewReplicas != nil {
		replicaDelta = *k8sChange.Scaling.NewReplicas - *k8sChange.Scaling.Replicas
	}

	hasResourceChange := false
	if k8sChange.Scaling != nil {
		hasResourceChange = (k8sChange.Scaling.OldCPU != "" && k8sChange.Scaling.NewCPU != "") ||
			(k8sChange.Scaling.OldMemory != "" && k8sChange.Scaling.NewMemory != "")
	}

	return map[string]any{
		"is_kubernetes":       true,
		"kind":                k8sChange.Kind,
		"api_version":         k8sChange.APIVersion,
		"namespace":           k8sChange.Namespace,
		"name":                k8sChange.Name,
		"version_upgrade":     versionUpgrade,
		"version_downgrade":   versionDowngrade,
		"has_sha_change":      hasSHAChange,
		"replica_delta":       replicaDelta,
		"has_env_change":      k8sChange.EnvironmentChange != nil,
		"has_resource_change": hasResourceChange,
	}
}

func buildFileContext(change *repomap.CommitChange) map[string]any {
	if change == nil || change.File == "" {
		return map[string]any{
			"extension": "",
			"directory": "",
			"is_test":   false,
			"is_config": false,
			"tech":      "",
		}
	}

	ext := filepath.Ext(change.File)
	dir := filepath.Dir(change.File)
	baseName := filepath.Base(change.File)

	isTest := strings.Contains(baseName, "_test.") ||
		strings.Contains(baseName, ".test.") ||
		strings.Contains(baseName, ".spec.") ||
		strings.HasSuffix(baseName, "_test"+ext) ||
		strings.HasSuffix(baseName, ".spec"+ext)

	isConfig := strings.Contains(baseName, "config") ||
		ext == ".yaml" || ext == ".yml" ||
		ext == ".json" || ext == ".toml" ||
		ext == ".env" || baseName == ".env" ||
		strings.HasPrefix(baseName, ".env.")

	tech := ""
	if len(change.Scope) > 0 {
		tech = string(change.Scope[0])
	} else {
		tech = detectTech(ext)
	}

	return map[string]any{
		"extension": ext,
		"directory": dir,
		"is_test":   isTest,
		"is_config": isConfig,
		"tech":      tech,
	}
}

func detectTech(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".js", ".jsx":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".py":
		return "python"
	case ".java":
		return "java"
	case ".rs":
		return "rust"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".cxx", ".hpp":
		return "cpp"
	case ".cs":
		return "csharp"
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	case ".md":
		return "markdown"
	case ".sh", ".bash":
		return "bash"
	case ".sql":
		return "sql"
	default:
		return ""
	}
}
