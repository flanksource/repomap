package deps

import (
	"context"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("interactive dependency updates", func() {
	It("shows every dependency when all published versions are current", func() {
		dir := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"dependencies":{"acme-ui":"1.0.0","acme-api":"2.0.0"}}`), 0o644)).To(Succeed())
		runner := &updateFakeRunner{responses: map[string]CommandResult{
			"npm view acme-ui versions --json":  {Stdout: `["1.0.0"]`},
			"npm view acme-api versions --json": {Stdout: `["2.0.0"]`},
		}}
		var shown []UpdateChoice
		plans, err := Update(context.Background(), dir, UpdateOptions{
			Managers: []Manager{ManagerNPM}, Interactive: true, Runner: runner,
			SelectCandidates: func(choices []UpdateChoice) ([]UpdateSelection, bool) {
				shown = choices
				return selectAllStable()(choices)
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(shown).To(HaveLen(2))
		Expect(shown[0].UpToDate).To(BeTrue())
		Expect(shown[1].UpToDate).To(BeTrue())
		Expect(plans).To(HaveLen(2))
		for _, plan := range plans {
			Expect(plan.Skipped).To(Equal("already up to date"))
			Expect(plan.Written).To(BeFalse())
		}
		Expect(runner.ran()).To(ConsistOf("npm view acme-ui versions --json", "npm view acme-api versions --json"))
		shownWithoutFlag := false
		plans, err = Update(context.Background(), dir, UpdateOptions{
			Managers: []Manager{ManagerNPM}, Runner: &updateFakeRunner{responses: runner.responses},
			SelectCandidates: func([]UpdateChoice) ([]UpdateSelection, bool) {
				shownWithoutFlag = true
				return nil, false
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(shownWithoutFlag).To(BeFalse())
		Expect(plans).To(BeEmpty())
	})

	It("keeps current and updateable dependencies in the same picker", func() {
		dir := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"dependencies":{"acme-ui":"1.0.0","acme-api":"2.0.0"}}`), 0o644)).To(Succeed())
		runner := &updateFakeRunner{responses: map[string]CommandResult{
			"npm view acme-ui versions --json":  {Stdout: `["1.0.0","1.1.0"]`},
			"npm view acme-api versions --json": {Stdout: `["2.0.0"]`},
		}}
		var shown []UpdateChoice
		_, err := Update(context.Background(), dir, UpdateOptions{
			Managers: []Manager{ManagerNPM}, Interactive: true, Runner: runner,
			SelectCandidates: func(choices []UpdateChoice) ([]UpdateSelection, bool) {
				shown = choices
				return nil, false
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(shown).To(HaveLen(2))
		Expect(shown).To(ContainElement(And(HaveField("UpToDate", true), HaveField("Candidate", HaveField("Name", "acme-api")))))
		Expect(shown).To(ContainElement(And(HaveField("LatestStable", "1.1.0"), HaveField("Candidate", HaveField("Name", "acme-ui")))))
	})

	It("rejects noninteractive modes combined with -i", func() {
		for _, opts := range []UpdateOptions{{Interactive: true, Latest: true}, {Interactive: true, Check: true}, {Interactive: true, Version: "2.0.0"}} {
			_, err := Update(context.Background(), ".", opts)
			Expect(err).To(MatchError(ContainSubstring("--interactive")))
		}
	})

	It("labels a current row and does not open an empty version picker", func() {
		choice := UpdateChoice{Candidate: UpdateCandidate{Manager: ManagerNPM, Name: "acme-ui", Current: "1.0.0", File: "package.json"}, UpToDate: true}
		model := newUpdateChoiceTreeModel([]UpdateChoice{choice})
		var leaf *updateChoiceTreeNode
		for _, node := range model.visible {
			if node.Choice != nil {
				leaf = node
				break
			}
		}
		Expect(leaf).NotTo(BeNil())
		Expect(updateTreeChoiceChips(leaf)).To(ContainSubstring("up to date"))
		model.moveCursorTo(leaf)
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
		Expect(updated.(updateChoiceTreeModel).pendingPick).To(BeNil())
	})
})
