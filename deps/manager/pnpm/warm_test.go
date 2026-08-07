package pnpm

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
		probe string
		want  []string
	}{
		{
			name: "download suppresses lifecycle scripts", probe: "10.7.0",
			want: []string{
				"manifest: write package.json",
				"download: exec pnpm install --ignore-scripts",
			},
		},
		{
			name: "verify replays offline against the frozen lockfile", verif: true, probe: "10.7.0",
			want: []string{
				"manifest: write package.json",
				"download: exec pnpm install --ignore-scripts",
				"clean: remove node_modules",
				"verify: exec pnpm install --offline --frozen-lockfile",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			steps, err := (Warmer{}).Steps(manifest.WarmRequest{
				Dir: "/scratch", Name: "left-pad", Version: "1.3.0", Build: tc.build, Verify: tc.verif,
			}, tc.probe)
			if err != nil {
				t.Fatal(err)
			}
			if got := manifest.FormatSteps(steps); strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Fatalf("steps mismatch\n got: %v\nwant: %v", got, tc.want)
			}
		})
	}
}

// pnpm 10.6 stopped running dependency lifecycle scripts even without
// --ignore-scripts, so on those versions building needs an explicit allowlist
// flag that older pnpm does not recognise.
func TestBuildFlagIsGatedOnTheProbedVersion(t *testing.T) {
	cases := []struct {
		version string
		want    string
	}{
		{version: "9.12.0", want: "download: exec pnpm install"},
		{version: "10.5.9", want: "download: exec pnpm install"},
		{version: "10.6.0", want: "download: exec pnpm install --config.dangerouslyAllowAllBuilds=true"},
		{version: "10.7.1", want: "download: exec pnpm install --config.dangerouslyAllowAllBuilds=true"},
		{version: "11.0.0", want: "download: exec pnpm install --config.dangerouslyAllowAllBuilds=true"},
	}
	for _, tc := range cases {
		t.Run(tc.version, func(t *testing.T) {
			steps, err := (Warmer{}).Steps(manifest.WarmRequest{
				Dir: "/scratch", Name: "left-pad", Version: "1.3.0", Build: true,
			}, tc.version)
			if err != nil {
				t.Fatal(err)
			}
			got := manifest.FormatSteps(steps)
			if len(got) != 2 || got[1] != tc.want {
				t.Fatalf("pnpm %s download step = %q, want %q", tc.version, got[len(got)-1], tc.want)
			}
		})
	}
}

// Guessing the flag set would either skip the builds the user asked for or pass
// an argument older pnpm rejects, so an unreadable probe is a hard failure.
func TestBuildRejectsAnUnreadableProbe(t *testing.T) {
	for _, probe := range []string{"", "not-a-version"} {
		if _, err := (Warmer{}).Steps(manifest.WarmRequest{
			Dir: "/scratch", Name: "left-pad", Version: "1.3.0", Build: true,
		}, probe); err == nil {
			t.Errorf("probe %q: expected an error when --build cannot determine the pnpm version", probe)
		}
	}
}

// Without --build the version is irrelevant, so a missing probe must not block a
// plain warm.
func TestDownloadIgnoresTheProbe(t *testing.T) {
	steps, err := (Warmer{}).Steps(manifest.WarmRequest{
		Dir: "/scratch", Name: "left-pad", Version: "1.3.0",
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"manifest: write package.json", "download: exec pnpm install --ignore-scripts"}
	if got := manifest.FormatSteps(steps); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("steps mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestWarmerIdentity(t *testing.T) {
	if got := (Warmer{}).Manager(); got != manifest.ManagerPNPM {
		t.Errorf("Manager() = %q, want %q", got, manifest.ManagerPNPM)
	}
	probe := (Warmer{}).Probe()
	if probe == nil {
		t.Fatal("Probe() = nil, want pnpm --version: the build flag depends on it")
	}
	if got := probe.String(); got != "pnpm --version" {
		t.Errorf("Probe() = %q, want %q", got, "pnpm --version")
	}
}
