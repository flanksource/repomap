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
	if p.ResolvedRef != "" {
		t = t.Space().Append("pin "+p.ResolvedRef, "font-mono text-muted")
	}
	switch {
	case p.Error != "":
		t = t.Space().Append("failed: "+p.Error, "text-red-600")
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
	if p.SourceFile != "" {
		t = t.Space().Append("catalog: "+p.SourceFile, "text-muted")
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
		api.Column("source_file").Label("Catalog").Build(),
		api.Column("consumers").Hidden().Build(),
		api.Column("command").Hidden().Build(),
		api.Column("resolved_ref").Hidden().Build(),
		api.Column("changed_files").Hidden().Build(),
		api.Column("staged").Hidden().Build(),
		api.Column("error").Hidden().Build(),
		api.Column("stage_error").Hidden().Build(),
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
	if p.ResolvedRef != "" {
		row["change"] = updateChangeText(p.OldVersion, p.NewVersion).Append(" (pin "+p.ResolvedRef+")", "font-mono text-muted")
	}
	switch {
	case p.Error != "":
		row["status"] = clicky.Text("failed: "+p.Error, "text-red-600")
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
	if p.SourceFile != "" {
		row["source_file"] = p.SourceFile
		row["consumers"] = p.Consumers
	}
	if len(p.Command) > 0 {
		row["command"] = p.Command
	}
	if p.ResolvedRef != "" {
		row["resolved_ref"] = p.ResolvedRef
	}
	if p.Error != "" {
		row["error"] = p.Error
	}
	if p.StageError != "" {
		row["stage_error"] = p.StageError
	}
	if len(p.ChangedFiles) > 0 {
		row["changed_files"] = p.ChangedFiles
	}
	if len(p.Staged) > 0 {
		row["staged"] = p.Staged
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
	plan := UpdatePlan{
		Manager:    candidate.Manager,
		Name:       candidate.Name,
		File:       candidate.File,
		Scope:      candidate.Scope,
		OldVersion: candidate.Current,
	}
	if candidate.catalog != nil {
		plan.SourceFile = cwdRelativePath(candidate.catalog.Workspace.File)
		for _, consumer := range candidate.consumers {
			plan.Consumers = append(plan.Consumers, cwdRelativePath(consumer))
		}
	}
	return plan
}

func failedUpdatePlan(candidate UpdateCandidate, reason string) UpdatePlan {
	plan := planFromCandidate(candidate)
	plan.Error = reason
	return plan
}

func skippedUpdatePlan(candidate UpdateCandidate, reason string) UpdatePlan {
	plan := planFromCandidate(candidate)
	plan.Skipped = reason
	return plan
}

func checkUpdatePlan(choice UpdateChoice) UpdatePlan {
	plan := planFromCandidate(choice.Candidate)
	plan.NewVersion = checkUpdateVersion(choice)
	plan.ResolvedRef = choice.ResolvedRef
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
