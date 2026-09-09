package deps

import (
	"context"
	"fmt"
)

func applyDependencyUpdate(ctx context.Context, candidate UpdateCandidate, version string, opts UpdateOptions) UpdatePlan {
	if candidate.Manager == ManagerImage || candidate.Manager == ManagerHelm {
		return applyImageTargetUpdate(ctx, candidate, version, opts)
	}
	plan := planFromCandidate(candidate)
	plan.NewVersion = version
	if selectedVersionIsCurrent(candidate.Current, version) {
		plan.Skipped = "already at selected version"
		return plan
	}
	cmd, err := updateCommand(candidate, version)
	if err != nil {
		plan.Skipped = err.Error()
		return plan
	}
	plan.Command = append([]string{cmd.Name}, cmd.Args...)
	if opts.DryRun {
		plan.DryRun = true
		return plan
	}
	runner := opts.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	if _, err := runner.Run(ctx, cmd); err != nil {
		plan.Skipped = err.Error()
		return plan
	}
	plan.Written = true
	return plan
}

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
		return Command{Dir: dir, Name: "pnpm", Args: packageManagerArgs([]string{"add", "--lockfile-only", "--ignore-scripts"}, scope, targets)}, nil
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
