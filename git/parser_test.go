package git

import (
	"testing"

	"github.com/flanksource/repomap"
)

func TestParseCommitTypeAndScope(t *testing.T) {
	tests := []struct {
		subject       string
		expectedType  repomap.CommitType
		expectedScope repomap.ScopeType
		expectedSubj  string
	}{
		{"feat(auth): add login", repomap.CommitTypeFeat, repomap.ScopeType("auth"), "add login"},
		{"fix: null pointer", repomap.CommitTypeFix, repomap.ScopeTypeUnknown, "null pointer"},
		{"plain commit message", repomap.CommitTypeUnknown, repomap.ScopeTypeUnknown, "plain commit message"},
		{"chore(deps): update go modules", repomap.CommitTypeChore, repomap.ScopeType("deps"), "update go modules"},
	}

	for _, tt := range tests {
		gotType, gotScope, gotSubj := ParseCommitTypeAndScope(tt.subject)
		if gotType != tt.expectedType {
			t.Errorf("ParseCommitTypeAndScope(%q) type = %q, want %q", tt.subject, gotType, tt.expectedType)
		}
		if gotScope != tt.expectedScope {
			t.Errorf("ParseCommitTypeAndScope(%q) scope = %q, want %q", tt.subject, gotScope, tt.expectedScope)
		}
		if gotSubj != tt.expectedSubj {
			t.Errorf("ParseCommitTypeAndScope(%q) subject = %q, want %q", tt.subject, gotSubj, tt.expectedSubj)
		}
	}
}

func TestParseReference(t *testing.T) {
	tests := []struct {
		input        string
		expectedSubj string
		expectedRef  string
	}{
		{"fix bug (#123)", "fix bug", "123"},
		{"fix bug #456", "fix bug", "456"},
		{"no reference here", "no reference here", ""},
	}

	for _, tt := range tests {
		gotSubj, gotRef := ParseReference(tt.input)
		if gotSubj != tt.expectedSubj {
			t.Errorf("ParseReference(%q) subject = %q, want %q", tt.input, gotSubj, tt.expectedSubj)
		}
		if gotRef != tt.expectedRef {
			t.Errorf("ParseReference(%q) ref = %q, want %q", tt.input, gotRef, tt.expectedRef)
		}
	}
}

func TestParseTrailers(t *testing.T) {
	message := `Some commit body text.

Signed-off-by: John <john@example.com>
Co-authored-by: Jane <jane@example.com>
`
	body, trailers := ParseTrailers(message)
	if len(trailers) != 2 {
		t.Fatalf("expected 2 trailers, got %d: %v", len(trailers), trailers)
	}
	if trailers["Signed-off-by"] != "John <john@example.com>" {
		t.Errorf("Signed-off-by = %q", trailers["Signed-off-by"])
	}
	if trailers["Co-authored-by"] != "Jane <jane@example.com>" {
		t.Errorf("Co-authored-by = %q", trailers["Co-authored-by"])
	}
	if body == "" {
		t.Error("expected non-empty body")
	}
}
