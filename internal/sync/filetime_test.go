package sync

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileTimes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	created, modified, err := fileTimes(path)
	if err != nil {
		t.Fatalf("fileTimes: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !modified.Equal(info.ModTime()) {
		t.Errorf("modified = %v, want %v", modified, info.ModTime())
	}
	if created.IsZero() {
		t.Error("created is zero")
	}
	if created.After(modified.Add(time.Second)) {
		t.Errorf("created %v is after modified %v", created, modified)
	}

	if _, _, err := fileTimes(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("expected error for missing file")
	}
}
