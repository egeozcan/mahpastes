package app

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// These tests pin the rule that a read-modify-write transaction waits out a
// concurrent writer instead of failing with SQLITE_BUSY.
//
// database/sql's Begin is a deferred BEGIN: no lock until the first statement.
// When that statement is a read, the transaction holds a WAL read snapshot,
// and its first write must upgrade it to the write lock. SQLite refuses that
// upgrade at once while another connection holds the write lock — busy_timeout
// is never consulted for it — so the transaction fails in microseconds. Each
// test below holds a write lock on another connection for writerHold and runs
// one such transaction against it: without the fix the transaction fails
// immediately, with it the transaction blocks until the writer commits.

const writerHold = 300 * time.Millisecond

// holdWriteLock takes SQLite's write lock on another pooled connection and
// keeps it for d, standing in for whatever else is writing at that moment:
// the next file of an UploadFiles batch, another publication's emission, a
// follow's last_seen_at bump. It returns once the lock is held; the returned
// func waits for the commit and reports when the lock was released, so a
// test can prove its operation really started while the lock was taken.
func holdWriteLock(t *testing.T, db *sql.DB, d time.Duration) (wait func() time.Time) {
	t.Helper()
	held := make(chan error, 1)
	released := make(chan time.Time, 1)
	go func() {
		tx, err := db.Begin()
		if err != nil {
			held <- err
			return
		}
		// A write as the first statement takes the lock from the
		// no-transaction state, so this cannot itself lose the race.
		if _, err := tx.Exec(
			`INSERT OR REPLACE INTO settings (key, value) VALUES ('test_write_lock_holder', '1')`,
		); err != nil {
			_ = tx.Rollback()
			held <- err
			return
		}
		held <- nil
		time.Sleep(d)
		releasedAt := time.Now()
		if err := tx.Commit(); err != nil {
			t.Errorf("commit lock holder: %v", err)
		}
		released <- releasedAt
	}()
	if err := <-held; err != nil {
		t.Fatalf("take write lock: %v", err)
	}
	return func() time.Time { return <-released }
}

// underConcurrentWriter runs op while another connection holds the write
// lock, and fails the test if op errors or if the lock was already gone by
// the time op started (the run would then prove nothing).
func underConcurrentWriter(t *testing.T, db *sql.DB, op func() error) {
	t.Helper()
	wait := holdWriteLock(t, db, writerHold)
	started := time.Now()
	err := op()
	releasedAt := wait()
	if !started.Before(releasedAt) {
		t.Fatalf("write lock was released before the operation started; the test did not exercise contention")
	}
	if err != nil {
		t.Fatalf("operation failed under a concurrent writer (it must wait out the lock, not fail): %v", err)
	}
}

// probeWrite attempts one write on a dedicated connection that gives up after
// 50ms instead of the DSN's busy_timeout, and reports whether it got through.
func probeWrite(t *testing.T, db *sql.DB) error {
	t.Helper()
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("probe conn: %v", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `PRAGMA busy_timeout = 50`); err != nil {
		t.Fatalf("lower probe busy_timeout: %v", err)
	}
	// Restore before the connection goes back to the pool.
	defer func() {
		if _, err := conn.ExecContext(ctx, `PRAGMA busy_timeout = 5000`); err != nil {
			t.Errorf("restore probe busy_timeout: %v", err)
		}
	}()
	_, err = conn.ExecContext(ctx,
		`INSERT OR REPLACE INTO settings (key, value) VALUES ('test_write_probe', '1')`)
	return err
}

// beginWriteTx's contract, pinned directly: it returns holding the write lock
// (a no-op statement that SQLite ever stopped treating as a write would
// silently bring the upgrade failure back), and the statement that takes the
// lock changes nothing.
func TestBeginWriteTxHoldsWriteLockAndChangesNothing(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	tagID := mustCreateTag(t, app, "kept")
	clipID := insertSharedTestClip(t, app, "hello")
	if _, err := app.db.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, ?)`, clipID, tagID); err != nil {
		t.Fatalf("tag clip: %v", err)
	}

	// Control: a plain Begin holds nothing, so the probe gets through.
	plain, err := app.db.Begin()
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := probeWrite(t, app.db); err != nil {
		t.Fatalf("probe under a plain Begin: %v (want it to get through)", err)
	}
	_ = plain.Rollback()

	tx, err := beginWriteTx(app.db, "clip_tags")
	if err != nil {
		t.Fatalf("beginWriteTx: %v", err)
	}
	if err := probeWrite(t, app.db); err == nil {
		t.Fatalf("probe write got through while beginWriteTx's transaction was open; it does not hold the write lock")
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	assertClipTagNames(t, app, clipID, "kept")
}

func clipsSent(t *testing.T, a *App, pubID int64) int {
	t.Helper()
	var n int
	if err := a.db.QueryRow(`SELECT clips_sent FROM shares WHERE id = ?`, pubID).Scan(&n); err != nil {
		t.Fatalf("read clips_sent: %v", err)
	}
	return n
}

// The emission reads last_seq and only then inserts the ring rows. Losing
// that upgrade used to be only a log line in OnClipCreated: the clip stayed
// in the shared folder and never reached a single follower.
func TestEmitClipWaitsOutConcurrentWriter(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	sharedTag := mustCreateTag(t, app, "shared")
	info, err := app.shareManager.StartShare(sharedTag)
	if err != nil {
		t.Fatalf("StartShare: %v", err)
	}
	clipID := insertSharedTestClip(t, app, "hello")
	if _, err := app.db.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, ?)`, clipID, sharedTag); err != nil {
		t.Fatalf("tag clip: %v", err)
	}

	underConcurrentWriter(t, app.db, func() error {
		return app.shareManager.OnClipCreated(clipID, []int64{sharedTag})
	})

	if got := ringCount(t, app, info.ID); got != ringRowsPerSmallClip {
		t.Fatalf("ring rows = %d, want %d — the clip was never published", got, ringRowsPerSmallClip)
	}
	if got := lastSeq(t, app, info.ID); got != ringRowsPerSmallClip {
		t.Fatalf("last_seq = %d, want %d", got, ringRowsPerSmallClip)
	}
	if got := clipsSent(t, app, info.ID); got != 1 {
		t.Fatalf("clips_sent = %d, want 1", got)
	}
}

// AddTagToClip reads the tag's name for tree exclusivity before its first
// delete. Losing that upgrade failed the whole call, so an upload auto-tagged
// into a folder landed in the library outside it.
func TestAddTagToClipWaitsOutConcurrentWriter(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	oldTag := mustCreateTag(t, app, "work/old")
	newTag := mustCreateTag(t, app, "work/new")
	clipID := insertSharedTestClip(t, app, "hello")
	if err := app.AddTagToClip(clipID, oldTag); err != nil {
		t.Fatalf("setup AddTagToClip: %v", err)
	}

	underConcurrentWriter(t, app.db, func() error {
		return app.AddTagToClip(clipID, newTag)
	})

	// The exclusivity delete and the insert both landed.
	assertClipTagNames(t, app, clipID, "work/new")
}

// The same tagging under a concurrent writer, into a shared tag: both the
// tagging transaction and the hook's emission have to survive it for the clip
// to reach followers.
func TestAddTagToClipIntoSharedTagPublishesUnderConcurrentWriter(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	sharedTag := mustCreateTag(t, app, "shared")
	info, err := app.shareManager.StartShare(sharedTag)
	if err != nil {
		t.Fatalf("StartShare: %v", err)
	}
	clipID := insertSharedTestClip(t, app, "hello")

	underConcurrentWriter(t, app.db, func() error {
		return app.AddTagToClip(clipID, sharedTag)
	})
	app.shareHookWG.Wait()

	assertClipTagNames(t, app, clipID, "shared")
	if got := ringCount(t, app, info.ID); got != ringRowsPerSmallClip {
		t.Fatalf("ring rows = %d, want %d — the clip was never published", got, ringRowsPerSmallClip)
	}
	if got := clipsSent(t, app, info.ID); got != 1 {
		t.Fatalf("clips_sent = %d, want 1", got)
	}
}

// BulkAddTag (bulk tagging and folder drag) runs the same exclusivity read
// for every clip ahead of its inserts.
func TestBulkAddTagWaitsOutConcurrentWriter(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	oldTag := mustCreateTag(t, app, "work/old")
	newTag := mustCreateTag(t, app, "work/new")
	clipA := insertSharedTestClip(t, app, "a")
	clipB := insertSharedTestClip(t, app, "b")
	if err := app.BulkAddTag([]int64{clipA, clipB}, oldTag); err != nil {
		t.Fatalf("setup BulkAddTag: %v", err)
	}

	underConcurrentWriter(t, app.db, func() error {
		return app.BulkAddTag([]int64{clipA, clipB}, newTag)
	})

	assertClipTagNames(t, app, clipA, "work/new")
	assertClipTagNames(t, app, clipB, "work/new")
}

// CreateTag checks for each missing ancestor before inserting, so creating a
// subtag under an existing parent reads before its first write.
func TestCreateTagWaitsOutConcurrentWriter(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	mustCreateTag(t, app, "work")

	var created *Tag
	underConcurrentWriter(t, app.db, func() error {
		var err error
		created, err = app.CreateTag("work/new")
		return err
	})

	var name string
	if err := app.db.QueryRow(`SELECT name FROM tags WHERE id = ?`, created.ID).Scan(&name); err != nil {
		t.Fatalf("read created tag: %v", err)
	}
	if name != "work/new" {
		t.Fatalf("created tag name = %q, want %q", name, "work/new")
	}
}

// UpdateTag reads the old name (to cascade the rename) before updating.
func TestUpdateTagWaitsOutConcurrentWriter(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	tag, err := app.CreateTag("before")
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}

	underConcurrentWriter(t, app.db, func() error {
		return app.UpdateTag(tag.ID, "after", tag.Color)
	})

	var name string
	if err := app.db.QueryRow(`SELECT name FROM tags WHERE id = ?`, tag.ID).Scan(&name); err != nil {
		t.Fatalf("read renamed tag: %v", err)
	}
	if name != "after" {
		t.Fatalf("tag name = %q, want %q", name, "after")
	}
}

// GetRemovableEmptyTags simulates the cleanup inside a rolled-back
// transaction: it queries the candidates, then deletes them to find the next
// pass's. The preview must not fail just because something else is writing.
func TestGetRemovableEmptyTagsWaitsOutConcurrentWriter(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	mustCreateTag(t, app, "lonely")

	var removable []Tag
	underConcurrentWriter(t, app.db, func() error {
		var err error
		removable, err = app.GetRemovableEmptyTags()
		return err
	})

	if len(removable) != 1 || removable[0].Name != "lonely" {
		t.Fatalf("removable = %+v, want just %q", removable, "lonely")
	}
}

// UpdateClipMetadata is a read-modify-write of the metadata JSON;
// SetClipMetadata (UI, REST, Lua metadata.set) routes through it.
func TestUpdateClipMetadataWaitsOutConcurrentWriter(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	clipID := insertSharedTestClip(t, app, "hello")

	underConcurrentWriter(t, app.db, func() error {
		return app.SetClipMetadata(clipID, "source", "camera")
	})

	meta, err := app.GetClipMetadata(clipID)
	if err != nil {
		t.Fatalf("GetClipMetadata: %v", err)
	}
	if meta["source"] != "camera" {
		t.Fatalf("metadata = %v, want source=camera", meta)
	}
}
