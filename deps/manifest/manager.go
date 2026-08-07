// Package manifest holds the dependency-manager taxonomy and the process
// execution seam shared between the deps orchestrator and the per-manager
// packages under deps/manager. It is a leaf: nothing here imports deps, so the
// manager packages can depend on it without creating an import cycle.
package manifest

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
