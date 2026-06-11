package deps

import "time"

type Manager string

const (
	ManagerGo     Manager = "go"
	ManagerMaven  Manager = "maven"
	ManagerGradle Manager = "gradle"
	ManagerNPM    Manager = "npm"
	ManagerPNPM   Manager = "pnpm"
	ManagerImage  Manager = "image"
	ManagerHelm   Manager = "helm"
)

type Mode string

const (
	ModeManifest Mode = "manifest"
)

type Options struct {
	Managers        []Manager
	Mode            Mode
	MaxDepth        int
	Filters         []string
	Flat            bool
	IncludeIndirect bool
	Runner          CommandRunner
	Now             func() time.Time
}

type Project struct {
	Manager Manager `json:"manager"`
	Dir     string  `json:"dir"`
	File    string  `json:"file"`
	Name    string  `json:"name,omitempty"`
}

type Export struct {
	Metadata   Metadata    `json:"metadata"`
	Roots      []*Node     `json:"roots,omitempty"`
	Nodes      []FlatNode  `json:"nodes,omitempty"`
	Edges      []Edge      `json:"edges,omitempty"`
	Statistics Statistics  `json:"statistics"`
	Duplicates []Duplicate `json:"duplicates,omitempty"`
	Warnings   []Warning   `json:"warnings,omitempty"`
}

type Metadata struct {
	ExportedAt      time.Time `json:"exported_at"`
	Version         string    `json:"version"`
	Path            string    `json:"path"`
	Managers        []Manager `json:"managers,omitempty"`
	Mode            Mode      `json:"mode"`
	Filter          []string  `json:"filter,omitempty"`
	MaxDepth        int       `json:"max_depth,omitempty"`
	Flat            bool      `json:"flat,omitempty"`
	ProjectsScanned int       `json:"projects_scanned"`
}

type Node struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Version    string  `json:"version,omitempty"`
	Manager    Manager `json:"manager"`
	Scope      string  `json:"scope,omitempty"`
	Source     string  `json:"source,omitempty"`
	Path       string  `json:"path,omitempty"`
	Direct     bool    `json:"direct,omitempty"`
	Dev        bool    `json:"dev,omitempty"`
	Optional   bool    `json:"optional,omitempty"`
	Local      bool    `json:"local,omitempty"`
	Depth      int     `json:"depth"`
	Circular   bool    `json:"circular,omitempty"`
	Duplicate  *DupRef `json:"duplicate,omitempty"`
	Children   []*Node `json:"children,omitempty"`
	properties map[string]string
}

type FlatNode struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Version  string  `json:"version,omitempty"`
	Manager  Manager `json:"manager"`
	Scope    string  `json:"scope,omitempty"`
	Source   string  `json:"source,omitempty"`
	Path     string  `json:"path,omitempty"`
	Direct   bool    `json:"direct,omitempty"`
	Dev      bool    `json:"dev,omitempty"`
	Optional bool    `json:"optional,omitempty"`
	Local    bool    `json:"local,omitempty"`
	Depth    int     `json:"depth"`
}

type Edge struct {
	From     string  `json:"from"`
	To       string  `json:"to"`
	Manager  Manager `json:"manager"`
	Scope    string  `json:"scope,omitempty"`
	Dev      bool    `json:"dev,omitempty"`
	Optional bool    `json:"optional,omitempty"`
}

type Statistics struct {
	Projects   int             `json:"projects"`
	Total      int             `json:"total"`
	Edges      int             `json:"edges"`
	ByManager  map[Manager]int `json:"by_manager"`
	MaxDepth   int             `json:"max_depth"`
	Duplicates int             `json:"duplicates"`
	Conflicts  int             `json:"conflicts"`
	Circular   int             `json:"circular_references"`
}

type Duplicate struct {
	Name      string              `json:"name"`
	Manager   Manager             `json:"manager"`
	Count     int                 `json:"count"`
	Conflicts bool                `json:"conflicts"`
	Versions  map[string][]string `json:"versions"`
}

type DupRef struct {
	Count     int  `json:"count"`
	Conflicts bool `json:"conflicts"`
}

type Warning struct {
	Manager Manager `json:"manager,omitempty"`
	Project string  `json:"project,omitempty"`
	Message string  `json:"message"`
}

// Structured node properties carried out-of-band for display grouping (not serialized).
const (
	propNamespace = "namespace"
	propKind      = "kind"
	propResource  = "resource"
	propContainer = "container"
)

func (n *Node) setProp(key, value string) {
	if value == "" {
		return
	}
	if n.properties == nil {
		n.properties = map[string]string{}
	}
	n.properties[key] = value
}

func (n *Node) prop(key string) string {
	if n == nil || n.properties == nil {
		return ""
	}
	return n.properties[key]
}

func NewNode(manager Manager, name, version string) *Node {
	return &Node{
		ID:      NodeID(manager, name, version),
		Name:    name,
		Version: version,
		Manager: manager,
	}
}

func NodeID(manager Manager, name, version string) string {
	if version == "" {
		return string(manager) + ":" + name
	}
	return string(manager) + ":" + name + "@" + version
}

func (n *Node) cloneShallow() *Node {
	if n == nil {
		return nil
	}
	cp := *n
	cp.Children = nil
	cp.Duplicate = nil
	cp.properties = nil
	if n.properties != nil {
		cp.properties = make(map[string]string, len(n.properties))
		for k, v := range n.properties {
			cp.properties[k] = v
		}
	}
	return &cp
}
