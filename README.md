# lz

A personal CLI toolkit. Fast, opinionated, zero-config.

Two command groups, each a TUI by default with non-interactive subcommands underneath:

- `lz task` — browse and create markdown tasks living in `_tasks/` directories
- `lz git` — multi-repo git status, commits and stashes

## `lz task`

Tasks are markdown files on disk. A project holds a `_tasks/` directory whose subdirectories are the lifecycle stages:

```
_tasks/
  backlog/    someday
  todo/       planned
  current/    in progress
  done/       completed
  canceled/   abandoned — hidden unless asked for
```

`lz task` walks up from `cwd` to find a project root (a `.lz.yml`, or a `_tasks/` co-located with a `justfile`/`CLAUDE.md`), then recursively discovers every `_tasks/` below it. Each one becomes a named project, so a tree of sub-projects lists as a single view.

### Browser (`lz task`)

```
 Active   Backlog   Done   All

 ▶ In Progress
 ▸ api    Migrate auth to OAuth2 ········································· 1d

 ○ Todo
   api    Add rate limiting to public endpoints ·························· 2d
   infra  Upgrade Postgres from 15 to 17 ································· 2w
   web    Refactor dashboard data fetching ······························ 3d

↑/↓ navigate · → open · e edit · 1/2/3 priority · tab filter · c canceled · q quit
```

`→` opens the task rendered as markdown, `e` opens it in `$EDITOR`, `1`/`2`/`3` set priority in place, `s` cycles effort, `c` toggles the otherwise-hidden Canceled view.

### Create (`lz task new`)

```bash
lz task new "Migrate auth to OAuth2"
# → /path/to/api/_tasks/backlog/migrate-auth-to-oauth2.md
```

The title is the only required input. The filename is its slug (suffixed `-2`, `-3` past collisions), and the file goes in the nearest `_tasks/` at or above `cwd`.

| Flag | |
|------|---|
| `-s, --stage` | `backlog` (default), `todo` or `current` |
| `-p, --priority` | `high`, `normal` or `low` |
| `-e, --effort` | `S`, `M`, `L` or `XL` |
| `-m, --summary` | one-line summary |
| `-C, --dir` | project whose `_tasks/` to use, instead of searching up from `cwd` |

Only the flags you pass become frontmatter keys, so a bare invocation writes nothing but the heading:

```markdown
# Migrate auth to OAuth2
```

Piped stdin is appended below that heading, and the created path is the only thing written to stdout — so both of these work:

```bash
lz task new "Investigate btime drift" < notes.md
$EDITOR $(lz task new -s todo "Add rate limiting")
```

Aliases: `n`, `add`.

### List (`lz task list`)

Non-interactive output for scripts and AI sessions. Base output is active work — `current` + `todo` — and the flags are additive.

```
$ lz task list -b
 ▶ In Progress
  api    Migrate auth to OAuth2 ···················· 1d  L  api/_tasks/current/migrate-auth-to-oauth2.md

 ○ Todo
  api    Add rate limiting to public endpoints ····· 2d ↑M  api/_tasks/todo/add-rate-limiting-to-public-endpoints.md
  api    Write integration tests for payment flow ·· 5d  S  api/_tasks/todo/write-integration-tests-for-payment-flow.md
  infra  Upgrade Postgres from 15 to 17 ············ 2w  XL infra/_tasks/todo/upgrade-postgres-from-15-to-17.md
  web    Refactor dashboard data fetching ·········· 3d  M  web/_tasks/todo/refactor-dashboard-data-fetching.md

 ◇ Backlog
  infra  Set up staging environment ················ 3w  M  infra/_tasks/backlog/set-up-staging-environment.md
  web    Dark mode ································· 1mo ↓M web/_tasks/backlog/dark-mode.md
```

`↑`/`↓` before the effort marks high/low priority.

| Flag | |
|------|---|
| `-b, --backlog` | add backlog |
| `-d, --done` | add done |
| `-a, --all` | add both (*not* canceled) |
| `-c, --canceled` | add canceled — the only way to see it |
| `-x, --exclude-active` | drop current + todo, so `-xb` is backlog alone |

Aliases: `l`, `ls`.

### Scaffold (`lz task init [path]`)

Creates `_tasks/{backlog,todo,current,done,canceled}` at `path` (default `cwd`). Idempotent — existing directories are left alone. Alias: `setup`.

### Sync (`lz task sync`)

Optional one-way sync of tasks to a Notion database. Off unless a project opts in. `-n` / `--dry-run` prints the CREATE/UPDATE/DELETE plan without touching anything.

Credentials live in `~/.lz/config.yml`:

```yaml
sync:
  type: notion
  notion:
    api_key: ntn_...
    database_id: YOUR_DATABASE_ID
    projects: [Api, Web, Infra]   # optional allowlist, guards against typos
```

Per-project opt-in lives in `.lz.yml` and inherits down the tree:

```yaml
sync:
  enabled: true
  project: Api
```

State (which task maps to which Notion page) is kept in `~/.lz/sync.yml`, concurrent runs are locked out, and each run logs to `~/.lz/logs/`.

## `lz git`

Scans `cwd` and its immediate children for git repos and reports on all of them in parallel.

```
$ lz git status
── api ············································ main  5m ↑1
   M src/a.go
   A src/handlers.go

── web ············································ main 20m ∅
   ? src/Dashboard.tsx

── docs ··········································· main  1d
── infra ·········································· main  3h ∅ ≡1 @v1.2.0
```

- Dirty repos sort to the top; branch names right-align for scanning
- `↑N` / `↓N` — ahead/behind upstream, `∅` — no upstream, `≡N` — stashes, `@tag` — tag at HEAD

`lz git` with no subcommand opens a TUI with three tabs; `enter` on a file shows its colored diff. The subcommands print the same data to stdout:

| | |
|---|---|
| `lz git status` | working-tree status per repo (aliases: `s`, `st`) |
| `lz git commits` | recent commits per repo (aliases: `c`, `log`) |
| `lz git stash` | stash entries per repo (alias: `z`) |

Repos can also be fed in on stdin as tab-separated `name<TAB>path` lines, instead of being discovered.

## Task file format

Frontmatter is optional and every key in it is optional:

```markdown
---
priority: high        # high | normal (default) | low
effort: L             # S | M (default) | L | XL
summary: "one-liner"  # used as the title if the body has no H1
---

# Migrate auth to OAuth2

Prose about what this is and why.
```

The title is taken from the body's first `H1`, falling back to `summary`, then the first `H2`, then the filename. Writers preserve unknown keys, so anything else you keep in the frontmatter survives edits made through `lz`.

## Config

`.lz.yml`, discovered top-down and merged parent → child:

```yaml
project: Api          # display name; defaults to the path relative to the root
skip: [vendor]        # directory names to skip while discovering (appends)
max_depth: 3
sync: {...}           # see above
```

## Install

```bash
just build       # compile to ./lz
just publish     # build + install to ~/.local/bin/
just vet         # go vet ./...
```

Requires Go 1.26+.
