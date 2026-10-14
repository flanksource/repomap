package gitstage

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestGitStage(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Updater Git staging")
}

var _ = Describe("staging the updater delta", func() {
	var dir string
	git := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		out, err := cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), string(out))
		return string(out)
	}
	write := func(name, content string) {
		Expect(os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)).To(Succeed())
	}
	begin := func(paths ...string) *Session {
		s, err := Begin(context.Background(), dir, paths)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(s.Close()).To(Succeed()) })
		return s
	}
	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		git("init", "--quiet")
		write("package.json", "name=original\n\n\n\n\nversion=1\n")
		git("add", "--", "package.json")
	})
	It("preserves staged and unstaged edits in the same file", func() {
		write("package.json", "name=staged\n\n\n\n\nversion=1\n")
		git("add", "--", "package.json")
		write("package.json", "name=unstaged\n\n\n\n\nversion=1\n")
		s := begin("package.json")
		write("package.json", "name=unstaged\n\n\n\n\nversion=2\n")
		Expect(s.Stage(context.Background())).To(Equal([]string{"package.json"}))
		Expect(git("show", ":package.json")).To(Equal("name=staged\n\n\n\n\nversion=2\n"))
		Expect(os.ReadFile(filepath.Join(dir, "package.json"))).To(Equal([]byte("name=unstaged\n\n\n\n\nversion=2\n")))
	})
	It("leaves the real index byte-identical on conflicting changes", func() {
		write("package.json", "name=original\n\n\n\n\nversion=user\n")
		s := begin("package.json")
		original, err := os.ReadFile(filepath.Join(dir, ".git/index"))
		Expect(err).NotTo(HaveOccurred())
		write("package.json", "name=original\n\n\n\n\nversion=updater\n")
		_, err = s.Stage(context.Background())
		Expect(err).To(HaveOccurred())
		Expect(os.ReadFile(filepath.Join(dir, ".git/index"))).To(Equal(original))
	})
	It("stages newly generated files and deletions", func() {
		s := begin("package.json", "pnpm-lock.yaml")
		Expect(os.Remove(filepath.Join(dir, "package.json"))).To(Succeed())
		write("pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
		Expect(s.Stage(context.Background())).To(Equal([]string{"package.json", "pnpm-lock.yaml"}))
		Expect(git("ls-files")).To(Equal("pnpm-lock.yaml\n"))
	})
	It("refuses to replace an index changed by another process", func() {
		s := begin("package.json")
		write("other.txt", "another agent\n")
		git("add", "--", "other.txt")
		original, err := os.ReadFile(filepath.Join(dir, ".git/index"))
		Expect(err).NotTo(HaveOccurred())
		write("package.json", "name=original\n\n\n\n\nversion=2\n")
		_, err = s.Stage(context.Background())
		Expect(err).To(MatchError(ContainSubstring("index changed")))
		Expect(os.ReadFile(filepath.Join(dir, ".git/index"))).To(Equal(original))
	})
	It("rejects pre-existing untracked targets before mutation", func() {
		write("pnpm-lock.yaml", "user content\n")
		_, err := Begin(context.Background(), dir, []string{"pnpm-lock.yaml"})
		Expect(err).To(MatchError(ContainSubstring("untracked")))
	})
	It("stages binary content and executable mode changes", func() {
		Expect(os.WriteFile(filepath.Join(dir, "binary.dat"), []byte{0, 1, 2, 3}, 0o644)).To(Succeed())
		git("add", "--", "binary.dat")
		s := begin("binary.dat", "package.json")
		Expect(os.WriteFile(filepath.Join(dir, "binary.dat"), []byte{0, 1, 2, 4}, 0o644)).To(Succeed())
		Expect(os.Chmod(filepath.Join(dir, "package.json"), 0o755)).To(Succeed())
		Expect(s.Stage(context.Background())).To(Equal([]string{"binary.dat", "package.json"}))
		Expect([]byte(git("show", ":binary.dat"))).To(Equal([]byte{0, 1, 2, 4}))
		Expect(git("ls-files", "--stage", "--", "package.json")).To(HavePrefix("100755 "))
	})
	It("rejects unmerged targets before mutation", func() {
		hash := git("hash-object", "package.json")
		cmd := exec.Command("git", "-C", dir, "update-index", "--index-info")
		cmd.Stdin = bytes.NewBufferString("0 0000000000000000000000000000000000000000\tpackage.json\n100644 " + strings.TrimSpace(hash) + " 1\tpackage.json\n100644 " + strings.TrimSpace(hash) + " 2\tpackage.json\n")
		Expect(cmd.Run()).To(Succeed())
		_, err := Begin(context.Background(), dir, []string{"package.json"})
		Expect(err).To(MatchError(ContainSubstring("unmerged")))
	})
})
