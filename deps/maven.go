package deps

import (
	"encoding/xml"
	"os"
	"strings"
)

func resolveMavenManifest(project Project) (*Node, []Warning, error) {
	root, err := parseMavenPOM(project.File)
	if err != nil {
		return nil, nil, err
	}
	return root, []Warning{{Manager: ManagerMaven, Project: project.Dir, Message: "offline pom.xml parsing includes direct dependencies only; resolved transitive edges are unavailable"}}, nil
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
