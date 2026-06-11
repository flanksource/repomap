package deps

import (
	"encoding/json"
	"strings"
	"testing"
)

func sampleComparison() *Comparison {
	return &Comparison{
		Metadata: ComparisonMetadata{Path: "/repo", BaseRef: "HEAD~1", HeadRef: ""},
		Added:    []Change{{Type: ChangeAdded, Manager: ManagerGo, Name: "fresh", Project: "svc/go.mod", NewVersion: "0.1"}},
		Removed:  []Change{{Type: ChangeRemoved, Manager: ManagerGo, Name: "gone", Project: "svc/go.mod", OldVersion: "1.0"}},
		Updated:  []Change{{Type: ChangeUpdated, Manager: ManagerGo, Name: "bump", Project: "svc/go.mod", OldVersion: "1.0", NewVersion: "2.0"}},
		Statistics: ComparisonStatistics{Added: 1, Removed: 1, Updated: 1},
	}
}

func TestComparisonPrettyMarkers(t *testing.T) {
	out := sampleComparison().Pretty().String()
	for _, want := range []string{"+1", "-1", "~1", "svc/go.mod", "fresh", "(new)", "gone", "(removed)", "bump", "→", "2.0", "working tree"} {
		if !strings.Contains(out, want) {
			t.Fatalf("pretty output missing %q:\n%s", want, out)
		}
	}
}

func TestComparisonPrettyEmpty(t *testing.T) {
	out := (&Comparison{}).Pretty().String()
	if !strings.Contains(out, "No dependency changes") {
		t.Fatalf("empty comparison should report no changes, got %q", out)
	}
}

func TestComparisonPrettyPrunesToChangedProjects(t *testing.T) {
	c := sampleComparison()
	c.Added = append(c.Added, Change{Type: ChangeAdded, Manager: ManagerNPM, Name: "left-pad", Project: "web/package.json", NewVersion: "1.3.0"})
	out := c.Pretty().String()
	if !strings.Contains(out, "web/package.json") || !strings.Contains(out, "svc/go.mod") {
		t.Fatalf("both changed projects should appear:\n%s", out)
	}
}

func TestComparisonJSONShape(t *testing.T) {
	data, err := json.Marshal(sampleComparison())
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, want := range []string{`"added"`, `"removed"`, `"updated"`, `"statistics"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("json missing %q: %s", want, body)
		}
	}
	for _, unwanted := range []string{`"roots"`, `"nodes"`, `"children"`} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("json should not leak tree field %q: %s", unwanted, body)
		}
	}
}
