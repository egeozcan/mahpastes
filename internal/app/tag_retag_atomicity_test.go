package app

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"
)

// These tests pin the rule that moving a clip within a tag tree is one
// indivisible step. The exclusivity delete and the insert that replaces it used
// to be separate statements on a.db: a failure between them left the clip
// stripped of its old tag and holding no new one, and a swallowed delete error
// left it holding two tags from the same tree.

// clipTagNames returns every tag name on a clip, sorted, so a test can assert
// the whole association set rather than the presence of one row.
func clipTagNames(t *testing.T, a *App, clipID int64) []string {
	t.Helper()
	rows, err := a.db.Query(
		`SELECT t.name FROM clip_tags ct INNER JOIN tags t ON t.id = ct.tag_id WHERE ct.clip_id = ?`,
		clipID)
	if err != nil {
		t.Fatalf("read clip tags: %v", err)
	}
	defer rows.Close()

	names := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan tag name: %v", err)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func assertClipTagNames(t *testing.T, a *App, clipID int64, want ...string) {
	t.Helper()
	got := clipTagNames(t, a, clipID)
	if len(got) != len(want) {
		t.Fatalf("clip %d has tags %v, want %v", clipID, got, want)
	}
	sort.Strings(want)
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("clip %d has tags %v, want %v", clipID, got, want)
		}
	}
}

// blockClipTagWrites aborts every INSERT (or DELETE) touching one clip's
// clip_tags rows, standing in for a mid-operation failure — a constraint, a
// disk error, or the process dying between the two statements.
func blockClipTagWrites(t *testing.T, a *App, op string, clipID int64) {
	t.Helper()
	row := "NEW"
	if op == "DELETE" {
		row = "OLD"
	}
	stmt := fmt.Sprintf(
		`CREATE TRIGGER block_clip_tag_%s BEFORE %s ON clip_tags
		 WHEN %s.clip_id = %d
		 BEGIN SELECT RAISE(ABORT, 'injected'); END;`, op, op, row, clipID)
	if _, err := a.db.Exec(stmt); err != nil {
		t.Fatalf("install %s trigger: %v", op, err)
	}
}

// retagFixture is a clip already sitting at a/old, plus the a/new tag it is
// about to be moved to — the exact shape that makes the move destructive.
func retagFixture(t *testing.T, a *App) (clipID, oldTagID, newTagID int64) {
	t.Helper()
	clipID, err := a.createTestClip("note.txt", "text/plain", []byte("body"))
	if err != nil {
		t.Fatalf("createTestClip: %v", err)
	}
	oldTagID = createTestTag(t, a.db, "a/old", "#000000")
	newTagID = createTestTag(t, a.db, "a/new", "#000000")
	if err := a.AddTagToClip(clipID, oldTagID); err != nil {
		t.Fatalf("AddTagToClip(a/old): %v", err)
	}
	return clipID, oldTagID, newTagID
}

// TestAddTagToClipMovesWithinTreeAtomically is the happy path the failure tests
// below measure against: the clip ends up holding a/new and only a/new.
func TestAddTagToClipMovesWithinTreeAtomically(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	clipID, _, newTagID := retagFixture(t, app)
	other := createTestTag(t, app.db, "x/keep", "#000000")
	if err := app.AddTagToClip(clipID, other); err != nil {
		t.Fatalf("AddTagToClip(x/keep): %v", err)
	}

	if err := app.AddTagToClip(clipID, newTagID); err != nil {
		t.Fatalf("AddTagToClip(a/new): %v", err)
	}
	// a/old is displaced; the unrelated tree is untouched.
	assertClipTagNames(t, app, clipID, "a/new", "x/keep")
}

// TestAddTagToClipRollsBackTreeRemovalWhenInsertFails covers the data-loss
// half: if the insert dies, the clip must keep the tag the removal was about
// to take away, not end up in no folder at all.
func TestAddTagToClipRollsBackTreeRemovalWhenInsertFails(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	clipID, _, newTagID := retagFixture(t, app)
	blockClipTagWrites(t, app, "INSERT", clipID)

	if err := app.AddTagToClip(clipID, newTagID); err == nil {
		t.Fatal("AddTagToClip with a failing insert should have returned an error")
	}
	assertClipTagNames(t, app, clipID, "a/old")
}

// TestAddTagToClipFailsWhenTreeRemovalFails covers the exclusivity half: a
// removal error used to be logged and ignored, letting the insert land and
// leaving the clip in two places in the same tree at once.
func TestAddTagToClipFailsWhenTreeRemovalFails(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	clipID, _, newTagID := retagFixture(t, app)
	blockClipTagWrites(t, app, "DELETE", clipID)

	if err := app.AddTagToClip(clipID, newTagID); err == nil {
		t.Fatal("AddTagToClip should fail when tree exclusivity cannot be enforced")
	}
	assertClipTagNames(t, app, clipID, "a/old")
}

// TestBulkAddTagRollsBackEveryClipWhenOneInsertFails is the bulk twin. The
// exclusivity removals run for the whole batch before any insert, so a failure
// on the last clip used to leave the earlier clips untagged.
func TestBulkAddTagRollsBackEveryClipWhenOneInsertFails(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	oldTagID := createTestTag(t, app.db, "a/old", "#000000")
	newTagID := createTestTag(t, app.db, "a/new", "#000000")

	var clipIDs []int64
	for i := 0; i < 3; i++ {
		id, err := app.createTestClip(fmt.Sprintf("note%d.txt", i), "text/plain", []byte("body"))
		if err != nil {
			t.Fatalf("createTestClip: %v", err)
		}
		if err := app.AddTagToClip(id, oldTagID); err != nil {
			t.Fatalf("AddTagToClip(a/old): %v", err)
		}
		clipIDs = append(clipIDs, id)
	}

	// Fail the last clip in the batch, after the first two have already been
	// stripped of a/old and re-inserted under a/new.
	blockClipTagWrites(t, app, "INSERT", clipIDs[len(clipIDs)-1])

	if err := app.BulkAddTag(clipIDs, newTagID); err == nil {
		t.Fatal("BulkAddTag with a failing insert should have returned an error")
	}
	for _, id := range clipIDs {
		assertClipTagNames(t, app, id, "a/old")
	}
}

// TestBulkAddTagFailsWhenTreeRemovalFails mirrors the single-clip case: an
// unenforceable removal aborts the whole batch instead of being logged away.
func TestBulkAddTagFailsWhenTreeRemovalFails(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	clipID, _, newTagID := retagFixture(t, app)
	blockClipTagWrites(t, app, "DELETE", clipID)

	if err := app.BulkAddTag([]int64{clipID}, newTagID); err == nil {
		t.Fatal("BulkAddTag should fail when tree exclusivity cannot be enforced")
	}
	assertClipTagNames(t, app, clipID, "a/old")
}

// ---------------------------------------------------------------------------
// Shutdown wait for in-flight share-publication hooks
// ---------------------------------------------------------------------------

func TestWaitForShareHooksWaitsForInFlightHook(t *testing.T) {
	app := &App{}
	const hookDuration = 80 * time.Millisecond

	app.shareHookWG.Add(1)
	go func() {
		time.Sleep(hookDuration)
		app.shareHookWG.Done()
	}()

	start := time.Now()
	if !app.waitForShareHooks(5 * time.Second) {
		t.Fatal("waitForShareHooks reported a timeout for a hook that finished in time")
	}
	if elapsed := time.Since(start); elapsed < hookDuration {
		t.Fatalf("returned after %s; it must block until the hook finishes", elapsed)
	}
}

func TestWaitForShareHooksTimesOutOnStuckHook(t *testing.T) {
	app := &App{}
	const timeout = 100 * time.Millisecond

	release := make(chan struct{})
	app.shareHookWG.Add(1)
	go func() {
		<-release
		app.shareHookWG.Done()
	}()
	defer close(release)

	start := time.Now()
	if app.waitForShareHooks(timeout) {
		t.Fatal("waitForShareHooks reported success while a hook was still in flight")
	}
	if elapsed := time.Since(start); elapsed < timeout {
		t.Fatalf("gave up after %s, before the %s timeout", elapsed, timeout)
	}
}

// TestShutdownWaitsForShareHooks is the defect itself: Shutdown used to stop
// the share manager and close the DB with hooks still running, so tagging a
// clip and quitting silently skipped its publication forever.
func TestShutdownWaitsForShareHooks(t *testing.T) {
	app := &App{}
	finished := make(chan struct{})

	app.shareHookWG.Add(1)
	go func() {
		defer app.shareHookWG.Done()
		time.Sleep(80 * time.Millisecond)
		close(finished)
	}()

	app.Shutdown(context.Background())

	select {
	case <-finished:
	default:
		t.Fatal("Shutdown returned while a share-publication hook was still running")
	}
}

// ---------------------------------------------------------------------------
// Share-hook registration gate
// ---------------------------------------------------------------------------

// TestClosedShareHookGateSkipsPublicationButKeepsTag pins the accepted
// trade-off: once shutdown closes the gate, tagging still commits — the user's
// edit is never rejected because the app is quitting — but the publication is
// dropped rather than handed to a share manager that is about to stop.
func TestClosedShareHookGateSkipsPublicationButKeepsTag(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	sharedTag := mustCreateTag(t, app, "shared")
	info, err := app.shareManager.StartShare(sharedTag)
	if err != nil {
		t.Fatalf("StartShare: %v", err)
	}

	// Positive control: with the gate open this exact setup publishes.
	openClip := insertSharedTestClip(t, app, "published")
	if err := app.AddTagToClip(openClip, sharedTag); err != nil {
		t.Fatalf("AddTagToClip(open gate): %v", err)
	}
	app.shareHookWG.Wait()
	if got := ringCount(t, app, info.ID); got != ringRowsPerSmallClip {
		t.Fatalf("control: ring rows = %d, want %d", got, ringRowsPerSmallClip)
	}

	app.closeShareHooks()

	closedClip := insertSharedTestClip(t, app, "suppressed")
	if err := app.AddTagToClip(closedClip, sharedTag); err != nil {
		t.Fatalf("AddTagToClip must still succeed with the gate closed: %v", err)
	}

	// No goroutine was registered, so Wait returns without blocking and the
	// ring is final — there is nothing in flight that could still append.
	app.shareHookWG.Wait()
	assertClipTagNames(t, app, closedClip, "shared")
	if got := ringCount(t, app, info.ID); got != ringRowsPerSmallClip {
		t.Fatalf("ring rows = %d, want %d — the closed gate let a publication through",
			got, ringRowsPerSmallClip)
	}
}

// TestBulkAddTagClosedShareHookGateSkipsPublication covers the second hook
// site: BulkAddTag spawns one hook per newly tagged clip, and every one of
// them has to go through the same gate.
func TestBulkAddTagClosedShareHookGateSkipsPublication(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	sharedTag := mustCreateTag(t, app, "shared")
	info, err := app.shareManager.StartShare(sharedTag)
	if err != nil {
		t.Fatalf("StartShare: %v", err)
	}

	clipIDs := []int64{
		insertSharedTestClip(t, app, "one"),
		insertSharedTestClip(t, app, "two"),
		insertSharedTestClip(t, app, "three"),
	}

	app.closeShareHooks()

	if err := app.BulkAddTag(clipIDs, sharedTag); err != nil {
		t.Fatalf("BulkAddTag must still succeed with the gate closed: %v", err)
	}
	app.shareHookWG.Wait()

	for _, clipID := range clipIDs {
		assertClipTagNames(t, app, clipID, "shared")
	}
	if got := ringCount(t, app, info.ID); got != 0 {
		t.Fatalf("ring rows = %d, want 0 — the closed gate let a publication through", got)
	}
}

// TestShareHookGateRaceWithShutdownClose is the race the gate exists for: a
// tag operation already past its permission checks commits and reaches its
// hook spawn exactly as shutdown starts waiting. Without the mutex that is an
// Add-from-zero concurrent with a Wait, which the race detector flags and
// WaitGroup documents as invalid. Run under -race.
func TestShareHookGateRaceWithShutdownClose(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	sharedTag := mustCreateTag(t, app, "shared")
	if _, err := app.shareManager.StartShare(sharedTag); err != nil {
		t.Fatalf("StartShare: %v", err)
	}

	// One clip per worker: concurrent tagging of distinct clips avoids the
	// tree-exclusivity delete/insert churn fighting over the same rows, which
	// would test SQLite contention rather than the gate.
	const workers = 8
	clipIDs := make([]int64, workers)
	for i := range clipIDs {
		clipIDs[i] = insertSharedTestClip(t, app, fmt.Sprintf("clip-%d", i))
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(clipID int64) {
			defer wg.Done()
			<-start
			// Errors are fine here (a busy DB is not what's under test); a
			// panic or a race report is not.
			_ = app.AddTagToClip(clipID, sharedTag)
		}(clipIDs[i])
	}

	close(start)
	app.closeShareHooks()

	if !app.waitForShareHooks(5 * time.Second) {
		t.Fatal("hooks admitted before the gate closed never drained")
	}
	wg.Wait()

	// The gate is one-way, so nothing can start after the drain: this is the
	// direct statement of "no hook is in flight and none can become one".
	if app.tryAddShareHook() {
		app.shareHookWG.Done()
		t.Fatal("tryAddShareHook admitted a hook after the gate was closed")
	}
}
