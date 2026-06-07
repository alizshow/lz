package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LzConfig is the per-project config loaded from .lz.yml files.
// Resolved top→bottom via Merge (parent first, child overrides).
type LzConfig struct {
	Project  string             `yaml:"project"`
	Skip     []string           `yaml:"skip"`
	MaxDepth int                `yaml:"max_depth"`
	Sync     *ProjectSyncConfig `yaml:"sync,omitempty"`
}

// ProjectSyncConfig controls sync behavior per project.
type ProjectSyncConfig struct {
	Enabled  *bool  `yaml:"enabled,omitempty"`
	Project  string `yaml:"project"`
	Effort   string `yaml:"effort"`
	OnDelete string `yaml:"on_delete"`
}

// GlobalConfig is loaded from ~/.lz/config.yml (not part of the cascade).
type GlobalConfig struct {
	Sync GlobalSyncConfig `yaml:"sync"`
}

// GlobalSyncConfig holds provider selection and credentials.
type GlobalSyncConfig struct {
	Type   string        `yaml:"type"`
	Notion *NotionConfig `yaml:"notion,omitempty"`
}

// NotionConfig holds Notion API credentials and the allowlist of project
// names accepted by the Project select. Empty Projects means accept any —
// useful for first-time setup or single-project users; populate it once you
// want a guard against typos creating junk select options.
type NotionConfig struct {
	APIKey     string   `yaml:"api_key"`
	DatabaseID string   `yaml:"database_id"`
	Projects   []string `yaml:"projects,omitempty"`
}

// LoadProjectConfig reads and unmarshals a single .lz.yml file.
func LoadProjectConfig(path string) (LzConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return LzConfig{}, err
	}
	var cfg LzConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return LzConfig{}, err
	}
	return cfg, nil
}

// LoadGlobalConfig reads ~/.lz/config.yml.
func LoadGlobalConfig() (GlobalConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return GlobalConfig{}, err
	}
	path := filepath.Join(home, ".lz", "config.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		return GlobalConfig{}, err
	}
	var cfg GlobalConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return GlobalConfig{}, err
	}
	return cfg, nil
}

// Merge resolves a child config against a parent.
// Scalars: child wins if non-zero. Lists (Skip): append. Sync: child wins if non-nil,
// with per-field override within the sync block.
func Merge(parent, child LzConfig) LzConfig {
	out := parent

	if child.Project != "" {
		out.Project = child.Project
	}
	if child.MaxDepth != 0 {
		out.MaxDepth = child.MaxDepth
	}

	// Skip: append + deduplicate
	if len(child.Skip) > 0 {
		seen := make(map[string]bool, len(out.Skip))
		for _, s := range out.Skip {
			seen[s] = true
		}
		for _, s := range child.Skip {
			if !seen[s] {
				out.Skip = append(out.Skip, s)
				seen[s] = true
			}
		}
	}

	// Sync: child overrides if non-nil; merge fields within
	if child.Sync != nil {
		if out.Sync == nil {
			out.Sync = &ProjectSyncConfig{}
		} else {
			cp := *out.Sync
			out.Sync = &cp
		}
		if child.Sync.Enabled != nil {
			out.Sync.Enabled = child.Sync.Enabled
		}
		if child.Sync.Project != "" {
			out.Sync.Project = child.Sync.Project
		}
		if child.Sync.Effort != "" {
			out.Sync.Effort = child.Sync.Effort
		}
		if child.Sync.OnDelete != "" {
			out.Sync.OnDelete = child.Sync.OnDelete
		}
	}

	return out
}
