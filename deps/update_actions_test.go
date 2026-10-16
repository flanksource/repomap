package deps

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GitHub Actions dependency updates", func() {
	const oldSHA = "1111111111111111111111111111111111111111"
	const newSHA = "2222222222222222222222222222222222222222"
	const workflow = "name: CI\non: push\njobs:\n  build:\n    steps:\n      - uses: actions/checkout@v4\n      - uses: actions/checkout@1111111111111111111111111111111111111111 # v4.2.2\n"
	const tags = oldSHA + "\trefs/tags/v4\n" + oldSHA + "\trefs/tags/v4.2.2\n" + newSHA + "\trefs/tags/v5.0.1\n"

	var dir string
	var file string
	var runner *updateFakeRunner
	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		file = filepath.Join(dir, ".github", "workflows", "ci.yml")
		Expect(os.MkdirAll(filepath.Dir(file), 0o755)).To(Succeed())
		Expect(os.WriteFile(file, []byte(workflow), 0o644)).To(Succeed())
		runner = &updateFakeRunner{responses: map[string]CommandResult{
			"git ls-remote --tags https://github.com/actions/checkout.git": {Stdout: tags},
		}}
	})

	It("discovers both uses occurrences and exports them in the dependency graph", func() {
		candidates, err := DiscoverUpdateCandidates(dir, []Manager{ManagerActions}, DiscoverFilter{})
		Expect(err).NotTo(HaveOccurred())
		Expect(candidates).To(HaveLen(2))
		Expect(candidates[0].key()).NotTo(Equal(candidates[1].key()))
		export, err := Scan(context.Background(), dir, Options{Managers: []Manager{ManagerActions}, MaxDepth: 1})
		Expect(err).NotTo(HaveOccurred())
		Expect(export.Roots).To(HaveLen(1))
		Expect(export.Roots[0].Children).To(HaveLen(2))
		Expect(export.Statistics.ByManager[ManagerActions]).To(Equal(3))
	})

	It("previews an exact release while preserving SHA pin style", func() {
		plans, err := Update(context.Background(), dir, UpdateOptions{Managers: []Manager{ManagerActions}, Runner: runner, Latest: true, DryRun: true})
		Expect(err).NotTo(HaveOccurred())
		Expect(plans).To(HaveLen(2))
		Expect(plans[0].NewVersion).To(Equal("v5.0.1"))
		Expect(plans[1].ResolvedRef).To(Equal(newSHA))
		Expect(plans[0].DryRun).To(BeTrue())
		Expect(os.ReadFile(file)).To(Equal([]byte(workflow)))
		Expect(runner.ran()).To(Equal([]string{"git ls-remote --tags https://github.com/actions/checkout.git"}))
	})

	It("checks available updates without writing and reports the SHA to be pinned", func() {
		plans, err := Update(context.Background(), dir, UpdateOptions{Managers: []Manager{ManagerActions}, Runner: runner, Check: true})
		Expect(err).NotTo(HaveOccurred())
		Expect(plans).To(HaveLen(2))
		Expect(plans[0].Checked).To(BeTrue())
		Expect(plans[0].NewVersion).To(Equal("v5.0.1"))
		Expect(plans[1].ResolvedRef).To(Equal(newSHA))
		Expect(os.ReadFile(file)).To(Equal([]byte(workflow)))
	})

	It("limits a direct workflow path to that file", func() {
		other := filepath.Join(dir, ".github", "workflows", "other.yml")
		Expect(os.WriteFile(other, []byte("jobs:\n  build:\n    steps:\n      - uses: actions/setup-go@v5\n"), 0o644)).To(Succeed())
		candidates, err := DiscoverUpdateCandidates(file, []Manager{ManagerActions}, DiscoverFilter{})
		Expect(err).NotTo(HaveOccurred())
		Expect(candidates).To(HaveLen(2))
		for _, candidate := range candidates {
			Expect(candidate.Name).To(Equal("actions/checkout"))
		}
	})

	It("discovers workflows when the scan root is the workflows directory", func() {
		Expect(prepareUpdateTestRepo(dir)).To(Succeed())
		candidates, err := DiscoverUpdateCandidates(filepath.Dir(file), []Manager{ManagerActions}, DiscoverFilter{})
		Expect(err).NotTo(HaveOccurred())
		Expect(candidates).To(HaveLen(2))
	})

	It("updates reusable workflow jobs and composite action steps", func() {
		workflowContent := "jobs:\n  call:\n    uses: acme/automation/.github/workflows/check.yml@v4\n"
		compositeContent := "runs:\n  using: composite\n  steps:\n    - uses: acme/automation/setup@v4\n"
		composite := filepath.Join(dir, "tools", "action.yml")
		Expect(os.MkdirAll(filepath.Dir(composite), 0o755)).To(Succeed())
		Expect(os.WriteFile(file, []byte(workflowContent), 0o644)).To(Succeed())
		Expect(os.WriteFile(composite, []byte(compositeContent), 0o644)).To(Succeed())
		Expect(prepareUpdateTestRepo(dir)).To(Succeed())
		runner.responses["git ls-remote --tags https://github.com/acme/automation.git"] = CommandResult{Stdout: tags}

		plans, err := Update(context.Background(), dir, UpdateOptions{Managers: []Manager{ManagerActions}, Runner: runner, Version: "v5.0.1"})
		Expect(err).NotTo(HaveOccurred())
		Expect(plans).To(HaveLen(2))
		Expect(plans[0].Written).To(BeTrue())
		Expect(plans[1].Written).To(BeTrue())
		updatedWorkflow, err := os.ReadFile(file)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(updatedWorkflow)).To(ContainSubstring("acme/automation/.github/workflows/check.yml@v5.0.1"))
		updatedComposite, err := os.ReadFile(composite)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(updatedComposite)).To(ContainSubstring("acme/automation/setup@v5.0.1"))
	})

	It("stages only action edits when the workflow already has an unstaged change", func() {
		Expect(prepareUpdateTestRepo(dir)).To(Succeed())
		modified := strings.Replace(workflow, "name: CI", "name: User edit", 1)
		Expect(os.WriteFile(file, []byte(modified), 0o644)).To(Succeed())
		plans, err := Update(context.Background(), dir, UpdateOptions{Managers: []Manager{ManagerActions}, Runner: runner, Version: "v5.0.1"})
		Expect(err).NotTo(HaveOccurred())
		Expect(plans).To(HaveLen(2))
		Expect(plans[0].Written).To(BeTrue())
		Expect(plans[1].ResolvedRef).To(Equal(newSHA))
		content, err := os.ReadFile(file)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(ContainSubstring("name: User edit"))
		Expect(string(content)).To(ContainSubstring("actions/checkout@v5.0.1"))
		Expect(string(content)).To(ContainSubstring("actions/checkout@" + newSHA + " # v5.0.1"))
		staged, err := exec.Command("git", "-C", dir, "diff", "--cached", "--", ".github/workflows/ci.yml").Output()
		Expect(err).NotTo(HaveOccurred())
		Expect(string(staged)).To(ContainSubstring("actions/checkout@v5.0.1"))
		Expect(string(staged)).NotTo(ContainSubstring("User edit"))
	})
})
