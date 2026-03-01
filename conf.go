package repomap

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/exec"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/repomap/kubernetes"
	"github.com/ghodss/yaml"
	"github.com/samber/lo"
	"github.com/samber/oops"
)

//go:embed defaults.yaml
var defaultArchYAML string

type ArchConf struct {
	Git      GitConfig      `json:"git,omitempty" yaml:"git,omitempty"`
	Build    BuildConfig    `json:"build,omitempty" yaml:"build,omitempty"`
	Golang   GolangConfig   `json:"golang,omitempty" yaml:"golang,omitempty"`
	Scopes   ScopesConfig   `json:"scopes,omitempty" yaml:"scopes,omitempty"`
	Severity SeverityConfig `json:"severity,omitempty" yaml:"severity,omitempty"`
	repoPath string         `json:"-" yaml:"-"`
}

func (conf *ArchConf) GetFileMap(path string, commit string) (*FileMap, error) {
	f := FileMap{
		Path:     path,
		Language: DetectLanguage(path),
	}

	f.Scopes = conf.Scopes.GetScopesByPath(path)

	if kubernetes.IsYaml(path) {
		if !conf.FileExistsAtCommit(path, commit) {
			return &f, nil
		}

		content, err := conf.ReadFile(path, commit)
		if err != nil {
			logger.Errorf("Error reading file path=%s commit=%s: %v", path, commit, err)
			return &f, nil
		}

		f.KubernetesRefs, err = kubernetes.ExtractKubernetesRefsFromContent(content)
		if err != nil {
			logger.Errorf("Error extracting k8s refs path=%s commit=%s: %v", path, commit, err)
		}
	}

	return &f, nil
}

func IsGitRoot(path string) bool {
	if _, err := os.Stat(filepath.Join(path, ".git")); os.IsNotExist(err) {
		return false
	}
	return true
}

func FindGitRoot(path string) string {
	dir := path
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		dir = filepath.Dir(path)
	}

	for {
		if IsGitRoot(dir) {
			dir, err := filepath.Abs(dir)
			if err != nil {
				panic(err)
			}
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func GetFileMap(path string, commit string) (*FileMap, error) {
	userConf, err := GetConfForFile(path)
	if err != nil {
		return nil, oops.Wrapf(err, "failed to get repomap.yaml for %s", path)
	}

	defaultConf, err := LoadDefaultArchConf()
	if err != nil {
		return nil, oops.Wrapf(err, "failed to load embedded defaults")
	}

	conf := defaultConf.Merge(userConf)

	repoPath := FindGitRoot(path)
	if repoPath == "" {
		return nil, fmt.Errorf("failed to find git repository root for path: %s", path)
	}
	conf.repoPath = repoPath

	return conf.GetFileMap(path, commit)
}

func GetConf(path string) (*ArchConf, error) {
	userConf, err := GetConfForFile(path)
	if err != nil {
		return nil, oops.Wrapf(err, "failed to get repomap.yaml for %s", path)
	}

	defaultConf, err := LoadDefaultArchConf()
	if err != nil {
		return nil, oops.Wrapf(err, "failed to load embedded defaults")
	}

	conf := defaultConf.Merge(userConf)

	repoPath := FindGitRoot(path)
	if repoPath == "" {
		return nil, fmt.Errorf("failed to find git repository root for path: %s", path)
	}
	conf.repoPath = repoPath

	return &conf, nil
}

func GetConfForFile(path string) (*ArchConf, error) {
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		path = filepath.Dir(path)
	}

	path, _ = filepath.Abs(path)
	file := filepath.Join(path, "repomap.yaml")
	if stat, err := os.Stat(file); os.IsNotExist(err) {
		if IsGitRoot(path) {
			return nil, nil
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil, nil
		}
		return GetConfForFile(parent)
	} else if err == nil && !stat.IsDir() {
		return LoadArchConf(file)
	}
	return nil, nil
}

func LoadArchConf(path string) (*ArchConf, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, oops.Wrapf(err, "failed to load repomap.yaml from %s", path)
	}

	var conf ArchConf
	if err := yaml.Unmarshal(data, &conf); err != nil {
		return nil, err
	}

	if err := conf.Scopes.Validate(); err != nil {
		return nil, err
	}

	if repoPath := FindGitRoot(path); repoPath != "" {
		conf.repoPath = repoPath
	}

	return &conf, nil
}

func LoadDefaultArchConf() (*ArchConf, error) {
	var conf ArchConf
	if err := yaml.Unmarshal([]byte(defaultArchYAML), &conf); err != nil {
		return nil, oops.Wrapf(err, "failed to unmarshal embedded defaults.yaml")
	}
	return &conf, nil
}

func (defaultConf ArchConf) Merge(userConf *ArchConf) ArchConf {
	if userConf == nil {
		return defaultConf
	}

	merged := ArchConf{
		Git:    userConf.Git,
		Build:  userConf.Build,
		Golang: userConf.Golang,
		Scopes: ScopesConfig{
			AllowedScopes: userConf.Scopes.AllowedScopes,
			Rules:         make(PathRules),
		},
	}

	merged.repoPath = lo.CoalesceOrEmpty(userConf.repoPath, defaultConf.repoPath)

	for scope, rules := range userConf.Scopes.Rules {
		merged.Scopes.Rules[scope] = rules
	}
	for scope, rules := range defaultConf.Scopes.Rules {
		if _, exists := merged.Scopes.Rules[scope]; !exists {
			merged.Scopes.Rules[scope] = rules
		} else {
			merged.Scopes.Rules[scope] = append(merged.Scopes.Rules[scope], rules...)
		}
	}

	return merged
}

func (conf *ArchConf) RepoPath() string {
	return conf.repoPath
}

func (conf *ArchConf) Exec() exec.WrapperFunc {
	return clicky.Exec("git").WithCwd(conf.repoPath).AsWrapper()
}

func (conf *ArchConf) FileExistsAtCommit(path string, commit string) bool {
	if conf.repoPath == "" || commit == "" {
		return false
	}
	git := conf.Exec()
	_, err := git("cat-file", "-e", fmt.Sprintf("%s:%s", commit, path))
	return err == nil
}

func (conf *ArchConf) ReadFile(path string, commit string) (string, error) {
	if conf.repoPath == "" {
		return "", fmt.Errorf("repository path not set")
	}
	if commit == "" {
		return "", fmt.Errorf("must specify a commit to read at")
	}
	git := conf.Exec()
	result, err := git("show", fmt.Sprintf("%s:%s", commit, path))
	if err != nil {
		return "", oops.Wrapf(err, "failed to read %s at commit %s", path, commit)
	}
	return result.Stdout, nil
}
