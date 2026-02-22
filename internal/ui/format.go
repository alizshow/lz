package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"
)

// RelativeTime formats a time as a compact relative string.
func RelativeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/(24*365)))
	}
}

// DotLine builds "left ····· right" padded to width with dot leaders.
func DotLine(left, right string, width int) string {
	lw := runewidth.StringWidth(left)
	rw := runewidth.StringWidth(right)
	pad := width - lw - rw
	if pad < 3 {
		pad = 3
	}
	dots := " " + strings.Repeat("·", pad-2) + " "
	return left + dots + right
}

// Truncate shortens s to max display cells, appending "…" if truncated.
// Unicode-safe via runewidth.
func Truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	w := runewidth.StringWidth(s)
	if w <= max {
		return s
	}
	result := []rune{}
	cur := 0
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if cur+rw > max-1 { // leave room for …
			break
		}
		result = append(result, r)
		cur += rw
	}
	return string(result) + "…"
}
