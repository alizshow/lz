package sync

import (
	"testing"
)

func TestMigrate_rekeysByPathToID(t *testing.T) {
	s := &SyncState{Entries: map[string]*SyncEntry{
		"/root/a.md": {PageID: "page-a", Title: "A", Project: "P"},
	}}
	pathToID := map[string]string{"/root/a.md": "id-a"}
	rep := s.Migrate("/root", pathToID, func() string { t.Fatal("newID should not be called"); return "" })

	if rep.Rekeyed != 1 || rep.OrphanEntries != 0 {
		t.Fatalf("rep = %+v", rep)
	}
	e, ok := s.Entries["id-a"]
	if !ok {
		t.Fatalf("entry not re-keyed; map=%v", s.Entries)
	}
	if e.ID != "id-a" || e.LastPath != "/root/a.md" {
		t.Fatalf("entry fields wrong: %+v", e)
	}
	if _, stillLegacy := s.Entries["/root/a.md"]; stillLegacy {
		t.Fatal("legacy key not deleted")
	}
}

func TestMigrate_orphanUsesNewID(t *testing.T) {
	s := &SyncState{Entries: map[string]*SyncEntry{
		"/root/gone.md": {PageID: "page-gone", Title: "G", Project: "P"},
	}}
	rep := s.Migrate("/root", map[string]string{}, func() string { return "synth-1" })

	if rep.Rekeyed != 1 || rep.OrphanEntries != 1 {
		t.Fatalf("rep = %+v", rep)
	}
	if _, ok := s.Entries["synth-1"]; !ok {
		t.Fatalf("orphan entry not re-keyed under synthetic id; map=%v", s.Entries)
	}
}

func TestMigrate_scopedToRoot(t *testing.T) {
	s := &SyncState{Entries: map[string]*SyncEntry{
		"/root-a/x.md": {PageID: "pa", Title: "X", Project: "A"},
		"/root-b/y.md": {PageID: "pb", Title: "Y", Project: "B"},
	}}
	pathToID := map[string]string{"/root-a/x.md": "id-x"}
	rep := s.Migrate("/root-a", pathToID, func() string {
		t.Fatal("should not need synthetic ids")
		return ""
	})

	if rep.Rekeyed != 1 {
		t.Fatalf("rep = %+v; expected only root-a migrated", rep)
	}
	if _, ok := s.Entries["id-x"]; !ok {
		t.Fatal("root-a entry not migrated")
	}
	// root-b entry must remain legacy-keyed, untouched.
	if e, ok := s.Entries["/root-b/y.md"]; !ok || e.ID != "" {
		t.Fatalf("root-b entry should be untouched; got ok=%v entry=%+v", ok, e)
	}
}

func TestMigrate_leavesAlreadyMigratedAlone(t *testing.T) {
	s := &SyncState{Entries: map[string]*SyncEntry{
		"id-a": {ID: "id-a", LastPath: "/root/a.md", PageID: "pa", Title: "A"},
	}}
	rep := s.Migrate("/root", map[string]string{"/root/a.md": "id-a"}, func() string {
		t.Fatal("should not be called")
		return ""
	})
	if rep.Rekeyed != 0 || rep.OrphanEntries != 0 {
		t.Fatalf("already-migrated entry should be left alone; rep=%+v", rep)
	}
}
