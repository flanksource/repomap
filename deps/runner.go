package deps

import (
	"context"
	"os"
	"os/exec"
)

type Command struct {
	Dir  string
	Name string
	Args []string
	Env  []string
}

type CommandResult struct {
	Stdout string
	Stderr string
}

type CommandRunner interface {
	Run(ctx context.Context, cmd Command) (CommandResult, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, spec Command) (CommandResult, error) {
	cmd := exec.CommandContext(ctx, spec.Name, spec.Args...)
	cmd.Dir = spec.Dir
	if len(spec.Env) > 0 {
		cmd.Env = append(os.Environ(), spec.Env...)
	}
	out, err := cmd.Output()
	result := CommandResult{Stdout: string(out)}
	if exit, ok := err.(*exec.ExitError); ok {
		result.Stderr = string(exit.Stderr)
	}
	return result, err
}
