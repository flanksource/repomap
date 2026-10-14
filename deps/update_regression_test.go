package deps

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestUpdateRegressions(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Dependency update regressions")
}

var _ = Describe("pnpm update discovery", func() {
	var dir string
	BeforeEach(func() { dir = GinkgoT().TempDir() })
	write := func(name, content string) {
		Expect(os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)).To(Succeed())
	}
	It("resolves a catalog and discovers members without local lockfiles", func() {
		write("pnpm-workspace.yaml", "packages: ['packages/*']\ncatalog:\n  '@example/ui': 0.3.47\n")
		write("pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
		write("packages/web/package.json", `{"dependencies":{"@example/ui":"catalog:"}}`)
		candidates, err := DiscoverUpdateCandidates(dir, []Manager{ManagerPNPM}, DiscoverFilter{})
		Expect(err).NotTo(HaveOccurred())
		Expect(candidates).To(HaveLen(1))
		Expect(candidates[0].Current).To(Equal("0.3.47"))
		plans, err := Update(context.Background(), dir, UpdateOptions{
			Managers: []Manager{ManagerPNPM}, Version: "0.3.47", DryRun: true,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(plans).To(HaveLen(1))
		Expect(plans[0].Skipped).To(Equal("already at selected version"))
	})
	It("fails on a missing named catalog entry", func() {
		write("pnpm-workspace.yaml", "packages: ['.']\ncatalogs:\n  shared:\n    other: 1.0.0\n")
		write("pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
		write("package.json", `{"dependencies":{"@example/ui":"catalog:shared"}}`)
		_, err := DiscoverUpdateCandidates(dir, []Manager{ManagerPNPM}, DiscoverFilter{})
		Expect(err).To(MatchError(And(ContainSubstring("shared"), ContainSubstring("@example/ui"))))
	})
	It("normalizes the explicit default catalog alias", func() {
		write("pnpm-workspace.yaml", "packages: ['packages/*']\ncatalog:\n  acme-ui: 1.0.0\n")
		write("packages/a/package.json", `{"dependencies":{"acme-ui":"catalog:"}}`)
		write("packages/b/package.json", `{"dependencies":{"acme-ui":"catalog:default"}}`)
		candidates, err := DiscoverUpdateCandidates(dir, []Manager{ManagerPNPM}, DiscoverFilter{})
		Expect(err).NotTo(HaveOccurred())
		Expect(candidates).To(HaveLen(2))
		Expect(candidates[0].catalog.Key()).To(Equal(candidates[1].catalog.Key()))
		Expect(candidates[0].consumers).To(HaveLen(2))
	})
	It("discovers consumers with recursive package patterns", func() {
		write("pnpm-workspace.yaml", "packages: ['./packages/**']\ncatalog:\n  acme-ui: 1.0.0\n")
		write("packages/a/package.json", `{"dependencies":{"acme-ui":"catalog:"}}`)
		write("packages/a/README.md", "documentation\n")
		write("packages/a/node_modules/ignored/package.json", "not a workspace manifest")
		candidates, err := DiscoverUpdateCandidates(dir, []Manager{ManagerPNPM}, DiscoverFilter{})
		Expect(err).NotTo(HaveOccurred())
		Expect(candidates).To(HaveLen(1))
		Expect(candidates[0].consumers).To(Equal([]string{filepath.Join(dir, "packages/a/package.json")}))
	})
	It("serializes overlapping nested workspaces", func() {
		write("pr/pnpm-workspace.yaml", "packages: ['.', '../testrunner']\n")
		write("pr/package.json", `{"dependencies":{"acme-ui":"1.0.0"}}`)
		write("testrunner/pnpm-workspace.yaml", "packages: ['.']\n")
		write("testrunner/package.json", `{"devDependencies":{"acme-ui":"1.0.0"}}`)
		candidates, err := DiscoverUpdateCandidates(dir, []Manager{ManagerPNPM}, DiscoverFilter{})
		Expect(err).NotTo(HaveOccurred())
		Expect(candidates).To(HaveLen(2))
		var requests []updateRequest
		for _, candidate := range candidates {
			requests = append(requests, updateRequest{Candidate: candidate, Version: "2.0.0"})
		}
		Expect(groupUpdateBatchesIntoLanes(groupUpdateRequestsIntoBatches(requests))).To(HaveLen(1))
	})
	It("rejects conflicting shared-catalog selections before writing", func() {
		workspace := "packages: ['packages/*']\ncatalog:\n  acme-ui: 1.0.0\n"
		write("pnpm-workspace.yaml", workspace)
		write("packages/a/package.json", `{"dependencies":{"acme-ui":"catalog:"}}`)
		write("packages/b/package.json", `{"dependencies":{"acme-ui":"catalog:"}}`)
		runner := &updateFakeRunner{responses: map[string]CommandResult{"pnpm view acme-ui versions --json": {Stdout: `["1.0.0","2.0.0","3.0.0"]`}}}
		plans, err := Update(context.Background(), dir, UpdateOptions{Managers: []Manager{ManagerPNPM}, Runner: runner, SelectCandidates: func(choices []UpdateChoice) ([]UpdateSelection, bool) {
			var selected []UpdateSelection
			for i, choice := range choices {
				selected = append(selected, UpdateSelection{UpdateChoice: choice, Version: []string{"2.0.0", "3.0.0"}[i]})
			}
			return selected, true
		}})
		Expect(err).To(MatchError(ContainSubstring("conflicting versions")))
		Expect(plans).To(HaveLen(2))
		Expect(os.ReadFile(filepath.Join(dir, "pnpm-workspace.yaml"))).To(Equal([]byte(workspace)))
		for _, cmd := range runner.commands {
			Expect(cmd.Args[0]).To(Equal("view"))
		}
	})
})
