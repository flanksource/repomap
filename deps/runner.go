package deps

import "github.com/flanksource/repomap/deps/manifest"

// The process execution seam lives in deps/manifest so the per-manager packages
// under deps/manager can declare commands without importing deps. These aliases
// keep it spelled deps.Command / deps.CommandRunner for every existing caller.
type (
	Command       = manifest.Command
	CommandResult = manifest.CommandResult
	CommandRunner = manifest.CommandRunner
	ExecRunner    = manifest.ExecRunner
)
