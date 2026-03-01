package repomap

import "testing"

func TestLoadDefaultArchConf(t *testing.T) {
	conf, err := LoadDefaultArchConf()
	if err != nil {
		t.Fatalf("LoadDefaultArchConf() error: %v", err)
	}

	if len(conf.Scopes.Rules) == 0 {
		t.Error("expected default scope rules to be loaded")
	}
	if _, ok := conf.Scopes.Rules["ci"]; !ok {
		t.Error("expected 'ci' scope in defaults")
	}
	if _, ok := conf.Scopes.Rules["kubernetes"]; !ok {
		t.Error("expected 'kubernetes' scope in defaults")
	}
}

func TestArchConfMerge(t *testing.T) {
	defaults, err := LoadDefaultArchConf()
	if err != nil {
		t.Fatalf("LoadDefaultArchConf() error: %v", err)
	}

	user := &ArchConf{
		Scopes: ScopesConfig{
			Rules: PathRules{
				"custom_scope": {{Path: "*.custom"}},
				"ci":           {{Path: "custom-ci.sh"}},
			},
		},
	}

	merged := defaults.Merge(user)

	if _, ok := merged.Scopes.Rules["custom_scope"]; !ok {
		t.Error("expected user custom_scope to be in merged config")
	}

	ciRules := merged.Scopes.Rules["ci"]
	if len(ciRules) <= 1 {
		t.Error("expected ci rules to have both user and default rules merged")
	}

	found := false
	for _, r := range ciRules {
		if r.Path == "custom-ci.sh" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected user ci rule 'custom-ci.sh' to be present in merged config")
	}
}

func TestArchConfMergeNilUser(t *testing.T) {
	defaults, err := LoadDefaultArchConf()
	if err != nil {
		t.Fatalf("LoadDefaultArchConf() error: %v", err)
	}

	merged := defaults.Merge(nil)
	if len(merged.Scopes.Rules) != len(defaults.Scopes.Rules) {
		t.Errorf("merging with nil should return defaults, got %d rules vs %d",
			len(merged.Scopes.Rules), len(defaults.Scopes.Rules))
	}
}

func TestSeverityConfigMerge(t *testing.T) {
	base := DefaultSeverityConfig()
	override := &SeverityConfig{
		Rules: map[string]Severity{
			`change.type == "deleted"`: High, // override from critical to high
		},
	}

	merged := base.Merge(override)
	if merged.Rules[`change.type == "deleted"`] != High {
		t.Errorf("expected override to take precedence, got %q", merged.Rules[`change.type == "deleted"`])
	}
	// Original rules should be preserved
	if _, ok := merged.Rules[`kubernetes.kind == "Secret"`]; !ok {
		t.Error("expected base rules to be preserved in merge")
	}
}

func TestFindGitRoot(t *testing.T) {
	root := FindGitRoot(".")
	if root == "" {
		t.Skip("not inside a git repository")
	}
}

func TestIsGitRoot(t *testing.T) {
	root := FindGitRoot(".")
	if root == "" {
		t.Skip("not inside a git repository")
	}
	if !IsGitRoot(root) {
		t.Errorf("IsGitRoot(%q) = false, want true", root)
	}
}
