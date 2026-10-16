package deps

import (
	"fmt"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
)

// versionPromptKey identifies occurrences that pose the identical version
// question: the same dependency, at the same current version, with the same
// available versions. Occurrences that differ in any of those are genuinely
// different decisions and stay separate prompts.
func (c UpdateChoice) versionPromptKey() string {
	parts := append([]string{string(c.Candidate.Manager), c.Candidate.Name, c.Candidate.Current}, c.Versions...)
	return strings.Join(parts, "\x00")
}

func newUpdateVersionPrompt(group []UpdateChoice) UpdateVersionPrompt {
	files := make([]string, 0, len(group))
	seen := map[string]bool{}
	for _, choice := range group {
		if file := choice.Candidate.File; !seen[file] {
			seen[file] = true
			files = append(files, file)
		}
	}
	return UpdateVersionPrompt{UpdateChoice: group[0], Files: files}
}

func promptUpdateVersion(prompt UpdateVersionPrompt) (string, bool) {
	candidate := prompt.Candidate
	location := candidate.File
	if len(prompt.Files) > 1 {
		location = fmt.Sprintf("%d files", len(prompt.Files))
	}
	title := fmt.Sprintf("Select version for %s in %s (current %s)", candidate.Name, location, candidate.Current)
	return clicky.PromptSelect(prompt.Versions, clicky.PromptSelectOptions[string]{
		Title:    title,
		PageSize: 12,
		Render: func(version string) api.Textable {
			text := clicky.Text(version, "font-mono")
			var tags []string
			if selectedVersionIsCurrent(candidate.Current, version) {
				tags = append(tags, "current")
			}
			if version == prompt.LatestStable {
				tags = append(tags, "latest stable")
			}
			if version == prompt.LatestPrerelease {
				tags = append(tags, "latest pre-release")
			}
			if isPrerelease(version) {
				tags = append(tags, "pre-release")
			}
			if len(tags) > 0 {
				text = text.Space().Append("("+strings.Join(tags, ", ")+")", "text-muted")
			}
			return text
		},
	})
}
