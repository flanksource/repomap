package repomap

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

type PathRule struct {
	Path   string `yaml:"path"`
	Prefix string `yaml:"prefix,omitempty"`
}

func (r PathRule) Match(path string) bool {
	matched, _ := doublestar.Match(r.Path, path)
	return matched
}

type PathRules map[string][]PathRule

func (pr PathRules) Apply(path string) []string {
	normalized := filepath.ToSlash(path)
	base := filepath.Base(normalized)

	type scopeMatch struct {
		value       string
		specificity int
	}
	var matches []scopeMatch

	for name, rules := range pr {
		for _, rule := range rules {
			matched := rule.Match(normalized)
			if !matched {
				matched = rule.Match(base)
			}
			if matched {
				specificity := len(rule.Path)
				wildcardCount := strings.Count(rule.Path, "*") + strings.Count(rule.Path, "?")
				specificity -= wildcardCount * 10
				matches = append(matches, scopeMatch{value: name, specificity: specificity})
			}
		}
	}

	if len(matches) == 0 {
		return nil
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].specificity > matches[j].specificity
	})

	seen := make(map[string]bool)
	var results []string
	for _, m := range matches {
		if !seen[m.value] {
			seen[m.value] = true
			results = append(results, m.value)
		}
	}
	return results
}

type GitConfig struct {
	Commits              CommitsConfig `yaml:"commits,omitempty"`
	VersionFieldPatterns []string      `yaml:"version_field_patterns,omitempty"`
}

type CommitsConfig struct {
	Enabled           bool     `yaml:"enabled"`
	AllowedTypes      []string `yaml:"allowed_types,omitempty"`
	Blocklist         []string `yaml:"blocklist,omitempty"`
	RequiredTrailers  []string `yaml:"required_trailers,omitempty"`
	RequiredReference bool     `yaml:"required_reference,omitempty"`
	RequiredScope     bool     `yaml:"required_scope,omitempty"`
}

type BuildConfig struct {
	Enabled  bool              `yaml:"enabled"`
	Tool     string            `yaml:"tool,omitempty"`
	Commands map[string]string `yaml:"commands,omitempty"`
}

type GolangConfig struct {
	Enabled   bool     `yaml:"enabled"`
	Blocklist []string `yaml:"blocklist,omitempty"`
}

type ScopesConfig struct {
	AllowedScopes []string  `yaml:"allowed_scopes,omitempty"`
	Rules         PathRules `yaml:"rules,omitempty"`
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
			_, err := filepath.Match(rule.Path, "test")
			if err != nil {
				return fmt.Errorf("invalid glob pattern '%s' in scope '%s': %w", rule.Path, scopeName, err)
			}
		}
	}

	return nil
}

func (sc *ScopesConfig) GetScopesByPath(path string) Scopes {
	scopes := Scopes{}
	for _, scope := range sc.Rules.Apply(path) {
		scopes = append(scopes, ScopeType(scope))
	}
	return scopes
}

type TechnologyConfig struct {
	Rules PathRules `yaml:"rules,omitempty"`
}

func (tc TechnologyConfig) GetTechByPath(path string) Technology {
	var techs []ScopeTechnology
	for _, tech := range tc.Rules.Apply(path) {
		techs = append(techs, ScopeTechnology(tech))
	}
	return techs
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
