// Package npm warms the npm cache for a single package.
package npm

import (
	"github.com/flanksource/repomap/deps/manager/node"
	"github.com/flanksource/repomap/deps/manifest"
)

type Warmer struct{}

func (Warmer) Manager() manifest.Manager { return manifest.ManagerNPM }

func (Warmer) Binary() string { return "npm" }

// Probe returns nil: npm has kept its lifecycle-script default, so no runtime
// version check is needed.
func (Warmer) Probe() *manifest.Command { return nil }

func (Warmer) Steps(req manifest.WarmRequest, _ string) ([]manifest.Step, error) {
	return node.Steps(req, node.Commands{
		Binary:        "npm",
		Install:       []string{"install"},
		IgnoreScripts: "--ignore-scripts",
		// ci rather than install: the lockfile the download step wrote makes it the
		// stricter replay, and it refuses to reach the network for anything missing.
		Offline: []string{"ci", "--offline"},
	})
}
