package deps

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/mod/modfile"
)

func resolveGoManifest(project Project, opts Options) (*Node, []Warning, error) {
	file, err := loadGoModFile(project.Dir)
	if err != nil {
		return nil, nil, err
	}
	root := NewNode(ManagerGo, file.Module.Mod.Path, "")
	root.Path = filepath.Join(project.Dir, "go.mod")
	root.Source = "go.mod"
	for _, req := range file.Require {
		if req.Indirect && !opts.IncludeIndirect {
			continue
		}
		child := NewNode(ManagerGo, req.Mod.Path, req.Mod.Version)
		child.Depth = 1
		child.Direct = !req.Indirect
		child.Scope = "require"
		if req.Indirect {
			child.Scope = "indirect"
		}
		if rep := goReplaceFor(file, req.Mod.Path, req.Mod.Version); rep != nil {
			child.Source = goReplaceSource(rep.New.Path, rep.New.Version)
			child.Local = isLocalRef(rep.New.Path)
		}
		root.Children = append(root.Children, child)
	}
	sortChildren(root)
	return root, []Warning{{Manager: ManagerGo, Project: project.Dir, Message: "offline go.mod parsing includes declared requirements only; transitive edges are unavailable"}}, nil
}

func loadGoModFile(dir string) (*modfile.File, error) {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return nil, err
	}
	return modfile.Parse("go.mod", data, nil)
}

func goReplaceFor(file *modfile.File, path, version string) *modfile.Replace {
	for _, rep := range file.Replace {
		if rep.Old.Path == path && (rep.Old.Version == "" || rep.Old.Version == version) {
			return rep
		}
	}
	return nil
}

func goReplaceSource(path, version string) string {
	if version == "" {
		return path
	}
	return fmt.Sprintf("%s@%s", path, version)
}
