package deps

import (
	"fmt"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
)

func (p UpdatePlan) Pretty() api.Text {
	t := clicky.Text(fmt.Sprintf("[%s] %s", p.Manager, p.Name), managerStyle(p.Manager))
	if p.OldVersion != "" || p.NewVersion != "" {
		t = t.Space().Append(p.OldVersion, "font-mono text-muted")
		if p.NewVersion != "" {
			t = t.Append(" -> ", "text-muted").Append(p.NewVersion, "font-mono text-green-600")
		}
	}
	switch {
	case p.Skipped != "":
		t = t.Space().Append("skipped: "+p.Skipped, "text-muted")
	case p.Checked:
		t = t.Space().Append("update available", "text-green-600")
	case p.DryRun:
		t = t.Space().Append("(dry-run)", "text-yellow-600")
	case p.Written:
		t = t.Space().Append("written", "text-green-600")
	}
	if len(p.Staged) > 0 {
		t = t.Space().Append("staged "+strings.Join(p.Staged, ", "), "text-muted")
	}
	if p.StageError != "" {
		t = t.Space().Append("stage failed: "+p.StageError, "text-yellow-600")
	}
	return t
}

func (UpdatePlan) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		api.Column("manager").Label("Manager").Build(),
		api.Column("dependency").Label("Dependency").Build(),
		api.Column("file").Label("File").Build(),
		api.Column("scope").Label("Scope").Build(),
		api.Column("change").Label("Change").Build(),
		api.Column("status").Label("Status").Build(),
	}
}

func (p UpdatePlan) Row() map[string]any {
	row := map[string]any{
		"manager":    clicky.Text(string(p.Manager), managerStyle(p.Manager)),
		"dependency": clicky.Text(p.Name, "font-bold text-cyan-600"),
		"file":       clicky.Text(p.File, "font-mono"),
		"scope":      clicky.Text(p.Scope, "text-muted"),
		"change":     updateChangeText(p.OldVersion, p.NewVersion),
	}
	switch {
	case p.Skipped != "":
		row["status"] = clicky.Text(p.Skipped, "text-muted")
	case p.Checked:
		row["status"] = clicky.Text("update available", "text-green-600")
	case p.DryRun:
		row["status"] = clicky.Text("dry-run", "text-yellow-600")
	case p.Written:
		status := clicky.Text("written", "text-green-600")
		if len(p.Staged) > 0 {
			status = status.Append(" + staged "+strings.Join(p.Staged, ", "), "text-muted")
		}
		if p.StageError != "" {
			status = status.Append(" (stage failed: "+p.StageError+")", "text-yellow-600")
		}
		row["status"] = status
	default:
		row["status"] = clicky.Text("")
	}
	return row
}

func updateChangeText(oldVersion, newVersion string) api.Text {
	text := clicky.Text(oldVersion, "font-mono text-muted")
	if newVersion != "" {
		text = text.Append(" -> ", "text-muted").Append(newVersion, "font-mono text-green-600")
	}
	return text
}

func planFromCandidate(candidate UpdateCandidate) UpdatePlan {
	return UpdatePlan{
		Manager:    candidate.Manager,
		Name:       candidate.Name,
		File:       candidate.File,
		Scope:      candidate.Scope,
		OldVersion: candidate.Current,
	}
}

func skippedUpdatePlan(candidate UpdateCandidate, reason string) UpdatePlan {
	plan := planFromCandidate(candidate)
	plan.Skipped = reason
	return plan
}

func checkUpdatePlan(choice UpdateChoice) UpdatePlan {
	plan := planFromCandidate(choice.Candidate)
	plan.NewVersion = checkUpdateVersion(choice)
	plan.Checked = true
	return plan
}

func checkUpdateVersion(choice UpdateChoice) string {
	if choice.LatestStable != "" {
		return choice.LatestStable
	}
	if choice.LatestPrerelease != "" {
		return choice.LatestPrerelease
	}
	if len(choice.Versions) > 0 {
		return choice.Versions[0]
	}
	return ""
}

func orderedUpdatePlans(candidates []UpdateCandidate, plansByKey map[string]UpdatePlan) []UpdatePlan {
	plans := make([]UpdatePlan, 0, len(plansByKey))
	for _, candidate := range candidates {
		if plan, ok := plansByKey[candidate.key()]; ok {
			plans = append(plans, plan)
		}
	}
	return plans
}
