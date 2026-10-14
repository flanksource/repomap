package gitstage

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type fileState struct {
	data   []byte
	mode   string
	exists bool
}

func readState(path string) (fileState, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return fileState{}, nil
	}
	if err != nil {
		return fileState{}, err
	}
	state := fileState{mode: "100644", exists: true}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		state.data, state.mode = []byte(target), "120000"
		return state, err
	}
	if !info.Mode().IsRegular() {
		return fileState{}, fmt.Errorf("target %s is not a regular file or symlink", path)
	}
	if info.Mode()&0o111 != 0 {
		state.mode = "100755"
	}
	state.data, err = os.ReadFile(path)
	return state, err
}

func (s fileState) equal(other fileState) bool {
	return s.exists == other.exists && s.mode == other.mode && bytes.Equal(s.data, other.data)
}

func (s *Session) tree(ctx context.Context, name string, after bool) (string, error) {
	index := filepath.Join(s.scratch, name+"-index")
	if _, err := runGit(ctx, s.Root, index, nil, "read-tree", "--empty"); err != nil {
		return "", err
	}
	paths := make([]string, 0, len(s.before))
	for path := range s.before {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		state := s.before[path]
		if after {
			var err error
			state, err = readState(filepath.Join(s.Root, filepath.FromSlash(path)))
			if err != nil {
				return "", err
			}
		}
		if !state.exists {
			continue
		}
		args := []string{"hash-object", "-w", "--stdin"}
		if state.mode != "120000" {
			args = append(args, "--path", path)
		}
		hash, err := runGit(ctx, s.Root, index, state.data, args...)
		if err != nil {
			return "", err
		}
		if _, err = runGit(ctx, s.Root, index, nil, "update-index", "--add", "--cacheinfo", state.mode+","+strings.TrimSpace(string(hash))+","+path); err != nil {
			return "", err
		}
	}
	tree, err := runGit(ctx, s.Root, index, nil, "write-tree")
	return strings.TrimSpace(string(tree)), err
}

func runGit(ctx context.Context, root, index string, input []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "color.ui=false", "-C", root}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	if index != "" {
		cmd.Env = append(cmd.Env, "GIT_INDEX_FILE="+index)
	}
	cmd.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s (cwd %s): %w: %s", strings.Join(args, " "), root, err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}
