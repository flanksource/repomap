package repomap

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/clicky/api/icons"
)

type FileTree struct {
	Name     string
	Children []FileTree
	File     *FileMap // nil for directories
}

func NewFileTree(files []FileMap) FileTree {
	root := FileTree{Name: "."}
	for i := range files {
		root.addFile(&files[i])
	}
	root.sortChildren()
	root.collapseChains()
	return root
}

func (t *FileTree) addFile(fm *FileMap) {
	segments := strings.Split(filepath.Clean(fm.Path), string(filepath.Separator))
	current := t
	for i, seg := range segments {
		if i == len(segments)-1 {
			current.Children = append(current.Children, FileTree{Name: seg, File: fm})
		} else {
			current = current.ensureChild(seg)
		}
	}
}

func (t *FileTree) ensureChild(name string) *FileTree {
	for i := range t.Children {
		if t.Children[i].Name == name && t.Children[i].File == nil {
			return &t.Children[i]
		}
	}
	t.Children = append(t.Children, FileTree{Name: name})
	return &t.Children[len(t.Children)-1]
}

func (t *FileTree) sortChildren() {
	sort.Slice(t.Children, func(i, j int) bool {
		iDir := t.Children[i].File == nil
		jDir := t.Children[j].File == nil
		if iDir != jDir {
			return iDir // directories first
		}
		return t.Children[i].Name < t.Children[j].Name
	})
	for i := range t.Children {
		t.Children[i].sortChildren()
	}
}

func (t *FileTree) collapseChains() {
	for i := range t.Children {
		t.Children[i].collapseChains()
	}
	for i := range t.Children {
		child := &t.Children[i]
		for child.File == nil && len(child.Children) == 1 && child.Children[0].File == nil {
			child.Name = child.Name + "/" + child.Children[0].Name
			child.Children = child.Children[0].Children
		}
	}
}

func (t FileTree) Pretty() api.Text {
	if t.File != nil {
		return t.File.PrettyShort()
	}
	return clicky.Text("").Add(icons.Folder).Space().Append(t.Name+"/", "font-bold")
}

func (t FileTree) GetChildren() []api.TreeNode {
	var nodes []api.TreeNode
	for _, child := range t.Children {
		nodes = append(nodes, child)
	}
	if t.File != nil {
		nodes = append(nodes, t.File.GetChildren()...)
	}
	return nodes
}
