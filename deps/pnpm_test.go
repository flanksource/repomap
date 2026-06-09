package deps

import (
	"path/filepath"
	"testing"
)

func TestParsePNPMNativeDependencyMap(t *testing.T) {
	dir := t.TempDir()
	root, err := parsePNPMNative([]byte(`[
  {
    "name": "app",
    "version": "1.0.0",
    "path": "/workspace/app",
    "dependencies": {
      "left-pad": {
        "version": "1.3.0",
        "path": "/workspace/app/node_modules/left-pad",
        "dependencies": {
          "repeat-string": {
            "from": "repeat-string",
            "version": "1.6.1",
            "path": "/workspace/app/node_modules/repeat-string"
          }
        }
      }
    }
  }
]`), Project{Manager: ManagerPNPM, Dir: dir, File: filepath.Join(dir, "pnpm-lock.yaml")})
	if err != nil {
		t.Fatal(err)
	}
	app := findChild(root, "app")
	if app == nil || app.Version != "1.0.0" || !app.Direct {
		t.Fatalf("app node not parsed: %#v", app)
	}
	leftPad := findChild(app, "left-pad")
	if leftPad == nil || leftPad.Version != "1.3.0" {
		t.Fatalf("map dependency not parsed: %#v", leftPad)
	}
	repeatString := findChild(leftPad, "repeat-string")
	if repeatString == nil || repeatString.Version != "1.6.1" {
		t.Fatalf("nested map dependency not parsed: %#v", repeatString)
	}
}
