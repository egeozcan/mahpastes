package app

import (
	"testing"
	"time"
)

// These tests pin the rule that a tag something else depends on outlives its
// last clip. deleteTagIfOrphaned tidies away a top-level tag the moment its
// last clip leaves, which is right for a throwaway label and wrong for a tag
// the user has wired into something: shares.tag_id cascades, so deleting a
// shared tag silently destroyed the share's key, sequence and ring while the
// in-memory publication lived on — followers reconnected forever against a
// share string that could never work again, and StopShare never ran.

// orphanRemovalPaths are the four calls that end in deleteTagIfOrphaned. Each
// takes the clip out of the tag a different way, and folder drag-to-root goes
// through BulkRemoveTag, so all four must agree.
var orphanRemovalPaths = []struct {
	name   string
	remove func(a *App, clipID, tagID int64) error
}{
	{"DeleteClip", func(a *App, clipID, _ int64) error { return a.DeleteClip(clipID) }},
	{"RemoveTagFromClip", func(a *App, clipID, tagID int64) error { return a.RemoveTagFromClip(clipID, tagID) }},
	{"BulkRemoveTag", func(a *App, clipID, tagID int64) error { return a.BulkRemoveTag([]int64{clipID}, tagID) }},
	{"BulkDelete", func(a *App, clipID, _ int64) error { return a.BulkDelete([]int64{clipID}) }},
}

func tagExists(t *testing.T, a *App, tagID int64) bool {
	t.Helper()
	var n int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM tags WHERE id = ?`, tagID).Scan(&n); err != nil {
		t.Fatalf("count tags: %v", err)
	}
	return n == 1
}

// tagOneClip creates a clip, tags it, and waits for any publication so the
// removal under test is the only thing left in flight.
func tagOneClip(t *testing.T, a *App, tagID int64) int64 {
	t.Helper()
	clipID := insertSharedTestClip(t, a, "hello")
	if err := a.AddTagToClip(clipID, tagID); err != nil {
		t.Fatalf("AddTagToClip: %v", err)
	}
	a.shareHookWG.Wait()
	return clipID
}

func TestOrphanCleanupKeepsSharedTag(t *testing.T) {
	for _, path := range orphanRemovalPaths {
		t.Run(path.name, func(t *testing.T) {
			app, cleanup := setupTestApp(t)
			defer cleanup()

			tagID := mustCreateTag(t, app, "photos")
			info, err := app.shareManager.StartShare(tagID)
			if err != nil {
				t.Fatalf("StartShare: %v", err)
			}
			clipID := tagOneClip(t, app, tagID)
			if got := ringCount(t, app, info.ID); got != ringRowsPerSmallClip {
				t.Fatalf("setup: ring rows = %d, want %d", got, ringRowsPerSmallClip)
			}

			if err := path.remove(app, clipID, tagID); err != nil {
				t.Fatalf("%s: %v", path.name, err)
			}

			if !tagExists(t, app, tagID) {
				t.Fatalf("%s auto-deleted the shared tag", path.name)
			}
			var shareRows int
			if err := app.db.QueryRow(
				`SELECT COUNT(*) FROM shares WHERE id = ? AND tag_id = ?`, info.ID, tagID,
			).Scan(&shareRows); err != nil {
				t.Fatalf("count shares: %v", err)
			}
			if shareRows != 1 {
				t.Fatalf("shares rows = %d after %s, want 1: the cascade destroyed the share's key, so the share string handed out is dead", shareRows, path.name)
			}
			// The ring is the catch-up backlog for followers that were offline;
			// a cascade (or a StopShare) would have taken it too.
			if got := ringCount(t, app, info.ID); got != ringRowsPerSmallClip {
				t.Fatalf("ring rows = %d after %s, want %d", got, path.name, ringRowsPerSmallClip)
			}

			shares, _ := app.shareManager.GetShareStatus()
			var found bool
			for _, s := range shares {
				if s.ID == info.ID {
					found = true
					if s.TagName != "photos" {
						t.Fatalf("GetShareStatus TagName = %q, want %q: the publication outlived its tag row", s.TagName, "photos")
					}
				}
			}
			if !found {
				t.Fatalf("GetShareStatus no longer lists share %d", info.ID)
			}
		})
	}
}

// TestOrphanCleanupStillDeletesUnreferencedTag is the control: a plain
// top-level tag nobody depends on is still tidied away by every path.
func TestOrphanCleanupStillDeletesUnreferencedTag(t *testing.T) {
	for _, path := range orphanRemovalPaths {
		t.Run(path.name, func(t *testing.T) {
			app, cleanup := setupTestApp(t)
			defer cleanup()

			tagID := mustCreateTag(t, app, "scratch")
			clipID := tagOneClip(t, app, tagID)

			if err := path.remove(app, clipID, tagID); err != nil {
				t.Fatalf("%s: %v", path.name, err)
			}
			if tagExists(t, app, tagID) {
				t.Fatalf("%s left an unreferenced empty top-level tag behind", path.name)
			}
		})
	}
}

// TestOrphanCleanupKeepsReferencedTags covers the other things a user can
// point at a tag. Each one is a standing instruction that outlives the tag's
// current contents: deleting the tag under any of them breaks it silently.
func TestOrphanCleanupKeepsReferencedTags(t *testing.T) {
	cases := []struct {
		name string
		// pin makes tagID something else depends on.
		pin func(t *testing.T, a *App, tagID int64)
		// check runs after the removal, for side effects beyond the tag row.
		check func(t *testing.T, a *App, tagID int64)
	}{
		{
			// follows.local_tag_id is ON DELETE RESTRICT, so the bare DELETE
			// already failed here — the guard turns a swallowed constraint
			// error into a deliberate skip.
			name: "follow target",
			pin: func(t *testing.T, a *App, tagID int64) {
				if _, err := a.db.Exec(
					`INSERT INTO follows (remote_peer_id, symkey, local_tag_id, created_at) VALUES ('peer', x'00', ?, 0)`, tagID,
				); err != nil {
					t.Fatalf("insert follow: %v", err)
				}
			},
		},
		{
			// watched_folders.auto_tag_id has no foreign key, so the delete
			// left the watch folder pointing at a tag id that no longer exists
			// and every later import failed to tag.
			name: "watch folder auto-tag",
			pin: func(t *testing.T, a *App, tagID int64) {
				if _, err := a.db.Exec(
					`INSERT INTO watched_folders (path, auto_tag_id) VALUES ('/tmp/watched', ?)`, tagID,
				); err != nil {
					t.Fatalf("insert watched folder: %v", err)
				}
			},
		},
		{
			// scoped_tag_id is ON DELETE SET NULL and the
			// api_keys_revoke_on_scope_null trigger revokes the key: emptying
			// the folder used to revoke the integration's key.
			name: "scoped API key",
			pin: func(t *testing.T, a *App, tagID int64) {
				if _, err := a.db.Exec(
					`INSERT INTO api_keys (name, key_hash, key_prefix, role, scoped_tag_id, is_revoked) VALUES ('scoped', 'hash-scoped', 'mp_', 'editor', ?, 0)`, tagID,
				); err != nil {
					t.Fatalf("insert api key: %v", err)
				}
			},
			check: func(t *testing.T, a *App, tagID int64) {
				var revoked int
				if err := a.db.QueryRow(`SELECT is_revoked FROM api_keys WHERE name = 'scoped'`).Scan(&revoked); err != nil {
					t.Fatalf("read api key: %v", err)
				}
				if revoked != 0 {
					t.Fatal("emptying the scoped tag revoked the API key")
				}
			},
		},
		{
			// Serving lives only in ServeManager's memory; a deleted tag left
			// the server running against a dead id, so a re-created tag of the
			// same name was never served.
			name: "served tag",
			pin: func(t *testing.T, a *App, tagID int64) {
				if _, err := a.serveManager.StartServing(tagID, 0, false, "none"); err != nil {
					t.Fatalf("StartServing: %v", err)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app, cleanup := setupTestApp(t)
			defer cleanup()

			tagID := mustCreateTag(t, app, "inbox")
			clipID := tagOneClip(t, app, tagID)
			tc.pin(t, app, tagID)

			if err := app.RemoveTagFromClip(clipID, tagID); err != nil {
				t.Fatalf("RemoveTagFromClip: %v", err)
			}
			if !tagExists(t, app, tagID) {
				t.Fatalf("orphan cleanup deleted a tag that is a %s", tc.name)
			}
			if tc.check != nil {
				tc.check(t, app, tagID)
			}
		})
	}
}

// TestOrphanCleanupIgnoresRevokedScopedKey: a revoked key is already dead and
// only lingers until the retention sweep, so it must not pin an empty tag.
func TestOrphanCleanupIgnoresRevokedScopedKey(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	tagID := mustCreateTag(t, app, "inbox")
	clipID := tagOneClip(t, app, tagID)
	if _, err := app.db.Exec(
		`INSERT INTO api_keys (name, key_hash, key_prefix, role, scoped_tag_id, is_revoked, revoked_at) VALUES ('old', 'hash-old', 'mp_', 'editor', ?, 1, CURRENT_TIMESTAMP)`, tagID,
	); err != nil {
		t.Fatalf("insert api key: %v", err)
	}

	if err := app.RemoveTagFromClip(clipID, tagID); err != nil {
		t.Fatalf("RemoveTagFromClip: %v", err)
	}
	if tagExists(t, app, tagID) {
		t.Fatal("a revoked key's scope kept an empty tag alive")
	}
}

// --- MergeTag into a shared destination -------------------------------------
//
// MergeTag moves every source clip into the destination in one INSERT ...
// SELECT. checkMergeTagPreconditions refuses a shared source, but a shared
// destination was allowed, so the merge filled the shared folder and published
// none of it.

func TestMergeTagIntoSharedDestPublishesMovedClips(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	srcTag := mustCreateTag(t, app, "inbox")
	dstTag := mustCreateTag(t, app, "photos")
	info, err := app.shareManager.StartShare(dstTag)
	if err != nil {
		t.Fatalf("StartShare: %v", err)
	}

	// arriving is only in the source: the merge is its arrival.
	arriving := tagOneClip(t, app, srcTag)
	// already sits in both (different trees) and was published when it
	// entered the destination; the merge must not publish it again.
	already := tagOneClip(t, app, dstTag)
	if err := app.AddTagToClip(already, srcTag); err != nil {
		t.Fatalf("AddTagToClip: %v", err)
	}
	app.shareHookWG.Wait()
	if got := ringCount(t, app, info.ID); got != ringRowsPerSmallClip {
		t.Fatalf("setup: ring rows = %d, want %d", got, ringRowsPerSmallClip)
	}

	if err := app.MergeTag(srcTag, dstTag); err != nil {
		t.Fatalf("MergeTag: %v", err)
	}
	app.shareHookWG.Wait()

	for _, clipID := range []int64{arriving, already} {
		var n int
		if err := app.db.QueryRow(`SELECT COUNT(*) FROM clip_tags WHERE clip_id = ? AND tag_id = ?`, clipID, dstTag).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("clip %d is not in the destination after the merge", clipID)
		}
	}
	want := 2 * ringRowsPerSmallClip
	if got := ringCount(t, app, info.ID); got != want {
		t.Fatalf("ring rows = %d after merging into a shared tag, want %d (the arrival published once, the clip already there not again)", got, want)
	}
}

// TestMergeTagBlocksWhileRestoreLockHeld: MergeTag is a share-relevant tag
// mutation like AddTagToClip, so it reads backupRestoreMu for the same reason —
// it must not reassign clips by ids a restore is in the middle of replacing.
func TestMergeTagBlocksWhileRestoreLockHeld(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	srcTag := mustCreateTag(t, app, "inbox")
	dstTag := mustCreateTag(t, app, "photos")
	tagOneClip(t, app, srcTag)

	app.backupRestoreMu.Lock()
	unlocked := false
	unlock := func() {
		if !unlocked {
			unlocked = true
			app.backupRestoreMu.Unlock()
		}
	}
	defer unlock()

	done := make(chan error, 1)
	go func() { done <- app.MergeTag(srcTag, dstTag) }()

	time.Sleep(300 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("MergeTag finished (err=%v) while the restore write lock was held", err)
	default:
	}

	unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("MergeTag after unlock: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("MergeTag never completed after the write lock was released")
	}
}

// TestMergeTagOperationAdmissionSpansCommit mirrors
// TestBulkAddTagOperationAdmissionSpansCommit for the merge: without an
// admission held across the commit, the merge could commit, stall, watch a
// restore drain and rebuild, and then publish ids that name restored rows.
func TestMergeTagOperationAdmissionSpansCommit(t *testing.T) {
	app, clipID, dstTag := newTagCommitBlockApp(t, false)
	const srcTag = 11
	// MergeTag also migrates api_keys / watched_folders references; give the
	// minimal schema just enough of them.
	for _, stmt := range []string{
		`CREATE TABLE api_keys (id INTEGER PRIMARY KEY, scoped_tag_id INTEGER)`,
		`CREATE TABLE watched_folders (id INTEGER PRIMARY KEY, auto_tag_id INTEGER)`,
		`INSERT INTO tags (id, name, color) VALUES (11, 'src', '#445566')`,
	} {
		if _, err := app.db.Exec(stmt); err != nil {
			t.Fatalf("exec %q: %v", stmt, err)
		}
	}
	if _, err := app.db.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, ?)`, clipID, srcTag); err != nil {
		t.Fatalf("tag clip: %v", err)
	}

	hook, entered, release := blockOnce()
	commitBlockHook.Store(&hook)
	t.Cleanup(func() { commitBlockHook.Store(nil) })

	merged := make(chan error, 1)
	go func() { merged <- app.MergeTag(srcTag, dstTag) }()
	<-entered

	resumeCh := assertDrainBlocked(t, app, "a merge sat inside tx.Commit()")

	close(release)
	if err := <-merged; err != nil {
		t.Fatalf("MergeTag: %v", err)
	}
	awaitResume(t, resumeCh, "the merge finished")
}
