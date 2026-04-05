package config

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Project is a directory containing a _tasks/ subdirectory, paired with its
// resolved config from the .lz.yml cascade.
type Project struct {
	Name   string
	Dir    string
	Config LzConfig
}

// hardSkip directories are always skipped regardless of config.
// Hidden dirs (.*) are also skipped unconditionally — see walk logic.
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
// Performance: hidden dirs are skipped, hardcoded build artifact dirs are skipped,
// and barren subtrees (no _tasks/ in any immediate child) are pruned via lookahead.
func Discover(root string) []Project {
	var projects []Project

	type stackEntry struct {
		depth  int
		config LzConfig
	}
	stack := []stackEntry{{depth: -1, config: LzConfig{}}}

	// Load root-level .lz.yml if present (before the walk, so the root
	// directory's own config is available when _tasks/ is encountered).
	if cfg, err := LoadProjectConfig(filepath.Join(root, ".lz.yml")); err == nil {
		stack = append(stack, stackEntry{depth: 0, config: Merge(stack[0].config, cfg)})
	}

	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			return nil
		}

		name := d.Name()

		// Skip all hidden directories (covers .git, .metals, .bloop, .gradle,
		// .idea, .bsp, .scala-build, .dart_tool, .venv, .vscode, etc.)
		if len(name) > 0 && name[0] == '.' && name != "." {
			return filepath.SkipDir
		}

		if hardSkip[name] {
			return filepath.SkipDir
		}

		rel, _ := filepath.Rel(root, path)
		depth := 0
		if rel != "." {
			depth = strings.Count(rel, string(os.PathSeparator)) + 1
		}

		// Pop stack entries at or beyond current depth.
		for len(stack) > 1 && stack[len(stack)-1].depth >= depth {
			stack = stack[:len(stack)-1]
		}
		current := stack[len(stack)-1].config

		// Skip directories matching config skip list.
		if depth > 0 && shouldSkip(name, current.Skip) {
			return filepath.SkipDir
		}

		// Respect max_depth.
		if current.MaxDepth > 0 && depth > current.MaxDepth {
			return filepath.SkipDir
		}

		// _tasks/ directory: register parent as project, don't descend.
		if name == "_tasks" {
			parentDir := filepath.Dir(path)
			parentConfig := current
			projectName := resolveProjectName(parentConfig, root, parentDir)
			projects = append(projects, Project{
				Name:   projectName,
				Dir:    parentDir,
				Config: parentConfig,
			})
			return filepath.SkipDir
		}

		// Load .lz.yml if present in this directory (skip root — already loaded).
		hasConfig := false
		if depth > 0 {
			if cfg, err := LoadProjectConfig(filepath.Join(path, ".lz.yml")); err == nil {
				current = Merge(current, cfg)
				stack = append(stack, stackEntry{depth: depth, config: current})
				hasConfig = true
			}
		}

		// Lookahead: if this dir (not root) has no .lz.yml and no _tasks/,
		// check if any immediate child has _tasks/. If none, prune the subtree.
		if depth > 0 && !hasConfig {
			if !hasTasksNearby(path) {
				return filepath.SkipDir
			}
		}

		return nil
	})

	return projects
}

// hasTasksNearby checks if dir itself contains _tasks/, or if any immediate
// subdirectory of dir contains _tasks/. Prunes barren subtrees early.
func hasTasksNearby(dir string) bool {
	// Check dir/_tasks/ directly.
	if _, err := os.Stat(filepath.Join(dir, "_tasks")); err == nil {
		return true
	}
	// Check dir/child/_tasks/ (one level deeper).
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
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
