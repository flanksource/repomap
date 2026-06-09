package deps

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type mavenTreeNode struct {
	GroupID    string          `json:"groupId"`
	ArtifactID string          `json:"artifactId"`
	Version    string          `json:"version"`
	Type       string          `json:"type"`
	Scope      string          `json:"scope"`
	Optional   any             `json:"optional"`
	Children   []mavenTreeNode `json:"children"`
}

func resolveMavenNative(ctx context.Context, project Project, opts Options) (*Node, []Warning, error) {
	tmp, err := os.CreateTemp("", "repomap-maven-*.json")
	if err != nil {
		return nil, nil, err
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpPath)

	_, err = opts.Runner.Run(ctx, Command{
		Dir:  project.Dir,
		Name: "mvn",
		Args: []string{
			"-q",
			"org.apache.maven.plugins:maven-dependency-plugin:3.11.0:tree",
			"-DoutputType=json",
			"-DoutputFile=" + tmpPath,
		},
	})
	if err != nil {
		return nil, nil, err
	}
	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil, fmt.Errorf("maven dependency plugin produced empty JSON")
	}
	root, err := parseMavenJSON(data)
	if err != nil {
		return nil, nil, err
	}
	root.Path = project.File
	root.Source = "mvn dependency:tree"
	return root, nil, nil
}

func resolveMavenManifest(project Project) (*Node, []Warning, error) {
	root, err := parseMavenPOM(project.File)
	if err != nil {
		return nil, nil, err
	}
	return root, []Warning{{Manager: ManagerMaven, Project: project.Dir, Message: "manifest fallback includes pom.xml direct dependencies only; resolved transitive edges are unavailable"}}, nil
}

func parseMavenJSON(data []byte) (*Node, error) {
	var tree mavenTreeNode
	if err := json.Unmarshal(data, &tree); err != nil {
		return nil, err
	}
	return convertMavenTree(tree, 0), nil
}

func convertMavenTree(tree mavenTreeNode, depth int) *Node {
	name := tree.ArtifactID
	if tree.GroupID != "" {
		name = tree.GroupID + ":" + tree.ArtifactID
	}
	node := NewNode(ManagerMaven, name, tree.Version)
	node.Depth = depth
	node.Scope = tree.Scope
	node.Optional = boolish(tree.Optional)
	if tree.Type != "" {
		node.Source = tree.Type
	}
	for _, childTree := range tree.Children {
		child := convertMavenTree(childTree, depth+1)
		child.Direct = depth == 0
		node.Children = append(node.Children, child)
	}
	sortChildren(node)
	return node
}

type pomProject struct {
	XMLName      xml.Name        `xml:"project"`
	GroupID      string          `xml:"groupId"`
	ArtifactID   string          `xml:"artifactId"`
	Version      string          `xml:"version"`
	Parent       pomParent       `xml:"parent"`
	Properties   []xmlProperty   `xml:"properties>*"`
	Dependencies []pomDependency `xml:"dependencies>dependency"`
}

type pomParent struct {
	GroupID string `xml:"groupId"`
	Version string `xml:"version"`
}

type xmlProperty struct {
	XMLName xml.Name
	Value   string `xml:",chardata"`
}

type pomDependency struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
	Version    string `xml:"version"`
	Scope      string `xml:"scope"`
	Optional   string `xml:"optional"`
}

func parseMavenPOM(path string) (*Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pom pomProject
	if err := xml.Unmarshal(data, &pom); err != nil {
		return nil, err
	}
	group := firstNonEmpty(pom.GroupID, pom.Parent.GroupID)
	version := firstNonEmpty(pom.Version, pom.Parent.Version)
	props := map[string]string{
		"project.groupId":    group,
		"project.version":    version,
		"pom.groupId":        group,
		"pom.version":        version,
		"project.artifactId": pom.ArtifactID,
		"pom.artifactId":     pom.ArtifactID,
	}
	for _, prop := range pom.Properties {
		props[prop.XMLName.Local] = strings.TrimSpace(prop.Value)
	}
	root := NewNode(ManagerMaven, group+":"+pom.ArtifactID, resolveProperty(version, props))
	root.Path = path
	root.Source = "pom.xml"
	for _, dep := range pom.Dependencies {
		name := resolveProperty(dep.GroupID, props) + ":" + resolveProperty(dep.ArtifactID, props)
		child := NewNode(ManagerMaven, name, resolveProperty(dep.Version, props))
		child.Depth = 1
		child.Direct = true
		child.Scope = firstNonEmpty(dep.Scope, "compile")
		child.Optional = strings.EqualFold(strings.TrimSpace(dep.Optional), "true")
		root.Children = append(root.Children, child)
	}
	sortChildren(root)
	return root, nil
}

func resolveProperty(value string, props map[string]string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") {
		key := strings.TrimSuffix(strings.TrimPrefix(value, "${"), "}")
		if props[key] != "" {
			return props[key]
		}
	}
	return value
}

func boolish(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true")
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func mavenProjectFile(dir string) string {
	return filepath.Join(dir, "pom.xml")
}
