package cmd

import (
	"cmp"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"

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

	// Gather status for all repos in parallel.
	type entry struct {
		repo   git.Repo
		status git.RepoStatus
	}
	entries := make([]entry, len(repos))
	var wg sync.WaitGroup
	wg.Add(len(repos))
	for i, r := range repos {
		entries[i].repo = r
		go func() {
			defer wg.Done()
			entries[i].status = git.GetStatus(r.Path)
		}()
	}
	wg.Wait()

	// Sort: dirty repos first, then by most recent commit.
	slices.SortFunc(entries, func(a, b entry) int {
		da, db := !a.status.IsClean, !b.status.IsClean
		if da != db {
			if da {
				return -1
			}
			return 1
		}
		return cmp.Compare(b.status.Age.Unix(), a.status.Age.Unix())
	})

	// Compute per-entry column values (plain text).
	// Columns: branch, ahead(or ∅), behind, stash, age, tag
	type rightCols struct {
		branch, ahead, behind, stash, age, tag string
	}
	cols := make([]rightCols, len(entries))
	for i, e := range entries {
		s := e.status
		c := &cols[i]
		c.branch = s.Branch
		if !s.HasUpstream {
			c.ahead = "∅"
		} else if s.Ahead > 0 {
			c.ahead = fmt.Sprintf("↑%d", s.Ahead)
		}
		if s.Behind > 0 {
			c.behind = fmt.Sprintf("↓%d", s.Behind)
		}
		if s.Stash > 0 {
			c.stash = fmt.Sprintf("≡%d", s.Stash)
		}
		c.age = ui.RelativeTime(s.Age)
		if s.Tag != "" {
			c.tag = "@" + s.Tag
		}
	}

	// Max column widths: branch[0] age[1] ahead[2] behind[3] stash[4] tag[5]
	var cw [6]int
	for _, c := range cols {
		for j, v := range [6]string{c.branch, c.age, c.ahead, c.behind, c.stash, c.tag} {
			cw[j] = max(cw[j], runewidth.StringWidth(v))
		}
	}

	// Pad helper: styled text right-padded with spaces to fixed width.
	padStyled := func(styled, plain string, maxW int) string {
		return styled + strings.Repeat(" ", maxW-runewidth.StringWidth(plain))
	}

	// renderExtra returns padded styled columns after the primary span (branch + age).
	renderExtra := func(c rightCols) string {
		var parts []string
		if cw[2] > 0 {
			var s string
			if c.ahead == "∅" {
				s = ui.Faint.Render(c.ahead)
			} else {
				s = ui.Green.Render(c.ahead)
			}
			parts = append(parts, padStyled(s, c.ahead, cw[2]))
		}
		if cw[3] > 0 {
			parts = append(parts, padStyled(ui.Red.Render(c.behind), c.behind, cw[3]))
		}
		if cw[4] > 0 {
			parts = append(parts, padStyled(c.stash, c.stash, cw[4]))
		}
		if cw[5] > 0 {
			parts = append(parts, padStyled(ui.Yellow.Render(c.tag), c.tag, cw[5]))
		}
		return strings.Join(parts, " ")
	}

	// Primary span: left + dots + branch + age. File paths influence this width.
	maxLeftW := 0
	for _, e := range entries {
		maxLeftW = max(maxLeftW, runewidth.StringWidth(fmt.Sprintf("── %s ", e.repo.Name)))
	}
	primaryW := max(60, maxLeftW+3+1+cw[0]+1+cw[1]) // left + min_dots + " " + branch + " " + age
	for _, e := range entries {
		for _, f := range e.status.Files {
			primaryW = max(primaryW, 5+runewidth.StringWidth(f.File)) // "   X " + path
		}
	}

	// Render.
	prevDirty := false
	for i, e := range entries {
		if i > 0 && (prevDirty || !e.status.IsClean) {
			fmt.Println()
		}

		left := fmt.Sprintf("── %s ", e.repo.Name)
		branchW := runewidth.StringWidth(cols[i].branch)
		dots := strings.Repeat("·", primaryW-runewidth.StringWidth(left)-branchW-cw[1]-2)

		var branchStyled string
		if e.status.IsClean {
			branchStyled = cols[i].branch
		} else {
			branchStyled = ui.Cyan.Render(cols[i].branch)
		}

		age := padStyled(ui.Faint.Render(cols[i].age), cols[i].age, cw[1])
		extra := renderExtra(cols[i])

		fmt.Printf("%s%s %s %s",
			ui.Faint.Render("── ")+ui.Bold.Render(e.repo.Name)+" ",
			ui.Faint.Render(dots),
			branchStyled,
			age,
		)
		if extra != "" {
			fmt.Printf(" %s", extra)
		}
		fmt.Println()
		if !e.status.IsClean {
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
