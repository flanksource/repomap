package deps

import (
	"context"
	"os"
	"path/filepath"
	"sort"
)

// stageUpdatedFiles runs `git add` for the manifest and lockfiles a successful
// dependency update touches, returning the cwd-relative paths that were staged.
func stageUpdatedFiles(ctx context.Context, runner CommandRunner, candidate UpdateCandidate) ([]string, error) {
	if runner == nil {
		runner = ExecRunner{}
	}
	relPaths := updatedRepoFiles(candidate)
	existing := make([]string, 0, len(relPaths))
	for _, rel := range relPaths {
		abs := filepath.Join(candidate.Dir, filepath.FromSlash(rel))
		if info, err := os.Stat(abs); err == nil && !info.IsDir() {
			existing = append(existing, rel)
		}
	}
	if len(existing) == 0 {
		return nil, nil
	}
	sort.Strings(existing)

	args := append([]string{"add", "--"}, existing...)
	if _, err := runner.Run(ctx, Command{Dir: candidate.Dir, Name: "git", Args: args}); err != nil {
		return nil, err
	}

	staged := make([]string, 0, len(existing))
	for _, rel := range existing {
		staged = append(staged, cwdRelativePath(filepath.Join(candidate.Dir, filepath.FromSlash(rel))))
	}
	sort.Strings(staged)
	return staged, nil
}

// updatedRepoFiles lists the files a manager rewrites during an update, relative
// to candidate.Dir using forward slashes.
func updatedRepoFiles(candidate UpdateCandidate) []string {
	switch candidate.Manager {
	case ManagerGo:
		return []string{"go.mod", "go.sum"}
	case ManagerNPM:
		return []string{"package.json", "package-lock.json"}
	case ManagerPNPM:
		return []string{"package.json", "pnpm-lock.yaml"}
	case ManagerImage, ManagerHelm:
		if candidate.Target != nil && candidate.Target.File != "" {
			return []string{candidate.Target.File}
		}
		return nil
	default:
		return nil
	}
}
