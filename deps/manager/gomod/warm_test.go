package gomod

import (
	"strings"
	"testing"

	"github.com/flanksource/repomap/deps/manifest"
)

// formatSteps renders each step as "name: argv [env]" so a single assertion pins
// the command sequence, the exact arguments, and the environment together.
func formatSteps(t *testing.T, steps []manifest.Step) []string {
	t.Helper()
	out := make([]string, 0, len(steps))
	for _, step := range steps {
		if step.Kind != manifest.StepExec {
			t.Fatalf("go warming should only produce exec steps, got %q for %q", step.Kind, step.Name)
		}
		out = append(out, step.Name+": "+step.Command.String()+" ["+strings.Join(step.Command.Env, " ")+"]")
	}
	return out
}

func TestStepsPerFlagCombination(t *testing.T) {
	const (
		online  = "[GOWORK=off GOFLAGS=-mod=mod]"
		offline = "[GOWORK=off GOFLAGS=-mod=mod GOPROXY=off]"
		module  = "github.com/acme/lib"
		version = "v1.2.3"
	)
	cases := []struct {
		name  string
		build bool
		verif bool
		want  []string
	}{
		{
			name: "download only",
			want: []string{
				"init: go mod init " + ModulePath + " " + online,
				"resolve: go get github.com/acme/lib/...@v1.2.3 " + online,
				"download: go mod download all " + online,
			},
		},
		{
			name:  "build compiles every package",
			build: true,
			want: []string{
				"init: go mod init " + ModulePath + " " + online,
				"resolve: go get github.com/acme/lib/...@v1.2.3 " + online,
				"download: go mod download all " + online,
				"build: go build github.com/acme/lib/... " + online,
			},
		},
		{
			// Without --build there is nothing compiled to replay, so the offline
			// proof is that every module zip is already resident.
			name:  "verify without build replays the download",
			verif: true,
			want: []string{
				"init: go mod init " + ModulePath + " " + online,
				"resolve: go get github.com/acme/lib/...@v1.2.3 " + online,
				"download: go mod download all " + online,
				"verify: go mod download all " + offline,
			},
		},
		{
			name:  "verify with build replays the build",
			build: true,
			verif: true,
			want: []string{
				"init: go mod init " + ModulePath + " " + online,
				"resolve: go get github.com/acme/lib/...@v1.2.3 " + online,
				"download: go mod download all " + online,
				"build: go build github.com/acme/lib/... " + online,
				"verify: go build github.com/acme/lib/... " + offline,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			steps, err := (Warmer{}).Steps(manifest.WarmRequest{
				Dir:     "/scratch",
				Name:    module,
				Version: version,
				Build:   tc.build,
				Verify:  tc.verif,
			}, "")
			if err != nil {
				t.Fatal(err)
			}
			got := formatSteps(t, steps)
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Fatalf("steps mismatch\n got: %s\nwant: %s", strings.Join(got, "\n "), strings.Join(tc.want, "\n "))
			}
		})
	}
}

// GOPROXY=off must never leak onto a warming step, or the warm would fail on a
// cold cache instead of populating it.
func TestOnlyVerifyStepDisablesTheProxy(t *testing.T) {
	steps, err := (Warmer{}).Steps(manifest.WarmRequest{
		Dir: "/scratch", Name: "github.com/acme/lib", Version: "v1.0.0", Build: true, Verify: true,
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range steps {
		offline := strings.Contains(strings.Join(step.Command.Env, " "), "GOPROXY=off")
		if offline != (step.Name == "verify") {
			t.Errorf("step %q: GOPROXY=off present = %v, want %v", step.Name, offline, step.Name == "verify")
		}
	}
}

// Every step must run inside the scratch project, otherwise go would resolve
// against whatever module happens to contain the process working directory.
func TestStepsRunInTheScratchDir(t *testing.T) {
	const dir = "/tmp/repomap-cache-warm-xyz"
	steps, err := (Warmer{}).Steps(manifest.WarmRequest{
		Dir: dir, Name: "github.com/acme/lib", Version: "v1.0.0", Build: true, Verify: true,
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range steps {
		if step.Command.Dir != dir {
			t.Errorf("step %q dir = %q, want %q", step.Name, step.Command.Dir, dir)
		}
	}
}

func TestStepsRejectsIncompleteRequest(t *testing.T) {
	cases := []struct {
		name    string
		request manifest.WarmRequest
	}{
		{name: "no module path", request: manifest.WarmRequest{Dir: "/scratch", Version: "v1.0.0"}},
		{name: "no version", request: manifest.WarmRequest{Dir: "/scratch", Name: "github.com/acme/lib"}},
		{name: "no dir", request: manifest.WarmRequest{Name: "github.com/acme/lib", Version: "v1.0.0"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := (Warmer{}).Steps(tc.request, ""); err == nil {
				t.Fatal("expected an error for an incomplete request")
			}
		})
	}
}

func TestNormalizeSpec(t *testing.T) {
	const module = "github.com/flanksource/commons"
	cases := []struct {
		spec string
		want string
	}{
		// A canonical module path is already what go get wants.
		{spec: "github.com/acme/lib@v1.2.3", want: "github.com/acme/lib@v1.2.3"},
		{spec: "github.com/acme/lib", want: "github.com/acme/lib@latest"},
		// A dot in the first element marks it as a host, so a vanity domain and a
		// major-version suffix both survive untouched.
		{spec: "gopkg.in/yaml.v3", want: "gopkg.in/yaml.v3@latest"},
		{spec: "github.com/acme/lib/v2@v2.1.0", want: "github.com/acme/lib/v2@v2.1.0"},
		// A bare owner/repo slug, the shape copied out of a GitHub page.
		{spec: "flanksource/commons", want: module + "@latest"},
		{spec: "flanksource/commons@v1.2.3", want: module + "@v1.2.3"},
		{spec: "  flanksource/commons  ", want: module + "@latest"},
		// The three URL shapes: browser, https clone, ssh clone.
		{spec: "https://github.com/flanksource/commons", want: module + "@latest"},
		{spec: "http://github.com/flanksource/commons/", want: module + "@latest"},
		{spec: "https://github.com/flanksource/commons.git@v1.2.3", want: module + "@v1.2.3"},
		{spec: "git@github.com:flanksource/commons.git", want: module + "@latest"},
		{spec: "ssh://git@github.com/flanksource/commons.git", want: module + "@latest"},
		{spec: "git://github.com/flanksource/commons.git", want: module + "@latest"},
		// A slug on another forge keeps its host rather than gaining github.com.
		{spec: "https://gitlab.com/acme/lib@v1.0.0", want: "gitlab.com/acme/lib@v1.0.0"},
	}
	for _, tc := range cases {
		t.Run(tc.spec, func(t *testing.T) {
			got, err := (Warmer{}).NormalizeSpec(tc.spec)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("NormalizeSpec(%q) = %q, want %q", tc.spec, got, tc.want)
			}
		})
	}
}

func TestNormalizeSpecRejectsWhatCannotBeAModulePath(t *testing.T) {
	cases := []struct {
		name string
		spec string
	}{
		// One element names no host, and inventing github.com/commons would warm
		// something the user never asked for.
		{name: "single element", spec: "commons"},
		{name: "single element with version", spec: "commons@v1.2.3"},
		{name: "port is not a module path", spec: "github.com/acme/lib:8080"},
		{name: "empty", spec: "   "},
		{name: "no version after the separator", spec: "flanksource/commons@"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := (Warmer{}).NormalizeSpec(tc.spec)
			if err == nil {
				t.Fatalf("NormalizeSpec(%q) = %q, want an error", tc.spec, got)
			}
		})
	}
}

func TestWarmerIdentity(t *testing.T) {
	if got := (Warmer{}).Manager(); got != manifest.ManagerGo {
		t.Errorf("Manager() = %q, want %q", got, manifest.ManagerGo)
	}
	if probe := (Warmer{}).Probe(); probe != nil {
		t.Errorf("Probe() = %v, want nil: go needs no runtime version check", probe)
	}
}
