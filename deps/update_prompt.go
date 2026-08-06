package deps

import (
	"fmt"
	"sort"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
)

func promptUpdateCandidates(choices []UpdateChoice) ([]UpdateChoice, bool) {
	return runUpdateChoiceTreePicker(choices)
}

// versionPromptKey identifies occurrences that pose the identical version
// question: the same dependency, at the same current version, with the same
// available versions. Occurrences that differ in any of those are genuinely
// different decisions and stay separate prompts.
func (c UpdateChoice) versionPromptKey() string {
	parts := append([]string{string(c.Candidate.Manager), c.Candidate.Name, c.Candidate.Current}, c.Versions...)
	return strings.Join(parts, "\x00")
}

// groupChoicesForVersionPrompt collapses occurrences sharing a version question
// into one group, keeping the input order of each group's first occurrence.
func groupChoicesForVersionPrompt(choices []UpdateChoice) [][]UpdateChoice {
	indexByKey := map[string]int{}
	var groups [][]UpdateChoice
	for _, choice := range choices {
		key := choice.versionPromptKey()
		if i, ok := indexByKey[key]; ok {
			groups[i] = append(groups[i], choice)
			continue
		}
		indexByKey[key] = len(groups)
		groups = append(groups, []UpdateChoice{choice})
	}
	return groups
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

type updateChoiceFileGroup struct {
	File    string
	Choices []UpdateChoice
}

func groupUpdateChoicesByFile(choices []UpdateChoice) []updateChoiceFileGroup {
	byFile := map[string][]UpdateChoice{}
	for _, choice := range choices {
		file := choice.Candidate.File
		byFile[file] = append(byFile[file], choice)
	}
	files := make([]string, 0, len(byFile))
	for file := range byFile {
		files = append(files, file)
	}
	sort.Strings(files)
	groups := make([]updateChoiceFileGroup, 0, len(files))
	for _, file := range files {
		groupChoices := append([]UpdateChoice(nil), byFile[file]...)
		sort.SliceStable(groupChoices, func(i, j int) bool {
			return groupChoices[i].Candidate.less(groupChoices[j].Candidate)
		})
		groups = append(groups, updateChoiceFileGroup{File: file, Choices: groupChoices})
	}
	return groups
}

func sortSelectedUpdateChoicesByFile(choices []UpdateChoice) []UpdateChoice {
	var out []UpdateChoice
	for _, group := range groupUpdateChoicesByFile(choices) {
		out = append(out, group.Choices...)
	}
	return out
}
