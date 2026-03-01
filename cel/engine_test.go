package cel

import (
	"testing"

	"github.com/flanksource/repomap"
)

func TestNewEngine(t *testing.T) {
	engine, err := NewEngine(nil)
	if err != nil {
		t.Fatalf("NewEngine(nil) error: %v", err)
	}
	if engine.RuleCount() == 0 {
		t.Error("expected default rules to be loaded")
	}
}

func TestEngineEvaluate(t *testing.T) {
	config := &repomap.SeverityConfig{
		Default: repomap.Medium,
		Rules: map[string]repomap.Severity{
			`change.type == "deleted"`:     repomap.Critical,
			`kubernetes.kind == "Secret"`:  repomap.Critical,
			`commit.line_changes > 100`:    repomap.High,
		},
	}

	engine, err := NewEngine(config)
	if err != nil {
		t.Fatalf("NewEngine() error: %v", err)
	}

	tests := []struct {
		name     string
		ctx      map[string]any
		expected repomap.Severity
	}{
		{
			"deleted change",
			map[string]any{
				"commit":     map[string]any{"line_changes": 10},
				"change":     map[string]any{"type": "deleted"},
				"kubernetes": map[string]any{"kind": ""},
				"file":       map[string]any{"extension": ".go"},
			},
			repomap.Critical,
		},
		{
			"kubernetes secret",
			map[string]any{
				"commit":     map[string]any{"line_changes": 10},
				"change":     map[string]any{"type": "modified"},
				"kubernetes": map[string]any{"kind": "Secret"},
				"file":       map[string]any{"extension": ".yaml"},
			},
			repomap.Critical,
		},
		{
			"large change",
			map[string]any{
				"commit":     map[string]any{"line_changes": 200},
				"change":     map[string]any{"type": "modified"},
				"kubernetes": map[string]any{"kind": ""},
				"file":       map[string]any{"extension": ".go"},
			},
			repomap.High,
		},
		{
			"default severity",
			map[string]any{
				"commit":     map[string]any{"line_changes": 5},
				"change":     map[string]any{"type": "modified"},
				"kubernetes": map[string]any{"kind": ""},
				"file":       map[string]any{"extension": ".go"},
			},
			repomap.Medium,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.Evaluate(tt.ctx)
			if got != tt.expected {
				t.Errorf("Evaluate() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestEngineEvaluateWithDetails(t *testing.T) {
	config := &repomap.SeverityConfig{
		Default: repomap.Low,
		Rules: map[string]repomap.Severity{
			`change.type == "deleted"`: repomap.Critical,
		},
	}

	engine, err := NewEngine(config)
	if err != nil {
		t.Fatalf("NewEngine() error: %v", err)
	}

	ctx := map[string]any{
		"commit":     map[string]any{},
		"change":     map[string]any{"type": "deleted"},
		"kubernetes": map[string]any{},
		"file":       map[string]any{},
	}

	sev, expr, err := engine.EvaluateWithDetails(ctx)
	if err != nil {
		t.Fatalf("EvaluateWithDetails() error: %v", err)
	}
	if sev != repomap.Critical {
		t.Errorf("severity = %q, want Critical", sev)
	}
	if expr != `change.type == "deleted"` {
		t.Errorf("expr = %q, want 'change.type == \"deleted\"'", expr)
	}
}

func TestEngineTestExpression(t *testing.T) {
	engine, err := NewEngine(&repomap.SeverityConfig{
		Default: repomap.Medium,
		Rules:   map[string]repomap.Severity{},
	})
	if err != nil {
		t.Fatalf("NewEngine() error: %v", err)
	}

	ctx := map[string]any{
		"commit":     map[string]any{"line_changes": 50},
		"change":     map[string]any{},
		"kubernetes": map[string]any{},
		"file":       map[string]any{},
	}

	result, err := engine.TestExpression(`commit.line_changes > 10`, ctx)
	if err != nil {
		t.Fatalf("TestExpression() error: %v", err)
	}
	if !result {
		t.Error("expected true for line_changes(50) > 10")
	}

	result, err = engine.TestExpression(`commit.line_changes > 100`, ctx)
	if err != nil {
		t.Fatalf("TestExpression() error: %v", err)
	}
	if result {
		t.Error("expected false for line_changes(50) > 100")
	}
}
