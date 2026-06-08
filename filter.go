package repomap

import (
	"strings"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/repomap/kubernetes"
)

// ResourceMatcher filters Kubernetes references using flanksource/commons
// MatchItem syntax (case-insensitive globs, `*` wildcards, `!` negation).
// Comma-separated values in a single flag are treated as alternative patterns.
type ResourceMatcher struct {
	Kind      []string
	Namespace []string
	Name      []string
	Selector  []LabelSelector
}

// LabelSelector matches a single label key against MatchItem value patterns.
// An empty Value list requires only that the key be present.
type LabelSelector struct {
	Key   string
	Value []string
}

// NewResourceMatcher builds a matcher from raw flag values. Each argument may
// contain comma-separated patterns (e.g. "default,kube-system"). Selectors use
// key=value form; multiple selectors are ANDed together.
func NewResourceMatcher(kind, namespace, name, selector []string) ResourceMatcher {
	return ResourceMatcher{
		Kind:      splitPatterns(kind),
		Namespace: splitPatterns(namespace),
		Name:      splitPatterns(name),
		Selector:  parseSelectors(selector),
	}
}

// IsEmpty reports whether the matcher has no active filters.
func (m ResourceMatcher) IsEmpty() bool {
	return len(m.Kind) == 0 && len(m.Namespace) == 0 && len(m.Name) == 0 && len(m.Selector) == 0
}

// MatchesRef reports whether a single Kubernetes reference satisfies every
// configured filter.
func (m ResourceMatcher) MatchesRef(ref kubernetes.KubernetesRef) bool {
	if len(m.Kind) > 0 {
		if matched, _ := collections.MatchItem(ref.Kind, m.Kind...); !matched {
			return false
		}
	}
	if len(m.Namespace) > 0 {
		if matched, _ := collections.MatchItem(ref.Namespace, m.Namespace...); !matched {
			return false
		}
	}
	if len(m.Name) > 0 {
		if matched, _ := collections.MatchItem(ref.Name, m.Name...); !matched {
			return false
		}
	}
	for _, sel := range m.Selector {
		if !sel.matches(ref.Labels) {
			return false
		}
	}
	return true
}

// MatchesFile reports whether a file has at least one Kubernetes reference that
// satisfies the matcher. Files with no references never match an active filter.
func (m ResourceMatcher) MatchesFile(f FileMap) bool {
	if m.IsEmpty() {
		return true
	}
	for _, ref := range f.KubernetesRefs {
		if m.MatchesRef(ref) {
			return true
		}
	}
	return false
}

// FilterRefs returns only the references that satisfy the matcher. With no
// active filters every reference is returned unchanged.
func (m ResourceMatcher) FilterRefs(refs []kubernetes.KubernetesRef) []kubernetes.KubernetesRef {
	if m.IsEmpty() {
		return refs
	}
	var out []kubernetes.KubernetesRef
	for _, ref := range refs {
		if m.MatchesRef(ref) {
			out = append(out, ref)
		}
	}
	return out
}

func (s LabelSelector) matches(labels map[string]string) bool {
	val, ok := labels[s.Key]
	if !ok {
		return false
	}
	if len(s.Value) == 0 {
		return true
	}
	matched, _ := collections.MatchItem(val, s.Value...)
	return matched
}

func splitPatterns(values []string) []string {
	var out []string
	for _, v := range values {
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

func parseSelectors(values []string) []LabelSelector {
	var out []LabelSelector
	for _, raw := range splitPatterns(values) {
		key, value, hasValue := strings.Cut(raw, "=")
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		sel := LabelSelector{Key: key}
		if hasValue {
			if v := strings.TrimSpace(value); v != "" {
				sel.Value = []string{v}
			} else {
				sel.Value = []string{""}
			}
		}
		out = append(out, sel)
	}
	return out
}
