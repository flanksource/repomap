package deps

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
)

const gitCommitPickerOption = "Git commit (SHA)"

func promptUpdateVersionWithGit(ctx context.Context, runner CommandRunner, prompt UpdateVersionPrompt) (string, bool, error) {
	if prompt.Candidate.Manager != ManagerGo && prompt.Candidate.git == nil {
		version, ok := promptUpdateVersion(prompt)
		return version, ok, nil
	}
	options := append(append([]string{}, prompt.Versions...), gitCommitPickerOption)
	selected, ok := clicky.PromptSelect(options, clicky.PromptSelectOptions[string]{
		Title:    fmt.Sprintf("Select version for %s in %s", prompt.Candidate.Name, strings.Join(prompt.Files, ", ")),
		PageSize: 12,
	})
	if !ok || selected != gitCommitPickerOption {
		return selected, ok, nil
	}
	remote := ""
	if prompt.Candidate.git != nil {
		remote = prompt.Candidate.git.Remote
	} else {
		var err error
		remote, err = goModuleGitRemote(ctx, runner, prompt.Candidate)
		if err != nil {
			return "", false, err
		}
	}
	refs, err := listGitRefChoices(ctx, runner, remote)
	if err != nil {
		return "", false, err
	}
	allRefs := append(refs.Branches, refs.PRHeads...)
	selectedRef, ok := clicky.PromptSelect(allRefs, clicky.PromptSelectOptions[gitRemoteRef]{
		Title:    fmt.Sprintf("Select branch or PR head for %s", prompt.Candidate.Name),
		PageSize: 12,
		Render: func(ref gitRemoteRef) api.Textable {
			label := strings.TrimPrefix(strings.TrimPrefix(ref.Name, "refs/heads/"), "refs/")
			if !ref.Date.IsZero() {
				label += "  " + ref.Date.Format("2006-01-02") + "  " + ref.Subject
			}
			return clicky.Text(label)
		},
	})
	if !ok {
		return "", false, nil
	}
	commits, err := recentGitCommits(ctx, runner, remote, selectedRef)
	if err != nil {
		return "", false, err
	}
	selectedCommit, ok := clicky.PromptSelect(commits, clicky.PromptSelectOptions[gitCommit]{
		Title:    fmt.Sprintf("Select commit for %s", prompt.Candidate.Name),
		PageSize: 12,
		Render: func(commit gitCommit) api.Textable {
			return clicky.Text(commit.SHA[:12] + "  " + commit.Date.Format("2006-01-02") + "  " + commit.Subject)
		},
	})
	if !ok {
		return "", false, nil
	}
	if prompt.Candidate.git != nil {
		return prompt.Candidate.git.Pin(selectedCommit.SHA), true, nil
	}
	return selectedCommit.SHA, true, nil
}

func goModuleGitRemote(ctx context.Context, runner CommandRunner, candidate UpdateCandidate) (string, error) {
	cmd := Command{
		Dir:  candidate.Dir,
		Name: "go",
		Args: []string{"list", "-m", "-json", candidate.Name + "@" + candidate.Current},
		Env:  []string{"GOFLAGS=-mod=readonly"},
	}
	result, err := runner.Run(ctx, cmd)
	if err != nil {
		return "", fmt.Errorf("%s", commandFailure(cmd, result, err))
	}
	var module struct {
		Path   string `json:"Path"`
		Origin struct {
			VCS string `json:"VCS"`
			URL string `json:"URL"`
		} `json:"Origin"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &module); err != nil {
		return "", fmt.Errorf("read Git origin for %s: %w", candidate.Name, err)
	}
	if module.Path != candidate.Name || module.Origin.VCS != "git" || module.Origin.URL == "" {
		return "", fmt.Errorf("%s: Go module metadata has no Git origin", candidate.Name)
	}
	return module.Origin.URL, nil
}
