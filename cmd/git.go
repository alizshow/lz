package cmd

import (
	"fmt"
	"os"
	"strings"

	"aliz/lz/internal/git"
	"aliz/lz/internal/ui"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// RunGit prints multi-repo git status to stdout.
func RunGit() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	repos, err := git.Discover(cwd)
	if err != nil {
		return err
	}
	if len(repos) == 0 {
		fmt.Println("No git repos found.")
		return nil
	}

	// Gather status for all repos.
	type entry struct {
		repo   git.Repo
		status git.RepoStatus
	}
	entries := make([]entry, len(repos))
	for i, r := range repos {
		entries[i] = entry{repo: r, status: git.GetStatus(r.Path)}
	}

	// Compute dynamic width from header lines only (file paths hang below).
	maxW := 60
	for _, e := range entries {
		if w := lineWidth(e.repo.Name, e.status); w > maxW {
			maxW = w
		}
	}

	// Render.
	prevDirty := false
	for i, e := range entries {
		// Spacing: compact clean repos together, breathe around dirty.
		if i > 0 && (prevDirty || !e.status.IsClean) {
			fmt.Println()
		}

		right := buildRight(e.status)
		left := fmt.Sprintf("── %s ", e.repo.Name)
		leftW := runewidth.StringWidth(left)
		rightW := runewidth.StringWidth(right)
		pad := maxW - leftW - rightW
		if pad < 3 {
			pad = 3
		}
		dots := strings.Repeat("·", pad)

		if e.status.IsClean {
			fmt.Printf("%s%s %s\n",
				ui.Faint.Render("── ")+ui.Bold.Render(e.repo.Name)+" ",
				ui.Faint.Render(dots),
				renderRight(e.status, true),
			)
		} else {
			fmt.Printf("%s%s %s\n",
				ui.Faint.Render("── ")+ui.Bold.Render(e.repo.Name)+" ",
				ui.Faint.Render(dots),
				renderRight(e.status, false),
			)
			for _, f := range e.status.Files {
				for _, line := range renderFile(f) {
					fmt.Printf("   %s\n", line)
				}
			}
		}
		prevDirty = !e.status.IsClean
	}

	return nil
}

// lineWidth computes the display width of a repo's header line.
func lineWidth(name string, s git.RepoStatus) int {
	left := fmt.Sprintf("── %s ", name)
	right := buildRight(s)
	return runewidth.StringWidth(left) + 3 + runewidth.StringWidth(right) // 3 = min dots
}

// buildRight builds the plain-text right side for width calculation.
func buildRight(s git.RepoStatus) string {
	var parts []string
	parts = append(parts, s.Branch)
	if s.Tag != "" {
		parts = append(parts, "@"+s.Tag)
	}
	if s.Ahead > 0 {
		parts = append(parts, fmt.Sprintf("↑%d", s.Ahead))
	}
	if s.Behind > 0 {
		parts = append(parts, fmt.Sprintf("↓%d", s.Behind))
	}
	if s.Stash > 0 {
		parts = append(parts, fmt.Sprintf("≡%d", s.Stash))
	}
	if s.IsClean {
		parts = append(parts, "✓")
	}
	parts = append(parts, " "+ui.RelativeTime(s.Age))
	return strings.Join(parts, " ")
}

// renderRight builds the colored right side.
func renderRight(s git.RepoStatus, clean bool) string {
	var parts []string
	if clean {
		parts = append(parts, ui.Faint.Render(s.Branch))
	} else {
		parts = append(parts, ui.Cyan.Render(s.Branch))
	}
	if s.Tag != "" {
		parts = append(parts, ui.Yellow.Render("@"+s.Tag))
	}
	if s.Ahead > 0 {
		parts = append(parts, fmt.Sprintf("↑%d", s.Ahead))
	}
	if s.Behind > 0 {
		parts = append(parts, fmt.Sprintf("↓%d", s.Behind))
	}
	if s.Stash > 0 {
		parts = append(parts, fmt.Sprintf("≡%d", s.Stash))
	}
	if clean {
		parts = append(parts, ui.Green.Render("✓"))
	}
	parts = append(parts, " "+ui.Faint.Render(ui.RelativeTime(s.Age)))
	return strings.Join(parts, " ")
}

// renderFile renders a porcelain status entry. Renames produce two lines.
func renderFile(f git.FileStatus) []string {
	ch, style := fileSign(f.XY)
	render := style.Render

	if strings.Contains(f.File, " -> ") {
		parts := strings.SplitN(f.File, " -> ", 2)
		return []string{
			ui.Faint.Render(string(ch) + " " + parts[0]),
			render("→ " + parts[1]),
		}
	}

	return []string{render(string(ch) + " " + f.File)}
}

func fileSign(xy string) (rune, lipgloss.Style) {
	switch xy {
	case "??":
		return '?', ui.Red
	case " M", "M ":
		if xy[0] == 'M' {
			return 'M', ui.Green
		}
		return 'M', ui.Yellow
	case "MM":
		return 'M', ui.Cyan
	case "A ":
		return 'A', ui.Green
	case "AM":
		return 'A', ui.Cyan
	case " D", "D ":
		return 'D', ui.Red
	case "R ":
		return 'R', ui.Green
	default:
		return '~', ui.Faint
	}
}
