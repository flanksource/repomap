package repomap

import (
	"fmt"

	"github.com/google/cel-go/cel"
)

type CompiledExcludeConfig struct {
	ExcludeConfig
	compiledRules []cel.Program
}

func (e *ExcludeConfig) Compile() (*CompiledExcludeConfig, error) {
	compiled := &CompiledExcludeConfig{ExcludeConfig: *e}
	if len(e.Rules) == 0 {
		return compiled, nil
	}

	env, err := newExcludeCELEnv()
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}

	for _, rule := range e.Rules {
		prog, err := compileExcludeCEL(env, rule.When)
		if err != nil {
			return nil, fmt.Errorf("failed to compile exclude rule '%s': %w", rule.When, err)
		}
		compiled.compiledRules = append(compiled.compiledRules, prog)
	}
	return compiled, nil
}

func newExcludeCELEnv() (*cel.Env, error) {
	return cel.NewEnv(
		cel.Variable("commit", cel.MapType(cel.StringType, cel.AnyType)),
	)
}

func compileExcludeCEL(env *cel.Env, expr string) (cel.Program, error) {
	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, issues.Err()
	}
	return env.Program(ast)
}

func (c *CompiledExcludeConfig) CompiledRules() []cel.Program {
	return c.compiledRules
}
