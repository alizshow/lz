package sync

import (
	"fmt"
	"strings"
	"time"

	"aliz/lz/internal/config"
	"aliz/lz/internal/task"

	"github.com/rclod/notion-go"
)

type action int

const (
	actionCreate action = iota
	actionUpdate
	actionDelete
	actionSkip
)

type planEntry struct {
	Action  action
	Path    string
	Task    *task.Task
	Entry   *SyncEntry
	Config  *config.LzConfig
	Changes []string // human-readable diffs for updates
}

// syncEnabled returns true if a project's resolved config has sync turned on.
// Sync requires an explicit sync: block. nil Sync = not enabled.
// Sync.Enabled nil (within a sync block) = enabled (opt-in by having the block).
// Sync.Enabled false = explicitly disabled.
func syncEnabled(cfg *config.LzConfig) bool {
	if cfg == nil || cfg.Sync == nil {
		return false
	}
	if cfg.Sync.Enabled != nil && !*cfg.Sync.Enabled {
		return false
	}
	return true
}

// syncProject returns the Notion Project select value for a task.
func syncProject(cfg *config.LzConfig) string {
	if cfg != nil && cfg.Sync != nil && cfg.Sync.Project != "" {
		return cfg.Sync.Project
	}
	if cfg != nil && cfg.Project != "" {
		return cfg.Project
	}
	return ""
}

// syncEffort returns the effort string for a task, falling back to config default.
func syncEffort(t *task.Task, cfg *config.LzConfig) string {
	e := t.Effort.String()
	if e == "" && cfg != nil && cfg.Sync != nil && cfg.Sync.Effort != "" {
		return cfg.Sync.Effort
	}
	return e
}

// syncScope returns the scope for a task (sub-project within a project).
func syncScope(t *task.Task) string {
	return t.Scope
}

// onDelete returns the delete strategy for a project (default: "archive").
func onDelete(cfg *config.LzConfig) string {
	if cfg != nil && cfg.Sync != nil && cfg.Sync.OnDelete != "" {
		return cfg.Sync.OnDelete
	}
	return "archive"
}

// RunSync is the main sync entry point.
func RunSync(root string, tasks []task.Task, configs map[string]*config.LzConfig, global config.GlobalConfig, dryRun bool) error {
	// Validate global config.
	if global.Sync.Type != "notion" {
		return fmt.Errorf("unsupported sync type %q (only \"notion\" is supported)", global.Sync.Type)
	}
	if global.Sync.Notion == nil {
		return fmt.Errorf("notion config missing in ~/.lz/config.yml")
	}
	if global.Sync.Notion.APIKey == "" || global.Sync.Notion.DatabaseID == "" {
		return fmt.Errorf("notion api_key and database_id required in ~/.lz/config.yml")
	}

	// Acquire lock.
	lock, err := AcquireLock()
	if err != nil {
		return err
	}
	defer lock.Close()

	// Load state.
	state, err := LoadState()
	if err != nil {
		return fmt.Errorf("load sync state: %w", err)
	}

	// Init logger.
	logger, err := NewSyncLogger()
	if err != nil {
		return fmt.Errorf("init sync logger: %w", err)
	}
	defer logger.Close()
	logger.Log("sync started (dry_run=%v)", dryRun)

	// Build eligible task set (only projects with sync enabled).
	eligible := make(map[string]*task.Task)
	for i := range tasks {
		t := &tasks[i]
		cfg := configs[t.Project]
		if syncEnabled(cfg) && syncProject(cfg) != "" {
			eligible[t.Path] = t
		}
	}

	// Build sync plan.
	var creates, updates, deletes, skips []planEntry

	// Check current tasks against state.
	for path, t := range eligible {
		cfg := configs[t.Project]
		notionSt := NotionStatusID(t.Status.String())
		notionProj := syncProject(cfg)
		notionEff := syncEffort(t, cfg)
		notionScope := syncScope(t)

		entry, exists := state.Entries[path]
		if !exists {
			creates = append(creates, planEntry{
				Action: actionCreate, Path: path, Task: t, Config: cfg,
			})
			continue
		}

		// Check for changes.
		var changes []string
		if entry.Project != notionProj {
			// Cross-project move: delete old + create new.
			deletes = append(deletes, planEntry{
				Action: actionDelete, Path: path, Entry: entry, Config: cfg,
				Changes: []string{fmt.Sprintf("project: %s → %s", entry.Project, notionProj)},
			})
			creates = append(creates, planEntry{
				Action: actionCreate, Path: path, Task: t, Config: cfg,
			})
			continue
		}
		if entry.Status != notionSt {
			changes = append(changes, fmt.Sprintf("status: %s → %s", entry.Status, notionSt))
		}
		if entry.Title != t.Title {
			changes = append(changes, fmt.Sprintf("title: %s → %s", entry.Title, t.Title))
		}
		if entry.Effort != notionEff {
			changes = append(changes, fmt.Sprintf("effort: %s → %s", entry.Effort, notionEff))
		}
		if entry.Scope != notionScope {
			changes = append(changes, fmt.Sprintf("scope: %s → %s", entry.Scope, notionScope))
		}
		if _, mtime, err := fileTimes(path); err == nil && !mtime.Truncate(time.Second).Equal(entry.ModTime.Truncate(time.Second)) {
			changes = append(changes, "date (modified)")
		}

		if len(changes) > 0 {
			updates = append(updates, planEntry{
				Action: actionUpdate, Path: path, Task: t, Entry: entry, Config: cfg, Changes: changes,
			})
		} else {
			skips = append(skips, planEntry{
				Action: actionSkip, Path: path, Task: t, Entry: entry,
			})
		}
	}

	// Check state entries for deleted tasks (scoped to current root).
	for path, entry := range state.Entries {
		if !strings.HasPrefix(path, root) {
			continue
		}
		if _, exists := eligible[path]; !exists {
			cfg := configs[entry.Project]
			deletes = append(deletes, planEntry{
				Action: actionDelete, Path: path, Entry: entry, Config: cfg,
			})
		}
	}

	totalChanges := len(creates) + len(updates) + len(deletes)

	// Print plan.
	if totalChanges == 0 {
		fmt.Printf("  No changes (%d tasks synced, all up to date)\n", len(skips))
		logger.Log("no changes (%d synced)", len(skips))
		return nil
	}

	if dryRun {
		fmt.Println("Sync plan (dry run — no changes will be made):")
		fmt.Println()
	} else {
		fmt.Println("Syncing tasks to Notion...")
		fmt.Println()
	}

	for _, e := range creates {
		proj := syncProject(e.Config)
		fmt.Printf("  CREATE  %-8s %s\n", proj, e.Task.Title)
		logger.Log("[CREATE] %s → %s/%s", e.Path, proj, e.Task.Title)
	}
	for _, e := range updates {
		proj := syncProject(e.Config)
		fmt.Printf("  UPDATE  %-8s %s (%s)\n", proj, e.Task.Title, joinChanges(e.Changes))
		logger.Log("[UPDATE] %s → %s", e.Path, joinChanges(e.Changes))
	}
	for _, e := range deletes {
		proj := e.Entry.Project
		fmt.Printf("  DELETE  %-8s %s\n", proj, e.Entry.Title)
		logger.Log("[DELETE] %s (page %s)", e.Path, e.Entry.PageID)
	}
	if len(skips) > 0 {
		fmt.Printf("  SKIP    %d unchanged\n", len(skips))
	}
	fmt.Println()

	if dryRun {
		fmt.Printf("%d to create, %d to update, %d to delete, %d unchanged\n",
			len(creates), len(updates), len(deletes), len(skips))
		return nil
	}

	// Execute.
	client := NewNotionClient(global.Sync.Notion.APIKey, global.Sync.Notion.DatabaseID)
	var errors int

	// Deletes first.
	for _, e := range deletes {
		strategy := onDelete(e.Config)
		var err error
		if strategy == "delete" {
			err = client.Archive(e.Entry.PageID)
		} else {
			err = client.Archive(e.Entry.PageID)
		}
		if err != nil {
			fmt.Printf("  ✗ delete  %s: %v\n", e.Entry.Title, err)
			logger.Log("[ERROR] delete %s: %v", e.Path, err)
			errors++
		} else {
			fmt.Printf("  ✓ deleted  %s\n", e.Entry.Title)
			delete(state.Entries, e.Path)
		}
	}

	// Updates.
	for _, e := range updates {
		cfg := e.Config
		notionSt := NotionStatusID(e.Task.Status.String())
		notionEff := syncEffort(e.Task, cfg)
		notionScope := syncScope(e.Task)

		props := notionapi.Properties{}
		if e.Entry.Status != notionSt {
			props["Status"] = notionapi.StatusProperty{
				Status: statusOption(e.Task.Status.String()),
			}
		}
		if e.Entry.Title != e.Task.Title {
			props["Task"] = notionapi.TitleProperty{
				Title: []notionapi.RichText{
					{Text: &notionapi.Text{Content: e.Task.Title}},
				},
			}
		}
		if e.Entry.Effort != notionEff {
			props["Effort"] = notionapi.SelectProperty{
				Select: notionapi.Option{Name: notionEff},
			}
		}
		if e.Entry.Scope != notionScope {
			if notionScope != "" {
				props["Scope"] = notionapi.SelectProperty{
					Select: notionapi.Option{Name: notionScope},
				}
			}
		}
		created, modified, ftErr := fileTimes(e.Path)
		if ftErr == nil && !modified.Truncate(time.Second).Equal(e.Entry.ModTime.Truncate(time.Second)) {
			props["Date"] = dateRange(created, modified)
		}

		err := client.Update(e.Entry.PageID, props)
		if err != nil {
			fmt.Printf("  ✗ update  %s: %v\n", e.Task.Title, err)
			logger.Log("[ERROR] update %s: %v", e.Path, err)
			errors++
		} else {
			fmt.Printf("  ✓ updated  %s\n", e.Task.Title)
			e.Entry.Status = notionSt
			e.Entry.Title = e.Task.Title
			e.Entry.Effort = notionEff
			e.Entry.Scope = notionScope
			if ftErr == nil {
				e.Entry.ModTime = modified
			}
			e.Entry.LastSynced = time.Now()
		}
	}

	// Creates.
	for _, e := range creates {
		cfg := e.Config
		localSt := e.Task.Status.String()
		notionSt := NotionStatusID(localSt)
		notionProj := syncProject(cfg)
		notionEff := syncEffort(e.Task, cfg)
		notionScope := syncScope(e.Task)

		created, modified, err := fileTimes(e.Path)
		if err != nil {
			created, modified = time.Now(), time.Now()
		}

		pageID, err := client.Create(e.Task.Title, notionProj, notionScope, localSt, notionEff, e.Task.Summary, created, modified)
		if err != nil {
			fmt.Printf("  ✗ create  %s: %v\n", e.Task.Title, err)
			logger.Log("[ERROR] create %s: %v", e.Path, err)
			errors++
		} else {
			fmt.Printf("  ✓ created  %s\n", e.Task.Title)
			state.Entries[e.Path] = &SyncEntry{
				PageID:     pageID,
				Status:     notionSt,
				Project:    notionProj,
				Scope:      notionScope,
				Title:      e.Task.Title,
				Effort:     notionEff,
				ModTime:    modified,
				LastSynced: time.Now(),
			}
		}
	}

	// Save state.
	if err := state.Save(); err != nil {
		return fmt.Errorf("save sync state: %w", err)
	}

	fmt.Printf("\nSync complete: %d created, %d updated, %d deleted, %d errors\n",
		len(creates), len(updates), len(deletes), errors)
	logger.Log("sync complete: %d created, %d updated, %d deleted, %d errors",
		len(creates), len(updates), len(deletes), errors)

	if errors > 0 {
		return fmt.Errorf("%d sync errors occurred", errors)
	}
	return nil
}

func joinChanges(changes []string) string {
	if len(changes) == 0 {
		return ""
	}
	s := changes[0]
	for _, c := range changes[1:] {
		s += ", " + c
	}
	return s
}
