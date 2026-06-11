package deps

import (
	"fmt"
	"io/fs"
	"os"
	osexec "os/exec"
	"path/filepath"
	"sort"
	"strings"
)

var ignoredDirs = map[string]bool{
	".git":         true,
	".gradle":      true,
	"build":        true,
	"dist":         true,
	"node_modules": true,
	"target":       true,
	"vendor":       true,
}

func Discover(root string, managers []Manager) ([]Project, []Warning, error) {
	return discover(root, managers, true)
}

func discoverOffline(root string, managers []Manager) ([]Project, []Warning, error) {
	return discover(root, managers, false)
}

func discover(root string, managers []Manager, useGit bool) ([]Project, []Warning, error) {
	selected := managerSet(managers)
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, err
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return nil, nil, err
	}
	if !info.IsDir() {
		absRoot = filepath.Dir(absRoot)
	}

	files, err := discoverManifestFiles(absRoot, useGit)
	if err != nil {
		return nil, nil, err
	}

	byDir := map[string]map[string]string{}
	for _, path := range files {
		name := filepath.Base(path)
		manager := managerForManifest(name)
		if manager == "" {
			continue
		}
		if len(selected) > 0 && !selected[manager] {
			continue
		}
		dir := filepath.Dir(path)
		if byDir[dir] == nil {
			byDir[dir] = map[string]string{}
		}
		byDir[dir][name] = path
	}

	var projects []Project
	var warnings []Warning
	for dir, files := range byDir {
		if path := files["go.work"]; path != "" {
			projects = append(projects, Project{Manager: ManagerGo, Dir: dir, File: path, Name: filepath.Base(dir)})
		} else if path := files["go.mod"]; path != "" {
			projects = append(projects, Project{Manager: ManagerGo, Dir: dir, File: path, Name: filepath.Base(dir)})
		}
		if path := files["pom.xml"]; path != "" {
			projects = append(projects, Project{Manager: ManagerMaven, Dir: dir, File: path, Name: filepath.Base(dir)})
		}
		if path := files["build.gradle"]; path != "" {
			projects = append(projects, Project{Manager: ManagerGradle, Dir: dir, File: path, Name: filepath.Base(dir)})
		} else if path := files["build.gradle.kts"]; path != "" {
			projects = append(projects, Project{Manager: ManagerGradle, Dir: dir, File: path, Name: filepath.Base(dir)})
		}
		pnpmLock := files["pnpm-lock.yaml"]
		npmLock := files["package-lock.json"]
		shrinkwrap := files["npm-shrinkwrap.json"]
		if pnpmLock != "" {
			projects = append(projects, Project{Manager: ManagerPNPM, Dir: dir, File: pnpmLock, Name: filepath.Base(dir)})
			if npmLock != "" || shrinkwrap != "" {
				warnings = append(warnings, Warning{
					Manager: ManagerPNPM,
					Project: dir,
					Message: "pnpm-lock.yaml and npm lockfile both found; using pnpm unless --manager npm is selected",
				})
				if selected[ManagerNPM] {
					if npmLock != "" {
						projects = append(projects, Project{Manager: ManagerNPM, Dir: dir, File: npmLock, Name: filepath.Base(dir)})
					} else {
						projects = append(projects, Project{Manager: ManagerNPM, Dir: dir, File: shrinkwrap, Name: filepath.Base(dir)})
					}
				}
			}
		} else if npmLock != "" {
			projects = append(projects, Project{Manager: ManagerNPM, Dir: dir, File: npmLock, Name: filepath.Base(dir)})
		} else if shrinkwrap != "" {
			projects = append(projects, Project{Manager: ManagerNPM, Dir: dir, File: shrinkwrap, Name: filepath.Base(dir)})
		} else if path := files["package.json"]; path != "" && (len(selected) == 0 || selected[ManagerNPM]) {
			projects = append(projects, Project{Manager: ManagerNPM, Dir: dir, File: path, Name: filepath.Base(dir)})
		}
	}

	sort.Slice(projects, func(i, j int) bool {
		if projects[i].Dir != projects[j].Dir {
			return projects[i].Dir < projects[j].Dir
		}
		return projects[i].Manager < projects[j].Manager
	})
	if len(projects) == 0 {
		return nil, warnings, fmt.Errorf("no supported dependency manifests found under %s", absRoot)
	}
	return projects, warnings, nil
}

func discoverManifestFiles(root string, useGit bool) ([]string, error) {
	if useGit {
		if files, ok := gitManifestFiles(root); ok {
			return files, nil
		}
	}
	return walkManifestFiles(root)
}

func gitManifestFiles(root string) ([]string, bool) {
	cmd := osexec.Command("git", "-C", root, "ls-files", "--cached", "--others", "--exclude-standard", "-z", "--", ".")
	out, err := cmd.Output()
	if err != nil {
		return nil, false
	}
	var files []string
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel == "" {
			continue
		}
		name := filepath.Base(filepath.FromSlash(rel))
		if managerForManifest(name) == "" {
			continue
		}
		files = append(files, filepath.Join(root, filepath.FromSlash(rel)))
	}
	sort.Strings(files)
	return files, true
}

func walkManifestFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && ignoredDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		manager := managerForManifest(name)
		if manager == "" {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func managerForManifest(name string) Manager {
	switch strings.ToLower(name) {
	case "go.mod", "go.work":
		return ManagerGo
	case "pom.xml":
		return ManagerMaven
	case "build.gradle", "build.gradle.kts":
		return ManagerGradle
	case "package.json", "package-lock.json", "npm-shrinkwrap.json":
		return ManagerNPM
	case "pnpm-lock.yaml":
		return ManagerPNPM
	default:
		return ""
	}
}

func managerSet(managers []Manager) map[Manager]bool {
	if len(managers) == 0 {
		return nil
	}
	selected := make(map[Manager]bool, len(managers))
	for _, m := range managers {
		if m != "" {
			selected[m] = true
		}
	}
	return selected
}
