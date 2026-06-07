package sync

import (
	"strings"
	"testing"

	"aliz/lz/internal/config"
	"aliz/lz/internal/task"
)

func cfgFor(project string) *config.LzConfig {
	enabled := true
	return &config.LzConfig{
		Project: project,
		Sync: &config.ProjectSyncConfig{
			Enabled: &enabled,
			Project: project,
		},
	}
}

func TestCollapseRenames_matchingPair(t *testing.T) {
	// Legacy DELETE (file at /root/a.md gone) + CREATE (file at /root/b.md)
	// that share (project, scope, title) — should collapse into UPDATE.
	state := &SyncState{Entries: map[string]*SyncEntry{
		"orphan-id": {ID: "orphan-id", LastPath: "/root/a.md", PageID: "page-1", Title: "Same Title", Project: "P"},
	}}
	cfg := cfgFor("P")
	creates := []planEntry{{
		Action: actionCreate, ID: "new-id", Path: "/root/b.md",
		Task: &task.Task{ID: "new-id", Title: "Same Title", Project: "P"}, Config: cfg,
	}}
	deletes := []planEntry{{
		Action: actionDelete, ID: "orphan-id", Path: "/root/a.md",
		Entry: state.Entries["orphan-id"], Config: cfg,
	}}

	newCreates, newDeletes, newUpdates := collapseRenames(creates, deletes, nil, state, nil)

	if len(newCreates) != 0 || len(newDeletes) != 0 {
		t.Fatalf("creates/deletes should be empty; got creates=%d deletes=%d", len(newCreates), len(newDeletes))
	}
	if len(newUpdates) != 1 {
		t.Fatalf("expected 1 update; got %d", len(newUpdates))
	}
	u := newUpdates[0]
	if u.ID != "new-id" || u.Path != "/root/b.md" {
		t.Fatalf("update id/path wrong: %+v", u)
	}
	if u.Entry.PageID != "page-1" {
		t.Fatalf("update should reuse the original Notion page id; got %q", u.Entry.PageID)
	}
	// State must be re-keyed under new-id, legacy orphan-id removed.
	if _, ok := state.Entries["orphan-id"]; ok {
		t.Fatal("orphan-id should be gone from state")
	}
	if e, ok := state.Entries["new-id"]; !ok || e.PageID != "page-1" || e.LastPath != "/root/b.md" {
		t.Fatalf("state not re-keyed correctly: %+v", e)
	}
	// Changes should mention path change at minimum.
	seenPath := false
	for _, c := range u.Changes {
		if len(c) >= 5 && c[:5] == "path:" {
			seenPath = true
		}
	}
	if !seenPath {
		t.Fatalf("expected path: change in Changes; got %v", u.Changes)
	}
}

func TestCollapseRenames_ambiguousMatchNotCollapsed(t *testing.T) {
	// Two deletes share (project, title, scope) — collapse must bail.
	state := &SyncState{Entries: map[string]*SyncEntry{
		"orphan-1": {ID: "orphan-1", LastPath: "/root/a.md", PageID: "p1", Title: "Dup", Project: "P"},
		"orphan-2": {ID: "orphan-2", LastPath: "/root/c.md", PageID: "p2", Title: "Dup", Project: "P"},
	}}
	cfg := cfgFor("P")
	creates := []planEntry{{
		Action: actionCreate, ID: "new-id", Path: "/root/b.md",
		Task: &task.Task{ID: "new-id", Title: "Dup", Project: "P"}, Config: cfg,
	}}
	deletes := []planEntry{
		{Action: actionDelete, ID: "orphan-1", Path: "/root/a.md", Entry: state.Entries["orphan-1"], Config: cfg},
		{Action: actionDelete, ID: "orphan-2", Path: "/root/c.md", Entry: state.Entries["orphan-2"], Config: cfg},
	}

	nc, nd, nu := collapseRenames(creates, deletes, nil, state, nil)

	if len(nc) != 1 || len(nd) != 2 || len(nu) != 0 {
		t.Fatalf("ambiguous match should leave plan unchanged; creates=%d deletes=%d updates=%d",
			len(nc), len(nd), len(nu))
	}
	// State is untouched.
	if _, ok := state.Entries["orphan-1"]; !ok {
		t.Fatal("orphan-1 should remain in state")
	}
}

func TestCollapseRenames_noDeletesOrCreates(t *testing.T) {
	state := &SyncState{Entries: map[string]*SyncEntry{}}
	nc, nd, nu := collapseRenames(nil, nil, nil, state, nil)
	if nc != nil || nd != nil || nu != nil {
		t.Fatalf("empty in → empty out; got %v %v %v", nc, nd, nu)
	}
}

func TestGuardEmptyEligible_emptyAndScopedStateBails(t *testing.T) {
	state := &SyncState{Entries: map[string]*SyncEntry{
		"a": {ID: "a", LastPath: "/root/sub/a.md"},
		"b": {ID: "b", LastPath: "/root/b.md"},
		"c": {ID: "c", LastPath: "/elsewhere/c.md"}, // out-of-scope
		"d": {ID: "d", LastPath: ""},                // never-synced
	}}
	err := guardEmptyEligible(map[string]*task.Task{}, state, "/root")
	if err == nil {
		t.Fatal("expected error when eligible is empty but state has scoped entries")
	}
	if !strings.Contains(err.Error(), "2 state entries") {
		t.Fatalf("error should report the 2 in-scope entries (not 4); got: %v", err)
	}
}

func TestGuardEmptyEligible_emptyAndNoScopedStatePasses(t *testing.T) {
	// State exists but nothing is under this root — a fresh root with no prior sync.
	state := &SyncState{Entries: map[string]*SyncEntry{
		"x": {ID: "x", LastPath: "/elsewhere/x.md"},
	}}
	if err := guardEmptyEligible(map[string]*task.Task{}, state, "/root"); err != nil {
		t.Fatalf("guard should not fire for unrelated state entries; got: %v", err)
	}
}

func TestGuardEmptyEligible_nonEmptyEligiblePasses(t *testing.T) {
	// Real "user deleted every task" cases are valid — guard only fires when
	// eligible is empty. With any eligible task, deletes are user-driven.
	state := &SyncState{Entries: map[string]*SyncEntry{
		"a": {ID: "a", LastPath: "/root/a.md"},
	}}
	eligible := map[string]*task.Task{"x": {ID: "x"}}
	if err := guardEmptyEligible(eligible, state, "/root"); err != nil {
		t.Fatalf("guard should not fire when eligible is non-empty; got: %v", err)
	}
}

func TestDiffChanges_detectsEachField(t *testing.T) {
	entry := &SyncEntry{
		Status: "s-old", Title: "t-old", Effort: "M", Scope: "x", Project: "A",
	}
	tt := &task.Task{
		Title:   "t-new",
		Project: "B",
		Scope:   "y",
		Status:  task.Done,
		Effort:  task.EffortL,
	}
	changes := diffChanges(entry, tt, cfgFor("B"), "/nonexistent")
	if len(changes) < 5 {
		t.Fatalf("expected status/title/effort/scope/project changes; got %v", changes)
	}
}
