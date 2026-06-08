package imageupdate

import (
	"path"
	"strings"

	"github.com/flanksource/repomap/kubernetes"
)

// KustomizeNode is one kustomization.yaml (kustomize) or a Flux Kustomization
// document. It records the namespace it imposes on the resources it includes and
// the repo-relative paths it pulls in.
type KustomizeNode struct {
	File      string // repo-relative path of the kustomization file / Flux doc
	Dir       string // path.Dir(File) — root for kustomize relative includes
	IsFlux    bool   // Flux Kustomization vs kustomize.config.k8s.io
	Namespace string // kustomize `namespace:` or Flux `spec.targetNamespace`
	Includes  []string
}

// KustomizeTree indexes the repo's kustomize/Flux include topology so a
// resource's effective (post-transform) namespace can be resolved.
type KustomizeTree struct {
	nodes     []*KustomizeNode
	byDir     map[string]*KustomizeNode   // dir -> its kustomization node
	parentsOf map[string][]*KustomizeNode // include target (file or dir) -> including nodes
}

func isKustomizationFile(file string) bool {
	base := path.Base(file)
	return base == "kustomization.yaml" || base == "kustomization.yml" || base == "Kustomization"
}

// BuildKustomizeTree parses every file's kustomize/Flux topology. files maps
// repo-relative POSIX paths to their content.
func BuildKustomizeTree(files map[string]string) *KustomizeTree {
	kt := &KustomizeTree{
		byDir:     map[string]*KustomizeNode{},
		parentsOf: map[string][]*KustomizeNode{},
	}

	for file, content := range files {
		docs, err := kubernetes.ParseYAMLDocuments(content)
		if err != nil {
			continue
		}
		for _, doc := range docs {
			if node := nodeFromDoc(file, doc.Content); node != nil {
				kt.add(node)
			}
		}
	}

	for _, n := range kt.nodes {
		for _, inc := range n.Includes {
			kt.parentsOf[inc] = append(kt.parentsOf[inc], n)
		}
	}
	return kt
}

func (kt *KustomizeTree) add(n *KustomizeNode) {
	kt.nodes = append(kt.nodes, n)
	if !n.IsFlux {
		kt.byDir[n.Dir] = n
	}
}

func nodeFromDoc(file string, m map[string]interface{}) *KustomizeNode {
	apiVersion, _ := m["apiVersion"].(string)
	kind, _ := m["kind"].(string)
	dir := path.Dir(file)

	switch {
	case strings.HasPrefix(apiVersion, "kustomize.toolkit.fluxcd.io/") && kind == "Kustomization":
		return fluxNode(file, dir, m)
	case strings.HasPrefix(apiVersion, "kustomize.config.k8s.io/"), isKustomizationFile(file):
		return kustomizeNode(file, dir, m)
	default:
		return nil
	}
}

func fluxNode(file, dir string, m map[string]interface{}) *KustomizeNode {
	spec, _ := m["spec"].(map[string]interface{})
	ns, _ := spec["targetNamespace"].(string)
	n := &KustomizeNode{File: file, Dir: dir, IsFlux: true, Namespace: ns}
	if p, ok := spec["path"].(string); ok && p != "" {
		// Flux spec.path is repo-root relative.
		n.Includes = []string{path.Clean(strings.TrimPrefix(p, "./"))}
	}
	return n
}

func kustomizeNode(file, dir string, m map[string]interface{}) *KustomizeNode {
	ns, _ := m["namespace"].(string)
	n := &KustomizeNode{File: file, Dir: dir, Namespace: ns}
	for _, key := range []string{"resources", "bases", "components"} {
		for _, rel := range stringList(m[key]) {
			n.Includes = append(n.Includes, resolveIncludePath(dir, rel))
		}
	}
	return n
}

func stringList(v interface{}) []string {
	items, ok := v.([]interface{})
	if !ok {
		return nil
	}
	var out []string
	for _, it := range items {
		if s, ok := it.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// resolveIncludePath resolves a kustomize include (node-dir relative, may use
// ./ or ../) to a repo-relative POSIX path.
func resolveIncludePath(nodeDir, rel string) string {
	rel = strings.TrimSuffix(rel, "/")
	return path.Clean(path.Join(nodeDir, rel))
}

// EffectiveNamespace resolves the namespace imposed on file by walking up its
// kustomize/Flux include chain to the root, then applying precedence downward:
// any Flux targetNamespace wins; otherwise the topmost (closest-to-root)
// kustomize namespace wins. Returns "" when nothing imposes a namespace.
func (kt *KustomizeTree) EffectiveNamespace(file string) string {
	chain := kt.includeChain(file)
	// chain is ordered leaf -> root; iterate root -> leaf for precedence.
	topmostKustomize := ""
	for i := len(chain) - 1; i >= 0; i-- {
		n := chain[i]
		if n.IsFlux && n.Namespace != "" {
			return n.Namespace
		}
		if !n.IsFlux && n.Namespace != "" && topmostKustomize == "" {
			topmostKustomize = n.Namespace
		}
	}
	return topmostKustomize
}

// includeChain returns the nodes that include file, ordered leaf -> root. It
// follows parentsOf from the file and from each including node's directory,
// guarding against cycles.
func (kt *KustomizeTree) includeChain(file string) []*KustomizeNode {
	var chain []*KustomizeNode
	seen := map[string]bool{}

	// Seed with nodes that include the file directly, plus the file's own dir node.
	frontier := append([]*KustomizeNode{}, kt.parentsOf[file]...)
	if dirNode, ok := kt.byDir[path.Dir(file)]; ok && !nodeIn(frontier, dirNode) {
		// The file's directory kustomization includes it implicitly when it lists
		// the file; its own includers are found via its dir below.
		frontier = append(frontier, dirNode)
	}

	for len(frontier) > 0 {
		n := frontier[0]
		frontier = frontier[1:]
		if seen[n.File] {
			continue
		}
		seen[n.File] = true
		chain = append(chain, n)
		// Walk up: who includes this node's directory?
		for _, parent := range kt.parentsOf[n.Dir] {
			if !seen[parent.File] {
				frontier = append(frontier, parent)
			}
		}
	}
	return chain
}

func nodeIn(nodes []*KustomizeNode, target *KustomizeNode) bool {
	for _, n := range nodes {
		if n.File == target.File {
			return true
		}
	}
	return false
}
