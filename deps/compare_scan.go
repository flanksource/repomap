package deps

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/flanksource/repomap"
)

// CompareOptions configures a dependency diff. BaseRef is required; HeadRef may
// be empty to compare against the current working tree.
type CompareOptions struct {
	Options
	BaseRef string
	HeadRef string
}

// CompareScan resolves dependency graphs at two git revisions (or a revision and
// the working tree) and diffs them. Each non-working-tree side is materialized
// with `git worktree add --detach` so image/Helm discovery, which requires a
// real checkout, works unchanged. A dirty working tree is not an error.
func CompareScan(ctx context.Context, path string, opts CompareOptions) (*Comparison, error) {
	if opts.BaseRef == "" {
		return nil, fmt.Errorf("base git ref is required")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	repoRoot := repomap.FindGitRoot(absPath)
	if repoRoot == "" {
		return nil, fmt.Errorf("not a git repository: %s", absPath)
	}
	relPath, err := filepath.Rel(repoRoot, absPath)
	if err != nil {
		return nil, err
	}

	baseSha, err := resolveRef(ctx, repoRoot, opts.BaseRef)
	if err != nil {
		return nil, err
	}
	baseExport, err := scanAtRef(ctx, repoRoot, baseSha, relPath, opts.Options)
	if err != nil {
		return nil, err
	}

	var headExport *Export
	if opts.HeadRef == "" {
		headExport, err = Scan(ctx, absPath, opts.Options)
		if err != nil {
			return nil, err
		}
	} else {
		headSha, refErr := resolveRef(ctx, repoRoot, opts.HeadRef)
		if refErr != nil {
			return nil, refErr
		}
		headExport, err = scanAtRef(ctx, repoRoot, headSha, relPath, opts.Options)
		if err != nil {
			return nil, err
		}
	}

	comparison := Compare(baseExport, headExport)
	comparison.Metadata = ComparisonMetadata{Path: absPath, BaseRef: opts.BaseRef, HeadRef: opts.HeadRef}
	return comparison, nil
}

func scanAtRef(ctx context.Context, repoRoot, sha, relPath string, opts Options) (*Export, error) {
	worktree, cleanup, err := addWorktree(ctx, repoRoot, sha)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	scanDir := filepath.Join(worktree, relPath)
	if _, statErr := os.Stat(scanDir); statErr != nil {
		return nil, fmt.Errorf("path %q does not exist at ref %s: %w", relPath, sha, statErr)
	}
	return Scan(ctx, scanDir, opts)
}

func addWorktree(ctx context.Context, repoRoot, sha string) (string, func(), error) {
	parent, err := os.MkdirTemp("", "repomap-diff-*")
	if err != nil {
		return "", nil, err
	}
	worktree := filepath.Join(parent, "worktree")
	if _, err := runGitCmd(ctx, repoRoot, "worktree", "add", "--detach", worktree, sha); err != nil {
		_ = os.RemoveAll(parent)
		return "", nil, err
	}
	cleanup := func() {
		_, _ = runGitCmd(ctx, repoRoot, "worktree", "remove", "--force", worktree)
		_ = os.RemoveAll(parent)
	}
	return worktree, cleanup, nil
}

func resolveRef(ctx context.Context, repoRoot, ref string) (string, error) {
	out, err := runGitCmd(ctx, repoRoot, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("invalid git ref %q: %w", ref, err)
	}
	return strings.TrimSpace(out), nil
}

func runGitCmd(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), msg, err)
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return stdout.String(), nil
}
