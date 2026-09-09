package deps

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
)

// goListModule is the subset of `go list -m -json` output version resolution
// needs. With -e a module that cannot be resolved reports its own Error instead
// of failing the whole invocation.
type goListModule struct {
	Path     string
	Versions []string
	Error    *struct{ Err string }
}

// newGoModuleVersionLookup resolves every module in one `go list` run the first
// time it is called, then serves the rest of the group from that result. The
// toolchain loads the module graph once per invocation, so one run for a
// directory costs far less than one run per dependency.
func newGoModuleVersionLookup(candidates []UpdateCandidate) func(context.Context, UpdateOptions, UpdateCandidate) ([]string, error) {
	if len(candidates) < 2 {
		return resolveCandidateRawVersions
	}
	modules := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		modules = append(modules, candidate.Name)
	}

	var (
		once      sync.Once
		versions  map[string][]string
		errsByMod map[string]error
	)
	return func(ctx context.Context, opts UpdateOptions, candidate UpdateCandidate) ([]string, error) {
		once.Do(func() {
			versions, errsByMod = goModuleVersionsBatch(ctx, opts.Runner, candidate.Dir, modules)
		})
		if err, ok := errsByMod[candidate.Name]; ok {
			return nil, err
		}
		if found, ok := versions[candidate.Name]; ok {
			return found, nil
		}
		// The batch did not report this module at all; resolve it on its own so
		// a surprising toolchain response degrades to today's behaviour rather
		// than silently dropping the dependency.
		return resolveCandidateRawVersions(ctx, opts, candidate)
	}
}

// goModuleVersionsBatch resolves several modules in one invocation. It returns
// the published versions per module path plus the per-module errors -e reports;
// a module missing from both maps was not mentioned by the toolchain and is the
// caller's cue to fall back.
func goModuleVersionsBatch(ctx context.Context, runner CommandRunner, dir string, modules []string) (map[string][]string, map[string]error) {
	if runner == nil {
		runner = ExecRunner{}
	}
	result, err := runner.Run(ctx, Command{
		Dir:  dir,
		Name: "go",
		Args: append([]string{"list", "-m", "-versions", "-json", "-e"}, modules...),
		Env:  []string{"GOFLAGS=-mod=readonly"},
	})
	if err != nil {
		// A process-level failure says nothing about individual modules, so
		// report nothing and let every module fall back.
		return nil, nil
	}

	versions := map[string][]string{}
	errsByMod := map[string]error{}
	decoder := json.NewDecoder(strings.NewReader(result.Stdout))
	for {
		var module goListModule
		if decodeErr := decoder.Decode(&module); decodeErr != nil {
			if errors.Is(decodeErr, io.EOF) {
				break
			}
			// Trailing garbage invalidates only what was not already decoded.
			break
		}
		if module.Path == "" {
			continue
		}
		if module.Error != nil && strings.TrimSpace(module.Error.Err) != "" {
			errsByMod[module.Path] = errors.New(strings.TrimSpace(module.Error.Err))
			continue
		}
		versions[module.Path] = sortDependencyVersions(module.Versions)
	}
	return versions, errsByMod
}
