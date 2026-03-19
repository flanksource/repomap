package repomap

import (
	"fmt"
	"path/filepath"

	"github.com/flanksource/commons/collections"
)

type ExcludeConfig struct {
	Files       []string         `json:"files,omitempty" yaml:"files,omitempty"`
	Authors     []string         `json:"authors,omitempty" yaml:"authors,omitempty"`
	Commits     []string         `json:"commits,omitempty" yaml:"commits,omitempty"`
	CommitTypes []string         `json:"commit_types,omitempty" yaml:"commit_types,omitempty"`
	Resources   []ResourceFilter `json:"resources,omitempty" yaml:"resources,omitempty"`
	Rules       []ExcludeRule    `json:"rules,omitempty" yaml:"rules,omitempty"`
}

type ResourceFilter struct {
	Kind      string `json:"kind,omitempty" yaml:"kind,omitempty"`
	Name      string `json:"name,omitempty" yaml:"name,omitempty"`
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	When      string `json:"when,omitempty" yaml:"when,omitempty"`
}

type ExcludeRule struct {
	When string `json:"when" yaml:"when"`
}

type Preset struct {
	Exclude ExcludeConfig `json:"exclude,omitempty" yaml:"exclude,omitempty"`
}

func (e ExcludeConfig) Merge(other ExcludeConfig) ExcludeConfig {
	return ExcludeConfig{
		Files:       append(sliceCopy(e.Files), other.Files...),
		Authors:     append(sliceCopy(e.Authors), other.Authors...),
		Commits:     append(sliceCopy(e.Commits), other.Commits...),
		CommitTypes: append(sliceCopy(e.CommitTypes), other.CommitTypes...),
		Resources:   append(sliceCopy(e.Resources), other.Resources...),
		Rules:       append(sliceCopy(e.Rules), other.Rules...),
	}
}

func (e ExcludeConfig) IsEmpty() bool {
	return len(e.Files) == 0 && len(e.Authors) == 0 && len(e.Commits) == 0 &&
		len(e.CommitTypes) == 0 && len(e.Resources) == 0 && len(e.Rules) == 0
}

func (e *ExcludeConfig) ResolvePresets(extends []string, presets map[string]Preset) {
	for _, ext := range extends {
		name := ext
		if len(name) > 7 && name[:7] == "preset:" {
			name = name[7:]
		}
		if preset, ok := presets[name]; ok {
			*e = preset.Exclude.Merge(*e)
		}
	}
}

func MatchesAuthor(author Author, patterns []string) (bool, string) {
	for _, pattern := range patterns {
		if author.Matches(pattern) {
			return true, fmt.Sprintf("author matches '%s'", pattern)
		}
	}
	return false, ""
}

func MatchesCommitMessage(subject string, patterns []string) (bool, string) {
	if len(patterns) == 0 {
		return false, ""
	}
	matched, _ := collections.MatchItem(subject, patterns...)
	if matched {
		return true, fmt.Sprintf("commit message matches '%v'", patterns)
	}
	return false, ""
}

func MatchesCommitType(commitType string, patterns []string) (bool, string) {
	if len(patterns) == 0 {
		return false, ""
	}
	matched, _ := collections.MatchItem(commitType, patterns...)
	if matched {
		return true, fmt.Sprintf("commit type '%s' matches '%v'", commitType, patterns)
	}
	return false, ""
}

func MatchesFile(file string, patterns []string) (bool, string) {
	if len(patterns) == 0 {
		return false, ""
	}
	if matched, _ := collections.MatchItem(file, patterns...); matched {
		return true, fmt.Sprintf("file '%s' matches patterns", file)
	}
	if matched, _ := collections.MatchItem(filepath.Base(file), patterns...); matched {
		return true, fmt.Sprintf("file '%s' matches patterns (basename)", file)
	}
	return false, ""
}

func MatchesResourceFields(kind, name, namespace string, f ResourceFilter) bool {
	if f.Kind != "" {
		if matched, _ := collections.MatchItem(kind, f.Kind); !matched {
			return false
		}
	}
	if f.Name != "" {
		if matched, _ := collections.MatchItem(name, f.Name); !matched {
			return false
		}
	}
	if f.Namespace != "" {
		if matched, _ := collections.MatchItem(namespace, f.Namespace); !matched {
			return false
		}
	}
	return true
}

func sliceCopy[T any](s []T) []T {
	if s == nil {
		return nil
	}
	out := make([]T, len(s))
	copy(out, s)
	return out
}
