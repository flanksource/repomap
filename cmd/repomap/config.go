package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/repomap"
)

type ConfigOptions struct {
	Path string `json:"path" flag:"path" help:"Path to show configuration for" default:"."`
}

func (opts ConfigOptions) GetName() string { return "config" }

func (opts ConfigOptions) Help() api.Text {
	return clicky.Text(`Display the effective repomap configuration.

Shows the merged result of user-defined repomap.yaml and embedded
defaults, including scope rules, git settings, and severity rules.
Useful for debugging why files are classified a certain way.

EXAMPLES:
  repomap config                  # show config for current directory
  repomap config --path ./my-repo # show config for a specific repo`)
}

type ConfigView struct {
	ConfigSource string
	Config       *repomap.ArchConf
}

func (v ConfigView) Pretty() api.Text {
	t := clicky.Text("")
	if v.ConfigSource != "" {
		t = t.Append("Configuration loaded from: ", "text-muted").Append(v.ConfigSource).NewLine().NewLine()
	} else {
		t = t.Append("No user configuration found, using embedded defaults only", "text-yellow-600").NewLine().NewLine()
	}
	t = t.Append("Merged Configuration:", "font-bold").NewLine().NewLine()
	t = t.Append(v.Config.Pretty())
	return t
}

func init() {
	cmd := clicky.AddCommand(rootCmd, ConfigOptions{}, runConfig)
	cmd.Short = "Display effective repomap configuration"
}

func runConfig(opts ConfigOptions) (any, error) {
	path, err := resolvePath(opts.Path)
	if err != nil {
		return nil, err
	}

	conf, err := repomap.GetConf(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	return ConfigView{
		ConfigSource: findConfigSource(path),
		Config:       conf,
	}, nil
}

func findConfigSource(path string) string {
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		path = filepath.Dir(path)
	}
	path, _ = filepath.Abs(path)

	for {
		configFile := filepath.Join(path, "repomap.yaml")
		if stat, err := os.Stat(configFile); err == nil && !stat.IsDir() {
			return configFile
		}
		if repomap.IsGitRoot(path) {
			break
		}
		parent := filepath.Dir(path)
		if parent == path {
			break
		}
		path = parent
	}
	return ""
}
