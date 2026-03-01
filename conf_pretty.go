package repomap

import (
	"fmt"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
)

func (conf *ArchConf) Pretty() api.Text {
	if conf == nil {
		return clicky.Text("(empty configuration)")
	}

	t := clicky.Text(conf.repoPath).NewLine()
	indent := "  "

	hasGit := conf.Git.Commits.Enabled || len(conf.Git.Commits.AllowedTypes) > 0
	if hasGit {
		t = t.Append("Git Configuration", "font-bold text-blue-600").NewLine()
		if conf.Git.Commits.Enabled {
			t = t.Append(indent + "Commits Validation: Enabled").NewLine()
			if len(conf.Git.Commits.AllowedTypes) > 0 {
				t = t.Append(indent+"  Allowed Types: ", "text-muted").Append(strings.Join(conf.Git.Commits.AllowedTypes, ", ")).NewLine()
			}
			if len(conf.Git.Commits.Blocklist) > 0 {
				t = t.Append(indent+"  Blocklist: ", "text-muted").Append(strings.Join(conf.Git.Commits.Blocklist, ", ")).NewLine()
			}
			if conf.Git.Commits.RequiredScope {
				t = t.Append(indent + "  Required Scope: Yes").NewLine()
			}
			if conf.Git.Commits.RequiredReference {
				t = t.Append(indent + "  Required Reference: Yes").NewLine()
			}
		}
		t = t.NewLine()
	}

	if len(conf.Scopes.Rules) > 0 || len(conf.Scopes.AllowedScopes) > 0 {
		t = t.Append("Scopes Configuration", "font-bold text-purple-600").NewLine()
		if len(conf.Scopes.AllowedScopes) > 0 {
			t = t.Append(indent+"Allowed Scopes: ", "text-muted").Append(strings.Join(conf.Scopes.AllowedScopes, ", ")).NewLine()
		}
		if len(conf.Scopes.Rules) > 0 {
			t = t.Append(indent + "Rules:").NewLine()
			for scope, rules := range conf.Scopes.Rules {
				t = t.Append(indent+"  "+scope+": ", "font-medium")
				t = t.Append(fmt.Sprintf("%d pattern(s)", len(rules)), "text-muted").NewLine()
				for i, rule := range rules {
					if i < 5 {
						t = t.Append(indent + "    - " + rule.Path).NewLine()
					} else if i == 5 {
						t = t.Append(indent+"    ... and ", "text-muted").Append(fmt.Sprintf("%d more", len(rules)-5), "text-muted").NewLine()
						break
					}
				}
			}
		}
		t = t.NewLine()
	}

	if len(conf.Tech.Rules) > 0 {
		t = t.Append("Technology Configuration", "font-bold text-orange-600").NewLine()
		t = t.Append(indent + "Rules:").NewLine()
		for tech, rules := range conf.Tech.Rules {
			t = t.Append(indent+"  "+tech+": ", "font-medium")
			t = t.Append(fmt.Sprintf("%d pattern(s)", len(rules)), "text-muted").NewLine()
			for i, rule := range rules {
				if i < 5 {
					t = t.Append(indent + "    - " + rule.Path).NewLine()
				} else if i == 5 {
					t = t.Append(indent+"    ... and ", "text-muted").Append(fmt.Sprintf("%d more", len(rules)-5), "text-muted").NewLine()
					break
				}
			}
		}
	}

	if len(conf.Severity.Rules) > 0 {
		t = t.Append("Severity Rules", "font-bold text-red-600").NewLine()
		for expr, sev := range conf.Severity.Rules {
			t = t.Append(indent).Append(string(sev), "font-medium").Append(": ", "text-muted").Append(expr).NewLine()
		}
	}

	return t
}
