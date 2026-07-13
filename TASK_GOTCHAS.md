# Task code — gotchas

Non-obvious traps in the `lz task` code path (`cmd/tsk.go`, `internal/task/`, `internal/sync/`). If you're editing any of those, **read this first**. Each entry is a one-time lesson from a real bug.

---

## 1. Rewriting a task file with `os.CreateTemp` + `os.Rename` destroys btime

`rename(2)` atomically retargets the directory entry to a *different inode* — it does not write into the old file. On APFS the new inode carries the tmp file's btime (= now), so `stat` on the path afterward reads the new btime. `internal/sync/filetime_darwin.go:fileTimes` reads btime via `Birthtimespec` and feeds it to Notion's `Date` property as the "created" bound, so an atomic-write via rename silently resets every synced task's creation date to "now".

**Do:** in-place rewrite via `os.WriteFile(path, data, mode)`. Truncating and overwriting an existing inode preserves btime. This is what `internal/task/frontmatter.go:WriteFile` does.

**Don't:** tmp+rename for task files, even if you want atomicity. Crash-safety is not worth the btime contamination — the old behavior in `cmd/tsk.go` (prior to the shared frontmatter helpers) was also in-place for this reason.

Symptom, for recognition: a wave of UPDATE operations all showing `date (modified)` as the only change, with Notion pages all getting `Date: <sync-time, sync-time>`. If you see that, check whether a recent change introduced a rename-based write.

## 2. Frontmatter writers must preserve unknown keys

Task frontmatter may carry any of: `priority`, `effort`, `summary`, `shape`, `id`, and future fields the writer hasn't heard of. Any function that rewrites the frontmatter (e.g. to set one field) must round-trip the others in their original order. An old `writeFrontmatter` in `cmd/tsk.go` hardcoded `[priority, effort, summary]` in its emit order and silently dropped everything else — so hitting `1`/`2`/`3` in the TUI on a task with `shape: investigation` would lose the `shape:`.

**Do:** go through `task.ReadFile` / `task.WriteFile`. Its `Frontmatter` type is an ordered slice of pairs and preserves insertion order across Set/Get.

**Don't:** re-implement `map[string]string` + fixed-order emit. It will drop fields.

## 3. `sync.yml` state migrations must be scoped to the current sync root

When `RunSync` walks and re-keys legacy entries, it only knows about tasks under `root` (the project tree of the current invocation). Re-keying entries from *other* project roots would strand their Notion page IDs under synthetic IDs that no live task could ever match — next sync from that other root would see each of its tasks as a fresh CREATE, producing duplicate Notion pages and orphaning the originals.

`internal/sync/state.go:Migrate` takes a `root` parameter and skips entries whose `LastPath` is outside it. Any future schema migration on `sync.yml` must do the same.

## 4. Dry-run must not mutate task files or state

`-n / --dry-run` is a previewing contract. `RunSync` returns before `state.Save()` on the dry-run path, but any helper that touches the filesystem (e.g. `task.EnsureID`, which writes `id:` to frontmatter) must be gated on `!dryRun` in the caller. When in doubt, generate ephemeral values in memory for the plan and let the real-run path do the writes.

Consequence: a dry-run *cannot* perfectly predict the subsequent real-run's change list, because some changes (e.g. `date (modified)` triggered by `EnsureID` bumping mtime) only surface after the real run writes files. That's expected, not a bug.

## 5. Task IDs written to frontmatter must start with a letter

IDs are `a-z` + `2-7` base32. If an ID happened to be all digits, YAML parsers would read `id: 2345672345676` as `!!int`, not `!!str`. lz's own parser is hand-rolled (colon-split, no type coercion) so it wouldn't care, but any third-party tool — `yq`, editor YAML plugins, frontmatter-aware static-site generators — would. `task.NewID` forces the first char into `a-z` to guarantee string parsing everywhere.

**Don't:** swap the ID generator for something that could produce an all-digit first char (hex, raw base32 without the letter-prefix trick) without thinking about the YAML-trap.

## 6. Cross-project moves are UPDATEs, not DELETE+CREATE

There used to be a special case in `internal/sync/sync.go` (pre-id-keyed state, around the 144-153 line range) that decomposed a cross-project change into DELETE(old project) + CREATE(new project). This was necessary when state was path-keyed and "same path, different project" was indistinguishable from "two different tasks". It is **not** necessary with id-keyed state — the Notion API's `pages.update` accepts a Project property change directly.

Reintroducing the DELETE+CREATE shortcut would archive and recreate the Notion page on every move, losing the page's URL, comments, inbound links, and change history. If you find yourself writing "if the project differs, delete and create" logic in the sync path, step back.

## 7. `sync.yml` is human-debuggable on purpose

The state file is YAML because the user reads and occasionally hand-edits it. Don't promote it to SQLite or a binary format casually. The escape hatch exists (see `_tasks/current/state-source-of-truth-and-date-recovery.md`), but only if the YAML stops being scannable at a glance — not before.

## 8. The filesystem is not the source of truth for task metadata

btime corruption (#1) is the symptom; the principle is that `touch`, `cp`, editor "save as", rsync, and untested atomic-write code all mutate filesystem times. Anything sync actually cares about (creation date, first-sync time, future: body hash) should live in `sync.yml` with FS times as a fallback/signal, not a ground truth. That refactor is scoped in the open task linked above.

## 9. Sync only sees pages it already tracks — Notion-side orphans are invisible

`RunSync` diffs local tasks against `sync.yml`'s id-keyed entries. If a Notion page exists that no `sync.yml` entry points to, the sync path has no way to discover or touch it. That's by design (the sync is local-driven), but it means **lost or partially-rebuilt state silently leaves orphans in Notion**. The path-keyed → id-keyed migration on 2026-04-23/24 produced ~37 such orphans because earlier syncs without IDs created twin pages whose old path keys didn't match cleanly under the new keying. `collapseRenames` only collapses *unambiguous* (project, scope, title) matches — it can't recognize "I already created two of these last week."

**Recovery recipe** (no built-in command exists; do it manually):

1. `POST https://api.notion.com/v1/data_sources/<ds_id>/query` with pagination (`page_size: 50`, `start_cursor`). Notion-Version `2025-09-03`. Collect every `id` where `archived==false && in_trash==false`.
2. Diff against `grep page_id: ~/.lz/sync.yml | awk '{print $2}' | tr -d '-' | tr A-Z a-z` (lz strips dashes and lowercases when storing). Notion IDs ∖ state IDs = orphans.
3. For each orphan, look up its title in the live tracked set:
   - **Title matches a tracked page** → very likely a true duplicate, safe to `PATCH /v1/pages/<id>` with `{"in_trash": true}`.
   - **No matching title** → could be pre-lz Work Log history with no markdown twin. Inspect before trashing.

The 32 surviving `33584484…` (2026-04-01) entries in the Work Log are exactly this case — old manually-authored entries that have no `_tasks/` file. Don't sweep them in future audits.

`PATCH /pages` with `in_trash: true` is recoverable for ~30 days from the Notion UI trash. There is no API for hard-delete.

## 10. Running sync from a subdirectory below `.lz.yml` produces a mass-DELETE plan

`findRoot` (`cmd/tsk.go`) walks up from cwd and stops at the first directory holding both `_tasks/` and a `CLAUDE.md`/`justfile` marker. Config discovery (`config.Discover`) then only walks *down* from that root. If the `.lz.yml` that enables sync sits at a parent (e.g. a grouping's `.lz.yml` enabling that project for everything beneath), running `lz task sync` from a leaf subdirectory below it finds a perfectly valid task root *but no enabling config*. Every task gets `syncEnabled() == false` → `eligible` is empty. The DELETE loop then walks `state.Entries` filtered to `LastPath` under the (sub)root — every state entry there has no eligible match → flagged for DELETE. Dry-run cheerfully prints "N to delete," and a real run would archive every Notion page that lives under the subdirectory.

**The guard** in `RunSync` refuses the operation when `len(eligible) == 0` and any state entries are scoped to root, with a hint to re-run from a parent directory. Don't relax this check — even if it were intentional ("the user really did delete every task in this subtree"), the right way to express that is not "config silently disappeared."

**Why findRoot doesn't just walk up further:** roots are intentionally per-grouping. A leaf subdirectory below the grouping having its own `_tasks/` is the design — it's a leaf with its own task list. The bug is that sync inherits `enabled: true` from the parent `.lz.yml` only when discovery starts from a parent root; the leaf can't see it. Fixing this properly would mean either (a) findRoot walks up past `_tasks/` markers when no `.lz.yml` is found at-or-below, or (b) config discovery walks up from root looking for parent `.lz.yml` to merge. Both have edge cases. The guard is the right interim — explicit failure beats silent destruction.
