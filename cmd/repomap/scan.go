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
	Path    string `json:"path" args:"true" help:"Path to scan"`
	Commit  string `json:"commit" flag:"commit" help:"Git commit to scan at" default:"HEAD"`
	All     bool   `json:"all" flag:"all" help:"Show all files including those with no scopes"`
	Flat    bool   `json:"flat" flag:"flat" help:"Output flat list instead of tree"`
	Verbose bool   `json:"verbose" flag:"verbose" help:"Show scope rules that matched each file"`
}

func (opts ScanOptions) GetName() string { return "scan" }

func (opts ScanOptions) Help() api.Text {
	return clicky.Text(`Scan a repository and classify tracked files by language, scope,
and Kubernetes references.

Uses git ls-files to enumerate tracked files. Outputs a tree view
by default, or a flat table with --flat. Files with no scopes are
hidden unless --all is specified.

EXAMPLES:
  repomap scan                  # scan current directory as tree
  repomap scan ./my-repo        # scan a specific path
  repomap scan --all            # include unclassified files
  repomap scan --flat           # flat table output
  repomap scan --format json    # JSON output`)
}

func init() {
	cmd := clicky.AddCommand(rootCmd, ScanOptions{}, runScan)
	cmd.Short = "Classify tracked files by language, scope, and Kubernetes references"
}

func runScan(opts ScanOptions) (any, error) {
	if opts.Path == "" {
		opts.Path = "."
	}
	path, err := resolvePath(opts.Path)
	if err != nil {
		return nil, err
	}

	conf, err := repomap.GetConf(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Determine prefix filter when scanning a subdirectory
	var prefix string
	if rel, err := filepath.Rel(conf.RepoPath(), path); err == nil && rel != "." {
		prefix = rel + string(filepath.Separator)
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
		if prefix != "" && !strings.HasPrefix(relPath, prefix) {
			continue
		}

		fm, err := conf.GetFileMap(relPath, opts.Commit)
		if err != nil {
			continue
		}
		if !opts.All && fm.IsEmpty() {
			continue
		}
		if !opts.Verbose {
			fm.ScopeMatches = nil
		}
		results = append(results, *fm)
	}

	if opts.Flat {
		return api.NewTableFrom(results), nil
	}
	tree := repomap.NewFileTree(results)
	return api.NewTree(tree), nil
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
