// Package pnpm warms the pnpm content-addressable store for a single package.
package pnpm

import (
	"fmt"

	"github.com/Masterminds/semver/v3"
	"github.com/flanksource/repomap/deps/manager/node"
	"github.com/flanksource/repomap/deps/manifest"
)

// allowAllBuilds opts every dependency into running its lifecycle scripts. pnpm
// 10.6 stopped honouring them by default — dropping --ignore-scripts is no longer
// enough — and older pnpm does not recognise the flag, hence the version gate.
const allowAllBuilds = "--config.dangerouslyAllowAllBuilds=true"

const (
	allowAllBuildsMajor = 10
	allowAllBuildsMinor = 6
)

type Warmer struct{}

func (Warmer) Manager() manifest.Manager { return manifest.ManagerPNPM }

func (Warmer) Binary() string { return "pnpm" }

// Probe reports the pnpm version, which decides how --build has to ask for
// lifecycle scripts. Dir is left for the orchestrator to fill in.
func (Warmer) Probe() *manifest.Command {
	return &manifest.Command{Name: "pnpm", Args: []string{"--version"}}
}

func (Warmer) Steps(req manifest.WarmRequest, probe string) ([]manifest.Step, error) {
	cmds := node.Commands{
		Binary:        "pnpm",
		Install:       []string{"install"},
		IgnoreScripts: "--ignore-scripts",
		Offline:       []string{"install", "--offline", "--frozen-lockfile"},
	}
	// The probe only matters for building; a plain warm must not depend on it.
	if req.Build {
		buildArgs, err := buildArgsFor(probe)
		if err != nil {
			return nil, err
		}
		cmds.BuildArgs = buildArgs
	}
	return node.Steps(req, cmds)
}

func buildArgsFor(probe string) ([]string, error) {
	if probe == "" {
		return nil, fmt.Errorf("--build needs the pnpm version to decide how to enable dependency builds, but `pnpm --version` reported nothing")
	}
	version, err := semver.NewVersion(probe)
	if err != nil {
		return nil, fmt.Errorf("--build needs the pnpm version to decide how to enable dependency builds, but `pnpm --version` reported %q: %w", probe, err)
	}
	if version.Major() > allowAllBuildsMajor ||
		(version.Major() == allowAllBuildsMajor && version.Minor() >= allowAllBuildsMinor) {
		return []string{allowAllBuilds}, nil
	}
	// Before 10.6, dropping --ignore-scripts is enough on its own.
	return nil, nil
}
