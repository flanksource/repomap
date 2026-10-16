package deps

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const gitPickerLimit = 30

type gitRemoteRef struct {
	Name    string
	Hash    string
	Date    time.Time
	Subject string
}

type gitRefChoices struct {
	Branches []gitRemoteRef
	PRHeads  []gitRemoteRef
}

type gitCommit struct {
	SHA     string
	Date    time.Time
	Subject string
}

func listGitRefChoices(ctx context.Context, runner CommandRunner, remote string) (gitRefChoices, error) {
	output, err := runGitPickerCommand(ctx, runner, "", remote, "ls-remote", "--symref", remote, "HEAD", "refs/heads/*", "refs/pull/*/head", "refs/merge-requests/*/head")
	if err != nil {
		return gitRefChoices{}, fmt.Errorf("list remote Git refs: %w", err)
	}
	refs, advertisedPRs, err := parseGitRemoteRefs(output)
	if err != nil || len(advertisedPRs) == 0 {
		return refs, err
	}
	refs.PRHeads, err = fetchPRHeadMetadata(ctx, runner, remote, advertisedPRs)
	return refs, err
}

func parseGitRemoteRefs(output string) (gitRefChoices, []gitRemoteRef, error) {
	var refs gitRefChoices
	var defaultBranch string
	var advertisedPRs []gitRemoteRef
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		fields := strings.SplitN(line, "\t", 2)
		if len(fields) != 2 {
			return gitRefChoices{}, nil, fmt.Errorf("invalid Git remote ref line %q", line)
		}
		if fields[1] == "HEAD" && strings.HasPrefix(fields[0], "ref: refs/heads/") {
			defaultBranch = strings.TrimPrefix(fields[0], "ref: ")
			continue
		}
		if fields[1] == "HEAD" {
			continue
		}
		ref := gitRemoteRef{Name: fields[1], Hash: fields[0]}
		if !validCommitSHA(ref.Hash) {
			return gitRefChoices{}, nil, fmt.Errorf("invalid commit hash for %s", ref.Name)
		}
		switch {
		case strings.HasPrefix(ref.Name, "refs/heads/"):
			refs.Branches = append(refs.Branches, ref)
		case strings.HasPrefix(ref.Name, "refs/pull/") && strings.HasSuffix(ref.Name, "/head"), strings.HasPrefix(ref.Name, "refs/merge-requests/") && strings.HasSuffix(ref.Name, "/head"):
			advertisedPRs = append(advertisedPRs, ref)
		}
	}
	if len(refs.Branches) == 0 {
		return gitRefChoices{}, nil, fmt.Errorf("remote has no advertised branches")
	}
	sort.Slice(refs.Branches, func(i, j int) bool {
		if refs.Branches[i].Name == defaultBranch {
			return true
		}
		if refs.Branches[j].Name == defaultBranch {
			return false
		}
		return refs.Branches[i].Name < refs.Branches[j].Name
	})
	return refs, advertisedPRs, nil
}

func fetchPRHeadMetadata(ctx context.Context, runner CommandRunner, remote string, advertisedPRs []gitRemoteRef) (heads []gitRemoteRef, err error) {
	repo, err := newGitPickerRepo(ctx, runner)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cleanupErr := os.RemoveAll(repo); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("remove Git metadata repository: %w", cleanupErr))
		}
	}()
	args := []string{"--git-dir", repo, "fetch", "--depth=1", "--filter=blob:none", "--no-tags", remote}
	for _, ref := range advertisedPRs {
		args = append(args, "+"+ref.Name+":refs/repomap/"+strings.TrimPrefix(ref.Name, "refs/"))
	}
	if _, err := runGitPickerCommand(ctx, runner, "", remote, args...); err != nil {
		return nil, fmt.Errorf("fetch PR head metadata: %w", err)
	}
	metadata, err := runGitPickerCommand(ctx, runner, "", remote, "--git-dir", repo, "for-each-ref", "--format=%(objectname)%09%(committerdate:iso-strict)%09%(subject)%09%(refname)", "refs/repomap")
	if err != nil {
		return nil, fmt.Errorf("read PR head metadata: %w", err)
	}
	return parsePRHeadMetadata(metadata, advertisedPRs)
}

func parsePRHeadMetadata(metadata string, advertisedPRs []gitRemoteRef) ([]gitRemoteRef, error) {
	byRef := make(map[string]gitRemoteRef, len(advertisedPRs))
	for _, ref := range advertisedPRs {
		byRef["refs/repomap/"+strings.TrimPrefix(ref.Name, "refs/")] = ref
	}
	var heads []gitRemoteRef
	for _, line := range strings.Split(strings.TrimSpace(metadata), "\n") {
		fields := strings.SplitN(line, "\t", 4)
		if len(fields) != 4 {
			return nil, fmt.Errorf("invalid PR head metadata %q", line)
		}
		ref, ok := byRef[fields[3]]
		if !ok || ref.Hash != fields[0] {
			return nil, fmt.Errorf("PR head changed while fetching: %s", fields[3])
		}
		date, err := time.Parse(time.RFC3339, fields[1])
		if err != nil {
			return nil, fmt.Errorf("invalid PR head date for %s: %w", ref.Name, err)
		}
		ref.Date = date
		ref.Subject = fields[2]
		heads = append(heads, ref)
	}
	if len(heads) != len(advertisedPRs) {
		return nil, fmt.Errorf("git remote returned incomplete PR head metadata")
	}
	sort.Slice(heads, func(i, j int) bool {
		if heads[i].Date.Equal(heads[j].Date) {
			return heads[i].Name < heads[j].Name
		}
		return heads[i].Date.After(heads[j].Date)
	})
	if len(heads) > gitPickerLimit {
		heads = heads[:gitPickerLimit]
	}
	return heads, nil
}

func recentGitCommits(ctx context.Context, runner CommandRunner, remote string, ref gitRemoteRef) (commits []gitCommit, err error) {
	repo, err := newGitPickerRepo(ctx, runner)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cleanupErr := os.RemoveAll(repo); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("remove Git metadata repository: %w", cleanupErr))
		}
	}()
	_, err = runGitPickerCommand(ctx, runner, "", remote, "--git-dir", repo, "fetch", "--depth=30", "--filter=blob:none", "--no-tags", remote, "+"+ref.Name+":refs/repomap/selected")
	if err != nil {
		return nil, fmt.Errorf("fetch branch commits: %w", err)
	}
	output, err := runGitPickerCommand(ctx, runner, "", remote, "--git-dir", repo, "log", "--date-order", "-n", "30", "--format=%H%x09%cI%x09%s", "refs/repomap/selected")
	if err != nil {
		return nil, fmt.Errorf("read branch commits: %w", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 || !validCommitSHA(fields[0]) {
			return nil, fmt.Errorf("invalid Git commit metadata %q", line)
		}
		date, err := time.Parse(time.RFC3339, fields[1])
		if err != nil {
			return nil, fmt.Errorf("invalid Git commit date: %w", err)
		}
		commits = append(commits, gitCommit{SHA: fields[0], Date: date, Subject: fields[2]})
	}
	if len(commits) == 0 || commits[0].SHA != ref.Hash {
		return nil, fmt.Errorf("branch %s changed while fetching", ref.Name)
	}
	return commits, nil
}

func newGitPickerRepo(ctx context.Context, runner CommandRunner) (string, error) {
	root, err := filepath.Abs(filepath.Join(".tmp", "repomap", "git-update"))
	if err != nil {
		return "", fmt.Errorf("locate Git metadata directory: %w", err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("create Git metadata directory: %w", err)
	}
	repo, err := os.MkdirTemp(root, "refs-")
	if err != nil {
		return "", fmt.Errorf("create Git metadata repository: %w", err)
	}
	if _, err := runGitPickerCommand(ctx, runner, "", "", "init", "--quiet", "--bare", repo); err != nil {
		return "", errors.Join(fmt.Errorf("initialize Git metadata repository: %w", err), os.RemoveAll(repo))
	}
	return repo, nil
}

func runGitPickerCommand(ctx context.Context, runner CommandRunner, dir, remote string, args ...string) (string, error) {
	if runner == nil {
		runner = ExecRunner{}
	}
	result, err := runner.Run(ctx, Command{Dir: dir, Name: "git", Args: args})
	if err != nil {
		stderr := result.Stderr
		if remote != "" {
			stderr = strings.ReplaceAll(stderr, remote, "<remote>")
		}
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr))
	}
	return result.Stdout, nil
}
