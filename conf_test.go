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

	merged, err := defaults.Merge(user)
	if err != nil {
		t.Fatalf("Merge() error: %v", err)
	}

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

	merged, err := defaults.Merge(nil)
	if err != nil {
		t.Fatalf("Merge() error: %v", err)
	}
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

func TestGetFileMapScopeMatches(t *testing.T) {
	sc := &ScopesConfig{
		Rules: PathRules{
			"security": {
				{Kind: "Secret"},
				{Kind: "Role"},
			},
			"networking": {
				{Kind: "Service"},
				{Kind: "Ingress"},
			},
			"kubernetes": {
				{Kind: "*"},
			},
		},
	}
	if err := sc.Validate(); err != nil {
		t.Fatalf("Validate() error: %v", err)
	}

	tests := []struct {
		name           string
		path           string
		expectedScopes []string
		expectedTypes  []string
	}{
		{
			name:           "security yaml returns resource matches",
			path:           "testdata/k8s/security.yaml",
			expectedScopes: []string{"kubernetes", "security"},
			expectedTypes:  []string{"resource", "resource"},
		},
		{
			name:           "networking yaml returns resource matches",
			path:           "testdata/k8s/networking.yaml",
			expectedScopes: []string{"kubernetes", "networking"},
			expectedTypes:  []string{"resource", "resource"},
		},
	}

	root := FindGitRoot(".")
	if root == "" {
		t.Skip("not inside a git repository")
	}

	conf := &ArchConf{
		Scopes:   *sc,
		repoPath: root,
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, err := conf.GetFileMap(tt.path, "HEAD")
			if err != nil {
				t.Fatalf("GetFileMap() error: %v", err)
			}
			if len(fm.ScopeMatches) == 0 {
				t.Fatal("expected non-empty ScopeMatches")
			}

			scopeSet := make(map[string]bool)
			typeSet := make(map[string]bool)
			for _, m := range fm.ScopeMatches {
				scopeSet[m.Scope] = true
				typeSet[m.Type] = true
			}

			for _, expected := range tt.expectedScopes {
				if !scopeSet[expected] {
					t.Errorf("expected scope %q in matches, got %+v", expected, fm.ScopeMatches)
				}
			}
			for _, expected := range tt.expectedTypes {
				if !typeSet[expected] {
					t.Errorf("expected type %q in matches, got %+v", expected, fm.ScopeMatches)
				}
			}
		})
	}
}

func TestGetFileMapPathScopeMatches(t *testing.T) {
	sc := &ScopesConfig{
		Rules: PathRules{
			"go":   {{Path: "*.go"}},
			"test": {{Path: "*test*"}},
			"ci":   {{Path: "Makefile"}},
		},
	}
	if err := sc.Validate(); err != nil {
		t.Fatalf("Validate() error: %v", err)
	}

	conf := &ArchConf{Scopes: *sc}

	tests := []struct {
		path           string
		expectedScopes []string
	}{
		{"main_test.go", []string{"test", "go"}},
		{"Makefile", []string{"ci"}},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			fm, err := conf.GetFileMap(tt.path, "")
			if err != nil {
				t.Fatalf("GetFileMap() error: %v", err)
			}
			if len(fm.ScopeMatches) != len(tt.expectedScopes) {
				t.Fatalf("expected %d scope matches, got %d: %+v",
					len(tt.expectedScopes), len(fm.ScopeMatches), fm.ScopeMatches)
			}
			for _, expected := range tt.expectedScopes {
				found := false
				for _, m := range fm.ScopeMatches {
					if m.Scope == expected && m.Type == "path" {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected path scope match %q, got %+v", expected, fm.ScopeMatches)
				}
			}
		})
	}
}
