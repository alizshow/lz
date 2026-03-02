package cmd

import (
	"bufio"
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
	m := initialModel(root)
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

	// Compute max widths for alignment.
	maxProjLen := 0
	for _, t := range m.filtered {
		if len(t.Project) > maxProjLen {
			maxProjLen = len(t.Project)
		}
	}
	maxTitleW := 0
	maxAgeLen := 0
	for _, t := range m.filtered {
		tw := runewidth.StringWidth(t.Title)
		if tw > maxTitleW {
			maxTitleW = tw
		}
		al := len(ui.RelativeTime(t.ModTime))
		if al > maxAgeLen {
			maxAgeLen = al
		}
	}
	prefixW := 2 + maxProjLen + 2 // "  proj  "
	lineW := prefixW + maxTitleW + 3 + 1 + maxAgeLen

	for _, status := range order {
		g := groups[status]

		var headerStyle lipgloss.Style
		var icon string
		switch status {
		case InProgress:
			headerStyle = styleInProgress
			icon = "▶"
		case Todo:
			headerStyle = lipgloss.NewStyle().Bold(true)
			icon = "○"
		case Backlog:
			headerStyle = ui.Faint.Bold(true)
			icon = "◇"
		case Done:
			headerStyle = styleDone.Bold(true)
			icon = "✓"
		}
		fmt.Println(headerStyle.Render(fmt.Sprintf(" %s %s", icon, status.String())))

		for _, t := range g.tasks {
			var taskStyle lipgloss.Style
			switch t.Status {
			case InProgress:
				taskStyle = styleInProgress
			case Todo:
				taskStyle = styleTodo
			case Backlog:
				taskStyle = ui.Faint
			case Done:
				taskStyle = styleDone
			}

			projPadded := fmt.Sprintf("%-*s", maxProjLen, t.Project)
			age := ui.RelativeTime(t.ModTime)
			titleW := runewidth.StringWidth(t.Title)
			dotsAvail := max(lineW-prefixW-titleW-1-len(age), 2)
			dots := " " + strings.Repeat("·", dotsAvail-1)

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

	for _, p := range projects {
		tasksDir := filepath.Join(p.dir, ".tasks")

		currentDir := filepath.Join(tasksDir, "current")
		if currentFiles, err := os.ReadDir(currentDir); err == nil {
			for _, f := range currentFiles {
				if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
					continue
				}
				fp := filepath.Join(currentDir, f.Name())
				info, err := f.Info()
				var mt time.Time
				if err == nil {
					mt = info.ModTime()
				}
				tasks = append(tasks, Task{
					Title:    extractTitle(fp),
					Filename: f.Name(),
					Project:  p.name,
					Status:   InProgress,
					Path:     fp,
					ModTime:  mt,
				})
			}
		} else {
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

		todoDir := filepath.Join(tasksDir, "todo")
		if todoFiles, err := os.ReadDir(todoDir); err == nil {
			for _, f := range todoFiles {
				if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
					continue
				}
				fp := filepath.Join(todoDir, f.Name())
				info, err := f.Info()
				var mt time.Time
				if err == nil {
					mt = info.ModTime()
				}
				tasks = append(tasks, Task{
					Title:    extractTitle(fp),
					Filename: f.Name(),
					Project:  p.name,
					Status:   Todo,
					Path:     fp,
					ModTime:  mt,
				})
			}
		}

		backlogDir := filepath.Join(tasksDir, "backlog")
		if backlogFiles, err := os.ReadDir(backlogDir); err == nil {
			for _, f := range backlogFiles {
				if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
					continue
				}
				fp := filepath.Join(backlogDir, f.Name())
				info, err := f.Info()
				var mt time.Time
				if err == nil {
					mt = info.ModTime()
				}
				tasks = append(tasks, Task{
					Title:    extractTitle(fp),
					Filename: f.Name(),
					Project:  p.name,
					Status:   Backlog,
					Path:     fp,
					ModTime:  mt,
				})
			}
		}

		doneDir := filepath.Join(tasksDir, "done")
		if doneFiles, err := os.ReadDir(doneDir); err == nil {
			for _, f := range doneFiles {
				if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
					continue
				}
				fp := filepath.Join(doneDir, f.Name())
				info, err := f.Info()
				var mt time.Time
				if err == nil {
					mt = info.ModTime()
				}
				tasks = append(tasks, Task{
					Title:    extractTitle(fp),
					Filename: f.Name(),
					Project:  p.name,
					Status:   Done,
					Path:     fp,
					ModTime:  mt,
				})
			}
		}
	}

	return tasks
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
	styleCursor     = ui.Cursor
	styleHeader     = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	styleFilterTab  = lipgloss.NewStyle().Padding(0, 1)
	styleActiveTab  = lipgloss.NewStyle().Bold(true).Padding(0, 1).Foreground(lipgloss.Color("4")).Underline(true)
	styleDots       = ui.Faint
	styleAge        = ui.Faint
	styleHelp       = ui.Faint
	styleDetailTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4")).Padding(0, 1)
)

func renderMarkdown(content string, width int) string {
	w := width - 4 // leave some margin
	if w < 40 {
		w = 40
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(w),
	)
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
	width       int
	height      int
}

func initialModel(root string) tskModel {
	tasks := discoverTasks(root)
	m := tskModel{root: root, allTasks: tasks, filter: FilterActive}
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

type editorDoneMsg struct{ err error }

func (m tskModel) openEditor() tea.Cmd {
	if len(m.filtered) == 0 {
		return nil
	}
	task := m.filtered[m.cursor]
	c := exec.Command("vim", task.Path)
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
		if m.viewing && m.content != "" {
			m.rendered = renderMarkdown(m.content, m.width)
		}
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
	case "enter", "right", "l":
		if len(m.filtered) > 0 {
			task := m.filtered[m.cursor]
			data, err := os.ReadFile(task.Path)
			if err != nil {
				m.content = fmt.Sprintf("Error reading file: %v", err)
				m.rendered = m.content
			} else {
				m.content = string(data)
				m.rendered = renderMarkdown(m.content, m.width)
			}
			m.detailTitle = task.Title
			m.viewing = true
			m.detail = ui.Scroll{}
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

func (m tskModel) viewportHeight() int {
	return m.height - 4
}

func (m tskModel) View() string {
	if m.viewing {
		return m.viewDetail()
	}
	return m.viewList()
}

func (m tskModel) viewList() string {
	var b strings.Builder

	tabs := []struct {
		label  string
		filter Filter
	}{
		{"Active", FilterActive},
		{"Backlog", FilterBacklog},
		{"Done", FilterDone},
		{"All", FilterAll},
	}
	var tabParts []string
	for _, t := range tabs {
		if t.filter == m.filter {
			tabParts = append(tabParts, styleActiveTab.Render(t.label))
		} else {
			tabParts = append(tabParts, styleFilterTab.Render(t.label))
		}
	}
	b.WriteString(strings.Join(tabParts, " "))
	b.WriteString("\n\n")

	if len(m.filtered) == 0 {
		b.WriteString(styleHelp.Render("  No tasks found."))
		b.WriteString("\n")
	}

	type group struct {
		status Status
		tasks  []struct {
			task  Task
			index int
		}
	}

	groups := make(map[Status]*group)
	order := []Status{}
	for i, t := range m.filtered {
		g, ok := groups[t.Status]
		if !ok {
			g = &group{status: t.Status}
			groups[t.Status] = g
			order = append(order, t.Status)
		}
		g.tasks = append(g.tasks, struct {
			task  Task
			index int
		}{t, i})
	}

	slices.Sort(order)

	maxProjLen := 0
	for _, t := range m.filtered {
		if len(t.Project) > maxProjLen {
			maxProjLen = len(t.Project)
		}
	}
	prefixW := 1 + 2 + maxProjLen + 2
	maxAgeLen := 0
	maxTitleW := 0
	for _, t := range m.filtered {
		tw := runewidth.StringWidth(t.Title)
		if tw > maxTitleW {
			maxTitleW = tw
		}
		al := len(ui.RelativeTime(t.ModTime))
		if al > maxAgeLen {
			maxAgeLen = al
		}
	}
	lineW := prefixW + maxTitleW + 3 + 1 + maxAgeLen

	var lines []string
	for _, status := range order {
		g := groups[status]

		var headerStyle lipgloss.Style
		var icon string
		switch status {
		case InProgress:
			headerStyle = styleInProgress
			icon = "▶"
		case Todo:
			headerStyle = lipgloss.NewStyle().Bold(true)
			icon = "○"
		case Backlog:
			headerStyle = ui.Faint.Bold(true)
			icon = "◇"
		case Done:
			headerStyle = styleDone.Bold(true)
			icon = "✓"
		}
		lines = append(lines, headerStyle.Render(fmt.Sprintf(" %s %s", icon, status.String())))

		for _, entry := range g.tasks {
			var taskStyle lipgloss.Style
			switch entry.task.Status {
			case InProgress:
				taskStyle = styleInProgress
			case Todo:
				taskStyle = styleTodo
			case Backlog:
				taskStyle = ui.Faint
			case Done:
				taskStyle = styleDone
			}

			projPadded := fmt.Sprintf("%-*s", maxProjLen, entry.task.Project)
			age := ui.RelativeTime(entry.task.ModTime)

			titleW := runewidth.StringWidth(entry.task.Title)
			dotsAvail := max(lineW-prefixW-titleW-1-len(age), 2)
			dots := " " + strings.Repeat("·", dotsAvail-1)

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
		cursorLine := 0
		for _, status := range order {
			g := groups[status]
			cursorLine++
			for _, entry := range g.tasks {
				if entry.index == m.cursor {
					goto found
				}
				cursorLine++
			}
			cursorLine++
		}
	found:
		start := ui.KeepCursorVisible(cursorLine, len(lines), listHeight)
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

	header := styleDetailTitle.Render("← " + m.detailTitle)
	b.WriteString(header)
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", min(m.width, 60)))
	b.WriteString("\n")

	lines := strings.Split(m.rendered, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	m.detail.Height = m.viewportHeight()
	if m.detail.Height < 1 {
		m.detail.Height = 20
	}
	for _, l := range m.detail.Visible(lines) {
		b.WriteString(l)
		b.WriteString("\n")
	}

	b.WriteString(ui.RenderHelp("↑/↓ scroll", "g/G top/bottom", "e edit", "← back"+m.detail.Percent()))
	return b.String()
}
