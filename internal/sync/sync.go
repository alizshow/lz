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
	ID      string
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

	// Resolve IDs for all eligible tasks first so the state migration can
	// re-key legacy entries against the same IDs we'll use to diff.
	//
	// Tasks without an ID in frontmatter get one minted here:
	//   - real run: written to frontmatter via EnsureID
	//   - dry run:  ephemeral, in-memory only
	eligible := make(map[string]*task.Task)
	pathToID := make(map[string]string)
	var idsWritten int
	for i := range tasks {
		t := &tasks[i]
		cfg := configs[t.Project]
		if !syncEnabled(cfg) || syncProject(cfg) == "" {
			continue
		}
		if t.Status == task.Backlog || t.Status == task.Canceled {
			continue
		}
		if t.ID == "" {
			if dryRun {
				t.ID = task.NewID()
			} else {
				id, wrote, err := task.EnsureID(t.Path)
				if err != nil {
					return fmt.Errorf("assign id to %s: %w", t.Path, err)
				}
				t.ID = id
				if wrote {
					idsWritten++
				}
			}
		}
		eligible[t.ID] = t
		pathToID[t.Path] = t.ID
	}

	if err := guardEmptyEligible(eligible, state, root); err != nil {
		return err
	}

	// Migrate legacy path-keyed state entries to id-keyed (scoped to root).
	migRep := state.Migrate(root, pathToID, task.NewID)
	if migRep.Rekeyed > 0 || idsWritten > 0 {
		fmt.Printf("  Migrated %d state entr%s to id-keyed (%d ids written to frontmatter, %d orphan).\n",
			migRep.Rekeyed, plural(migRep.Rekeyed, "y", "ies"), idsWritten, migRep.OrphanEntries)
		logger.Log("migrated state: rekeyed=%d ids_written=%d orphan=%d", migRep.Rekeyed, idsWritten, migRep.OrphanEntries)
	}

	// Build sync plan.
	var creates, updates, deletes, skips []planEntry

	// Check current tasks against state.
	for id, t := range eligible {
		cfg := configs[t.Project]
		entry, exists := state.Entries[id]
		if !exists {
			creates = append(creates, planEntry{
				Action: actionCreate, ID: id, Path: t.Path, Task: t, Config: cfg,
			})
			continue
		}
		changes := diffChanges(entry, t, cfg, t.Path)
		if entry.LastPath != "" && entry.LastPath != t.Path {
			changes = append(changes, fmt.Sprintf("path: %s → %s", entry.LastPath, t.Path))
		}
		if len(changes) > 0 {
			updates = append(updates, planEntry{
				Action: actionUpdate, ID: id, Path: t.Path, Task: t, Entry: entry, Config: cfg, Changes: changes,
			})
		} else {
			skips = append(skips, planEntry{
				Action: actionSkip, ID: id, Path: t.Path, Task: t, Entry: entry,
			})
		}
	}

	// Check state entries for deleted tasks (scoped to current root by LastPath).
	for id, entry := range state.Entries {
		scopedPath := entry.LastPath
		if scopedPath == "" || !strings.HasPrefix(scopedPath, root) {
			continue
		}
		if _, exists := eligible[id]; !exists {
			cfg := configs[entry.Project]
			deletes = append(deletes, planEntry{
				Action: actionDelete, ID: id, Path: scopedPath, Entry: entry, Config: cfg,
			})
		}
	}

	// Migration heuristic: a legacy rename/move shows up in the plan as a
	// DELETE (old path's state entry, file gone) + CREATE (new path, no
	// state) pair. Collapse them into an UPDATE when (project, scope, title)
	// matches exactly — this lets pre-existing renames done before the first
	// id-keyed sync still resolve cleanly without archiving+recreating the
	// Notion page.
	creates, deletes, updates = collapseRenames(creates, deletes, updates, state, logger)

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
	client := NewNotionClient(global.Sync.Notion.APIKey, global.Sync.Notion.DatabaseID, global.Sync.Notion.Projects)
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
			delete(state.Entries, e.ID)
		}
	}

	// Updates.
	for _, e := range updates {
		cfg := e.Config
		notionSt := NotionStatusID(e.Task.Status.String())
		notionProj := syncProject(cfg)
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
		if e.Entry.Project != notionProj && notionProj != "" {
			props["Project"] = notionapi.SelectProperty{
				Select: notionapi.Option{Name: notionProj},
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
			e.Entry.Project = notionProj
			e.Entry.LastPath = e.Path
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
			state.Entries[e.ID] = &SyncEntry{
				ID:         e.ID,
				LastPath:   e.Path,
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

// guardEmptyEligible refuses a sync run when no tasks are eligible but state
// has entries scoped to root — the usual cause is running sync from a
// subdirectory below the .lz.yml that enables it, which would silently
// produce a mass-DELETE plan. See TASK_GOTCHAS.md #10.
func guardEmptyEligible(eligible map[string]*task.Task, state *SyncState, root string) error {
	if len(eligible) > 0 {
		return nil
	}
	var inScope int
	for _, entry := range state.Entries {
		if entry.LastPath != "" && strings.HasPrefix(entry.LastPath, root) {
			inScope++
		}
	}
	if inScope == 0 {
		return nil
	}
	return fmt.Errorf("no project with sync enabled was discovered under %s, "+
		"but %d state entr%s scoped here would be marked for deletion. "+
		"likely cause: running sync from a subdirectory below where .lz.yml lives. "+
		"re-run from a directory whose subtree includes the .lz.yml that enables sync",
		root, inScope, plural(inScope, "y", "ies"))
}

// diffChanges compares a state entry against a live task and returns a list of
// human-readable change descriptions (excluding path, which the caller may add
// with its own source-of-truth old path).
func diffChanges(entry *SyncEntry, t *task.Task, cfg *config.LzConfig, path string) []string {
	notionSt := NotionStatusID(t.Status.String())
	notionProj := syncProject(cfg)
	notionEff := syncEffort(t, cfg)
	notionScope := syncScope(t)
	var changes []string
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
	if entry.Project != notionProj {
		changes = append(changes, fmt.Sprintf("project: %s → %s", entry.Project, notionProj))
	}
	if _, mtime, err := fileTimes(path); err == nil && !mtime.Truncate(time.Second).Equal(entry.ModTime.Truncate(time.Second)) {
		changes = append(changes, "date (modified)")
	}
	return changes
}

// collapseRenames rewrites CREATE+DELETE pairs into UPDATEs when they match on
// (project, scope, title) — treating them as a rename/move that predated
// id-keyed state. state.Entries is re-keyed in place so the caller's execute
// phase targets the surviving Notion page.
func collapseRenames(creates, deletes, updates []planEntry, state *SyncState, logger *SyncLogger) ([]planEntry, []planEntry, []planEntry) {
	if len(creates) == 0 || len(deletes) == 0 {
		return creates, deletes, updates
	}
	// Index deletes by a composite key.
	type key struct{ project, scope, title string }
	delByKey := make(map[key][]int)
	for i, d := range deletes {
		k := key{d.Entry.Project, d.Entry.Scope, d.Entry.Title}
		delByKey[k] = append(delByKey[k], i)
	}
	keepCreate := make([]bool, len(creates))
	dropDelete := make(map[int]bool)
	for ci, c := range creates {
		proj := ""
		if c.Config != nil {
			proj = syncProject(c.Config)
		}
		k := key{proj, c.Task.Scope, c.Task.Title}
		cands := delByKey[k]
		if len(cands) != 1 { // only collapse unambiguous matches
			keepCreate[ci] = true
			continue
		}
		di := cands[0]
		if dropDelete[di] {
			keepCreate[ci] = true
			continue
		}
		dropDelete[di] = true
		delByKey[k] = nil

		// Re-key state entry under the CREATE's id, reusing the Notion page.
		d := deletes[di]
		entry := d.Entry
		delete(state.Entries, d.ID)
		entry.ID = c.ID
		entry.LastPath = c.Path
		state.Entries[c.ID] = entry

		changes := diffChanges(entry, c.Task, c.Config, c.Path)
		if d.Path != c.Path {
			changes = append(changes, fmt.Sprintf("path: %s → %s", d.Path, c.Path))
		}
		if len(changes) == 0 {
			changes = append(changes, "rematched from legacy entry")
		}
		updates = append(updates, planEntry{
			Action: actionUpdate, ID: c.ID, Path: c.Path, Task: c.Task, Entry: entry, Config: c.Config, Changes: changes,
		})
		if logger != nil {
			logger.Log("rename-collapse: %s → %s (id=%s page=%s)", d.Path, c.Path, c.ID, entry.PageID)
		}
	}
	newCreates := creates[:0:0]
	for i, c := range creates {
		if keepCreate[i] {
			newCreates = append(newCreates, c)
		}
	}
	newDeletes := deletes[:0:0]
	for i, d := range deletes {
		if !dropDelete[i] {
			newDeletes = append(newDeletes, d)
		}
	}
	return newCreates, newDeletes, updates
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
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
