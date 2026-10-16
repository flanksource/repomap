package actions

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Masterminds/semver/v3"
	"gopkg.in/yaml.v3"
)

var repositoryPart = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
var fullSHA = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)
var versionToken = regexp.MustCompile(`\bv[0-9]+(?:\.[0-9]+){0,2}(?:[-+][A-Za-z0-9.-]+)?\b`)

type Reference struct {
	Name           string
	Repository     string
	Ref            string
	Line           int
	Scope          string
	Pinned         bool
	VersionComment string
}

type Skipped struct {
	Value  string
	Line   int
	Scope  string
	Reason string
}

type Result struct {
	References []Reference
	Skipped    []Skipped
}

func Parse(file string, content []byte) (Result, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		return Result{}, fmt.Errorf("parse %s: %w", file, err)
	}
	if len(root.Content) == 0 {
		return Result{}, nil
	}
	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return Result{}, fmt.Errorf("%s: expected a YAML mapping", file)
	}
	var out Result
	base := strings.ToLower(filepath.Base(file))
	if (base == "action.yml" || base == "action.yaml") && !isWorkflowFile(file) {
		runs := mapValue(doc, "runs")
		if runs != nil && mapValue(runs, "using") != nil && mapValue(runs, "using").Value == "composite" {
			if err := collectSteps(&out, mapValue(runs, "steps"), "runs.steps"); err != nil {
				return Result{}, fmt.Errorf("%s: %w", file, err)
			}
		}
		return out, nil
	}
	jobs := mapValue(doc, "jobs")
	if jobs == nil {
		return out, nil
	}
	if jobs.Kind != yaml.MappingNode {
		return Result{}, fmt.Errorf("%s: jobs must be a mapping", file)
	}
	for i := 0; i < len(jobs.Content); i += 2 {
		jobName, job := jobs.Content[i].Value, jobs.Content[i+1]
		if job.Kind != yaml.MappingNode {
			return Result{}, fmt.Errorf("%s: jobs.%s must be a mapping", file, jobName)
		}
		if err := collectUse(&out, mapValue(job, "uses"), "jobs."+jobName+".uses"); err != nil {
			return Result{}, fmt.Errorf("%s: %w", file, err)
		}
		if err := collectSteps(&out, mapValue(job, "steps"), "jobs."+jobName+".steps"); err != nil {
			return Result{}, fmt.Errorf("%s: %w", file, err)
		}
	}
	return out, nil
}

func isWorkflowFile(file string) bool {
	return filepath.Base(filepath.Dir(file)) == "workflows" && filepath.Base(filepath.Dir(filepath.Dir(file))) == ".github"
}

func mapValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func collectSteps(out *Result, steps *yaml.Node, scope string) error {
	if steps == nil {
		return nil
	}
	if steps.Kind != yaml.SequenceNode {
		return fmt.Errorf("%s must be a sequence", scope)
	}
	for i, step := range steps.Content {
		if step.Kind != yaml.MappingNode {
			return fmt.Errorf("%s[%d] must be a mapping", scope, i)
		}
		if err := collectUse(out, mapValue(step, "uses"), fmt.Sprintf("%s[%d].uses", scope, i)); err != nil {
			return err
		}
	}
	return nil
}

func collectUse(out *Result, node *yaml.Node, scope string) error {
	if node == nil {
		return nil
	}
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("%s:%d: uses must be a scalar", scope, node.Line)
	}
	value := strings.TrimSpace(node.Value)
	skip := func(reason string) {
		out.Skipped = append(out.Skipped, Skipped{Value: value, Line: node.Line, Scope: scope, Reason: reason})
	}
	switch {
	case strings.HasPrefix(value, "./"), strings.HasPrefix(value, "$/"):
		skip("local action")
		return nil
	case strings.HasPrefix(value, "docker://"):
		skip("Docker action")
		return nil
	case strings.Contains(value, "${{"):
		skip("dynamic reference")
		return nil
	}
	name, ref, ok := strings.Cut(value, "@")
	if !ok || ref == "" {
		skip("missing ref")
		return nil
	}
	parts := strings.Split(name, "/")
	if len(parts) < 2 || !repositoryPart.MatchString(parts[0]) || !repositoryPart.MatchString(parts[1]) || strings.ContainsAny(ref, " \t\r\n") {
		return fmt.Errorf("%s:%d: invalid external uses reference %q", scope, node.Line, value)
	}
	for _, part := range parts[2:] {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("%s:%d: invalid external uses reference %q", scope, node.Line, value)
		}
	}
	if !fullSHA.MatchString(ref) {
		if _, err := semver.NewVersion(ref); err != nil {
			skip("non-version ref")
			return nil
		}
	}
	reference := Reference{Name: name, Repository: parts[0] + "/" + parts[1], Ref: ref, Line: node.Line, Scope: scope, Pinned: fullSHA.MatchString(ref)}
	versions := versionToken.FindAllString(node.LineComment, -1)
	if reference.Pinned && len(versions) > 1 {
		return fmt.Errorf("%s:%d: multiple version comments for SHA pin", scope, node.Line)
	}
	if len(versions) == 1 {
		reference.VersionComment = versions[0]
	}
	out.References = append(out.References, reference)
	return nil
}

type Edit struct {
	Line       int
	OldValue   string
	NewValue   string
	VersionTag string
}

func Apply(content []byte, edits []Edit) ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		return nil, err
	}
	byLine := map[int]*yaml.Node{}
	var visit func(*yaml.Node)
	visit = func(node *yaml.Node) {
		if node.Kind == yaml.MappingNode {
			for i := 0; i < len(node.Content); i += 2 {
				if node.Content[i].Value == "uses" && node.Content[i+1].Kind == yaml.ScalarNode {
					byLine[node.Content[i+1].Line] = node.Content[i+1]
				}
			}
		}
		for _, child := range node.Content {
			visit(child)
		}
	}
	visit(&root)
	lines := strings.Split(string(content), "\n")
	seen := map[int]bool{}
	for _, edit := range edits {
		if seen[edit.Line] {
			return nil, fmt.Errorf("duplicate action edit on line %d", edit.Line)
		}
		seen[edit.Line] = true
		node := byLine[edit.Line]
		if node == nil || node.Value != edit.OldValue || edit.Line < 1 || edit.Line > len(lines) {
			return nil, fmt.Errorf("action reference changed on line %d", edit.Line)
		}
		line := lines[edit.Line-1]
		if node.Column < 1 || node.Column > len(line) {
			return nil, fmt.Errorf("action reference position invalid on line %d", edit.Line)
		}
		start := node.Column - 1
		if line[start] == '\'' || line[start] == '"' {
			start++
		}
		if !strings.HasPrefix(line[start:], edit.OldValue) {
			return nil, fmt.Errorf("action reference changed on line %d", edit.Line)
		}
		line = line[:start] + edit.NewValue + line[start+len(edit.OldValue):]
		if edit.VersionTag != "" && node.LineComment != "" {
			versions := versionToken.FindAllString(node.LineComment, -1)
			if len(versions) != 1 {
				return nil, fmt.Errorf("action pin version comment changed on line %d", edit.Line)
			}
			comment := strings.Replace(node.LineComment, versions[0], edit.VersionTag, 1)
			line = strings.Replace(line, node.LineComment, comment, 1)
		}
		lines[edit.Line-1] = line
	}
	updated := []byte(strings.Join(lines, "\n"))
	if err := yaml.Unmarshal(updated, &root); err != nil {
		return nil, fmt.Errorf("edited GitHub Actions YAML is invalid: %w", err)
	}
	return updated, nil
}
