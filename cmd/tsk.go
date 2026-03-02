package cmd

import (
	"bufio"
	"cmp"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"aliz/lz/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/muesli/termenv"
)

// Task status lifecycle.
type Status int

const (
	InProgress Status = iota
	Todo
	Backlog
	Done
)

func (s Status) String() string {
	switch s {
	case InProgress:
		return "In Progress"
	case Todo:
		return "Todo"
	case Backlog:
		return "Backlog"
	case Done:
		return "Done"
	}
	return ""
}

// Filter controls which tasks are visible.
type Filter int

const (
	FilterActive Filter = iota
	FilterBacklog
	FilterDone
	FilterAll
)

// Task is a single task file discovered from a .tasks/ directory.
type Task struct {
	Title    string
	Filename string
	Project  string
	Status   Status
	Path     string
	ModTime  time.Time
}

// RunTsk launches the task browser TUI, or prints a list with --list.
func RunTsk() error {
	var listMode, showAll bool
	for _, arg := range os.Args[2:] {
		switch arg {
		case "--list", "-l":
			listMode = true
		case "--all", "-a":
			showAll = true
		}
	}

	if listMode || showAll {
		return runTskList(showAll)
	}

	root := findRoot()

	// Detect terminal style before entering alt screen — the OSC query
	// for background color times out inside BubbleTea's alt screen.
	// The renderer itself is recreated on resize with the correct width.
	styleOpt := detectGlamourStyle()

	m := initialModel(root, styleOpt)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// runTskList prints tasks to stdout (non-interactive mode).
func runTskList(showAll bool) error {
	root := findRoot()
	tasks := discoverTasks(root)

	filter := FilterActive
	if showAll {
		filter = FilterAll
	}
	m := tskModel{allTasks: tasks, filter: filter}
	m.applyFilter()

	if len(m.filtered) == 0 {
		fmt.Println("No tasks found.")
		return nil
	}

	// Group by status.
	type group struct {
		status Status
		tasks  []Task
	}
	groups := make(map[Status]*group)
	var order []Status
	for _, t := range m.filtered {
		g, ok := groups[t.Status]
		if !ok {
			g = &group{status: t.Status}
			groups[t.Status] = g
			order = append(order, t.Status)
		}
		g.tasks = append(g.tasks, t)
	}
	slices.Sort(order)

	lay := computeTskLayout(m.filtered, false)

	for _, status := range order {
		g := groups[status]

		icon, headerStyle, _ := statusPresentation(status)
		fmt.Println(headerStyle.Render(fmt.Sprintf(" %s %s", icon, status.String())))

		for _, t := range g.tasks {
			_, _, taskStyle := statusPresentation(t.Status)

			projPadded := fmt.Sprintf("%-*s", lay.maxProjLen, t.Project)
			age := ui.RelativeTime(t.ModTime)
			titleW := runewidth.StringWidth(t.Title)
			dots := ui.DotFill(lay.lineW - lay.prefixW - titleW - 1 - len(age))

			fmt.Printf("  %s  %s%s %s  %s\n",
				styleProject.Render(projPadded),
				taskStyle.Render(t.Title),
				styleDots.Render(dots),
				styleAge.Render(age),
				ui.Faint.Render(strings.TrimPrefix(t.Path, root+"/")),
			)
		}
		fmt.Println()
	}
	return nil
}

func findRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".tasks")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "justfile")); err == nil {
				return dir
			}
			if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	fmt.Fprintln(os.Stderr, "lz t: no .tasks/ directory found (searched up to /)")
	cwd, _ := os.Getwd()
	return cwd
}

// ── Discovery ──

func discoverTasks(root string) []Task {
	var tasks []Task

	type project struct {
		name string
		dir  string
	}

	var projects []project
	if info, err := os.Stat(filepath.Join(root, ".tasks")); err == nil && info.IsDir() {
		projects = append(projects, project{"root", root})
	}

	entries, err := os.ReadDir(root)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			child := filepath.Join(root, e.Name())
			if info, err := os.Stat(filepath.Join(child, ".tasks")); err == nil && info.IsDir() {
				projects = append(projects, project{e.Name(), child})
			}
		}
	}

	dirs := []struct {
		name   string
		status Status
	}{
		{"current", InProgress},
		{"todo", Todo},
		{"backlog", Backlog},
		{"done", Done},
	}

	for _, p := range projects {
		tasksDir := filepath.Join(p.dir, ".tasks")

		for _, d := range dirs {
			dir := filepath.Join(tasksDir, d.name)
			if files, err := os.ReadDir(dir); err == nil {
				scanTaskDir(dir, d.status, p.name, files, &tasks)
			} else if d.status == InProgress {
				// Fallback: support legacy current.md single file
				cur := filepath.Join(tasksDir, "current.md")
				if info, err := os.Stat(cur); err == nil {
					tasks = append(tasks, Task{
						Title:    extractTitle(cur),
						Filename: "current.md",
						Project:  p.name,
						Status:   InProgress,
						Path:     cur,
						ModTime:  info.ModTime(),
					})
				}
			}
		}
	}

	return tasks
}

func scanTaskDir(dir string, status Status, project string, files []os.DirEntry, tasks *[]Task) {
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
			continue
		}
		fp := filepath.Join(dir, f.Name())
		info, err := f.Info()
		var mt time.Time
		if err == nil {
			mt = info.ModTime()
		}
		*tasks = append(*tasks, Task{
			Title:    extractTitle(fp),
			Filename: f.Name(),
			Project:  project,
			Status:   status,
			Path:     fp,
			ModTime:  mt,
		})
	}
}

func extractTitle(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return filepath.Base(path)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if t, ok := strings.CutPrefix(line, "## "); ok {
			return t
		}
		if t, ok := strings.CutPrefix(line, "# "); ok {
			return t
		}
	}
	return strings.TrimSuffix(filepath.Base(path), ".md")
}

// ── Styles ──

var (
	styleInProgress = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("3"))
	styleTodo       = lipgloss.NewStyle()
	styleDone       = ui.FaintGreen
	styleProject    = ui.Cyan
	styleCursor = ui.Cursor
	styleDots = ui.Faint
	styleAge  = ui.Faint
)

func statusPresentation(s Status) (icon string, header lipgloss.Style, task lipgloss.Style) {
	switch s {
	case InProgress:
		return "▶", styleInProgress, styleInProgress
	case Todo:
		return "○", lipgloss.NewStyle().Bold(true), styleTodo
	case Backlog:
		return "◇", ui.Faint.Bold(true), ui.Faint
	case Done:
		return "✓", styleDone.Bold(true), styleDone
	}
	return "", lipgloss.NewStyle(), lipgloss.NewStyle()
}

// detectGlamourStyle runs the slow OSC terminal background query once and
// returns the resolved glamour style option. Must be called before alt screen.
func detectGlamourStyle() glamour.TermRendererOption {
	if termenv.HasDarkBackground() {
		return glamour.WithStandardStyle("dark")
	}
	return glamour.WithStandardStyle("light")
}

func renderMarkdown(styleOpt glamour.TermRendererOption, content string, width int) string {
	w := width - 4
	if w < 40 {
		w = 40
	}
	r, err := glamour.NewTermRenderer(styleOpt, glamour.WithWordWrap(w))
	if err != nil {
		return content
	}
	out, err := r.Render(content)
	if err != nil {
		return content
	}
	return out
}

// ── Model ──

type tskModel struct {
	root        string
	allTasks    []Task
	filtered    []Task
	cursor      int
	filter      Filter
	viewing     bool
	detail      ui.Scroll
	content     string
	rendered    string
	detailTitle string
	styleOpt    glamour.TermRendererOption
	width       int
	height      int
}

func initialModel(root string, styleOpt glamour.TermRendererOption) tskModel {
	tasks := discoverTasks(root)
	m := tskModel{root: root, allTasks: tasks, filter: FilterActive, styleOpt: styleOpt}
	m.applyFilter()
	return m
}

func (m *tskModel) applyFilter() {
	m.filtered = nil
	for _, status := range []Status{InProgress, Todo, Backlog, Done} {
		if m.filter == FilterActive && (status == Done || status == Backlog) {
			continue
		}
		if m.filter == FilterBacklog && status != Backlog {
			continue
		}
		if m.filter == FilterDone && status != Done {
			continue
		}
		start := len(m.filtered)
		for _, t := range m.allTasks {
			if t.Status == status {
				m.filtered = append(m.filtered, t)
			}
		}
		if status == Done {
			slices.SortFunc(m.filtered[start:], func(a, b Task) int {
				return b.ModTime.Compare(a.ModTime)
			})
		}
	}
	m.cursor = 0
}

// tskLayout holds precomputed column widths for task list rendering.
type tskLayout struct {
	maxProjLen int
	maxTitleW  int
	maxAgeLen  int
	prefixW    int
	lineW      int
}

func computeTskLayout(tasks []Task, cursorCol bool) tskLayout {
	var l tskLayout
	for _, t := range tasks {
		l.maxProjLen = max(l.maxProjLen, len(t.Project))
		l.maxTitleW = max(l.maxTitleW, runewidth.StringWidth(t.Title))
		l.maxAgeLen = max(l.maxAgeLen, len(ui.RelativeTime(t.ModTime)))
	}
	if cursorCol {
		l.prefixW = 1 + 2 + l.maxProjLen + 2 // "▸ proj  "
	} else {
		l.prefixW = 2 + l.maxProjLen + 2 // "  proj  "
	}
	l.lineW = l.prefixW + l.maxTitleW + 3 + 1 + l.maxAgeLen
	return l
}

type editorDoneMsg struct{ err error }
type renderDoneMsg struct{ rendered string }

func (m tskModel) openEditor() tea.Cmd {
	if len(m.filtered) == 0 {
		return nil
	}
	task := m.filtered[m.cursor]
	editor := cmp.Or(os.Getenv("VISUAL"), os.Getenv("EDITOR"), "vim")
	c := exec.Command(editor, task.Path)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorDoneMsg{err}
	})
}

func (m tskModel) Init() tea.Cmd { return nil }

func (m tskModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.detail.Height = max(msg.Height-4, 1)
	case renderDoneMsg:
		m.rendered = msg.rendered
		m.detail.Total = len(strings.Split(strings.TrimRight(m.rendered, "\n"), "\n"))
	case editorDoneMsg:
		m.viewing = false
		cursor := m.cursor
		m.allTasks = discoverTasks(m.root)
		m.applyFilter()
		m.cursor = cursor
		if m.cursor >= len(m.filtered) {
			m.cursor = len(m.filtered) - 1
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
	case tea.KeyMsg:
		if m.viewing {
			return m.updateDetail(msg)
		}
		return m.updateList(msg)
	}
	return m, nil
}

func (m tskModel) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		} else if len(m.filtered) > 0 {
			m.cursor = len(m.filtered) - 1
		}
	case "down", "j":
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
		} else {
			m.cursor = 0
		}
	case "tab":
		m.filter = (m.filter + 1) % 4
		m.applyFilter()
	case "shift+tab":
		m.filter = (m.filter + 3) % 4
		m.applyFilter()
	case "enter", "right", "l":
		if len(m.filtered) > 0 {
			task := m.filtered[m.cursor]
			data, err := os.ReadFile(task.Path)
			if err != nil {
				m.content = fmt.Sprintf("Error reading file: %v", err)
				m.rendered = m.content
			} else {
				m.content = string(data)
				m.rendered = m.content // show raw until render completes
			}
			m.detailTitle = task.Title
			m.viewing = true
			total := len(strings.Split(strings.TrimRight(m.rendered, "\n"), "\n"))
			m.detail = ui.Scroll{Height: max(m.height-4, 1), Total: total}
			styleOpt, content, width := m.styleOpt, m.content, m.width
			return m, func() tea.Msg {
				return renderDoneMsg{rendered: renderMarkdown(styleOpt, content, width)}
			}
		}
	case "e":
		return m, m.openEditor()
	}
	return m, nil
}

func (m tskModel) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "q", "esc", "backspace", "left", "h":
		m.viewing = false
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "e":
		return m, m.openEditor()
	default:
		m.detail.HandleKey(key)
	}
	return m, nil
}

func (m tskModel) View() string {
	if m.viewing {
		return m.viewDetail()
	}
	return m.viewList()
}

func (m tskModel) viewList() string {
	var b strings.Builder

	b.WriteString(ui.RenderTabBar([]string{"Active", "Backlog", "Done", "All"}, int(m.filter)))
	b.WriteString("\n\n")

	if len(m.filtered) == 0 {
		b.WriteString(ui.Faint.Render("  No tasks found."))
		b.WriteString("\n")
	}

	type tskEntry struct {
		task  Task
		index int
	}
	type tskGroup struct {
		tasks []tskEntry
	}

	groups := make(map[Status]*tskGroup)
	order := []Status{}
	for i, t := range m.filtered {
		g, ok := groups[t.Status]
		if !ok {
			g = &tskGroup{}
			groups[t.Status] = g
			order = append(order, t.Status)
		}
		g.tasks = append(g.tasks, tskEntry{t, i})
	}

	slices.Sort(order)

	lay := computeTskLayout(m.filtered, true)

	var lines []string
	for _, status := range order {
		g := groups[status]

		icon, headerStyle, _ := statusPresentation(status)
		lines = append(lines, headerStyle.Render(fmt.Sprintf(" %s %s", icon, status.String())))

		for _, entry := range g.tasks {
			_, _, taskStyle := statusPresentation(entry.task.Status)

			projPadded := fmt.Sprintf("%-*s", lay.maxProjLen, entry.task.Project)
			age := ui.RelativeTime(entry.task.ModTime)

			titleW := runewidth.StringWidth(entry.task.Title)
			dots := ui.DotFill(lay.lineW - lay.prefixW - titleW - 1 - len(age))

			cursor := "  "
			var proj, title, styledDots, styledAge string

			if entry.index == m.cursor {
				cursor = styleCursor.Render("▸ ")
				proj = styleCursor.Render(projPadded)
				title = styleCursor.Render(entry.task.Title)
				styledDots = styleCursor.Render(dots)
				styledAge = styleCursor.Render(age)
			} else {
				proj = styleProject.Render(projPadded)
				title = taskStyle.Render(entry.task.Title)
				styledDots = styleDots.Render(dots)
				styledAge = styleAge.Render(age)
			}

			line := fmt.Sprintf(" %s%s  %s%s %s", cursor, proj, title, styledDots, styledAge)
			lines = append(lines, line)
		}
		lines = append(lines, "")
	}

	listHeight := m.height - 3
	if listHeight > 0 && len(lines) > listHeight {
		cl := func() int {
			n := 0
			for _, st := range order {
				n++ // header line
				for _, e := range groups[st].tasks {
					if e.index == m.cursor {
						return n
					}
					n++
				}
				n++ // blank line
			}
			return n
		}()
		start := ui.KeepCursorVisible(cl, len(lines), listHeight)
		lines = lines[start:]
		if len(lines) > listHeight {
			lines = lines[:listHeight]
		}
	}

	for _, l := range lines {
		b.WriteString(l)
		b.WriteString("\n")
	}

	b.WriteString(ui.RenderHelp("↑/↓ navigate", "→ open", "e edit", "tab filter", "q quit"))

	return b.String()
}

func (m tskModel) viewDetail() string {
	var b strings.Builder

	header := ui.DetailTitle.Render("← " + m.detailTitle)
	b.WriteString(header)
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", m.width))
	b.WriteString("\n")

	lines := strings.Split(m.rendered, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	for _, l := range m.detail.Visible(lines) {
		b.WriteString(l)
		b.WriteString("\n")
	}

	b.WriteString(ui.RenderHelp("↑/↓ scroll", "g/G top/bottom", "e edit", "← back"+m.detail.Percent()))
	return b.String()
}
