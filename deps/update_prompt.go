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

func promptUpdateVersion(choice UpdateChoice) (string, bool) {
	candidate := choice.Candidate
	title := fmt.Sprintf("Select version for %s in %s (current %s)", candidate.Name, candidate.File, candidate.Current)
	return clicky.PromptSelect(choice.Versions, clicky.PromptSelectOptions[string]{
		Title:    title,
		PageSize: 12,
		Render: func(version string) api.Textable {
			text := clicky.Text(version, "font-mono")
			var tags []string
			if selectedVersionIsCurrent(candidate.Current, version) {
				tags = append(tags, "current")
			}
			if version == choice.LatestStable {
				tags = append(tags, "latest stable")
			}
			if version == choice.LatestPrerelease {
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
