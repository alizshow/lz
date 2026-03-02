package cmd

import (
	"cmp"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

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

// ── Config ──

const defaultHistoryLimit = 5

// ── Shared data gathering ──

type repoEntry struct {
	repo    git.Repo
	status  git.RepoStatus
	commits []git.Commit
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
			entries[i].commits = git.RecentCommits(r.Path, defaultHistoryLimit)
		}()
	}
	wg.Wait()

	slices.SortFunc(entries, func(a, b repoEntry) int {
		// root (cwd) always first
		if a.repo.Name == "root" {
			return -1
		}
		if b.repo.Name == "root" {
			return 1
		}
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

type gitTab int

const (
	tabStatus  gitTab = iota
	tabCommits
	tabStash
)

type rowKind int

const (
	rowRepo rowKind = iota
	rowFile
	rowCommit
	rowStash
)

type row struct {
	kind       rowKind
	entryIdx   int // index into gitModel.entries
	fileIdx    int // index into entries[entryIdx].status.Files (only for rowFile)
	repoName   string
	filePath   string
	fileXY     string
	commitHash string
	commitMsg  string
	commitTime time.Time
	stashIndex string
	stashMsg   string
}

// repoCol holds precomputed column strings for a single repo header.
type repoCol struct {
	branch, age, ahead, behind, stash, tag string
}

type gitModel struct {
	entries  []repoEntry
	repoCols []repoCol // parallel to entries
	colW     [6]int    // max width per column: branch, age, ahead, behind, stash, tag
	maxNameW int       // max repo name width
	rows     []row
	cursor   int
	tab      gitTab
	viewing  bool
	detail    ui.Scroll
	diffLines []string
	primaryW  int // width of name-through-age section (dots fill the gap)
	maxHashW  int // max commit hash width (for commits tab alignment)
	maxIdxW   int // max stash index label width (for stash tab alignment)
	maxRowAge int // max age width across entry rows
	width     int
	height    int
}

func initialGitModel() (gitModel, error) {
	entries, err := gatherEntries()
	if err != nil {
		return gitModel{}, err
	}
	m := gitModel{entries: entries, tab: tabStatus}
	m.computeRepoCols()
	m.rebuildRows()
	m.cursor = 0
	return m, nil
}

func (m *gitModel) computeRepoCols() {
	m.repoCols = make([]repoCol, len(m.entries))
	m.colW = [6]int{}
	m.maxNameW = 0
	for i, e := range m.entries {
		s := e.status
		c := &m.repoCols[i]
		c.branch = s.Branch
		c.age = ui.RelativeTime(s.Age)
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
		if s.Tag != "" {
			c.tag = "@" + s.Tag
		}
		for j, v := range [6]string{c.branch, c.age, c.ahead, c.behind, c.stash, c.tag} {
			m.colW[j] = max(m.colW[j], runewidth.StringWidth(v))
		}
		m.maxNameW = max(m.maxNameW, runewidth.StringWidth(e.repo.Name))
	}
}

func (m *gitModel) rebuildRows() {
	switch m.tab {
	case tabStatus:
		m.rows = flattenRows(m.entries)
	case tabCommits:
		m.rows = flattenCommitRows(m.entries)
	case tabStash:
		m.rows = flattenStashRows(m.entries)
	}
	m.maxHashW = 0
	m.maxIdxW = 0
	m.maxRowAge = 0
	for _, r := range m.rows {
		switch r.kind {
		case rowCommit:
			m.maxHashW = max(m.maxHashW, len(r.commitHash))
			m.maxRowAge = max(m.maxRowAge, len(ui.RelativeTime(r.commitTime)))
		case rowStash:
			m.maxIdxW = max(m.maxIdxW, len("stash@{"+r.stashIndex+"}"))
		}
	}
	m.primaryW = m.computePrimaryWidth()
}

// computePrimaryWidth returns the width of the "name···dots···branch  age"
// section. Same approach as runGitList's primaryW.
func (m gitModel) computePrimaryWidth() int {
	maxLeftW := 3 + m.maxNameW + 1 // "── name "
	w := max(60, maxLeftW+3+1+m.colW[0]+1+m.colW[1])

	for _, r := range m.rows {
		switch r.kind {
		case rowFile:
			w = max(w, 5+runewidth.StringWidth(r.filePath))
		case rowCommit:
			w = max(w, 4+len(r.commitHash)+2+runewidth.StringWidth(r.commitMsg)+2+len(ui.RelativeTime(r.commitTime)))
		case rowStash:
			w = max(w, 4+len("stash@{"+r.stashIndex+"}")+2+runewidth.StringWidth(r.stashMsg))
		}
	}
	return w
}

func flattenCommitRows(entries []repoEntry) []row {
	var rows []row
	for i, e := range entries {
		rows = append(rows, row{
			kind:     rowRepo,
			entryIdx: i,
			repoName: e.repo.Name,
		})
		for j, c := range e.commits {
			if j >= defaultHistoryLimit {
				break
			}
			rows = append(rows, row{
				kind:       rowCommit,
				entryIdx:   i,
				repoName:   e.repo.Name,
				commitHash: c.Hash,
				commitMsg:  c.Subject,
				commitTime: c.Time,
			})
		}
	}
	return rows
}

func flattenStashRows(entries []repoEntry) []row {
	var rows []row
	for i, e := range entries {
		if len(e.status.Stashes) == 0 {
			continue
		}
		rows = append(rows, row{
			kind:     rowRepo,
			entryIdx: i,
			repoName: e.repo.Name,
		})
		for j, s := range e.status.Stashes {
			if j >= defaultHistoryLimit {
				break
			}
			rows = append(rows, row{
				kind:       rowStash,
				entryIdx:   i,
				repoName:   e.repo.Name,
				stashIndex: s.Index,
				stashMsg:   s.Message,
			})
		}
	}
	return rows
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

func (m gitModel) moveCursor(from, delta int) int {
	n := len(m.rows)
	if n == 0 {
		return 0
	}
	return (from + delta + n) % n
}

func (m gitModel) Init() tea.Cmd { return nil }

func (m gitModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.detail.Height = max(msg.Height-4, 1)
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
		m.cursor = m.moveCursor(m.cursor, -1)
	case "down", "j":
		m.cursor = m.moveCursor(m.cursor, 1)
	case "tab":
		m.tab = (m.tab + 1) % 3
		m.rebuildRows()
		m.cursor = 0
	case "shift+tab":
		m.tab = (m.tab + 2) % 3
		m.rebuildRows()
		m.cursor = 0
	case "enter", "right", "l":
		if m.cursor >= len(m.rows) {
			break
		}
		r := m.rows[m.cursor]
		e := m.entries[r.entryIdx]
		switch r.kind {
		case rowFile:
			m.diffLines = colorDiff(git.Diff(e.repo.Path, r.filePath, r.fileXY))
			m.viewing = true
			m.detail = ui.Scroll{Height: max(m.height-4, 1), Total: len(m.diffLines)}
		case rowCommit:
			m.diffLines = colorDiff(git.ShowCommit(e.repo.Path, r.commitHash))
			m.viewing = true
			m.detail = ui.Scroll{Height: max(m.height-4, 1), Total: len(m.diffLines)}
		case rowStash:
			m.diffLines = colorDiff(git.ShowStash(e.repo.Path, r.stashIndex))
			m.viewing = true
			m.detail = ui.Scroll{Height: max(m.height-4, 1), Total: len(m.diffLines)}
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

	var b strings.Builder

	// Tab bar
	tabs := []struct {
		label string
		tab   gitTab
	}{
		{"Status", tabStatus},
		{"Commits", tabCommits},
		{"Stash", tabStash},
	}
	var tabParts []string
	for _, t := range tabs {
		if t.tab == m.tab {
			tabParts = append(tabParts, styleActiveTab.Render(t.label))
		} else {
			tabParts = append(tabParts, styleFilterTab.Render(t.label))
		}
	}
	b.WriteString(strings.Join(tabParts, " "))
	b.WriteString("\n\n")

	var lines []string
	for i, r := range m.rows {
		isCursor := i == m.cursor
		switch r.kind {
		case rowRepo:
			lines = append(lines, m.renderRepoRow(r))
		case rowFile:
			lines = append(lines, m.renderFileRow(r, isCursor))
		case rowCommit:
			lines = append(lines, m.renderCommitRow(r, isCursor))
		case rowStash:
			lines = append(lines, m.renderStashRow(r, isCursor))
		}
	}

	listH := m.height - 4 // tab bar + blank + help + padding
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

	b.WriteString(ui.RenderHelp("↑/↓ navigate", "enter detail", "tab switch", "q quit"))
	return b.String()
}

func (m gitModel) renderRepoRow(r row) string {
	e := m.entries[r.entryIdx]
	s := e.status
	c := m.repoCols[r.entryIdx]

	pad := func(s string, w int) string {
		return s + strings.Repeat(" ", max(w-runewidth.StringWidth(s), 0))
	}
	padS := func(styled, plain string, w int) string {
		return styled + strings.Repeat(" ", max(w-runewidth.StringWidth(plain), 0))
	}

	// Dots fill between "── name " and "branch  age" (variable per row)
	left := "── " + e.repo.Name + " "
	dotsW := max(m.primaryW-runewidth.StringWidth(left)-runewidth.StringWidth(c.branch)-m.colW[1]-2, 3)
	dots := strings.Repeat("·", dotsW)

	// Styled branch
	var branchStyled string
	if !s.IsClean {
		branchStyled = ui.Cyan.Render(c.branch)
	} else {
		branchStyled = c.branch
	}
	age := padS(ui.Faint.Render(c.age), c.age, m.colW[1])

	// Styled extras
	type colStyle struct {
		val   string
		style func(string) string
	}
	extraStyles := [4]colStyle{
		{c.ahead, func(v string) string {
			if v == "∅" {
				return ui.Faint.Render(v)
			}
			return ui.Green.Render(v)
		}},
		{c.behind, func(v string) string { return ui.Red.Render(v) }},
		{c.stash, func(v string) string { return v }},
		{c.tag, func(v string) string { return ui.Yellow.Render(v) }},
	}
	var extraStyled string
	for i := 2; i < 6; i++ {
		if m.colW[i] > 0 {
			es := extraStyles[i-2]
			if es.val != "" {
				extraStyled += " " + padS(es.style(es.val), es.val, m.colW[i])
			} else {
				extraStyled += " " + pad("", m.colW[i])
			}
		}
	}

	return ui.Faint.Render("  ── ") + ui.Bold.Render(e.repo.Name) + " " +
		ui.Faint.Render(dots) + " " + branchStyled + " " + age + extraStyled
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

func (m gitModel) renderCommitRow(r row, cursor bool) string {
	hash := r.commitHash
	hashPad := strings.Repeat(" ", max(m.maxHashW-len(hash), 0))
	age := ui.RelativeTime(r.commitTime)
	maxSubjectW := max(m.primaryW-4-m.maxHashW-2-2-m.maxRowAge, 20) // "    hash  subject  age"
	subject := ui.Truncate(r.commitMsg, maxSubjectW)
	dotsW := max(maxSubjectW-runewidth.StringWidth(subject), 0)
	dots := strings.Repeat("·", dotsW)

	if cursor {
		return ui.Cursor.Render("  ▸ " + hash + hashPad + "  " + subject + dots + "  " + age)
	}
	return "    " + ui.Yellow.Render(hash) + hashPad + "  " + subject + ui.Faint.Render(dots) + "  " + ui.Faint.Render(age)
}

func (m gitModel) renderStashRow(r row, cursor bool) string {
	idx := "stash@{" + r.stashIndex + "}"
	idxPad := strings.Repeat(" ", max(m.maxIdxW-len(idx), 0))
	subject := ui.Truncate(r.stashMsg, max(m.primaryW-m.maxIdxW-10, 30))

	if cursor {
		return ui.Cursor.Render("  ▸ " + idx + idxPad + "  " + subject)
	}
	return "    " + ui.Yellow.Render(idx) + idxPad + "  " + subject
}

func (m gitModel) viewDetail() string {
	var b strings.Builder

	r := m.rows[m.cursor]
	var title string
	switch r.kind {
	case rowFile:
		title = r.repoName + " — " + r.filePath
	case rowCommit:
		title = r.repoName + " — " + r.commitHash + " " + r.commitMsg
	case rowStash:
		title = r.repoName + " — stash@{" + r.stashIndex + "} " + r.stashMsg
	default:
		title = r.repoName
	}
	b.WriteString(styleDetailTitle.Render("← " + title))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", m.width))
	b.WriteString("\n")

	for _, l := range m.detail.Visible(m.diffLines) {
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
	case "M ":
		return 'M', ui.Green
	case " M":
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
