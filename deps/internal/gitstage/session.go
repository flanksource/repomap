package gitstage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type Session struct {
	Root     string
	index    string
	original []byte
	scratch  string
	before   map[string]fileState
}

func Begin(ctx context.Context, dir string, paths []string) (*Session, error) {
	root, err := runGit(ctx, dir, "", nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	s := &Session{Root: strings.TrimSpace(string(root)), before: map[string]fileState{}}
	index, err := runGit(ctx, s.Root, "", nil, "rev-parse", "--path-format=absolute", "--git-path", "index")
	if err != nil {
		return nil, err
	}
	s.index = strings.TrimSpace(string(index))
	s.original, err = os.ReadFile(s.index)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err := s.snapshot(ctx, dir, paths); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(s.Root, ".tmp"), 0o755); err != nil {
		return nil, err
	}
	s.scratch, err = os.MkdirTemp(filepath.Join(s.Root, ".tmp"), "repomap-stage-")
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Session) snapshot(ctx context.Context, dir string, paths []string) error {
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		rel, err := filepath.Rel(s.Root, path)
		if err != nil {
			return err
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("target %s is outside repository %s", path, s.Root)
		}
		rel = filepath.ToSlash(rel)
		state, err := readState(path)
		if err != nil {
			return err
		}
		entries, err := runGit(ctx, s.Root, "", nil, "ls-files", "--stage", "-z", "--", rel)
		if err != nil {
			return err
		}
		for _, entry := range bytes.Split(entries, []byte{0}) {
			if len(entry) == 0 {
				continue
			}
			parts := strings.SplitN(string(entry), "\t", 2)
			fields := strings.Fields(parts[0])
			if len(fields) != 3 || fields[2] != "0" {
				return fmt.Errorf("target %s is unmerged", rel)
			}
		}
		if state.exists && len(entries) == 0 {
			return fmt.Errorf("target %s is untracked; cannot isolate updater changes", rel)
		}
		s.before[rel] = state
	}
	return nil
}

func (s *Session) Changed() ([]string, error) {
	var paths []string
	for path, before := range s.before {
		after, err := readState(filepath.Join(s.Root, filepath.FromSlash(path)))
		if err != nil {
			return nil, err
		}
		if !before.equal(after) {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	return paths, nil
}

func (s *Session) Stage(ctx context.Context) ([]string, error) {
	changed, err := s.Changed()
	if err != nil || len(changed) == 0 {
		return changed, err
	}
	before, err := s.tree(ctx, "before", false)
	if err != nil {
		return nil, err
	}
	after, err := s.tree(ctx, "after", true)
	if err != nil {
		return nil, err
	}
	patch, err := runGit(ctx, s.Root, "", nil, "diff-tree", "--no-ext-diff", "--no-textconv", "-p", "--binary", "--full-index", "--no-renames", before, after)
	if err != nil {
		return nil, err
	}
	candidate := filepath.Join(s.scratch, "candidate-index")
	if len(s.original) > 0 {
		err = os.WriteFile(candidate, s.original, 0o600)
	} else {
		_, err = runGit(ctx, s.Root, candidate, nil, "read-tree", "--empty")
	}
	if err != nil {
		return nil, err
	}
	if _, err = runGit(ctx, s.Root, candidate, patch, "apply", "--cached", "--3way", "--whitespace=nowarn", "-"); err != nil {
		return nil, fmt.Errorf("cannot stage updater changes; original index preserved: %w", err)
	}
	if err = s.publish(candidate); err != nil {
		return nil, err
	}
	return changed, nil
}

func (s *Session) publish(candidate string) (err error) {
	lock, err := os.OpenFile(s.index+".lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("lock index: %w", err)
	}
	defer func() {
		if removeErr := os.Remove(s.index + ".lock"); removeErr != nil && !os.IsNotExist(removeErr) {
			err = errors.Join(err, fmt.Errorf("remove index lock: %w", removeErr))
		}
	}()
	current, readErr := os.ReadFile(s.index)
	if readErr != nil && !os.IsNotExist(readErr) {
		_ = lock.Close()
		return readErr
	}
	if !bytes.Equal(current, s.original) {
		_ = lock.Close()
		return fmt.Errorf("index changed during dependency update; original index preserved")
	}
	data, err := os.ReadFile(candidate)
	if err == nil {
		_, err = lock.Write(data)
	}
	if err == nil {
		err = lock.Sync()
	}
	closeErr := lock.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(s.index+".lock", s.index)
}

func (s *Session) Close() error { return os.RemoveAll(s.scratch) }
