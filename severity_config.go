package repomap

import (
	"fmt"

	"github.com/goccy/go-yaml"
)

type SeverityRule struct {
	ID       string   `json:"id,omitempty" yaml:"id,omitempty"`
	When     string   `json:"when" yaml:"when"`
	Severity Severity `json:"severity" yaml:"severity"`
}

type SeverityConfig struct {
	Default   Severity            `json:"default,omitempty" yaml:"default"`
	Rules     map[string]Severity `json:"rules,omitempty" yaml:"rules"`
	RulesList []SeverityRule      `json:"rules_list,omitempty" yaml:"rules_list,omitempty"`
}

func DefaultSeverityConfig() *SeverityConfig {
	return &SeverityConfig{
		Default: Medium,
		Rules: map[string]Severity{
			`change.type == "deleted"`: Critical,

			`kubernetes.kind == "Secret"`:             Critical,
			`kubernetes.kind == "ServiceAccount"`:     High,
			`kubernetes.kind == "Role"`:               High,
			`kubernetes.kind == "ClusterRole"`:        High,
			`kubernetes.kind == "RoleBinding"`:        High,
			`kubernetes.kind == "ClusterRoleBinding"`: High,

			`kubernetes.kind == "Service"`:       High,
			`kubernetes.kind == "Ingress"`:       High,
			`kubernetes.kind == "NetworkPolicy"`: High,

			`kubernetes.kind == "PersistentVolume"`:      Medium,
			`kubernetes.kind == "PersistentVolumeClaim"`: Medium,

			`kubernetes.version_downgrade != ""`:    High,
			`kubernetes.version_upgrade == "major"`: High,
			`kubernetes.has_sha_change`:             High,

			`commit.line_changes > 500`:  Critical,
			`commit.line_changes > 100`:  High,
			`commit.file_count > 20`:     Critical,
			`commit.file_count > 10`:     High,
			`commit.resource_count > 25`: Critical,
			`commit.resource_count > 15`: High,
			`change.field_count > 20`:    High,

			`kubernetes.replica_delta > 10`: High,
			`kubernetes.replica_delta < -5`: High,

			`kubernetes.has_env_change`:      Medium,
			`kubernetes.has_resource_change`: Medium,

			`file.extension == ".env"`:                    High,
			`file.is_config && change.type == "modified"`: Medium,
		},
	}
}

func LoadSeverityConfig(data []byte) (*SeverityConfig, error) {
	config := &SeverityConfig{
		Default: Medium,
		Rules:   make(map[string]Severity),
	}

	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse severity config: %w", err)
	}

	for expr, severity := range config.Rules {
		if severity.Value() == 0 {
			return nil, fmt.Errorf("invalid severity '%s' for rule: %s", severity, expr)
		}
	}

	if config.Default.Value() == 0 {
		config.Default = Medium
	}

	return config, nil
}

func (c *SeverityConfig) Merge(overrides *SeverityConfig) *SeverityConfig {
	if overrides == nil {
		return c
	}

	merged := &SeverityConfig{
		Default:   c.Default,
		Rules:     make(map[string]Severity),
		RulesList: sliceCopy(c.RulesList),
	}

	for expr, sev := range c.Rules {
		merged.Rules[expr] = sev
	}
	for expr, sev := range overrides.Rules {
		merged.Rules[expr] = sev
	}

	if len(overrides.RulesList) > 0 {
		merged.RulesList = append(merged.RulesList, overrides.RulesList...)
	}

	if overrides.Default.Value() > 0 {
		merged.Default = overrides.Default
	}

	return merged
}

// AllRules returns all rules as a map, combining both map-based and list-based rules.
// List-based rules take precedence over map-based rules with the same expression.
func (c *SeverityConfig) AllRules() map[string]Severity {
	all := make(map[string]Severity, len(c.Rules)+len(c.RulesList))
	for expr, sev := range c.Rules {
		all[expr] = sev
	}
	for _, rule := range c.RulesList {
		all[rule.When] = rule.Severity
	}
	return all
}
