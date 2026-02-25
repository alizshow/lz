package git

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
)

// Repo is a named git repository path.
type Repo struct {
	Name string
	Path string
}

// Discover finds git repos. If stdin is a pipe, reads name\tpath pairs.
// Otherwise scans dir and 1-level children for .git/ dirs.
func Discover(dir string) ([]Repo, error) {
	fi, err := os.Stdin.Stat()
	if err == nil && fi.Mode()&os.ModeCharDevice == 0 {
		return discoverFromStdin(dir)
	}
	return discoverFromDir(dir)
}

func discoverFromStdin(root string) ([]Repo, error) {
	var repos []Repo
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		name, path, ok := splitTab(line)
		if !ok {
			continue
		}
		absPath := path
		if !filepath.IsAbs(path) {
			absPath = filepath.Join(root, path)
		}
		if isGitDir(absPath) {
			repos = append(repos, Repo{Name: name, Path: absPath})
		}
	}
	return repos, scanner.Err()
}

func discoverFromDir(root string) ([]Repo, error) {
	var repos []Repo

	// current dir
	if isGitDir(root) {
		repos = append(repos, Repo{Name: ".", Path: root})
	}

	// 1-level children
	entries, err := os.ReadDir(root)
	if err != nil {
		return repos, fmt.Errorf("reading %s: %w", root, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		child := filepath.Join(root, e.Name())
		if isGitDir(child) {
			repos = append(repos, Repo{Name: e.Name(), Path: child})
		}
	}
	return repos, nil
}

func isGitDir(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && info.IsDir()
}

func splitTab(s string) (string, string, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == '\t' {
			return s[:i], s[i+1:], true
		}
	}
	return "", "", false
}
