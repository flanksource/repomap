package deps

import (
	"fmt"
	"sort"
	"strings"

	"github.com/flanksource/commons/collections"
)

func updateManagers(managers []Manager) ([]Manager, error) {
	if len(managers) == 0 {
		return []Manager{ManagerGo, ManagerNPM, ManagerPNPM, ManagerImage, ManagerHelm}, nil
	}
	out := make([]Manager, 0, len(managers))
	var unsupported []string
	for _, manager := range managers {
		if !supportedUpdateManagers[manager] {
			unsupported = append(unsupported, string(manager))
			continue
		}
		out = append(out, manager)
	}
	if len(unsupported) > 0 {
		sort.Strings(unsupported)
		return nil, fmt.Errorf("dependency updates currently support go, npm, pnpm, image, and helm; unsupported manager(s): %s", strings.Join(unsupported, ", "))
	}
	return out, nil
}

func packageUpdateManagers(managers []Manager) []Manager {
	var out []Manager
	for _, manager := range managers {
		switch manager {
		case ManagerGo, ManagerNPM, ManagerPNPM:
			out = append(out, manager)
		}
	}
	return out
}

func imageUpdateManagers(managers []Manager) []Manager {
	var out []Manager
	for _, manager := range managers {
		switch manager {
		case ManagerImage, ManagerHelm:
			out = append(out, manager)
		}
	}
	return out
}

func splitUpdatePatterns(values []string) []string {
	var out []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func (c UpdateCandidate) matches(patterns []string) bool {
	identityPatterns, pathPatterns := splitExplicitPathPatterns(patterns)
	hasPositivePattern := identityPatterns.hasPositive || pathPatterns.hasPositive
	matchedPositive := false

	for _, value := range c.matchValues() {
		if value == "" {
			continue
		}
		ok, negated := collections.MatchItem(value, identityPatterns.values...)
		if negated {
			return false
		}
		if ok && identityPatterns.hasPositive {
			matchedPositive = true
		}
	}
	if len(pathPatterns.values) > 0 {
		ok, negated := collections.MatchItem(c.File, pathPatterns.values...)
		if negated {
			return false
		}
		if ok && pathPatterns.hasPositive {
			matchedPositive = true
		}
	}
	if hasPositivePattern {
		return matchedPositive
	}
	return true
}

func (c UpdateCandidate) matchValues() []string {
	return []string{
		c.Name,
		string(c.Manager),
		fmt.Sprintf("%s:%s", c.Manager, c.Name),
		fmt.Sprintf("%s:%s@%s", c.Manager, c.Name, c.Current),
		c.Scope,
		c.Current,
	}
}

type updatePatternSet struct {
	values      []string
	hasPositive bool
}

func splitExplicitPathPatterns(patterns []string) (identity updatePatternSet, path updatePatternSet) {
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		negated := strings.HasPrefix(pattern, "!")
		body := strings.TrimPrefix(pattern, "!")
		field, value, explicit := strings.Cut(body, ":")
		if explicit && (field == "path" || field == "file") {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if strings.HasPrefix(value, "!") {
				negated = true
				value = strings.TrimPrefix(value, "!")
			}
			if negated {
				value = "!" + value
			} else {
				path.hasPositive = true
			}
			path.values = append(path.values, value)
			continue
		}
		if !negated {
			identity.hasPositive = true
		}
		identity.values = append(identity.values, pattern)
	}
	return identity, path
}

func (c UpdateCandidate) key() string {
	return strings.Join([]string{string(c.Manager), c.Dir, c.File, c.Scope, c.Name}, "\x00")
}

func (c UpdateCandidate) less(other UpdateCandidate) bool {
	if c.Manager != other.Manager {
		return c.Manager < other.Manager
	}
	if c.File != other.File {
		return c.File < other.File
	}
	if c.Scope != other.Scope {
		return c.Scope < other.Scope
	}
	return c.Name < other.Name
}

func updateTaskName(candidate UpdateCandidate) string {
	return fmt.Sprintf("%s %s", candidate.Manager, candidate.Name)
}

func isLocalUpdateSpec(spec string) bool {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return true
	}
	if strings.HasPrefix(spec, "workspace:") {
		return true
	}
	return isLocalRef(spec)
}
