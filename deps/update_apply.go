package deps

import (
	"fmt"

	"github.com/flanksource/repomap/deps/internal/pnpm"
)

func updateCommand(candidate UpdateCandidate, version string) (Command, error) {
	return updateBatchCommand(candidate.Manager, candidate.Dir, candidate.Scope, []string{candidate.Name + "@" + version})
}

// updateBatchCommand builds the single command that updates every target in one
// manifest. Package managers accept several name@version arguments per
// invocation, and one run costs far less than one per dependency.
func updateBatchCommand(manager Manager, dir, scope string, targets []string) (Command, error) {
	switch manager {
	case ManagerGo:
		return Command{Dir: dir, Name: "go", Args: append([]string{"get"}, targets...)}, nil
	case ManagerNPM:
		return Command{Dir: dir, Name: "npm", Args: packageManagerArgs([]string{"install", "--package-lock-only", "--ignore-scripts"}, scope, targets)}, nil
	case ManagerPNPM:
		workspace, err := pnpm.Find(dir)
		if err != nil {
			return Command{}, err
		}
		args := []string{"add", "--lockfile-only", "--ignore-scripts"}
		if workspace != nil && workspace.Root == dir {
			args = append(args, "--workspace-root")
		}
		return Command{Dir: dir, Name: "pnpm", Args: packageManagerArgs(args, scope, targets)}, nil
	default:
		return Command{}, fmt.Errorf("package-manager updates do not support manager %q", manager)
	}
}

func packageManagerArgs(args []string, scope string, targets []string) []string {
	if flag := packageSaveFlag(scope); flag != "" {
		args = append(args, flag)
	}
	return append(args, targets...)
}

func packageSaveFlag(scope string) string {
	switch scope {
	case "dependencies":
		return "--save-prod"
	case "devDependencies":
		return "--save-dev"
	case "optionalDependencies":
		return "--save-optional"
	case "peerDependencies":
		return "--save-peer"
	default:
		return ""
	}
}
