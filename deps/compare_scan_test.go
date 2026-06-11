package deps

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "test")
	return dir
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s failed: %v", strings.Join(args, " "), err)
	}
	return string(out)
}

func TestCompareScanHeadDiffAgainstWorkingTree(t *testing.T) {
	dir := initRepo(t)
	gomod := filepath.Join(dir, "go.mod")
	writeFile(t, gomod, "module github.com/acme/app\n\ngo 1.22\n\nrequire github.com/acme/a v1.0.0\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "base")
	writeFile(t, gomod, "module github.com/acme/app\n\ngo 1.22\n\nrequire github.com/acme/a v1.5.0\n")

	cmp, err := CompareScan(context.Background(), dir, CompareOptions{Options: Options{MaxDepth: 1}, BaseRef: "HEAD"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cmp.Updated) != 1 || cmp.Updated[0].Name != "github.com/acme/a" || cmp.Updated[0].OldVersion != "v1.0.0" || cmp.Updated[0].NewVersion != "v1.5.0" {
		t.Fatalf("expected a 1.0.0 -> 2.0.0, got %+v", cmp.Updated)
	}
	if lines := strings.TrimSpace(gitOutput(t, dir, "worktree", "list")); strings.Count(lines, "\n") != 0 {
		t.Fatalf("worktree not cleaned up:\n%s", lines)
	}
}

func TestCompareScanRefRange(t *testing.T) {
	dir := initRepo(t)
	gomod := filepath.Join(dir, "go.mod")
	writeFile(t, gomod, "module github.com/acme/app\n\ngo 1.22\n\nrequire github.com/acme/a v1.0.0\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "v1")
	writeFile(t, gomod, "module github.com/acme/app\n\ngo 1.22\n\nrequire github.com/acme/a v1.5.0\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "v2")

	cmp, err := CompareScan(context.Background(), dir, CompareOptions{Options: Options{MaxDepth: 1}, BaseRef: "HEAD~1", HeadRef: "HEAD"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cmp.Updated) != 1 || cmp.Updated[0].NewVersion != "v1.5.0" {
		t.Fatalf("ref1..ref2 diff failed: %+v", cmp.Updated)
	}
}

func TestCompareScanFilterAppliedBothSides(t *testing.T) {
	dir := initRepo(t)
	gomod := filepath.Join(dir, "go.mod")
	writeFile(t, gomod, "module github.com/acme/app\n\ngo 1.22\n\nrequire (\n\tgithub.com/acme/alpha v1.0.0\n\tgithub.com/acme/beta v1.0.0\n)\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "base")
	writeFile(t, gomod, "module github.com/acme/app\n\ngo 1.22\n\nrequire (\n\tgithub.com/acme/alpha v1.5.0\n\tgithub.com/acme/beta v1.5.0\n)\n")

	cmp, err := CompareScan(context.Background(), dir, CompareOptions{Options: Options{MaxDepth: 1, Filters: []string{"*alpha*"}}, BaseRef: "HEAD"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cmp.Updated) != 1 || !strings.Contains(cmp.Updated[0].Name, "alpha") {
		t.Fatalf("filter should restrict both sides to alpha, got %+v", cmp.Updated)
	}
}

func TestCompareScanInvalidRef(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, filepath.Join(dir, "go.mod"), "module github.com/acme/app\n\ngo 1.22\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "base")

	_, err := CompareScan(context.Background(), dir, CompareOptions{Options: Options{MaxDepth: 1}, BaseRef: "does-not-exist"})
	if err == nil || !strings.Contains(err.Error(), "invalid git ref") {
		t.Fatalf("expected invalid ref error, got %v", err)
	}
}

func TestCompareScanNonGitDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module github.com/acme/app\n\ngo 1.22\n")
	_, err := CompareScan(context.Background(), dir, CompareOptions{Options: Options{MaxDepth: 1}, BaseRef: "HEAD"})
	if err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("expected non-git error, got %v", err)
	}
}

func TestCompareScanPathMissingAtRef(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, filepath.Join(dir, "go.mod"), "module github.com/acme/app\n\ngo 1.22\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "base")
	// svc only exists in the working tree, not at HEAD.
	writeFile(t, filepath.Join(dir, "svc", "go.mod"), "module github.com/acme/svc\n\ngo 1.22\n")

	_, err := CompareScan(context.Background(), filepath.Join(dir, "svc"), CompareOptions{Options: Options{MaxDepth: 1}, BaseRef: "HEAD"})
	if err == nil || !strings.Contains(err.Error(), "does not exist at ref") {
		t.Fatalf("expected path-missing-at-ref error, got %v", err)
	}
}
