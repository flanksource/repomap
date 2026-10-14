package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestUpdateCLI(t *testing.T) {
	if os.Getenv("REPOMAP_UPDATE_TEST_PROCESS") == "1" {
		for i, arg := range os.Args {
			if arg == "--" {
				os.Args = append([]string{os.Args[0]}, os.Args[i+1:]...)
				break
			}
		}
		main()
		return
	}
	RegisterFailHandler(Fail)
	RunSpecs(t, "Dependency update command")
}

var _ = Describe("dependency update command failures", func() {
	It("prints one JSON result document, retains stderr, and exits nonzero", func() {
		dir := GinkgoT().TempDir()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"acme-ui","dist-tags":{"latest":"1.0.0"},"versions":{"1.0.0":{"name":"acme-ui","version":"1.0.0"}}}`))
		}))
		DeferCleanup(server.Close)
		files := map[string]string{
			"package.json":        `{"name":"web","packageManager":"pnpm@11.22.0","dependencies":{"acme-ui":"1.0.0"}}`,
			"pnpm-workspace.yaml": "packages: ['.']\n",
			".npmrc":              "registry=" + server.URL + "\nfetch-retries=0\nstore-dir=" + filepath.Join(dir, ".tmp/store") + "\n",
		}
		for name, data := range files {
			Expect(os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644)).To(Succeed())
		}
		for _, args := range [][]string{{"init", "--quiet"}, {"add", "--", "package.json", "pnpm-workspace.yaml", ".npmrc"}} {
			out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), string(out))
		}
		original, err := os.ReadFile(filepath.Join(dir, ".git/index"))
		Expect(err).NotTo(HaveOccurred())
		binary, err := os.Executable()
		Expect(err).NotTo(HaveOccurred())
		cmd := exec.Command(binary, "-test.run=TestUpdateCLI", "--", "deps", "update", "acme-ui", dir, "--version", "9.9.9", "--json", "--no-progress")
		cmd.Env = append(os.Environ(), "REPOMAP_UPDATE_TEST_PROCESS=1")
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		Expect(cmd.Run()).To(HaveOccurred())
		var rows []map[string]any
		Expect(json.Unmarshal(stdout.Bytes(), &rows)).To(Succeed(), stdout.String()+stderr.String())
		Expect(rows).To(HaveLen(1))
		Expect(rows[0]["status"]).To(ContainSubstring("9.9.9"))
		Expect(rows[0]["status"]).To(ContainSubstring("cwd"))
		Expect(rows[0]["status"]).To(ContainSubstring("NO_MATCHING_VERSION"))
		Expect(rows[0]["command"]).To(ContainSubstring("--workspace-root"))
		Expect(os.ReadFile(filepath.Join(dir, ".git/index"))).To(Equal(original))
	})
})
