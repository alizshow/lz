package cmd

import (
	"fmt"
	"os"
	"strings"

	"aliz/lz/internal/git"
	"aliz/lz/internal/ui"

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

	// Compute dynamic width from content.
	maxW := 0
	for _, e := range entries {
		w := lineWidth(e.repo.Name, e.status)
		if w > maxW {
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
				fmt.Printf("   %s\n", renderFile(f, maxW-3))
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

// renderFile renders a single porcelain status line with color.
func renderFile(f git.FileStatus, maxFileW int) string {
	file := f.File

	// Truncate rename paths: "old -> new" → "…/old → …/new"
	if strings.Contains(file, " -> ") {
		parts := strings.SplitN(file, " -> ", 2)
		half := (maxFileW - 4) / 2 // 4 for " → "
		if half < 8 {
			half = 8
		}
		old := ui.Truncate(parts[0], half)
		newF := ui.Truncate(parts[1], half)
		file = old + " → " + newF
	} else if runewidth.StringWidth(file) > maxFileW {
		file = ui.Truncate(file, maxFileW)
	}

	label := fmt.Sprintf("%s %s", f.XY, file)

	switch f.XY {
	case "??":
		return ui.Red.Render(label)
	case " M":
		return ui.Yellow.Render(label)
	case "M ":
		return ui.Green.Render(label)
	case "A ":
		return ui.Green.Render(label)
	case "MM":
		return ui.Cyan.Render(label)
	case " D":
		return ui.Red.Render(label)
	case "D ":
		return ui.Red.Render(label)
	case "R ":
		return ui.Green.Render(label)
	case "AM":
		return ui.Cyan.Render(label)
	default:
		return ui.Faint.Render(label)
	}
}
