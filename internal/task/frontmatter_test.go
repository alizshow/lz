package task

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadFile_noFrontmatter(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "a.md", "# Title\n\nBody.\n")
	fm, body, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(fm.pairs) != 0 {
		t.Fatalf("want no frontmatter pairs, got %d", len(fm.pairs))
	}
	if body != "# Title\n\nBody.\n" {
		t.Fatalf("body mismatch: %q", body)
	}
}

func TestRoundTrip_preservesUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	in := "---\npriority: high\neffort: L\nshape: investigation\ncustom: foo\n---\n\n# Title\n\nBody.\n"
	p := writeFile(t, dir, "a.md", in)

	fm, body, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	// Edit a known key; unknown keys (shape, custom) must survive in place.
	fm.Set("priority", "low")
	if err := WriteFile(p, fm, body); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	want := "---\npriority: low\neffort: L\nshape: investigation\ncustom: foo\n---\n\n# Title\n\nBody.\n"
	if string(got) != want {
		t.Fatalf("round-trip mismatch:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestEnsureID_idempotentAndWritesOnlyFirstTime(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "a.md", "---\npriority: high\n---\n\n# Title\n")

	id, wrote, err := EnsureID(p)
	if err != nil {
		t.Fatal(err)
	}
	if !wrote {
		t.Fatal("first EnsureID should write")
	}
	if id == "" {
		t.Fatal("first EnsureID should return a non-empty id")
	}

	id2, wrote2, err := EnsureID(p)
	if err != nil {
		t.Fatal(err)
	}
	if wrote2 {
		t.Fatal("second EnsureID should not write")
	}
	if id2 != id {
		t.Fatalf("second EnsureID should return same id; got %q vs %q", id2, id)
	}

	// Verify id was appended to frontmatter.
	got, _ := os.ReadFile(p)
	fm, _, _ := ReadFile(p)
	if fm.Get("id") != id {
		t.Fatalf("id not persisted in frontmatter; file=%s", got)
	}
	// priority: high must still be present.
	if fm.Get("priority") != "high" {
		t.Fatalf("existing key lost during EnsureID; got frontmatter: %v", fm.pairs)
	}
}

func TestEnsureID_onFileWithoutFrontmatter(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "a.md", "# Title\n\nBody.\n")

	id, wrote, err := EnsureID(p)
	if err != nil || !wrote || id == "" {
		t.Fatalf("EnsureID on plain file: id=%q wrote=%v err=%v", id, wrote, err)
	}
	fm, body, _ := ReadFile(p)
	if fm.Get("id") != id {
		t.Fatalf("id not persisted; fm=%v", fm.pairs)
	}
	if body != "# Title\n\nBody.\n" {
		t.Fatalf("body mutated by EnsureID: %q", body)
	}
}

func TestNewID_unique(t *testing.T) {
	seen := make(map[string]bool)
	for i := range 1000 {
		id := NewID()
		if id == "" {
			t.Fatal("empty id")
		}
		if seen[id] {
			t.Fatalf("collision after %d ids: %s", i, id)
		}
		seen[id] = true
	}
}

func TestNewID_firstCharIsLetter(t *testing.T) {
	// YAML bare scalars that are all digits parse as integers. Guard against
	// that by requiring every id to start with a letter.
	for range 2000 {
		id := NewID()
		if id == "" || id[0] < 'a' || id[0] > 'z' {
			t.Fatalf("id %q does not start with a-z", id)
		}
	}
}

func TestAtomicWrite_preservesMode(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "a.md", "---\npriority: high\n---\n\nbody\n")
	if err := os.Chmod(p, 0o600); err != nil {
		t.Fatal(err)
	}
	fm, body, _ := ReadFile(p)
	fm.Set("priority", "low")
	if err := WriteFile(p, fm, body); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode not preserved: %v", info.Mode().Perm())
	}
}
