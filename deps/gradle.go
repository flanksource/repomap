package deps

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func resolveGradleManifest(project Project) (*Node, []Warning, error) {
	root, err := parseGradleBuildFile(project.File)
	if err != nil {
		return nil, nil, err
	}
	return root, []Warning{{Manager: ManagerGradle, Project: project.Dir, Message: "offline Gradle build-file parsing includes direct dependency declarations only; resolved transitive edges are unavailable"}}, nil
}

var gradleDepRe = regexp.MustCompile(`(?m)^\s*([A-Za-z][A-Za-z0-9_]*(?:Implementation|CompileOnly|RuntimeOnly|Api|TestImplementation|testImplementation|implementation|api|compileOnly|runtimeOnly|testRuntimeOnly|annotationProcessor|kapt)?)\s*(?:\(?\s*)["']([^:"']+):([^:"']+):([^"']+)["']`)

func parseGradleBuildFile(path string) (*Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	root := NewNode(ManagerGradle, filepath.Base(filepath.Dir(path)), "")
	root.Path = path
	root.Source = filepath.Base(path)
	for _, match := range gradleDepRe.FindAllStringSubmatch(string(data), -1) {
		scope := match[1]
		name := match[2] + ":" + match[3]
		version := match[4]
		child := NewNode(ManagerGradle, name, version)
		child.Depth = 1
		child.Direct = true
		child.Scope = scope
		child.Dev = strings.Contains(strings.ToLower(scope), "test")
		root.Children = append(root.Children, child)
	}
	sortChildren(root)
	return root, nil
}
