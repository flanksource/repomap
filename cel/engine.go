package cel

import (
	"fmt"

	"github.com/flanksource/repomap"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

type Engine struct {
	config        *repomap.SeverityConfig
	celEnv        *cel.Env
	compiledRules []compiledRule
}

type compiledRule struct {
	expression string
	program    cel.Program
	severity   repomap.Severity
}

func NewEngine(config *repomap.SeverityConfig) (*Engine, error) {
	if config == nil {
		config = repomap.DefaultSeverityConfig()
	}

	env, err := cel.NewEnv(
		cel.Variable("commit", cel.MapType(cel.StringType, cel.AnyType)),
		cel.Variable("change", cel.MapType(cel.StringType, cel.AnyType)),
		cel.Variable("kubernetes", cel.MapType(cel.StringType, cel.AnyType)),
		cel.Variable("file", cel.MapType(cel.StringType, cel.AnyType)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}

	allRules := config.AllRules()

	engine := &Engine{
		config:        config,
		celEnv:        env,
		compiledRules: make([]compiledRule, 0, len(allRules)),
	}

	for expr, severity := range allRules {
		program, err := engine.compileExpression(expr)
		if err != nil {
			return nil, fmt.Errorf("failed to compile rule '%s': %w", expr, err)
		}
		engine.compiledRules = append(engine.compiledRules, compiledRule{
			expression: expr,
			program:    program,
			severity:   severity,
		})
	}

	return engine, nil
}

func (e *Engine) compileExpression(expr string) (cel.Program, error) {
	ast, issues := e.celEnv.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("CEL compilation error: %w", issues.Err())
	}
	program, err := e.celEnv.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("CEL program creation error: %w", err)
	}
	return program, nil
}

func (e *Engine) Evaluate(ctx map[string]any) repomap.Severity {
	for _, rule := range e.compiledRules {
		result, _, err := rule.program.Eval(ctx)
		if err != nil {
			continue
		}
		if boolVal, ok := result.(types.Bool); ok && boolVal == types.True {
			return rule.severity
		}
	}
	return e.config.Default
}

func (e *Engine) EvaluateWithDetails(ctx map[string]any) (repomap.Severity, string, error) {
	for _, rule := range e.compiledRules {
		result, _, err := rule.program.Eval(ctx)
		if err != nil {
			continue
		}
		if boolVal, ok := result.(types.Bool); ok && boolVal == types.True {
			return rule.severity, rule.expression, nil
		}
	}
	return e.config.Default, "", nil
}

func (e *Engine) TestExpression(expr string, ctx map[string]any) (bool, error) {
	program, err := e.compileExpression(expr)
	if err != nil {
		return false, err
	}

	result, _, err := program.Eval(ctx)
	if err != nil {
		return false, fmt.Errorf("evaluation error: %w", err)
	}

	boolVal, ok := result.(ref.Val)
	if !ok {
		return false, fmt.Errorf("expression did not return a boolean value")
	}

	if boolVal.Type().TypeName() != "bool" {
		return false, fmt.Errorf("expression returned %s, expected bool", boolVal.Type().TypeName())
	}

	return boolVal.Equal(types.True) == types.True, nil
}

func (e *Engine) GetConfig() *repomap.SeverityConfig {
	return e.config
}

func (e *Engine) RuleCount() int {
	return len(e.compiledRules)
}
