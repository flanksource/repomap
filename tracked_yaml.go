package repomap

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/flanksource/repomap/kubernetes"
)

// TrackedYAMLContents returns every git-tracked YAML file in the repo keyed by
// its repo-relative POSIX path. Empty or unreadable files are skipped. It is the
// shared first pass for image/chart discovery (imageupdate.DiscoverTargets),
// used by both `repomap deps update` and `repomap images list`.
func (conf *ArchConf) TrackedYAMLContents() (map[string]string, error) {
	result, err := conf.Exec()("ls-files")
	if err != nil {
		return nil, fmt.Errorf("git ls-files failed: %w", err)
	}
	contents := map[string]string{}
	for _, line := range strings.Split(result.Stdout, "\n") {
		file := filepath.ToSlash(strings.TrimSpace(line))
		if file == "" || !kubernetes.IsYaml(file) {
			continue
		}
		content, err := conf.ReadFileWithFallback(file, "")
		if err != nil || content == "" {
			continue
		}
		contents[file] = content
	}
	return contents, nil
}
