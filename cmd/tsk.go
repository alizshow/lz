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

	"aliz/lz/internal/config"
	lzsync "aliz/lz/internal/sync"
	"aliz/lz/internal/task"
	"aliz/lz/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/muesli/termenv"
)

// Filter controls which tasks are visible.
type Filter int

const (
	FilterActive Filter = iota
	FilterBacklog
	FilterDone
	FilterAll
	FilterNoDone // like FilterAll but excludes Done (CLI only, not in TUI tab cycle)
)


// RunTaskSync syncs local tasks to Notion.
func RunTaskSync(dryRun bool) error {
	globalCfg, err := config.LoadGlobalConfig()
	if err != nil {
		return fmt.Errorf("load ~/.lz/config.yml: %w\n\nCreate it with:\n  mkdir -p ~/.lz\n  cat > ~/.lz/config.yml << 'EOF'\n  sync:\n    type: notion\n    notion:\n      api_key: ntn_...\n      database_id: YOUR_DATABASE_ID\n  EOF", err)
	}
	root := findRoot()
	tasks, configs := discoverTasks(root)
	return lzsync.RunSync(root, tasks, configs, globalCfg, dryRun)
}

// RunTaskTUI launches the interactive task browser.
func RunTaskTUI() error {
	root := findRoot()
	styleOpt := detectGlamourStyle()
	m := initialModel(root, styleOpt)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// RunTaskAdd creates a new task file in _tasks/backlog/.
func RunTaskAdd(title string) error {
	root := findRoot()
	dir := filepath.Join(root, "_tasks", "backlog")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := slugify(title) + ".md"
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("task file already exists: %s", path)
	}
	return os.WriteFile(path, []byte("# "+title+"\n"), 0o644)
}

// RunTaskDone creates a new task file in _tasks/done/.
func RunTaskDone(title string) error {
	root := findRoot()
	dir := filepath.Join(root, "_tasks", "done")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := slugify(title) + ".md"
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("task file already exists: %s", path)
	}
	return os.WriteFile(path, []byte("# "+title+"\n"), 0o644)
}

func slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '_' || r == '-':
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// RunTaskList prints tasks to stdout (non-interactive mode).
// Flags are additive: base output is active (current + todo).
// -b adds backlog, -d adds done, -a adds both, -x excludes active.
func RunTaskList(backlog, done, all, excludeActive bool) error {
	root := findRoot()
	tasks, _ := discoverTasks(root)

	include := map[task.Status]bool{task.InProgress: true, task.Todo: true}
	if backlog || all {
		include[task.Backlog] = true
	}
	if done || all {
		include[task.Done] = true
	}
	if excludeActive {
		delete(include, task.InProgress)
		delete(include, task.Todo)
	}

	var filtered []task.Task
	for _, t := range tasks {
		if include[t.Status] {
			filtered = append(filtered, t)
		}
	}

	if len(filtered) == 0 {
		fmt.Println("No tasks found.")
		return nil
	}

	// Group by status.
	type group struct {
		status task.Status
		tasks  []task.Task
	}
	groups := make(map[task.Status]*group)
	var order []task.Status
	for _, t := range filtered {
		g, ok := groups[t.Status]
		if !ok {
			g = &group{status: t.Status}
			groups[t.Status] = g
			order = append(order, t.Status)
		}
		g.tasks = append(g.tasks, t)
	}
	slices.Sort(order)

	lay := computeTskLayout(filtered, false)

	for _, status := range order {
		g := groups[status]

		icon, headerStyle, _ := statusPresentation(status)
		fmt.Println(headerStyle.Render(fmt.Sprintf(" %s %s", icon, status.String())))

		for _, t := range g.tasks {
			_, _, taskStyle := statusPresentation(t.Status)

			projPadded := fmt.Sprintf("%-*s", lay.maxProjLen, t.Project)
			age := ui.RelativeTime(t.ModTime)
			badge := taskBadge(t.Priority, t.Effort)
			titleW := runewidth.StringWidth(t.Title)
			dots := ui.DotFill(lay.lineW - lay.prefixW - titleW - 1 - len(age))

			fmt.Printf("  %s  %s%s %s %s %s\n",
				styleProject.Render(projPadded),
				taskStyle.Render(t.Title),
				styleDots.Render(dots),
				styleAge.Render(age),
				badge,
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
		// .lz.yml is a root marker on its own.
		if _, err := os.Stat(filepath.Join(dir, ".lz.yml")); err == nil {
			return dir
		}
		// Legacy: _tasks/ co-located with justfile or CLAUDE.md.
		if _, err := os.Stat(filepath.Join(dir, "_tasks")); err == nil {
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
	// If cwd has _tasks/, use it silently — we just couldn't find an anchored root above.
	if _, err := os.Stat(filepath.Join(cwd, "_tasks")); err != nil {
		fmt.Fprintln(os.Stderr, "lz: no _tasks/ directory found (searched up to /)")
	}
	return cwd
}

// ── Discovery ──

func discoverTasks(root string) ([]task.Task, map[string]*config.LzConfig) {
	projects := config.Discover(root)
	var tasks []task.Task
	configs := make(map[string]*config.LzConfig, len(projects))

	dirs := []struct {
		name   string
		status task.Status
	}{
		{"current", task.InProgress},
		{"todo", task.Todo},
		{"backlog", task.Backlog},
		{"done", task.Done},
	}

	for _, p := range projects {
		cfg := p.Config
		configs[p.Name] = &cfg
		tasksDir := filepath.Join(p.Dir, "_tasks")

		for _, d := range dirs {
			dir := filepath.Join(tasksDir, d.name)
			if files, err := os.ReadDir(dir); err == nil {
				scanTaskDir(dir, d.status, p.Name, files, &tasks)
			} else if d.status == task.InProgress {
				// Fallback: support legacy current.md single file
				cur := filepath.Join(tasksDir, "current.md")
				if info, err := os.Stat(cur); err == nil {
					meta := extractMeta(cur)
					tasks = append(tasks, task.Task{
						Title:    meta.Title,
						Filename: "current.md",
						Project:  p.Name,
						Status:   task.InProgress,
						Priority: meta.Priority,
						Path:     cur,
						ModTime:  info.ModTime(),
					})
				}
			}
		}
	}

	return tasks, configs
}

func scanTaskDir(dir string, status task.Status, project string, files []os.DirEntry, tasks *[]task.Task) {
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
		meta := extractMeta(fp)
		*tasks = append(*tasks, task.Task{
			Title:    meta.Title,
			Filename: f.Name(),
			Project:  project,
			Status:   status,
			Priority: meta.Priority,
			Effort:   meta.Effort,
			Summary:  meta.Summary,
			Path:     fp,
			ModTime:  mt,
		})
	}
}

type taskMeta struct {
	Title    string
	Priority task.Priority
	Effort   task.Effort
	Summary  string
}

func extractMeta(path string) taskMeta {
	f, err := os.Open(path)
	if err != nil {
		return taskMeta{Title: filepath.Base(path), Priority: task.PriorityNormal, Effort: task.EffortM}
	}
	defer func() { _ = f.Close() }()

	meta := taskMeta{Priority: task.PriorityNormal, Effort: task.EffortM}
	scanner := bufio.NewScanner(f)

	// Parse optional YAML frontmatter (--- fenced).
	if scanner.Scan() {
		first := scanner.Text()
		if first == "---" {
			for scanner.Scan() {
				line := scanner.Text()
				if line == "---" {
					break
				}
				if v, ok := strings.CutPrefix(line, "priority:"); ok {
					switch strings.TrimSpace(v) {
					case "high":
						meta.Priority = task.PriorityHigh
					case "low":
						meta.Priority = task.PriorityLow
					}
				} else if v, ok := strings.CutPrefix(line, "effort:"); ok {
					meta.Effort = task.ParseEffort(v)
				} else if v, ok := strings.CutPrefix(line, "summary:"); ok {
					meta.Summary = strings.TrimSpace(v)
				}
			}
		} else {
			// First line wasn't frontmatter — check it for a heading.
			if t, ok := strings.CutPrefix(first, "## "); ok {
				meta.Title = t
				return meta
			}
			if t, ok := strings.CutPrefix(first, "# "); ok {
				meta.Title = t
				return meta
			}
		}
	}

	// Scan remaining lines for first heading.
	for scanner.Scan() {
		line := scanner.Text()
		if t, ok := strings.CutPrefix(line, "## "); ok {
			meta.Title = t
			return meta
		}
		if t, ok := strings.CutPrefix(line, "# "); ok {
			meta.Title = t
			return meta
		}
	}

	meta.Title = strings.TrimSuffix(filepath.Base(path), ".md")
	return meta
}

// ── Styles ──

var (
	styleInProgress = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("3"))
	styleTodo       = lipgloss.NewStyle()
	styleTodoHeader = lipgloss.NewStyle().Bold(true)
	styleBacklog    = ui.Faint
	styleBacklogHdr = ui.Faint.Bold(true)
	styleDone       = ui.FaintGreen
	styleDoneHeader = ui.FaintGreen.Bold(true)
	styleProject    = ui.Cyan
	styleCursor     = ui.Cursor
	styleDots       = ui.Faint
	styleAge        = ui.Faint
)

var (
	stylePriHigh = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1"))
	stylePriNorm = ui.Faint
	stylePriLow  = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	styleEffS    = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleEffM    = ui.Faint
	styleEffL    = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styleEffXL   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1"))
)

// taskBadge returns a priority+effort indicator like "↑L" or "–M".
func taskBadge(p task.Priority, e task.Effort) string {
	var pri string
	switch p {
	case task.PriorityHigh:
		pri = stylePriHigh.Render("↑")
	case task.PriorityLow:
		pri = stylePriLow.Render("↓")
	default:
		pri = " "
	}

	label := fmt.Sprintf("%-2s", e.String())
	var eff string
	switch e {
	case task.EffortS:
		eff = styleEffS.Render(label)
	case task.EffortL:
		eff = styleEffL.Render(label)
	case task.EffortXL:
		eff = styleEffXL.Render(label)
	default:
		eff = styleEffM.Render(label)
	}

	return pri + eff
}

func statusPresentation(s task.Status) (icon string, header lipgloss.Style, tsk lipgloss.Style) {
	switch s {
	case task.InProgress:
		return "▶", styleInProgress, styleInProgress
	case task.Todo:
		return "○", styleTodoHeader, styleTodo
	case task.Backlog:
		return "◇", styleBacklogHdr, styleBacklog
	case task.Done:
		return "✓", styleDoneHeader, styleDone
	}
	return "", styleTodo, styleTodo
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
	allTasks    []task.Task
	filtered    []task.Task
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
	tasks, _ := discoverTasks(root)
	m := tskModel{root: root, allTasks: tasks, filter: FilterActive, styleOpt: styleOpt}
	m.applyFilter()
	return m
}

func (m *tskModel) refreshAfterChange(path string) {
	m.allTasks, _ = discoverTasks(m.root)
	m.applyFilter()
	for i, t := range m.filtered {
		if t.Path == path {
			m.cursor = i
			return
		}
	}
}

func (m *tskModel) applyFilter() {
	m.filtered = nil
	for _, status := range []task.Status{task.InProgress, task.Todo, task.Backlog, task.Done} {
		if m.filter == FilterActive && (status == task.Done || status == task.Backlog) {
			continue
		}
		if m.filter == FilterNoDone && status == task.Done {
			continue
		}
		if m.filter == FilterBacklog && status != task.Backlog {
			continue
		}
		if m.filter == FilterDone && status != task.Done {
			continue
		}
		start := len(m.filtered)
		for _, t := range m.allTasks {
			if t.Status == status {
				m.filtered = append(m.filtered, t)
			}
		}
		group := m.filtered[start:]
		if status == task.Done {
			slices.SortFunc(group, func(a, b task.Task) int {
				return b.ModTime.Compare(a.ModTime)
			})
		} else {
			slices.SortStableFunc(group, func(a, b task.Task) int {
				return int(a.Priority) - int(b.Priority)
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

func computeTskLayout(tasks []task.Task, cursorCol bool) tskLayout {
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

// parseFrontmatter splits a task file into its frontmatter fields and body.
func parseFrontmatter(content string) (fields map[string]string, body string) {
	fields = make(map[string]string)
	body = content
	if !strings.HasPrefix(content, "---\n") {
		return
	}
	end := strings.Index(content[4:], "\n---\n")
	if end < 0 {
		return
	}
	for _, line := range strings.Split(content[4:4+end], "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok {
			fields[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	body = strings.TrimLeft(content[4+end+5:], "\n")
	return
}

// writeFrontmatter rebuilds a task file with the given frontmatter fields and body.
func writeFrontmatter(fields map[string]string, body string) string {
	// Collect non-empty fields in a stable order.
	order := []string{"priority", "effort", "summary"}
	var lines []string
	for _, k := range order {
		if v := fields[k]; v != "" {
			lines = append(lines, k+": "+v)
		}
	}
	if len(lines) == 0 {
		return body
	}
	return "---\n" + strings.Join(lines, "\n") + "\n---\n\n" + body
}

// setTaskField updates a single frontmatter field in a task file,
// preserving all other fields.
func setTaskField(path, key, value string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	fields, body := parseFrontmatter(string(data))
	fields[key] = value
	return os.WriteFile(path, []byte(writeFrontmatter(fields, body)), 0o644)
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
		if m.viewing {
			styleOpt, content, width := m.styleOpt, m.content, msg.Width
			return m, func() tea.Msg {
				return renderDoneMsg{rendered: renderMarkdown(styleOpt, content, width)}
			}
		}
	case renderDoneMsg:
		m.rendered = msg.rendered
		m.detail.Total = len(strings.Split(strings.TrimRight(m.rendered, "\n"), "\n"))
	case editorDoneMsg:
		m.viewing = false
		cursor := m.cursor
		m.allTasks, _ = discoverTasks(m.root)
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
	case "1", "2", "3":
		if len(m.filtered) > 0 {
			t := m.filtered[m.cursor]
			pri := map[string]task.Priority{"1": task.PriorityHigh, "2": task.PriorityNormal, "3": task.PriorityLow}[msg.String()]
			if t.Priority != pri {
				priStr := map[task.Priority]string{task.PriorityHigh: "high", task.PriorityNormal: "normal", task.PriorityLow: "low"}[pri]
				_ = setTaskField(t.Path, "priority", priStr)
				(&m).refreshAfterChange(t.Path)
			}
		}
	case "s":
		if len(m.filtered) > 0 {
			t := m.filtered[m.cursor]
			next := map[task.Effort]task.Effort{task.EffortS: task.EffortM, task.EffortM: task.EffortL, task.EffortL: task.EffortXL, task.EffortXL: task.EffortS}[t.Effort]
			_ = setTaskField(t.Path, "effort", next.String())
			(&m).refreshAfterChange(t.Path)
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
		task  task.Task
		index int
	}
	type tskGroup struct {
		tasks []tskEntry
	}

	groups := make(map[task.Status]*tskGroup)
	order := []task.Status{}
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

		icon, headerStyle, taskStyle := statusPresentation(status)
		lines = append(lines, headerStyle.Render(fmt.Sprintf(" %s %s", icon, status.String())))

		for _, entry := range g.tasks {
			projPadded := fmt.Sprintf("%-*s", lay.maxProjLen, entry.task.Project)
			age := ui.RelativeTime(entry.task.ModTime)

			titleW := runewidth.StringWidth(entry.task.Title)
			dots := ui.DotFill(lay.lineW - lay.prefixW - titleW - 1 - len(age))

			cursor := "  "
			var proj, title, styledDots, styledAge, badge string

			if entry.index == m.cursor {
				cursor = styleCursor.Render("▸ ")
				proj = styleCursor.Render(projPadded)
				title = styleCursor.Render(entry.task.Title)
				styledDots = styleCursor.Render(dots)
				styledAge = styleCursor.Render(age)
				badge = styleCursor.Render(
					map[task.Priority]string{task.PriorityHigh: "↑", task.PriorityNormal: " ", task.PriorityLow: "↓"}[entry.task.Priority] +
						fmt.Sprintf("%-2s", entry.task.Effort.String()))
			} else {
				proj = styleProject.Render(projPadded)
				title = taskStyle.Render(entry.task.Title)
				styledDots = styleDots.Render(dots)
				styledAge = styleAge.Render(age)
				badge = taskBadge(entry.task.Priority, entry.task.Effort)
			}

			line := fmt.Sprintf(" %s%s  %s%s %s %s", cursor, proj, title, styledDots, styledAge, badge)
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

	b.WriteString(ui.RenderHelp("↑/↓ navigate", "→ open", "e edit", "1/2/3 priority", "tab filter", "q quit"))

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
