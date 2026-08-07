// Package gomod warms the Go module and build caches for a single module.
//
// It is named gomod rather than go because a directory named "go" reads badly at
// import sites.
package gomod

import (
	"fmt"

	"github.com/flanksource/repomap/deps/manifest"
)

// ModulePath is the synthetic module the scratch project declares. It is never
// published, but it must be a valid module path for `go mod init` to accept it.
const ModulePath = "repomap.local/cachewarm"

// Warmer drives the go toolchain against a throwaway module that requires the
// target, populating GOMODCACHE and — with Build — GOCACHE.
type Warmer struct{}

func (Warmer) Manager() manifest.Manager { return manifest.ManagerGo }

func (Warmer) Binary() string { return "go" }

// Probe returns nil: the go commands used here have been stable for many
// releases, so no runtime version check is needed.
func (Warmer) Probe() *manifest.Command { return nil }

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
