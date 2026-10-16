package actions

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestActions(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "GitHub Actions references")
}

var _ = Describe("workflow references", func() {
	It("finds external steps and reusable jobs at distinct source lines", func() {
		content := []byte("name: Example\non: push\njobs:\n  build:\n    steps:\n      - uses: actions/checkout@v4\n      - uses: actions/checkout@v3\n      - uses: ./local\n      - uses: docker://alpine:3.20\n  call:\n    uses: acme/automation/.github/workflows/check.yml@v2.1.0\n")
		result, err := Parse(".github/workflows/ci.yml", content)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.References).To(Equal([]Reference{
			{Name: "actions/checkout", Repository: "actions/checkout", Ref: "v4", Line: 6, Scope: "jobs.build.steps[0].uses"},
			{Name: "actions/checkout", Repository: "actions/checkout", Ref: "v3", Line: 7, Scope: "jobs.build.steps[1].uses"},
			{Name: "acme/automation/.github/workflows/check.yml", Repository: "acme/automation", Ref: "v2.1.0", Line: 11, Scope: "jobs.call.uses"},
		}))
		Expect(result.Skipped).To(Equal([]Skipped{
			{Value: "./local", Line: 8, Scope: "jobs.build.steps[2].uses", Reason: "local action"},
			{Value: "docker://alpine:3.20", Line: 9, Scope: "jobs.build.steps[3].uses", Reason: "Docker action"},
		}))
	})

	It("finds composite action steps and ignores unrelated uses keys", func() {
		content := []byte("name: Example\nruns:\n  using: composite\n  steps:\n    - uses: acme/setup/tools@v1.2.3\n      with:\n        uses: pretend/action@v1\n")
		result, err := Parse("action.yaml", content)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.References).To(Equal([]Reference{{Name: "acme/setup/tools", Repository: "acme/setup", Ref: "v1.2.3", Line: 5, Scope: "runs.steps[0].uses"}}))
	})

	It("treats a workflow named action.yml as a workflow and reports branch refs", func() {
		content := []byte("jobs:\n  build:\n    steps:\n      - uses: actions/checkout@v4\n      - uses: acme/tool@main\n")
		result, err := Parse(".github/workflows/action.yml", content)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.References).To(Equal([]Reference{{Name: "actions/checkout", Repository: "actions/checkout", Ref: "v4", Line: 4, Scope: "jobs.build.steps[0].uses"}}))
		Expect(result.Skipped).To(Equal([]Skipped{{Value: "acme/tool@main", Line: 5, Scope: "jobs.build.steps[1].uses", Reason: "non-version ref"}}))
	})
})

var _ = Describe("reference edits", func() {
	It("preserves formatting and updates a verified pin comment", func() {
		content := []byte("steps:\n  - uses: 'actions/checkout@1111111111111111111111111111111111111111' # v4.2.2\n  - uses: actions/setup-go@v5 # keep this note\n")
		updated, err := Apply(content, []Edit{
			{Line: 2, OldValue: "actions/checkout@1111111111111111111111111111111111111111", NewValue: "actions/checkout@2222222222222222222222222222222222222222", VersionTag: "v5.0.1"},
			{Line: 3, OldValue: "actions/setup-go@v5", NewValue: "actions/setup-go@v6.0.0"},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(string(updated)).To(Equal("steps:\n  - uses: 'actions/checkout@2222222222222222222222222222222222222222' # v5.0.1\n  - uses: actions/setup-go@v6.0.0 # keep this note\n"))
	})

	It("rejects a changed source value before editing", func() {
		_, err := Apply([]byte("steps:\n  - uses: actions/checkout@v5\n"), []Edit{{Line: 2, OldValue: "actions/checkout@v4", NewValue: "actions/checkout@v6"}})
		Expect(err).To(MatchError(ContainSubstring("changed")))
	})

	It("updates a version embedded in a pin comment without changing its other words", func() {
		content := []byte("jobs:\n  build:\n    steps:\n      - uses: actions/checkout@1111111111111111111111111111111111111111 # pinned at v4.2.2 for review\n")
		result, err := Parse(".github/workflows/ci.yml", content)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.References[0].VersionComment).To(Equal("v4.2.2"))
		updated, err := Apply(content, []Edit{{Line: 4, OldValue: "actions/checkout@1111111111111111111111111111111111111111", NewValue: "actions/checkout@2222222222222222222222222222222222222222", VersionTag: "v5.0.1"}})
		Expect(err).NotTo(HaveOccurred())
		Expect(string(updated)).To(ContainSubstring("# pinned at v5.0.1 for review"))
	})
})
