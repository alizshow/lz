# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> **Before touching `cmd/tsk.go`, `internal/task/`, or `internal/sync/`, read [`TASK_GOTCHAS.md`](./TASK_GOTCHAS.md).** Each entry there is a one-time lesson from a real bug in the task code path — rewriting frontmatter, migrating sync state, handling dry-run, and so on. Do this first, before anything else in that subtree.

## Project

`lz` is a personal CLI toolkit written in Go. Two command groups:

- `lz task` — Task browser TUI + subcommands (list, add, done, sync)
- `lz git` — Multi-repo git status TUI + subcommands (status, commits, stash)

### Commands

```
lz task                    # TUI browser (default)
lz task list               # active tasks (current + todo)
lz task list -b/--backlog  # active + backlog (additive)
lz task list -d/--done     # active + done (additive)
lz task list -a/--all      # all categories
lz task list -x/--exclude-active  # exclude active, combine with -b/-d
lz task list -xb           # just backlog (short flag combining)
lz task sync               # sync tasks to Notion Work Log
lz task sync -n/--dry-run  # preview sync plan without changes

lz git                     # TUI browser (default)
lz git status              # repo status list
lz git commits             # recent commits
lz git stash               # stash entries
```

## Build & Run

Use `just` for all commands. Do not invoke `go` directly.

```bash
just build              # compile to ./lz
just publish            # build + copy to ~/.local/bin/
just vet                # go vet ./...
```

## Architecture

```
main.go              CLI command tree (urfave/cli v3)
cmd/
  tsk.go             Task browser — BubbleTea Model with Init/Update/View
  git.go             Git status TUI — BubbleTea Model, flat row list + diff detail view
internal/
  config/
    config.go        LzConfig (per-project .lz.yml), GlobalConfig (~/.lz/config.yml), Merge
    discover.go      Recursive _tasks/ discovery with cascading .lz.yml config resolution
  sync/
    sync.go          Diff engine — compares local tasks against state, produces create/update/delete plan
    notion.go        Notion client (jomei/notionapi) — create, update, archive pages; status by ID
    state.go         ~/.lz/sync.yml — maps task paths to Notion page IDs + last-known properties
    lock.go          flock(2) on ~/.lz/sync.lock for concurrent sync prevention
    log.go           Per-run log files in ~/.lz/logs/
  task/
    types.go         Task, Status, Priority, Effort types (shared by cmd/ and sync/)
  git/
    discover.go      Finds git repos at cwd and 1-level children
    status.go        Parses `git status --porcelain`, branch, tags, stash entries, commits, diff
  ui/
    styles.go        Shared lipgloss color/style constants (DetailTitle, Cursor, colors)
    format.go        Formatting helpers (RelativeTime, DotFill, Truncate, PadStyled, RenderTabBar, RenderHelp)
    scroll.go        Reusable scroll viewport (used by both TUIs)
```

**CLI dispatch** (`main.go`): Uses `urfave/cli/v3` for subcommand routing with `UseShortOptionHandling` for POSIX-style flag combining (`-xb`). Each subcommand delegates to an exported function in `cmd/`.

**Project discovery** (`internal/config/`): `Discover(root)` walks the directory tree with a custom recursive walker (via `os.ReadDir` + `os.Stat`), collecting `.lz.yml` config files and `_tasks/` directories in a single pass. Follows symbolic links to directories, with cycle detection via resolved real-path tracking (`filepath.EvalSymlinks` on each symlink entry, skip if already visited). Config cascades top→bottom: scalars override, lists (skip) append, `*bool` fields support nil=inherit. Hardcoded skip floor: `.git`, `node_modules`. Returns `[]Project` with resolved configs. `cmd/tsk.go` then scans each project's `_tasks/{current,todo,backlog,done}/*.md` into `[]Task`.

**Config split**: Per-project `.lz.yml` controls discovery (skip, max_depth, project name) and sync behavior (enabled, project mapping, effort, on_delete). Global `~/.lz/config.yml` holds provider credentials (Notion API key, database ID) and the optional `sync.notion.projects` allowlist that guards against typos creating new Notion select options.

**Task discovery** (`tsk.go`): `findRoot` walks up looking for `.lz.yml` or `_tasks/` dir co-located with `justfile`/`CLAUDE.md` (prints stderr hint if not found). `discoverTasks` delegates to `config.Discover` for recursive project finding, then scans each project's task dirs. Tasks have four states: InProgress (`current/*.md`), Todo (`todo/*.md`), Backlog (`backlog/*.md`), Done (`done/*.md`). Tasks support optional YAML frontmatter with `priority: high|normal|low`; TUI keybinds `1/2/3` to set priority.

**Title extraction** (`tsk.go:extractMeta`): Falls back through H1 → frontmatter `summary:` → first H2 → filename stem, skipping lines inside fenced code blocks. Sub-headings (`##` and below) are last-resort because they're often section markers (`## Status`, `## Plan`) that masquerade as titles when no real title exists.

**Task list** (`tsk.go`): `RunTaskList` uses additive flags — base output is active (current + todo), `-b` adds backlog, `-d` adds done, `-a` adds both, `-x` excludes active. Filter is a `map[Status]bool` inclusion set.

**Shared helpers** (`tsk.go`): `statusPresentation()` maps Status → (icon, headerStyle, taskStyle) — used by both list and TUI modes. `computeTskLayout()` computes column widths shared between `RunTaskList` and `viewList`.

**Git status** (`git.go`): BubbleTea TUI with three tabs (Status, Commits, Stash) and flat row model (repo headers + entry rows). Enter on an entry shows colored diff. Tab/shift+tab cycles tabs. Cursor skips repo header rows. `computeRepoCols()` is a standalone function that computes column strings + widths for repo headers, shared between TUI and list modes. Diff detail view caches wrapped lines (`wrappedLines`) via `rewrapDiff()`; rewrap on enter and on terminal resize only (not per-render). Accepts repos from stdin (tab-separated name/path) or auto-discovers them.

**Task detail** (`tsk.go`): Markdown rendered async via glamour (non-blocking). Terminal style (dark/light) is detected once at startup before alt screen via `detectGlamourStyle()` (avoids OSC timeout); the renderer is recreated on enter and on terminal resize with the current width. Detail view uses full terminal width.

**Notion sync** (`internal/sync/`): `RunSync` diffs local tasks (filtered by `sync.enabled` in `.lz.yml`) against `~/.lz/sync.yml` state file. Produces CREATE/UPDATE/DELETE/SKIP plan, prints it, then executes via `rclod/notion-go`. Status properties use Notion option IDs (not names) so renames in Notion UI don't break sync. State file maps absolute task file paths to Notion page IDs + last-known property values (title, status ID, project, scope, effort). Cross-project moves are handled as delete+create. `flock(2)` prevents concurrent syncs. Per-run logs go to `~/.lz/logs/`. Global credentials in `~/.lz/config.yml`; per-project opt-in via `.lz.yml` `sync:` block (inherits through config cascade).

**Scope** (`internal/config/discover.go`): Scope is the sub-project path relative to the `.lz.yml` that defined the project/sync name. Computed automatically from filesystem structure during discovery — e.g. `infra/kube/_tasks/` → Project "Infra", Scope "kube". Root-level tasks (where `_tasks/` is at the same level as `.lz.yml`) have empty scope. Synced to Notion as a select property. New select options are created automatically by the Notion API when a new scope value appears.

**UI shared** (`ui/`): `RenderTabBar` renders tab bars for both TUIs. `RenderHelp` renders faint help bars. `DotFill` generates dot-leader strings. `DetailTitle` style is shared between both detail views.

## Dependencies

Core: `urfave/cli/v3` (CLI framework), `charmbracelet/bubbletea` (TUI), `charmbracelet/lipgloss` (styling), `mattn/go-runewidth` (column widths), `jomei/notionapi` (Notion API client). Go 1.25+.
