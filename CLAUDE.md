# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

`lz` is a personal CLI toolkit written in Go. Two commands:

- `lz t` / `lz tsk` — Task browser TUI (BubbleTea)
- `lz g` / `lz git` — Multi-repo git status viewer

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
  git.go             Git status — discovers repos, runs git commands, prints table
internal/
  git/
    discover.go      Finds git repos at cwd and 1-level children
    status.go        Parses `git status --porcelain`, branch, tags, stash
  ui/
    styles.go        Shared lipgloss color/style constants
    format.go        Formatting helpers (relative time, padding)
```

**Task discovery** (`tsk.go`): walks up looking for `.tasks/` dir co-located with `justfile` or `CLAUDE.md` to find project root. Tasks have four states: InProgress (`current/*.md`), Todo (`todo/*.md`), Backlog (`backlog/*.md`), Done (`done/*.md`).

**Git status** (`git.go`): accepts repos from stdin (tab-separated name/path) or auto-discovers them. Runs git porcelain commands via `exec.Command` with `-C <dir>`.

## Dependencies

Core: `charmbracelet/bubbletea` (TUI), `charmbracelet/lipgloss` (styling), `mattn/go-runewidth` (column widths). Go 1.25+.
