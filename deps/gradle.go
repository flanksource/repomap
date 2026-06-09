package deps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type gradleExport struct {
	Projects []gradleProject `json:"projects"`
}

type gradleProject struct {
	Name           string                `json:"name"`
	Path           string                `json:"path"`
	Configurations []gradleConfiguration `json:"configurations"`
}

type gradleConfiguration struct {
	Name         string       `json:"name"`
	Dependencies []gradleNode `json:"dependencies"`
}

type gradleNode struct {
	Group    string       `json:"group"`
	Module   string       `json:"module"`
	Version  string       `json:"version"`
	Selected string       `json:"selected"`
	Children []gradleNode `json:"children"`
}

func resolveGradleNative(ctx context.Context, project Project, opts Options) (*Node, []Warning, error) {
	tmp, err := os.CreateTemp("", "repomap-gradle-*.json")
	if err != nil {
		return nil, nil, err
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpPath)

	initFile, err := os.CreateTemp("", "repomap-gradle-*.gradle")
	if err != nil {
		return nil, nil, err
	}
	initPath := initFile.Name()
	if _, err := initFile.WriteString(gradleInitScript(tmpPath, opts.Configurations)); err != nil {
		_ = initFile.Close()
		return nil, nil, err
	}
	_ = initFile.Close()
	defer os.Remove(initPath)

	bin := "gradle"
	args := []string{"-I", initPath, "-q", "repomapDeps"}
	if _, err := os.Stat(filepath.Join(project.Dir, "gradlew")); err == nil {
		bin = "./gradlew"
	}
	_, err = opts.Runner.Run(ctx, Command{Dir: project.Dir, Name: bin, Args: args})
	if err != nil {
		return nil, nil, err
	}
	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, nil, err
	}
	root, err := parseGradleJSON(data, project)
	if err != nil {
		return nil, nil, err
	}
	root.Path = project.File
	root.Source = "gradle ResolutionResult"
	return root, nil, nil
}

func resolveGradleManifest(project Project) (*Node, []Warning, error) {
	root, err := parseGradleBuildFile(project.File)
	if err != nil {
		return nil, nil, err
	}
	return root, []Warning{{Manager: ManagerGradle, Project: project.Dir, Message: "manifest fallback includes direct Gradle dependency declarations only; resolved transitive edges are unavailable"}}, nil
}

func gradleInitScript(outputPath string, configurations []string) string {
	quotedOutput := strings.ReplaceAll(outputPath, "\\", "\\\\")
	quotedOutput = strings.ReplaceAll(quotedOutput, "'", "\\'")
	var configSet string
	if len(configurations) > 0 {
		var quoted []string
		for _, cfg := range configurations {
			cfg = strings.TrimSpace(cfg)
			if cfg != "" {
				quoted = append(quoted, "'"+strings.ReplaceAll(cfg, "'", "\\'")+"'")
			}
		}
		configSet = "[" + strings.Join(quoted, ",") + "] as Set"
	} else {
		configSet = "[] as Set"
	}
	return fmt.Sprintf(`
import groovy.json.JsonOutput
gradle.projectsEvaluated {
  rootProject.tasks.register('repomapDeps') {
    doLast {
      def selectedConfigurations = %s
      def seen = [] as Set
      def convert
      convert = { dep, depth ->
        def id = dep.selected.id
        def group = id.hasProperty('group') ? id.group : ''
        def module = id.hasProperty('module') ? id.module : id.displayName
        def version = id.hasProperty('version') ? id.version : ''
        def key = group + ':' + module + ':' + version
        if (seen.contains(key + ':' + depth)) {
          return [group: group, module: module, version: version, children: []]
        }
        seen.add(key + ':' + depth)
        return [group: group, module: module, version: version, selected: id.displayName,
          children: dep.selected.dependencies.findAll { it instanceof org.gradle.api.artifacts.result.ResolvedDependencyResult }.collect { convert(it, depth + 1) }]
      }
      def projects = []
      allprojects.each { prj ->
        def configs = []
        prj.configurations.findAll { it.canBeResolved && (selectedConfigurations.isEmpty() || selectedConfigurations.contains(it.name)) }.each { cfg ->
          try {
            configs << [name: cfg.name, dependencies: cfg.incoming.resolutionResult.root.dependencies.findAll { it instanceof org.gradle.api.artifacts.result.ResolvedDependencyResult }.collect { convert(it, 1) }]
          } catch (Throwable ignored) {}
        }
        if (!configs.isEmpty()) {
          projects << [name: prj.name, path: prj.path, configurations: configs]
        }
      }
      new File('%s').text = JsonOutput.toJson([projects: projects])
    }
  }
}
`, configSet, quotedOutput)
}

func parseGradleJSON(data []byte, project Project) (*Node, error) {
	var payload gradleExport
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	root := NewNode(ManagerGradle, filepath.Base(project.Dir), "")
	root.Source = "gradle"
	for _, p := range payload.Projects {
		projectNode := NewNode(ManagerGradle, firstNonEmpty(p.Path, p.Name), "")
		projectNode.Depth = 1
		projectNode.Direct = true
		projectNode.Source = "project"
		for _, cfg := range p.Configurations {
			cfgNode := NewNode(ManagerGradle, cfg.Name, "")
			cfgNode.Depth = 2
			cfgNode.Scope = cfg.Name
			cfgNode.Source = "configuration"
			for _, dep := range cfg.Dependencies {
				child := convertGradleNode(dep, 3, cfg.Name)
				child.Direct = true
				cfgNode.Children = append(cfgNode.Children, child)
			}
			sortChildren(cfgNode)
			projectNode.Children = append(projectNode.Children, cfgNode)
		}
		sortChildren(projectNode)
		root.Children = append(root.Children, projectNode)
	}
	sortChildren(root)
	return root, nil
}

func convertGradleNode(dep gradleNode, depth int, scope string) *Node {
	name := dep.Module
	if dep.Group != "" {
		name = dep.Group + ":" + dep.Module
	}
	if name == "" {
		name = dep.Selected
	}
	node := NewNode(ManagerGradle, name, dep.Version)
	node.Depth = depth
	node.Scope = scope
	node.Source = "gradle"
	for _, childDep := range dep.Children {
		node.Children = append(node.Children, convertGradleNode(childDep, depth+1, scope))
	}
	sortChildren(node)
	return node
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
