package deps

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/flanksource/clicky/task"
	flanksourceContext "github.com/flanksource/commons/context"
	"github.com/flanksource/repomap/deps/manager/gomod"
	"github.com/flanksource/repomap/deps/manager/npm"
	"github.com/flanksource/repomap/deps/manager/pnpm"
	"github.com/flanksource/repomap/deps/manifest"
)

// cacheWarmConcurrency bounds how many specs warm at once. GOMODCACHE and the
// pnpm store are both lock-protected against concurrent writers, so the limit is
// about not saturating the network rather than correctness.
const cacheWarmConcurrency = 4

// warmers is the single place an ecosystem is wired into cache warming.
var warmers = map[Manager]manifest.Warmer{
	ManagerGo:   gomod.Warmer{},
	ManagerNPM:  npm.Warmer{},
	ManagerPNPM: pnpm.Warmer{},
}

type WarmOptions struct {
	Manager Manager
	Specs   []string
	// Build compiles every package of the target (Go) or lets dependency
	// lifecycle scripts run so native addons are built (npm, pnpm).
	Build bool
	// Verify replays the work with the network disabled, turning "a download ran"
	// into "this cache can build offline".
	Verify bool
	Runner CommandRunner
}

type WarmStep struct {
	Name     string        `json:"name"`
	Command  string        `json:"command"`
	Duration time.Duration `json:"duration"`
	Error    string        `json:"error,omitempty"`
}

type WarmResult struct {
	Manager Manager `json:"manager"`
	// Spec is the requested name@version; Version is what the manager resolved.
	Spec     string     `json:"spec"`
	Name     string     `json:"name"`
	Version  string     `json:"version,omitempty"`
	Packages int        `json:"packages,omitempty"`
	Cache    string     `json:"cache,omitempty"`
	Built    bool       `json:"built,omitempty"`
	Verified bool       `json:"verified,omitempty"`
	Steps    []WarmStep `json:"steps,omitempty"`
	Error    string     `json:"error,omitempty"`
	// SummaryError records a failure to read back what was warmed. The cache is
	// still warm when this is set, so it does not fail the spec — but it is
	// reported rather than swallowed.
	SummaryError string `json:"summary_error,omitempty"`
}

func (r WarmResult) failed() bool { return r.Error != "" }

// WarmCache downloads each spec's full dependency closure into the machine's
// shared package cache, using a throwaway project per spec so nothing in the
// user's working tree is touched.
func WarmCache(ctx context.Context, opts WarmOptions) ([]WarmResult, error) {
	warmer, ok := warmers[opts.Manager]
	if !ok {
		return nil, fmt.Errorf("cache warming does not support manager %q (expected go, npm, or pnpm)", opts.Manager)
	}
	if len(opts.Specs) == 0 {
		return nil, fmt.Errorf("cache warming needs at least one name@version spec")
	}
	runner := opts.Runner
	if runner == nil {
		runner = ExecRunner{}
		// Only preflight when we are really going to exec: an injected runner is a
		// test double or a recorder, and need not correspond to a binary on PATH.
		if _, err := exec.LookPath(warmer.Binary()); err != nil {
			return nil, fmt.Errorf("%s not found on PATH, which is required to warm %s caches: %w", warmer.Binary(), opts.Manager, err)
		}
	}

	results := make([]WarmResult, len(opts.Specs))
	group := task.StartGroup[int]("Warming package caches", task.WithConcurrency(cacheWarmConcurrency))
	for i, spec := range opts.Specs {
		idx, spec := i, spec
		group.Add(fmt.Sprintf("%s %s", opts.Manager, spec), func(_ flanksourceContext.Context, tk *task.Task) (int, error) {
			results[idx] = warmSpec(ctx, warmer, runner, spec, opts, tk)
			switch {
			case results[idx].failed():
				tk.Errorf("%s", results[idx].Error)
				tk.Failed()
			case results[idx].SummaryError != "":
				tk.Warnf("%s", results[idx].SummaryError)
				tk.Warning()
			default:
				tk.Success()
			}
			return idx, nil
		})
	}
	_, _ = group.GetResults()

	// Each failure carries its own command and the tool's stderr. They are folded
	// into the returned error rather than left on the results, because the scratch
	// directory is already gone and a caller that only prints the error would
	// otherwise have nothing to reproduce from.
	var failed []string
	for _, result := range results {
		if result.failed() {
			failed = append(failed, fmt.Sprintf("%s: %s", result.Spec, result.Error))
		}
	}
	if len(failed) > 0 {
		return results, fmt.Errorf("failed to warm %d of %d specs:\n  %s", len(failed), len(results), strings.Join(failed, "\n  "))
	}
	return results, nil
}

func warmSpec(ctx context.Context, warmer manifest.Warmer, runner CommandRunner, spec string, opts WarmOptions, tk *task.Task) WarmResult {
	result := WarmResult{Manager: warmer.Manager(), Spec: spec}
	name, version, err := parseWarmSpec(spec)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Name = name

	dir, err := os.MkdirTemp("", "repomap-cache-warm-*")
	if err != nil {
		result.Error = err.Error()
		return result
	}
	// The scratch project is disposable: the durable result is what landed in the
	// module cache or package store.
	defer func() { _ = os.RemoveAll(dir) }()

	probe, err := runProbe(ctx, warmer, runner, dir)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	steps, err := warmer.Steps(manifest.WarmRequest{
		Dir:     dir,
		Name:    name,
		Version: version,
		Build:   opts.Build,
		Verify:  opts.Verify,
	}, probe)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	tk.SetProgress(0, len(steps))
	for i, step := range steps {
		tk.Infof("%s", step.Name)
		started := time.Now()
		err := runWarmStep(ctx, runner, dir, step)
		record := WarmStep{Name: step.Name, Command: step.Detail(), Duration: time.Since(started)}
		if err != nil {
			record.Error = err.Error()
			result.Steps = append(result.Steps, record)
			result.Error = err.Error()
			return result
		}
		result.Steps = append(result.Steps, record)
		tk.SetProgress(i+1, len(steps))
	}

	result.Built = opts.Build
	result.Verified = opts.Verify
	summarizeWarm(ctx, &result, dir, runner)
	return result
}

// runProbe executes a manager's optional version probe inside the scratch dir and
// returns its trimmed stdout, which Steps consumes as an ordinary input.
func runProbe(ctx context.Context, warmer manifest.Warmer, runner CommandRunner, dir string) (string, error) {
	cmd := warmer.Probe()
	if cmd == nil {
		return "", nil
	}
	cmd.Dir = dir
	result, err := runner.Run(ctx, *cmd)
	if err != nil {
		return "", commandError(*cmd, result, err)
	}
	return strings.TrimSpace(result.Stdout), nil
}

func runWarmStep(ctx context.Context, runner CommandRunner, dir string, step manifest.Step) error {
	switch step.Kind {
	case manifest.StepWrite:
		return os.WriteFile(filepath.Join(dir, step.Path), step.Content, 0o600)
	case manifest.StepRemove:
		return os.RemoveAll(filepath.Join(dir, step.Path))
	case manifest.StepExec:
		result, err := runner.Run(ctx, step.Command)
		if err != nil {
			return commandError(step.Command, result, err)
		}
		return nil
	default:
		return fmt.Errorf("unknown warm step kind %q for step %q", step.Kind, step.Name)
	}
}

// commandError names the exact invocation and the tool's own diagnostics. The
// scratch dir is gone by the time a caller sees this, so the message is the only
// reproduction handle there is.
func commandError(cmd manifest.Command, result CommandResult, err error) error {
	if stderr := strings.TrimSpace(result.Stderr); stderr != "" {
		return fmt.Errorf("%s: %w: %s", cmd.String(), err, stderr)
	}
	return fmt.Errorf("%s: %w", cmd.String(), err)
}

// summarizeWarm reports what actually landed by reading the warmed scratch
// project back through repomap's own manifest resolvers, rather than re-parsing
// go.mod or a lockfile here. The manager did the resolving, so this is also where
// the concrete version (including a Go pseudo-version) is discovered.
//
// It calls discoverOffline/resolveManifest rather than Scan deliberately: Scan
// renders its own task group, which would surface read-back warnings about a
// temporary directory the user never asked about.
func summarizeWarm(ctx context.Context, result *WarmResult, dir string, runner CommandRunner) {
	result.Cache = warmCachePath(ctx, result.Manager, runner, dir)

	projects, _, err := discoverOffline(dir, []Manager{result.Manager})
	if err != nil {
		result.SummaryError = fmt.Sprintf("could not read back the warmed project: %s", err)
		return
	}
	if len(projects) == 0 {
		result.SummaryError = fmt.Sprintf("no %s manifest was produced in the scratch project", result.Manager)
		return
	}
	// MaxDepth 1 keeps the read-back offline — it parses the manifest and lockfile
	// the warm just wrote instead of shelling out again.
	root, _, err := resolveManifest(ctx, projects[0], Options{MaxDepth: 1, IncludeIndirect: true})
	if err != nil {
		result.SummaryError = fmt.Sprintf("could not read back the warmed project: %s", err)
		return
	}
	result.Packages, result.Version = walkWarmed(root, result.Name)
}

// walkWarmed counts every resolved dependency below root and finds the target's
// version. It walks rather than reading root.Children directly because pnpm nests
// dependencies under a synthetic importer node while go.mod lists them flat.
//
// Only nodes carrying a version are counted, which is what distinguishes a real
// package from pnpm's importer (and from the project root itself).
func walkWarmed(root *Node, name string) (count int, version string) {
	seen := map[*Node]bool{}
	stack := []*Node{root}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if node == nil || seen[node] {
			continue
		}
		seen[node] = true
		for _, child := range node.Children {
			if child.Version != "" {
				count++
				if version == "" && child.Name == name {
					version = child.Version
				}
			}
			stack = append(stack, child)
		}
	}
	return count, version
}

// warmCachePath asks the manager where its cache lives, so the output names the
// directory that grew. A failure here is not worth reporting: it costs only the
// display of a path.
func warmCachePath(ctx context.Context, manager Manager, runner CommandRunner, dir string) string {
	var cmd manifest.Command
	switch manager {
	case ManagerGo:
		cmd = manifest.Command{Dir: dir, Name: "go", Args: []string{"env", "GOMODCACHE"}}
	case ManagerPNPM:
		cmd = manifest.Command{Dir: dir, Name: "pnpm", Args: []string{"store", "path"}}
	case ManagerNPM:
		cmd = manifest.Command{Dir: dir, Name: "npm", Args: []string{"config", "get", "cache"}}
	default:
		return ""
	}
	result, err := runner.Run(ctx, cmd)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(result.Stdout)
}

// parseWarmSpec splits "name@version". The split uses the last @ at a non-zero
// index so a scoped npm name such as @scope/pkg keeps its leading @. An omitted
// version becomes "latest" and the manager decides what that means; the concrete
// version is read back after warming.
func parseWarmSpec(spec string) (name, version string, err error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", "", fmt.Errorf("empty dependency spec: expected name@version")
	}
	at := strings.LastIndex(spec, "@")
	if at <= 0 {
		if spec == "@" {
			return "", "", fmt.Errorf("invalid dependency spec %q: expected name@version", spec)
		}
		return spec, "latest", nil
	}
	name, version = spec[:at], spec[at+1:]
	if name == "" || version == "" {
		return "", "", fmt.Errorf("invalid dependency spec %q: expected name@version", spec)
	}
	return name, version, nil
}
