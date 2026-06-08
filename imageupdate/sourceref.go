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
// namespace, with a name bucket for cross-namespace fallback.
type SourceIndex struct {
	byKey  map[string]HelmRepositorySource
	byName map[string][]sourceCandidate
	kt     *KustomizeTree
}

// NewSourceIndex returns an empty index bound to a kustomize tree. The tree may
// be nil, in which case effective namespaces equal raw namespaces.
func NewSourceIndex(kt *KustomizeTree) *SourceIndex {
	return &SourceIndex{
		byKey:  map[string]HelmRepositorySource{},
		byName: map[string][]sourceCandidate{},
		kt:     kt,
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

// IndexHelmRepositories parses one file and indexes every HelmRepository doc it
// contains under both its raw and effective (tree-derived) namespace. It uses
// the line-based document splitter rather than the YAML AST parser, which drops
// trailing documents in files that contain a comment-only document.
func (idx *SourceIndex) IndexHelmRepositories(file, content string) error {
	docs, err := kubernetes.ParseYAMLDocuments(content)
	if err != nil {
		return err
	}
	for _, doc := range docs {
		m := doc.Content
		if kind, _ := m["kind"].(string); kind != "HelmRepository" {
			continue
		}
		ref := kubernetes.ExtractKubernetesRef(kubernetes.YAMLDocument{Content: m})
		spec, _ := m["spec"].(map[string]interface{})
		url, _ := spec["url"].(string)
		if url == "" {
			continue
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
	return nil
}

// Resolve sets RepoURL and IsOCI on a chart target. It first tries an exact
// effective-namespace match, then falls back to matching by name in any
// namespace (a source often sits in the controller namespace, not the
// HelmRelease's). It fails loud only when the source is genuinely unresolvable.
func (idx *SourceIndex) Resolve(t *UpdateTarget) error {
	if t.Kind != TargetChart {
		return nil
	}
	if t.SourceRefName == "" {
		return fmt.Errorf("HelmRelease %s/%s has no sourceRef name", t.Ref.Namespace, t.Ref.Name)
	}

	wantNS := t.SourceRefNamespace
	if wantNS == "" && idx.kt != nil {
		wantNS = idx.kt.EffectiveNamespace(t.File)
	}

	if src, ok := idx.byKey[sourceKey(wantNS, t.SourceRefName)]; ok {
		return idx.apply(t, src)
	}

	candidates := idx.byName[t.SourceRefName]
	switch {
	case len(candidates) == 0:
		return fmt.Errorf("HelmRelease %s/%s references HelmRepository %s which was not found in the scanned manifests",
			t.Ref.Namespace, t.Ref.Name, sourceKey(wantNS, t.SourceRefName))
	case len(candidates) == 1:
		return idx.apply(t, candidates[0].src)
	default:
		if c, ok := pickCandidate(candidates, wantNS); ok {
			return idx.apply(t, c.src)
		}
		return fmt.Errorf("HelmRelease %s/%s references HelmRepository %q which is ambiguous across namespaces %s",
			t.Ref.Namespace, t.Ref.Name, t.SourceRefName, candidateNamespaces(candidates))
	}
}

func (idx *SourceIndex) apply(t *UpdateTarget, src HelmRepositorySource) error {
	t.RepoURL = src.URL
	t.IsOCI = src.IsOCI
	return nil
}

// pickCandidate disambiguates multiple same-named sources: prefer an exact
// effective-namespace match, then a known controller namespace.
func pickCandidate(candidates []sourceCandidate, wantNS string) (sourceCandidate, bool) {
	for _, c := range candidates {
		if c.effNamespace == wantNS || c.rawNamespace == wantNS {
			return c, true
		}
	}
	for _, ns := range controllerNamespaces {
		for _, c := range candidates {
			if c.effNamespace == ns || c.rawNamespace == ns {
				return c, true
			}
		}
	}
	return sourceCandidate{}, false
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
