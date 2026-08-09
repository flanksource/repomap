package deps

import (
	"fmt"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
)

func (r WarmResult) Pretty() api.Text {
	t := clicky.Text(fmt.Sprintf("[%s] %s", r.Manager, r.Name), managerStyle(r.Manager))
	if version := r.displayVersion(); version != "" {
		t = t.Space().Append(version, "font-mono text-muted")
	}
	if r.Error != "" {
		return t.Space().Append("failed: "+r.Error, "text-red-600")
	}
	if r.Packages > 0 {
		t = t.Space().Append(fmt.Sprintf("%d packages", r.Packages), "text-muted")
	}
	t = t.Space().Append(strings.Join(r.badges(), " "), "text-green-600")
	if r.Cache != "" {
		t = t.Space().Append(r.Cache, "font-mono text-muted")
	}
	if r.SummaryError != "" {
		t = t.Space().Append("("+r.SummaryError+")", "text-yellow-600")
	}
	return t
}

func (WarmResult) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		api.Column("manager").Label("Manager").Build(),
		api.Column("dependency").Label("Dependency").Build(),
		api.Column("version").Label("Version").Build(),
		api.Column("packages").Label("Packages").Build(),
		api.Column("status").Label("Status").Build(),
		api.Column("cache").Label("Cache").Build(),
	}
}

func (r WarmResult) Row() map[string]any {
	row := map[string]any{
		"manager":    clicky.Text(string(r.Manager), managerStyle(r.Manager)),
		"dependency": clicky.Text(r.Name, "font-bold text-cyan-600"),
		"version":    clicky.Text(r.displayVersion(), "font-mono"),
		"cache":      clicky.Text(r.Cache, "font-mono text-muted"),
	}
	if r.Packages > 0 {
		row["packages"] = clicky.Text(fmt.Sprintf("%d", r.Packages), "text-muted")
	} else {
		row["packages"] = clicky.Text("")
	}
	if r.Error != "" {
		row["status"] = clicky.Text("failed: "+r.Error, "text-red-600")
		return row
	}
	status := clicky.Text(strings.Join(r.badges(), " "), "text-green-600")
	if r.SummaryError != "" {
		status = status.Append(" ("+r.SummaryError+")", "text-yellow-600")
	}
	row["status"] = status
	return row
}

// badges names what the warm actually did, so "warmed" is never confused with
// "compiled" or with "proven to work offline".
func (r WarmResult) badges() []string {
	badges := []string{"warmed"}
	if r.Built {
		badges = append(badges, "built")
	}
	if r.Verified {
		badges = append(badges, "verified offline")
	}
	return badges
}

// displayVersion prefers the version the manager resolved, falling back to what
// was requested when the read-back could not determine it.
func (r WarmResult) displayVersion() string {
	if r.Version != "" {
		return r.Version
	}
	return r.RequestedVersion
}
