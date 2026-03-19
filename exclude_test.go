package repomap

import (
	"testing"
)

func TestExcludeConfigMerge(t *testing.T) {
	base := ExcludeConfig{
		Files:   []string{"*.lock"},
		Authors: []string{"bot@test.com"},
	}
	other := ExcludeConfig{
		Files:       []string{"*.svg"},
		CommitTypes: []string{"chore"},
	}

	merged := base.Merge(other)

	if len(merged.Files) != 2 || merged.Files[0] != "*.lock" || merged.Files[1] != "*.svg" {
		t.Errorf("expected merged files [*.lock, *.svg], got %v", merged.Files)
	}
	if len(merged.Authors) != 1 || merged.Authors[0] != "bot@test.com" {
		t.Errorf("expected merged authors [bot@test.com], got %v", merged.Authors)
	}
	if len(merged.CommitTypes) != 1 || merged.CommitTypes[0] != "chore" {
		t.Errorf("expected merged commit types [chore], got %v", merged.CommitTypes)
	}
}

func TestExcludeConfigIsEmpty(t *testing.T) {
	empty := ExcludeConfig{}
	if !empty.IsEmpty() {
		t.Error("expected empty config to be empty")
	}

	notEmpty := ExcludeConfig{Files: []string{"*.lock"}}
	if notEmpty.IsEmpty() {
		t.Error("expected non-empty config to not be empty")
	}
}

func TestResolvePresets(t *testing.T) {
	presets := map[string]Preset{
		"bots":  {Exclude: ExcludeConfig{Authors: []string{"dependabot*"}}},
		"noise": {Exclude: ExcludeConfig{Files: []string{"*.lock"}}},
	}

	config := ExcludeConfig{Files: []string{"*.svg"}}
	config.ResolvePresets([]string{"preset:bots", "preset:noise"}, presets)

	if len(config.Authors) != 1 || config.Authors[0] != "dependabot*" {
		t.Errorf("expected authors [dependabot*], got %v", config.Authors)
	}
	// Original files should still be there, plus preset files
	hasLock := false
	hasSvg := false
	for _, f := range config.Files {
		if f == "*.lock" {
			hasLock = true
		}
		if f == "*.svg" {
			hasSvg = true
		}
	}
	if !hasLock || !hasSvg {
		t.Errorf("expected files to contain both *.lock and *.svg, got %v", config.Files)
	}
}

func TestResolvePresetsWithoutPrefix(t *testing.T) {
	presets := map[string]Preset{
		"bots": {Exclude: ExcludeConfig{Authors: []string{"renovate*"}}},
	}

	config := ExcludeConfig{}
	config.ResolvePresets([]string{"bots"}, presets)

	if len(config.Authors) != 1 || config.Authors[0] != "renovate*" {
		t.Errorf("expected authors [renovate*], got %v", config.Authors)
	}
}

func TestMatchesAuthor(t *testing.T) {
	author := Author{Name: "dependabot[bot]", Email: "dependabot@github.com"}

	matched, reason := MatchesAuthor(author, []string{"dependabot*"})
	if !matched {
		t.Error("expected match for dependabot*")
	}
	if reason == "" {
		t.Error("expected non-empty reason")
	}

	matched, _ = MatchesAuthor(author, []string{"renovate*"})
	if matched {
		t.Error("expected no match for renovate*")
	}
}

func TestMatchesCommitMessage(t *testing.T) {
	matched, _ := MatchesCommitMessage("fixup! some commit", []string{"fixup!*"})
	if !matched {
		t.Error("expected match for fixup!*")
	}

	matched, _ = MatchesCommitMessage("feat: new feature", []string{"fixup!*"})
	if matched {
		t.Error("expected no match for fixup!*")
	}

	matched, _ = MatchesCommitMessage("anything", nil)
	if matched {
		t.Error("expected no match for nil patterns")
	}
}

func TestMatchesCommitType(t *testing.T) {
	matched, _ := MatchesCommitType("chore", []string{"chore", "ci"})
	if !matched {
		t.Error("expected match for chore")
	}

	matched, _ = MatchesCommitType("feat", []string{"chore", "ci"})
	if matched {
		t.Error("expected no match for feat")
	}
}

func TestMatchesFile(t *testing.T) {
	matched, _ := MatchesFile("package-lock.json", []string{"package-lock.json"})
	if !matched {
		t.Error("expected match for package-lock.json")
	}

	matched, _ = MatchesFile("src/utils/helper.go", []string{"*.lock"})
	if matched {
		t.Error("expected no match for *.lock")
	}

	matched, _ = MatchesFile("go.sum", []string{"go.sum"})
	if !matched {
		t.Error("expected match for go.sum")
	}

	matched, _ = MatchesFile("path/to/something.lock", []string{"*.lock"})
	if !matched {
		t.Error("expected match for *.lock via basename")
	}
}

func TestMatchesResourceFields(t *testing.T) {
	if !MatchesResourceFields("Secret", "my-secret", "default", ResourceFilter{Kind: "Secret"}) {
		t.Error("expected match for kind=Secret")
	}

	if MatchesResourceFields("ConfigMap", "my-cm", "default", ResourceFilter{Kind: "Secret"}) {
		t.Error("expected no match for kind=Secret when kind is ConfigMap")
	}

	if !MatchesResourceFields("Deployment", "nginx", "kube-system", ResourceFilter{Namespace: "kube-system"}) {
		t.Error("expected match for namespace=kube-system")
	}

	if !MatchesResourceFields("Secret", "api-key", "prod", ResourceFilter{Kind: "Secret", Name: "api-*"}) {
		t.Error("expected match for kind=Secret, name=api-*")
	}
}

func TestCompileExcludeConfig(t *testing.T) {
	config := &ExcludeConfig{
		Rules: []ExcludeRule{
			{When: "commit.is_merge"},
			{When: "commit.line_changes > 10000"},
		},
	}

	compiled, err := config.Compile()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(compiled.CompiledRules()) != 2 {
		t.Errorf("expected 2 compiled rules, got %d", len(compiled.CompiledRules()))
	}
}

func TestCompileExcludeConfigInvalid(t *testing.T) {
	config := &ExcludeConfig{
		Rules: []ExcludeRule{
			{When: "invalid syntax !!!"},
		},
	}

	_, err := config.Compile()
	if err == nil {
		t.Error("expected error for invalid CEL expression")
	}
}

func TestSeverityRulesList(t *testing.T) {
	config := &SeverityConfig{
		Default: Medium,
		Rules: map[string]Severity{
			`change.type == "deleted"`: Critical,
		},
		RulesList: []SeverityRule{
			{ID: "large-commits", When: `commit.line_changes > 500`, Severity: Critical},
		},
	}

	all := config.AllRules()
	if len(all) != 2 {
		t.Errorf("expected 2 rules, got %d", len(all))
	}
	if all[`change.type == "deleted"`] != Critical {
		t.Error("expected deleted rule to be critical")
	}
	if all[`commit.line_changes > 500`] != Critical {
		t.Error("expected large commits rule to be critical")
	}
}
