package node

import (
	"strings"
	"testing"

	"github.com/flanksource/repomap/deps/manifest"
)

func TestManifestDeclaresExactlyOneDependency(t *testing.T) {
	cases := []struct {
		name    string
		pkg     string
		version string
		want    string
	}{
		{
			name: "plain package", pkg: "left-pad", version: "1.3.0",
			want: `{
  "name": "repomap-cache-warm",
  "version": "0.0.0",
  "private": true,
  "dependencies": {
    "left-pad": "1.3.0"
  }
}`,
		},
		{
			// A scoped name must survive verbatim as the dependency key.
			name: "scoped package", pkg: "@flanksource/clicky-ui", version: "^2.1.0",
			want: `{
  "name": "repomap-cache-warm",
  "version": "0.0.0",
  "private": true,
  "dependencies": {
    "@flanksource/clicky-ui": "^2.1.0"
  }
}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Manifest(tc.pkg, tc.version)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("manifest mismatch\n got: %s\nwant: %s", got, tc.want)
			}
		})
	}
}

func testCommands() Commands {
	return Commands{
		Binary:        "fakepm",
		Install:       []string{"install"},
		IgnoreScripts: "--ignore-scripts",
		Offline:       []string{"install", "--offline"},
	}
}

func TestStepsWriteManifestBeforeInstalling(t *testing.T) {
	steps, err := Steps(manifest.WarmRequest{Dir: "/scratch", Name: "left-pad", Version: "1.3.0"}, testCommands())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"manifest: write package.json",
		"download: exec fakepm install --ignore-scripts",
	}
	if got := manifest.FormatSteps(steps); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("steps mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestBuildDropsIgnoreScriptsAndAppendsBuildArgs(t *testing.T) {
	cmds := testCommands()
	cmds.BuildArgs = []string{"--allow-builds"}
	steps, err := Steps(manifest.WarmRequest{Dir: "/scratch", Name: "left-pad", Version: "1.3.0", Build: true}, cmds)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"manifest: write package.json",
		"download: exec fakepm install --allow-builds",
	}
	if got := manifest.FormatSteps(steps); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("steps mismatch\n got: %v\nwant: %v", got, want)
	}
}

// Reinstalling over a populated node_modules is a no-op, so without the removal
// the offline replay would pass without proving the cache holds anything.
func TestVerifyRemovesNodeModulesBeforeReplaying(t *testing.T) {
	steps, err := Steps(manifest.WarmRequest{Dir: "/scratch", Name: "left-pad", Version: "1.3.0", Verify: true}, testCommands())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"manifest: write package.json",
		"download: exec fakepm install --ignore-scripts",
		"clean: remove node_modules",
		"verify: exec fakepm install --offline",
	}
	if got := manifest.FormatSteps(steps); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("steps mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestStepsRejectsIncompleteRequest(t *testing.T) {
	cases := []struct {
		name    string
		request manifest.WarmRequest
	}{
		{name: "no package name", request: manifest.WarmRequest{Dir: "/scratch", Version: "1.3.0"}},
		{name: "no version", request: manifest.WarmRequest{Dir: "/scratch", Name: "left-pad"}},
		{name: "no dir", request: manifest.WarmRequest{Name: "left-pad", Version: "1.3.0"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Steps(tc.request, testCommands()); err == nil {
				t.Fatal("expected an error for an incomplete request")
			}
		})
	}
}
