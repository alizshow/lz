package sync

import (
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// SyncState tracks which local tasks have been synced to Notion.
type SyncState struct {
	Entries map[string]*SyncEntry `yaml:"entries"`
}

// SyncEntry records the Notion page ID and last-known property values
// for a synced task. Keyed by absolute file path in SyncState.Entries.
type SyncEntry struct {
	PageID     string    `yaml:"page_id"`
	Status     string    `yaml:"status"`
	Project    string    `yaml:"project"`
	Title      string    `yaml:"title"`
	Effort     string    `yaml:"effort"`
	ModTime    time.Time `yaml:"mod_time"`
	LastSynced time.Time `yaml:"last_synced"`
}

// StateDir returns ~/.lz/, used for state, lock, and log files.
func StateDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".lz")
}

// LoadState reads ~/.lz/sync.yml. Returns empty state if the file doesn't exist.
func LoadState() (*SyncState, error) {
	path := filepath.Join(StateDir(), "sync.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &SyncState{Entries: make(map[string]*SyncEntry)}, nil
		}
		return nil, err
	}
	var s SyncState
	if err := yaml.Unmarshal(data, &s); err != nil {
		return &SyncState{Entries: make(map[string]*SyncEntry)}, nil
	}
	if s.Entries == nil {
		s.Entries = make(map[string]*SyncEntry)
	}
	return &s, nil
}

// Save writes the state back to ~/.lz/sync.yml.
func (s *SyncState) Save() error {
	dir := StateDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "sync.yml"), data, 0o644)
}
