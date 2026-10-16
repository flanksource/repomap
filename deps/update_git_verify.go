package deps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/repomap/deps/internal/pnpm"
	"gopkg.in/yaml.v3"
)

func verifyGitPins(ctx context.Context, requests []updateRequest, plans []UpdatePlan, opts UpdateOptions) {
	if opts.DryRun {
		return
	}
	for i, request := range requests {
		if !plans[i].Written || plans[i].Error != "" {
			continue
		}
		var err error
		switch {
		case request.Candidate.git != nil:
			err = verifyNodeGitPin(request)
		case request.Candidate.Manager == ManagerGo && validCommitSHA(request.Version):
			err = verifyGoGitPin(ctx, opts.Runner, request)
		}
		if err != nil {
			plans[i].Error = err.Error()
		}
	}
}

func verifyNodeGitPin(request updateRequest) error {
	candidate := request.Candidate
	spec, _, _ := parseNodeGitSpec(request.Version)
	if err := verifyNodeGitManifest(candidate, request.Version); err != nil {
		return err
	}
	return verifyNodeGitLock(candidate, spec.Ref)
}

func verifyNodeGitManifest(candidate UpdateCandidate, expected string) error {
	actual := ""
	if candidate.catalog != nil {
		workspace, err := pnpm.Find(candidate.Dir)
		if err != nil {
			return err
		}
		if workspace == nil {
			return fmt.Errorf("%s: pnpm workspace disappeared after update", candidate.Name)
		}
		ref, err := workspace.Resolve(candidate.Name, "catalog:"+candidate.catalog.Catalog)
		if err != nil {
			return err
		}
		actual = ref.Current
	} else {
		data, err := os.ReadFile(filepath.Join(candidate.Dir, "package.json"))
		if err != nil {
			return fmt.Errorf("read pinned manifest for %s: %w", candidate.Name, err)
		}
		var pkg packageJSON
		if err := json.Unmarshal(data, &pkg); err != nil {
			return fmt.Errorf("parse pinned manifest for %s: %w", candidate.Name, err)
		}
		switch candidate.Scope {
		case "dependencies":
			actual = pkg.Dependencies[candidate.Name]
		case "devDependencies":
			actual = pkg.DevDependencies[candidate.Name]
		case "optionalDependencies":
			actual = pkg.OptionalDependencies[candidate.Name]
		case "peerDependencies":
			actual = pkg.PeerDependencies[candidate.Name]
		}
	}
	if actual != expected {
		return fmt.Errorf("%s: manifest Git pin is %q, expected %q", candidate.Name, actual, expected)
	}
	return nil
}

func verifyNodeGitLock(candidate UpdateCandidate, sha string) error {
	lockfile := filepath.Join(candidate.Dir, "package-lock.json")
	if candidate.Manager == ManagerPNPM {
		if candidate.workspace != nil {
			lockfile = filepath.Join(candidate.workspace.Root, "pnpm-lock.yaml")
		} else {
			lockfile = filepath.Join(candidate.Dir, "pnpm-lock.yaml")
		}
	}
	data, err := os.ReadFile(lockfile)
	if err != nil {
		return fmt.Errorf("read Git lockfile for %s: %w", candidate.Name, err)
	}
	locked := ""
	if candidate.Manager == ManagerNPM {
		var lock packageLock
		if err := json.Unmarshal(data, &lock); err != nil {
			return fmt.Errorf("parse npm lockfile for %s: %w", candidate.Name, err)
		}
		item := lock.Packages["node_modules/"+candidate.Name]
		locked = item.Resolved + " " + item.Version
	} else {
		var lock struct {
			Importers map[string]struct {
				Dependencies         map[string]struct{ Version string } `yaml:"dependencies"`
				DevDependencies      map[string]struct{ Version string } `yaml:"devDependencies"`
				OptionalDependencies map[string]struct{ Version string } `yaml:"optionalDependencies"`
				PeerDependencies     map[string]struct{ Version string } `yaml:"peerDependencies"`
			} `yaml:"importers"`
		}
		if err := yaml.Unmarshal(data, &lock); err != nil {
			return fmt.Errorf("parse pnpm lockfile for %s: %w", candidate.Name, err)
		}
		root := candidate.Dir
		if candidate.workspace != nil {
			root = candidate.workspace.Root
		}
		importer, err := filepath.Rel(root, candidate.Dir)
		if err != nil {
			return err
		}
		entry := lock.Importers[filepath.ToSlash(importer)]
		switch candidate.Scope {
		case "dependencies":
			locked = entry.Dependencies[candidate.Name].Version
		case "devDependencies":
			locked = entry.DevDependencies[candidate.Name].Version
		case "optionalDependencies":
			locked = entry.OptionalDependencies[candidate.Name].Version
		case "peerDependencies":
			locked = entry.PeerDependencies[candidate.Name].Version
		}
	}
	if !strings.Contains(locked, sha) {
		return fmt.Errorf("%s: lockfile entry does not contain selected commit %s", candidate.Name, sha)
	}
	return nil
}

func verifyGoGitPin(ctx context.Context, runner CommandRunner, request updateRequest) error {
	candidate := request.Candidate
	cmd := Command{Dir: candidate.Dir, Name: "go", Args: []string{"list", "-m", "-json", candidate.Name}, Env: []string{"GOFLAGS=-mod=readonly"}}
	result, err := runner.Run(ctx, cmd)
	if err != nil {
		return fmt.Errorf("%s", commandFailure(cmd, result, err))
	}
	var module struct {
		Path   string `json:"Path"`
		Origin struct {
			Hash string `json:"Hash"`
		} `json:"Origin"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &module); err != nil {
		return fmt.Errorf("read pinned Go module %s: %w", candidate.Name, err)
	}
	if module.Path != candidate.Name || module.Origin.Hash != request.Version {
		return fmt.Errorf("%s: Go module origin is %s, expected %s", candidate.Name, module.Origin.Hash, request.Version)
	}
	return nil
}
