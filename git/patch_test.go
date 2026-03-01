package git

import "testing"

func TestParsePatch(t *testing.T) {
	patch := `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,3 +1,5 @@
 package main

+import "fmt"
+
+func hello() { fmt.Println("hello") }
diff --git a/new.go b/new.go
new file mode 100644
--- /dev/null
+++ b/new.go
@@ -0,0 +1,3 @@
+package main
+
+func world() {}
`
	changes, err := ParsePatch(patch)
	if err != nil {
		t.Fatalf("ParsePatch() error: %v", err)
	}

	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(changes))
	}

	if changes[0].File != "main.go" {
		t.Errorf("changes[0].File = %q, want 'main.go'", changes[0].File)
	}
	if changes[0].Adds != 3 {
		t.Errorf("changes[0].Adds = %d, want 3", changes[0].Adds)
	}

	if changes[1].File != "new.go" {
		t.Errorf("changes[1].File = %q, want 'new.go'", changes[1].File)
	}
	if changes[1].Type != "added" {
		t.Errorf("changes[1].Type = %q, want 'added'", changes[1].Type)
	}
}

func TestParsePatchEmpty(t *testing.T) {
	changes, err := ParsePatch("")
	if err != nil {
		t.Fatalf("ParsePatch('') error: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("expected 0 changes for empty patch, got %d", len(changes))
	}
}

func TestParsePatchDeleted(t *testing.T) {
	patch := `diff --git a/removed.go b/removed.go
deleted file mode 100644
--- a/removed.go
+++ /dev/null
@@ -1,3 +0,0 @@
-package main
-
-func old() {}
`
	changes, err := ParsePatch(patch)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Type != "deleted" {
		t.Errorf("Type = %q, want 'deleted'", changes[0].Type)
	}
	if changes[0].Dels != 3 {
		t.Errorf("Dels = %d, want 3", changes[0].Dels)
	}
}
