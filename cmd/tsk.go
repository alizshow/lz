package cmd

import (
	"cmp"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
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
	FilterNoDone   // like FilterAll but excludes Done (CLI only, not in TUI tab cycle)
	FilterCanceled // canceled-only; never in the tab cycle, reached by explicit toggle
)

// RunTaskSync syncs local tasks to Notion.
func RunTaskSync(dryRun bool) error {
	globalCfg, err := config.LoadGlobalConfig()
	if err != nil {
		return fmt.Errorf("load ~/.lz/config.yml: %w\n\nCreate it with:\n  mkdir -p ~/.lz\n  cat > ~/.lz/config.yml << 'EOF'\n  sync:\n    type: notion\n    notion:\n      api_key: ntn_...\n      database_id: YOUR_DATABASE_ID\n      # Optional: allowlist of Project select values. Empty = accept any.\n      # projects: [BA, Xpand, Infra]\n  EOF", err)
	}
	root := findRoot()
	tasks, configs := discoverTasks(root)
	return lzsync.RunSync(root, tasks, configs, globalCfg, dryRun)
}

// taskSubdirs are the lifecycle stages, each a subdirectory of _tasks/. The
// first four are the main flow; "canceled" is an optional graveyard that stays
// hidden from the TUI tabs and list profiles unless explicitly toggled.
var taskSubdirs = []string{"backlog", "todo", "current", "done", "canceled"}

// RunTaskSetup creates a _tasks/ scaffold (the four lifecycle subdirs) at path,
// defaulting to the current working directory. Idempotent: existing dirs are
// left untouched and reported as already present.
func RunTaskSetup(path string) error {
	if path == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("resolve cwd: %w", err)
		}
		path = cwd
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", path, err)
	}
	if info, err := os.Stat(abs); err != nil {
		return fmt.Errorf("%s does not exist", abs)
	} else if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", abs)
	}

	tasksDir := filepath.Join(abs, "_tasks")
	var created, existed int
	for _, sub := range taskSubdirs {
		dir := filepath.Join(tasksDir, sub)
		if _, err := os.Stat(dir); err == nil {
			existed++
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
		created++
	}

	switch {
	case created == 0:
		fmt.Printf("_tasks/ already set up at %s\n", tasksDir)
	case existed == 0:
		fmt.Printf("Created _tasks/{%s} at %s\n", strings.Join(taskSubdirs, ","), tasksDir)
	default:
		suffix := "s"
		if created == 1 {
			suffix = ""
		}
		fmt.Printf("Created %d missing subdir%s under %s (%d already present)\n",
			created, suffix, tasksDir, existed)
	}
	return nil
}

// newStages are the lifecycle stages a task may be created in. done/ is
// excluded because its files are numbered at close time, and canceled/ is an
// off-ramp — neither is a place work starts.
var newStages = []string{"backlog", "todo", "current"}

// RunTaskNew creates a task file from a title and writes its path to stdout.
// stage/priority/effort/summary/dir are the flag values ("" where unset); only
// the flags actually passed become frontmatter keys, so a plain invocation
// yields a file with no frontmatter at all. When stdin is a pipe, its contents
// are appended below the title heading.
func RunTaskNew(title, stage, priority, effort, summary, dir string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("a task title is required: lz task new \"Some title\"")
	}

	if stage == "" {
		stage = "backlog"
	}
	stage = strings.ToLower(strings.TrimSpace(stage))
	if !slices.Contains(newStages, stage) {
		if slices.Contains(taskSubdirs, stage) {
			return fmt.Errorf("cannot create a task directly in %s/ — tasks reach it by moving through the lifecycle", stage)
		}
		return fmt.Errorf("unknown stage %q (want one of: %s)", stage, strings.Join(newStages, ", "))
	}

	if priority != "" {
		priority = strings.ToLower(strings.TrimSpace(priority))
		if !slices.Contains([]string{"high", "normal", "low"}, priority) {
			return fmt.Errorf("unknown priority %q (want one of: high, normal, low)", priority)
		}
	}
	if effort != "" {
		effort = strings.ToUpper(strings.TrimSpace(effort))
		if !slices.Contains([]string{"S", "M", "L", "XL"}, effort) {
			return fmt.Errorf("unknown effort %q (want one of: S, M, L, XL)", effort)
		}
	}

	tasksDir, err := resolveTasksDir(dir)
	if err != nil {
		return err
	}
	stageDir := filepath.Join(tasksDir, stage)
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", stageDir, err)
	}

	body := "# " + title + "\n"
	piped, err := readPipedStdin()
	if err != nil {
		return err
	}
	if piped != "" {
		body += "\n" + piped
	}

	var fm task.Frontmatter
	if priority != "" {
		fm.Set("priority", priority)
	}
	if effort != "" {
		fm.Set("effort", effort)
	}
	if summary != "" {
		fm.Set("summary", strconv.Quote(summary))
	}

	path, err := claimTaskPath(stageDir, slugify(title))
	if err != nil {
		return err
	}
	if err := task.WriteFile(path, fm, body); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Println(path)
	return nil
}

// resolveTasksDir returns the _tasks/ directory a new task belongs in. With an
// explicit dir it must already hold one (or be one); otherwise the nearest
// _tasks/ at or above cwd wins. Unlike findRoot this never falls back to cwd —
// creating a stray _tasks/ tree because the user was in the wrong directory is
// worse than an error.
func resolveTasksDir(dir string) (string, error) {
	if dir != "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return "", fmt.Errorf("resolve %q: %w", dir, err)
		}
		if filepath.Base(abs) == "_tasks" {
			return abs, nil
		}
		tasksDir := filepath.Join(abs, "_tasks")
		if info, err := os.Stat(tasksDir); err != nil || !info.IsDir() {
			return "", fmt.Errorf("no _tasks/ directory at %s (create one with: lz task init %s)", abs, abs)
		}
		return tasksDir, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve cwd: %w", err)
	}
	for d := cwd; ; {
		tasksDir := filepath.Join(d, "_tasks")
		if info, err := os.Stat(tasksDir); err == nil && info.IsDir() {
			return tasksDir, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return "", fmt.Errorf("no _tasks/ directory found at or above %s (create one with: lz task init)", cwd)
}

// readPipedStdin returns stdin's contents when it is a pipe or file, and ""
// when it is a terminal (nothing was redirected in). The trailing newline is
// normalized so the body always ends with exactly one.
func readPipedStdin() (string, error) {
	info, err := os.Stdin.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice != 0 {
		return "", nil
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	s := strings.TrimSpace(string(data))
	if s == "" {
		return "", nil
	}
	return s + "\n", nil
}

// slugify turns a title into a filename stem: lowercase, non-alphanumerics
// collapsed to single dashes, trimmed. Falls back to "task" for titles with
// nothing slug-able in them (e.g. all CJK or all punctuation).
func slugify(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "task"
	}
	return s
}

// claimTaskPath creates an empty file for stem (suffixing -2, -3, … past
// collisions) and returns its path. O_EXCL claims the name atomically, so two
// concurrent invocations with the same title can't land on one file.
func claimTaskPath(dir, stem string) (string, error) {
	for n := 1; n < 1000; n++ {
		name := stem + ".md"
		if n > 1 {
			name = fmt.Sprintf("%s-%d.md", stem, n)
		}
		path := filepath.Join(dir, name)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			f.Close()
			return path, nil
		}
		if !os.IsExist(err) {
			return "", fmt.Errorf("create %s: %w", path, err)
		}
	}
	return "", fmt.Errorf("too many tasks named %q in %s", stem, dir)
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

// RunTaskList prints tasks to stdout (non-interactive mode).
// Flags are additive: base output is active (current + todo).
// -b adds backlog, -d adds done, -a adds both, -x excludes active,
// -c adds canceled (never included by -a).
func RunTaskList(backlog, done, all, excludeActive, canceled bool) error {
	root := findRoot()
	tasks, _ := discoverTasks(root)

	include := map[task.Status]bool{task.InProgress: true, task.Todo: true}
	if backlog || all {
		include[task.Backlog] = true
	}
	if done || all {
		include[task.Done] = true
	}
	// Canceled stays out of -a/--all; it surfaces only on explicit -c.
	if canceled {
		include[task.Canceled] = true
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
		{"canceled", task.Canceled},
	}

	for _, p := range projects {
		cfg := p.Config
		configs[p.Name] = &cfg
		tasksDir := filepath.Join(p.Dir, "_tasks")

		for _, d := range dirs {
			dir := filepath.Join(tasksDir, d.name)
			if files, err := os.ReadDir(dir); err == nil {
				scanTaskDir(dir, d.status, p.Name, p.Scope, files, &tasks)
			} else if d.status == task.InProgress {
				// Fallback: support legacy current.md single file
				cur := filepath.Join(tasksDir, "current.md")
				if info, err := os.Stat(cur); err == nil {
					meta := extractMeta(cur)
					tasks = append(tasks, task.Task{
						ID:       meta.ID,
						Title:    meta.Title,
						Filename: "current.md",
						Project:  p.Name,
						Scope:    p.Scope,
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

func scanTaskDir(dir string, status task.Status, project, scope string, files []os.DirEntry, tasks *[]task.Task) {
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
			ID:       meta.ID,
			Title:    meta.Title,
			Filename: f.Name(),
			Project:  project,
			Scope:    scope,
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
	ID       string
	Title    string
	Priority task.Priority
	Effort   task.Effort
	Summary  string
}

func extractMeta(path string) taskMeta {
	meta := taskMeta{Priority: task.PriorityNormal, Effort: task.EffortM}
	fm, body, err := task.ReadFile(path)
	if err != nil {
		meta.Title = filepath.Base(path)
		return meta
	}

	switch strings.TrimSpace(fm.Get("priority")) {
	case "high":
		meta.Priority = task.PriorityHigh
	case "low":
		meta.Priority = task.PriorityLow
	}
	if e := fm.Get("effort"); e != "" {
		meta.Effort = task.ParseEffort(e)
	}
	meta.Summary = task.Unquote(fm.Get("summary"))
	meta.ID = fm.Get("id")

	// Title: H1 in the body wins; else summary from frontmatter; else first
	// H2 (legacy fallback for files that never grew an H1); else filename
	// stem. Lines inside fenced code blocks (``` … ```) are skipped — shell
	// comments like `# foo` would otherwise pose as headings.
	//
	// Earlier the loop took whichever heading came first regardless of level,
	// which let "## Status" claim the title role on files that have both a
	// summary and a Status section.
	var firstH2 string
	inFence := false
	for line := range strings.SplitSeq(body, "\n") {
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if t, ok := strings.CutPrefix(line, "# "); ok {
			meta.Title = strings.TrimSpace(t)
			return meta
		}
		if firstH2 == "" {
			if t, ok := strings.CutPrefix(line, "## "); ok {
				firstH2 = strings.TrimSpace(t)
			}
		}
	}
	if meta.Summary != "" {
		meta.Title = meta.Summary
		return meta
	}
	if firstH2 != "" {
		meta.Title = firstH2
		return meta
	}
	meta.Title = strings.TrimSuffix(filepath.Base(path), ".md")
	return meta
}

// ── Styles ──

var (
	styleInProgress  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("3"))
	styleTodo        = lipgloss.NewStyle()
	styleTodoHeader  = lipgloss.NewStyle().Bold(true)
	styleBacklog     = ui.Faint
	styleBacklogHdr  = ui.Faint.Bold(true)
	styleDone        = ui.FaintGreen
	styleDoneHeader  = ui.FaintGreen.Bold(true)
	styleCanceled    = ui.Faint.Strikethrough(true)
	styleCanceledHdr = ui.Faint.Bold(true)
	styleProject     = ui.Cyan
	styleCursor      = ui.Cursor
	styleDots        = ui.Faint
	styleAge         = ui.Faint
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
	case task.Canceled:
		return "✗", styleCanceledHdr, styleCanceled
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
	for _, status := range []task.Status{task.InProgress, task.Todo, task.Backlog, task.Done, task.Canceled} {
		// Canceled is a graveyard: visible only in its dedicated filter, and
		// that filter shows nothing else.
		if (status == task.Canceled) != (m.filter == FilterCanceled) {
			continue
		}
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
		if status == task.Done || status == task.Canceled {
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

// setTaskField updates a single frontmatter field in a task file,
// preserving all other fields.
func setTaskField(path, key, value string) error {
	fm, body, err := task.ReadFile(path)
	if err != nil {
		return err
	}
	fm.Set(key, value)
	return task.WriteFile(path, fm, body)
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
		if m.filter == FilterCanceled {
			m.filter = FilterActive
		} else {
			m.filter = (m.filter + 1) % 4
		}
		m.applyFilter()
	case "shift+tab":
		if m.filter == FilterCanceled {
			m.filter = FilterAll
		} else {
			m.filter = (m.filter + 3) % 4
		}
		m.applyFilter()
	case "c":
		if m.filter == FilterCanceled {
			m.filter = FilterActive
		} else {
			m.filter = FilterCanceled
		}
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

	tabs := []string{"Active", "Backlog", "Done", "All"}
	active := int(m.filter)
	// The canceled view is off the normal tab cycle; surface it as a tab only
	// while it's the one in use, so the bar stays four tabs in normal operation.
	if m.filter == FilterCanceled {
		tabs = append(tabs, "Canceled")
		active = len(tabs) - 1
	}
	b.WriteString(ui.RenderTabBar(tabs, active))
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

	listHeight := m.height - 4
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

	if m.cursor >= 0 && m.cursor < len(m.filtered) {
		rel := strings.TrimPrefix(m.filtered[m.cursor].Path, m.root+"/")
		b.WriteString(ui.Faint.Render("  " + rel))
	}
	b.WriteString("\n")

	b.WriteString(ui.RenderHelp("↑/↓ navigate", "→ open", "e edit", "1/2/3 priority", "tab filter", "c canceled", "q quit"))

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
