package npm

import (
	"strings"
	"testing"

	"github.com/flanksource/repomap/deps/manifest"
)

func TestSteps(t *testing.T) {
	cases := []struct {
		name  string
		build bool
		verif bool
		want  []string
	}{
		{
			name: "download suppresses lifecycle scripts",
			want: []string{
				"manifest: write package.json",
				"download: exec npm install --ignore-scripts",
			},
		},
		{
			// Compiling native addons is the point of building, so the suppression
			// has to come off.
			name:  "build allows lifecycle scripts",
			build: true,
			want: []string{
				"manifest: write package.json",
				"download: exec npm install",
			},
		},
		{
			// npm ci over npm install: the lockfile written by the download step
			// makes it the stricter replay.
			name:  "verify replays from the lockfile offline",
			verif: true,
			want: []string{
				"manifest: write package.json",
				"download: exec npm install --ignore-scripts",
				"clean: remove node_modules",
				"verify: exec npm ci --offline",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			steps, err := (Warmer{}).Steps(manifest.WarmRequest{
				Dir: "/scratch", Name: "left-pad", Version: "1.3.0", Build: tc.build, Verify: tc.verif,
			}, "")
			if err != nil {
				t.Fatal(err)
			}
			if got := manifest.FormatSteps(steps); strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Fatalf("steps mismatch\n got: %v\nwant: %v", got, tc.want)
			}
		})
	}
}

func TestWarmerIdentity(t *testing.T) {
	if got := (Warmer{}).Manager(); got != manifest.ManagerNPM {
		t.Errorf("Manager() = %q, want %q", got, manifest.ManagerNPM)
	}
	if probe := (Warmer{}).Probe(); probe != nil {
		t.Errorf("Probe() = %v, want nil: npm needs no runtime version check", probe)
	}
}
