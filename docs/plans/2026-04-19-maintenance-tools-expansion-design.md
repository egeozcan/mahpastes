# Maintenance Tools Expansion & Tag Reference Integrity Design

## Goal

Round out the maintenance modal with four new tools, add a tag merge feature, and fix latent tag-reference bugs that already exist across `DeleteTag` / `UpdateTag` so merge can be built on a consistent foundation.

## Scope at a glance

- **Part A — Tag reference integrity (prerequisite).** Audit and fix everything keyed on `tag_id` so deletion, rename, and merge behave consistently across schema, in-memory state, and the frontend.
- **Part B — Maintenance tools.** Four additions to the existing `Deduplicate` / `RemoveEmptyTags` surface:
  1. **Merge tags** (tag right-click menu)
  2. **Database compact** (VACUUM + ANALYZE)
  3. **Stale file sweep** (`clip_temp_files/` + `share-staging/`)
  4. **Orphan DB rows sweep** (uninstalled-plugin detritus)

All tools follow the established **scan → dry-run preview → confirm → execute** pattern.

---

## Part A — Tag Reference Integrity

### Audit

Everything currently keyed on a tag's ID:

| Location | Key | FK cascade today | Handled by `DeleteTag` today |
|---|---|---|---|
| `clip_tags.tag_id` | FK | `ON DELETE CASCADE` | via cascade |
| `api_keys.scoped_tag_id` | FK | `ON DELETE CASCADE` | via cascade (**bug: deletes the key**) |
| `shares.tag_id` | FK | none | `StopShare(id)` called before delete |
| `follows.local_tag_id` | FK | `ON DELETE RESTRICT` | **DB rejects the delete — caller sees SQL error** |
| `watched_folders.auto_tag_id` | col | none | **not handled (stale ID)** |
| Hidden tag list (`settings` key `hidden_tags`, JSON `[]int64`) | list | n/a | **not handled (stale ID)** |
| `ServeManager.servers` (runtime) | map key | n/a | **not handled (server keeps running)** |
| Frontend current-folder view | path | n/a | **not handled (broken URL)** |
| Frontend tag filter pills | id | n/a | **reloaded on tag change (OK)** |

**Rename via `UpdateTag`** is mostly safe at the Go layer — it changes `name`, preserves `id`, so every ID-keyed reference survives. Two exceptions:

- **Served subtree.** `ServeManager` caches `ts.tagName` at start time and resolves descendant paths relative to that root (`serve_manager.go:241, 310-313`). If any tag in a renamed subtree (source or a descendant) is being served, the server keeps resolving against the old prefix and returns 404s. This is a pre-existing latent bug addressed in Part A.
- **Frontend current-folder view** resolves by path, not ID, so renaming the current folder's tag (or an ancestor) silently breaks the view. Also pre-existing, also fixed here.

### Fixes

**Schema migration.**

```sql
-- Change api_keys.scoped_tag_id from ON DELETE CASCADE to SET NULL + revoke.
-- Implemented as ALTER via table-rebuild pattern (SQLite can't change FK actions in place).
-- After the rebuild, add a trigger that auto-revokes keys when their scope goes null:
CREATE TRIGGER IF NOT EXISTS api_keys_revoke_on_scope_null
AFTER UPDATE OF scoped_tag_id ON api_keys
WHEN NEW.scoped_tag_id IS NULL AND OLD.scoped_tag_id IS NOT NULL
BEGIN
  UPDATE api_keys SET is_revoked = 1 WHERE id = NEW.id;
END;
```

Rationale: silently deleting a user's API key on tag delete is surprising. `SET NULL` preserves the row for audit; the trigger revokes it so it can't escalate from tag-scoped to unscoped access.

**Two-phase cleanup (not one helper).** `ShareManager.StopShare` runs its own transaction + emits a frontend event (`share_manager.go:557-580`), and `ServeManager.StopServing` mutates runtime state. Neither is rollback-safe, so calling them inside the tag-delete transaction would diverge runtime state from the DB if the tag transaction rolled back. The flow is therefore three explicit phases:

1. **Preconditions check (read-only).** `checkTagReferencePreconditions(tagID) []string` returns a list of blockers. For delete: non-empty `follows` for this tag (user must `UpdateFollowTag` to a different tag or stop the follow first). For merge: same, plus active share, plus any tag in the source subtree being served. Returns empty slice on success.
2. **DB transaction (SQL only, fully rollback-safe).** Pure SQL inside `tx`:
   - For delete: `UPDATE watched_folders SET auto_tag_id = NULL WHERE auto_tag_id = ?`, then remove from hidden-tag list (read `hidden_tags` setting, filter, write back), then `DELETE FROM tags WHERE id = ?`. `api_keys.scoped_tag_id` is handled by the new `SET NULL` FK + auto-revoke trigger below.
   - For merge: `migrateTagReferences(tx, fromID, toID)` does `UPDATE api_keys SET scoped_tag_id = ? WHERE scoped_tag_id = ?`, same for `watched_folders.auto_tag_id`, swap hidden-tag list membership, reassign `clip_tags`, cascade-rename descendants, then `DELETE FROM tags WHERE id = ?` on the source.
3. **Post-commit runtime cleanup.** After `tx.Commit()` succeeds: call `shareManager.StopShare(tagID)` and `serveManager.StopServing(tagID)` (both no-ops if nothing is active). Errors are logged but do not fail the enclosing operation — the tag is gone, and the orphan-rows tool (Part B) is the backstop for any residual DB state.

For **merge**, post-commit runtime cleanup is a no-op by construction: the precondition phase refuses if the source has an active share or is served anywhere in its subtree. Merge never silently tears down networked state.

For **delete**, if we chose to auto-stop the share for the user's convenience (matching today's DeleteTag behavior), that's the post-commit phase. Follows are refused up front because `ON DELETE RESTRICT` would reject the DB delete anyway, and silently dropping an active subscription changes product semantics.

**`UpdateTag` also gets the subtree-served precondition.** `UpdateTag` today runs a single transaction and is rollback-safe at the DB layer, but ServeManager's in-memory cache becomes stale when any tag in the renamed subtree is being served. Part A adds a precondition check: if the old name (or any descendant) is currently served, refuse the rename with "Stop the server on X before renaming." No runtime mutation needed — pure rejection.

**Frontend re-navigation.** `UpdateTag` and `DeleteTag` currently emit **plugin** events only (`pluginManager.EmitEvent`, `app.go:1421-1446`) — those go to Lua handlers, not the frontend runtime bus. The frontend receives nothing today. Part A adds Wails runtime events via the existing `App.emitEvent` helper (`app.go:63-65`, which goes through `wailsbridge`):

- In `UpdateTag`: `a.emitEvent("tag:updated", map[string]any{"id": id, "old_name": oldName, "new_name": name})`
- In `DeleteTag`: `a.emitEvent("tag:deleted", map[string]any{"id": id, "name": name})`
- In `MergeTag`: new `a.emitEvent("tag:merged", map[string]any{"source_id": src, "dest_id": dst, "source_name": srcName, "dest_name": dstName})`

Plugin events stay as-is and the new `tag:merged` is also registered in `plugin/manifest.go:ValidEvents()` (handler convention: `on_tag_merged`). Wails runtime events and plugin events are emitted in pairs so both layers stay in sync.

In `frontend/js/tags.js` / `wails-api.js`:

- Maintain `currentFolderTagID` alongside the existing path-based folder state.
- On any `tag:updated` / `tag:deleted` / `tag:merged` event, re-resolve the current folder by ID from the fresh tag list:
  - If tag still exists → update path to its new name (covers rename + merge destination).
  - If tag is gone → navigate to parent folder, or out of folder mode if no parent.
- Same resolution for tag filter pills — rebuild from IDs after any tag change.

### Tests

Consolidated with the main test list in the "Cross-cutting → Tests" section below. Summary: one Go test per Part A reference-site fix, plus e2e regressions for folder-view re-navigation on rename/delete/merge.

---

## Part B — Maintenance Tools

### 1. Merge tags

**Entry point.** New right-click context menu on tag rows in the tag sidebar (`frontend/js/tags.js`). Tags currently have no context menu — we reuse the generic `ContextMenu` module from `context-menu.js`. Single menu item: **"Merge into…"**.

**Picker.** Click opens a modal with an autocomplete input using `window.TagAutocomplete` (already used in `share.js:484`). User types the destination tag path.

**Semantics (subtree move).**

Given source `a/x` with descendants `a/x/foo`, `a/x/bar` and destination `b/y`:

1. All clips tagged `a/x` are reassigned to `b/y` (via the existing same-tree-exclusivity rule in `AddTagToClip`).
2. All descendants are renamed with the prefix swap: `a/x/foo` → `b/y/foo`, `a/x/bar` → `b/y/bar`. Reuses the existing cascade-rename SQL from `UpdateTag` (`app.go:1407-1408`).
3. `migrateTagReferences(tx, sourceID, destID)` moves scoped API keys, auto-tag watched folders, and hidden-tag list membership from source to destination.
4. Source tag row is deleted.
5. All of the above inside a single transaction.

**Rejections (checked up front, before preview).**

- `source == destination`.
- Destination is a descendant of source (e.g., `a/x` → `a/x/y`) — self-referential.
- Source has an active share (user must stop it first).
- A `follows` row points at the source tag (user must call `UpdateFollowTag` to retarget, or stop the follow first — see `share_manager.go:917-939` for the supported retargeting path).
- **Source tag *or any descendant* is being served by `ServeManager`** (user must stop the server first). Checking only the source misses the case where `a/x/foo` is served and we merge `a/x` into `b/y` — the server would keep resolving against a prefix that no longer exists.
- Any descendant rename would collide with an existing tag on the destination side (e.g., `a/x/foo` → `b/y/foo` but `b/y/foo` already exists).

Each rejection returns a clear error string the frontend surfaces in the preview dialog: `"Cannot merge: tag is currently served on port 8080. Stop the server first."` etc.

**Preview dialog.**

```
Merge "a/x" into "b/y"?
  N clips will be reassigned
  K descendant tags will move to b/y/*
  [Cancel]  [Merge]
```

If preconditions fail, the dialog shows the blockers and a disabled **Merge** button.

**Go methods** (on `App`, exposed to frontend via Wails):

```go
// PreviewMergeTag returns counts and any blocking conditions, no mutation.
func (a *App) PreviewMergeTag(sourceID, destID int64) (MergeTagPreview, error)

// MergeTag executes the merge in a single transaction.
func (a *App) MergeTag(sourceID, destID int64) error
```

```go
type MergeTagPreview struct {
    ClipCount       int      // clips that will be reassigned
    DescendantCount int      // descendant tags that will move
    Blockers        []string // e.g. ["tag is actively shared", "tag is served on port 8080"]
}
```

### 2. Database compact

**Entry.** New button in maintenance modal: **"Compact database"**.

**Flow.** Click shows a small confirm ("This may take a few seconds on large databases"), runs `VACUUM; ANALYZE;`, toasts `"Reclaimed X MB (was Y MB, now Z MB)"`.

**Go methods.**

```go
func (a *App) GetDatabaseSize() (int64, error)           // filesize of clips.db + -wal + -shm
func (a *App) CompactDatabase() (before, after int64, error)
```

Implementation note: `VACUUM` cannot run inside an explicit transaction and requires no other writes for the duration; call `db.Exec("VACUUM")` directly on the pool and accept it blocks briefly. `ANALYZE` follows to refresh query planner stats (may have drifted after big deletions).

### 3. Stale file sweep

**Entry.** New button in maintenance modal: **"Sweep stale files"**.

**Scope.**

- `clip_temp_files/`: any file with mtime older than the `TempClipStore` lease window (60 minutes). We read the existing lease constant so the threshold stays in sync.
- `share-staging/`: any file with mtime older than 24 hours. Share assemblies are normally completed within minutes; 24 h is a conservative "definitely abandoned" floor.

**Preview.** List grouped by source, with filename + age + size, and a total bytes reclaimable.

**Go methods.**

```go
type StaleFile struct {
    Source   string // "clip_temp_files" or "share-staging"
    Name     string
    Size     int64
    AgeHours float64
}

func (a *App) GetStaleFiles() ([]StaleFile, error)
func (a *App) CleanStaleFiles() (count int, bytes int64, err error)
```

### 4. Orphan DB rows sweep

**Entry.** New button in maintenance modal: **"Clean orphan rows"**.

**Scope.**

- `plugin_storage` rows whose `plugin_id` is not in `plugins` (survives the FK cascade only if the FK was briefly off during a migration — defensive).
- `plugin_permissions` rows whose `plugin_id` is not in `plugins` (same).
- `follows` rows whose `local_tag_id` is not in `tags` (belt-and-suspenders for anything that slipped through Part A).
- `watched_folders` rows with `auto_tag_id` pointing at a missing tag (NULL out the column, don't delete the row).
- Hidden-tag list entries pointing at missing tag IDs.

**Preview.** Counts per category.

**Go methods.**

```go
type OrphanReport struct {
    PluginStorage      int
    PluginPermissions  int
    StaleFollows       int
    StaleAutoTags      int  // watched_folders rows that will be NULL-ed
    StaleHiddenTagIDs  int
}

func (a *App) GetOrphanDBRows() (OrphanReport, error)
func (a *App) CleanOrphanDBRows() (OrphanReport, error) // returns rows cleaned per category
```

Runs inside a single transaction.

---

## Cross-cutting

### UI additions

- `frontend/index.html`: three new buttons inside the existing maintenance modal for vacuum, stale files, orphan rows. Reuse existing button styling from the two current buttons.
- `frontend/js/maintenance.js`: three new handlers mirroring the existing `runDeduplicate` / `runRemoveEmptyTags` pattern (fetch preview → close modal → confirm dialog → execute → toast).
- `frontend/js/tags.js`: bind `contextmenu` on tag rows; open `ContextMenu` with a single "Merge into…" item. On click, open a new merge modal (autocomplete input + preview area).
- `frontend/index.html`: new merge modal (small; reuses existing modal shell styling).
- `frontend/js/tags.js` (new helper): `currentFolderTagID` tracking + re-resolution on `tag:updated` / `tag:deleted` / `tag:merged`.

### Wails bindings

Run `make bindings` after the new Go methods land. Exposes:

- `App.PreviewMergeTag(sourceID, destID)`
- `App.MergeTag(sourceID, destID)`
- `App.GetDatabaseSize()`
- `App.CompactDatabase()`
- `App.GetStaleFiles()`
- `App.CleanStaleFiles()`
- `App.GetOrphanDBRows()`
- `App.CleanOrphanDBRows()`

### REST API parity

Add matching endpoints in `api_manager.go` under `/api/v1/maintenance/*` and `/api/v1/tags/{id}/merge`, following existing patterns. Update `cmd/mp` with `mp tag merge <source> <dest>` and `mp maintenance {vacuum,stale-files,orphan-rows}` subcommands.

### Design system compliance

All new UI uses the stone-based palette and existing button/modal patterns from CLAUDE.md (`bg-stone-800` primary, `border-stone-200` borders, IBM Plex Mono, `text-xs font-medium` for body, etc.). No new accent colors introduced.

### Tests

**Go unit tests** (under `app_test.go` or new `maintenance_test.go`):

- `TestMergeTag_BasicReassignment` — clips reassigned, source deleted.
- `TestMergeTag_SubtreeMove` — descendants renamed to new path.
- `TestMergeTag_BlockedByShare` / `_ByFollow` / `_ByServe` / `_ByServedDescendant`.
- `TestMergeTag_BlockedBySelfReference`, `_ByDescendantCollision`.
- `TestMergeTag_MigratesAPIKeyScope` / `_MigratesWatchFolderAutoTag` / `_MigratesHiddenTag`.
- `TestUpdateTag_BlockedByServedSubtree` — new Part A fix (rename must refuse when a descendant is served).
- `TestDeleteTag_BlockedByFollow` — confirms the `ON DELETE RESTRICT` surface is kept and user gets an actionable message.
- `TestDeleteTag_StopsActiveServePostCommit` — runtime teardown runs after DB commit.
- `TestDeleteTag_DoesNotStopServeIfTagDeleteFails` — verifies the two-phase ordering (runtime cleanup is skipped if the SQL layer rolled back).
- `TestDeleteTag_RevokesScopedAPIKey` — Part A migration + trigger.
- `TestDeleteTag_NullsWatchFolderAutoTag` / `_RemovesFromHiddenList`.
- `TestUpdateTag_EmitsFrontendEvent` / `TestDeleteTag_EmitsFrontendEvent` / `TestMergeTag_EmitsFrontendEvent` — all three call `a.emitEvent` with the expected payload.
- `TestCompactDatabase_ReducesSizeAfterBigDelete`.
- `TestGetStaleFiles` / `TestCleanStaleFiles` — with a fake time source for mtime.
- `TestGetOrphanDBRows` / `TestCleanOrphanDBRows` — seed orphans, verify categorization.

**E2e tests** (under `e2e/tests/maintenance/` and `e2e/tests/tags/`):

- `e2e/tests/maintenance/vacuum.spec.ts`
- `e2e/tests/maintenance/stale-files.spec.ts`
- `e2e/tests/maintenance/orphan-rows.spec.ts`
- `e2e/tests/tags/merge.spec.ts` — basic merge, subtree move, blocked by serve, re-navigation after merging current folder.
- `e2e/tests/tags/rename-folder-view.spec.ts` — renaming current folder re-navigates (Part A regression test).
- `e2e/tests/tags/delete-folder-view.spec.ts` — deleting current folder navigates up (Part A regression test).

### Safety

- Every destructive operation has a dry-run preview step.
- All DB mutations inside a single transaction per operation; rollback on any error.
- VACUUM is the one exception (cannot run in a txn) — document the brief blocking window in the confirm dialog.
- FK schema change (api_keys) uses the SQLite table-rebuild pattern, guarded by a migration that's idempotent on re-run.

### Spec location

`docs/plans/2026-04-19-maintenance-tools-expansion-design.md` — matches existing project convention (`docs/plans/YYYY-MM-DD-<topic>-<design|impl>.md`).

---

## Out of scope

- Hash integrity verification, content-type re-detection, bulk format conversion — too niche for "polish" scope.
- Oversized / oldest-clip reports — these are filter/view features, not maintenance actions.
- Watch folder dead-link prune, stale API key audit — belong in their respective feature panes.
- Migrating active shares/follows/serves across merge — refused instead, by design. Revisit if it becomes a repeated request.
- A generic "find tags without clips/colors/descriptions" audit — redundant with existing `RemoveEmptyTags`.

## Open questions — resolved during brainstorm

- **Subtree behavior on merge?** Subtree-move (descendants carry across), not flatten.
- **Shared/followed/served tag merge?** Refused with actionable error, not auto-stopped.
- **`follows` handling on delete/merge?** Refused (matching the existing `ON DELETE RESTRICT` constraint). Users retarget explicitly via `UpdateFollowTag`, or stop the follow. Never dropped silently.
- **`api_keys` cascade on tag delete?** Switched from `CASCADE` to `SET NULL` + trigger auto-revoke, preserving the row for audit.
- **Rename breaking folder view?** Existing latent bug — fixed as part of Part A (re-resolve by ID).
- **Rename breaking a served descendant?** Existing latent bug — fixed by adding a subtree-served precondition to `UpdateTag`.
- **Transaction atomicity with runtime teardown?** Two-phase: validate → SQL transaction → post-commit runtime cleanup. Share/serve teardown cannot participate in SQL rollback, so post-commit is the only correct sequencing.
- **Frontend visibility of tag changes?** `UpdateTag`/`DeleteTag`/`MergeTag` all emit Wails runtime events via `a.emitEvent` in addition to plugin events. The frontend currently receives no tag-change notifications — that's added here.
- **Merge/rename placement?** Tag right-click menu (per-tag operations), not the maintenance modal (app-wide scans).
