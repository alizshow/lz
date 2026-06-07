package sync

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// SyncState tracks which local tasks have been synced to Notion.
//
// Entries is keyed by the task's stable ID (from frontmatter), not by path —
// that way moves and renames are UPDATEs, not DELETE+CREATE pairs. Legacy
// state files (keyed by absolute path) are migrated on load by Migrate.
type SyncState struct {
	Entries map[string]*SyncEntry `yaml:"entries"`
}

// SyncEntry records the Notion page ID and last-known property values for a
// synced task.
type SyncEntry struct {
	ID         string    `yaml:"id"`                  // matches the frontmatter id
	LastPath   string    `yaml:"last_path,omitempty"` // last known on-disk path; tracked, not key
	PageID     string    `yaml:"page_id"`
	Status     string    `yaml:"status"`
	Project    string    `yaml:"project"`
	Scope      string    `yaml:"scope,omitempty"`
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

// MigrateReport summarizes what Migrate did.
type MigrateReport struct {
	Rekeyed       int // legacy path-keyed entries that now live under an ID key
	OrphanEntries int // legacy entries whose file no longer exists on disk
}

// Migrate converts legacy path-keyed entries to id-keyed, scoped to root.
// Entries outside root are left untouched so a future sync from their project
// root can migrate them correctly.
//
// For each legacy entry under root:
//   - if pathToID has an id for the path, re-key using it
//   - otherwise (file vanished, entry is DELETE-pending), synthesize a random
//     id via newID so the caller's DELETE pass still fires
//
// Already-migrated entries (ID matches the map key) are left alone.
func (s *SyncState) Migrate(root string, pathToID map[string]string, newID func() string) MigrateReport {
	var rep MigrateReport
	keys := make([]string, 0, len(s.Entries))
	for k := range s.Entries {
		keys = append(keys, k)
	}
	for _, k := range keys {
		e := s.Entries[k]
		if e == nil {
			delete(s.Entries, k)
			continue
		}
		if e.ID != "" && e.ID == k {
			continue // already id-keyed
		}
		legacyPath := k
		// Scope migration to the current sync root — other roots will
		// migrate their own entries when they next sync.
		if !strings.HasPrefix(legacyPath, root) {
			continue
		}
		if e.LastPath == "" {
			e.LastPath = legacyPath
		}
		id, ok := pathToID[legacyPath]
		if !ok {
			id = newID()
			rep.OrphanEntries++
		}
		e.ID = id
		delete(s.Entries, k)
		s.Entries[id] = e
		rep.Rekeyed++
	}
	return rep
}
