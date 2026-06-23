package imageupdate

import (
	"fmt"
	"strings"

	"github.com/goccy/go-yaml/ast"

	"github.com/flanksource/repomap/kubernetes"
)

// chartSource is a Flux source object that pins a concrete chart version and can
// therefore be the edit target of a chartRef HelmRelease: an OCIRepository
// (version in spec.ref.tag) or a HelmChart (version in spec.version). Unlike a
// HelmRepository, which is a bare URL, it carries the file and line of the
// version literal so Resolve can redirect the HelmRelease's target onto it.
type chartSource struct {
	kind        string // OCIRepository | HelmChart
	file        string
	versionLine int
	versionPath string
	version     string
	repoURL     string
	isOCI       bool
	chartName   string

	// srcRef chains a HelmChart to its HelmRepository for the repo URL.
	srcRefName      string
	srcRefNamespace string
}

type chartSourceCandidate struct {
	rawNamespace string
	effNamespace string
	src          chartSource
}

// extractChartRef builds a deferred chart target from a HelmRelease's
// spec.chartRef. Only OCIRepository/HelmChart references resolve to a queryable
// version source; GitRepository/Bucket references carry no chart version.
func extractChartRef(file string, ref kubernetes.KubernetesRef, chartRef map[string]interface{}) (UpdateTarget, bool) {
	kind, _ := chartRef["kind"].(string)
	name, _ := chartRef["name"].(string)
	namespace, _ := chartRef["namespace"].(string)
	if name == "" || (kind != "OCIRepository" && kind != "HelmChart") {
		return UpdateTarget{}, false
	}
	return UpdateTarget{
		Ref:               ref,
		Kind:              TargetChart,
		File:              file,
		ChartRefKind:      kind,
		ChartRefName:      name,
		ChartRefNamespace: namespace,
	}, true
}

func (idx *SourceIndex) indexOCIRepository(file string, ref kubernetes.KubernetesRef, m map[string]interface{}, doc *ast.DocumentNode, offset int) {
	spec, _ := m["spec"].(map[string]interface{})
	url, _ := spec["url"].(string)
	if url == "" {
		return
	}
	// The OCIRepository url already points at the chart artifact; split off the
	// last path segment so RepoURL+ChartName recompose to it (matching the
	// inline OCI HelmRepository convention the resolver expects).
	repoURL, chartName := splitOCIChartURL(url)
	src := chartSource{
		kind:        "OCIRepository",
		file:        file,
		versionPath: "$.spec.ref.tag",
		repoURL:     repoURL,
		isOCI:       true,
		chartName:   chartName,
	}
	if refSpec, ok := spec["ref"].(map[string]interface{}); ok {
		src.version, _ = refSpec["tag"].(string)
	}
	src.versionLine = valueLine(doc, src.versionPath, offset)
	idx.addChartSource(file, ref, src)
}

func (idx *SourceIndex) indexHelmChart(file string, ref kubernetes.KubernetesRef, m map[string]interface{}, doc *ast.DocumentNode, offset int) {
	spec, _ := m["spec"].(map[string]interface{})
	if spec == nil {
		return
	}
	src := chartSource{
		kind:        "HelmChart",
		file:        file,
		versionPath: "$.spec.version",
	}
	src.chartName, _ = spec["chart"].(string)
	src.version, _ = spec["version"].(string)
	if sr, ok := spec["sourceRef"].(map[string]interface{}); ok {
		src.srcRefName, _ = sr["name"].(string)
		src.srcRefNamespace, _ = sr["namespace"].(string)
	}
	src.versionLine = valueLine(doc, src.versionPath, offset)
	idx.addChartSource(file, ref, src)
}

func (idx *SourceIndex) addChartSource(file string, ref kubernetes.KubernetesRef, src chartSource) {
	effNS := idx.effectiveNamespace(file, ref.Namespace)
	idx.chartByKey[chartKey(src.kind, ref.Namespace, ref.Name)] = src
	idx.chartByKey[chartKey(src.kind, effNS, ref.Name)] = src
	nameKey := src.kind + "|" + ref.Name
	idx.chartByName[nameKey] = append(idx.chartByName[nameKey], chartSourceCandidate{
		rawNamespace: ref.Namespace,
		effNamespace: effNS,
		src:          src,
	})
}

// resolveChartRef redirects a chartRef target onto the OCIRepository/HelmChart it
// references: File/FieldLine/CurrentValue become that object's version literal and
// RepoURL/IsOCI are resolved (chaining a HelmChart through its HelmRepository).
func (idx *SourceIndex) resolveChartRef(t *UpdateTarget) error {
	wantNS := t.ChartRefNamespace
	if wantNS == "" && idx.kt != nil {
		wantNS = idx.kt.EffectiveNamespace(t.File)
	}
	src, err := idx.lookupChartSource(t.ChartRefKind, t.ChartRefName, wantNS, t.Ref)
	if err != nil {
		return err
	}

	switch src.kind {
	case "OCIRepository":
		t.RepoURL = src.repoURL
		t.IsOCI = src.isOCI
	case "HelmChart":
		repo, err := idx.lookupHelmRepository(src.srcRefName, chartSourceNamespace(src, wantNS), t.Ref)
		if err != nil {
			return fmt.Errorf("HelmChart %s/%s: %w", src.srcRefNamespace, src.chartName, err)
		}
		t.RepoURL = repo.URL
		t.IsOCI = repo.IsOCI
	}

	t.File = src.file
	t.FieldLine = src.versionLine
	t.FieldJSONPath = src.versionPath
	t.CurrentValue = src.version
	t.ChartName = src.chartName
	if t.CurrentValue == "" {
		return fmt.Errorf("HelmRelease %s/%s references %s %q which has no editable version (uses semver/digest, not a tag)",
			t.Ref.Namespace, t.Ref.Name, src.kind, t.ChartRefName)
	}
	return nil
}

func chartSourceNamespace(src chartSource, fallback string) string {
	if src.srcRefNamespace != "" {
		return src.srcRefNamespace
	}
	return fallback
}

func (idx *SourceIndex) lookupChartSource(kind, name, wantNS string, requester kubernetes.KubernetesRef) (chartSource, error) {
	if src, ok := idx.chartByKey[chartKey(kind, wantNS, name)]; ok {
		return src, nil
	}
	candidates := idx.chartByName[kind+"|"+name]
	switch {
	case len(candidates) == 0:
		return chartSource{}, fmt.Errorf("HelmRelease %s/%s references %s %s which was not found in the scanned manifests",
			requester.Namespace, requester.Name, kind, sourceKey(wantNS, name))
	case len(candidates) == 1:
		return candidates[0].src, nil
	default:
		if c, ok := pickByNamespace(candidates, wantNS,
			func(c chartSourceCandidate) string { return c.rawNamespace },
			func(c chartSourceCandidate) string { return c.effNamespace }); ok {
			return c.src, nil
		}
		return chartSource{}, fmt.Errorf("HelmRelease %s/%s references %s %q which is ambiguous across namespaces %s",
			requester.Namespace, requester.Name, kind, name, chartCandidateNamespaces(candidates))
	}
}

func chartKey(kind, namespace, name string) string {
	return kind + "|" + namespace + "/" + name
}

// splitOCIChartURL splits an OCIRepository url (oci://host/path/chart) into the
// parent repo url and chart name, mirroring how an inline OCI HelmRepository
// (parent url) plus chart name recompose to the full artifact path.
func splitOCIChartURL(url string) (repoURL, chartName string) {
	trimmed := strings.TrimSuffix(strings.TrimPrefix(url, "oci://"), "/")
	i := strings.LastIndex(trimmed, "/")
	if i < 0 {
		return "oci://" + trimmed, ""
	}
	return "oci://" + trimmed[:i], trimmed[i+1:]
}

func chartCandidateNamespaces(candidates []chartSourceCandidate) string {
	seen := map[string]bool{}
	var out []string
	for _, c := range candidates {
		ns := c.effNamespace
		if ns == "" {
			ns = c.rawNamespace
		}
		if ns == "" {
			ns = "(none)"
		}
		if !seen[ns] {
			seen[ns] = true
			out = append(out, ns)
		}
	}
	return strings.Join(out, ", ")
}
