package repomap

import (
	"testing"

	"github.com/flanksource/repomap/kubernetes"
)

func deployment() kubernetes.KubernetesRef {
	return kubernetes.KubernetesRef{
		Kind:      "Deployment",
		Name:      "nginx",
		Namespace: "default",
		Labels:    map[string]string{"app": "nginx", "tier": "frontend"},
	}
}

func TestResourceMatcherNamespace(t *testing.T) {
	tests := []struct {
		name      string
		namespace []string
		want      bool
	}{
		{"exact match", []string{"default"}, true},
		{"no match", []string{"kube-system"}, false},
		{"glob match", []string{"def*"}, true},
		{"comma alternatives", []string{"kube-system,default"}, true},
		{"negation excludes", []string{"!default"}, false},
		{"negation keeps others", []string{"!kube-system"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewResourceMatcher(nil, tt.namespace, nil, nil)
			if got := m.MatchesRef(deployment()); got != tt.want {
				t.Errorf("namespace=%v: got %v, want %v", tt.namespace, got, tt.want)
			}
		})
	}
}

func TestResourceMatcherKind(t *testing.T) {
	tests := []struct {
		name string
		kind []string
		want bool
	}{
		{"exact match", []string{"Deployment"}, true},
		{"case insensitive", []string{"deployment"}, true},
		{"no match", []string{"Service"}, false},
		{"glob match", []string{"Deploy*"}, true},
		{"comma alternatives", []string{"Service,Deployment"}, true},
		{"negation excludes", []string{"!Deployment"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewResourceMatcher(tt.kind, nil, nil, nil)
			if got := m.MatchesRef(deployment()); got != tt.want {
				t.Errorf("kind=%v: got %v, want %v", tt.kind, got, tt.want)
			}
		})
	}
}

func TestResourceMatcherName(t *testing.T) {
	m := NewResourceMatcher(nil, nil, []string{"ngin*"}, nil)
	if !m.MatchesRef(deployment()) {
		t.Error("expected name ngin* to match nginx")
	}

	m = NewResourceMatcher(nil, nil, []string{"redis"}, nil)
	if m.MatchesRef(deployment()) {
		t.Error("expected name redis to not match nginx")
	}
}

func TestResourceMatcherSelector(t *testing.T) {
	tests := []struct {
		name     string
		selector []string
		want     bool
	}{
		{"key=value match", []string{"app=nginx"}, true},
		{"key=value mismatch", []string{"app=redis"}, false},
		{"key presence only", []string{"tier"}, true},
		{"missing key", []string{"region"}, false},
		{"glob value", []string{"app=ngin*"}, true},
		{"two selectors ANDed", []string{"app=nginx,tier=frontend"}, true},
		{"two selectors one fails", []string{"app=nginx,tier=backend"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewResourceMatcher(nil, nil, nil, tt.selector)
			if got := m.MatchesRef(deployment()); got != tt.want {
				t.Errorf("selector=%v: got %v, want %v", tt.selector, got, tt.want)
			}
		})
	}
}

func TestResourceMatcherCombined(t *testing.T) {
	m := NewResourceMatcher([]string{"Deployment"}, []string{"default"}, []string{"nginx"}, []string{"tier=frontend"})
	if !m.MatchesRef(deployment()) {
		t.Error("expected all filters to match")
	}

	m = NewResourceMatcher([]string{"Service"}, []string{"default"}, []string{"nginx"}, []string{"tier=frontend"})
	if m.MatchesRef(deployment()) {
		t.Error("expected mismatched kind to fail combined match")
	}

	m = NewResourceMatcher([]string{"Deployment"}, []string{"default"}, []string{"nginx"}, []string{"tier=backend"})
	if m.MatchesRef(deployment()) {
		t.Error("expected mismatched selector to fail combined match")
	}
}

func TestResourceMatcherEmpty(t *testing.T) {
	m := NewResourceMatcher(nil, nil, nil, nil)
	if !m.IsEmpty() {
		t.Error("expected empty matcher")
	}
	if !m.MatchesFile(FileMap{}) {
		t.Error("empty matcher should match any file")
	}
}

func TestResourceMatcherMatchesFile(t *testing.T) {
	file := FileMap{KubernetesRefs: []kubernetes.KubernetesRef{
		{Kind: "Service", Name: "redis", Namespace: "cache"},
		deployment(),
	}}

	m := NewResourceMatcher(nil, []string{"default"}, nil, nil)
	if !m.MatchesFile(file) {
		t.Error("expected file to match via one of its refs")
	}

	m = NewResourceMatcher(nil, []string{"missing"}, nil, nil)
	if m.MatchesFile(file) {
		t.Error("expected file with no matching ref to be excluded")
	}

	m = NewResourceMatcher(nil, []string{"default"}, nil, nil)
	if m.MatchesFile(FileMap{}) {
		t.Error("expected file with no refs to be excluded by active filter")
	}
}

func TestResourceMatcherFilterRefs(t *testing.T) {
	refs := []kubernetes.KubernetesRef{
		{Kind: "Service", Name: "redis", Namespace: "cache"},
		deployment(),
	}

	m := NewResourceMatcher(nil, []string{"default"}, nil, nil)
	got := m.FilterRefs(refs)
	if len(got) != 1 || got[0].Name != "nginx" {
		t.Errorf("expected only the default-namespace ref, got %v", got)
	}

	empty := NewResourceMatcher(nil, nil, nil, nil)
	if len(empty.FilterRefs(refs)) != 2 {
		t.Error("expected empty matcher to return all refs unchanged")
	}
}
