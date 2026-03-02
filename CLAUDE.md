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
    status.go        Parses `git status --porcelain`, branch, tags, stash, diff
  ui/
    styles.go        Shared lipgloss color/style constants
    format.go        Formatting helpers (relative time, padding)
    scroll.go        Reusable scroll viewport (used by both TUIs)
```

**Task discovery** (`tsk.go`): walks up looking for `.tasks/` dir co-located with `justfile` or `CLAUDE.md` to find project root. Tasks have four states: InProgress (`current/*.md`), Todo (`todo/*.md`), Backlog (`backlog/*.md`), Done (`done/*.md`).

**Git status** (`git.go`): BubbleTea TUI with flat row model (repo headers + file entries). Enter on a file shows colored diff. Accepts repos from stdin (tab-separated name/path) or auto-discovers them. `-l` flag for non-interactive output.

## Dependencies

Core: `charmbracelet/bubbletea` (TUI), `charmbracelet/lipgloss` (styling), `mattn/go-runewidth` (column widths). Go 1.25+.
