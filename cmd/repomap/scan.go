package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/repomap"
)

type ScanOptions struct {
	Path   string `json:"path" flag:"path" help:"Path to scan" default:"."`
	Commit string `json:"commit" flag:"commit" help:"Git commit to scan at" default:"HEAD"`
	All    bool   `json:"all" flag:"all" help:"Show all files including those with no scopes"`
}

func (opts ScanOptions) GetName() string { return "scan" }

func (opts ScanOptions) Help() api.Text {
	return clicky.Text(`Scan a repository and output FileMap for each tracked file.

Uses git ls-files to enumerate tracked files, detecting language,
scopes, and Kubernetes references for each file.
By default, files with no scopes are filtered out. Use --all to show everything.

EXAMPLES:
  repomap scan
  repomap scan --all
  repomap scan --path ./my-repo`)
}

func init() {
	clicky.AddCommand(rootCmd, ScanOptions{}, runScan)
}

func runScan(opts ScanOptions) (any, error) {
	path, err := resolvePath(opts.Path)
	if err != nil {
		return nil, err
	}

	conf, err := repomap.GetConf(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	files, err := gitListFiles(conf.RepoPath())
	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}

	var results []repomap.FileMap
	for _, f := range files {
		relPath := f
		if filepath.IsAbs(f) {
			relPath, _ = filepath.Rel(conf.RepoPath(), f)
		}

		fm, err := conf.GetFileMap(relPath, opts.Commit)
		if err != nil {
			continue
		}
		if !opts.All && fm.IsEmpty() {
			continue
		}
		results = append(results, *fm)
	}

	return results, nil
}

func resolvePath(path string) (string, error) {
	if workingDir != "" {
		path = filepath.Join(workingDir, path)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path: %w", err)
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return "", fmt.Errorf("path does not exist: %s", absPath)
	}
	return absPath, nil
}

func gitListFiles(repoPath string) ([]string, error) {
	git := clicky.Exec("git").WithCwd(repoPath).AsWrapper()
	result, err := git("ls-files")
	if err != nil {
		return nil, fmt.Errorf("git ls-files failed: %w", err)
	}

	var files []string
	for _, line := range strings.Split(result.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}
