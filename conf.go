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
	Git      GitConfig         `json:"git,omitempty" yaml:"git,omitempty"`
	Build    BuildConfig       `json:"build,omitempty" yaml:"build,omitempty"`
	Golang   GolangConfig      `json:"golang,omitempty" yaml:"golang,omitempty"`
	Scopes   ScopesConfig      `json:"scopes,omitempty" yaml:"scopes,omitempty"`
	Severity SeverityConfig    `json:"severity,omitempty" yaml:"severity,omitempty"`
	Extends  []string          `json:"extends,omitempty" yaml:"extends,omitempty"`
	Presets  map[string]Preset `json:"presets,omitempty" yaml:"presets,omitempty"`
	Exclude  ExcludeConfig     `json:"exclude,omitempty" yaml:"exclude,omitempty"`
	repoPath string            `json:"-" yaml:"-"`
}

func (conf *ArchConf) GetFileMap(path string, commit string) (*FileMap, error) {
	f := FileMap{
		Path:     path,
		Language: DetectLanguage(path),
	}

	pathScopes, pathMatches := conf.Scopes.GetScopesByPath(path)
	f.Scopes = pathScopes
	f.ScopeMatches = pathMatches

	if kubernetes.IsYaml(path) {
		content, err := conf.ReadFileWithFallback(path, commit)
		if err != nil {
			logger.Errorf("Error reading file path=%s commit=%s: %v", path, commit, err)
			return &f, nil
		}
		if content != "" {
			f.KubernetesRefs, err = kubernetes.ExtractKubernetesRefsFromContent(content)
			if err != nil {
				logger.Errorf("Error extracting k8s refs path=%s commit=%s: %v", path, commit, err)
			}
			refScopes, refMatches := conf.Scopes.GetScopesByRefs(f.KubernetesRefs)
			f.Scopes = f.Scopes.Merge(refScopes)
			f.ScopeMatches = append(f.ScopeMatches, refMatches...)
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

	conf, err := defaultConf.Merge(userConf)
	if err != nil {
		return nil, oops.Wrapf(err, "failed to merge configs")
	}

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

	conf, err := defaultConf.Merge(userConf)
	if err != nil {
		return nil, oops.Wrapf(err, "failed to merge configs")
	}

	repoPath := FindGitRoot(path)
	if repoPath == "" {
		return nil, fmt.Errorf("failed to find git repository root for path: %s", path)
	}
	conf.repoPath = repoPath

	return &conf, nil
}

var ConfigAliases = []string{
	"repomap.yaml", "repomap.yml",
	".gavel.yaml", ".gavel.yml",
	".gitanalyze.yaml", ".gitanalyze.yml",
	"arch.yaml", "arch.yml",
	".arch-unit.yaml", ".arch-unit.yml",
}

func GetConfForFile(path string) (*ArchConf, error) {
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		path = filepath.Dir(path)
	}

	path, _ = filepath.Abs(path)

	for _, alias := range ConfigAliases {
		file := filepath.Join(path, alias)
		if stat, err := os.Stat(file); err == nil && !stat.IsDir() {
			return LoadArchConf(file)
		}
	}

	if IsGitRoot(path) {
		return nil, nil
	}
	parent := filepath.Dir(path)
	if parent == path {
		return nil, nil
	}
	return GetConfForFile(parent)
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
	if err := conf.Scopes.Validate(); err != nil {
		return nil, oops.Wrapf(err, "failed to validate embedded defaults")
	}
	return &conf, nil
}

func (defaultConf ArchConf) Merge(userConf *ArchConf) (ArchConf, error) {
	if userConf == nil {
		return defaultConf, nil
	}

	merged := ArchConf{
		Git:    userConf.Git,
		Build:  userConf.Build,
		Golang: userConf.Golang,
		Scopes: ScopesConfig{
			AllowedScopes: userConf.Scopes.AllowedScopes,
			Rules:         make(PathRules),
		},
		Extends: userConf.Extends,
		Exclude: defaultConf.Exclude.Merge(userConf.Exclude),
	}

	merged.repoPath = lo.CoalesceOrEmpty(userConf.repoPath, defaultConf.repoPath)

	// Merge presets: default presets first, user overrides
	merged.Presets = make(map[string]Preset)
	for name, preset := range defaultConf.Presets {
		merged.Presets[name] = preset
	}
	for name, preset := range userConf.Presets {
		merged.Presets[name] = preset
	}

	// If user didn't specify extends, use defaults
	if len(merged.Extends) == 0 {
		merged.Extends = defaultConf.Extends
	}

	// Resolve presets into exclude config
	merged.Exclude.ResolvePresets(merged.Extends, merged.Presets)

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

	if err := merged.Scopes.Validate(); err != nil {
		return merged, err
	}

	return merged, nil
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

func (conf *ArchConf) ReadFileWithFallback(path string, commit string) (string, error) {
	if conf.FileExistsAtCommit(path, commit) {
		return conf.ReadFile(path, commit)
	}
	if conf.repoPath != "" {
		data, err := os.ReadFile(filepath.Join(conf.repoPath, path))
		if err != nil {
			return "", nil
		}
		return string(data), nil
	}
	return "", nil
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
