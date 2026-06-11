package deps

import (
	"strings"
	"testing"
)

func prettyLine(out, substr string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, substr) {
			return line
		}
	}
	return ""
}

func goTreeExport() *Export {
	root := NewNode(ManagerGo, "github.com/acme/app", "")
	root.Source = "go.mod"
	root.Path = "/abs/proj/go.mod"
	root.Depth = 0

	direct := NewNode(ManagerGo, "github.com/acme/lib", "v1.2.3")
	direct.Direct = true
	direct.Scope = "require"
	direct.Depth = 1

	indirect := NewNode(ManagerGo, "github.com/acme/dep", "v0.1.0")
	indirect.Scope = "indirect"
	indirect.Depth = 2

	replaced := NewNode(ManagerGo, "github.com/acme/forked", "v2.0.0")
	replaced.Direct = true
	replaced.Scope = "require"
	replaced.Source = "github.com/fork/forked@v2.0.0"
	replaced.Depth = 1

	direct.Children = []*Node{indirect}
	root.Children = []*Node{direct, replaced}
	return &Export{Metadata: Metadata{Path: "/abs/proj"}, Roots: []*Node{root}}
}

func TestTreeOmitsManagerPrefixOnChildren(t *testing.T) {
	out := goTreeExport().Pretty().String()

	rootLine := prettyLine(out, "github.com/acme/app")
	if !strings.Contains(rootLine, "[go]") {
		t.Fatalf("root line should carry the [go] group prefix: %q", rootLine)
	}

	childLine := prettyLine(out, "github.com/acme/lib@v1.2.3")
	if childLine == "" {
		t.Fatalf("child dependency line missing from:\n%s", out)
	}
	if strings.Contains(childLine, "[go]") {
		t.Fatalf("child line must not repeat the [go] prefix: %q", childLine)
	}
}

func TestTreeRootPrintsRelativePath(t *testing.T) {
	out := goTreeExport().Pretty().String()
	rootLine := prettyLine(out, "github.com/acme/app")
	if strings.Contains(rootLine, "/abs/proj/go.mod") {
		t.Fatalf("root line should not print absolute path: %q", rootLine)
	}
	if !strings.Contains(rootLine, "go.mod") {
		t.Fatalf("root line should print relative go.mod path: %q", rootLine)
	}
}

func TestTreeDropsDefaultScopeAndDirectTags(t *testing.T) {
	out := goTreeExport().Pretty().String()

	directLine := prettyLine(out, "github.com/acme/lib@v1.2.3")
	if strings.Contains(directLine, "(direct") || strings.Contains(directLine, "require") {
		t.Fatalf("default direct/require dependency should carry no scope tag: %q", directLine)
	}

	indirectLine := prettyLine(out, "github.com/acme/dep@v0.1.0")
	if !strings.Contains(indirectLine, "indirect") {
		t.Fatalf("indirect dependency should keep its indirect tag: %q", indirectLine)
	}

	replacedLine := prettyLine(out, "github.com/acme/forked@v2.0.0")
	if !strings.Contains(replacedLine, "replaced") {
		t.Fatalf("replaced dependency should carry a replaced tag: %q", replacedLine)
	}
}

func imageTreeExport() *Export {
	root := NewNode(ManagerImage, "container images", "")
	root.Depth = 0
	root.Path = "/abs/proj"

	nginx := NewNode(ManagerImage, "nginx", "1.25.3")
	nginx.Depth = 1
	nginx.Path = "apps/web.yaml"
	nginx.setProp(propNamespace, "default")
	nginx.setProp(propKind, "Deployment")
	nginx.setProp(propResource, "web")
	nginx.setProp(propContainer, "web")

	postgres := NewNode(ManagerImage, "registry.k8s.io/postgres", "15.5")
	postgres.Depth = 1
	postgres.Path = "data/db.yaml"
	postgres.setProp(propNamespace, "data")
	postgres.setProp(propKind, "StatefulSet")
	postgres.setProp(propResource, "db")
	postgres.setProp(propContainer, "postgres")

	root.Children = []*Node{nginx, postgres}
	return &Export{Metadata: Metadata{Path: "/abs/proj"}, Roots: []*Node{root}}
}

func TestTreeGroupsKubernetesByNamespaceAndKind(t *testing.T) {
	out := imageTreeExport().Pretty().String()

	if prettyLine(out, "container images") != "" {
		t.Fatalf("synthetic container images root should not render:\n%s", out)
	}

	for _, group := range []string{"default", "Deployment", "data", "StatefulSet"} {
		if prettyLine(out, group) == "" {
			t.Fatalf("kubernetes tree missing %q group line in:\n%s", group, out)
		}
	}

	resourceLine := prettyLine(out, "apps/web.yaml")
	if resourceLine == "" || !strings.Contains(resourceLine, "web") {
		t.Fatalf("expected resource node 'web (apps/web.yaml)' in:\n%s", out)
	}

	leafLine := prettyLine(out, "nginx@1.25.3")
	if leafLine == "" {
		t.Fatalf("image leaf missing from:\n%s", out)
	}
	if strings.Contains(leafLine, "[image]") {
		t.Fatalf("image leaf must not repeat the [image] prefix: %q", leafLine)
	}
	if !strings.Contains(leafLine, "(web)") {
		t.Fatalf("image leaf should show its container in parens: %q", leafLine)
	}

	nsLine := prettyLine(out, "default")
	kindLine := prettyLine(out, "Deployment")
	if strings.Contains(nsLine, "[image]") || strings.Contains(kindLine, "[image]") {
		t.Fatalf("namespace/kind group nodes must not carry a manager prefix")
	}
}
