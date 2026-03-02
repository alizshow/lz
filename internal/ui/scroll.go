package ui

import (
	"fmt"
	"strings"
)

// Scroll tracks viewport state for a scrollable list of lines.
type Scroll struct {
	Offset int // first visible line
	Total  int // total line count
	Height int // viewport height in lines
}

func (s *Scroll) Up() {
	if s.Offset > 0 {
		s.Offset--
	}
}

func (s *Scroll) Down() {
	s.Offset++
	s.Clamp()
}

func (s *Scroll) Top() { s.Offset = 0 }

func (s *Scroll) Bottom() {
	s.Offset = s.maxOffset()
}

func (s *Scroll) Clamp() {
	if mx := s.maxOffset(); s.Offset > mx {
		s.Offset = mx
	}
	if s.Offset < 0 {
		s.Offset = 0
	}
}

func (s Scroll) maxOffset() int {
	return max(s.Total-s.Height, 0)
}

// Visible returns the slice of lines that fit in the viewport.
func (s *Scroll) Visible(lines []string) []string {
	s.Total = len(lines)
	s.Clamp()
	end := min(s.Offset+s.Height, len(lines))
	return lines[s.Offset:end]
}

// Percent returns a scroll position string like " · 42%" or empty if no scrolling needed.
func (s Scroll) Percent() string {
	mx := s.maxOffset()
	if mx <= 0 {
		return ""
	}
	pct := 100 * s.Offset / mx
	return fmt.Sprintf(" · %d%%", pct)
}

// HandleKey processes common scroll keys (up/k, down/j, g, G).
// Returns true if the key was handled.
func (s *Scroll) HandleKey(key string) bool {
	switch key {
	case "up", "k":
		s.Up()
	case "down", "j":
		s.Down()
	case "g":
		s.Top()
	case "G":
		s.Bottom()
	default:
		return false
	}
	return true
}

// KeepCursorVisible adjusts a scroll offset so that cursorLine stays within
// the viewport of the given height. Returns the new offset.
func KeepCursorVisible(cursor, totalLines, viewportH int) int {
	if viewportH <= 0 || totalLines <= viewportH {
		return 0
	}
	offset := 0
	if cursor > viewportH-2 {
		offset = cursor - viewportH + 2
	}
	if mx := totalLines - viewportH; offset > mx {
		offset = mx
	}
	if offset < 0 {
		offset = 0
	}
	return offset
}

// RenderHelp formats a help bar string in faint style.
func RenderHelp(parts ...string) string {
	return Faint.Render(strings.Join(parts, " · "))
}
