package main

import (
	"context"
	"os"
	"path/filepath"

	depgraph "github.com/flanksource/repomap/deps"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GitHub Actions CLI manager", func() {
	It("accepts actions for scanning and updating", func() {
		managers, err := parseManagers([]string{"actions"})
		Expect(err).NotTo(HaveOccurred())
		Expect(managers).To(Equal([]depgraph.Manager{depgraph.ManagerActions}))
		managers, err = parseUpdateManagers([]string{"actions"})
		Expect(err).NotTo(HaveOccurred())
		Expect(managers).To(Equal([]depgraph.Manager{depgraph.ManagerActions}))
	})

	It("scans a workflow-only repository through the deps command", func() {
		dir := GinkgoT().TempDir()
		file := filepath.Join(dir, ".github", "workflows", "ci.yaml")
		Expect(os.MkdirAll(filepath.Dir(file), 0o755)).To(Succeed())
		Expect(os.WriteFile(file, []byte("jobs:\n  build:\n    steps:\n      - uses: actions/checkout@v4\n"), 0o644)).To(Succeed())
		export, err := runDeps(context.Background(), DepsOptions{Path: dir, Manager: []string{"actions"}, Depth: 1})
		Expect(err).NotTo(HaveOccurred())
		Expect(export.Roots).To(HaveLen(1))
		Expect(export.Roots[0].Children).To(HaveLen(1))
		Expect(export.Roots[0].Children[0].Name).To(Equal("actions/checkout"))
	})
})
