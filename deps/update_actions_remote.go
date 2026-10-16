package deps

import (
	"context"
	"fmt"
	"sync"

	"github.com/flanksource/repomap/deps/internal/actions"
)

type actionTagEntry struct {
	once sync.Once
	tags actions.TagSet
	err  error
}

type actionVersionResolver struct {
	runner  CommandRunner
	mu      sync.Mutex
	entries map[string]*actionTagEntry
}

func newActionVersionResolver(runner CommandRunner) *actionVersionResolver {
	return &actionVersionResolver{runner: runner, entries: map[string]*actionTagEntry{}}
}

func (r *actionVersionResolver) Load(ctx context.Context, repository string) (actions.TagSet, error) {
	r.mu.Lock()
	entry := r.entries[repository]
	if entry == nil {
		entry = &actionTagEntry{}
		r.entries[repository] = entry
	}
	r.mu.Unlock()
	entry.once.Do(func() {
		remote := "https://github.com/" + repository + ".git"
		cmd := Command{Name: "git", Args: []string{"ls-remote", "--tags", remote}, Env: []string{"GIT_TERMINAL_PROMPT=0"}}
		result, err := r.runner.Run(ctx, cmd)
		if err != nil {
			entry.err = fmt.Errorf("list action tags for %s: %s", repository, commandFailure(cmd, result, err))
			return
		}
		entry.tags, entry.err = actions.ParseTags(result.Stdout)
		if entry.err != nil {
			entry.err = fmt.Errorf("parse action tags for %s: %w", repository, entry.err)
		}
	})
	return entry.tags, entry.err
}
