package repomap

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/clicky/api/icons"
	"github.com/flanksource/repomap/kubernetes"
)

func (f FileMap) Pretty() api.Text {
	t := clicky.Text(f.Path)
	if len(f.Scopes) > 0 {
		t = t.Space()
		for _, scope := range f.Scopes {
			t = t.Add(scope.Pretty()).Space()
		}
	}
	if f.Ignored {
		t = t.Append(" [IGNORED]", "text-yellow-600")
	}
	return t
}

func (FileMap) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		api.Column("path").Label("Path").Build(),
		api.Column("language").Label("Language").Build(),
		api.Column("scopes").Label("Scopes").Build(),
		api.Column("k8s").Label("Kubernetes").Build(),
	}
}

func (f FileMap) Row() map[string]any {
	var scopes Scopes
	for _, s := range f.Scopes {
		if string(s) != f.Language {
			scopes = append(scopes, s)
		}
	}
	row := map[string]any{
		"path":     clicky.Text("").Add(getFileIcon(f.Path)).Space().Append(f.Path, "font-mono"),
		"language": f.Language,
		"scopes":   scopes.Pretty(),
	}
	if len(f.KubernetesRefs) > 0 {
		t := clicky.Text("")
		for i, ref := range f.KubernetesRefs {
			if i > 0 {
				t = t.Append(", ")
			}
			t = t.Add(ref.Pretty())
		}
		row["k8s"] = t
	}
	return row
}

func (f FileMap) PrettyShort() api.Text {
	t := clicky.Text("").Add(getFileIcon(f.Path)).Space().Append(filepath.Base(f.Path), "font-mono")
	for _, scope := range f.Scopes {
		if string(scope) != f.Language {
			t = t.Space().Add(scope.Pretty())
		}
	}
	if f.Ignored {
		t = t.Append(" [IGNORED]", "text-yellow-600")
	}
	return t
}

func (f FileMap) GetChildren() []api.TreeNode {
	var children []api.TreeNode
	for _, ref := range f.KubernetesRefs {
		children = append(children, k8sRefNode{ref})
	}
	for _, match := range f.ScopeMatches {
		children = append(children, scopeMatchNode{match})
	}
	return children
}

type scopeMatchNode struct {
	ScopeMatch
}

func (n scopeMatchNode) Pretty() api.Text {
	return clicky.Text("").
		Add(ScopeType(n.Scope).Pretty()).Space().
		Append(n.Scope, "font-bold").
		Append(" ← ", "text-muted").
		Append(n.Rule, "text-muted")
}
func (n scopeMatchNode) GetChildren() []api.TreeNode { return nil }

type k8sRefNode struct {
	kubernetes.KubernetesRef
}

func (n k8sRefNode) Pretty() api.Text            { return n.KubernetesRef.Pretty() }
func (n k8sRefNode) GetChildren() []api.TreeNode { return nil }

func getFileIcon(path string) icons.Icon {
	switch filepath.Ext(path) {
	case ".go":
		return icons.Golang
	case ".js", ".jsx":
		return icons.JS
	case ".ts", ".tsx":
		return icons.TS
	case ".py":
		return icons.Python
	case ".java":
		return icons.Java
	case ".md":
		return icons.MD
	case ".yaml", ".yml":
		return icons.Config
	case ".json":
		return icons.File
	case ".sql":
		return icons.DB
	default:
		return icons.File
	}
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
