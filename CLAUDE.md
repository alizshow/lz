# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

`lz` is a personal CLI toolkit written in Go. Two commands:

- `lz t` / `lz tsk` — Task browser TUI (BubbleTea)
- `lz g` / `lz git` — Multi-repo git status TUI (`-l` for non-interactive list)

## Build & Run

Use `just` for all commands. Do not invoke `go` directly.

```bash
just build              # compile to ./lz
just publish            # build + copy to ~/.local/bin/
just vet                # go vet ./...
```

## Architecture

```
main.go              CLI dispatcher (switch on os.Args[1])
cmd/
  tsk.go             Task browser — BubbleTea Model with Init/Update/View
  git.go             Git status TUI — BubbleTea Model, flat row list + diff detail view (-l for non-interactive)
internal/
  git/
    discover.go      Finds git repos at cwd and 1-level children
    status.go        Parses `git status --porcelain`, branch, tags, stash entries, commits, diff
  ui/
    styles.go        Shared lipgloss color/style constants (DetailTitle, Cursor, colors)
    format.go        Formatting helpers (RelativeTime, DotFill, Truncate, RenderTabBar, RenderHelp)
    scroll.go        Reusable scroll viewport (used by both TUIs)
```

**Task discovery** (`tsk.go`): `findRoot` walks up looking for `.tasks/` dir co-located with `justfile` or `CLAUDE.md` (prints stderr hint if not found). `discoverTasks` uses `scanTaskDir` helper to scan each status directory. Tasks have four states: InProgress (`current/*.md`), Todo (`todo/*.md`), Backlog (`backlog/*.md`), Done (`done/*.md`).

**Shared helpers** (`tsk.go`): `statusPresentation()` maps Status → (icon, headerStyle, taskStyle) — used by both list and TUI modes. `computeTskLayout()` computes column widths shared between `runTskList` and `viewList`.

**Git status** (`git.go`): BubbleTea TUI with three tabs (Status, Commits, Stash) and flat row model (repo headers + entry rows). Enter on an entry shows colored diff. Tab/shift+tab cycles tabs. Cursor skips repo header rows. `computeRepoCols()` is a standalone function that computes column strings + widths for repo headers, shared between TUI and `-l` modes. Accepts repos from stdin (tab-separated name/path) or auto-discovers them. `-l` flag for non-interactive output.

**Task detail** (`tsk.go`): Markdown rendered async via glamour (non-blocking). Terminal style (dark/light) is detected once at startup before alt screen via `detectGlamourStyle()` (avoids OSC timeout); the renderer is recreated per render with the current terminal width. Detail view uses full terminal width.

**UI shared** (`ui/`): `RenderTabBar` renders tab bars for both TUIs. `RenderHelp` renders faint help bars. `DotFill` generates dot-leader strings. `DetailTitle` style is shared between both detail views.

## Dependencies

Core: `charmbracelet/bubbletea` (TUI), `charmbracelet/lipgloss` (styling), `mattn/go-runewidth` (column widths). Go 1.25+.
