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
	stageUpdatePlan(ctx, &plan, candidate, runner)
	return plan
}

func stageUpdatePlan(ctx context.Context, plan *UpdatePlan, candidate UpdateCandidate, runner CommandRunner) {
	staged, err := stageUpdatedFiles(ctx, runner, candidate)
	plan.Staged = staged
	if err != nil {
		plan.StageError = err.Error()
	}
}

func updateCommand(candidate UpdateCandidate, version string) (Command, error) {
	target := candidate.Name + "@" + version
	switch candidate.Manager {
	case ManagerGo:
		return Command{Dir: candidate.Dir, Name: "go", Args: []string{"get", target}}, nil
	case ManagerNPM:
		args := []string{"install", "--package-lock-only", "--ignore-scripts"}
		if flag := packageSaveFlag(candidate.Scope); flag != "" {
			args = append(args, flag)
		}
		args = append(args, target)
		return Command{Dir: candidate.Dir, Name: "npm", Args: args}, nil
	case ManagerPNPM:
		args := []string{"add", "--lockfile-only", "--ignore-scripts"}
		if flag := packageSaveFlag(candidate.Scope); flag != "" {
			args = append(args, flag)
		}
		args = append(args, target)
		return Command{Dir: candidate.Dir, Name: "pnpm", Args: args}, nil
	default:
		return Command{}, fmt.Errorf("package-manager updates do not support manager %q", candidate.Manager)
	}
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
