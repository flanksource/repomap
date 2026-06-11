package deps

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNPMCorruptLockfileFails(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, "package-lock.json")
	writeFile(t, lock, `{ "name": "app", "version": "1.0.0", `) // truncated/corrupt JSON
	writeFile(t, filepath.Join(dir, "package.json"), `{"name":"app","version":"1.0.0"}`)

	_, _, err := resolveNPMManifest(Project{Manager: ManagerNPM, Dir: dir, File: lock})
	if err == nil {
		t.Fatal("expected corrupt lockfile to error instead of silently falling back")
	}
	if !strings.Contains(err.Error(), "package-lock.json") {
		t.Fatalf("error should name the corrupt lockfile, got %v", err)
	}
}
