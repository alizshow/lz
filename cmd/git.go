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

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// RunGit launches the git status TUI, or prints a list with -l/--list.
func RunGit() error {
	for _, arg := range os.Args[2:] {
		if arg == "-l" || arg == "--list" {
			return runGitList()
		}
	}

	m, err := initialGitModel()
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

// ── Shared data gathering ──

type repoEntry struct {
	repo   git.Repo
	status git.RepoStatus
}

func gatherEntries() ([]repoEntry, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	repos, err := git.Discover(cwd)
	if err != nil {
		return nil, err
	}

	entries := make([]repoEntry, len(repos))
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

	slices.SortFunc(entries, func(a, b repoEntry) int {
		da, db := !a.status.IsClean, !b.status.IsClean
		if da != db {
			if da {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.repo.Name, b.repo.Name)
	})

	return entries, nil
}

// ── Non-interactive list mode (lz g -l) ──

func runGitList() error {
	entries, err := gatherEntries()
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Println("No git repos found.")
		return nil
	}

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

	var cw [6]int
	for _, c := range cols {
		for j, v := range [6]string{c.branch, c.age, c.ahead, c.behind, c.stash, c.tag} {
			cw[j] = max(cw[j], runewidth.StringWidth(v))
		}
	}

	padStyled := func(styled, plain string, maxW int) string {
		return styled + strings.Repeat(" ", maxW-runewidth.StringWidth(plain))
	}
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

	maxLeftW := 0
	for _, e := range entries {
		maxLeftW = max(maxLeftW, runewidth.StringWidth(fmt.Sprintf("── %s ", e.repo.Name)))
	}
	primaryW := max(60, maxLeftW+3+1+cw[0]+1+cw[1])
	for _, e := range entries {
		for _, f := range e.status.Files {
			primaryW = max(primaryW, 5+runewidth.StringWidth(f.File))
		}
	}

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

// ── TUI model ──

type rowKind int

const (
	rowRepo rowKind = iota
	rowFile
)

type row struct {
	kind      rowKind
	entryIdx  int // index into gitModel.entries
	fileIdx   int // index into entries[entryIdx].status.Files (only for rowFile)
	repoName  string
	filePath  string
	fileXY    string
}

type gitModel struct {
	entries []repoEntry
	rows    []row
	cursor  int
	viewing bool
	detail  ui.Scroll
	diff    string
	width   int
	height  int
}

func initialGitModel() (gitModel, error) {
	entries, err := gatherEntries()
	if err != nil {
		return gitModel{}, err
	}
	rows := flattenRows(entries)
	return gitModel{entries: entries, rows: rows}, nil
}

func flattenRows(entries []repoEntry) []row {
	var rows []row
	for i, e := range entries {
		rows = append(rows, row{
			kind:     rowRepo,
			entryIdx: i,
			repoName: e.repo.Name,
		})
		for j, f := range e.status.Files {
			path := f.File
			if strings.Contains(path, " -> ") {
				parts := strings.SplitN(path, " -> ", 2)
				path = parts[1]
			}
			rows = append(rows, row{
				kind:     rowFile,
				entryIdx: i,
				fileIdx:  j,
				repoName: e.repo.Name,
				filePath: path,
				fileXY:   f.XY,
			})
		}
	}
	return rows
}

func (m gitModel) Init() tea.Cmd { return nil }

func (m gitModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		if m.viewing {
			return m.updateDetail(msg)
		}
		return m.updateList(msg)
	}
	return m, nil
}

func (m gitModel) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		} else if len(m.rows) > 0 {
			m.cursor = len(m.rows) - 1
		}
	case "down", "j":
		if m.cursor < len(m.rows)-1 {
			m.cursor++
		} else {
			m.cursor = 0
		}
	case "enter", "right", "l":
		if m.cursor < len(m.rows) && m.rows[m.cursor].kind == rowFile {
			r := m.rows[m.cursor]
			e := m.entries[r.entryIdx]
			m.diff = git.Diff(e.repo.Path, r.filePath, r.fileXY)
			m.viewing = true
			m.detail = ui.Scroll{}
		}
	}
	return m, nil
}

func (m gitModel) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "q", "esc", "backspace", "left", "h":
		m.viewing = false
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	default:
		m.detail.HandleKey(key)
	}
	return m, nil
}

func (m gitModel) View() string {
	if m.viewing {
		return m.viewDetail()
	}
	return m.viewList()
}

func (m gitModel) viewList() string {
	if len(m.entries) == 0 {
		return "No git repos found.\n"
	}

	var lines []string
	for i, r := range m.rows {
		isCursor := i == m.cursor
		switch r.kind {
		case rowRepo:
			lines = append(lines, m.renderRepoRow(r, isCursor))
		case rowFile:
			lines = append(lines, m.renderFileRow(r, isCursor))
		}
	}

	var b strings.Builder
	listH := m.height - 2 // 1 for help, 1 for padding
	if listH > 0 && len(lines) > listH {
		start := ui.KeepCursorVisible(m.cursor, len(lines), listH)
		lines = lines[start:]
		if len(lines) > listH {
			lines = lines[:listH]
		}
	}

	for _, l := range lines {
		b.WriteString(l)
		b.WriteString("\n")
	}

	b.WriteString(ui.RenderHelp("↑/↓ navigate", "enter diff", "q quit"))
	return b.String()
}

func (m gitModel) renderRepoRow(r row, cursor bool) string {
	e := m.entries[r.entryIdx]
	s := e.status

	branch := s.Branch
	age := ui.RelativeTime(s.Age)

	// Build right-side info
	var info []string
	info = append(info, branch)
	if age != "" {
		info = append(info, age)
	}
	if !s.HasUpstream {
		info = append(info, "∅")
	} else if s.Ahead > 0 {
		info = append(info, fmt.Sprintf("↑%d", s.Ahead))
	}
	if s.Behind > 0 {
		info = append(info, fmt.Sprintf("↓%d", s.Behind))
	}
	if s.Stash > 0 {
		info = append(info, fmt.Sprintf("≡%d", s.Stash))
	}
	if s.Tag != "" {
		info = append(info, "@"+s.Tag)
	}

	right := strings.Join(info, "  ")
	left := "── " + e.repo.Name + " "

	// Compute dot fill
	available := m.width - runewidth.StringWidth(left) - runewidth.StringWidth(right) - 4 // margins
	if available < 3 {
		available = 3
	}
	dots := strings.Repeat("·", available)

	if cursor {
		return ui.Cursor.Render("▸ "+e.repo.Name+" ") +
			ui.Cursor.Render(dots+" ") +
			ui.Cursor.Render(right)
	}

	// Style the right side parts individually
	var styledRight []string
	if !s.IsClean {
		styledRight = append(styledRight, ui.Cyan.Render(branch))
	} else {
		styledRight = append(styledRight, branch)
	}
	if age != "" {
		styledRight = append(styledRight, ui.Faint.Render(age))
	}
	if !s.HasUpstream {
		styledRight = append(styledRight, ui.Faint.Render("∅"))
	} else if s.Ahead > 0 {
		styledRight = append(styledRight, ui.Green.Render(fmt.Sprintf("↑%d", s.Ahead)))
	}
	if s.Behind > 0 {
		styledRight = append(styledRight, ui.Red.Render(fmt.Sprintf("↓%d", s.Behind)))
	}
	if s.Stash > 0 {
		styledRight = append(styledRight, fmt.Sprintf("≡%d", s.Stash))
	}
	if s.Tag != "" {
		styledRight = append(styledRight, ui.Yellow.Render("@"+s.Tag))
	}

	return ui.Faint.Render("  ── ") + ui.Bold.Render(e.repo.Name) + " " +
		ui.Faint.Render(dots+" ") +
		strings.Join(styledRight, "  ")
}

func (m gitModel) renderFileRow(r row, cursor bool) string {
	e := m.entries[r.entryIdx]
	f := e.status.Files[r.fileIdx]

	fileLines := renderFile(f)
	line := fileLines[0]
	// For renames, join both lines
	if len(fileLines) > 1 {
		line = strings.Join(fileLines, " ")
	}

	if cursor {
		// Strip existing styling for cursor — re-render plain
		ch, _ := fileSign(f.XY)
		plain := string(ch) + " " + f.File
		return ui.Cursor.Render("  ▸ " + plain)
	}
	return "    " + line
}

func (m gitModel) viewDetail() string {
	var b strings.Builder

	r := m.rows[m.cursor]
	title := r.repoName + " — " + r.filePath
	b.WriteString(styleDetailTitle.Render("← " + title))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", min(m.width, 80)))
	b.WriteString("\n")

	lines := colorDiff(m.diff)

	m.detail.Height = m.height - 4
	if m.detail.Height < 1 {
		m.detail.Height = 20
	}
	for _, l := range m.detail.Visible(lines) {
		b.WriteString(l)
		b.WriteString("\n")
	}

	b.WriteString(ui.RenderHelp("↑/↓ scroll", "g/G top/bottom", "← back"+m.detail.Percent()))
	return b.String()
}

// ── Diff coloring ──

func colorDiff(raw string) []string {
	if raw == "" {
		return []string{ui.Faint.Render("  (no diff)")}
	}
	src := strings.Split(strings.TrimRight(raw, "\n"), "\n")
	out := make([]string, 0, len(src))
	for _, line := range src {
		out = append(out, colorDiffLine(line))
	}
	return out
}

func colorDiffLine(line string) string {
	switch {
	case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
		return ui.Bold.Render(line)
	case strings.HasPrefix(line, "@@"):
		return ui.Cyan.Render(line)
	case strings.HasPrefix(line, "+"):
		return ui.Green.Render(line)
	case strings.HasPrefix(line, "-"):
		return ui.Red.Render(line)
	case strings.HasPrefix(line, "diff "), strings.HasPrefix(line, "index "):
		return ui.Faint.Render(line)
	default:
		return line
	}
}

// ── Shared file rendering ──

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
