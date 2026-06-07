// recover-dates restores Notion Date properties for tasks whose btime was
// clobbered during the id-keyed-sync migration.
//
// It reads the pre-migration backup at ~/.lz/sync.yml.pre-id-migration.bak
// (path-keyed, older schema), filters to entries under _tasks/{done,current}/,
// matches them to the current id-keyed live state via page_id, and for each
// match sets the Notion Date to <backup.mod_time, backup.mod_time> and
// restores the live state's mod_time to the backup value.
//
// Usage:
//
//	go run ./cmd/recover-dates           # dry-run: prints plan, makes no changes
//	go run ./cmd/recover-dates --execute # acquires ~/.lz/sync.lock, applies changes
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"aliz/lz/internal/config"
	"aliz/lz/internal/sync"

	"gopkg.in/yaml.v3"
)

// backupEntry is the subset of legacy (pre-id) SyncEntry fields we need.
// The backup is path-keyed; ID and LastPath didn't exist in that schema.
type backupEntry struct {
	PageID  string    `yaml:"page_id"`
	Title   string    `yaml:"title"`
	ModTime time.Time `yaml:"mod_time"`
}

type backupFile struct {
	Entries map[string]*backupEntry `yaml:"entries"`
}

type recoveryItem struct {
	LiveID      string    // key in live state
	PageID      string
	Path        string    // backup key
	Title       string
	OldModTime  time.Time // current (migration-contaminated) live mod_time
	NewModTime  time.Time // pre-migration backup mod_time
}

func main() {
	execute := flag.Bool("execute", false, "apply changes (default: dry-run)")
	backupPath := flag.String("backup", filepath.Join(sync.StateDir(), "sync.yml.pre-id-migration.bak"), "backup file path")
	flag.Parse()

	if err := run(*execute, *backupPath); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(execute bool, backupPath string) error {
	// Load backup directly (older schema, path-keyed).
	data, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("read backup: %w", err)
	}
	var backup backupFile
	if err := yaml.Unmarshal(data, &backup); err != nil {
		return fmt.Errorf("parse backup: %w", err)
	}

	// Load live state (id-keyed).
	live, err := sync.LoadState()
	if err != nil {
		return fmt.Errorf("load live state: %w", err)
	}

	// Index live by page_id so we can match backup entries that have the same page.
	liveByPageID := make(map[string]string, len(live.Entries)) // pageID → liveID
	for id, e := range live.Entries {
		if e == nil || e.PageID == "" {
			continue
		}
		liveByPageID[e.PageID] = id
	}

	var plan []recoveryItem
	var orphans []string // backup entries whose page no longer exists in live
	for path, be := range backup.Entries {
		if be == nil {
			continue
		}
		if !strings.Contains(path, "/_tasks/done/") && !strings.Contains(path, "/_tasks/current/") {
			continue
		}
		liveID, ok := liveByPageID[be.PageID]
		if !ok {
			orphans = append(orphans, fmt.Sprintf("%s  (page %s)", path, be.PageID))
			continue
		}
		le := live.Entries[liveID]
		plan = append(plan, recoveryItem{
			LiveID:     liveID,
			PageID:     be.PageID,
			Path:       path,
			Title:      be.Title,
			OldModTime: le.ModTime,
			NewModTime: be.ModTime,
		})
	}

	sort.Slice(plan, func(i, j int) bool { return plan[i].Path < plan[j].Path })
	sort.Strings(orphans)

	printPlan(plan, orphans, execute)

	if !execute {
		return nil
	}
	if len(plan) == 0 {
		return nil
	}

	// Execute: acquire lock, update each Notion page, restore live state, save.
	gcfg, err := config.LoadGlobalConfig()
	if err != nil {
		return fmt.Errorf("load global config: %w", err)
	}
	if gcfg.Sync.Notion == nil || gcfg.Sync.Notion.APIKey == "" {
		return fmt.Errorf("notion credentials missing from ~/.lz/config.yml")
	}

	closer, err := sync.AcquireLock()
	if err != nil {
		return fmt.Errorf("acquire lock: %w", err)
	}
	defer closer.Close()

	client := sync.NewNotionClient(gcfg.Sync.Notion.APIKey, gcfg.Sync.Notion.DatabaseID, gcfg.Sync.Notion.Projects)

	var failed []string
	for i, item := range plan {
		fmt.Printf("[%d/%d] %s\n", i+1, len(plan), item.Title)
		if err := client.UpdateDate(item.PageID, item.NewModTime); err != nil {
			fmt.Fprintf(os.Stderr, "  ! notion update failed: %v\n", err)
			failed = append(failed, fmt.Sprintf("%s: %v", item.Path, err))
			continue
		}
		// Restore live state's mod_time so future syncs don't think the file changed.
		if le := live.Entries[item.LiveID]; le != nil {
			le.ModTime = item.NewModTime
		}
	}

	if err := live.Save(); err != nil {
		return fmt.Errorf("save live state: %w", err)
	}

	fmt.Printf("\nUpdated %d page(s). Live state mod_time restored from backup.\n", len(plan)-len(failed))
	if len(failed) > 0 {
		fmt.Fprintf(os.Stderr, "\n%d failure(s):\n", len(failed))
		for _, f := range failed {
			fmt.Fprintf(os.Stderr, "  - %s\n", f)
		}
		return fmt.Errorf("%d notion update(s) failed", len(failed))
	}
	return nil
}

func printPlan(plan []recoveryItem, orphans []string, execute bool) {
	mode := "DRY RUN"
	if execute {
		mode = "EXECUTE"
	}
	fmt.Printf("=== Date recovery plan (%s) ===\n", mode)
	fmt.Printf("%d page(s) will be updated:\n\n", len(plan))
	for _, item := range plan {
		fmt.Printf("  %s\n", item.Title)
		fmt.Printf("    %s\n", item.Path)
		fmt.Printf("    page:    %s\n", item.PageID)
		fmt.Printf("    date:    %s  →  %s\n",
			item.OldModTime.Format("2006-01-02"),
			item.NewModTime.Format("2006-01-02"))
		fmt.Println()
	}
	if len(orphans) > 0 {
		fmt.Printf("Skipping %d backup entr(ies) with no matching live page:\n", len(orphans))
		for _, o := range orphans {
			fmt.Printf("  - %s\n", o)
		}
		fmt.Println()
	}
}
