package imageupdate

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml/parser"
)

// EditResult captures a single-line edit for preview (--dry-run) and reporting.
type EditResult struct {
	File       string `json:"file"`
	Line       int    `json:"line"`
	Before     string `json:"before"`
	After      string `json:"after"`
	NewContent string `json:"-"`
}

// valueLineRe captures, on a `key: value` line:
//   1 the prefix (indent + key + colon + spaces, optional leading "- ")
//   2/3/4 the value (double-quoted / single-quoted / bare)
//   5 the trailing remainder (spaces + optional comment)
var valueLineRe = regexp.MustCompile(`^(\s*(?:-\s+)?[^:\s]+:\s*)(?:"([^"]*)"|'([^']*)'|(\S+))(\s*(?:#.*)?)$`)

// Edit replaces the value on t.FieldLine with newValue, preserving indentation,
// quoting style, and any trailing comment. It operates on a string so it is
// unit-testable without disk I/O. The edited content is re-parsed to guarantee
// it still forms valid YAML before being returned.
func Edit(content string, t UpdateTarget, newValue string) (EditResult, error) {
	lines := strings.Split(content, "\n")
	if t.FieldLine < 1 || t.FieldLine > len(lines) {
		return EditResult{}, fmt.Errorf("%s: field line %d out of range (1..%d)", t.File, t.FieldLine, len(lines))
	}

	idx := t.FieldLine - 1
	before := lines[idx]
	m := valueLineRe.FindStringSubmatch(before)
	if m == nil {
		return EditResult{}, fmt.Errorf("%s:%d: cannot locate a scalar value to edit on line %q (block scalar or anchor?)", t.File, t.FieldLine, before)
	}

	prefix, suffix := m[1], m[5]
	valueRegion := before[len(prefix) : len(before)-len(suffix)]
	var rendered string
	switch {
	case strings.HasPrefix(valueRegion, `"`):
		rendered = prefix + `"` + newValue + `"` + suffix
	case strings.HasPrefix(valueRegion, `'`):
		rendered = prefix + `'` + newValue + `'` + suffix
	default:
		rendered = prefix + newValue + suffix
	}

	after := rendered
	lines[idx] = rendered
	newContent := strings.Join(lines, "\n")

	if _, err := parser.ParseBytes([]byte(newContent), 0); err != nil {
		return EditResult{}, fmt.Errorf("%s: edited content no longer parses: %w", t.File, err)
	}

	return EditResult{
		File:       t.File,
		Line:       t.FieldLine,
		Before:     before,
		After:      after,
		NewContent: newContent,
	}, nil
}

// ApplyEdit reads filePath, applies the single-line edit, and (unless dryRun)
// writes it back preserving the file's permissions. The returned EditResult
// always carries the before/after lines for reporting.
func ApplyEdit(filePath string, t UpdateTarget, newValue string, dryRun bool) (EditResult, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return EditResult{}, err
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		return EditResult{}, err
	}
	res, err := Edit(string(content), t, newValue)
	if err != nil {
		return EditResult{}, err
	}
	if dryRun {
		return res, nil
	}
	if err := os.WriteFile(filePath, []byte(res.NewContent), info.Mode()); err != nil {
		return EditResult{}, err
	}
	return res, nil
}
