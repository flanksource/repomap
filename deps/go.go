package deps

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
)

type goModuleInfo struct {
	Path    string        `json:"Path"`
	Version string        `json:"Version"`
	Main    bool          `json:"Main"`
	Replace *goModuleInfo `json:"Replace"`
	Dir     string        `json:"Dir"`
}

func resolveGoNative(ctx context.Context, project Project, opts Options) (*Node, []Warning, error) {
	graphResult, err := opts.Runner.Run(ctx, Command{
		Dir:  project.Dir,
		Name: "go",
		Args: []string{"mod", "graph"},
		Env:  []string{"GOFLAGS=-mod=readonly"},
	})
	if err != nil {
		return nil, nil, err
	}
	listResult, listErr := opts.Runner.Run(ctx, Command{
		Dir:  project.Dir,
		Name: "go",
		Args: []string{"list", "-m", "-json", "all"},
		Env:  []string{"GOFLAGS=-mod=readonly"},
	})
	infos := map[string]goModuleInfo{}
	if listErr == nil {
		infos = parseGoModuleInfos([]byte(listResult.Stdout))
	}

	rootToken, err := goRootToken(project, infos)
	if err != nil {
		return nil, nil, err
	}
	direct := goDirectRequires(project.File)
	root := buildGoGraph(rootToken, parseGoGraph(graphResult.Stdout), infos, direct)
	root.Path = project.File
	root.Source = "go mod graph"
	if listErr != nil {
		return root, []Warning{{Manager: ManagerGo, Project: project.Dir, Message: "go list -m -json all failed; replacement metadata may be incomplete: " + listErr.Error()}}, nil
	}
	return root, nil, nil
}

func resolveGoManifest(project Project) (*Node, []Warning, error) {
	data, err := os.ReadFile(filepath.Join(project.Dir, "go.mod"))
	if err != nil {
		return nil, nil, err
	}
	file, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return nil, nil, err
	}
	root := NewNode(ManagerGo, file.Module.Mod.Path, "")
	root.Path = filepath.Join(project.Dir, "go.mod")
	root.Source = "go.mod"
	for _, req := range file.Require {
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
	return root, []Warning{{Manager: ManagerGo, Project: project.Dir, Message: "manifest fallback includes go.mod requirements only; transitive edges are unavailable"}}, nil
}

func parseGoModuleInfos(data []byte) map[string]goModuleInfo {
	out := map[string]goModuleInfo{}
	dec := json.NewDecoder(bytes.NewReader(data))
	for {
		var info goModuleInfo
		if err := dec.Decode(&info); err != nil {
			break
		}
		if info.Path == "" {
			continue
		}
		out[goToken(info.Path, info.Version)] = info
		if info.Main {
			out[info.Path] = info
		}
	}
	return out
}

func parseGoGraph(stdout string) map[string][]string {
	out := map[string][]string{}
	scanner := bufio.NewScanner(strings.NewReader(stdout))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		out[fields[0]] = append(out[fields[0]], fields[1])
	}
	for key := range out {
		sortStrings(out[key])
	}
	return out
}

func goRootToken(project Project, infos map[string]goModuleInfo) (string, error) {
	for token, info := range infos {
		if info.Main {
			return token, nil
		}
	}
	data, err := os.ReadFile(filepath.Join(project.Dir, "go.mod"))
	if err != nil {
		return "", err
	}
	file, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return "", err
	}
	return file.Module.Mod.Path, nil
}

func goDirectRequires(path string) map[string]bool {
	out := map[string]bool{}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	file, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return out
	}
	for _, req := range file.Require {
		out[goToken(req.Mod.Path, req.Mod.Version)] = !req.Indirect
	}
	return out
}

func buildGoGraph(rootToken string, edges map[string][]string, infos map[string]goModuleInfo, direct map[string]bool) *Node {
	var build func(token string, depth int, path map[string]bool) *Node
	build = func(token string, depth int, path map[string]bool) *Node {
		name, version := splitGoToken(token)
		node := NewNode(ManagerGo, name, version)
		node.Depth = depth
		node.Source = "go mod graph"
		if depth == 1 {
			if isDirect, ok := direct[token]; ok {
				node.Direct = isDirect
				if isDirect {
					node.Scope = "require"
				} else {
					node.Scope = "indirect"
				}
			}
		}
		if info, ok := infos[token]; ok && info.Replace != nil {
			node.Source = goReplaceSource(info.Replace.Path, info.Replace.Version)
			node.Local = isLocalRef(info.Replace.Path) || info.Replace.Dir != ""
		}
		if path[token] {
			node.Circular = true
			return node
		}
		path[token] = true
		for _, childToken := range edges[token] {
			child := build(childToken, depth+1, cloneBoolMap(path))
			node.Children = append(node.Children, child)
		}
		sortChildren(node)
		return node
	}
	return build(rootToken, 0, map[string]bool{})
}

func splitGoToken(token string) (string, string) {
	name, version, ok := strings.Cut(token, "@")
	if !ok {
		return token, ""
	}
	return name, version
}

func goToken(path, version string) string {
	if version == "" {
		return path
	}
	return path + "@" + version
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
