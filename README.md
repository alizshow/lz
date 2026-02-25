# lz

A personal CLI toolkit. Fast, opinionated, zero-config.

## Commands

### `lz g` — Multi-repo git status

Scans the current directory and its immediate children for git repos, then prints a compact status overview.

```
── api ···································· main  5m ↑1 @v2.3.0
   M src/handlers/auth.go
   M src/middleware/cors.go

── web ···································· main 20m ↑2 @v1.8.0
   M src/components/Dashboard.tsx
   A src/components/Settings.tsx

── infra ·································· main  3h ∅
   M terraform/modules/cdn/main.tf

── docs ··································· main  1d
── shared ································· main  2d ∅  @v0.6.0
── mobile ························· dev/redesign  1w    @v3.1.0
```

- Fetches status in parallel
- Dirty repos sort to the top
- `↑N` / `↓N` — ahead/behind upstream (colored green/red)
- `∅` — no upstream configured
- `≡N` — stash count
- Branch names right-align for easy scanning
- Header width adapts to the longest changed file path

### `lz t` — Task browser TUI

Interactive BubbleTea TUI for browsing `.tasks/` directories. Walks up from `cwd` to find a project root (co-located with `justfile` or `CLAUDE.md`).

```
 Active   Done   All

 ▶ In Progress
 ▸ myapp   Migrate auth to OAuth2 ·········································· 1d

 ○ Todo
   myapp   Add rate limiting to public endpoints ··························· 2d
   myapp   Refactor database connection pooling ···························· 1w
   myapp   Write integration tests for payment flow ························ 3d
   infra   Upgrade Postgres from 15 to 17 ·································· 2w
   infra   Set up staging environment ······································ 1w
   docs    API reference for v2 endpoints ·································· 3d

↑/↓ navigate · → open · e edit · tab filter · q quit
```

Tasks live as markdown files:
- `current.md` — in progress
- `todo/*.md` — planned
- `done/*.md` — completed

## Install

```bash
go install github.com/alizshow/lz@latest
```

Or build locally:

```bash
just build       # compile to ./lz
just publish     # build + copy to ~/.local/bin/
```

Requires Go 1.25+.
