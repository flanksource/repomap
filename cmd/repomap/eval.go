package main

import (
	"fmt"
	"os"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	celengine "github.com/flanksource/repomap/cel"
	"github.com/flanksource/repomap"
	"github.com/ghodss/yaml"
)

type EvalOptions struct {
	Expr string `json:"expr" flag:"expr" help:"CEL expression to evaluate"`
	File string `json:"file" flag:"file" help:"YAML file with severity rules to evaluate"`
	Path string `json:"path" flag:"path" help:"Repository path for context" default:"."`
}

func (opts EvalOptions) GetName() string { return "eval" }

func (opts EvalOptions) Help() api.Text {
	return clicky.Text(`Evaluate CEL expressions against a sample context.

Tests CEL expressions or severity rules from a file. Useful for
developing and debugging severity rules in repomap.yaml.

EXAMPLES:
  repomap eval --expr 'change.type == "deleted"'
  repomap eval --file rules.yaml
  repomap eval --expr 'kubernetes.kind == "Secret"'`)
}

type EvalResult struct {
	Expression string          `json:"expression"`
	Result     bool            `json:"result"`
	Severity   repomap.Severity `json:"severity,omitempty"`
}

func (r EvalResult) Pretty() api.Text {
	t := clicky.Text("")
	if r.Result {
		t = t.Append("MATCH", "font-bold text-green-600")
	} else {
		t = t.Append("NO MATCH", "font-bold text-red-600")
	}
	t = t.Space().Append(r.Expression, "text-muted")
	if r.Severity != "" {
		t = t.Space().Append("severity=", "text-muted").Append(string(r.Severity), "font-bold")
	}
	return t
}

func init() {
	clicky.AddCommand(rootCmd, EvalOptions{}, runEval)
}

func runEval(opts EvalOptions) (any, error) {
	if opts.Expr == "" && opts.File == "" {
		return nil, fmt.Errorf("must specify --expr or --file")
	}

	ctx := map[string]any{
		"commit":     map[string]any{"type": "feat", "scope": "api"},
		"change":     map[string]any{"type": "modified", "file": "", "adds": 10, "dels": 5},
		"kubernetes": map[string]any{"kind": "", "name": "", "namespace": ""},
		"file":       map[string]any{"path": "", "language": "", "scopes": []string{}},
	}

	if opts.Expr != "" {
		config := &repomap.SeverityConfig{
			Rules: map[string]repomap.Severity{
				opts.Expr: repomap.Info,
			},
		}
		engine, err := celengine.NewEngine(config)
		if err != nil {
			return nil, fmt.Errorf("failed to compile expression: %w", err)
		}

		matched, err := engine.TestExpression(opts.Expr, ctx)
		if err != nil {
			return nil, fmt.Errorf("evaluation failed: %w", err)
		}

		return EvalResult{
			Expression: opts.Expr,
			Result:     matched,
		}, nil
	}

	data, err := os.ReadFile(opts.File)
	if err != nil {
		return nil, fmt.Errorf("failed to read rules file: %w", err)
	}

	var config repomap.SeverityConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse rules file: %w", err)
	}

	engine, err := celengine.NewEngine(&config)
	if err != nil {
		return nil, fmt.Errorf("failed to compile rules: %w", err)
	}

	severity, matchedExpr, err := engine.EvaluateWithDetails(ctx)
	if err != nil {
		return nil, fmt.Errorf("evaluation failed: %w", err)
	}

	return EvalResult{
		Expression: matchedExpr,
		Result:     matchedExpr != "",
		Severity:   severity,
	}, nil
}
