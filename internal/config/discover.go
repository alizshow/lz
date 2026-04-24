package config

import (
	"io/fs"
	"os"
	"path/filepath"
)

// Project is a directory containing a _tasks/ subdirectory, paired with its
// resolved config from the .lz.yml cascade.
type Project struct {
	Name   string
	Dir    string
	Scope  string // relative path from .lz.yml project root to _tasks/ parent (e.g. "kube")
	Config LzConfig
}

// hardSkip directories are always skipped regardless of config.
// Hidden dirs (.*) are also skipped unconditionally.
var hardSkip = map[string]bool{
	"node_modules": true,
	"target":       true,
	"vendor":       true,
	"build":        true,
	"venv":         true,
	"__pycache__":  true,
}

// Discover walks root recursively, collecting .lz.yml configs and _tasks/
// directories. Returns one Project per _tasks/ found, with fully resolved config.
//
// Symlinks to directories are followed, with cycle detection via resolved
// real-path tracking. Hidden dirs, hardcoded artifact dirs, and barren
// subtrees (no _tasks/ in any immediate child) are pruned.
func Discover(root string) []Project {
	var projects []Project
	visited := map[string]bool{}
	if real, err := filepath.EvalSymlinks(root); err == nil {
		visited[real] = true
	}

	rootCfg := LzConfig{}
	rootProjDir := ""
	if cfg, err := LoadProjectConfig(filepath.Join(root, ".lz.yml")); err == nil {
		rootCfg = cfg
		if cfg.Project != "" || (cfg.Sync != nil && cfg.Sync.Project != "") {
			rootProjDir = root
		}
	}

	var walk func(dir string, depth int, cfg LzConfig, projectDir string)
	walk = func(dir string, depth int, cfg LzConfig, projectDir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			name := e.Name()
			if len(name) > 0 && name[0] == '.' {
				continue
			}
			path := filepath.Join(dir, name)

			// Stat follows symlinks; filter to directories only.
			info, err := os.Stat(path)
			if err != nil || !info.IsDir() {
				continue
			}

			childDepth := depth + 1

			if cfg.MaxDepth > 0 && childDepth > cfg.MaxDepth {
				continue
			}
			if hardSkip[name] || shouldSkip(name, cfg.Skip) {
				continue
			}

			// _tasks/ directory: register parent as project, don't descend.
			if name == "_tasks" {
				projectName := resolveProjectName(cfg, root, dir)
				scope := ""
				if projectDir != "" {
					if rel, err := filepath.Rel(projectDir, dir); err == nil && rel != "." {
						scope = rel
					}
				}
				projects = append(projects, Project{
					Name:   projectName,
					Dir:    dir,
					Scope:  scope,
					Config: cfg,
				})
				continue
			}

			// Cycle detection for symlinks: skip if we've already walked the
			// target via another path.
			if e.Type()&fs.ModeSymlink != 0 {
				real, err := filepath.EvalSymlinks(path)
				if err != nil {
					continue
				}
				if visited[real] {
					continue
				}
				visited[real] = true
			}

			// Resolve child config if present.
			childCfg := cfg
			childProjDir := projectDir
			hasConfig := false
			if c, err := LoadProjectConfig(filepath.Join(path, ".lz.yml")); err == nil {
				childCfg = Merge(cfg, c)
				if c.Project != "" || (c.Sync != nil && c.Sync.Project != "") {
					childProjDir = path
				}
				hasConfig = true
			}

			// Lookahead prune: no config here and no _tasks/ in this dir or
			// its immediate children means nothing to find below.
			if !hasConfig && !hasTasksNearby(path) {
				continue
			}

			walk(path, childDepth, childCfg, childProjDir)
		}
	}

	walk(root, 0, rootCfg, rootProjDir)
	return projects
}

// hasTasksNearby checks if dir itself contains _tasks/, or if any immediate
// subdirectory of dir contains _tasks/. Stats through symlinks so symlinked
// subprojects are recognised. Prunes barren subtrees early.
func hasTasksNearby(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "_tasks")); err == nil {
		return true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		info, err := os.Stat(filepath.Join(dir, e.Name()))
		if err != nil || !info.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, e.Name(), "_tasks")); err == nil {
			return true
		}
	}
	return false
}

func shouldSkip(name string, skipList []string) bool {
	for _, s := range skipList {
		if name == s {
			return true
		}
	}
	return false
}

func resolveProjectName(cfg LzConfig, root, dir string) string {
	if cfg.Project != "" {
		return cfg.Project
	}
	rel, _ := filepath.Rel(root, dir)
	if rel == "." {
		return "root"
	}
	return rel
}
