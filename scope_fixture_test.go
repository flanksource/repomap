package repomap

import (
	"os/exec"
	"testing"

	"github.com/flanksource/gavel/fixtures"
	_ "github.com/flanksource/gavel/fixtures/types"
)

func TestScopeFixtures(t *testing.T) {
	out, err := exec.Command("go", "build", "-o", ".bin/repomap", "./cmd/repomap").CombinedOutput()
	if err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}

	runner, err := fixtures.NewRunner(fixtures.RunnerOptions{
		Paths: []string{"testdata/scope-*.md"},
	})
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}

	tree, err := runner.Run()
	if err != nil {
		stats := tree.GetStats()
		t.Fatalf("fixtures failed: %d/%d passed, %d failed, %d errors",
			stats.Passed, stats.Total, stats.Failed, stats.Error)
	}

	stats := tree.GetStats()
	if stats.Error > 0 {
		t.Fatalf("fixtures had errors: %d/%d passed, %d failed, %d errors",
			stats.Passed, stats.Total, stats.Failed, stats.Error)
	}
}
