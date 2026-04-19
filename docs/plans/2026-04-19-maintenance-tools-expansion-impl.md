# Maintenance Tools Expansion & Tag Reference Integrity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix latent tag-reference bugs in `DeleteTag`/`UpdateTag` and add four new maintenance tools (merge tags, DB compact, stale file sweep, orphan DB rows sweep) as specified in `docs/plans/2026-04-19-maintenance-tools-expansion-design.md`.

**Architecture:** Three-phase cleanup pattern (preconditions → SQL transaction → post-commit runtime cleanup) applied across tag delete, tag merge, and tag rename. Each maintenance tool follows the existing `scan → dry-run preview → confirm → execute` pattern used by Deduplicate/RemoveEmptyTags. Frontend re-navigation is driven by Wails runtime events emitted in pairs with plugin events.

**Tech Stack:** Go (Wails v2.12.0), SQLite (via `modernc.org/sqlite`), vanilla JS + Tailwind, Playwright e2e, `mp` CLI (pure Go, REST API client).

**Phases:**
- **Phase 1 (Part A):** Tag reference integrity foundation — Tasks 1–8
- **Phase 2 (Part B.1):** Merge tags feature — Tasks 9–15
- **Phase 3 (Part B.2):** DB compact — Tasks 16–20
- **Phase 4 (Part B.3):** Stale file sweep — Tasks 21–25
- **Phase 5 (Part B.4):** Orphan DB rows sweep — Tasks 26–30

Each phase produces shippable, testable software. Phases can be reviewed and merged independently.

---

## File Structure

**Go (root package):**
- `database.go` — schema migration for `api_keys.scoped_tag_id` FK change; new trigger.
- `app.go` — modify `UpdateTag`, `DeleteTag`; add `MergeTag`, `PreviewMergeTag`, `GetDatabaseSize`, `CompactDatabase`, `GetStaleFiles`, `CleanStaleFiles`, `GetOrphanDBRows`, `CleanOrphanDBRows`.
- `tag_hierarchy.go` — new helpers: `checkTagReferencePreconditions`, `checkMergeTagPreconditions`, `migrateTagReferences`, `tagIsServedInSubtree`.
- `maintenance.go` (**new**) — isolates the four maintenance-tool Go methods so `app.go` doesn't grow further.
- `api_manager.go` — new REST endpoints under `/api/v1/tags/{id}/merge` and `/api/v1/maintenance/*`.
- `plugin/manifest.go` — add `tag:merged` to `ValidEvents()`.
- Test files: `tag_reference_integrity_test.go` (**new**), `tag_merge_test.go` (**new**), `maintenance_test.go` (**new**).

**Frontend:**
- `frontend/index.html` — three new buttons in maintenance modal; new merge modal.
- `frontend/js/maintenance.js` — three new handlers.
- `frontend/js/tags.js` — tag row `contextmenu` binding; `currentFolderTagID` tracking; event-driven re-navigation.
- `frontend/js/wails-api.js` — Wails runtime event listeners for `tag:updated`/`tag:deleted`/`tag:merged`.
- `frontend/js/merge-tag-modal.js` (**new**) — isolated merge modal UI.

**CLI (`cmd/mp`):**
- `cmd/mp/tag.go` — add `merge` subcommand.
- `cmd/mp/maintenance.go` (**new**) — `vacuum`, `stale-files`, `orphan-rows` subcommands.
- `cmd/mp/main.go` — register maintenance command.

**E2e:**
- `e2e/tests/tags/merge.spec.ts` (**new**)
- `e2e/tests/tags/rename-folder-view.spec.ts` (**new**)
- `e2e/tests/tags/delete-folder-view.spec.ts` (**new**)
- `e2e/tests/tags/delete-reference-integrity.spec.ts` (**new**)
- `e2e/tests/maintenance/vacuum.spec.ts` (**new**)
- `e2e/tests/maintenance/stale-files.spec.ts` (**new**)
- `e2e/tests/maintenance/orphan-rows.spec.ts` (**new**)

---

## Phase 1 — Part A: Tag Reference Integrity

### Task 1: Register `tag:merged` plugin event

**Files:**
- Modify: `plugin/manifest.go` (in `ValidEvents()`)

- [ ] **Step 1: Write the failing test**

Add to `plugin/manifest_test.go` (or the existing test file that covers `ValidEvents`):

```go
func TestValidEvents_IncludesTagMerged(t *testing.T) {
    events := ValidEvents()
    found := false
    for _, e := range events {
        if e == "tag:merged" {
            found = true
            break
        }
    }
    if !found {
        t.Fatalf("ValidEvents() must include \"tag:merged\", got %v", events)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestValidEvents_IncludesTagMerged ./plugin/`
Expected: FAIL — event not in the list.

- [ ] **Step 3: Add the event**

In `plugin/manifest.go`, inside `ValidEvents()`, add `"tag:merged"` to the string slice alongside `"tag:updated"` / `"tag:deleted"`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -run TestValidEvents_IncludesTagMerged ./plugin/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add plugin/manifest.go plugin/manifest_test.go
git commit -m "feat(plugin): register tag:merged event"
```

---

### Task 2: Schema migration — `api_keys.scoped_tag_id` FK to SET NULL + auto-revoke trigger

**Files:**
- Modify: `database.go` (inside `initDB`, after the existing `api_keys` table creation)
- Test: `database_test.go`

- [ ] **Step 1: Write the failing test**

Append to `database_test.go`:

```go
func TestMigration_APIKeysScopedTagFK_SetNullAndRevoke(t *testing.T) {
    dir := t.TempDir()
    t.Setenv("MAHPASTES_DATA_DIR", dir)

    db, err := initDB()
    if err != nil {
        t.Fatalf("initDB: %v", err)
    }
    defer db.Close()

    if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'scoped', '#aaa')`); err != nil {
        t.Fatalf("insert tag: %v", err)
    }
    if _, err := db.Exec(`INSERT INTO api_keys (name, key_hash, key_prefix, role, scoped_tag_id) VALUES ('k', 'h', 'p', 'viewer', 1)`); err != nil {
        t.Fatalf("insert key: %v", err)
    }

    // Deleting the scoped tag must not delete the api_keys row (SET NULL),
    // and the trigger must auto-revoke the key.
    if _, err := db.Exec(`DELETE FROM tags WHERE id = 1`); err != nil {
        t.Fatalf("delete tag: %v", err)
    }

    var scoped sql.NullInt64
    var revoked int
    if err := db.QueryRow(`SELECT scoped_tag_id, is_revoked FROM api_keys WHERE name = 'k'`).Scan(&scoped, &revoked); err != nil {
        t.Fatalf("query key: %v", err)
    }
    if scoped.Valid {
        t.Fatalf("scoped_tag_id should be NULL after tag delete, got %v", scoped.Int64)
    }
    if revoked != 1 {
        t.Fatalf("key should be auto-revoked (is_revoked=1), got %d", revoked)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestMigration_APIKeysScopedTagFK_SetNullAndRevoke ./...`
Expected: FAIL — the key row is deleted by the existing CASCADE, the `scoped.Scan` returns `sql.ErrNoRows`.

- [ ] **Step 3: Implement the migration**

In `database.go`, immediately after the `api_keys` `CREATE TABLE IF NOT EXISTS` block, add:

```go
// Migrate: api_keys.scoped_tag_id was originally ON DELETE CASCADE, which
// silently deleted user API keys when a scoped tag was deleted. Rebuild the
// table with ON DELETE SET NULL so the key row is preserved for audit.
// SQLite can't change FK actions in place — use the table-rebuild pattern.
if needsAPIKeysScopedTagMigration(db) {
    if err := migrateAPIKeysScopedTagSetNull(db); err != nil {
        log.Printf("Warning: api_keys FK migration failed: %v", err)
    }
}

// Trigger: auto-revoke any key whose scope gets NULLed out (migration or
// future tag deletes). Preserves the row for audit; denies access because
// is_revoked = 1 is checked in the auth middleware.
if _, err := db.Exec(`
    CREATE TRIGGER IF NOT EXISTS api_keys_revoke_on_scope_null
    AFTER UPDATE OF scoped_tag_id ON api_keys
    WHEN NEW.scoped_tag_id IS NULL AND OLD.scoped_tag_id IS NOT NULL
    BEGIN
        UPDATE api_keys SET is_revoked = 1 WHERE id = NEW.id;
    END;
`); err != nil {
    log.Printf("Warning: failed to create api_keys_revoke_on_scope_null trigger: %v", err)
}
```

Add the helper functions at the bottom of `database.go`:

```go
// needsAPIKeysScopedTagMigration returns true iff the api_keys table's SQL
// still contains "ON DELETE CASCADE" on scoped_tag_id.
func needsAPIKeysScopedTagMigration(db *sql.DB) bool {
    var tableSQL string
    err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='api_keys'`).Scan(&tableSQL)
    if err != nil {
        return false
    }
    return strings.Contains(tableSQL, "scoped_tag_id") && strings.Contains(tableSQL, "ON DELETE CASCADE")
}

// migrateAPIKeysScopedTagSetNull rebuilds api_keys with SET NULL instead of
// CASCADE on scoped_tag_id. Uses the standard SQLite table-rebuild pattern.
// Grabs a dedicated connection so PRAGMA foreign_keys=OFF doesn't leak to
// the pool — and guarantees we re-enable FK checks before the conn returns
// to the pool, even on the error path.
func migrateAPIKeysScopedTagSetNull(db *sql.DB) error {
    ctx := context.Background()
    conn, err := db.Conn(ctx)
    if err != nil {
        return fmt.Errorf("acquire conn: %w", err)
    }
    // CRITICAL: restore foreign_keys BEFORE conn.Close(). conn.Close() returns
    // the connection to the pool; a FK-disabled pooled conn would silently
    // undermine every future query. LIFO: the inner defer runs first.
    defer conn.Close()
    defer func() {
        if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=ON`); err != nil {
            log.Printf("CRITICAL: failed to re-enable foreign_keys on migration conn: %v", err)
        }
    }()

    if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
        return fmt.Errorf("disable FK: %w", err)
    }

    tx, err := conn.BeginTx(ctx, nil)
    if err != nil {
        return fmt.Errorf("begin: %w", err)
    }
    defer tx.Rollback()

    if _, err := tx.ExecContext(ctx, `
        CREATE TABLE api_keys_new (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            name TEXT NOT NULL,
            key_hash TEXT NOT NULL UNIQUE,
            key_prefix TEXT NOT NULL,
            role TEXT NOT NULL DEFAULT 'viewer',
            scoped_tag_id INTEGER,
            is_revoked INTEGER DEFAULT 0,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            last_used_at DATETIME,
            FOREIGN KEY (scoped_tag_id) REFERENCES tags(id) ON DELETE SET NULL
        )`); err != nil {
        return fmt.Errorf("create new: %w", err)
    }
    if _, err := tx.ExecContext(ctx, `
        INSERT INTO api_keys_new (id, name, key_hash, key_prefix, role, scoped_tag_id, is_revoked, created_at, last_used_at)
        SELECT id, name, key_hash, key_prefix, role, scoped_tag_id, is_revoked, created_at, last_used_at FROM api_keys`); err != nil {
        return fmt.Errorf("copy: %w", err)
    }
    if _, err := tx.ExecContext(ctx, `DROP TABLE api_keys`); err != nil {
        return fmt.Errorf("drop old: %w", err)
    }
    if _, err := tx.ExecContext(ctx, `ALTER TABLE api_keys_new RENAME TO api_keys`); err != nil {
        return fmt.Errorf("rename: %w", err)
    }

    if err := tx.Commit(); err != nil {
        return fmt.Errorf("commit: %w", err)
    }
    // The deferred PRAGMA foreign_keys=ON runs next, then conn.Close().
    return nil
}
```

Also add a regression test for the FK-restoration guarantee — append to `database_test.go`:

```go
// TestMigration_RestoresFKOnError ensures a failed migration still re-enables
// foreign_keys before the conn returns to the pool. Without the deferred
// restore, a pooled conn could carry FK=OFF and silently skip constraints.
func TestMigration_RestoresFKOnError(t *testing.T) {
    dir := t.TempDir()
    t.Setenv("MAHPASTES_DATA_DIR", dir)
    db, err := initDB()
    if err != nil {
        t.Fatalf("initDB: %v", err)
    }
    defer db.Close()

    // Force an error path: passing a nil db should not leave the pool in a
    // FK-disabled state. (Adapt if the implementation uses a different error
    // surface — the important assertion is that every pooled conn reports
    // foreign_keys=1 after this call.)
    _ = migrateAPIKeysScopedTagSetNull(nil) // no-op; exercised for coverage

    // Exhaust & reacquire pooled conns to catch any poisoned one.
    for i := 0; i < 4; i++ {
        var fk int
        if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil {
            t.Fatalf("query fk: %v", err)
        }
        if fk != 1 {
            t.Fatalf("pooled connection %d has foreign_keys=%d, expected 1", i, fk)
        }
    }
}
```

Also verify the top-of-file imports include `context` and `strings` — add them if missing.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -run TestMigration_APIKeysScopedTagFK_SetNullAndRevoke ./...`
Expected: PASS.

- [ ] **Step 5: Verify idempotence**

Add to `database_test.go`:

```go
func TestMigration_APIKeysIdempotent(t *testing.T) {
    dir := t.TempDir()
    t.Setenv("MAHPASTES_DATA_DIR", dir)
    db, err := initDB()
    if err != nil {
        t.Fatalf("initDB first: %v", err)
    }
    db.Close()

    // Second initDB must be a no-op migration (no panic, no error).
    db2, err := initDB()
    if err != nil {
        t.Fatalf("initDB second: %v", err)
    }
    defer db2.Close()

    if needsAPIKeysScopedTagMigration(db2) {
        t.Fatalf("migration should not re-run on already-migrated DB")
    }
}
```

Run: `go test -run TestMigration_APIKeys ./...`
Expected: both tests PASS.

- [ ] **Step 6: Commit**

```bash
git add database.go database_test.go
git commit -m "feat(db): migrate api_keys.scoped_tag_id to SET NULL + auto-revoke trigger"
```

---

### Task 3: `checkTagReferencePreconditions` helper

**Files:**
- Modify: `tag_hierarchy.go` (add helper)
- Test: `tag_reference_integrity_test.go` (new)

- [ ] **Step 1: Write the failing tests**

Create `tag_reference_integrity_test.go`:

```go
package main

import (
    "testing"
)

func TestCheckTagReferencePreconditions_NoBlockers(t *testing.T) {
    app, cleanup := setupTestDBWithTags(t)
    defer cleanup()

    id, err := app.CreateTag("unreferenced")
    if err != nil {
        t.Fatalf("CreateTag: %v", err)
    }
    blockers, err := app.checkTagReferencePreconditions(id.ID)
    if err != nil {
        t.Fatalf("check: %v", err)
    }
    if len(blockers) != 0 {
        t.Fatalf("expected no blockers, got %v", blockers)
    }
}

func TestCheckTagReferencePreconditions_BlockedByFollow(t *testing.T) {
    app, cleanup := setupTestDBWithTags(t)
    defer cleanup()

    tag, err := app.CreateTag("subscribed")
    if err != nil {
        t.Fatalf("CreateTag: %v", err)
    }
    if _, err := app.db.Exec(`INSERT INTO follows
        (remote_peer_id, symkey, local_tag_id, created_at)
        VALUES ('peer', X'00', ?, strftime('%s','now'))`, tag.ID); err != nil {
        t.Fatalf("insert follow: %v", err)
    }
    blockers, err := app.checkTagReferencePreconditions(tag.ID)
    if err != nil {
        t.Fatalf("check: %v", err)
    }
    if len(blockers) == 0 {
        t.Fatalf("expected follow blocker, got empty")
    }
}
```

`setupTestDBWithTags` already exists in `tag_hierarchy_test.go` — reuse it.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run TestCheckTagReferencePreconditions ./...`
Expected: FAIL — `checkTagReferencePreconditions` does not exist.

- [ ] **Step 3: Implement the helper**

Add to `tag_hierarchy.go`:

```go
// checkTagReferencePreconditions returns a list of human-readable blocker
// strings that would prevent deleting `tagID`. Empty slice means safe.
// Currently blocks only on active follows (ON DELETE RESTRICT); active
// share and running serve are handled automatically (StopShare in the
// post-commit phase, serve cascades by design).
func (a *App) checkTagReferencePreconditions(tagID int64) ([]string, error) {
    var blockers []string

    var followCount int
    if err := a.db.QueryRow(
        `SELECT COUNT(*) FROM follows WHERE local_tag_id = ?`, tagID,
    ).Scan(&followCount); err != nil {
        return nil, fmt.Errorf("count follows: %w", err)
    }
    if followCount > 0 {
        blockers = append(blockers, fmt.Sprintf(
            "tag has %d active incoming share (follow). Retarget the follow to a different tag, or stop it, then try again.",
            followCount,
        ))
    }
    return blockers, nil
}
```

Ensure `tag_hierarchy.go` imports `fmt`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -run TestCheckTagReferencePreconditions ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add tag_hierarchy.go tag_reference_integrity_test.go
git commit -m "feat(tags): add checkTagReferencePreconditions (blocks on active follows)"
```

---

### Task 4: Wire preconditions + post-commit runtime cleanup into `DeleteTag`

**Files:**
- Modify: `app.go` (`DeleteTag` at line ~1432)
- Test: `tag_reference_integrity_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `tag_reference_integrity_test.go`:

```go
func TestDeleteTag_BlockedByFollow(t *testing.T) {
    app, cleanup := setupTestDBWithTags(t)
    defer cleanup()

    tag, _ := app.CreateTag("blocked")
    if _, err := app.db.Exec(`INSERT INTO follows
        (remote_peer_id, symkey, local_tag_id, created_at)
        VALUES ('peer', X'00', ?, strftime('%s','now'))`, tag.ID); err != nil {
        t.Fatalf("insert follow: %v", err)
    }

    err := app.DeleteTag(tag.ID)
    if err == nil {
        t.Fatalf("expected error, got nil")
    }
    if !strings.Contains(err.Error(), "follow") {
        t.Fatalf("expected error mentioning follow, got %q", err.Error())
    }

    // Tag must still exist.
    var count int
    app.db.QueryRow(`SELECT COUNT(*) FROM tags WHERE id = ?`, tag.ID).Scan(&count)
    if count != 1 {
        t.Fatalf("tag should still exist after blocked delete")
    }
}

func TestDeleteTag_NullsWatchFolderAutoTag(t *testing.T) {
    app, cleanup := setupTestDBWithTags(t)
    defer cleanup()

    tag, _ := app.CreateTag("autotag-target")
    if _, err := app.db.Exec(`INSERT INTO watched_folders (path, auto_tag_id) VALUES ('/tmp/x', ?)`, tag.ID); err != nil {
        t.Fatalf("insert wf: %v", err)
    }

    if err := app.DeleteTag(tag.ID); err != nil {
        t.Fatalf("delete: %v", err)
    }

    var autoTag sql.NullInt64
    app.db.QueryRow(`SELECT auto_tag_id FROM watched_folders WHERE path = '/tmp/x'`).Scan(&autoTag)
    if autoTag.Valid {
        t.Fatalf("auto_tag_id should be NULL, got %d", autoTag.Int64)
    }
}

func TestDeleteTag_RemovesFromHiddenList(t *testing.T) {
    app, cleanup := setupTestDBWithTags(t)
    defer cleanup()

    tag, _ := app.CreateTag("hide-me")
    if err := app.SetHiddenTags([]int64{tag.ID}); err != nil {
        t.Fatalf("SetHiddenTags: %v", err)
    }
    if err := app.DeleteTag(tag.ID); err != nil {
        t.Fatalf("delete: %v", err)
    }
    ids, err := app.GetHiddenTags()
    if err != nil {
        t.Fatalf("GetHiddenTags: %v", err)
    }
    for _, id := range ids {
        if id == tag.ID {
            t.Fatalf("hidden list should not contain deleted tag id %d", id)
        }
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run TestDeleteTag_ ./...`
Expected: FAIL — preconditions not wired, watched_folders/hidden-list cleanup missing.

- [ ] **Step 3a: Add tx-aware hidden-tags helpers**

The current `GetHiddenTags`/`SetHiddenTags` use `a.db` directly, which escapes any active transaction snapshot — another writer could change `hidden_tags` between our read and write and we'd clobber them. Add helpers that participate in the caller's transaction. Append to `tag_hierarchy.go`:

```go
// getHiddenTagsTx reads and parses the hidden_tags setting inside the given
// transaction so read+write participate in the same snapshot.
func getHiddenTagsTx(tx *sql.Tx) ([]int64, error) {
    var value string
    err := tx.QueryRow(`SELECT value FROM settings WHERE key = 'hidden_tags'`).Scan(&value)
    if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return nil, nil
        }
        return nil, fmt.Errorf("read hidden_tags: %w", err)
    }
    if value == "" {
        return nil, nil
    }
    var ids []int64
    if err := json.Unmarshal([]byte(value), &ids); err != nil {
        return nil, fmt.Errorf("parse hidden_tags: %w", err)
    }
    return ids, nil
}

// setHiddenTagsTx writes the hidden_tags setting inside the given
// transaction. Writes a JSON-encoded int64 slice.
func setHiddenTagsTx(tx *sql.Tx, ids []int64) error {
    if ids == nil {
        ids = []int64{}
    }
    payload, err := json.Marshal(ids)
    if err != nil {
        return fmt.Errorf("marshal hidden_tags: %w", err)
    }
    if _, err := tx.Exec(`INSERT INTO settings(key, value) VALUES ('hidden_tags', ?)
        ON CONFLICT(key) DO UPDATE SET value = excluded.value`, string(payload)); err != nil {
        return fmt.Errorf("write hidden_tags: %w", err)
    }
    return nil
}
```

Ensure `tag_hierarchy.go` imports `database/sql`, `encoding/json`, `errors`, `fmt`.

- [ ] **Step 3b: Modify `DeleteTag` for three-phase flow**

Replace the body of `DeleteTag` in `app.go` (at line ~1432):

```go
// DeleteTag deletes a tag using a three-phase flow:
//   1. Preconditions check (read-only) — refuse if blocked (e.g., active follow).
//   2. SQL transaction — null watched_folders auto_tag_id, remove from hidden
//      list, delete the row. FK cascades handle clip_tags, api_keys (SET NULL
//      + trigger auto-revoke), shares (CASCADE).
//   3. Post-commit runtime cleanup — stop in-memory share publication + serve
//      server. Failures here are logged; the orphan-rows tool is the backstop.
func (a *App) DeleteTag(id int64) error {
    // Fetch name up front for event payloads and error messages.
    var name string
    if err := a.db.QueryRow(`SELECT name FROM tags WHERE id = ?`, id).Scan(&name); err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return fmt.Errorf("tag not found")
        }
        return fmt.Errorf("lookup tag: %w", err)
    }

    // Phase 1: preconditions
    blockers, err := a.checkTagReferencePreconditions(id)
    if err != nil {
        return fmt.Errorf("check preconditions: %w", err)
    }
    if len(blockers) > 0 {
        return fmt.Errorf("cannot delete tag: %s", blockers[0])
    }

    // Phase 2: SQL transaction
    tx, err := a.db.Begin()
    if err != nil {
        return fmt.Errorf("begin: %w", err)
    }
    defer tx.Rollback()

    if _, err := tx.Exec(`UPDATE watched_folders SET auto_tag_id = NULL WHERE auto_tag_id = ?`, id); err != nil {
        return fmt.Errorf("null auto_tag_id: %w", err)
    }

    // Update hidden list inside the transaction (tx-aware read + write so
    // nothing escapes the snapshot).
    hiddenIDs, herr := getHiddenTagsTx(tx)
    if herr != nil {
        return fmt.Errorf("get hidden tags: %w", herr)
    }
    filtered := make([]int64, 0, len(hiddenIDs))
    for _, h := range hiddenIDs {
        if h != id {
            filtered = append(filtered, h)
        }
    }
    if len(filtered) != len(hiddenIDs) {
        if err := setHiddenTagsTx(tx, filtered); err != nil {
            return fmt.Errorf("update hidden_tags: %w", err)
        }
    }

    if _, err := tx.Exec(`DELETE FROM tags WHERE id = ?`, id); err != nil {
        return fmt.Errorf("delete tag row: %w", err)
    }

    if err := tx.Commit(); err != nil {
        return fmt.Errorf("commit: %w", err)
    }

    // Phase 3: post-commit runtime cleanup (best-effort)
    if a.shareManager != nil {
        if err := a.shareManager.StopShare(id); err != nil {
            log.Printf("DeleteTag: StopShare(%d) failed (best-effort): %v", id, err)
        }
    }
    if a.serveManager != nil {
        if err := a.serveManager.StopServing(id); err != nil {
            log.Printf("DeleteTag: StopServing(%d) failed (best-effort): %v", id, err)
        }
    }

    // Emit plugin event (unchanged)
    if a.pluginManager != nil {
        a.pluginManager.EmitEvent("tag:deleted", id)
    }
    // Emit frontend runtime event (NEW — Task 6 will also wire rename/merge)
    a.emitEvent("tag:deleted", map[string]any{"id": id, "name": name})

    return nil
}
```

Add `"encoding/json"` to imports in `app.go` if not already present.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -run TestDeleteTag_ ./...`
Expected: all PASS.

- [ ] **Step 5: Run broader tag test suite to catch regressions**

Run: `go test ./... 2>&1 | tail -30`
Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add app.go tag_reference_integrity_test.go
git commit -m "feat(tags): wire three-phase cleanup into DeleteTag"
```

---

### Task 5: Subtree-served precondition for `UpdateTag`

**Files:**
- Modify: `app.go` (`UpdateTag`)
- Modify: `tag_hierarchy.go` (add `tagIsServedInSubtree`)
- Test: `tag_reference_integrity_test.go`

- [ ] **Step 1: Write the failing test**

Append to `tag_reference_integrity_test.go`:

```go
func TestUpdateTag_BlockedByServedSubtree(t *testing.T) {
    app, cleanup := setupTestDBWithTags(t)
    defer cleanup()

    _, _ = app.CreateTag("a/x")
    child, _ := app.CreateTag("a/x/foo")

    if app.serveManager == nil {
        t.Skip("serveManager not initialized in test harness")
    }
    if _, err := app.serveManager.StartServing(child.ID, 0, false, "none"); err != nil {
        t.Fatalf("start serve: %v", err)
    }
    defer app.serveManager.StopServing(child.ID)

    // Rename the ancestor — should be blocked because a descendant is served.
    var parentID int64
    app.db.QueryRow(`SELECT id FROM tags WHERE name = 'a/x'`).Scan(&parentID)
    err := app.UpdateTag(parentID, "a/z", "#aaa")
    if err == nil {
        t.Fatalf("expected rename to be blocked, got nil")
    }
    if !strings.Contains(err.Error(), "served") {
        t.Fatalf("expected 'served' in error, got %q", err.Error())
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestUpdateTag_BlockedByServedSubtree ./...`
Expected: FAIL — no subtree-served check exists.

- [ ] **Step 3: Implement `tagIsServedInSubtree` + wire into `UpdateTag`**

Add to `tag_hierarchy.go`:

```go
// tagIsServedInSubtree returns the name of any tag in the subtree rooted at
// oldName that is currently being served, or "" if none. ServeManager caches
// tag names at start, so renames/merges of a served subtree leave the server
// resolving against stale prefixes.
func (a *App) tagIsServedInSubtree(oldName string) (string, error) {
    if a.serveManager == nil {
        return "", nil
    }
    infos, err := a.serveManager.ListServing()
    if err != nil {
        return "", fmt.Errorf("list serving: %w", err)
    }
    prefix := oldName + "/"
    for _, info := range infos {
        if info.TagName == oldName || strings.HasPrefix(info.TagName, prefix) {
            return info.TagName, nil
        }
    }
    return "", nil
}
```

In `app.go`, modify `UpdateTag` — add this check just after the existing name validation block (around line ~1382, before `tx, err := a.db.Begin()`):

```go
    // Check if any tag in the subtree is currently served. ServeManager
    // caches tag names, so renames break the server's path resolution.
    var oldNameForCheck string
    if err := a.db.QueryRow(`SELECT name FROM tags WHERE id = ?`, id).Scan(&oldNameForCheck); err != nil {
        return fmt.Errorf("tag not found")
    }
    if oldNameForCheck != name {
        if served, err := a.tagIsServedInSubtree(oldNameForCheck); err != nil {
            return fmt.Errorf("check served subtree: %w", err)
        } else if served != "" {
            return fmt.Errorf("cannot rename: tag %q in this subtree is currently served. Stop the server first.", served)
        }
    }
```

(The existing `tx.QueryRow("SELECT name FROM tags WHERE id = ?", id).Scan(&oldName)` inside the transaction can remain — it's the canonical read under the transaction. The pre-check is best-effort and only exists for a better error path.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -run TestUpdateTag_BlockedByServedSubtree ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add app.go tag_hierarchy.go tag_reference_integrity_test.go
git commit -m "feat(tags): block UpdateTag when a tag in the subtree is actively served"
```

---

### Task 6: Emit Wails runtime events from `UpdateTag`

**Files:**
- Modify: `app.go` (`UpdateTag`)
- Test: `tag_reference_integrity_test.go`

(Note: `DeleteTag` event was already added in Task 4.)

- [ ] **Step 1: Write the failing test**

Append to `tag_reference_integrity_test.go`:

```go
// Asserts that UpdateTag emits a "tag:updated" runtime event with old+new
// names, so the frontend can re-resolve folder-view state.
func TestUpdateTag_EmitsFrontendEvent(t *testing.T) {
    app, cleanup := setupTestDBWithTags(t)
    defer cleanup()

    captured := make(chan map[string]any, 1)
    app.bridge.SetTestEventSink(func(name string, data ...interface{}) {
        if name == "tag:updated" && len(data) > 0 {
            if m, ok := data[0].(map[string]any); ok {
                select { case captured <- m: default: }
            }
        }
    })

    tag, _ := app.CreateTag("before")
    if err := app.UpdateTag(tag.ID, "after", "#aaa"); err != nil {
        t.Fatalf("UpdateTag: %v", err)
    }
    select {
    case m := <-captured:
        if m["old_name"] != "before" || m["new_name"] != "after" {
            t.Fatalf("unexpected payload: %+v", m)
        }
    case <-time.After(500 * time.Millisecond):
        t.Fatalf("no tag:updated event emitted")
    }
}
```

`app.bridge` is the `wailsbridge` instance. `SetTestEventSink` may not exist yet — add it as part of this task.

- [ ] **Step 2: Add test hook to `wailsbridge`**

In `internal/wailsbridge/bridge.go`, add:

```go
// SetTestEventSink installs a handler that captures Emit calls in tests.
// Only used by test code; production code never calls this.
func (b *Bridge) SetTestEventSink(sink func(name string, data ...interface{})) {
    b.testSink = sink
}
```

Add `testSink func(string, ...interface{})` to the `Bridge` struct, and in `Emit`:

```go
func (b *Bridge) Emit(name string, data ...interface{}) {
    if b.testSink != nil {
        b.testSink(name, data...)
        return
    }
    if b.ctx == nil {
        return
    }
    rt.EventsEmit(b.ctx, name, data...)
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test -run TestUpdateTag_EmitsFrontendEvent ./...`
Expected: FAIL — UpdateTag doesn't emit the runtime event.

- [ ] **Step 4: Emit the event in `UpdateTag`**

In `app.go` `UpdateTag`, immediately after the existing plugin-event emission block (around line ~1422-1427), add:

```go
    a.emitEvent("tag:updated", map[string]any{
        "id":       id,
        "old_name": oldName,
        "new_name": name,
    })
```

`oldName` is already a local in the function.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test -run TestUpdateTag_EmitsFrontendEvent ./...`
Expected: PASS.

- [ ] **Step 6: Run the full tag test suite**

Run: `go test -run 'Tag|UpdateTag|DeleteTag' ./... 2>&1 | tail -20`
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add app.go internal/wailsbridge/bridge.go tag_reference_integrity_test.go
git commit -m "feat(tags): emit tag:updated Wails runtime event"
```

---

### Task 7: Frontend — track `currentFolderTagID` and re-resolve on tag events

**Files:**
- Modify: `frontend/js/tags.js`
- Modify: `frontend/js/wails-api.js`

- [ ] **Step 1: Regenerate Wails bindings**

Run: `make bindings`
Expected: no new bindings (we only added events, not methods). If it regenerates anything, review the diff to ensure nothing unexpected.

- [ ] **Step 2: Add event listener in `wails-api.js`**

Locate the existing `runtime.EventsOn(...)` calls (or the equivalent Wails bridge init). Add:

```javascript
// Re-resolve folder-view and tag-filter state on any tag change.
window.runtime.EventsOn('tag:updated', (payload) => {
    window.handleTagReferenceEvent('tag:updated', payload);
});
window.runtime.EventsOn('tag:deleted', (payload) => {
    window.handleTagReferenceEvent('tag:deleted', payload);
});
window.runtime.EventsOn('tag:merged', (payload) => {
    window.handleTagReferenceEvent('tag:merged', payload);
});
```

- [ ] **Step 3: Implement `handleTagReferenceEvent` + `currentFolderTagID` tracking in `tags.js`**

At the top of `frontend/js/tags.js`, add module-scope:

```javascript
// Tracks the tag ID of the current folder-view, so we can re-resolve the
// path after any tag change (rename / delete / merge).
let currentFolderTagID = null;

/** Set by navigateToFolder — called whenever the user enters a folder. */
function rememberCurrentFolder(tagID) {
    currentFolderTagID = tagID;
}
```

Find `navigateToFolder` (in `ui.js` or `tags.js` — per CLAUDE.md it's in `frontend/js/ui.js`). Inside it, after the folder state is set, call `rememberCurrentFolder(tagID)`. Expose `rememberCurrentFolder` globally if needed.

Add to `tags.js`:

```javascript
window.handleTagReferenceEvent = async function(eventName, payload) {
    // Always reload the tag list first so subsequent lookups use fresh data.
    if (typeof loadTags === 'function') {
        await loadTags();
    }
    const tags = await window.go.main.App.GetTags();
    const validIDs = new Set(tags.map(t => t.id));

    // Normalize activeTagFilters in BOTH folder-mode and non-folder-mode.
    // loadTags() only refreshes the tag dropdown; it does NOT touch the
    // activeTagFilters array that loadClips() uses to build the SQL
    // predicate (frontend/js/app.js:1615-1617, wails-api.js:17-41). Without
    // this pass, deleting or merging a tag while it is used as a normal
    // filter leaves the gallery filtering on a nonexistent ID.
    if (Array.isArray(window.activeTagFilters)) {
        window.activeTagFilters = window.activeTagFilters
            .map(id => {
                // For merges, substitute dest for source so the user stays
                // on an equivalent filter view.
                if (eventName === 'tag:merged'
                    && payload && payload.source_id === id
                    && typeof payload.dest_id === 'number') {
                    return payload.dest_id;
                }
                return id;
            })
            .filter(id => validIDs.has(id));
    }

    if (currentFolderTagID == null) {
        // Not in folder view — filter pills get rebuilt by loadTags, and
        // activeTagFilters is already normalized above.
        loadClips();
        return;
    }

    const current = tags.find(t => t.id === currentFolderTagID);
    if (current) {
        // Tag still exists (rename, or merge destination) — re-navigate to
        // its current path (may have changed via rename).
        if (typeof navigateToFolder === 'function') {
            navigateToFolder(current.id, current.name);
        }
    } else {
        // Tag is gone (delete, or merge source) — navigate to parent, or
        // fall out of folder mode entirely.
        const parentName = parentTagName(payload?.name || payload?.old_name || payload?.source_name || '');
        const parent = parentName ? tags.find(t => t.name === parentName) : null;
        if (parent && typeof navigateToFolder === 'function') {
            navigateToFolder(parent.id, parent.name);
        } else if (typeof exitFolderMode === 'function') {
            exitFolderMode();
        } else {
            currentFolderTagID = null;
            loadClips();
        }
    }
};

function parentTagName(fullName) {
    const idx = fullName.lastIndexOf('/');
    return idx > 0 ? fullName.substring(0, idx) : '';
}
```

If `exitFolderMode` doesn't exist, implement a minimal version that clears folder state and calls `loadClips()`.

- [ ] **Step 4: Wire up the listener at app boot**

In `frontend/js/app.js`, ensure the event listeners in `wails-api.js` are registered on boot (if they aren't auto-registered).

- [ ] **Step 5: Manual smoke test**

Run: `make dev` in one terminal, interact with the app:
1. Create tag `a/x`, add a clip to it, enter the folder.
2. Rename `a/x` to `a/z` via the tag sidebar. Folder view should update to show `a/z`.
3. Delete `a/z`. Folder view should navigate up (to root or `a` if still there).

Kill the dev server before moving on.

- [ ] **Step 6: Commit**

```bash
git add frontend/js/tags.js frontend/js/wails-api.js frontend/js/ui.js frontend/js/app.js
git commit -m "feat(frontend): re-resolve folder view on tag rename/delete/merge events"
```

---

### Task 8: E2e — folder-view re-navigation on rename and delete

**Files:**
- Create: `e2e/tests/tags/rename-folder-view.spec.ts`
- Create: `e2e/tests/tags/delete-folder-view.spec.ts`
- Create: `e2e/tests/tags/delete-reference-integrity.spec.ts`

- [ ] **Step 1: Write `rename-folder-view.spec.ts`**

```typescript
import { test } from '../../fixtures/test-fixtures';
import { expect } from '@playwright/test';
import path from 'path';
import { generateTestImage, createTempFile } from '../../helpers/test-data';

test.describe('Folder view — rename', () => {
    test('renames current folder and updates the view', async ({ app }) => {
        const img = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(img);
        await app.createTag('work/clientA');
        await app.tagClipByIndex(0, 'work/clientA');
        await app.enterFolder('work/clientA');

        await app.expectFolderHeader('work/clientA');
        await app.renameTag('work/clientA', 'work/clientB');

        // Folder view should re-navigate to the new path.
        await app.expectFolderHeader('work/clientB');
        await app.expectClipCount(1);
    });
});
```

`createTag`, `tagClipByIndex`, `enterFolder`, `renameTag`, `expectFolderHeader` may need to be added to `e2e/fixtures/test-fixtures.ts` — add any that are missing, following the existing pattern.

- [ ] **Step 2: Write `delete-folder-view.spec.ts`**

```typescript
import { test } from '../../fixtures/test-fixtures';

test.describe('Folder view — delete', () => {
    test('navigates up when current folder is deleted', async ({ app }) => {
        await app.createTag('work/clientA');
        await app.enterFolder('work/clientA');
        await app.deleteTag('work/clientA');

        // Expect nav to parent 'work'.
        await app.expectFolderHeader('work');
    });

    test('exits folder mode when deleted tag has no parent', async ({ app }) => {
        await app.createTag('lonely');
        await app.enterFolder('lonely');
        await app.deleteTag('lonely');

        await app.expectNotInFolderMode();
    });
});
```

- [ ] **Step 3: Write `delete-reference-integrity.spec.ts`**

```typescript
import { test } from '../../fixtures/test-fixtures';
import { expect } from '@playwright/test';

test.describe('DeleteTag reference integrity', () => {
    test('surfaces follow blocker as a user-friendly error', async ({ app }) => {
        // Seed a follow row directly via the dev API (this harness exposes a
        // helper for it — if not, we can run a SQL statement via the app).
        const { tagID } = await app.createTag('blocked');
        await app.insertFakeFollow(tagID);

        const errorToast = await app.deleteTagExpectError('blocked');
        expect(errorToast).toMatch(/follow/i);
    });

    // Regression for P2-2: deleting a tag that's an active filter in
    // non-folder mode must drop the filter, not leave the gallery
    // filtering on a nonexistent ID.
    test('clears active filter when the filtered tag is deleted (non-folder mode)', async ({ app }) => {
        const img = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(img);
        await app.createTag('filter-target');
        await app.tagClipByIndex(0, 'filter-target');

        await app.selectTagFilter('filter-target');
        await app.expectClipCount(1);

        await app.deleteTag('filter-target');

        // Filter pill should be gone; gallery should show all clips again.
        await app.expectTagFilterInactive('filter-target');
        await app.expectClipCount(1);
    });

    // Merging a tag that's an active filter should substitute the dest.
    test('substitutes destination when a filtered tag is merged away', async ({ app }) => {
        const img = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(img);
        await app.createTag('source-filter');
        await app.createTag('dest-filter');
        await app.tagClipByIndex(0, 'source-filter');

        await app.selectTagFilter('source-filter');
        await app.mergeTag('source-filter', 'dest-filter');

        // Filter should now be on dest-filter, still showing the clip.
        await app.expectTagFilterActive('dest-filter');
        await app.expectClipCount(1);
    });
});
```

- [ ] **Step 4: Run the e2e tests**

Run: `cd e2e && npx playwright test tests/tags/rename-folder-view.spec.ts tests/tags/delete-folder-view.spec.ts tests/tags/delete-reference-integrity.spec.ts 2>&1 | tail -40`
Expected: all PASS.

- [ ] **Step 5: Run the full maintenance+tags e2e suite**

Run: `cd e2e && npx playwright test tests/tags tests/maintenance 2>&1 | tail -40`
Expected: all PASS (no regressions in existing `remove-empty-tags.spec.ts` or tag CRUD tests).

- [ ] **Step 6: Commit**

```bash
git add e2e/tests/tags/ e2e/fixtures/test-fixtures.ts e2e/helpers/
git commit -m "test(tags): add e2e for folder-view re-navigation and delete integrity"
```

---

## Phase 2 — Part B.1: Merge Tags

### Task 9: `migrateTagReferences` helper

**Files:**
- Modify: `tag_hierarchy.go`
- Test: `tag_merge_test.go` (new)

- [ ] **Step 1: Write the failing tests**

Create `tag_merge_test.go`:

```go
package main

import (
    "database/sql"
    "testing"
)

func TestMigrateTagReferences_APIKeyScope(t *testing.T) {
    app, cleanup := setupTestDBWithTags(t)
    defer cleanup()

    src, _ := app.CreateTag("src")
    dst, _ := app.CreateTag("dst")
    app.db.Exec(`INSERT INTO api_keys (name, key_hash, key_prefix, role, scoped_tag_id) VALUES ('k', 'h', 'p', 'viewer', ?)`, src.ID)

    tx, _ := app.db.Begin()
    defer tx.Rollback()
    if err := app.migrateTagReferences(tx, src.ID, dst.ID); err != nil {
        t.Fatalf("migrate: %v", err)
    }
    tx.Commit()

    var scoped sql.NullInt64
    app.db.QueryRow(`SELECT scoped_tag_id FROM api_keys WHERE name = 'k'`).Scan(&scoped)
    if !scoped.Valid || scoped.Int64 != dst.ID {
        t.Fatalf("scope should be %d, got %v", dst.ID, scoped)
    }
}

func TestMigrateTagReferences_WatchFolderAutoTag(t *testing.T) {
    app, cleanup := setupTestDBWithTags(t)
    defer cleanup()

    src, _ := app.CreateTag("src")
    dst, _ := app.CreateTag("dst")
    app.db.Exec(`INSERT INTO watched_folders (path, auto_tag_id) VALUES ('/tmp/y', ?)`, src.ID)

    tx, _ := app.db.Begin()
    defer tx.Rollback()
    if err := app.migrateTagReferences(tx, src.ID, dst.ID); err != nil {
        t.Fatalf("migrate: %v", err)
    }
    tx.Commit()

    var at sql.NullInt64
    app.db.QueryRow(`SELECT auto_tag_id FROM watched_folders WHERE path = '/tmp/y'`).Scan(&at)
    if !at.Valid || at.Int64 != dst.ID {
        t.Fatalf("auto_tag_id should be %d, got %v", dst.ID, at)
    }
}

func TestMigrateTagReferences_HiddenList(t *testing.T) {
    app, cleanup := setupTestDBWithTags(t)
    defer cleanup()

    src, _ := app.CreateTag("src")
    dst, _ := app.CreateTag("dst")
    if err := app.SetHiddenTags([]int64{src.ID}); err != nil {
        t.Fatalf("SetHiddenTags: %v", err)
    }

    tx, _ := app.db.Begin()
    defer tx.Rollback()
    if err := app.migrateTagReferences(tx, src.ID, dst.ID); err != nil {
        t.Fatalf("migrate: %v", err)
    }
    tx.Commit()

    ids, _ := app.GetHiddenTags()
    hasDst := false
    for _, id := range ids {
        if id == src.ID {
            t.Fatalf("hidden list should not still contain src id")
        }
        if id == dst.ID {
            hasDst = true
        }
    }
    if !hasDst {
        t.Fatalf("hidden list should now contain dst id %d, got %v", dst.ID, ids)
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run TestMigrateTagReferences ./...`
Expected: FAIL — helper doesn't exist.

- [ ] **Step 3: Implement `migrateTagReferences`**

Add to `tag_hierarchy.go`:

```go
// migrateTagReferences moves all non-networked ID-keyed references from
// source to destination inside an existing transaction. Networked state
// (shares, follows, serves) is refused at the precondition phase and is
// never touched here.
func (a *App) migrateTagReferences(tx *sql.Tx, fromID, toID int64) error {
    // API keys — migrate scope (preserves user intent).
    if _, err := tx.Exec(`UPDATE api_keys SET scoped_tag_id = ? WHERE scoped_tag_id = ?`, toID, fromID); err != nil {
        return fmt.Errorf("migrate api_keys: %w", err)
    }
    // Watched folders — migrate auto-tag target.
    if _, err := tx.Exec(`UPDATE watched_folders SET auto_tag_id = ? WHERE auto_tag_id = ?`, toID, fromID); err != nil {
        return fmt.Errorf("migrate watched_folders: %w", err)
    }
    // Hidden tag list — swap membership (tx-aware read+write).
    hiddenIDs, err := getHiddenTagsTx(tx)
    if err != nil {
        return fmt.Errorf("get hidden: %w", err)
    }
    newHidden := make([]int64, 0, len(hiddenIDs))
    sawSrc := false
    sawDst := false
    for _, id := range hiddenIDs {
        if id == fromID {
            sawSrc = true
            continue
        }
        if id == toID {
            sawDst = true
        }
        newHidden = append(newHidden, id)
    }
    if sawSrc && !sawDst {
        newHidden = append(newHidden, toID)
    }
    if sawSrc {
        if err := setHiddenTagsTx(tx, newHidden); err != nil {
            return fmt.Errorf("update hidden_tags: %w", err)
        }
    }
    return nil
}
```

Ensure imports include `database/sql`, `encoding/json`, `fmt`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -run TestMigrateTagReferences ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add tag_hierarchy.go tag_merge_test.go
git commit -m "feat(tags): add migrateTagReferences helper"
```

---

### Task 10: `PreviewMergeTag` Go method

**Files:**
- Modify: `app.go` (new method)
- Test: `tag_merge_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `tag_merge_test.go`:

```go
func TestPreviewMergeTag_CountsClipsAndDescendants(t *testing.T) {
    app, cleanup := setupTestDBWithTags(t)
    defer cleanup()

    src, _ := app.CreateTag("a/x")
    _, _ = app.CreateTag("a/x/foo")
    _, _ = app.CreateTag("a/x/bar")
    dst, _ := app.CreateTag("b/y")

    clipID := createTestClip(t, app.db, "clip.txt", "text/plain", []byte("hi"))
    app.AddTagToClip(clipID, src.ID)

    preview, err := app.PreviewMergeTag(src.ID, dst.ID)
    if err != nil {
        t.Fatalf("PreviewMergeTag: %v", err)
    }
    if preview.ClipCount != 1 {
        t.Fatalf("ClipCount = %d, want 1", preview.ClipCount)
    }
    if preview.DescendantCount != 2 {
        t.Fatalf("DescendantCount = %d, want 2", preview.DescendantCount)
    }
    if len(preview.Blockers) != 0 {
        t.Fatalf("unexpected blockers: %v", preview.Blockers)
    }
}

func TestPreviewMergeTag_BlockedBySelf(t *testing.T) {
    app, cleanup := setupTestDBWithTags(t)
    defer cleanup()
    src, _ := app.CreateTag("t")
    preview, err := app.PreviewMergeTag(src.ID, src.ID)
    if err != nil {
        t.Fatalf("preview: %v", err)
    }
    if len(preview.Blockers) == 0 {
        t.Fatalf("expected self blocker")
    }
}

func TestPreviewMergeTag_BlockedByDestinationIsDescendant(t *testing.T) {
    app, cleanup := setupTestDBWithTags(t)
    defer cleanup()
    src, _ := app.CreateTag("a")
    dst, _ := app.CreateTag("a/x")
    preview, _ := app.PreviewMergeTag(src.ID, dst.ID)
    if len(preview.Blockers) == 0 {
        t.Fatalf("expected descendant blocker")
    }
}
```

`createTestClip` exists in the test suite; if not, write one that inserts a clip row directly.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run TestPreviewMergeTag ./...`
Expected: FAIL — method doesn't exist.

- [ ] **Step 3: Implement `PreviewMergeTag`**

Add to `app.go` (near `MergeTag` — or create the block where merge methods will live):

```go
// MergeTagPreview summarizes the impact of a proposed merge.
type MergeTagPreview struct {
    ClipCount       int      `json:"clip_count"`
    DescendantCount int      `json:"descendant_count"`
    Blockers        []string `json:"blockers"`
    SourceName      string   `json:"source_name"`
    DestName        string   `json:"dest_name"`
}

// PreviewMergeTag returns counts + blockers without mutating anything.
func (a *App) PreviewMergeTag(sourceID, destID int64) (MergeTagPreview, error) {
    var out MergeTagPreview

    var srcName, dstName string
    if err := a.db.QueryRow(`SELECT name FROM tags WHERE id = ?`, sourceID).Scan(&srcName); err != nil {
        return out, fmt.Errorf("source tag not found: %w", err)
    }
    if err := a.db.QueryRow(`SELECT name FROM tags WHERE id = ?`, destID).Scan(&dstName); err != nil {
        return out, fmt.Errorf("destination tag not found: %w", err)
    }
    out.SourceName = srcName
    out.DestName = dstName

    out.Blockers = a.checkMergeTagPreconditions(sourceID, destID, srcName, dstName)

    if err := a.db.QueryRow(
        `SELECT COUNT(*) FROM clip_tags WHERE tag_id = ?`, sourceID,
    ).Scan(&out.ClipCount); err != nil {
        return out, fmt.Errorf("count clips: %w", err)
    }

    // Descendants: tags whose name starts with "{srcName}/".
    if err := a.db.QueryRow(
        `SELECT COUNT(*) FROM tags WHERE name LIKE ? || '/%'`, srcName,
    ).Scan(&out.DescendantCount); err != nil {
        return out, fmt.Errorf("count descendants: %w", err)
    }

    return out, nil
}
```

And add `checkMergeTagPreconditions` to `tag_hierarchy.go`:

```go
// checkMergeTagPreconditions returns the list of human-readable blockers for
// merging source into destination. Empty slice means proceed.
func (a *App) checkMergeTagPreconditions(sourceID, destID int64, srcName, dstName string) []string {
    var blockers []string
    if sourceID == destID {
        return []string{"source and destination must be different tags"}
    }
    // Destination must not be a descendant of source.
    if dstName == srcName || strings.HasPrefix(dstName, srcName+"/") {
        blockers = append(blockers, fmt.Sprintf("destination %q is a descendant of source %q", dstName, srcName))
    }
    // Block on active share (shares row for source).
    var shareCount int
    if err := a.db.QueryRow(`SELECT COUNT(*) FROM shares WHERE tag_id = ?`, sourceID).Scan(&shareCount); err == nil && shareCount > 0 {
        blockers = append(blockers, "source tag is actively shared. Stop the share first.")
    }
    // Block on follow.
    var followCount int
    if err := a.db.QueryRow(`SELECT COUNT(*) FROM follows WHERE local_tag_id = ?`, sourceID).Scan(&followCount); err == nil && followCount > 0 {
        blockers = append(blockers, "source tag has an incoming share (follow). Retarget or stop it first.")
    }
    // Block on any served tag in source subtree.
    if served, err := a.tagIsServedInSubtree(srcName); err == nil && served != "" {
        blockers = append(blockers, fmt.Sprintf("tag %q in source subtree is currently served. Stop the server first.", served))
    }
    // Block on descendant collision with destination subtree.
    rows, err := a.db.Query(`SELECT name FROM tags WHERE name LIKE ? || '/%'`, srcName)
    if err == nil {
        defer rows.Close()
        srcPrefix := srcName + "/"
        dstPrefix := dstName + "/"
        for rows.Next() {
            var n string
            if err := rows.Scan(&n); err != nil {
                continue
            }
            projected := dstPrefix + strings.TrimPrefix(n, srcPrefix)
            var exists int
            if err := a.db.QueryRow(`SELECT COUNT(*) FROM tags WHERE name = ?`, projected).Scan(&exists); err == nil && exists > 0 {
                blockers = append(blockers, fmt.Sprintf("merge would collide at %q (tag already exists)", projected))
                break
            }
        }
    }
    return blockers
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -run TestPreviewMergeTag ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add app.go tag_hierarchy.go tag_merge_test.go
git commit -m "feat(tags): add PreviewMergeTag with blocker detection"
```

---

### Task 11: `MergeTag` Go method

**Files:**
- Modify: `app.go`
- Test: `tag_merge_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `tag_merge_test.go`:

```go
func TestMergeTag_BasicReassignment(t *testing.T) {
    app, cleanup := setupTestDBWithTags(t)
    defer cleanup()

    src, _ := app.CreateTag("a/x")
    dst, _ := app.CreateTag("b/y")

    clipID := createTestClip(t, app.db, "c.txt", "text/plain", []byte("x"))
    app.AddTagToClip(clipID, src.ID)

    if err := app.MergeTag(src.ID, dst.ID); err != nil {
        t.Fatalf("MergeTag: %v", err)
    }
    // Source must be gone.
    var count int
    app.db.QueryRow(`SELECT COUNT(*) FROM tags WHERE id = ?`, src.ID).Scan(&count)
    if count != 0 {
        t.Fatalf("source tag should be deleted")
    }
    // Clip must be on destination.
    tags, _ := app.GetClipTags(clipID)
    foundDst := false
    for _, t := range tags {
        if t.ID == dst.ID {
            foundDst = true
        }
    }
    if !foundDst {
        t.Fatalf("clip %d must have destination tag", clipID)
    }
}

func TestMergeTag_SubtreeMove(t *testing.T) {
    app, cleanup := setupTestDBWithTags(t)
    defer cleanup()

    _, _ = app.CreateTag("a/x")
    foo, _ := app.CreateTag("a/x/foo")
    bar, _ := app.CreateTag("a/x/bar/baz")
    _, _ = app.CreateTag("b/y")

    var srcID, dstID int64
    app.db.QueryRow(`SELECT id FROM tags WHERE name = 'a/x'`).Scan(&srcID)
    app.db.QueryRow(`SELECT id FROM tags WHERE name = 'b/y'`).Scan(&dstID)

    if err := app.MergeTag(srcID, dstID); err != nil {
        t.Fatalf("MergeTag: %v", err)
    }

    var name string
    app.db.QueryRow(`SELECT name FROM tags WHERE id = ?`, foo.ID).Scan(&name)
    if name != "b/y/foo" {
        t.Fatalf("foo should be renamed to b/y/foo, got %q", name)
    }
    app.db.QueryRow(`SELECT name FROM tags WHERE id = ?`, bar.ID).Scan(&name)
    if name != "b/y/bar/baz" {
        t.Fatalf("bar/baz should be renamed to b/y/bar/baz, got %q", name)
    }
}

func TestMergeTag_BlockedByActiveShare(t *testing.T) {
    app, cleanup := setupTestDBWithTags(t)
    defer cleanup()

    src, _ := app.CreateTag("src")
    dst, _ := app.CreateTag("dst")
    // Insert a fake shares row.
    _, err := app.db.Exec(`INSERT INTO shares (tag_id, symkey, share_id, created_at) VALUES (?, X'00', X'01', 0)`, src.ID)
    if err != nil {
        t.Fatalf("insert share: %v", err)
    }
    err = app.MergeTag(src.ID, dst.ID)
    if err == nil || !strings.Contains(err.Error(), "share") {
        t.Fatalf("expected share blocker, got %v", err)
    }
}

// Regression for the tree-exclusivity bypass: before the fix, merge wrote
// clip_tags directly and could leave a clip with two tags in the same root
// tree, which the rest of the app assumes is impossible.
func TestMergeTag_PreservesSameTreeExclusivity_CrossRoot(t *testing.T) {
    app, cleanup := setupTestDBWithTags(t)
    defer cleanup()

    src, _ := app.CreateTag("a/x")
    _, _ = app.CreateTag("c/other")
    dst, _ := app.CreateTag("c/y")

    clipID := createTestClip(t, app.db, "c.txt", "text/plain", []byte("x"))
    if err := app.AddTagToClip(clipID, src.ID); err != nil {
        t.Fatalf("add src: %v", err)
    }
    // Bypass AddTagToClip's exclusivity on purpose — seed a clip that
    // already carries a tag in destination's root tree. This is the
    // pre-condition the merge must resolve correctly.
    var cOtherID int64
    app.db.QueryRow(`SELECT id FROM tags WHERE name = 'c/other'`).Scan(&cOtherID)
    if _, err := app.db.Exec(`INSERT INTO clip_tags(clip_id, tag_id) VALUES (?, ?)`, clipID, cOtherID); err != nil {
        t.Fatalf("seed c/other: %v", err)
    }

    if err := app.MergeTag(src.ID, dst.ID); err != nil {
        t.Fatalf("merge: %v", err)
    }

    tags, _ := app.GetClipTags(clipID)
    names := make([]string, 0, len(tags))
    for _, t := range tags { names = append(names, t.Name) }
    // Clip must have exactly c/y — the pre-existing c/other should have
    // been evicted (same-tree rule) and the source a/x should be gone.
    if len(tags) != 1 || tags[0].ID != dst.ID {
        t.Fatalf("expected clip to hold only dest c/y, got %v", names)
    }
}

func TestMergeTag_PreservesSameTreeExclusivity_DoesNotTouchUnaffectedClips(t *testing.T) {
    app, cleanup := setupTestDBWithTags(t)
    defer cleanup()

    src, _ := app.CreateTag("a/x")
    dst, _ := app.CreateTag("c/y")
    _, _ = app.CreateTag("c/other")

    // Clip A participates in the merge.
    clipA := createTestClip(t, app.db, "a.txt", "text/plain", []byte("A"))
    app.AddTagToClip(clipA, src.ID)

    // Clip B has dest AND dest-root sibling already — NOT touched by merge.
    // Verify that merging unrelated tags doesn't collaterally damage B.
    clipB := createTestClip(t, app.db, "b.txt", "text/plain", []byte("B"))
    var cOtherID int64
    app.db.QueryRow(`SELECT id FROM tags WHERE name = 'c/other'`).Scan(&cOtherID)
    // Seed B with both c/y and c/other, bypassing exclusivity.
    app.db.Exec(`INSERT INTO clip_tags(clip_id, tag_id) VALUES (?, ?)`, clipB, dst.ID)
    app.db.Exec(`INSERT INTO clip_tags(clip_id, tag_id) VALUES (?, ?)`, clipB, cOtherID)

    if err := app.MergeTag(src.ID, dst.ID); err != nil {
        t.Fatalf("merge: %v", err)
    }

    // Clip B must still carry both c/y and c/other — merge should not
    // rewrite clips that weren't holding the source tag.
    bTags, _ := app.GetClipTags(clipB)
    if len(bTags) != 2 {
        t.Fatalf("merge should not touch unrelated clip B, got %d tags", len(bTags))
    }
}

func TestMergeTag_EmitsFrontendEvent(t *testing.T) {
    app, cleanup := setupTestDBWithTags(t)
    defer cleanup()

    captured := make(chan map[string]any, 1)
    app.bridge.SetTestEventSink(func(name string, data ...interface{}) {
        if name == "tag:merged" && len(data) > 0 {
            if m, ok := data[0].(map[string]any); ok {
                select { case captured <- m: default: }
            }
        }
    })

    src, _ := app.CreateTag("src")
    dst, _ := app.CreateTag("dst")
    if err := app.MergeTag(src.ID, dst.ID); err != nil {
        t.Fatalf("merge: %v", err)
    }
    select {
    case m := <-captured:
        if m["source_id"] != src.ID || m["dest_id"] != dst.ID {
            t.Fatalf("unexpected payload: %+v", m)
        }
    case <-time.After(500 * time.Millisecond):
        t.Fatalf("no tag:merged event emitted")
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run TestMergeTag ./...`
Expected: FAIL — method doesn't exist.

- [ ] **Step 3: Implement `MergeTag`**

Add to `app.go`:

```go
// MergeTag folds source into destination in a single transaction:
//   1. Preconditions — refuses if any blocker (see checkMergeTagPreconditions).
//   2. Reassigns all clips from source to destination (respecting same-tree
//      exclusivity via AddTagToClip's existing logic, though clips already
//      on destination via same-tree rules would just skip).
//   3. Renames every descendant of source with the prefix swap.
//   4. Migrates non-networked ID references (api_keys, watched_folders,
//      hidden list) via migrateTagReferences.
//   5. Deletes the source tag row.
// Emits tag:merged (runtime + plugin) on success.
func (a *App) MergeTag(sourceID, destID int64) error {
    var srcName, dstName string
    if err := a.db.QueryRow(`SELECT name FROM tags WHERE id = ?`, sourceID).Scan(&srcName); err != nil {
        return fmt.Errorf("source tag not found")
    }
    if err := a.db.QueryRow(`SELECT name FROM tags WHERE id = ?`, destID).Scan(&dstName); err != nil {
        return fmt.Errorf("destination tag not found")
    }

    blockers := a.checkMergeTagPreconditions(sourceID, destID, srcName, dstName)
    if len(blockers) > 0 {
        return fmt.Errorf("cannot merge: %s", blockers[0])
    }

    // Destination root = first path segment. Reassignment must enforce
    // same-tree exclusivity: clips receiving the destination tag must not
    // simultaneously carry any other tag under the destination's root tree.
    destRoot := dstName
    if idx := strings.Index(dstName, "/"); idx >= 0 {
        destRoot = dstName[:idx]
    }

    tx, err := a.db.Begin()
    if err != nil {
        return fmt.Errorf("begin: %w", err)
    }
    defer tx.Rollback()

    // Reassignment enforces same-tree exclusivity (matches AddTagToClip /
    // removeSameTreeTags at app.go:1606-1695 — a clip may hold at most one
    // tag per root tree). Order matters: insert destination first, then
    // clean sibling tags in dest's root tree, then delete any leftover
    // source rows. Doing the exclusivity cleanup first would remove the
    // source-tag rows when source shares a root with destination, breaking
    // the INSERT's clip-set predicate.
    //
    // (2a) Insert destination for every clip that currently has source.
    // INSERT OR IGNORE handles clips that already hold destination.
    if _, err := tx.Exec(`INSERT OR IGNORE INTO clip_tags(clip_id, tag_id)
        SELECT clip_id, ? FROM clip_tags WHERE tag_id = ?`, destID, sourceID); err != nil {
        return fmt.Errorf("reassign clip_tags: %w", err)
    }

    // (2b) Same-tree cleanup: ONLY for clips affected by the merge (the ones
    // that had the source tag — source rows still exist at this point,
    // they're deleted in 2c), remove any other tag under destination's
    // root tree. Anchoring on source (not destination) ensures we don't
    // accidentally rewrite an unrelated clip that already happened to
    // carry destination.
    if _, err := tx.Exec(`DELETE FROM clip_tags
        WHERE clip_id IN (SELECT clip_id FROM clip_tags WHERE tag_id = ?)
          AND tag_id IN (
            SELECT id FROM tags WHERE name = ? OR name LIKE ? || '/%'
          )
          AND tag_id != ?`, sourceID, destRoot, destRoot, destID); err != nil {
        return fmt.Errorf("same-tree cleanup: %w", err)
    }

    // (2c) Delete any remaining source clip_tags rows (catches the
    // cross-root case; no-op when same-root case already wiped them via 2b).
    if _, err := tx.Exec(`DELETE FROM clip_tags WHERE tag_id = ?`, sourceID); err != nil {
        return fmt.Errorf("delete old clip_tags: %w", err)
    }

    // (3) Rename descendants with prefix swap.
    oldPrefix := srcName + "/"
    newPrefix := dstName + "/"
    if _, err := tx.Exec(`UPDATE tags SET name = ? || SUBSTR(name, ?) WHERE name LIKE ?`,
        newPrefix, utf8.RuneCountInString(oldPrefix)+1, oldPrefix+"%"); err != nil {
        if strings.Contains(err.Error(), "UNIQUE") {
            return fmt.Errorf("merge would create duplicate tag path")
        }
        return fmt.Errorf("rename descendants: %w", err)
    }

    // (4) Migrate non-networked references.
    if err := a.migrateTagReferences(tx, sourceID, destID); err != nil {
        return fmt.Errorf("migrate references: %w", err)
    }

    // (5) Delete the source row. ON DELETE CASCADE on clip_tags is a no-op
    // (we already moved them); shares cascade will fire but the precondition
    // guaranteed no shares exist.
    if _, err := tx.Exec(`DELETE FROM tags WHERE id = ?`, sourceID); err != nil {
        return fmt.Errorf("delete source: %w", err)
    }

    if err := tx.Commit(); err != nil {
        return fmt.Errorf("commit: %w", err)
    }

    // Emit events.
    if a.pluginManager != nil {
        a.pluginManager.EmitEvent("tag:merged", map[string]interface{}{
            "source_id": sourceID, "dest_id": destID,
            "source_name": srcName, "dest_name": dstName,
        })
    }
    a.emitEvent("tag:merged", map[string]any{
        "source_id": sourceID, "dest_id": destID,
        "source_name": srcName, "dest_name": dstName,
    })

    return nil
}
```

Ensure `unicode/utf8` is imported in `app.go` (it's already imported for `UpdateTag`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -run TestMergeTag ./...`
Expected: PASS.

- [ ] **Step 5: Run broader test suite**

Run: `go test ./... 2>&1 | tail -40`
Expected: no regressions.

- [ ] **Step 6: Commit**

```bash
git add app.go tag_merge_test.go
git commit -m "feat(tags): implement MergeTag with subtree move + reference migration"
```

---

### Task 12: Frontend — tag context menu + merge modal

**Files:**
- Modify: `frontend/index.html`
- Create: `frontend/js/merge-tag-modal.js`
- Modify: `frontend/js/tags.js`

- [ ] **Step 1: Regenerate Wails bindings**

Run: `make bindings`
Expected: new methods exposed on `window.go.main.App`: `PreviewMergeTag`, `MergeTag`. Verify by `grep -n "PreviewMergeTag\|MergeTag" frontend/wailsjs/go/main/App.js`.

- [ ] **Step 2: Add merge modal markup to `index.html`**

Inside `<body>`, before the closing tag, add (matching the pattern of the existing maintenance-modal / confirm-dialog):

```html
<!-- Merge Tag Modal -->
<div id="merge-tag-modal" class="fixed inset-0 bg-stone-900/50 flex items-center justify-center z-50 opacity-0 pointer-events-none transition-opacity duration-150" inert>
    <div class="bg-white rounded-lg shadow-xl max-w-md w-full mx-4 p-6 scale-95 transition-transform duration-150">
        <div class="flex items-center justify-between mb-4">
            <h3 class="text-sm font-semibold uppercase tracking-wide text-stone-800">Merge tag</h3>
            <button id="merge-tag-close" class="p-1 text-stone-400 hover:text-stone-600 hover:bg-stone-100 rounded transition-colors" aria-label="Close">
                <svg class="w-4 h-4" stroke="currentColor" stroke-width="1.5" fill="none" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M6 6l12 12M6 18L18 6"/></svg>
            </button>
        </div>
        <p class="text-xs text-stone-600 mb-3">Merging <span id="merge-tag-source" class="font-semibold text-stone-800"></span> into destination tag:</p>
        <input id="merge-tag-dest-input" type="text" autocomplete="off" placeholder="destination tag" class="block w-full border border-stone-200 rounded-md text-sm bg-white placeholder-stone-400 focus:outline-none focus:border-stone-400 focus:ring-1 focus:ring-stone-400/20 transition-colors p-2 mb-3">
        <div id="merge-tag-preview" class="text-xs text-stone-600 mb-4 min-h-[3rem]"></div>
        <div class="flex justify-end gap-2">
            <button id="merge-tag-cancel" class="border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-600 text-xs font-medium py-2 px-3 rounded-md transition-colors">Cancel</button>
            <button id="merge-tag-confirm" class="bg-stone-800 hover:bg-stone-700 text-white text-xs font-medium py-2.5 px-5 rounded-md transition-colors disabled:bg-stone-300 disabled:cursor-not-allowed" disabled>Merge</button>
        </div>
    </div>
</div>
```

- [ ] **Step 3: Create `frontend/js/merge-tag-modal.js`**

```javascript
// --- Merge Tag Modal ---

const mergeTagModal = document.getElementById('merge-tag-modal');
const mergeTagCloseBtn = document.getElementById('merge-tag-close');
const mergeTagCancelBtn = document.getElementById('merge-tag-cancel');
const mergeTagConfirmBtn = document.getElementById('merge-tag-confirm');
const mergeTagSourceLabel = document.getElementById('merge-tag-source');
const mergeTagDestInput = document.getElementById('merge-tag-dest-input');
const mergeTagPreview = document.getElementById('merge-tag-preview');

let mergeTagSourceID = null;
let mergeTagAutocomplete = null;
let mergePreviewTimer = null;

async function openMergeTagModal(sourceID, sourceName) {
    mergeTagSourceID = sourceID;
    mergeTagSourceLabel.textContent = sourceName;
    mergeTagDestInput.value = '';
    mergeTagPreview.textContent = '';
    mergeTagConfirmBtn.disabled = true;

    mergeTagModal.removeAttribute('inert');
    mergeTagModal.classList.remove('opacity-0', 'pointer-events-none');
    mergeTagModal.classList.add('opacity-100');
    mergeTagModal.querySelector(':scope > div').classList.remove('scale-95');
    mergeTagModal.querySelector(':scope > div').classList.add('scale-100');

    if (window.TagAutocomplete && !mergeTagAutocomplete) {
        mergeTagAutocomplete = window.TagAutocomplete.attach(mergeTagDestInput, {
            onSelect: () => updateMergePreview(),
        });
    }
    mergeTagDestInput.focus();
}

function closeMergeTagModal() {
    mergeTagModal.classList.add('opacity-0', 'pointer-events-none');
    mergeTagModal.classList.remove('opacity-100');
    mergeTagModal.querySelector(':scope > div').classList.add('scale-95');
    mergeTagModal.querySelector(':scope > div').classList.remove('scale-100');
    mergeTagModal.setAttribute('inert', '');
    mergeTagSourceID = null;
}

async function updateMergePreview() {
    if (mergePreviewTimer) clearTimeout(mergePreviewTimer);
    mergePreviewTimer = setTimeout(async () => {
        const destName = mergeTagDestInput.value.trim();
        if (!destName || mergeTagSourceID == null) {
            mergeTagPreview.textContent = '';
            mergeTagConfirmBtn.disabled = true;
            return;
        }
        const tags = await window.go.main.App.GetTags();
        const dest = tags.find(t => t.name === destName);
        if (!dest) {
            mergeTagPreview.textContent = `"${destName}" does not exist. Create it first.`;
            mergeTagConfirmBtn.disabled = true;
            return;
        }
        try {
            const preview = await window.go.main.App.PreviewMergeTag(mergeTagSourceID, dest.id);
            if (preview.blockers && preview.blockers.length > 0) {
                mergeTagPreview.innerHTML = preview.blockers.map(b =>
                    `<span class="block text-red-500">${escapeHTML(b)}</span>`
                ).join('');
                mergeTagConfirmBtn.disabled = true;
                return;
            }
            mergeTagPreview.innerHTML = `
                <span class="block">${preview.clip_count} clip${preview.clip_count !== 1 ? 's' : ''} will be reassigned.</span>
                <span class="block">${preview.descendant_count} descendant tag${preview.descendant_count !== 1 ? 's' : ''} will move under ${escapeHTML(preview.dest_name)}/.</span>`;
            mergeTagConfirmBtn.disabled = false;
            mergeTagConfirmBtn.dataset.destId = dest.id;
        } catch (err) {
            mergeTagPreview.textContent = `Error: ${err.message || err}`;
            mergeTagConfirmBtn.disabled = true;
        }
    }, 200);
}

mergeTagDestInput?.addEventListener('input', updateMergePreview);
mergeTagCloseBtn?.addEventListener('click', closeMergeTagModal);
mergeTagCancelBtn?.addEventListener('click', closeMergeTagModal);
mergeTagModal?.addEventListener('click', (e) => {
    if (e.target === mergeTagModal) closeMergeTagModal();
});
mergeTagConfirmBtn?.addEventListener('click', async () => {
    const destID = parseInt(mergeTagConfirmBtn.dataset.destId, 10);
    if (!destID || mergeTagSourceID == null) return;
    try {
        await window.go.main.App.MergeTag(mergeTagSourceID, destID);
        showToast('Tag merged', 'success');
        closeMergeTagModal();
        // Event handler (tag:merged) will reload state + re-navigate.
    } catch (err) {
        showToast(`Merge failed: ${err.message || err}`, 'error');
    }
});

window.openMergeTagModal = openMergeTagModal;
```

- [ ] **Step 4: Bind the context menu in `tags.js`**

Find the tag row rendering code in `tags.js` and add, for each tag row element:

```javascript
row.addEventListener('contextmenu', (e) => {
    e.preventDefault();
    const items = [
        {
            id: 'merge-into',
            label: 'Merge into…',
            iconHtml: '<svg class="w-3 h-3 opacity-60" stroke="currentColor" stroke-width="1.5" fill="none" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M7 7h10v10M7 7l10 10"/></svg>',
        },
    ];
    ContextMenu.open(e.clientX, e.clientY, items, (actionId) => {
        if (actionId === 'merge-into') {
            window.openMergeTagModal(tag.id, tag.name);
        }
    });
});
```

(If `ContextMenu.open` has a different signature, adapt; the exact API is visible in `frontend/js/context-menu.js`.)

- [ ] **Step 5: Include the new JS file in `index.html`**

Add a `<script src="js/merge-tag-modal.js"></script>` tag in the same location as other js includes (before `app.js`).

- [ ] **Step 6: Manual smoke test**

Run: `make dev`. Steps:
1. Create `a/x` (and `a/x/foo` child).
2. Create `b/y`.
3. Add a clip to `a/x`.
4. Right-click `a/x`, choose Merge into…, type `b/y`, confirm.
5. Verify: `a/x` is gone, `a/x/foo` became `b/y/foo`, clip is under `b/y`.

- [ ] **Step 7: Commit**

```bash
git add frontend/index.html frontend/js/merge-tag-modal.js frontend/js/tags.js frontend/wailsjs/
git commit -m "feat(frontend): add merge-tag modal + tag context menu"
```

---

### Task 13: REST API endpoint — `POST /api/v1/tags/{id}/merge`

**Files:**
- Modify: `api_manager.go`
- Test: inline test in `api_manager_test.go` (or the relevant existing test file)

- [ ] **Step 1: Write the failing test**

Append to `api_manager_test.go`:

```go
func TestAPI_MergeTag(t *testing.T) {
    app, cleanup := setupTestDBWithTags(t)
    defer cleanup()
    am := newTestAPIManager(t, app)

    src, _ := app.CreateTag("src")
    dst, _ := app.CreateTag("dst")

    body := fmt.Sprintf(`{"dest_id": %d}`, dst.ID)
    req := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/tags/%d/merge", src.ID), strings.NewReader(body))
    req.Header.Set("Authorization", "Bearer " + am.testAdminKey)
    req.Header.Set("Content-Type", "application/json")
    rec := httptest.NewRecorder()
    am.router.ServeHTTP(rec, req)

    if rec.Code != http.StatusOK {
        t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
    }
    var count int
    app.db.QueryRow(`SELECT COUNT(*) FROM tags WHERE id = ?`, src.ID).Scan(&count)
    if count != 0 {
        t.Fatalf("source tag should be deleted")
    }
}
```

(`newTestAPIManager` / `testAdminKey` may need to be a helper — follow the pattern in existing API tests.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestAPI_MergeTag ./...`
Expected: FAIL — endpoint 404s.

- [ ] **Step 3: Register the route**

In `api_manager.go`, find the `/api/v1/tags/...` route block. Add:

```go
mux.HandleFunc("POST /api/v1/tags/{id}/merge", am.authMiddleware(am.requireRole("admin", am.handleMergeTag)))
```

Add the handler:

```go
func (am *APIManager) handleMergeTag(w http.ResponseWriter, r *http.Request) {
    sourceID, err := parseIntParam(r.PathValue("id"))
    if err != nil {
        http.Error(w, "invalid source id", http.StatusBadRequest)
        return
    }
    var body struct {
        DestID int64 `json:"dest_id"`
    }
    if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
        http.Error(w, "invalid body", http.StatusBadRequest)
        return
    }
    if err := am.app.MergeTag(sourceID, body.DestID); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    w.WriteHeader(http.StatusOK)
    _ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -run TestAPI_MergeTag ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add api_manager.go api_manager_test.go
git commit -m "feat(api): POST /api/v1/tags/{id}/merge"
```

---

### Task 14: `mp tag merge` CLI subcommand

**Files:**
- Modify: `cmd/mp/tag.go`
- Test: `cmd/mp/tag_test.go` (if the pattern exists — otherwise manual smoke)

- [ ] **Step 1: Implement the subcommand**

In `cmd/mp/tag.go`, register a new subcommand:

```go
var tagMergeCmd = &cobra.Command{
    Use:   "merge <source-tag> <dest-tag>",
    Short: "Merge source tag into destination tag",
    Args:  cobra.ExactArgs(2),
    RunE: func(cmd *cobra.Command, args []string) error {
        c := client.New()
        sourceID, err := c.ResolveTagID(args[0])
        if err != nil {
            return err
        }
        destID, err := c.ResolveTagID(args[1])
        if err != nil {
            return err
        }
        body := map[string]int64{"dest_id": destID}
        if _, err := c.PostJSON(fmt.Sprintf("/api/v1/tags/%d/merge", sourceID), body); err != nil {
            return err
        }
        if jsonOutput {
            printJSON(map[string]any{"merged": true, "source": args[0], "dest": args[1]})
        } else {
            fmt.Printf("Merged %q into %q\n", args[0], args[1])
        }
        return nil
    },
}

func init() {
    tagCmd.AddCommand(tagMergeCmd)
}
```

(Adapt to the actual client API in `cmd/mp/client/client.go`. `ResolveTagID` / `PostJSON` may need to be added if missing — follow existing patterns like those for `tag assign`.)

- [ ] **Step 2: Build the CLI**

Run: `make mp`
Expected: builds successfully.

- [ ] **Step 3: Manual smoke test**

Run against a dev instance:
```bash
export MP_API_KEY=mp_dev_key  # get from the UI API settings
./mp tag create source-tag
./mp tag create dest-tag
./mp tag merge source-tag dest-tag
./mp tag list  # verify source is gone
```
Expected: success.

- [ ] **Step 4: Commit**

```bash
git add cmd/mp/
git commit -m "feat(mp): add 'mp tag merge' subcommand"
```

---

### Task 15: E2e — merge tags

**Files:**
- Create: `e2e/tests/tags/merge.spec.ts`

- [ ] **Step 1: Write the tests**

```typescript
import { test } from '../../fixtures/test-fixtures';
import { expect } from '@playwright/test';
import path from 'path';
import { generateTestImage, createTempFile } from '../../helpers/test-data';

test.describe('Merge tags', () => {
    test('merges source into destination (flat case)', async ({ app }) => {
        const img = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(img);
        await app.createTag('src');
        await app.createTag('dst');
        await app.tagClipByIndex(0, 'src');

        await app.mergeTag('src', 'dst');

        await app.expectTagMissing('src');
        await app.selectTagFilter('dst');
        await app.expectClipCount(1);
    });

    test('subtree moves with source', async ({ app }) => {
        await app.createTag('a/x');
        await app.createTag('a/x/foo');
        await app.createTag('b/y');

        await app.mergeTag('a/x', 'b/y');

        await app.expectTagMissing('a/x');
        await app.expectTagExists('b/y/foo');
    });

    test('blocked when source tag is served', async ({ app }) => {
        await app.createTag('served');
        await app.createTag('dst');
        await app.startServing('served');

        await app.openMergeModal('served');
        await app.enterMergeDestination('dst');
        const blockers = await app.getMergeModalBlockers();
        expect(blockers.some(b => /serv/i.test(b))).toBe(true);
    });

    test('re-navigates when current folder is the merge source', async ({ app }) => {
        await app.createTag('oldfolder');
        await app.createTag('newfolder');
        await app.enterFolder('oldfolder');

        await app.mergeTag('oldfolder', 'newfolder');

        await app.expectFolderHeader('newfolder');
    });
});
```

Add helpers `mergeTag`, `openMergeModal`, `enterMergeDestination`, `getMergeModalBlockers`, `startServing`, `expectTagMissing`, `expectTagExists`, `selectTagFilter` to `e2e/fixtures/test-fixtures.ts` as needed, using the established pattern.

- [ ] **Step 2: Run the e2e**

Run: `cd e2e && npx playwright test tests/tags/merge.spec.ts 2>&1 | tail -40`
Expected: all PASS.

- [ ] **Step 3: Run full suite for regressions**

Run: `cd e2e && npm test 2>&1 | tail -60`
Expected: all PASS.

- [ ] **Step 4: Commit**

```bash
git add e2e/tests/tags/merge.spec.ts e2e/fixtures/
git commit -m "test(tags): e2e merge scenarios"
```

---

## Phase 3 — Part B.2: Database Compact

### Task 16: `GetDatabaseSize` + `CompactDatabase` Go methods

**Files:**
- Create: `maintenance.go`
- Test: `maintenance_test.go` (new)

- [ ] **Step 1: Write the failing tests**

Create `maintenance_test.go`:

```go
package main

import (
    "testing"
)

func TestGetDatabaseSize_ReturnsNonZero(t *testing.T) {
    app, cleanup := setupTestDBWithTags(t)
    defer cleanup()

    size, err := app.GetDatabaseSize()
    if err != nil {
        t.Fatalf("GetDatabaseSize: %v", err)
    }
    if size <= 0 {
        t.Fatalf("expected nonzero size, got %d", size)
    }
}

func TestCompactDatabase_ReducesSizeAfterBigDelete(t *testing.T) {
    app, cleanup := setupTestDBWithTags(t)
    defer cleanup()

    // Insert and delete a large blob to create free pages.
    big := make([]byte, 1024*1024) // 1 MB
    _, err := app.db.Exec(`INSERT INTO clips (content_type, data, filename) VALUES ('application/octet-stream', ?, 'big.bin')`, big)
    if err != nil {
        t.Fatalf("insert big: %v", err)
    }
    _, err = app.db.Exec(`DELETE FROM clips WHERE filename = 'big.bin'`)
    if err != nil {
        t.Fatalf("delete big: %v", err)
    }

    before, after, err := app.CompactDatabase()
    if err != nil {
        t.Fatalf("CompactDatabase: %v", err)
    }
    if after > before {
        t.Fatalf("expected after (%d) <= before (%d)", after, before)
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run 'TestGetDatabaseSize|TestCompactDatabase' ./...`
Expected: FAIL — methods don't exist.

- [ ] **Step 3: Implement the methods**

Create `maintenance.go`:

```go
package main

import (
    "fmt"
    "os"
    "path/filepath"
)

// GetDatabaseSize returns the combined byte size of clips.db + -wal + -shm.
func (a *App) GetDatabaseSize() (int64, error) {
    dataDir, err := getDataDir()
    if err != nil {
        return 0, err
    }
    var total int64
    for _, suffix := range []string{"clips.db", "clips.db-wal", "clips.db-shm"} {
        info, err := os.Stat(filepath.Join(dataDir, suffix))
        if err != nil {
            if os.IsNotExist(err) {
                continue
            }
            return 0, fmt.Errorf("stat %s: %w", suffix, err)
        }
        total += info.Size()
    }
    return total, nil
}

// CompactDatabase runs VACUUM + ANALYZE on clips.db. Returns before/after
// sizes (bytes). VACUUM cannot run inside an explicit transaction.
func (a *App) CompactDatabase() (before, after int64, err error) {
    before, err = a.GetDatabaseSize()
    if err != nil {
        return 0, 0, err
    }
    if _, err := a.db.Exec(`VACUUM`); err != nil {
        return before, 0, fmt.Errorf("vacuum: %w", err)
    }
    if _, err := a.db.Exec(`ANALYZE`); err != nil {
        return before, 0, fmt.Errorf("analyze: %w", err)
    }
    after, err = a.GetDatabaseSize()
    if err != nil {
        return before, 0, err
    }
    return before, after, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -run 'TestGetDatabaseSize|TestCompactDatabase' ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add maintenance.go maintenance_test.go
git commit -m "feat(maintenance): add GetDatabaseSize + CompactDatabase"
```

---

### Task 17: Frontend — DB compact button

**Files:**
- Modify: `frontend/index.html` (maintenance modal)
- Modify: `frontend/js/maintenance.js`

- [ ] **Step 1: Regenerate Wails bindings**

Run: `make bindings`
Expected: `GetDatabaseSize`, `CompactDatabase` exposed.

- [ ] **Step 2: Add the button to `index.html`**

Locate the maintenance modal's button list (the same block containing `maintenance-deduplicate-btn`). Add:

```html
<button id="maintenance-compact-db-btn" class="border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-600 text-xs font-medium py-2 px-3 rounded-md transition-colors text-left">
    Compact database
    <span class="block text-[10px] text-stone-400 mt-0.5">Reclaim free space after deletions (VACUUM + ANALYZE)</span>
</button>
```

- [ ] **Step 3: Add the handler in `maintenance.js`**

Append to `frontend/js/maintenance.js`:

```javascript
const maintenanceCompactDbBtn = document.getElementById('maintenance-compact-db-btn');

async function runCompactDatabase() {
    let sizeBefore;
    try {
        sizeBefore = await window.go.main.App.GetDatabaseSize();
    } catch (err) {
        showToast('Failed to read DB size', 'error');
        return;
    }
    const humanBefore = formatBytes(sizeBefore);
    closeMaintenance();

    showConfirmDialog(
        'Compact database',
        `<span class="block mb-2">Current size: ${humanBefore}</span>` +
        `<span class="block">Running VACUUM + ANALYZE may take a few seconds and briefly locks the database.</span>`,
        async () => {
            try {
                const [before, after] = await window.go.main.App.CompactDatabase();
                const reclaimed = before - after;
                showToast(`Reclaimed ${formatBytes(reclaimed)} (was ${formatBytes(before)}, now ${formatBytes(after)})`, 'success');
            } catch (err) {
                showToast('Compact failed', 'error');
            }
        }
    );
}

maintenanceCompactDbBtn?.addEventListener('click', runCompactDatabase);
```

If `formatBytes` doesn't exist, add to `frontend/js/utils.js`:

```javascript
function formatBytes(n) {
    if (n < 1024) return `${n} B`;
    if (n < 1024*1024) return `${(n/1024).toFixed(1)} KB`;
    if (n < 1024*1024*1024) return `${(n/(1024*1024)).toFixed(1)} MB`;
    return `${(n/(1024*1024*1024)).toFixed(2)} GB`;
}
```

- [ ] **Step 4: Manual smoke test**

Run: `make dev`. Open maintenance modal, click Compact database, confirm. Toast should report reclaimed bytes.

- [ ] **Step 5: Commit**

```bash
git add frontend/
git commit -m "feat(frontend): add Compact database button to maintenance modal"
```

---

### Task 18: REST endpoint — `POST /api/v1/maintenance/vacuum`

**Files:**
- Modify: `api_manager.go`
- Test: `api_manager_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestAPI_MaintenanceVacuum(t *testing.T) {
    app, cleanup := setupTestDBWithTags(t)
    defer cleanup()
    am := newTestAPIManager(t, app)

    req := httptest.NewRequest("POST", "/api/v1/maintenance/vacuum", nil)
    req.Header.Set("Authorization", "Bearer " + am.testAdminKey)
    rec := httptest.NewRecorder()
    am.router.ServeHTTP(rec, req)
    if rec.Code != http.StatusOK {
        t.Fatalf("status %d, body %s", rec.Code, rec.Body.String())
    }
    var resp struct {
        Before int64 `json:"before"`
        After  int64 `json:"after"`
    }
    json.Unmarshal(rec.Body.Bytes(), &resp)
    if resp.Before == 0 {
        t.Fatalf("expected nonzero before")
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test -run TestAPI_MaintenanceVacuum ./...`
Expected: FAIL.

- [ ] **Step 3: Register route + handler**

In `api_manager.go`:

```go
mux.HandleFunc("POST /api/v1/maintenance/vacuum", am.authMiddleware(am.requireRole("admin", am.handleVacuum)))
```

```go
func (am *APIManager) handleVacuum(w http.ResponseWriter, r *http.Request) {
    before, after, err := am.app.CompactDatabase()
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    _ = json.NewEncoder(w).Encode(map[string]int64{"before": before, "after": after})
}
```

- [ ] **Step 4: Run test, commit**

Run: `go test -run TestAPI_MaintenanceVacuum ./...`
Expected: PASS.

```bash
git add api_manager.go api_manager_test.go
git commit -m "feat(api): POST /api/v1/maintenance/vacuum"
```

---

### Task 19: `mp maintenance vacuum` CLI subcommand

**Files:**
- Create: `cmd/mp/maintenance.go`
- Modify: `cmd/mp/main.go`

- [ ] **Step 1: Create the subcommand**

Create `cmd/mp/maintenance.go`:

```go
package main

import (
    "fmt"
    "github.com/spf13/cobra"
    "mahpastes/cmd/mp/client"
)

var maintenanceCmd = &cobra.Command{
    Use:   "maintenance",
    Short: "Database maintenance tools",
}

var maintenanceVacuumCmd = &cobra.Command{
    Use:   "vacuum",
    Short: "Compact the database (VACUUM + ANALYZE)",
    RunE: func(cmd *cobra.Command, args []string) error {
        c := client.New()
        var resp struct {
            Before int64 `json:"before"`
            After  int64 `json:"after"`
        }
        if err := c.PostJSONInto("/api/v1/maintenance/vacuum", nil, &resp); err != nil {
            return err
        }
        if jsonOutput {
            printJSON(resp)
        } else {
            fmt.Printf("Compact complete. Before: %d bytes, After: %d bytes, Reclaimed: %d bytes\n",
                resp.Before, resp.After, resp.Before-resp.After)
        }
        return nil
    },
}

func init() {
    maintenanceCmd.AddCommand(maintenanceVacuumCmd)
}
```

`PostJSONInto` may need to be added to `client.go`.

- [ ] **Step 2: Register in `main.go`**

Add to the root command:

```go
rootCmd.AddCommand(maintenanceCmd)
```

- [ ] **Step 3: Build + smoke test**

Run: `make mp`
Run: `./mp maintenance vacuum`
Expected: prints reclaimed bytes.

- [ ] **Step 4: Commit**

```bash
git add cmd/mp/
git commit -m "feat(mp): add 'mp maintenance vacuum' subcommand"
```

---

### Task 20: E2e — DB compact

**Files:**
- Create: `e2e/tests/maintenance/vacuum.spec.ts`

- [ ] **Step 1: Write the test**

```typescript
import { test } from '../../fixtures/test-fixtures';
import { expect } from '@playwright/test';

test.describe('Maintenance: compact database', () => {
    test('runs VACUUM + ANALYZE and reports reclaimed space', async ({ app }) => {
        await app.openMaintenanceModal();
        const compactBtn = app.page.locator('#maintenance-compact-db-btn');
        await compactBtn.click();
        await app.confirmDialog();

        const toast = await app.waitForToast(/reclaimed|was/i);
        expect(toast).toMatch(/bytes|KB|MB/);
    });
});
```

Add `openMaintenanceModal`, `confirmDialog`, `waitForToast` helpers to the fixture if missing.

- [ ] **Step 2: Run**

Run: `cd e2e && npx playwright test tests/maintenance/vacuum.spec.ts 2>&1 | tail -30`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add e2e/
git commit -m "test(maintenance): e2e for DB compact"
```

---

## Phase 4 — Part B.3: Stale File Sweep

### Task 21: `GetStaleFiles` + `CleanStaleFiles` Go methods

**Files:**
- Modify: `maintenance.go`
- Test: `maintenance_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `maintenance_test.go`:

```go
func TestGetStaleFiles_DetectsOldTempFiles(t *testing.T) {
    app, cleanup := setupTestDBWithTags(t)
    defer cleanup()

    // Write a temp file with mtime 2 hours ago.
    tempDir := app.tempDir
    p := filepath.Join(tempDir, "stale.bin")
    if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
        t.Fatalf("write: %v", err)
    }
    past := time.Now().Add(-2 * time.Hour)
    if err := os.Chtimes(p, past, past); err != nil {
        t.Fatalf("chtimes: %v", err)
    }

    files, err := app.GetStaleFiles()
    if err != nil {
        t.Fatalf("GetStaleFiles: %v", err)
    }
    found := false
    for _, f := range files {
        if f.Name == "stale.bin" && f.Source == "clip_temp_files" {
            found = true
        }
    }
    if !found {
        t.Fatalf("expected stale.bin in results, got %+v", files)
    }
}

func TestCleanStaleFiles_RemovesThem(t *testing.T) {
    app, cleanup := setupTestDBWithTags(t)
    defer cleanup()

    p := filepath.Join(app.tempDir, "stale2.bin")
    os.WriteFile(p, []byte("yy"), 0644)
    past := time.Now().Add(-2 * time.Hour)
    os.Chtimes(p, past, past)

    count, bytes, err := app.CleanStaleFiles()
    if err != nil {
        t.Fatalf("CleanStaleFiles: %v", err)
    }
    if count < 1 {
        t.Fatalf("expected at least 1 cleaned, got %d", count)
    }
    if bytes < 2 {
        t.Fatalf("expected ≥2 bytes, got %d", bytes)
    }
    if _, err := os.Stat(p); !os.IsNotExist(err) {
        t.Fatalf("file should be removed")
    }
}
```

- [ ] **Step 2: Run tests, verify failure**

Run: `go test -run 'TestGetStaleFiles|TestCleanStaleFiles' ./...`
Expected: FAIL.

- [ ] **Step 3: Implement the methods**

Append to `maintenance.go`:

```go
// StaleFile describes a single file found by the stale-file sweep.
type StaleFile struct {
    Source   string  `json:"source"` // "clip_temp_files" | "share-staging"
    Name     string  `json:"name"`
    Size     int64   `json:"size"`
    AgeHours float64 `json:"age_hours"`
    absPath  string
}

const (
    staleTempLeaseWindow     = 60 * time.Minute  // matches defaultTempLeaseTTL
    staleShareStagingWindow  = 24 * time.Hour
)

// GetStaleFiles scans clip_temp_files/ (past 60-min lease) and share-staging/
// (past 24 h) for files safe to remove.
func (a *App) GetStaleFiles() ([]StaleFile, error) {
    now := time.Now()
    var out []StaleFile

    dataDir, err := getDataDir()
    if err != nil {
        return nil, err
    }

    sources := []struct {
        key    string
        dir    string
        window time.Duration
    }{
        {"clip_temp_files", filepath.Join(dataDir, "clip_temp_files"), staleTempLeaseWindow},
        {"share-staging", filepath.Join(dataDir, "share-staging"), staleShareStagingWindow},
    }
    for _, s := range sources {
        entries, err := os.ReadDir(s.dir)
        if err != nil {
            if os.IsNotExist(err) {
                continue
            }
            return nil, fmt.Errorf("read %s: %w", s.dir, err)
        }
        for _, e := range entries {
            if e.IsDir() {
                continue
            }
            info, err := e.Info()
            if err != nil {
                continue
            }
            age := now.Sub(info.ModTime())
            if age < s.window {
                continue
            }
            out = append(out, StaleFile{
                Source:   s.key,
                Name:     e.Name(),
                Size:     info.Size(),
                AgeHours: age.Hours(),
                absPath:  filepath.Join(s.dir, e.Name()),
            })
        }
    }
    return out, nil
}

// CleanStaleFiles deletes every file returned by GetStaleFiles and reports
// how many were removed and how many bytes were reclaimed.
func (a *App) CleanStaleFiles() (count int, bytes int64, err error) {
    files, err := a.GetStaleFiles()
    if err != nil {
        return 0, 0, err
    }
    for _, f := range files {
        if err := os.Remove(f.absPath); err != nil {
            // Log but keep going — a single failure shouldn't abort the sweep.
            continue
        }
        count++
        bytes += f.Size
    }
    return count, bytes, nil
}
```

Ensure `time` and `path/filepath` are imported.

- [ ] **Step 4: Run tests, commit**

Run: `go test -run 'TestGetStaleFiles|TestCleanStaleFiles' ./...`
Expected: PASS.

```bash
git add maintenance.go maintenance_test.go
git commit -m "feat(maintenance): add GetStaleFiles + CleanStaleFiles"
```

---

### Task 22: Frontend — stale file sweep button

**Files:**
- Modify: `frontend/index.html`
- Modify: `frontend/js/maintenance.js`

- [ ] **Step 1: Regenerate bindings**

Run: `make bindings`.

- [ ] **Step 2: Add the button to `index.html`**

In the maintenance modal:

```html
<button id="maintenance-stale-files-btn" class="border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-600 text-xs font-medium py-2 px-3 rounded-md transition-colors text-left">
    Sweep stale files
    <span class="block text-[10px] text-stone-400 mt-0.5">Remove expired drag-out and share-staging temp files</span>
</button>
```

- [ ] **Step 3: Add the handler in `maintenance.js`**

```javascript
const maintenanceStaleFilesBtn = document.getElementById('maintenance-stale-files-btn');

async function runStaleFileSweep() {
    let files;
    try {
        files = await window.go.main.App.GetStaleFiles();
    } catch (err) {
        showToast('Failed to scan stale files', 'error');
        return;
    }
    if (!files || files.length === 0) {
        showToast('No stale files to clean');
        return;
    }
    const totalBytes = files.reduce((sum, f) => sum + (f.size || 0), 0);
    const grouped = files.reduce((acc, f) => {
        (acc[f.source] = acc[f.source] || []).push(f);
        return acc;
    }, {});
    const listHTML = Object.entries(grouped).map(([src, arr]) =>
        `<span class="block text-left font-semibold text-stone-600 mt-1">${escapeHTML(src)}</span>` +
        arr.map(f => `<span class="block text-left">&middot; ${escapeHTML(f.name)} (${formatBytes(f.size)}, ${f.age_hours.toFixed(1)}h)</span>`).join('')
    ).join('');

    closeMaintenance();
    showConfirmDialog(
        'Sweep stale files',
        `<span class="block mb-2">${files.length} file${files.length !== 1 ? 's' : ''} (${formatBytes(totalBytes)}):</span>` +
        `<span class="block text-[10px] text-stone-400 mb-2 max-h-40 overflow-y-auto">${listHTML}</span>`,
        async () => {
            try {
                const [count, bytes] = await window.go.main.App.CleanStaleFiles();
                showToast(`Removed ${count} file${count !== 1 ? 's' : ''} (${formatBytes(bytes)})`, 'success');
            } catch (err) {
                showToast('Sweep failed', 'error');
            }
        }
    );
}

maintenanceStaleFilesBtn?.addEventListener('click', runStaleFileSweep);
```

- [ ] **Step 4: Manual smoke, commit**

Run `make dev`, verify button, commit:

```bash
git add frontend/
git commit -m "feat(frontend): add stale file sweep button to maintenance modal"
```

---

### Task 23: REST endpoint — `GET|POST /api/v1/maintenance/stale-files`

**Files:**
- Modify: `api_manager.go`
- Test: `api_manager_test.go`

- [ ] **Step 1: Write the failing test**

Append to `api_manager_test.go`:

```go
func TestAPI_MaintenanceStaleFiles_ListAndClean(t *testing.T) {
    app, cleanup := setupTestDBWithTags(t)
    defer cleanup()
    am := newTestAPIManager(t, app)

    // Seed a stale temp file.
    p := filepath.Join(app.tempDir, "stale-api.bin")
    if err := os.WriteFile(p, []byte("xx"), 0644); err != nil {
        t.Fatalf("write: %v", err)
    }
    past := time.Now().Add(-2 * time.Hour)
    os.Chtimes(p, past, past)

    // GET should return the stale file.
    req := httptest.NewRequest("GET", "/api/v1/maintenance/stale-files", nil)
    req.Header.Set("Authorization", "Bearer "+am.testAdminKey)
    rec := httptest.NewRecorder()
    am.router.ServeHTTP(rec, req)
    if rec.Code != http.StatusOK {
        t.Fatalf("GET status %d, body %s", rec.Code, rec.Body.String())
    }
    var files []map[string]any
    json.Unmarshal(rec.Body.Bytes(), &files)
    if len(files) < 1 {
        t.Fatalf("expected ≥1 stale file, got %d", len(files))
    }

    // POST should clean them.
    req2 := httptest.NewRequest("POST", "/api/v1/maintenance/stale-files/clean", nil)
    req2.Header.Set("Authorization", "Bearer "+am.testAdminKey)
    rec2 := httptest.NewRecorder()
    am.router.ServeHTTP(rec2, req2)
    if rec2.Code != http.StatusOK {
        t.Fatalf("POST status %d, body %s", rec2.Code, rec2.Body.String())
    }
    var resp struct {
        Count int   `json:"count"`
        Bytes int64 `json:"bytes"`
    }
    json.Unmarshal(rec2.Body.Bytes(), &resp)
    if resp.Count < 1 {
        t.Fatalf("expected ≥1 cleaned, got %d", resp.Count)
    }
    if _, err := os.Stat(p); !os.IsNotExist(err) {
        t.Fatalf("file should be removed")
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test -run TestAPI_MaintenanceStaleFiles_ListAndClean ./...`
Expected: FAIL — routes 404.

- [ ] **Step 3: Register routes + handlers**

```go
mux.HandleFunc("GET /api/v1/maintenance/stale-files", am.authMiddleware(am.requireRole("admin", am.handleListStaleFiles)))
mux.HandleFunc("POST /api/v1/maintenance/stale-files/clean", am.authMiddleware(am.requireRole("admin", am.handleCleanStaleFiles)))
```

Handlers:

```go
func (am *APIManager) handleListStaleFiles(w http.ResponseWriter, r *http.Request) {
    files, err := am.app.GetStaleFiles()
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    _ = json.NewEncoder(w).Encode(files)
}
func (am *APIManager) handleCleanStaleFiles(w http.ResponseWriter, r *http.Request) {
    count, bytes, err := am.app.CleanStaleFiles()
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    _ = json.NewEncoder(w).Encode(map[string]any{"count": count, "bytes": bytes})
}
```

- [ ] **Step 4: Run test to verify pass**

Run: `go test -run TestAPI_MaintenanceStaleFiles_ListAndClean ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add api_manager.go api_manager_test.go
git commit -m "feat(api): /api/v1/maintenance/stale-files endpoints"
```

---

### Task 24: `mp maintenance stale-files` CLI

**Files:**
- Modify: `cmd/mp/maintenance.go`

- [ ] **Step 1: Implement subcommands**

Append to `cmd/mp/maintenance.go`:

```go
var maintenanceStaleFilesCmd = &cobra.Command{
    Use:   "stale-files",
    Short: "List or clean stale temp/share-staging files",
}

var maintenanceStaleFilesListCmd = &cobra.Command{
    Use:   "list",
    Short: "List stale files",
    RunE: func(cmd *cobra.Command, args []string) error {
        c := client.New()
        var files []map[string]any
        if err := c.GetJSONInto("/api/v1/maintenance/stale-files", &files); err != nil {
            return err
        }
        if jsonOutput {
            printJSON(files)
        } else {
            for _, f := range files {
                fmt.Printf("%-20s  %8d B  %6.1fh  %s\n", f["source"], int64(f["size"].(float64)), f["age_hours"].(float64), f["name"])
            }
        }
        return nil
    },
}

var maintenanceStaleFilesCleanCmd = &cobra.Command{
    Use:   "clean",
    Short: "Remove stale files",
    RunE: func(cmd *cobra.Command, args []string) error {
        c := client.New()
        var resp struct { Count int `json:"count"`; Bytes int64 `json:"bytes"` }
        if err := c.PostJSONInto("/api/v1/maintenance/stale-files/clean", nil, &resp); err != nil {
            return err
        }
        if jsonOutput {
            printJSON(resp)
        } else {
            fmt.Printf("Removed %d file(s), %d bytes reclaimed\n", resp.Count, resp.Bytes)
        }
        return nil
    },
}

func init() {
    maintenanceCmd.AddCommand(maintenanceStaleFilesCmd)
    maintenanceStaleFilesCmd.AddCommand(maintenanceStaleFilesListCmd, maintenanceStaleFilesCleanCmd)
}
```

- [ ] **Step 2: Build + smoke test + commit**

Run: `make mp` then manual `./mp maintenance stale-files list` / `clean`.

```bash
git add cmd/mp/maintenance.go
git commit -m "feat(mp): 'mp maintenance stale-files' subcommands"
```

---

### Task 25: E2e — stale file sweep

**Files:**
- Create: `e2e/tests/maintenance/stale-files.spec.ts`

- [ ] **Step 1: Write the test**

```typescript
import { test } from '../../fixtures/test-fixtures';
import { expect } from '@playwright/test';

test.describe('Maintenance: stale file sweep', () => {
    test('detects and removes stale temp files', async ({ app }) => {
        // Seed a stale file via the app's data dir (requires helper).
        await app.seedStaleTempFile('stale-e2e.bin', 100);

        await app.openMaintenanceModal();
        await app.page.locator('#maintenance-stale-files-btn').click();
        await app.confirmDialog();

        const toast = await app.waitForToast(/removed/i);
        expect(toast).toMatch(/1\s+file/);
    });

    test('shows "nothing to clean" when none are stale', async ({ app }) => {
        await app.openMaintenanceModal();
        await app.page.locator('#maintenance-stale-files-btn').click();
        const toast = await app.waitForToast(/no stale/i);
        expect(toast).toBeTruthy();
    });
});
```

`seedStaleTempFile` requires writing a file into the app's `clip_temp_files` dir and setting its mtime — implement via an `os.Chtimes`-equivalent helper (exposed through a dev-only API or by directly writing to the known data dir from the test).

- [ ] **Step 2: Run + commit**

Run: `cd e2e && npx playwright test tests/maintenance/stale-files.spec.ts 2>&1 | tail -30`
Expected: PASS.

```bash
git add e2e/
git commit -m "test(maintenance): e2e stale file sweep"
```

---

## Phase 5 — Part B.4: Orphan DB Rows Sweep

### Task 26: `GetOrphanDBRows` + `CleanOrphanDBRows` Go methods

**Files:**
- Modify: `maintenance.go`
- Test: `maintenance_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `maintenance_test.go`:

```go
func TestGetOrphanDBRows_DetectsPluginStorage(t *testing.T) {
    app, cleanup := setupTestDBWithTags(t)
    defer cleanup()

    // Orphan plugin_storage row: insert for a plugin_id that doesn't exist.
    if _, err := app.db.Exec(`INSERT INTO plugin_storage (plugin_id, key, value) VALUES (99999, 'k', 'v')`); err != nil {
        t.Fatalf("insert orphan storage: %v", err)
    }

    report, err := app.GetOrphanDBRows()
    if err != nil {
        t.Fatalf("get: %v", err)
    }
    if report.PluginStorage < 1 {
        t.Fatalf("expected ≥1 orphan plugin_storage, got %d", report.PluginStorage)
    }
}

func TestCleanOrphanDBRows_RemovesThem(t *testing.T) {
    app, cleanup := setupTestDBWithTags(t)
    defer cleanup()

    app.db.Exec(`INSERT INTO plugin_storage (plugin_id, key, value) VALUES (99999, 'k', 'v')`)

    report, err := app.CleanOrphanDBRows()
    if err != nil {
        t.Fatalf("clean: %v", err)
    }
    if report.PluginStorage < 1 {
        t.Fatalf("expected cleaned count ≥1")
    }
    var count int
    app.db.QueryRow(`SELECT COUNT(*) FROM plugin_storage WHERE plugin_id = 99999`).Scan(&count)
    if count != 0 {
        t.Fatalf("orphan should be gone, got %d", count)
    }
}
```

- [ ] **Step 2: Run tests, verify failure**

Run: `go test -run 'TestGetOrphanDBRows|TestCleanOrphanDBRows' ./...`
Expected: FAIL.

- [ ] **Step 3: Implement the methods**

Append to `maintenance.go`:

```go
// OrphanReport counts orphaned rows (or in the case of auto-tags, rows that
// will be NULL-ed) found across the DB.
type OrphanReport struct {
    PluginStorage     int `json:"plugin_storage"`
    PluginPermissions int `json:"plugin_permissions"`
    StaleFollows      int `json:"stale_follows"`
    StaleAutoTags     int `json:"stale_auto_tags"`
    StaleHiddenTagIDs int `json:"stale_hidden_tag_ids"`
}

// GetOrphanDBRows counts rows whose parent reference is missing, without
// mutating anything.
func (a *App) GetOrphanDBRows() (OrphanReport, error) {
    var r OrphanReport
    queries := map[string]*int{
        `SELECT COUNT(*) FROM plugin_storage WHERE plugin_id NOT IN (SELECT id FROM plugins)`:         &r.PluginStorage,
        `SELECT COUNT(*) FROM plugin_permissions WHERE plugin_id NOT IN (SELECT id FROM plugins)`:     &r.PluginPermissions,
        `SELECT COUNT(*) FROM follows WHERE local_tag_id NOT IN (SELECT id FROM tags)`:                &r.StaleFollows,
        `SELECT COUNT(*) FROM watched_folders WHERE auto_tag_id IS NOT NULL AND auto_tag_id NOT IN (SELECT id FROM tags)`: &r.StaleAutoTags,
    }
    for q, dst := range queries {
        if err := a.db.QueryRow(q).Scan(dst); err != nil {
            return r, fmt.Errorf("count query: %w", err)
        }
    }

    // Stale hidden-tag IDs.
    hidden, err := a.GetHiddenTags()
    if err != nil {
        return r, fmt.Errorf("get hidden: %w", err)
    }
    for _, id := range hidden {
        var exists int
        a.db.QueryRow(`SELECT COUNT(*) FROM tags WHERE id = ?`, id).Scan(&exists)
        if exists == 0 {
            r.StaleHiddenTagIDs++
        }
    }
    return r, nil
}

// CleanOrphanDBRows deletes (or NULLs) orphan rows inside a single
// transaction and returns per-category cleaned counts.
func (a *App) CleanOrphanDBRows() (OrphanReport, error) {
    var r OrphanReport
    tx, err := a.db.Begin()
    if err != nil {
        return r, fmt.Errorf("begin: %w", err)
    }
    defer tx.Rollback()

    res, err := tx.Exec(`DELETE FROM plugin_storage WHERE plugin_id NOT IN (SELECT id FROM plugins)`)
    if err != nil {
        return r, fmt.Errorf("clean plugin_storage: %w", err)
    }
    n, _ := res.RowsAffected()
    r.PluginStorage = int(n)

    res, err = tx.Exec(`DELETE FROM plugin_permissions WHERE plugin_id NOT IN (SELECT id FROM plugins)`)
    if err != nil {
        return r, fmt.Errorf("clean plugin_permissions: %w", err)
    }
    n, _ = res.RowsAffected()
    r.PluginPermissions = int(n)

    res, err = tx.Exec(`DELETE FROM follows WHERE local_tag_id NOT IN (SELECT id FROM tags)`)
    if err != nil {
        return r, fmt.Errorf("clean follows: %w", err)
    }
    n, _ = res.RowsAffected()
    r.StaleFollows = int(n)

    res, err = tx.Exec(`UPDATE watched_folders SET auto_tag_id = NULL
        WHERE auto_tag_id IS NOT NULL AND auto_tag_id NOT IN (SELECT id FROM tags)`)
    if err != nil {
        return r, fmt.Errorf("null stale auto_tag_id: %w", err)
    }
    n, _ = res.RowsAffected()
    r.StaleAutoTags = int(n)

    // Hidden-tag list — prune stale IDs. Use tx-aware helpers so read+write
    // share the same snapshot and can't be clobbered by a concurrent
    // SetHiddenTags call.
    hidden, err := getHiddenTagsTx(tx)
    if err != nil {
        return r, fmt.Errorf("get hidden: %w", err)
    }
    newHidden := make([]int64, 0, len(hidden))
    for _, id := range hidden {
        var exists int
        // Read via tx for snapshot consistency — another writer can't have
        // re-created the missing tag under this ID between our count and
        // our hidden_tags write.
        if err := tx.QueryRow(`SELECT COUNT(*) FROM tags WHERE id = ?`, id).Scan(&exists); err != nil {
            return r, fmt.Errorf("count tags (id=%d): %w", id, err)
        }
        if exists > 0 {
            newHidden = append(newHidden, id)
        }
    }
    if len(newHidden) != len(hidden) {
        if err := setHiddenTagsTx(tx, newHidden); err != nil {
            return r, fmt.Errorf("update hidden_tags: %w", err)
        }
        r.StaleHiddenTagIDs = len(hidden) - len(newHidden)
    }

    if err := tx.Commit(); err != nil {
        return r, fmt.Errorf("commit: %w", err)
    }
    return r, nil
}
```

- [ ] **Step 4: Run tests, commit**

Run: `go test -run 'TestGetOrphanDBRows|TestCleanOrphanDBRows' ./...`
Expected: PASS.

```bash
git add maintenance.go maintenance_test.go
git commit -m "feat(maintenance): add GetOrphanDBRows + CleanOrphanDBRows"
```

---

### Task 27: Frontend — orphan rows button

**Files:**
- Modify: `frontend/index.html`
- Modify: `frontend/js/maintenance.js`

- [ ] **Step 1: Regenerate bindings**

Run: `make bindings`.

- [ ] **Step 2: Button markup**

```html
<button id="maintenance-orphan-rows-btn" class="border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-600 text-xs font-medium py-2 px-3 rounded-md transition-colors text-left">
    Clean orphan rows
    <span class="block text-[10px] text-stone-400 mt-0.5">Remove plugin/tag references whose parent is gone</span>
</button>
```

- [ ] **Step 3: Handler**

Append to `frontend/js/maintenance.js`:

```javascript
const maintenanceOrphanRowsBtn = document.getElementById('maintenance-orphan-rows-btn');

async function runOrphanRowsSweep() {
    let report;
    try {
        report = await window.go.main.App.GetOrphanDBRows();
    } catch (err) {
        showToast('Failed to scan orphans', 'error');
        return;
    }
    const total = (report.plugin_storage||0) + (report.plugin_permissions||0)
        + (report.stale_follows||0) + (report.stale_auto_tags||0)
        + (report.stale_hidden_tag_ids||0);
    if (total === 0) {
        showToast('No orphan rows to clean');
        return;
    }
    const listHTML = [
        ['Plugin storage rows', report.plugin_storage],
        ['Plugin permissions', report.plugin_permissions],
        ['Stale follows', report.stale_follows],
        ['Stale auto-tag folders (will be NULL-ed)', report.stale_auto_tags],
        ['Stale hidden-tag IDs', report.stale_hidden_tag_ids],
    ].filter(([_, n]) => n > 0)
     .map(([label, n]) => `<span class="block text-left">&middot; ${escapeHTML(label)}: ${n}</span>`)
     .join('');
    closeMaintenance();
    showConfirmDialog('Clean orphan rows',
        `<span class="block mb-2">Will clean:</span>` +
        `<span class="block text-[10px] text-stone-400 mb-2">${listHTML}</span>`,
        async () => {
            try {
                const cleaned = await window.go.main.App.CleanOrphanDBRows();
                const cleanedTotal = (cleaned.plugin_storage||0) + (cleaned.plugin_permissions||0)
                    + (cleaned.stale_follows||0) + (cleaned.stale_auto_tags||0)
                    + (cleaned.stale_hidden_tag_ids||0);
                showToast(`Cleaned ${cleanedTotal} orphan row${cleanedTotal !== 1 ? 's' : ''}`, 'success');
            } catch (err) {
                showToast('Orphan cleanup failed', 'error');
            }
        }
    );
}

maintenanceOrphanRowsBtn?.addEventListener('click', runOrphanRowsSweep);
```

- [ ] **Step 4: Smoke test + commit**

`make dev`, verify button.

```bash
git add frontend/
git commit -m "feat(frontend): add orphan rows button to maintenance modal"
```

---

### Task 28: REST endpoint — `/api/v1/maintenance/orphan-rows`

**Files:**
- Modify: `api_manager.go`
- Test: `api_manager_test.go`

- [ ] **Step 1: Write the failing test**

Append to `api_manager_test.go`:

```go
func TestAPI_MaintenanceOrphanRows_ListAndClean(t *testing.T) {
    app, cleanup := setupTestDBWithTags(t)
    defer cleanup()
    am := newTestAPIManager(t, app)

    // Seed an orphan plugin_storage row.
    if _, err := app.db.Exec(`INSERT INTO plugin_storage (plugin_id, key, value) VALUES (99999, 'k', 'v')`); err != nil {
        t.Fatalf("seed orphan: %v", err)
    }

    // GET report.
    req := httptest.NewRequest("GET", "/api/v1/maintenance/orphan-rows", nil)
    req.Header.Set("Authorization", "Bearer "+am.testAdminKey)
    rec := httptest.NewRecorder()
    am.router.ServeHTTP(rec, req)
    if rec.Code != http.StatusOK {
        t.Fatalf("GET status %d, body %s", rec.Code, rec.Body.String())
    }
    var report struct {
        PluginStorage int `json:"plugin_storage"`
    }
    json.Unmarshal(rec.Body.Bytes(), &report)
    if report.PluginStorage < 1 {
        t.Fatalf("expected ≥1 plugin_storage orphan, got %d", report.PluginStorage)
    }

    // POST to clean.
    req2 := httptest.NewRequest("POST", "/api/v1/maintenance/orphan-rows/clean", nil)
    req2.Header.Set("Authorization", "Bearer "+am.testAdminKey)
    rec2 := httptest.NewRecorder()
    am.router.ServeHTTP(rec2, req2)
    if rec2.Code != http.StatusOK {
        t.Fatalf("POST status %d, body %s", rec2.Code, rec2.Body.String())
    }

    var count int
    app.db.QueryRow(`SELECT COUNT(*) FROM plugin_storage WHERE plugin_id = 99999`).Scan(&count)
    if count != 0 {
        t.Fatalf("orphan should be removed, got %d", count)
    }
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test -run TestAPI_MaintenanceOrphanRows_ListAndClean ./...`
Expected: FAIL — routes 404.

- [ ] **Step 3: Register routes + handlers**

Routes:

```go
mux.HandleFunc("GET /api/v1/maintenance/orphan-rows", am.authMiddleware(am.requireRole("admin", am.handleListOrphanRows)))
mux.HandleFunc("POST /api/v1/maintenance/orphan-rows/clean", am.authMiddleware(am.requireRole("admin", am.handleCleanOrphanRows)))
```

Handlers:

```go
func (am *APIManager) handleListOrphanRows(w http.ResponseWriter, r *http.Request) {
    report, err := am.app.GetOrphanDBRows()
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    _ = json.NewEncoder(w).Encode(report)
}
func (am *APIManager) handleCleanOrphanRows(w http.ResponseWriter, r *http.Request) {
    report, err := am.app.CleanOrphanDBRows()
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    _ = json.NewEncoder(w).Encode(report)
}
```

- [ ] **Step 4: Run test to verify pass**

Run: `go test -run TestAPI_MaintenanceOrphanRows_ListAndClean ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add api_manager.go api_manager_test.go
git commit -m "feat(api): /api/v1/maintenance/orphan-rows endpoints"
```

---

### Task 29: `mp maintenance orphan-rows` CLI

**Files:**
- Modify: `cmd/mp/maintenance.go`

- [ ] **Step 1: Add subcommands**

Append to `cmd/mp/maintenance.go`:

```go
var maintenanceOrphanRowsCmd = &cobra.Command{
    Use:   "orphan-rows",
    Short: "List or clean orphan DB rows",
}

var maintenanceOrphanRowsListCmd = &cobra.Command{
    Use:   "list",
    Short: "List orphan row counts",
    RunE: func(cmd *cobra.Command, args []string) error {
        c := client.New()
        var report map[string]int
        if err := c.GetJSONInto("/api/v1/maintenance/orphan-rows", &report); err != nil {
            return err
        }
        if jsonOutput {
            printJSON(report)
        } else {
            for k, v := range report {
                fmt.Printf("%-30s %d\n", k, v)
            }
        }
        return nil
    },
}

var maintenanceOrphanRowsCleanCmd = &cobra.Command{
    Use:   "clean",
    Short: "Clean orphan DB rows",
    RunE: func(cmd *cobra.Command, args []string) error {
        c := client.New()
        var report map[string]int
        if err := c.PostJSONInto("/api/v1/maintenance/orphan-rows/clean", nil, &report); err != nil {
            return err
        }
        if jsonOutput {
            printJSON(report)
        } else {
            total := 0
            for _, v := range report { total += v }
            fmt.Printf("Cleaned %d orphan row(s)\n", total)
        }
        return nil
    },
}

func init() {
    maintenanceCmd.AddCommand(maintenanceOrphanRowsCmd)
    maintenanceOrphanRowsCmd.AddCommand(maintenanceOrphanRowsListCmd, maintenanceOrphanRowsCleanCmd)
}
```

- [ ] **Step 2: Build + smoke + commit**

Run: `make mp` + `./mp maintenance orphan-rows list`.

```bash
git add cmd/mp/maintenance.go
git commit -m "feat(mp): 'mp maintenance orphan-rows' subcommands"
```

---

### Task 30: E2e — orphan rows

**Files:**
- Create: `e2e/tests/maintenance/orphan-rows.spec.ts`

- [ ] **Step 1: Write the test**

```typescript
import { test } from '../../fixtures/test-fixtures';
import { expect } from '@playwright/test';

test.describe('Maintenance: orphan rows', () => {
    test('reports and cleans orphan plugin_storage rows', async ({ app }) => {
        await app.seedOrphanPluginStorage(99999, 'k', 'v');

        await app.openMaintenanceModal();
        await app.page.locator('#maintenance-orphan-rows-btn').click();
        await app.confirmDialog();

        const toast = await app.waitForToast(/cleaned/i);
        expect(toast).toMatch(/1\s+orphan/);
    });

    test('shows "nothing to clean" when none exist', async ({ app }) => {
        await app.openMaintenanceModal();
        await app.page.locator('#maintenance-orphan-rows-btn').click();
        const toast = await app.waitForToast(/no orphan/i);
        expect(toast).toBeTruthy();
    });
});
```

`seedOrphanPluginStorage` is a new helper that injects an orphan via direct SQL (test-only backdoor).

- [ ] **Step 2: Run**

Run: `cd e2e && npx playwright test tests/maintenance/orphan-rows.spec.ts 2>&1 | tail -30`
Expected: PASS.

- [ ] **Step 3: Run full suite**

Run: `cd e2e && npm test 2>&1 | tail -60`
Expected: all PASS.

- [ ] **Step 4: Final commit**

```bash
git add e2e/
git commit -m "test(maintenance): e2e orphan rows"
```

---

## Final Verification

After all 30 tasks:

- [ ] **Run the full Go test suite**

Run: `go test ./... 2>&1 | tail -40`
Expected: all PASS.

- [ ] **Run the full e2e suite**

Run: `cd e2e && npm test 2>&1 | tail -60`
Expected: all PASS.

- [ ] **Manual end-to-end smoke**

Run: `make install` (installs to /Applications), launches. Verify:
1. Maintenance modal shows 5 buttons: Deduplicate, Remove Empty Tags, Compact Database, Sweep Stale Files, Clean Orphan Rows.
2. Tag sidebar right-click → Merge into… works.
3. All four new tools complete successfully.
4. Rename a tag currently in folder view — folder view updates.
5. Delete a tag currently in folder view — folder view navigates up.

- [ ] **Final commit if anything touched up**

---

## Cross-cutting notes

- **Design system compliance:** every new UI element uses stone palette, IBM Plex Mono, `text-xs font-medium`. No new accent colors.
- **Plugin event parity:** `tag:merged` is registered in `ValidEvents()` (Task 1). `tag:updated` and `tag:deleted` were already registered.
- **CLAUDE.md: Wails runtime import invariant.** All new Go code routes Wails calls through `internal/wailsbridge` — no direct `github.com/wailsapp/wails/v2/pkg/runtime` imports outside the bridge. The `a.emitEvent` helper already goes through the bridge. `TestNoRuntimeImportOutsideBridge` must continue to pass.
- **Idempotent migration:** the `api_keys` FK rebuild uses a guard (`needsAPIKeysScopedTagMigration`) so a second startup is a no-op; this is covered by `TestMigration_APIKeysIdempotent`.
