package main

import (
	"os"
	"path/filepath"

	"github.com/flanksource/repomap"
)

type displayPathFunc func(string) string

func displayPathForRepoFile(conf *repomap.ArchConf, repoRelPath string) string {
	if conf == nil || repoRelPath == "" {
		return repoRelPath
	}
	absPath := filepath.Join(conf.RepoPath(), filepath.FromSlash(repoRelPath))
	cwd, err := displayCwd()
	if err != nil {
		return filepath.ToSlash(filepath.Clean(repoRelPath))
	}
	rel, err := filepath.Rel(cwd, absPath)
	if err != nil {
		return filepath.ToSlash(filepath.Clean(repoRelPath))
	}
	return filepath.ToSlash(rel)
}

func displayPathFuncForConf(conf *repomap.ArchConf) displayPathFunc {
	return func(repoRelPath string) string {
		return displayPathForRepoFile(conf, repoRelPath)
	}
}

func displayCwd() (string, error) {
	if workingDir != "" {
		return filepath.Abs(workingDir)
	}
	return os.Getwd()
}
