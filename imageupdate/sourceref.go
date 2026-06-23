package imageupdate

import (
	"fmt"
	"strings"

	"github.com/flanksource/repomap/kubernetes"
)

// controllerNamespaces are the namespaces a Flux/Helm HelmRepository commonly
// lives in, used as a tie-breaker when matching a source by name alone.
var controllerNamespaces = []string{"flux-system", "flux"}

// HelmRepositorySource is a resolved Flux HelmRepository: its URL and whether it
// is an OCI registry (oci://) versus a classic HTTP chart repo.
type HelmRepositorySource struct {
	URL   string
	IsOCI bool
}

type sourceCandidate struct {
	rawNamespace string
	effNamespace string
	src          HelmRepositorySource
}

// SourceIndex resolves a HelmRelease sourceRef to its HelmRepository, accounting
// for namespaces imposed by the Flux/kustomize tree rather than written into the
// manifests. HelmRepositories are indexed under both their raw and effective
// namespace, with a name bucket for cross-namespace fallback. OCIRepository and
// HelmChart objects (chartRef sources) are indexed the same way in chartBy*.
type SourceIndex struct {
	byKey  map[string]HelmRepositorySource
	byName map[string][]sourceCandidate

	chartByKey  map[string]chartSource
	chartByName map[string][]chartSourceCandidate

	kt *KustomizeTree
}

// NewSourceIndex returns an empty index bound to a kustomize tree. The tree may
// be nil, in which case effective namespaces equal raw namespaces.
func NewSourceIndex(kt *KustomizeTree) *SourceIndex {
	return &SourceIndex{
		byKey:       map[string]HelmRepositorySource{},
		byName:      map[string][]sourceCandidate{},
		chartByKey:  map[string]chartSource{},
		chartByName: map[string][]chartSourceCandidate{},
		kt:          kt,
	}
}

func sourceKey(namespace, name string) string {
	return namespace + "/" + name
}

func (idx *SourceIndex) effectiveNamespace(file, rawNS string) string {
	if idx.kt != nil {
		if eff := idx.kt.EffectiveNamespace(file); eff != "" {
			return eff
		}
	}
	return rawNS
}

// IndexSources parses one file and indexes every Flux chart source it contains —
// HelmRepository (a bare URL) plus OCIRepository/HelmChart (chartRef sources that
// also pin a version) — under both their raw and effective (tree-derived)
// namespace, with a name bucket for cross-namespace fallback.
func (idx *SourceIndex) IndexSources(file, content string) error {
	return forEachResourceDoc(content, func(d resourceDoc) {
		switch d.ref.Kind {
		case "HelmRepository":
			idx.indexHelmRepository(file, d.ref, d.m)
		case "OCIRepository":
			idx.indexOCIRepository(file, d.ref, d.m, d.ast, d.offset)
		case "HelmChart":
			idx.indexHelmChart(file, d.ref, d.m, d.ast, d.offset)
		}
	})
}

func (idx *SourceIndex) indexHelmRepository(file string, ref kubernetes.KubernetesRef, m map[string]interface{}) {
	spec, _ := m["spec"].(map[string]interface{})
	url, _ := spec["url"].(string)
	if url == "" {
		return
	}
	src := HelmRepositorySource{URL: url, IsOCI: strings.HasPrefix(url, "oci://")}
	effNS := idx.effectiveNamespace(file, ref.Namespace)

	idx.byKey[sourceKey(ref.Namespace, ref.Name)] = src
	idx.byKey[sourceKey(effNS, ref.Name)] = src
	idx.byName[ref.Name] = append(idx.byName[ref.Name], sourceCandidate{
		rawNamespace: ref.Namespace,
		effNamespace: effNS,
		src:          src,
	})
}

// Resolve sets RepoURL/IsOCI (and, for chartRef targets, the edit anchor) on a
// chart target. For inline sourceRef targets it matches the HelmRepository by
// effective namespace then by name across namespaces; for chartRef targets it
// redirects onto the referenced OCIRepository/HelmChart. It is idempotent — an
// already-resolved target (RepoURL set) returns immediately — and fails loud only
// when the source is genuinely unresolvable.
func (idx *SourceIndex) Resolve(t *UpdateTarget) error {
	if t.Kind != TargetChart || t.RepoURL != "" {
		return nil
	}
	if t.ChartRefName != "" {
		return idx.resolveChartRef(t)
	}
	if t.SourceRefName == "" {
		return fmt.Errorf("HelmRelease %s/%s has no sourceRef name", t.Ref.Namespace, t.Ref.Name)
	}

	wantNS := t.SourceRefNamespace
	if wantNS == "" && idx.kt != nil {
		wantNS = idx.kt.EffectiveNamespace(t.File)
	}
	src, err := idx.lookupHelmRepository(t.SourceRefName, wantNS, t.Ref)
	if err != nil {
		return err
	}
	t.RepoURL = src.URL
	t.IsOCI = src.IsOCI
	return nil
}

// lookupHelmRepository finds a HelmRepository by effective namespace, then by
// name across namespaces (a source often sits in the controller namespace, not
// the consumer's). requester names the HelmRelease for error messages.
func (idx *SourceIndex) lookupHelmRepository(name, wantNS string, requester kubernetes.KubernetesRef) (HelmRepositorySource, error) {
	if name == "" {
		return HelmRepositorySource{}, fmt.Errorf("HelmRelease %s/%s has no HelmRepository sourceRef name", requester.Namespace, requester.Name)
	}
	if src, ok := idx.byKey[sourceKey(wantNS, name)]; ok {
		return src, nil
	}
	candidates := idx.byName[name]
	switch {
	case len(candidates) == 0:
		return HelmRepositorySource{}, fmt.Errorf("HelmRelease %s/%s references HelmRepository %s which was not found in the scanned manifests",
			requester.Namespace, requester.Name, sourceKey(wantNS, name))
	case len(candidates) == 1:
		return candidates[0].src, nil
	default:
		if c, ok := pickByNamespace(candidates, wantNS,
			func(c sourceCandidate) string { return c.rawNamespace },
			func(c sourceCandidate) string { return c.effNamespace }); ok {
			return c.src, nil
		}
		return HelmRepositorySource{}, fmt.Errorf("HelmRelease %s/%s references HelmRepository %q which is ambiguous across namespaces %s",
			requester.Namespace, requester.Name, name, candidateNamespaces(candidates))
	}
}

// pickByNamespace disambiguates multiple same-named candidates: prefer an exact
// raw/effective-namespace match, then a known controller namespace.
func pickByNamespace[C any](candidates []C, wantNS string, raw, eff func(C) string) (C, bool) {
	var zero C
	for _, c := range candidates {
		if eff(c) == wantNS || raw(c) == wantNS {
			return c, true
		}
	}
	for _, ns := range controllerNamespaces {
		for _, c := range candidates {
			if eff(c) == ns || raw(c) == ns {
				return c, true
			}
		}
	}
	return zero, false
}

func candidateNamespaces(candidates []sourceCandidate) string {
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
