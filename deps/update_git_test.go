package deps

import (
	"context"
	"fmt"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var _ = Describe("Git dependency commit selection", func() {
	DescribeTable("preserves the declared Git source when pinning a commit", func(spec, remote, pinned string) {
		parsed, ok, err := parseNodeGitSpec(spec)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())
		Expect(parsed.Remote).To(Equal(remote))
		Expect(parsed.Pin("0123456789abcdef0123456789abcdef01234567")).To(Equal(pinned))
	},
		Entry("GitHub shorthand", "github:acme/widget#main", "https://github.com/acme/widget.git", "github:acme/widget#0123456789abcdef0123456789abcdef01234567"),
		Entry("GitLab subgroup", "gitlab:acme/platform/widget#main", "https://gitlab.com/acme/platform/widget.git", "gitlab:acme/platform/widget#0123456789abcdef0123456789abcdef01234567"),
		Entry("SSH URL", "git+ssh://git@example.com/acme/widget.git#semver:^1.0.0", "ssh://git@example.com/acme/widget.git", "git+ssh://git@example.com/acme/widget.git#0123456789abcdef0123456789abcdef01234567"),
		Entry("SCP URL", "git@example.com:acme/widget.git#feature", "git@example.com:acme/widget.git", "git@example.com:acme/widget.git#0123456789abcdef0123456789abcdef01234567"),
	)

	It("does not treat registry versions as Git sources", func() {
		_, ok, err := parseNodeGitSpec("^1.2.3")
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeFalse())
	})

	It("rejects malformed Git URLs before running Git", func() {
		for _, spec := range []string{"git+widget", "git+https://user:secret@example.com/acme/widget.git"} {
			_, _, err := parseNodeGitSpec(spec)
			Expect(err).To(HaveOccurred())
		}
	})

	It("keeps Git dependencies in the interactive choices without querying a registry", func() {
		gitSpec, ok, err := parseNodeGitSpec("github:acme/widget#main")
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())
		candidate := UpdateCandidate{Manager: ManagerNPM, Name: "widget", Current: "github:acme/widget#main", Dir: GinkgoT().TempDir(), git: &gitSpec}
		runner := &updateFakeRunner{responses: map[string]CommandResult{}}
		choices, plans := resolveUpdateChoices(context.Background(), []UpdateCandidate{candidate}, UpdateOptions{Runner: runner})
		Expect(plans).To(BeEmpty())
		Expect(choices).To(HaveLen(1))
		Expect(choices[0].Versions).To(BeEmpty())
		Expect(runner.ran()).To(BeEmpty())
	})

	It("keeps current Go modules available for SHA selection", func() {
		candidate := UpdateCandidate{Manager: ManagerGo, Name: "example.com/widget", Current: "v1.2.0", Dir: GinkgoT().TempDir()}
		runner := &updateFakeRunner{responses: map[string]CommandResult{
			"go list -m -versions -json example.com/widget": {Stdout: `{"Versions":["v1.2.0"]}`},
		}}
		choices, plans := resolveUpdateChoices(context.Background(), []UpdateCandidate{candidate}, UpdateOptions{Runner: runner})
		Expect(plans).To(BeEmpty())
		Expect(choices).To(HaveLen(1))
		Expect(choices[0].Versions).To(BeEmpty())
	})

	It("uses the Go module origin instead of guessing a repository URL", func() {
		candidate := UpdateCandidate{Manager: ManagerGo, Name: "example.com/widget/v2", Current: "v2.1.0", Dir: GinkgoT().TempDir()}
		runner := &updateFakeRunner{responses: map[string]CommandResult{
			"go list -m -json example.com/widget/v2@v2.1.0": {Stdout: `{"Path":"example.com/widget/v2","Origin":{"VCS":"git","URL":"https://git.example.com/acme/widget"}}`},
		}}
		remote, err := goModuleGitRemote(context.Background(), runner, candidate)
		Expect(err).NotTo(HaveOccurred())
		Expect(remote).To(Equal("https://git.example.com/acme/widget"))
	})

	It("rejects a Git selection that changes the source or lacks a full SHA", func() {
		dir := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"dependencies":{"widget":"github:acme/widget#main"}}`), 0o644)).To(Succeed())
		for _, selected := range []string{"github:other/widget#0123456789abcdef0123456789abcdef01234567", "github:acme/widget#main"} {
			_, err := Update(context.Background(), dir, UpdateOptions{Managers: []Manager{ManagerNPM}, DryRun: true, SelectCandidates: func(choices []UpdateChoice) ([]UpdateSelection, bool) {
				return []UpdateSelection{{UpdateChoice: choices[0], Version: selected}}, true
			}})
			Expect(err).To(MatchError(ContainSubstring("full commit SHA")))
		}
	})

	It("skips Git sourced Node dependencies in release modes and rejects --version", func() {
		dir := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"dependencies":{"widget":"github:acme/widget#main"}}`), 0o644)).To(Succeed())
		for _, mode := range []UpdateOptions{{Check: true}, {Latest: true}} {
			runner := &updateFakeRunner{responses: map[string]CommandResult{}}
			mode.Managers = []Manager{ManagerNPM}
			mode.Runner = runner
			plans, err := Update(context.Background(), dir, mode)
			Expect(err).NotTo(HaveOccurred())
			Expect(plans).To(HaveLen(1))
			Expect(plans[0].Skipped).To(Equal("Git dependency requires interactive SHA selection"))
			Expect(runner.ran()).To(BeEmpty())
		}
		_, err := Update(context.Background(), dir, UpdateOptions{Managers: []Manager{ManagerNPM}, Version: "1.2.0"})
		Expect(err).To(MatchError(ContainSubstring("interactive v picker")))
	})

	It("fails verification when a lockfile resolves a different commit", func() {
		dir := GinkgoT().TempDir()
		pinned := "github:acme/widget#0123456789abcdef0123456789abcdef01234567"
		Expect(os.WriteFile(filepath.Join(dir, "package.json"), []byte(fmt.Sprintf(`{"dependencies":{"widget":%q}}`, pinned)), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte(`{"packages":{"node_modules/widget":{"resolved":"github:acme/widget#abcdef0123456789abcdef0123456789abcdef01"}}}`), 0o644)).To(Succeed())
		spec, ok, err := parseNodeGitSpec(pinned)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())
		err = verifyNodeGitPin(updateRequest{Candidate: UpdateCandidate{Manager: ManagerNPM, Name: "widget", Scope: "dependencies", Dir: dir, git: &spec}, Version: pinned})
		Expect(err).To(MatchError(ContainSubstring("lockfile entry does not contain selected commit")))
	})

	It("verifies the Go module origin hash after applying a SHA", func() {
		sha := "0123456789abcdef0123456789abcdef01234567"
		request := updateRequest{Candidate: UpdateCandidate{Manager: ManagerGo, Name: "example.com/widget", Dir: GinkgoT().TempDir()}, Version: sha}
		runner := &updateFakeRunner{responses: map[string]CommandResult{
			"go list -m -json example.com/widget": {Stdout: `{"Path":"example.com/widget","Origin":{"Hash":"` + sha + `"}}`},
		}}
		Expect(verifyGoGitPin(context.Background(), runner, request)).To(Succeed())
	})

	It("passes a selected Go commit to go get", func() {
		dir := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n\ngo 1.26\n\nrequire github.com/acme/widget v1.0.0\n"), 0o644)).To(Succeed())
		sha := "0123456789abcdef0123456789abcdef01234567"
		runner := &updateFakeRunner{responses: map[string]CommandResult{
			"go list -m -versions -json github.com/acme/widget": {Stdout: `{"Versions":["v1.0.0"]}`},
		}}
		plans, err := Update(context.Background(), dir, UpdateOptions{Managers: []Manager{ManagerGo}, DryRun: true, Runner: runner, SelectCandidates: func(choices []UpdateChoice) ([]UpdateSelection, bool) {
			Expect(choices).To(HaveLen(1))
			return []UpdateSelection{{UpdateChoice: choices[0], Version: sha}}, true
		}})
		Expect(err).NotTo(HaveOccurred())
		Expect(plans).To(HaveLen(1))
		Expect(plans[0].Command).To(Equal([]string{"go", "get", "github.com/acme/widget@" + sha}))
		Expect(plans[0].DryRun).To(BeTrue())
	})

	It("pins a pnpm catalog Git spec without changing the consumer reference or comment", func() {
		dir := GinkgoT().TempDir()
		workspace := "packages: ['.']\ncatalog:\n  widget: github:acme/widget#main # owned by platform\n"
		Expect(os.WriteFile(filepath.Join(dir, "pnpm-workspace.yaml"), []byte(workspace), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"dependencies":{"widget":"catalog:"}}`), 0o644)).To(Succeed())
		candidate, err := DiscoverUpdateCandidates(dir, []Manager{ManagerPNPM}, DiscoverFilter{})
		Expect(err).NotTo(HaveOccurred())
		Expect(candidate).To(HaveLen(1))
		Expect(candidate[0].git).NotTo(BeNil())
		pinned := candidate[0].git.Pin("0123456789abcdef0123456789abcdef01234567")
		Expect(candidate[0].catalog.Apply(pinned, false)).To(Succeed())
		data, err := os.ReadFile(filepath.Join(dir, "pnpm-workspace.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(data)).To(ContainSubstring("widget: " + pinned + " # owned by platform"))
		consumer, err := os.ReadFile(filepath.Join(dir, "package.json"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(consumer)).To(ContainSubstring(`"widget":"catalog:"`))
	})

	It("applies an npm Git pin to the manifest and lockfile", func() {
		root := GinkgoT().TempDir()
		source := filepath.Join(root, "source")
		consumer := filepath.Join(root, "consumer")
		Expect(os.MkdirAll(source, 0o755)).To(Succeed())
		Expect(os.MkdirAll(consumer, 0o755)).To(Succeed())
		git := func(dir string, args ...string) string {
			cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
			out, err := cmd.CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), string(out))
			return strings.TrimSpace(string(out))
		}
		for _, dir := range []string{source, consumer} {
			git(dir, "init", "-b", "main")
			git(dir, "config", "user.name", "Example")
			git(dir, "config", "user.email", "example@example.com")
		}
		Expect(os.WriteFile(filepath.Join(source, "package.json"), []byte(`{"name":"widget","version":"1.0.0"}`), 0o644)).To(Succeed())
		git(source, "add", "package.json")
		git(source, "commit", "-m", "package")
		sha := git(source, "rev-parse", "HEAD")
		base := "git+file://" + source
		Expect(os.WriteFile(filepath.Join(consumer, "package.json"), []byte(fmt.Sprintf(`{"name":"consumer","version":"1.0.0","dependencies":{"widget":%q}}`, base+"#main")), 0o644)).To(Succeed())
		git(consumer, "add", "package.json")
		git(consumer, "commit", "-m", "consumer")
		plans, err := Update(context.Background(), consumer, UpdateOptions{Managers: []Manager{ManagerNPM}, SelectCandidates: func(choices []UpdateChoice) ([]UpdateSelection, bool) {
			Expect(choices).To(HaveLen(1))
			return []UpdateSelection{{UpdateChoice: choices[0], Version: base + "#" + sha}}, true
		}})
		Expect(err).NotTo(HaveOccurred())
		Expect(plans).To(HaveLen(1))
		Expect(plans[0].Written).To(BeTrue())
		manifest, err := os.ReadFile(filepath.Join(consumer, "package.json"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(manifest)).To(ContainSubstring(base + "#" + sha))
		lock, err := os.ReadFile(filepath.Join(consumer, "package-lock.json"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(lock)).To(ContainSubstring(sha))
	})

	It("applies a pnpm catalog Git pin to the workspace and lockfile", func() {
		root := GinkgoT().TempDir()
		source := filepath.Join(root, "source")
		consumer := filepath.Join(root, "consumer")
		for _, dir := range []string{source, consumer} {
			Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
			for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.name", "Example"}, {"config", "user.email", "example@example.com"}} {
				cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
				out, err := cmd.CombinedOutput()
				Expect(err).NotTo(HaveOccurred(), string(out))
			}
		}
		Expect(os.WriteFile(filepath.Join(source, "package.json"), []byte(`{"name":"widget","version":"1.0.0"}`), 0o644)).To(Succeed())
		git := func(dir string, args ...string) string {
			cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
			out, err := cmd.CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), string(out))
			return strings.TrimSpace(string(out))
		}
		git(source, "add", "package.json")
		git(source, "commit", "-m", "package")
		sha := git(source, "rev-parse", "HEAD")
		base := "git+file://" + source
		Expect(os.WriteFile(filepath.Join(consumer, "pnpm-workspace.yaml"), []byte("packages: ['.']\ncatalog:\n  widget: "+base+"#main # shared\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(consumer, "package.json"), []byte(`{"name":"consumer","version":"1.0.0","dependencies":{"widget":"catalog:"}}`), 0o644)).To(Succeed())
		git(consumer, "add", "package.json", "pnpm-workspace.yaml")
		git(consumer, "commit", "-m", "consumer")
		plans, err := Update(context.Background(), consumer, UpdateOptions{Managers: []Manager{ManagerPNPM}, SelectCandidates: func(choices []UpdateChoice) ([]UpdateSelection, bool) {
			Expect(choices).To(HaveLen(1))
			return []UpdateSelection{{UpdateChoice: choices[0], Version: base + "#" + sha}}, true
		}})
		Expect(err).NotTo(HaveOccurred())
		Expect(plans).To(HaveLen(1))
		Expect(plans[0].Written).To(BeTrue())
		workspace, err := os.ReadFile(filepath.Join(consumer, "pnpm-workspace.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(workspace)).To(ContainSubstring(base + "#" + sha + " # shared"))
		lock, err := os.ReadFile(filepath.Join(consumer, "pnpm-lock.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(lock)).To(ContainSubstring(sha))
	})

	It("lists branches and the newest PR heads by tip commit date", func() {
		dir := GinkgoT().TempDir()
		git := func(args ...string) string {
			cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
			out, err := cmd.CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), string(out))
			return strings.TrimSpace(string(out))
		}
		git("init", "-b", "main")
		git("config", "user.name", "Example")
		git("config", "user.email", "example@example.com")
		Expect(os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"example"}`), 0o644)).To(Succeed())
		git("add", "package.json")
		git("commit", "-m", "base")
		base := git("rev-parse", "HEAD")
		git("branch", "feature")
		git("update-ref", "refs/pull/7/head", base)
		git("update-ref", "refs/pull/8/head", base)

		refs, err := listGitRefChoices(context.Background(), ExecRunner{}, dir)
		Expect(err).NotTo(HaveOccurred())
		Expect(refs.Branches).To(HaveLen(2))
		Expect(refs.Branches[0].Name).To(Equal("refs/heads/main"))
		Expect(refs.PRHeads).To(HaveLen(2))
		Expect(refs.PRHeads[0].Hash).To(Equal(base))
		Expect(refs.PRHeads[0].Date).To(BeTemporally("~", time.Now(), time.Minute))

		commits, err := recentGitCommits(context.Background(), ExecRunner{}, dir, refs.Branches[0])
		Expect(err).NotTo(HaveOccurred())
		Expect(commits).To(HaveLen(1))
		Expect(commits[0].SHA).To(Equal(base))
		Expect(commits[0].Subject).To(Equal("base"))
	})

	It("limits PR heads to the newest 30 commit dates", func() {
		var refs []gitRemoteRef
		var lines []string
		start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
		for i := 0; i <= gitPickerLimit; i++ {
			sha := fmt.Sprintf("%040x", i+1)
			name := fmt.Sprintf("refs/pull/%d/head", i)
			refs = append(refs, gitRemoteRef{Name: name, Hash: sha})
			lines = append(lines, strings.Join([]string{sha, start.Add(time.Duration(i) * time.Hour).Format(time.RFC3339), "change", fmt.Sprintf("refs/repomap/pull/%d/head", i)}, "\t"))
		}
		heads, err := parsePRHeadMetadata(strings.Join(lines, "\n"), refs)
		Expect(err).NotTo(HaveOccurred())
		Expect(heads).To(HaveLen(gitPickerLimit))
		Expect(heads[0].Name).To(Equal("refs/pull/30/head"))
		Expect(heads[len(heads)-1].Name).To(Equal("refs/pull/1/head"))
	})
})
