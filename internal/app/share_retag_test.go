package app

import (
	"encoding/base64"
	"testing"
)

// These tests pin the rule that a clip is published to a share exactly once,
// when it genuinely enters the shared tag — never again because some unrelated
// tag was added to it afterwards. Re-publication is invisible to the publisher
// but shows up on every follower as a duplicate clip they cannot merge away.
//
// All assertions read share_ring directly: a small text clip emits
// clip_start + one chunk + clip_end = 3 rows per publication.

const ringRowsPerSmallClip = 3

// insertSharedTestClip writes a small clip straight into the clips table,
// bypassing UploadFiles so the tagging path under test is the only thing that
// can publish. Wraps insertTestClip (tag_merge_test.go).
func insertSharedTestClip(t *testing.T, a *App, body string) int64 {
	t.Helper()
	return insertTestClip(t, a, "note.txt", "text/plain", []byte(body))
}

func mustCreateTag(t *testing.T, a *App, name string) int64 {
	t.Helper()
	tag, err := a.CreateTag(name)
	if err != nil {
		t.Fatalf("CreateTag(%q): %v", name, err)
	}
	return tag.ID
}

func ringCount(t *testing.T, a *App, pubID int64) int {
	t.Helper()
	var n int
	if err := a.db.QueryRow(
		`SELECT COUNT(*) FROM share_ring WHERE publication_id = ?`, pubID,
	).Scan(&n); err != nil {
		t.Fatalf("count share_ring: %v", err)
	}
	return n
}

func lastSeq(t *testing.T, a *App, pubID int64) int64 {
	t.Helper()
	var seq int64
	if err := a.db.QueryRow(`SELECT last_seq FROM shares WHERE id = ?`, pubID).Scan(&seq); err != nil {
		t.Fatalf("read last_seq: %v", err)
	}
	return seq
}

// TestAddTagToClipPublishesNewlySharedClipOnce is the baseline the regression
// tests below measure against: entering the shared tag emits one clip burst.
func TestAddTagToClipPublishesNewlySharedClipOnce(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	sharedTag := mustCreateTag(t, app, "shared")
	info, err := app.shareManager.StartShare(sharedTag)
	if err != nil {
		t.Fatalf("StartShare: %v", err)
	}

	clipID := insertSharedTestClip(t, app, "hello")
	if err := app.AddTagToClip(clipID, sharedTag); err != nil {
		t.Fatalf("AddTagToClip: %v", err)
	}
	app.shareHookWG.Wait()

	if got := ringCount(t, app, info.ID); got != ringRowsPerSmallClip {
		t.Fatalf("ring rows = %d, want %d (start + chunk + end)", got, ringRowsPerSmallClip)
	}
	if got := lastSeq(t, app, info.ID); got != ringRowsPerSmallClip {
		t.Fatalf("last_seq = %d, want %d", got, ringRowsPerSmallClip)
	}
}

// TestAddTagToClipUnrelatedTagDoesNotRepublish is the core regression: tag
// trees are independent, so adding "misc" to a clip already sitting in the
// shared tag leaves the shared tag in place — and must not re-emit the clip.
func TestAddTagToClipUnrelatedTagDoesNotRepublish(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	sharedTag := mustCreateTag(t, app, "shared")
	otherTag := mustCreateTag(t, app, "misc")
	info, err := app.shareManager.StartShare(sharedTag)
	if err != nil {
		t.Fatalf("StartShare: %v", err)
	}

	clipID := insertSharedTestClip(t, app, "hello")
	if err := app.AddTagToClip(clipID, sharedTag); err != nil {
		t.Fatalf("AddTagToClip(shared): %v", err)
	}
	app.shareHookWG.Wait()
	before := ringCount(t, app, info.ID)
	if before != ringRowsPerSmallClip {
		t.Fatalf("setup: ring rows = %d, want %d", before, ringRowsPerSmallClip)
	}

	if err := app.AddTagToClip(clipID, otherTag); err != nil {
		t.Fatalf("AddTagToClip(misc): %v", err)
	}
	app.shareHookWG.Wait()

	// Sanity: the unrelated tag really did land (different tree, so tree
	// exclusivity did not strip the shared tag either).
	tags, err := app.GetClipTags(clipID)
	if err != nil {
		t.Fatalf("GetClipTags: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("clip has %d tags, want 2 (shared + misc)", len(tags))
	}

	if got := ringCount(t, app, info.ID); got != before {
		t.Fatalf("ring rows = %d after unrelated tag, want %d — clip was re-published", got, before)
	}
	if got := lastSeq(t, app, info.ID); got != int64(before) {
		t.Fatalf("last_seq = %d after unrelated tag, want %d", got, before)
	}
}

// TestAddTagToClipRepeatAddDoesNotRepublish covers re-applying the shared tag
// itself, which INSERT OR IGNORE turns into a no-op at the DB level.
func TestAddTagToClipRepeatAddDoesNotRepublish(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	sharedTag := mustCreateTag(t, app, "shared")
	info, err := app.shareManager.StartShare(sharedTag)
	if err != nil {
		t.Fatalf("StartShare: %v", err)
	}

	clipID := insertSharedTestClip(t, app, "hello")
	for i := 0; i < 3; i++ {
		if err := app.AddTagToClip(clipID, sharedTag); err != nil {
			t.Fatalf("AddTagToClip #%d: %v", i+1, err)
		}
		app.shareHookWG.Wait()
	}

	if got := ringCount(t, app, info.ID); got != ringRowsPerSmallClip {
		t.Fatalf("ring rows = %d after 3 identical adds, want %d", got, ringRowsPerSmallClip)
	}
}

// TestAddTagToClipMoveWithinTreePublishesOnArrival pins the edge case that a
// move inside one tree (a/old → a/new, where a/new is the shared tag) IS a
// genuine arrival and must publish.
func TestAddTagToClipMoveWithinTreePublishesOnArrival(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	oldTag := mustCreateTag(t, app, "a/old")
	sharedTag := mustCreateTag(t, app, "a/new")
	info, err := app.shareManager.StartShare(sharedTag)
	if err != nil {
		t.Fatalf("StartShare: %v", err)
	}

	clipID := insertSharedTestClip(t, app, "hello")
	if err := app.AddTagToClip(clipID, oldTag); err != nil {
		t.Fatalf("AddTagToClip(a/old): %v", err)
	}
	app.shareHookWG.Wait()
	if got := ringCount(t, app, info.ID); got != 0 {
		t.Fatalf("ring rows = %d before the clip entered the shared tag, want 0", got)
	}

	// Tree exclusivity drops a/old and installs a/new — an arrival.
	if err := app.AddTagToClip(clipID, sharedTag); err != nil {
		t.Fatalf("AddTagToClip(a/new): %v", err)
	}
	app.shareHookWG.Wait()
	if got := ringCount(t, app, info.ID); got != ringRowsPerSmallClip {
		t.Fatalf("ring rows = %d after move into shared tag, want %d", got, ringRowsPerSmallClip)
	}
}

// TestBulkAddTagPublishesOnlyNewlyTaggedClips covers the bulk/folder-drag path:
// a mixed selection where one clip is already in the shared tag must publish
// only the clips that just arrived.
func TestBulkAddTagPublishesOnlyNewlyTaggedClips(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	sharedTag := mustCreateTag(t, app, "shared")
	info, err := app.shareManager.StartShare(sharedTag)
	if err != nil {
		t.Fatalf("StartShare: %v", err)
	}

	already := insertSharedTestClip(t, app, "already here")
	if err := app.AddTagToClip(already, sharedTag); err != nil {
		t.Fatalf("AddTagToClip: %v", err)
	}
	app.shareHookWG.Wait()
	if got := ringCount(t, app, info.ID); got != ringRowsPerSmallClip {
		t.Fatalf("setup: ring rows = %d, want %d", got, ringRowsPerSmallClip)
	}

	fresh1 := insertSharedTestClip(t, app, "new one")
	fresh2 := insertSharedTestClip(t, app, "new two")
	if err := app.BulkAddTag([]int64{already, fresh1, fresh2}, sharedTag); err != nil {
		t.Fatalf("BulkAddTag: %v", err)
	}
	app.shareHookWG.Wait()

	// Two arrivals on top of the one from setup — the re-tagged clip must not
	// contribute a second burst.
	want := 3 * ringRowsPerSmallClip
	if got := ringCount(t, app, info.ID); got != want {
		t.Fatalf("ring rows = %d, want %d (3 clips × %d, no re-publish)", got, want, ringRowsPerSmallClip)
	}
}

// TestBulkAddTagUnrelatedTagDoesNotRepublish is the bulk twin of the core
// regression: bulk-tagging shared clips with an unrelated tag publishes nothing.
func TestBulkAddTagUnrelatedTagDoesNotRepublish(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	sharedTag := mustCreateTag(t, app, "shared")
	otherTag := mustCreateTag(t, app, "misc")
	info, err := app.shareManager.StartShare(sharedTag)
	if err != nil {
		t.Fatalf("StartShare: %v", err)
	}

	clipA := insertSharedTestClip(t, app, "one")
	clipB := insertSharedTestClip(t, app, "two")
	if err := app.BulkAddTag([]int64{clipA, clipB}, sharedTag); err != nil {
		t.Fatalf("BulkAddTag(shared): %v", err)
	}
	app.shareHookWG.Wait()
	before := ringCount(t, app, info.ID)
	if before != 2*ringRowsPerSmallClip {
		t.Fatalf("setup: ring rows = %d, want %d", before, 2*ringRowsPerSmallClip)
	}

	if err := app.BulkAddTag([]int64{clipA, clipB}, otherTag); err != nil {
		t.Fatalf("BulkAddTag(misc): %v", err)
	}
	app.shareHookWG.Wait()

	if got := ringCount(t, app, info.ID); got != before {
		t.Fatalf("ring rows = %d after unrelated bulk tag, want %d — clips were re-published", got, before)
	}
}

// TestUploadFilesIntoSharedTagPublishesOnce guards the initial-upload path.
// UploadFiles deliberately owns no share hook of its own — it routes through
// AddTagToClip via autoTagID — so a fresh upload into a shared folder must
// emit exactly one burst, not zero and not two.
func TestUploadFilesIntoSharedTagPublishesOnce(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	sharedTag := mustCreateTag(t, app, "shared")
	info, err := app.shareManager.StartShare(sharedTag)
	if err != nil {
		t.Fatalf("StartShare: %v", err)
	}

	file := FileData{
		Name:        "upload.txt",
		ContentType: "text/plain",
		Data:        base64.StdEncoding.EncodeToString([]byte("uploaded body")),
	}
	if err := app.UploadFiles([]FileData{file}, 0, sharedTag); err != nil {
		t.Fatalf("UploadFiles: %v", err)
	}
	app.shareHookWG.Wait()

	if got := ringCount(t, app, info.ID); got != ringRowsPerSmallClip {
		t.Fatalf("ring rows = %d, want %d (exactly one burst)", got, ringRowsPerSmallClip)
	}
}
