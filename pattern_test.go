package repomap

import "testing"

func TestPathRuleMatch(t *testing.T) {
	tests := []struct {
		pattern  string
		path     string
		expected bool
	}{
		{"*.go", "main.go", true},
		{"*.go", "main.py", false},
		{"*.yaml", "deploy.yaml", true},
		{"*test*", "main_test.go", true},
		{"Chart.yaml", "Chart.yaml", true},
		{"Chart.yaml", "other.yaml", false},
	}

	for _, tt := range tests {
		rule := PathRule{Path: tt.pattern}
		if got := rule.Match(tt.path); got != tt.expected {
			t.Errorf("PathRule{%q}.Match(%q) = %v, want %v", tt.pattern, tt.path, got, tt.expected)
		}
	}
}

func TestPathRulesApply(t *testing.T) {
	rules := PathRules{
		"test": {
			{Path: "*_test.go"},
			{Path: "*test*"},
		},
		"go": {
			{Path: "*.go"},
		},
		"docs": {
			{Path: "README.md"},
		},
	}

	tests := []struct {
		path     string
		expected []string
	}{
		{"main_test.go", []string{"test", "go"}},
		{"README.md", []string{"docs"}},
		{"unknown.txt", nil},
	}

	for _, tt := range tests {
		got := rules.Apply(tt.path)
		if len(got) != len(tt.expected) {
			t.Errorf("Apply(%q) = %v, want %v", tt.path, got, tt.expected)
			continue
		}
		// Check first result (highest specificity) matches
		if len(got) > 0 && got[0] != tt.expected[0] {
			t.Errorf("Apply(%q)[0] = %q, want %q", tt.path, got[0], tt.expected[0])
		}
	}
}

func TestScopesConfigGetScopesByPath(t *testing.T) {
	sc := &ScopesConfig{
		Rules: PathRules{
			"ci": {
				{Path: "Makefile"},
				{Path: "*.sh"},
			},
			"docs": {
				{Path: "*.md"},
			},
		},
	}

	scopes := sc.GetScopesByPath("Makefile")
	if len(scopes) == 0 {
		t.Fatal("expected non-empty scopes for Makefile")
	}
	if scopes[0] != ScopeType("ci") {
		t.Errorf("GetScopesByPath(Makefile)[0] = %q, want 'ci'", scopes[0])
	}
}

func TestPathPatternMatch(t *testing.T) {
	tests := []struct {
		pattern  string
		negate   bool
		path     string
		expected bool
	}{
		{"*.go", false, "main.go", true},
		{"*.go", true, "main.go", false},
		{"*.go", true, "main.py", true},
	}

	for _, tt := range tests {
		p := PathPattern{Pattern: tt.pattern, Negate: tt.negate}
		if got := p.Match(tt.path); got != tt.expected {
			t.Errorf("PathPattern{%q, negate=%v}.Match(%q) = %v, want %v",
				tt.pattern, tt.negate, tt.path, got, tt.expected)
		}
	}
}
