package repomap

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/flanksource/commons/collections"
	"github.com/flanksource/repomap/kubernetes"
	"github.com/google/cel-go/cel"
)

type ScopeRule struct {
	Path       string `json:"path,omitempty" yaml:"path,omitempty"`
	Prefix     string `json:"prefix,omitempty" yaml:"prefix,omitempty"`
	Kind       string `json:"kind,omitempty" yaml:"kind,omitempty"`
	Name       string `json:"name,omitempty" yaml:"name,omitempty"`
	Namespace  string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	APIVersion string `json:"apiVersion,omitempty" yaml:"apiVersion,omitempty"`
	When       string `json:"when,omitempty" yaml:"when,omitempty"`
}

// PathRule is an alias for backwards compatibility
type PathRule = ScopeRule

func (r ScopeRule) IsPathRule() bool {
	return r.Path != ""
}

func (r ScopeRule) IsResourceRule() bool {
	return r.Kind != "" || r.Name != "" || r.Namespace != "" || r.APIVersion != ""
}

func (r ScopeRule) IsCELRule() bool {
	return r.When != ""
}

func (r ScopeRule) MatchPath(path string) bool {
	if r.Path == "" {
		return false
	}
	matched, _ := doublestar.Match(r.Path, path)
	return matched
}

func (r ScopeRule) MatchResource(ref kubernetes.KubernetesRef) bool {
	if !r.IsResourceRule() {
		return false
	}
	if r.Kind != "" {
		if matched, _ := collections.MatchItem(ref.Kind, r.Kind); !matched {
			return false
		}
	}
	if r.Name != "" {
		if matched, _ := collections.MatchItem(ref.Name, r.Name); !matched {
			return false
		}
	}
	if r.Namespace != "" {
		if matched, _ := collections.MatchItem(ref.Namespace, r.Namespace); !matched {
			return false
		}
	}
	if r.APIVersion != "" {
		if matched, _ := collections.MatchItem(ref.APIVersion, r.APIVersion); !matched {
			return false
		}
	}
	return true
}

type PathRules map[string][]ScopeRule

func (pr PathRules) Apply(path string) ([]string, []ScopeMatch) {
	normalized := filepath.ToSlash(path)
	base := filepath.Base(normalized)

	type scopeHit struct {
		value       string
		specificity int
		rule        ScopeRule
	}
	var matches []scopeHit

	for name, rules := range pr {
		for _, rule := range rules {
			if !rule.IsPathRule() {
				continue
			}
			matched := rule.MatchPath(normalized)
			if !matched {
				matched = rule.MatchPath(base)
			}
			if matched {
				specificity := len(rule.Path)
				wildcardCount := strings.Count(rule.Path, "*") + strings.Count(rule.Path, "?")
				specificity -= wildcardCount * 10
				matches = append(matches, scopeHit{value: name, specificity: specificity, rule: rule})
			}
		}
	}

	if len(matches) == 0 {
		return nil, nil
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].specificity > matches[j].specificity
	})

	seen := make(map[string]bool)
	var results []string
	var scopeMatches []ScopeMatch
	for _, m := range matches {
		if !seen[m.value] {
			seen[m.value] = true
			results = append(results, m.value)
			scopeMatches = append(scopeMatches, ScopeMatch{
				Scope: m.value,
				Rule:  fmt.Sprintf("path:%s", m.rule.Path),
				Type:  "path",
			})
		}
	}
	return results, scopeMatches
}

func (pr PathRules) ApplyResource(ref kubernetes.KubernetesRef) ([]string, []ScopeMatch) {
	seen := make(map[string]bool)
	var results []string
	var scopeMatches []ScopeMatch
	for name, rules := range pr {
		for _, rule := range rules {
			if rule.IsResourceRule() && rule.MatchResource(ref) {
				if !seen[name] {
					seen[name] = true
					results = append(results, name)
					scopeMatches = append(scopeMatches, ScopeMatch{
						Scope: name,
						Rule:  fmt.Sprintf("kind:%s", rule.Kind),
						Type:  "resource",
					})
				}
			}
		}
	}
	sort.Strings(results)
	sort.Slice(scopeMatches, func(i, j int) bool { return scopeMatches[i].Scope < scopeMatches[j].Scope })
	return results, scopeMatches
}

type compiledCELScopeRule struct {
	scope   string
	expr    string
	program cel.Program
}

func (pr PathRules) CompileCELRules() ([]compiledCELScopeRule, error) {
	var celRules []compiledCELScopeRule
	for name, rules := range pr {
		for _, rule := range rules {
			if !rule.IsCELRule() {
				continue
			}
			env, err := cel.NewEnv(
				cel.Variable("kubernetes", cel.MapType(cel.StringType, cel.AnyType)),
				cel.Variable("file", cel.MapType(cel.StringType, cel.AnyType)),
			)
			if err != nil {
				return nil, fmt.Errorf("failed to create CEL environment for scope '%s': %w", name, err)
			}
			ast, issues := env.Compile(rule.When)
			if issues != nil && issues.Err() != nil {
				return nil, fmt.Errorf("failed to compile CEL rule '%s' in scope '%s': %w", rule.When, name, issues.Err())
			}
			program, err := env.Program(ast)
			if err != nil {
				return nil, fmt.Errorf("failed to create CEL program for scope '%s': %w", name, err)
			}
			celRules = append(celRules, compiledCELScopeRule{scope: name, expr: rule.When, program: program})
		}
	}
	return celRules, nil
}

func (pr PathRules) ApplyCEL(celRules []compiledCELScopeRule, ctx map[string]any) []string {
	seen := make(map[string]bool)
	var results []string
	for _, rule := range celRules {
		result, _, err := rule.program.Eval(ctx)
		if err != nil {
			continue
		}
		if result.Value() == true && !seen[rule.scope] {
			seen[rule.scope] = true
			results = append(results, rule.scope)
		}
	}
	return results
}

type GitConfig struct {
	Commits              CommitsConfig `json:"commits,omitempty" yaml:"commits,omitempty"`
	VersionFieldPatterns []string      `json:"version_field_patterns,omitempty" yaml:"version_field_patterns,omitempty"`
}

type CommitsConfig struct {
	Enabled           bool     `json:"enabled,omitempty" yaml:"enabled"`
	AllowedTypes      []string `json:"allowed_types,omitempty" yaml:"allowed_types,omitempty"`
	Blocklist         []string `json:"blocklist,omitempty" yaml:"blocklist,omitempty"`
	RequiredTrailers  []string `json:"required_trailers,omitempty" yaml:"required_trailers,omitempty"`
	RequiredReference bool     `json:"required_reference,omitempty" yaml:"required_reference,omitempty"`
	RequiredScope     bool     `json:"required_scope,omitempty" yaml:"required_scope,omitempty"`
}

type BuildConfig struct {
	Enabled  bool              `json:"enabled,omitempty" yaml:"enabled"`
	Tool     string            `json:"tool,omitempty" yaml:"tool,omitempty"`
	Commands map[string]string `json:"commands,omitempty" yaml:"commands,omitempty"`
}

type GolangConfig struct {
	Enabled   bool     `json:"enabled,omitempty" yaml:"enabled"`
	Blocklist []string `json:"blocklist,omitempty" yaml:"blocklist,omitempty"`
}

type ScopesConfig struct {
	AllowedScopes []string  `json:"allowed_scopes,omitempty" yaml:"allowed_scopes,omitempty"`
	Rules         PathRules `json:"rules,omitempty" yaml:"rules,omitempty"`
	celRules      []compiledCELScopeRule
}

func (sc *ScopesConfig) Validate() error {
	if sc == nil {
		return nil
	}

	var allowedScopes map[string]bool
	if len(sc.AllowedScopes) > 0 {
		allowedScopes = make(map[string]bool, len(sc.AllowedScopes))
		for _, scope := range sc.AllowedScopes {
			allowedScopes[scope] = true
		}
	}

	for scopeName, rules := range sc.Rules {
		if allowedScopes != nil && !allowedScopes[scopeName] {
			return fmt.Errorf("invalid scope name '%s': not in allowed_scopes list %v", scopeName, sc.AllowedScopes)
		}
		for _, rule := range rules {
			if rule.Path != "" {
				_, err := filepath.Match(rule.Path, "test")
				if err != nil {
					return fmt.Errorf("invalid glob pattern '%s' in scope '%s': %w", rule.Path, scopeName, err)
				}
			}
		}
	}

	celRules, err := sc.Rules.CompileCELRules()
	if err != nil {
		return err
	}
	sc.celRules = celRules

	return nil
}

func (sc *ScopesConfig) GetScopesByPath(path string) (Scopes, []ScopeMatch) {
	names, matches := sc.Rules.Apply(path)
	scopes := Scopes{}
	for _, scope := range names {
		scopes = append(scopes, ScopeType(scope))
	}
	return scopes, matches
}

func (sc *ScopesConfig) GetScopesByResource(ref kubernetes.KubernetesRef) (Scopes, []ScopeMatch) {
	names, matches := sc.Rules.ApplyResource(ref)
	scopes := Scopes{}
	for _, scope := range names {
		scopes = append(scopes, ScopeType(scope))
	}
	return scopes, matches
}

func (sc *ScopesConfig) GetScopesByCEL(ctx map[string]any) Scopes {
	if len(sc.celRules) == 0 {
		return nil
	}
	scopes := Scopes{}
	for _, scope := range sc.Rules.ApplyCEL(sc.celRules, ctx) {
		scopes = append(scopes, ScopeType(scope))
	}
	return scopes
}

func (sc *ScopesConfig) GetScopesByRefs(refs []kubernetes.KubernetesRef) (Scopes, []ScopeMatch) {
	all := Scopes{}
	var allMatches []ScopeMatch
	seenMatches := make(map[string]bool)
	for _, ref := range refs {
		scopes, matches := sc.GetScopesByResource(ref)
		all = all.Merge(scopes)
		for _, m := range matches {
			key := m.Scope + "|" + m.Rule
			if !seenMatches[key] {
				seenMatches[key] = true
				allMatches = append(allMatches, m)
			}
		}
		if len(sc.celRules) > 0 {
			ctx := map[string]any{
				"kubernetes": map[string]any{
					"kind":        ref.Kind,
					"name":        ref.Name,
					"namespace":   ref.Namespace,
					"api_version": ref.APIVersion,
					"labels":      ref.Labels,
					"annotations": ref.Annotations,
				},
				"file": map[string]any{},
			}
			all = all.Merge(sc.GetScopesByCEL(ctx))
		}
	}
	return all, allMatches
}

type PathPattern struct {
	Pattern string
	Negate  bool
}

func (p PathPattern) Match(path string) bool {
	matched, err := doublestar.Match(p.Pattern, filepath.ToSlash(path))
	if err != nil {
		return false
	}
	if p.Negate {
		return !matched
	}
	return matched
}
