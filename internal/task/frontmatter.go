package task

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Frontmatter is an ordered list of key/value pairs from a task file's --- fence.
// Keys preserve their original order so unknown fields (e.g. shape:) survive
// round-trips through ReadFile/WriteFile.
type Frontmatter struct {
	pairs []kv
}

type kv struct{ k, v string }

// Get returns the value for key, or "" if absent.
func (f *Frontmatter) Get(key string) string {
	for _, p := range f.pairs {
		if p.k == key {
			return p.v
		}
	}
	return ""
}

// Set inserts or updates a key. New keys append at the end in insertion order.
func (f *Frontmatter) Set(key, value string) {
	for i := range f.pairs {
		if f.pairs[i].k == key {
			f.pairs[i].v = value
			return
		}
	}
	f.pairs = append(f.pairs, kv{key, value})
}

// Unquote strips one level of YAML scalar quoting from v: a surrounding pair
// of double quotes (escape handling via strconv.Unquote) or single quotes
// (with '' collapsing to '). Unquoted values pass through unchanged, as does
// anything that fails to unquote cleanly. Get stays raw so round-trips
// preserve the file byte-for-byte — call this where the value is consumed
// (display, sync), not re-emitted.
func Unquote(v string) string {
	if len(v) < 2 {
		return v
	}
	switch {
	case v[0] == '"' && v[len(v)-1] == '"':
		if s, err := strconv.Unquote(v); err == nil {
			return s
		}
	case v[0] == '\'' && v[len(v)-1] == '\'':
		return strings.ReplaceAll(v[1:len(v)-1], "''", "'")
	}
	return v
}

// Has reports whether key is present.
func (f *Frontmatter) Has(key string) bool {
	for _, p := range f.pairs {
		if p.k == key {
			return true
		}
	}
	return false
}

// render emits the frontmatter block (fence + body fence), or empty string
// if there are no fields.
func (f *Frontmatter) render() string {
	if len(f.pairs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("---\n")
	for _, p := range f.pairs {
		b.WriteString(p.k)
		b.WriteString(": ")
		b.WriteString(p.v)
		b.WriteByte('\n')
	}
	b.WriteString("---\n")
	return b.String()
}

// ReadFile parses a task file into its frontmatter and body.
// If the file has no frontmatter fence, Frontmatter is empty and body is the
// entire file contents.
func ReadFile(path string) (Frontmatter, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Frontmatter{}, "", err
	}
	fm, body := parseContent(string(data))
	return fm, body, nil
}

func parseContent(content string) (Frontmatter, string) {
	if !strings.HasPrefix(content, "---\n") {
		return Frontmatter{}, content
	}
	end := strings.Index(content[4:], "\n---\n")
	if end < 0 {
		return Frontmatter{}, content
	}
	var fm Frontmatter
	for line := range strings.SplitSeq(content[4:4+end], "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		fm.pairs = append(fm.pairs, kv{strings.TrimSpace(k), strings.TrimSpace(v)})
	}
	body := strings.TrimLeft(content[4+end+5:], "\n")
	return fm, body
}

// WriteFile rewrites the file at path with the given frontmatter and body.
// Writes in place (truncate + write on the same inode) so the file's btime
// is preserved across rewrites — sync uses btime as the "created" date and a
// tmp+rename atomic write would reset it to now.
func WriteFile(path string, fm Frontmatter, body string) error {
	content := fm.render()
	if content != "" && body != "" {
		content += "\n"
	}
	content += body
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	return os.WriteFile(path, []byte(content), mode)
}

// NewID returns a short random identifier (14 chars, ~65 bits). The first
// char is always a letter so the value parses as a YAML string even if all
// base32 chars happen to be digits.
func NewID() string {
	var b [9]byte
	_, _ = rand.Read(b[:])
	prefix := 'a' + b[0]%26
	suffix := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[1:]))
	return string(prefix) + suffix
}

// EnsureID reads the file, returns its id, or assigns and writes a new one.
// When a new id is written, wrote is true.
func EnsureID(path string) (id string, wrote bool, err error) {
	fm, body, err := ReadFile(path)
	if err != nil {
		return "", false, err
	}
	if id := fm.Get("id"); id != "" {
		return id, false, nil
	}
	id = NewID()
	fm.Set("id", id)
	if err := WriteFile(path, fm, body); err != nil {
		return "", false, fmt.Errorf("write id to %s: %w", path, err)
	}
	return id, true, nil
}
