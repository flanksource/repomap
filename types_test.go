package repomap

import "testing"

func TestNewLineRanges(t *testing.T) {
	tests := []struct {
		input    []int
		expected LineRanges
	}{
		{nil, ""},
		{[]int{1, 2, 3, 4, 5}, "1-5"},
		{[]int{1, 2, 3, 7, 8, 9, 12}, "1-3,7-9,12"},
		{[]int{5}, "5"},
		{[]int{3, 1, 2}, "1-3"},
	}

	for _, tt := range tests {
		got := NewLineRanges(tt.input)
		if got != tt.expected {
			t.Errorf("NewLineRanges(%v) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestChangesSummary(t *testing.T) {
	changes := Changes{
		{File: "a.go", Adds: 10, Dels: 5, Type: SourceChangeTypeModified, Scope: Scopes{ScopeTypeApp}},
		{File: "b.go", Adds: 3, Dels: 1, Type: SourceChangeTypeModified, Scope: Scopes{ScopeTypeTest}},
	}

	summary := changes.Summary()
	if summary.Adds != 13 {
		t.Errorf("Summary().Adds = %d, want 13", summary.Adds)
	}
	if summary.Dels != 6 {
		t.Errorf("Summary().Dels = %d, want 6", summary.Dels)
	}
	if summary.Type != SourceChangeTypeModified {
		t.Errorf("Summary().Type = %q, want 'modified'", summary.Type)
	}
}

func TestCommitParsePatch(t *testing.T) {
	patch := `diff --git a/old.go b/new.go
new file mode 100644
--- /dev/null
+++ b/new.go
@@ -0,0 +1,3 @@
+package main
+
+func main() {}
diff --git a/existing.go b/existing.go
--- a/existing.go
+++ b/existing.go
@@ -1,3 +1,4 @@
 package main

+// added line
 func main() {}
`
	c := &Commit{Patch: patch}
	patches := c.ParsePatch()

	if len(patches) != 2 {
		t.Fatalf("expected 2 file patches, got %d", len(patches))
	}
	if !patches[0].IsNew {
		t.Error("expected first patch to be new file")
	}
	if patches[0].NewPath != "new.go" {
		t.Errorf("expected first patch path 'new.go', got %q", patches[0].NewPath)
	}
	if patches[1].NewPath != "existing.go" {
		t.Errorf("expected second patch path 'existing.go', got %q", patches[1].NewPath)
	}
}

func TestCommitGetFilePatch(t *testing.T) {
	patch := `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1 +1 @@
-old
+new
diff --git a/b.go b/b.go
--- a/b.go
+++ b/b.go
@@ -1 +1 @@
-old2
+new2
`
	c := &Commit{Patch: patch}
	got := c.GetFilePatch("b.go")
	if got == "" {
		t.Error("expected non-empty patch for b.go")
	}
	if c.GetFilePatch("nonexistent.go") != "" {
		t.Error("expected empty patch for nonexistent file")
	}
}
