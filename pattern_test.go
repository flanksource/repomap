package repomap

import (
	"testing"

	"github.com/flanksource/repomap/kubernetes"
)

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
		if got := rule.MatchPath(tt.path); got != tt.expected {
			t.Errorf("PathRule{%q}.MatchPath(%q) = %v, want %v", tt.pattern, tt.path, got, tt.expected)
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
		got, _ := rules.Apply(tt.path)
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

	scopes, _ := sc.GetScopesByPath("Makefile")
	if len(scopes) == 0 {
		t.Fatal("expected non-empty scopes for Makefile")
	}
	if scopes[0] != ScopeType("ci") {
		t.Errorf("GetScopesByPath(Makefile)[0] = %q, want 'ci'", scopes[0])
	}
}

func TestScopeRuleMatchResource(t *testing.T) {
	tests := []struct {
		rule     ScopeRule
		ref      kubernetes.KubernetesRef
		expected bool
	}{
		{
			ScopeRule{Kind: "Deployment"},
			kubernetes.KubernetesRef{Kind: "Deployment", Name: "nginx", Namespace: "default"},
			true,
		},
		{
			ScopeRule{Kind: "Deployment"},
			kubernetes.KubernetesRef{Kind: "Service", Name: "nginx", Namespace: "default"},
			false,
		},
		{
			ScopeRule{Kind: "Deployment", Namespace: "prod*"},
			kubernetes.KubernetesRef{Kind: "Deployment", Name: "nginx", Namespace: "production"},
			true,
		},
		{
			ScopeRule{Kind: "Deployment", Namespace: "prod*"},
			kubernetes.KubernetesRef{Kind: "Deployment", Name: "nginx", Namespace: "staging"},
			false,
		},
		{
			ScopeRule{Kind: "*"},
			kubernetes.KubernetesRef{Kind: "Secret", Name: "db-creds"},
			true,
		},
		{
			ScopeRule{APIVersion: "apps/v1"},
			kubernetes.KubernetesRef{Kind: "Deployment", APIVersion: "apps/v1"},
			true,
		},
		{
			ScopeRule{APIVersion: "apps/v1"},
			kubernetes.KubernetesRef{Kind: "Pod", APIVersion: "v1"},
			false,
		},
		{
			ScopeRule{Kind: "Secret", Name: "db-*"},
			kubernetes.KubernetesRef{Kind: "Secret", Name: "db-password"},
			true,
		},
		{
			ScopeRule{Kind: "Secret", Name: "db-*"},
			kubernetes.KubernetesRef{Kind: "Secret", Name: "api-key"},
			false,
		},
	}

	for _, tt := range tests {
		got := tt.rule.MatchResource(tt.ref)
		if got != tt.expected {
			t.Errorf("ScopeRule%+v.MatchResource(%+v) = %v, want %v", tt.rule, tt.ref, got, tt.expected)
		}
	}
}

func TestApplyResource(t *testing.T) {
	rules := PathRules{
		"security": {
			{Kind: "Secret"},
			{Kind: "Role"},
			{Kind: "ClusterRole"},
		},
		"networking": {
			{Kind: "Service"},
			{Kind: "Ingress"},
			{Kind: "NetworkPolicy"},
		},
		"go": {
			{Path: "*.go"},
		},
	}

	tests := []struct {
		ref      kubernetes.KubernetesRef
		expected []string
	}{
		{kubernetes.KubernetesRef{Kind: "Secret", Name: "db"}, []string{"security"}},
		{kubernetes.KubernetesRef{Kind: "Service", Name: "api"}, []string{"networking"}},
		{kubernetes.KubernetesRef{Kind: "Deployment", Name: "app"}, nil},
	}

	for _, tt := range tests {
		got, _ := rules.ApplyResource(tt.ref)
		if len(got) != len(tt.expected) {
			t.Errorf("ApplyResource(%+v) = %v, want %v", tt.ref, got, tt.expected)
			continue
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Errorf("ApplyResource(%+v)[%d] = %q, want %q", tt.ref, i, got[i], tt.expected[i])
			}
		}
	}
}

func TestScopesConfigGetScopesByResource(t *testing.T) {
	sc := &ScopesConfig{
		Rules: PathRules{
			"security": {
				{Kind: "Secret"},
				{Kind: "ServiceAccount"},
			},
			"networking": {
				{Kind: "Service"},
				{Kind: "Ingress"},
			},
		},
	}

	scopes, _ := sc.GetScopesByResource(kubernetes.KubernetesRef{Kind: "Secret", Name: "db-creds"})
	if len(scopes) != 1 || scopes[0] != ScopeType("security") {
		t.Errorf("GetScopesByResource(Secret) = %v, want [security]", scopes)
	}

	scopes, _ = sc.GetScopesByResource(kubernetes.KubernetesRef{Kind: "Deployment", Name: "app"})
	if len(scopes) != 0 {
		t.Errorf("GetScopesByResource(Deployment) = %v, want []", scopes)
	}
}

func TestScopesConfigGetScopesByRefs(t *testing.T) {
	sc := &ScopesConfig{
		Rules: PathRules{
			"security": {
				{Kind: "Secret"},
			},
			"networking": {
				{Kind: "Service"},
			},
		},
	}

	refs := []kubernetes.KubernetesRef{
		{Kind: "Secret", Name: "db-creds"},
		{Kind: "Service", Name: "api"},
		{Kind: "Deployment", Name: "app"},
	}

	scopes, _ := sc.GetScopesByRefs(refs)
	if !scopes.Contains(ScopeType("security")) {
		t.Error("expected scopes to contain 'security'")
	}
	if !scopes.Contains(ScopeType("networking")) {
		t.Error("expected scopes to contain 'networking'")
	}
	if scopes.Contains(ScopeType("deployment")) {
		t.Error("expected scopes to NOT contain 'deployment'")
	}
}

func TestCELScopeRules(t *testing.T) {
	sc := &ScopesConfig{
		Rules: PathRules{
			"security": {
				{When: `kubernetes.kind == "Secret" || kubernetes.kind == "Role"`},
			},
			"monitoring": {
				{When: `kubernetes.labels.app == "prometheus"`},
			},
		},
	}

	if err := sc.Validate(); err != nil {
		t.Fatalf("Validate() error: %v", err)
	}

	refs := []kubernetes.KubernetesRef{
		{Kind: "Secret", Name: "db-creds"},
	}
	scopes, _ := sc.GetScopesByRefs(refs)
	if !scopes.Contains(ScopeType("security")) {
		t.Errorf("expected CEL rule to match Secret, got scopes: %v", scopes)
	}

	refs = []kubernetes.KubernetesRef{
		{Kind: "Deployment", Name: "prometheus", Labels: map[string]string{"app": "prometheus"}},
	}
	scopes, _ = sc.GetScopesByRefs(refs)
	if !scopes.Contains(ScopeType("monitoring")) {
		t.Errorf("expected CEL rule to match labels, got scopes: %v", scopes)
	}

	refs = []kubernetes.KubernetesRef{
		{Kind: "ConfigMap", Name: "config"},
	}
	scopes, _ = sc.GetScopesByRefs(refs)
	if len(scopes) != 0 {
		t.Errorf("expected no CEL scope match for ConfigMap, got: %v", scopes)
	}
}

func TestCELScopeRulesValidationError(t *testing.T) {
	sc := &ScopesConfig{
		Rules: PathRules{
			"bad": {
				{When: "invalid syntax !!!"},
			},
		},
	}

	if err := sc.Validate(); err == nil {
		t.Error("expected validation error for invalid CEL expression")
	}
}

func TestMixedPathAndResourceRules(t *testing.T) {
	sc := &ScopesConfig{
		Rules: PathRules{
			"security": {
				{Path: "**/*rbac*"},
				{Kind: "Secret"},
				{Kind: "Role"},
			},
			"kubernetes": {
				{Path: "*.yaml"},
			},
		},
	}

	pathScopes, _ := sc.GetScopesByPath("rbac-config.yaml")
	if !pathScopes.Contains(ScopeType("security")) {
		t.Error("expected path rule to match rbac file")
	}
	if !pathScopes.Contains(ScopeType("kubernetes")) {
		t.Error("expected path rule to match yaml file")
	}

	refScopes, _ := sc.GetScopesByResource(kubernetes.KubernetesRef{Kind: "Secret", Name: "key"})
	if !refScopes.Contains(ScopeType("security")) {
		t.Error("expected resource rule to match Secret")
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
