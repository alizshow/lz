# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

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
lz task add <title>        # add to backlog
lz task done <title>       # add to done
lz task sync               # sync to Notion (stub)

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
  git/
    discover.go      Finds git repos at cwd and 1-level children
    status.go        Parses `git status --porcelain`, branch, tags, stash entries, commits, diff
  ui/
    styles.go        Shared lipgloss color/style constants (DetailTitle, Cursor, colors)
    format.go        Formatting helpers (RelativeTime, DotFill, Truncate, PadStyled, RenderTabBar, RenderHelp)
    scroll.go        Reusable scroll viewport (used by both TUIs)
```

**CLI dispatch** (`main.go`): Uses `urfave/cli/v3` for subcommand routing with `UseShortOptionHandling` for POSIX-style flag combining (`-xb`). Each subcommand delegates to an exported function in `cmd/`.

**Task discovery** (`tsk.go`): `findRoot` walks up looking for `_tasks/` dir co-located with `justfile` or `CLAUDE.md` (prints stderr hint if not found). `discoverTasks` uses `scanTaskDir` helper to scan each status directory. Tasks have four states: InProgress (`current/*.md`), Todo (`todo/*.md`), Backlog (`backlog/*.md`), Done (`done/*.md`). Tasks support optional YAML frontmatter with `priority: high|normal|low`; TUI keybinds `1/2/3` to set priority.

**Task list** (`tsk.go`): `RunTaskList` uses additive flags — base output is active (current + todo), `-b` adds backlog, `-d` adds done, `-a` adds both, `-x` excludes active. Filter is a `map[Status]bool` inclusion set.

**Shared helpers** (`tsk.go`): `statusPresentation()` maps Status → (icon, headerStyle, taskStyle) — used by both list and TUI modes. `computeTskLayout()` computes column widths shared between `RunTaskList` and `viewList`.

**Git status** (`git.go`): BubbleTea TUI with three tabs (Status, Commits, Stash) and flat row model (repo headers + entry rows). Enter on an entry shows colored diff. Tab/shift+tab cycles tabs. Cursor skips repo header rows. `computeRepoCols()` is a standalone function that computes column strings + widths for repo headers, shared between TUI and list modes. Diff detail view caches wrapped lines (`wrappedLines`) via `rewrapDiff()`; rewrap on enter and on terminal resize only (not per-render). Accepts repos from stdin (tab-separated name/path) or auto-discovers them.

**Task detail** (`tsk.go`): Markdown rendered async via glamour (non-blocking). Terminal style (dark/light) is detected once at startup before alt screen via `detectGlamourStyle()` (avoids OSC timeout); the renderer is recreated on enter and on terminal resize with the current width. Detail view uses full terminal width.

**UI shared** (`ui/`): `RenderTabBar` renders tab bars for both TUIs. `RenderHelp` renders faint help bars. `DotFill` generates dot-leader strings. `DetailTitle` style is shared between both detail views.

## Dependencies

Core: `urfave/cli/v3` (CLI framework), `charmbracelet/bubbletea` (TUI), `charmbracelet/lipgloss` (styling), `mattn/go-runewidth` (column widths). Go 1.25+.
