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
	return clicky.Text(`Show the merged repomap configuration.

Displays the effective configuration after merging user-defined
repomap.yaml rules with embedded defaults.

EXAMPLES:
  repomap config
  repomap config --path ./my-repo`)
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
	clicky.AddCommand(rootCmd, ConfigOptions{}, runConfig)
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
