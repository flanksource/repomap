package deps

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func updateInTestRepo(ctx context.Context, path string, opts UpdateOptions) ([]UpdatePlan, error) {
	if !opts.DryRun && !opts.Check {
		if err := prepareUpdateTestRepo(path); err != nil {
			return nil, err
		}
	}
	return Update(ctx, path, opts)
}

func prepareUpdateTestRepo(path string) error {
	if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
		return nil
	}
	cmd := exec.Command("git", "-C", path, "init", "--quiet")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("init fixture: %w: %s", err, out)
	}
	var files []string
	err := filepath.WalkDir(path, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == ".tmp" || entry.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(path, file)
		if err != nil {
			return err
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}
	cmd = exec.Command("git", append([]string{"-C", path, "add", "--"}, files...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("track fixtures %s: %w: %s", strings.Join(files, ","), err, out)
	}
	return nil
}
