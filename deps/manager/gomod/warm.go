// Package gomod warms the Go module and build caches for a single module.
//
// It is named gomod rather than go because a directory named "go" reads badly at
// import sites.
package gomod

import (
	"fmt"
	"strings"

	"golang.org/x/mod/module"

	"github.com/flanksource/repomap/deps/manifest"
)

// ModulePath is the synthetic module the scratch project declares. It is never
// published, but it must be a valid module path for `go mod init` to accept it.
const ModulePath = "repomap.local/cachewarm"

// DefaultHost is prepended to a spec whose first path element carries no dot,
// which is what a bare owner/repo shorthand looks like.
const DefaultHost = "github.com"

// schemes are stripped from a spec typed as a URL. Only the scheme goes: the
// rest of a repository URL already reads as a module path.
var schemes = []string{"https://", "http://", "ssh://", "git://"}

// Warmer drives the go toolchain against a throwaway module that requires the
// target, populating GOMODCACHE and — with Build — GOCACHE.
type Warmer struct{}

func (Warmer) Manager() manifest.Manager { return manifest.ManagerGo }

func (Warmer) Binary() string { return "go" }

// Probe returns nil: the go commands used here have been stable for many
// releases, so no runtime version check is needed.
func (Warmer) Probe() *manifest.Command { return nil }

// NormalizeSpec turns what a user actually has to hand — a GitHub slug, a
// browser URL, a clone URL — into the canonical module path `go get` demands,
// and rejects anything that still is not one. Without it the only diagnostic is
// the go toolchain's own "malformed module path", which never says what repomap
// wanted.
//
// A repository URL is not always a module path (vanity domains, modules nested
// in a monorepo), so this canonicalises the common GitHub shape rather than
// claiming to resolve every repository.
func (Warmer) NormalizeSpec(spec string) (string, error) {
	original := strings.TrimSpace(spec)
	trimmed := original
	for _, scheme := range schemes {
		trimmed = strings.TrimPrefix(trimmed, scheme)
	}

	name, version, err := manifest.SplitSpec(trimGitURL(trimmed))
	if err != nil {
		return "", err
	}
	name = strings.TrimSuffix(strings.TrimSuffix(name, "/"), ".git")

	// A dotless first element is a bare owner/repo shorthand: no module path can
	// start without a host, and every host has a dot. A single element is left
	// alone so it fails below rather than becoming github.com/<something>.
	if head, _, ok := strings.Cut(name, "/"); ok && !strings.Contains(head, ".") {
		name = DefaultHost + "/" + name
	}
	if err := module.CheckPath(name); err != nil {
		return "", fmt.Errorf("%q is not a Go module path (expected github.com/owner/repo or owner/repo): %w", original, err)
	}
	return name + "@" + version, nil
}

// trimGitURL rewrites the two clone-URL shapes a module path cannot express:
// scp syntax (git@host:owner/repo) and a leftover userinfo prefix
// (git@host/owner/repo, what stripping ssh:// leaves behind). Both must go
// before the name@version split, which would otherwise read git@ as the version
// separator. A spec with no slash is never a clone URL, so name@version is left
// intact.
func trimGitURL(spec string) string {
	head, path, ok := strings.Cut(spec, "/")
	at := strings.Index(head, "@")
	if !ok || at < 0 {
		return spec
	}
	// In scp syntax the colon plays the role of the first slash.
	return strings.Replace(head[at+1:], ":", "/", 1) + "/" + path
}

func (Warmer) Steps(req manifest.WarmRequest, _ string) ([]manifest.Step, error) {
	switch {
	case req.Dir == "":
		return nil, fmt.Errorf("go warming needs a scratch directory")
	case req.Name == "":
		return nil, fmt.Errorf("go warming needs a module path")
	case req.Version == "":
		return nil, fmt.Errorf("go warming needs a version for %s", req.Name)
	}

	// The /... package pattern, rather than the bare module, is what records the
	// go.sum entries needed to *build* every package in the module. Resolving the
	// module alone records only enough to reference it, which leaves a later
	// offline build short of its dependencies.
	packages := req.Name + "/..."

	steps := []manifest.Step{
		goStep("init", req, false, "mod", "init", ModulePath),
		goStep("resolve", req, false, "get", packages+"@"+req.Version),
		// The synthetic module imports nothing, so a bare `go mod download` would
		// have no packages to work from. The `all` pattern materialises zips for
		// the whole resolved graph, which is what makes a later build offline-able.
		goStep("download", req, false, "mod", "download", "all"),
	}
	if req.Build {
		steps = append(steps, goStep("build", req, false, "build", packages))
	}
	if req.Verify {
		steps = append(steps, verifyStep(req, packages))
	}
	return steps, nil
}

// verifyStep replays the most demanding work already done, with the proxy
// disabled so any cache miss is a hard failure rather than a silent refetch.
// With Build there are compiled packages to reproduce; without it, the strongest
// available claim is that every module zip is already resident.
func verifyStep(req manifest.WarmRequest, packages string) manifest.Step {
	if req.Build {
		return goStep("verify", req, true, "build", packages)
	}
	return goStep("verify", req, true, "mod", "download", "all")
}

func goStep(name string, req manifest.WarmRequest, offline bool, args ...string) manifest.Step {
	return manifest.Step{
		Kind: manifest.StepExec,
		Name: name,
		Command: manifest.Command{
			Dir:  req.Dir,
			Name: "go",
			Args: args,
			Env:  goEnv(offline),
		},
	}
}

// goEnv pins the module mode explicitly. GOWORK=off matters most: without it a
// scratch dir that happens to sit inside a go.work tree silently joins that
// workspace and warms the wrong module graph.
func goEnv(offline bool) []string {
	env := []string{"GOWORK=off", "GOFLAGS=-mod=mod"}
	if offline {
		env = append(env, "GOPROXY=off")
	}
	return env
}
