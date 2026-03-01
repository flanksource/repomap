package repomap

import (
	"fmt"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
)

func (f FileMap) Pretty() api.Text {
	t := clicky.Text(f.Path)
	if len(f.Scopes) > 0 {
		t = t.Append(" scopes: ", "text-muted").Append(f.Scopes)
	}
	if len(f.Tech) > 0 {
		t = t.Append(" tech: ", "text-muted").Append(f.Tech)
	}
	if f.Ignored {
		t = t.Append(" [IGNORED]", "text-yellow-600")
	}

	if len(f.KubernetesRefs) > 0 {
		t = t.NewLine().NewLine()
		t = t.Append("Kubernetes Resources", "font-bold text-blue-600").NewLine()

		for _, ref := range f.KubernetesRefs {
			t = t.Append("  ")
			if ref.APIVersion != "" && ref.Kind != "" {
				t = t.Append(ref.APIVersion+" "+ref.Kind, "font-medium text-cyan-600").NewLine()
			} else if ref.Kind != "" {
				t = t.Append(ref.Kind, "font-medium text-cyan-600").NewLine()
			}

			if ref.Name != "" {
				t = t.Append("    Name: ", "text-muted").Append(ref.Name).NewLine()
			}

			if ref.Namespace != "" {
				t = t.Append("    Namespace: ", "text-muted").Append(ref.Namespace).NewLine()
			}

			if ref.StartLine > 0 && ref.EndLine > 0 {
				t = t.Append("    Lines: ", "text-muted").Append(fmt.Sprintf("%d-%d", ref.StartLine, ref.EndLine)).NewLine()
			}

			if len(ref.Labels) > 0 {
				t = t.Append("    Labels:").NewLine()
				for k, v := range ref.Labels {
					t = t.Append("      "+k+": ", "text-muted").Append(v).NewLine()
				}
			}

			if len(ref.Annotations) > 0 {
				t = t.Append("    Annotations:").NewLine()
				for k, v := range ref.Annotations {
					t = t.Append("      "+k+": ", "text-muted").Append(v).NewLine()
				}
			}

			t = t.NewLine()
		}
	}

	return t
}

func (author Author) Pretty() api.Text {
	return api.Text{}.Append(author.Name, "font-bold").
		Append(" <"+author.Email+"> ", "text-muted").
		Append(author.Date, "text-muted")
}

func (c Commits) Pretty() api.Text {
	t := clicky.Text("")
	for i, commit := range c {
		if i > 0 {
			t = t.NewLine()
		}
		t = t.Add(commit.Pretty())
	}
	return t
}

func (c Commit) Pretty() api.Text {
	t := clicky.Text("")
	t = t.Append("commit ", "text-orange-500").Append(c.Hash).NewLine()
	t = t.Append(clicky.Text("Date: ", "text-muted")).Append(c.Author.Date).NewLine().
		Append("Author: ", "text-muted").Append(c.Author.Name).NewLine()

	if c.Committer.Name != "" && c.Committer.Name != c.Author.Name {
		t = t.Append("Committer: ", "text-muted").Append(c.Committer).NewLine()
	}
	t = t.Append(c.PrettySubject()).NewLine()

	for k, v := range c.Trailers {
		t = t.Append(k+": ", "text-muted").Append(v).NewLine()
	}
	for _, tag := range c.Tags {
		t = t.Append(clicky.Badge(tag, "text-sm text-gray-600 bg-gray-200 mr-1")).Space()
	}
	if len(c.Tags) > 0 {
		t = t.NewLine()
	}

	t = t.Append(c.Body).NewLine()

	return t
}

func (c Commit) PrettyShort() api.Text {
	return clicky.Text(c.Hash[:8], "text-muted").Space().
		Append(time.Since(c.Author.Date), "text-muted").Space().
		Append(c.Author.Name, "font-bold").Space().
		Append(c.PrettySubject(), "max-w-[60ch] truncate")
}

func (c Commit) PrettySubject() api.Text {
	t := clicky.Text("")
	if c.CommitType != CommitTypeUnknown {
		t = t.Append(c.CommitType.Pretty()).Space().Append(string(c.CommitType))
	}

	if c.Scope != ScopeTypeUnknown {
		t = t.Append("(", "text-muted").Append(string(c.Scope)).Append(")", "text-muted")
	}

	if c.CommitType != CommitTypeUnknown || c.Scope != ScopeTypeUnknown {
		t = t.Append(": ", "text-muted")
	}

	t = t.Append(c.Subject)
	if c.Reference != "" {
		t = t.Space().Append("#").Append(c.Reference, "text-muted").Space()
	}
	return t
}

func (c Commit) PrettyBody() api.Text {
	return clicky.Text(c.Body, "text-muted, max-lines-[3]")
}

func (c Changes) PrettyShort() api.Text {
	return c.Summary().Pretty()
}

func (c Changes) Pretty() api.Text {
	t := clicky.Text("")
	for i, change := range c {
		if i > 0 {
			t = t.NewLine()
		}
		t = t.Add(change.Pretty())
	}
	return t
}

func (ca CommitAnalysis) PrettyShort() api.Text {
	return clicky.Text(ca.Hash[:8], "text-muted").Space().
		Append(time.Since(ca.Author.Date), "text-muted").Space().
		Append(ca.Author.Name, "font-bold").Space().
		Append(ca.PrettySubject())
}

func (ca CommitAnalysis) Pretty() api.Text {
	t := ca.Commit.Pretty().NewLine()

	for _, change := range ca.Changes {
		t = t.NewLine().Add(change.Pretty())
	}
	t = t.HR()

	return t
}

func (c CommitChange) Pretty() api.Text {
	if len(c.KubernetesChanges) > 0 {
		var t api.Text
		for i, kc := range c.KubernetesChanges {
			if i > 0 {
				t = t.NewLine()
			}
			t = t.Append(kc.Pretty())
		}
		return t
	}

	t := clicky.Text("").Append(c.File, "font-mono").Space()

	if c.Adds > 0 {
		t = t.Append(fmt.Sprintf("+%d", c.Adds), "text-green-600")
	}
	if c.Dels > 0 {
		if c.Adds > 0 {
			t = t.Append("/", "text-muted")
		}
		t = t.Append(fmt.Sprintf("-%d", c.Dels), "text-red-600")
	}
	if len(c.Scope) > 0 {
		t = t.Append(" scope=", "text-muted").Append(c.Scope)
	}
	if len(c.Tech) > 0 {
		t = t.Append(" tech=", "text-muted")
		for i, v := range c.Tech {
			if i > 0 {
				t = t.Append(",", "text-muted")
			}
			t = t.Append(string(v))
		}
	}

	return t
}

func (ca CommitAnalyses) Pretty() api.Text {
	t := clicky.Text("")
	for i, analysis := range ca {
		if i > 0 {
			t = t.NewLine()
		}
		t = t.Add(analysis.Pretty())
	}
	return t
}

func (a AIAnalysisOutput) Pretty() api.Text {
	t := clicky.Text("AI Analysis:", "font-bold").Space().Space()
	t = t.Append("Type: ", "text-muted").Append(string(a.Type)).Space()
	t = t.Append("Scope: ", "text-muted").Append(string(a.Scope)).NewLine()
	t = t.Append(a.Subject).NewLine()
	t = t.NewLine().Append(a.Body, "text-muted")

	return t
}

func (v Violation) Pretty() api.Text {
	return api.Text{}.Append(v.File).Append(":").Append(v.Line)
}
