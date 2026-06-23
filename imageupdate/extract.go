package imageupdate

import (
	"fmt"

	"github.com/argoproj-labs/argocd-image-updater/registry-scanner/pkg/image"
	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"

	"github.com/flanksource/repomap/kubernetes"
)

// appsV1Kinds are the workload kinds that nest a pod template with containers.
var appsV1Kinds = map[string]bool{
	"Deployment":  true,
	"StatefulSet": true,
	"DaemonSet":   true,
	"ReplicaSet":  true,
}

// ExtractTargets parses a manifest file's content and returns every editable
// version field: apps/v1 container images and Flux HelmRelease chart versions.
// FieldLine on each target is the absolute 1-based line of the value, resolved
// from the YAML AST so the editor can replace it surgically.
//
// Documents are split with the line-based splitter (kubernetes.ParseYAMLDocuments)
// and each parsed individually: the goccy AST parser drops every document after a
// comment-only document, which would hide trailing HelmReleases/workloads. The
// per-document line offset is added back so FieldLine stays absolute in the file.
func ExtractTargets(file, content string) ([]UpdateTarget, error) {
	var targets []UpdateTarget
	err := forEachResourceDoc(content, func(d resourceDoc) {
		switch {
		case appsV1Kinds[d.ref.Kind]:
			targets = append(targets, extractImages(file, d.ast, d.ref, d.m, d.offset)...)
		case d.ref.Kind == "HelmRelease":
			if t, ok := extractChart(file, d.ast, d.ref, d.m, d.offset); ok {
				targets = append(targets, t)
			}
		}
	})
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", file, err)
	}
	return targets, nil
}

// resourceDoc is a single Kubernetes resource document with both its decoded map
// content and its single-document AST, plus the line offset that converts AST
// line positions back to absolute file lines.
type resourceDoc struct {
	ref    kubernetes.KubernetesRef
	m      map[string]interface{}
	ast    *ast.DocumentNode
	offset int
}

// forEachResourceDoc splits content into documents (line-based, so trailing docs
// after a comment-only document survive), and yields each Kubernetes resource
// with its decoded map and single-doc AST. Documents whose AST cannot be parsed
// are skipped. Shared by target extraction and source indexing so both agree on
// document boundaries and line numbers.
func forEachResourceDoc(content string, fn func(resourceDoc)) error {
	docs, err := kubernetes.ParseYAMLDocuments(content)
	if err != nil {
		return err
	}
	for _, d := range docs {
		m := d.Content
		if !kubernetes.IsKubernetesResource(m) {
			continue
		}
		ref := kubernetes.ExtractKubernetesRef(kubernetes.YAMLDocument{StartLine: d.StartLine, Content: m})
		astFile, err := parser.ParseBytes([]byte(docText(content, d)), 0)
		if err != nil || len(astFile.Docs) == 0 {
			continue
		}
		fn(resourceDoc{ref: ref, m: m, ast: astFile.Docs[0], offset: d.StartLine - 1})
	}
	return nil
}

// docText returns the raw source lines of a single document (1-based inclusive
// StartLine..EndLine) so it can be parsed in isolation.
func docText(content string, d kubernetes.YAMLDocument) string {
	lines := splitLinesKeep(content)
	start := d.StartLine - 1
	end := d.EndLine
	if start < 0 {
		start = 0
	}
	if end > len(lines) {
		end = len(lines)
	}
	if start >= end {
		return ""
	}
	return joinLines(lines[start:end])
}

func splitLinesKeep(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	out = append(out, cur)
	return out
}

func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}

func extractImages(file string, doc *ast.DocumentNode, ref kubernetes.KubernetesRef, m map[string]interface{}, offset int) []UpdateTarget {
	containers := containerList(m)
	var targets []UpdateTarget
	for i, c := range containers {
		cm, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		imageVal, ok := cm["image"].(string)
		if !ok || imageVal == "" {
			continue
		}
		path := fmt.Sprintf("$.spec.template.spec.containers[%d].image", i)
		line := valueLine(doc, path, offset)
		name, _ := cm["name"].(string)
		targets = append(targets, UpdateTarget{
			Ref:           ref,
			Kind:          TargetImage,
			File:          file,
			FieldLine:     line,
			FieldJSONPath: path,
			CurrentValue:  imageVal,
			Image:         image.NewFromIdentifier(imageVal),
			ContainerName: name,
		})
	}
	return targets
}

func containerList(m map[string]interface{}) []interface{} {
	spec, _ := m["spec"].(map[string]interface{})
	template, _ := spec["template"].(map[string]interface{})
	podSpec, _ := template["spec"].(map[string]interface{})
	containers, _ := podSpec["containers"].([]interface{})
	return containers
}

func extractChart(file string, doc *ast.DocumentNode, ref kubernetes.KubernetesRef, m map[string]interface{}, offset int) (UpdateTarget, bool) {
	spec, _ := m["spec"].(map[string]interface{})
	if spec == nil {
		return UpdateTarget{}, false
	}
	// Flux v2 spec.chartRef points at an OCIRepository/HelmChart that holds the
	// version literal; emit a deferred target that Resolve redirects onto it.
	if chartRef, ok := spec["chartRef"].(map[string]interface{}); ok {
		return extractChartRef(file, ref, chartRef)
	}
	chart, _ := spec["chart"].(map[string]interface{})
	chartSpec, _ := chart["spec"].(map[string]interface{})
	if chartSpec == nil {
		return UpdateTarget{}, false
	}
	version, ok := chartSpec["version"].(string)
	if !ok || version == "" {
		return UpdateTarget{}, false
	}
	chartName, _ := chartSpec["chart"].(string)

	srcName, srcKind, srcNS := "", "", ""
	if srcRef, ok := chartSpec["sourceRef"].(map[string]interface{}); ok {
		srcName, _ = srcRef["name"].(string)
		srcKind, _ = srcRef["kind"].(string)
		srcNS, _ = srcRef["namespace"].(string)
	}
	if srcKind != "" && srcKind != "HelmRepository" {
		// GitRepository / Bucket sources don't carry chart versions we can resolve.
		return UpdateTarget{}, false
	}
	if srcNS == "" {
		// Default to the HelmRelease's own namespace, but only when the manifest
		// actually pins one. When it doesn't (the namespace is imposed by the
		// Flux/kustomize tree at apply time), leave it empty so Resolve derives
		// the effective namespace from the tree instead of treating "" as real.
		srcNS = ref.Namespace
	}

	path := "$.spec.chart.spec.version"
	return UpdateTarget{
		Ref:                ref,
		Kind:               TargetChart,
		File:               file,
		FieldLine:          valueLine(doc, path, offset),
		FieldJSONPath:      path,
		CurrentValue:       version,
		ChartName:          chartName,
		SourceRefName:      srcName,
		SourceRefNamespace: srcNS,
	}, true
}

// valueLine resolves the 1-based line of the value at a YAML path within a
// single-document parse, adding offset to yield the absolute file line. Returns
// 0 when the path cannot be resolved.
func valueLine(doc *ast.DocumentNode, pathStr string, offset int) int {
	path, err := yaml.PathString(pathStr)
	if err != nil {
		return 0
	}
	node, err := path.FilterNode(doc.Body)
	if err != nil || node == nil {
		return 0
	}
	tok := node.GetToken()
	if tok == nil {
		return 0
	}
	return tok.Position.Line + offset
}
