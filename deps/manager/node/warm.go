// Package node holds the warming logic npm and pnpm share: generating the
// single-dependency package.json and assembling the install/verify step
// sequence. The npm and pnpm packages supply only the argv that differs.
package node

import (
	"encoding/json"
	"fmt"

	"github.com/flanksource/repomap/deps/manifest"
)

// ProjectName is the synthetic package the scratch project declares. It is
// marked private so the registry client never treats it as publishable.
const ProjectName = "repomap-cache-warm"

// Commands is how one node package manager spells the operations warming needs.
type Commands struct {
	Binary string
	// Install is the base argv that downloads and links the dependency.
	Install []string
	// IgnoreScripts suppresses dependency lifecycle scripts. It is applied unless
	// the caller asked to build, since compiling native addons is the point of
	// building.
	IgnoreScripts string
	// BuildArgs are appended when building. pnpm uses this for its version-gated
	// lifecycle-script allowlist; npm needs nothing.
	BuildArgs []string
	// Offline is the full argv for the replay that proves the cache is complete.
	Offline []string
}

// Manifest renders the scratch package.json. This is the one place repomap
// generates a package manifest rather than editing an existing one.
func Manifest(name, version string) ([]byte, error) {
	return json.MarshalIndent(struct {
		Name         string            `json:"name"`
		Version      string            `json:"version"`
		Private      bool              `json:"private"`
		Dependencies map[string]string `json:"dependencies"`
	}{
		Name:         ProjectName,
		Version:      "0.0.0",
		Private:      true,
		Dependencies: map[string]string{name: version},
	}, "", "  ")
}

func Steps(req manifest.WarmRequest, cmds Commands) ([]manifest.Step, error) {
	switch {
	case req.Dir == "":
		return nil, fmt.Errorf("%s warming needs a scratch directory", cmds.Binary)
	case req.Name == "":
		return nil, fmt.Errorf("%s warming needs a package name", cmds.Binary)
	case req.Version == "":
		return nil, fmt.Errorf("%s warming needs a version for %s", cmds.Binary, req.Name)
	}
	content, err := Manifest(req.Name, req.Version)
	if err != nil {
		return nil, err
	}

	install := append([]string{}, cmds.Install...)
	if req.Build {
		install = append(install, cmds.BuildArgs...)
	} else if cmds.IgnoreScripts != "" {
		install = append(install, cmds.IgnoreScripts)
	}

	steps := []manifest.Step{
		{Kind: manifest.StepWrite, Name: "manifest", Path: "package.json", Content: content},
		{Kind: manifest.StepExec, Name: "download", Command: manifest.Command{Dir: req.Dir, Name: cmds.Binary, Args: install}},
	}
	if req.Verify {
		// Installing over a populated node_modules is a no-op, so the tree has to
		// go before the offline replay can prove the cache holds the packages.
		steps = append(steps,
			manifest.Step{Kind: manifest.StepRemove, Name: "clean", Path: "node_modules"},
			manifest.Step{Kind: manifest.StepExec, Name: "verify", Command: manifest.Command{Dir: req.Dir, Name: cmds.Binary, Args: cmds.Offline}},
		)
	}
	return steps, nil
}
