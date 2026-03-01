package kubernetes

import (
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
)

func IsYaml(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yaml" || ext == ".yml"
}

type documentBoundary struct {
	content   string
	startLine int
	endLine   int
}

func splitYAMLDocuments(content string) []documentBoundary {
	var boundaries []documentBoundary
	lines := strings.Split(content, "\n")

	var currentDoc []string
	docStartLine := 1

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if trimmed == "---" {
			if len(currentDoc) > 0 {
				boundaries = append(boundaries, documentBoundary{
					content:   strings.Join(currentDoc, "\n"),
					startLine: docStartLine,
					endLine:   i,
				})
			}
			currentDoc = []string{}
			docStartLine = lineNum + 1
		} else {
			currentDoc = append(currentDoc, line)
		}
	}

	if len(currentDoc) > 0 {
		boundaries = append(boundaries, documentBoundary{
			content:   strings.Join(currentDoc, "\n"),
			startLine: docStartLine,
			endLine:   len(lines),
		})
	}

	return boundaries
}

func ParseYAMLDocuments(content string) ([]YAMLDocument, error) {
	boundaries := splitYAMLDocuments(content)
	var documents []YAMLDocument

	for _, boundary := range boundaries {
		var doc map[string]interface{}
		err := yaml.Unmarshal([]byte(boundary.content), &doc)
		if err != nil {
			continue
		}
		if doc == nil {
			continue
		}
		documents = append(documents, YAMLDocument{
			StartLine: boundary.startLine,
			EndLine:   boundary.endLine,
			Content:   doc,
		})
	}

	return documents, nil
}

func IsKubernetesResource(doc map[string]interface{}) bool {
	_, hasAPIVersion := doc["apiVersion"]
	_, hasKind := doc["kind"]
	return hasAPIVersion && hasKind
}

func ExtractKubernetesRef(doc YAMLDocument) KubernetesRef {
	ref := KubernetesRef{
		StartLine: doc.StartLine,
		EndLine:   doc.EndLine,
	}

	if apiVersion, ok := doc.Content["apiVersion"].(string); ok {
		ref.APIVersion = apiVersion
	}
	if kind, ok := doc.Content["kind"].(string); ok {
		ref.Kind = kind
	}

	if metadata, ok := doc.Content["metadata"].(map[string]interface{}); ok {
		if namespace, ok := metadata["namespace"].(string); ok {
			ref.Namespace = namespace
		}
		if name, ok := metadata["name"].(string); ok {
			ref.Name = name
		}
		if labels, ok := metadata["labels"].(map[string]interface{}); ok {
			ref.Labels = make(map[string]string)
			for k, v := range labels {
				if strVal, ok := v.(string); ok {
					ref.Labels[k] = strVal
				}
			}
		}
		if annotations, ok := metadata["annotations"].(map[string]interface{}); ok {
			ref.Annotations = make(map[string]string)
			for k, v := range annotations {
				if strVal, ok := v.(string); ok {
					ref.Annotations[k] = strVal
				}
			}
		}
	}

	return ref
}

func ExtractKubernetesRefsFromContent(content string) ([]KubernetesRef, error) {
	documents, err := ParseYAMLDocuments(content)
	if err != nil {
		return nil, err
	}

	var refs []KubernetesRef
	for _, doc := range documents {
		if !IsKubernetesResource(doc.Content) {
			continue
		}
		ref := ExtractKubernetesRef(doc)
		refs = append(refs, ref)
	}

	return refs, nil
}
