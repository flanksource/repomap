package deps

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("real pnpm dependency updates", func() {
	DescribeTable("updates manifests and stages only changed files", func(catalog string) {
		dir := GinkgoT().TempDir()
		registry := newUpdateRegistry()
		DeferCleanup(registry.Close)
		manifest := `{"name":"web","packageManager":"pnpm@11.22.0","dependencies":{"acme-ui":"1.0.0"}}`
		workspace := "packages: ['.', 'packages/*']\n"
		if catalog != "direct" {
			manifest = strings.ReplaceAll(manifest, `"acme-ui":"1.0.0"`, `"acme-ui":"catalog:`+catalog+`"`)
			if catalog == "" {
				workspace += "catalog:\n  acme-ui: 1.0.0 # keep this comment\n"
			} else {
				workspace += "catalogs:\n  shared:\n    acme-ui: 1.0.0 # keep this comment\n"
			}
		}
		Expect(os.WriteFile(filepath.Join(dir, "package.json"), []byte(manifest), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "pnpm-workspace.yaml"), []byte(workspace), 0o644)).To(Succeed())
		if catalog != "direct" {
			Expect(os.MkdirAll(filepath.Join(dir, "packages/mirror"), 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(dir, "packages/mirror/package.json"), []byte(manifest), 0o644)).To(Succeed())
		}
		config := fmt.Sprintf("registry=%s\nstore-dir=%s\n", registry.URL, filepath.Join(dir, ".tmp/store"))
		Expect(os.WriteFile(filepath.Join(dir, ".npmrc"), []byte(config), 0o644)).To(Succeed())
		_, err := (ExecRunner{}).Run(context.Background(), Command{Dir: dir, Name: "pnpm", Args: []string{"install", "--lockfile-only", "--ignore-scripts"}})
		Expect(err).NotTo(HaveOccurred())
		Expect(prepareUpdateTestRepo(dir)).To(Succeed())
		opts := UpdateOptions{Managers: []Manager{ManagerPNPM}, Filters: []string{"acme-ui"}, Version: "2.0.0"}
		if catalog != "direct" {
			opts.Version = ""
			opts.SelectCandidates = func(choices []UpdateChoice) ([]UpdateSelection, bool) {
				return []UpdateSelection{{UpdateChoice: choices[0], Version: "2.0.0"}}, true
			}
		}
		plans, err := Update(context.Background(), dir, opts)
		Expect(err).NotTo(HaveOccurred())
		if catalog == "direct" {
			Expect(plans).To(HaveLen(1))
		} else {
			Expect(plans).To(HaveLen(2))
			Expect(plans[0].Consumers).To(HaveLen(2))
		}
		Expect(plans[0].Written).To(BeTrue())
		Expect(plans[0].Staged).NotTo(BeEmpty())
		data, err := os.ReadFile(filepath.Join(dir, "package.json"))
		Expect(err).NotTo(HaveOccurred())
		if catalog != "direct" {
			Expect(string(data)).To(ContainSubstring("catalog:" + catalog))
			Expect(os.ReadFile(filepath.Join(dir, "pnpm-workspace.yaml"))).To(Equal([]byte(strings.Replace(workspace, "1.0.0", "2.0.0", 1))))
			Expect(plans[0].Staged).To(ContainElement(cwdRelativePath(filepath.Join(dir, "pnpm-workspace.yaml"))))
		} else {
			Expect(string(data)).To(ContainSubstring("2.0.0"))
		}
		lock, err := os.ReadFile(filepath.Join(dir, "pnpm-lock.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(lock)).To(ContainSubstring("2.0.0"))
		_, err = os.Stat(filepath.Join(dir, "node_modules"))
		Expect(os.IsNotExist(err)).To(BeTrue())
	}, Entry("root dependency", "direct"), Entry("default catalog", ""), Entry("named catalog", "shared"))
})

func newUpdateRegistry() *httptest.Server {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		defer GinkgoRecover()
		if strings.HasSuffix(req.URL.Path, ".tgz") {
			version := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(req.URL.Path), "acme-ui-"), ".tgz")
			_, err := w.Write(updatePackageArchive(version))
			Expect(err).NotTo(HaveOccurred())
			return
		}
		versions := map[string]any{}
		for _, version := range []string{"1.0.0", "2.0.0"} {
			versions[version] = map[string]any{"name": "acme-ui", "version": version, "dist": map[string]string{"tarball": server.URL + "/acme-ui/-/acme-ui-" + version + ".tgz"}}
		}
		w.Header().Set("Content-Type", "application/json")
		Expect(json.NewEncoder(w).Encode(map[string]any{"name": "acme-ui", "dist-tags": map[string]string{"latest": "2.0.0"}, "versions": versions, "time": map[string]string{"1.0.0": "2020-01-01T00:00:00Z", "2.0.0": "2020-01-02T00:00:00Z"}})).To(Succeed())
	}))
	return server
}

func updatePackageArchive(version string) []byte {
	var buffer bytes.Buffer
	zip := gzip.NewWriter(&buffer)
	archive := tar.NewWriter(zip)
	data := []byte(fmt.Sprintf(`{"name":"acme-ui","version":%q,"scripts":{"postinstall":"repomap-forbidden-lifecycle-script"}}`, version))
	Expect(archive.WriteHeader(&tar.Header{Name: "package/package.json", Mode: 0o644, Size: int64(len(data))})).To(Succeed())
	_, err := archive.Write(data)
	Expect(err).NotTo(HaveOccurred())
	Expect(archive.Close()).To(Succeed())
	Expect(zip.Close()).To(Succeed())
	return buffer.Bytes()
}
