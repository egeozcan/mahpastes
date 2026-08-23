package app

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// seedShareForRestore gives db one tag with one active publication so a backup
// taken from it carries a shares row for the identity policy to act on. The
// share_id is derived from tagID because the column is UNIQUE: seeding two
// publications in one database is what the paused-share cases need.
func seedShareForRestore(t *testing.T, db *sql.DB, tagName string) {
	t.Helper()
	res, err := db.Exec(`INSERT INTO tags (name, color) VALUES (?, '#112233')`, tagName)
	if err != nil {
		t.Fatalf("insert tag %q: %v", tagName, err)
	}
	tagID, _ := res.LastInsertId()
	shareID := []byte("srcshare16XXXXXX")
	shareID[len(shareID)-1] = byte('0' + tagID%10)
	if _, err := db.Exec(
		`INSERT INTO shares (tag_id, symkey, share_id, last_seq, clips_sent, status, created_at)
		 VALUES (?,?,?,0,0,'active',2000000)`,
		tagID, []byte("srckey32bytesXXXXXXXXXXXXXXXXXXX"), shareID,
	); err != nil {
		t.Fatalf("insert share: %v", err)
	}
}

// backupWithShare builds a backup ZIP from a fresh source DB holding one active
// publication and returns its path.
func backupWithShare(t *testing.T, tagName string) string {
	t.Helper()
	srcDB := newBackupTestDB(t)
	seedShareForRestore(t, srcDB, tagName)
	if _, err := srcDB.Exec(
		`INSERT INTO clips (id, content_type, data, filename, created_at, is_archived, name)
		 VALUES (900, 'text/plain', X'6261636B7570', 'from-backup.txt', 1000, 0, 'from-backup')`,
	); err != nil {
		t.Fatalf("insert src clip: %v", err)
	}
	zipPath := filepath.Join(t.TempDir(), "src.zip")
	if err := (&App{db: srcDB}).CreateBackup(zipPath); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	return zipPath
}

// backupWithActiveAndPausedShares builds a backup carrying one active
// publication and one paused one. It is what pins the captured-set semantics of
// an adopting restore: the statuses are taken down before the blanket
// invalidation and put back afterwards, so a paused publication comes back
// paused rather than being reactivated or left invalid.
func backupWithActiveAndPausedShares(t *testing.T) string {
	t.Helper()
	srcDB := newBackupTestDB(t)
	seedShareForRestore(t, srcDB, "policy-tag-active")
	seedShareForRestore(t, srcDB, "policy-tag-paused")
	if _, err := srcDB.Exec(
		`UPDATE shares SET status = 'paused'
		 WHERE tag_id = (SELECT id FROM tags WHERE name = 'policy-tag-paused')`,
	); err != nil {
		t.Fatalf("pause share: %v", err)
	}
	zipPath := filepath.Join(t.TempDir(), "src-mixed.zip")
	if err := (&App{db: srcDB}).CreateBackup(zipPath); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	return zipPath
}

// newIdentityBytes returns a freshly generated, marshalled Ed25519 identity. It
// round-trips through LoadOrCreateIdentity rather than marshalling by hand, so
// the bytes are exactly what an install has on disk — and exactly what a
// restore is now allowed to adopt. Tests that put an identity into a backup and
// expect it to be installed must use these; arbitrary bytes are rejected.
func newIdentityBytes(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), ShareIdentityFile)
	if _, err := LoadOrCreateIdentity(path); err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated identity: %v", err)
	}
	return b
}

// assertNoIdentityLeftovers fails if a failed adoption stranded either of the
// files it writes beside the destination: the staging copy the validation runs
// against, or the temp file extractZipFile renames into that staging path.
func assertNoIdentityLeftovers(t *testing.T, dataDir string) {
	t.Helper()
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		t.Fatalf("read data dir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if name == ShareIdentityFile {
			continue
		}
		if strings.HasPrefix(name, "."+ShareIdentityFile) || strings.HasPrefix(name, ShareIdentityFile+".") {
			t.Errorf("failed identity adoption left %s behind", name)
		}
	}
}

// restoreDataDir sets MAHPASTES_DATA_DIR to a fresh temp dir with a plugins
// subdirectory and returns it.
func restoreDataDir(t *testing.T) string {
	t.Helper()
	dataDir := t.TempDir()
	t.Setenv("MAHPASTES_DATA_DIR", dataDir)
	if err := os.MkdirAll(filepath.Join(dataDir, "plugins"), 0755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	return dataDir
}

// TestRestoreKeepInvalidationFailureRollsBackRestore covers the reason the
// "keep" invalidation moved inside the restore transaction: when the UPDATE
// cannot be applied, the restore must fail as a whole. The old post-commit
// version printed a warning and returned success, leaving the backup's shares
// ACTIVE under the local identity — publishing under a key whose share strings
// point at a different peer.
func TestRestoreKeepInvalidationFailureRollsBackRestore(t *testing.T) {
	dataDir := restoreDataDir(t)
	backupZip := backupWithShare(t, "pub-tag")

	// A pre-existing local identity is what makes "keep" the right policy.
	if err := os.WriteFile(filepath.Join(dataDir, ShareIdentityFile), []byte("local-identity"), 0600); err != nil {
		t.Fatalf("write local identity: %v", err)
	}

	dstDB := newBackupTestDB(t)
	// Marker row: the DELETE pass removes it, so its survival proves rollback.
	if _, err := dstDB.Exec(
		`INSERT INTO clips (id, content_type, data, filename, created_at, is_archived, name)
		 VALUES (1, 'text/plain', X'6C6F63616C', 'local.txt', 1, 0, 'local-only')`,
	); err != nil {
		t.Fatalf("insert marker clip: %v", err)
	}
	if _, err := dstDB.Exec(
		`CREATE TRIGGER fail_share_invalidate BEFORE UPDATE OF status ON shares
		 BEGIN SELECT RAISE(ABORT, 'injected'); END;`,
	); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	dstApp := &App{db: dstDB}
	err := dstApp.RestoreBackup(backupZip, "keep")
	if err == nil {
		t.Fatal("RestoreBackup(keep) returned nil with the share invalidation failing")
	}
	if !strings.Contains(err.Error(), "invalidate restored shares") {
		t.Errorf("error = %v, want it to name the failed share invalidation", err)
	}

	// The whole restore must have rolled back: local row back, backup row absent.
	var localCount, backupCount int
	if err := dstDB.QueryRow(`SELECT COUNT(*) FROM clips WHERE name = 'local-only'`).Scan(&localCount); err != nil {
		t.Fatalf("count local clips: %v", err)
	}
	if localCount != 1 {
		t.Errorf("local clip count = %d, want 1: the failed restore destroyed pre-restore data", localCount)
	}
	if err := dstDB.QueryRow(`SELECT COUNT(*) FROM clips WHERE name = 'from-backup'`).Scan(&backupCount); err != nil {
		t.Fatalf("count restored clips: %v", err)
	}
	if backupCount != 0 {
		t.Errorf("restored clip count = %d, want 0: the failed restore was left partially applied", backupCount)
	}
	var shareCount int
	if err := dstDB.QueryRow(`SELECT COUNT(*) FROM shares`).Scan(&shareCount); err != nil {
		t.Fatalf("count shares: %v", err)
	}
	if shareCount != 0 {
		t.Errorf("shares count = %d, want 0: the backup's shares survived a failed restore", shareCount)
	}
}

// TestRestoreTakeoverWithoutIdentityInvalidatesShares covers a backup that
// carries no peer identity: there is nothing to adopt, so the restored
// publications cannot be valid and must not come back active.
func TestRestoreTakeoverWithoutIdentityInvalidatesShares(t *testing.T) {
	restoreDataDir(t) // no share_identity.key written, so the ZIP has none
	backupZip := backupWithShare(t, "no-identity-tag")

	dstDB := newBackupTestDB(t)
	if err := (&App{db: dstDB}).RestoreBackup(backupZip, "takeover"); err != nil {
		t.Fatalf("RestoreBackup(takeover): %v", err)
	}

	assertAllSharesInvalid(t, dstDB)
}

// TestRestoreTakeoverIdentityExtractionFailureReportsAndInvalidates covers the
// takeover path failing at the filesystem step. The restore transaction is
// already committed by then and cannot be undone, so the shares are marked
// invalid and the failure is reported instead of silently leaving them active
// under a local identity that does not match their share strings.
//
// The identity in the backup is a real key, so the failure is genuinely the
// filesystem's — the install stages and validates fine and dies on the rename
// — rather than the validation catching junk on the way past.
func TestRestoreTakeoverIdentityExtractionFailureReportsAndInvalidates(t *testing.T) {
	dataDir := restoreDataDir(t)
	identityPath := filepath.Join(dataDir, ShareIdentityFile)
	if err := os.WriteFile(identityPath, newIdentityBytes(t), 0600); err != nil {
		t.Fatalf("write src identity: %v", err)
	}

	backupZip := backupWithShare(t, "takeover-tag")

	// Make the install fail: the finished bytes cannot be renamed over a
	// directory.
	if err := os.Remove(identityPath); err != nil {
		t.Fatalf("remove identity file: %v", err)
	}
	if err := os.Mkdir(identityPath, 0755); err != nil {
		t.Fatalf("mkdir over identity path: %v", err)
	}

	dstDB := newBackupTestDB(t)
	err := (&App{db: dstDB}).RestoreBackup(backupZip, "takeover")
	if err == nil {
		t.Fatal("RestoreBackup(takeover) returned nil with the identity extraction failing")
	}
	if !strings.Contains(err.Error(), "identity file") {
		t.Errorf("error = %v, want it to name the identity extraction failure", err)
	}

	// The data restore itself committed before the identity step.
	var backupCount int
	if err := dstDB.QueryRow(`SELECT COUNT(*) FROM clips WHERE name = 'from-backup'`).Scan(&backupCount); err != nil {
		t.Fatalf("count restored clips: %v", err)
	}
	if backupCount != 1 {
		t.Errorf("restored clip count = %d, want 1", backupCount)
	}
	assertNoIdentityLeftovers(t, dataDir)
	assertAllSharesInvalid(t, dstDB)
}

func assertAllSharesInvalid(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.Query(`SELECT status FROM shares`)
	if err != nil {
		t.Fatalf("query shares: %v", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var status string
		if err := rows.Scan(&status); err != nil {
			t.Fatalf("scan status: %v", err)
		}
		count++
		if status != "invalid" {
			t.Errorf("share status = %q, want %q", status, "invalid")
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if count == 0 {
		t.Error("expected at least one restored shares row, got none")
	}
}

// TestRestoreReopensShareHooks verifies the gate is left usable after a restore
// on both exit paths — the suspension is only for the duration of the restore,
// unlike Shutdown's close.
func TestRestoreReopensShareHooks(t *testing.T) {
	dataDir := restoreDataDir(t)
	backupZip := backupWithShare(t, "reopen-tag")

	dstDB := newBackupTestDB(t)
	dstApp := &App{db: dstDB}
	if err := dstApp.RestoreBackup(backupZip, "none"); err != nil {
		t.Fatalf("RestoreBackup(none): %v", err)
	}
	assertHooksAdmitted(t, dstApp, "after a successful restore")

	// Now a restore that fails inside the transaction.
	if err := os.WriteFile(filepath.Join(dataDir, ShareIdentityFile), []byte("local-identity"), 0600); err != nil {
		t.Fatalf("write local identity: %v", err)
	}
	if _, err := dstDB.Exec(
		`CREATE TRIGGER fail_share_invalidate BEFORE UPDATE OF status ON shares
		 BEGIN SELECT RAISE(ABORT, 'injected'); END;`,
	); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}
	if err := dstApp.RestoreBackup(backupZip, "keep"); err == nil {
		t.Fatal("RestoreBackup(keep) returned nil with the share invalidation failing")
	}
	assertHooksAdmitted(t, dstApp, "after a failed restore")
}

func assertHooksAdmitted(t *testing.T, a *App, when string) {
	t.Helper()
	if !a.tryAddShareHook() {
		t.Fatalf("tryAddShareHook refused a hook %s: the gate was left suspended", when)
	}
	a.shareHookWG.Done()
}

// TestShareHookSuspendResumeGate pins the reopenable half of the gate: a
// suspension refuses hooks, and the matching resume admits them again.
func TestShareHookSuspendResumeGate(t *testing.T) {
	a := &App{}

	if !a.tryAddShareHook() {
		t.Fatal("tryAddShareHook refused a hook on a fresh app")
	}
	a.shareHookWG.Done()

	a.suspendShareHooks()
	if a.tryAddShareHook() {
		a.shareHookWG.Done()
		t.Fatal("tryAddShareHook admitted a hook while the gate was suspended")
	}

	// Suspensions nest: one resume must not undo two suspends.
	a.suspendShareHooks()
	a.resumeShareHooks()
	if a.tryAddShareHook() {
		a.shareHookWG.Done()
		t.Fatal("tryAddShareHook admitted a hook with one suspension still outstanding")
	}

	a.resumeShareHooks()
	if !a.tryAddShareHook() {
		t.Fatal("tryAddShareHook still refuses hooks after the last suspension was released")
	}
	a.shareHookWG.Done()
}

// TestShareHookCloseIsNotReopenable pins the one-way half: once Shutdown closes
// the gate, a restore's resume — however it interleaves — must never revive it.
func TestShareHookCloseIsNotReopenable(t *testing.T) {
	a := &App{}
	a.closeShareHooks()
	a.resumeShareHooks()
	if a.tryAddShareHook() {
		a.shareHookWG.Done()
		t.Fatal("tryAddShareHook admitted a hook after closeShareHooks + resumeShareHooks")
	}

	// A restore already in progress when shutdown closes the gate: its deferred
	// resume must not readmit hooks either.
	b := &App{}
	b.suspendShareHooks()
	b.closeShareHooks()
	b.resumeShareHooks()
	if b.tryAddShareHook() {
		b.shareHookWG.Done()
		t.Fatal("tryAddShareHook admitted a hook after a restore's resume raced shutdown's close")
	}
}

// TestQuiesceShareHooksForRestoreDrainsInFlight is the ordering guarantee the
// restore depends on: a hook admitted before the restore starts finishes before
// the quiesce returns, so it can never run against the replaced database or the
// swapped share manager. The gate stays shut until the returned resume runs.
func TestQuiesceShareHooksForRestoreDrainsInFlight(t *testing.T) {
	a := &App{}

	if !a.tryAddShareHook() {
		t.Fatal("tryAddShareHook refused the staged hook")
	}
	release := make(chan struct{})
	var hookFinished atomic.Bool
	go func() {
		defer a.shareHookWG.Done()
		<-release
		hookFinished.Store(true)
	}()

	resumeCh := make(chan func(), 1)
	go func() { resumeCh <- a.quiesceShareHooksForRestore() }()

	// The quiesce must still be draining while the hook is blocked.
	select {
	case <-resumeCh:
		t.Fatal("quiesceShareHooksForRestore returned while a hook was still in flight")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)

	var resume func()
	select {
	case resume = <-resumeCh:
	case <-time.After(shareHookShutdownTimeout + 2*time.Second):
		t.Fatal("quiesceShareHooksForRestore never returned after the hook finished")
	}
	if !hookFinished.Load() {
		t.Fatal("quiesceShareHooksForRestore returned before the in-flight hook completed")
	}

	// Still suspended until the caller's deferred resume runs.
	if a.tryAddShareHook() {
		a.shareHookWG.Done()
		t.Fatal("tryAddShareHook admitted a hook between the drain and the resume")
	}
	resume()
	if !a.tryAddShareHook() {
		t.Fatal("tryAddShareHook still refuses hooks after the restore's resume")
	}
	a.shareHookWG.Done()
}

// TestShareHookAdmissionPinsRestoreDrain is the ordering guarantee behind
// spawnShareHook: by the time the share manager pointer is read, the call is
// already counted by the WaitGroup, so a restore's drain cannot run to
// completion — stopping the manager, replacing the database and rebuilding —
// while a tagging call is still holding that pointer. Capturing the manager
// before admission left exactly that window open, and the hook that came out
// of it published pre-restore state into the restored ring.
//
// shareHookAdmittedHook fires between the admission and the capture, which is
// where the stalled call sits for the duration of the assertion.
func TestShareHookAdmissionPinsRestoreDrain(t *testing.T) {
	a := &App{}

	admitted := make(chan struct{})
	release := make(chan struct{})
	stall := func() {
		close(admitted)
		<-release
	}
	shareHookAdmittedHook.Store(&stall)
	t.Cleanup(func() { shareHookAdmittedHook.Store(nil) })

	spawned := make(chan bool, 1)
	go func() { spawned <- a.spawnShareHook(1, 2, "test") }()
	<-admitted

	resumeCh := make(chan func(), 1)
	go func() { resumeCh <- a.quiesceShareHooksForRestore() }()

	select {
	case <-resumeCh:
		t.Fatal("quiesceShareHooksForRestore drained while a tagging call sat between admission and the manager capture")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)

	if !<-spawned {
		t.Fatal("spawnShareHook reported the gate refused an admitted hook")
	}
	var resume func()
	select {
	case resume = <-resumeCh:
	case <-time.After(shareHookShutdownTimeout + 2*time.Second):
		t.Fatal("quiesceShareHooksForRestore never returned after the stalled call finished")
	}
	resume()
}

// TestSpawnShareHookWithoutManagerReleasesAdmission covers the two paths that
// admit or refuse without ever spawning a goroutine. Either one leaking its
// admission would stall every later drain — a restore, or shutdown — for the
// full timeout and then log a straggler that does not exist.
func TestSpawnShareHookWithoutManagerReleasesAdmission(t *testing.T) {
	a := &App{} // no share manager

	if !a.spawnShareHook(7, 9, "test") {
		t.Fatal("spawnShareHook refused a hook on a fresh app")
	}
	if !a.waitForShareHooks(2 * time.Second) {
		t.Fatal("a nil share manager left its admission in the WaitGroup")
	}

	a.suspendShareHooks()
	if a.spawnShareHook(7, 9, "test") {
		t.Fatal("spawnShareHook admitted a hook while the gate was suspended")
	}
	a.resumeShareHooks()
	if !a.waitForShareHooks(2 * time.Second) {
		t.Fatal("a refused hook left an admission in the WaitGroup")
	}
}

// backupWithoutShares builds a backup ZIP from an empty source database, so it
// carries no shares rows for the identity policy to act on.
func backupWithoutShares(t *testing.T) string {
	t.Helper()
	zipPath := filepath.Join(t.TempDir(), "src-empty.zip")
	if err := (&App{db: newBackupTestDB(t)}).CreateBackup(zipPath); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	return zipPath
}

func assertAllSharesActive(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.Query(`SELECT status FROM shares`)
	if err != nil {
		t.Fatalf("query shares: %v", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var status string
		if err := rows.Scan(&status); err != nil {
			t.Fatalf("scan status: %v", err)
		}
		count++
		if status != "active" {
			t.Errorf("share status = %q, want %q", status, "active")
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if count == 0 {
		t.Error("expected at least one restored shares row, got none")
	}
}

func assertNoShares(t *testing.T, db *sql.DB) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM shares`).Scan(&count); err != nil {
		t.Fatalf("count shares: %v", err)
	}
	if count != 0 {
		t.Errorf("shares count = %d, want 0", count)
	}
}

// TestRestoreIdentityPolicyShareValidity walks the decision matrix documented
// at the invalidation in RestoreBackup: restored publications survive as
// 'active' only when the identity they were created under is the one this
// install will run as afterwards.
//
// The "none" rows are the ones that used to be wrong. The frontend sends
// "none" whenever BackupInspect reports no collision — which includes a backup
// with no identity at all, and includes a real collision that BackupInspect
// failed to see — so "none" never meant "the backup's identity is installed",
// and the cases where nothing is adopted have to invalidate exactly like
// "keep" does.
func TestRestoreIdentityPolicyShareValidity(t *testing.T) {
	// Which identity the case expects to find on disk afterwards. Both are real
	// generated keys: the adopted one has to be, because the install validates
	// it, and making the local one real too keeps the two indistinguishable in
	// every respect except which install they belong to.
	const (
		noIdentity = iota
		srcIdentity
		localIdentity
	)

	cases := []struct {
		name              string
		policy            string
		backupHasIdentity bool
		backupHasShares   bool
		// backupPausedShare adds a second, paused publication to the backup.
		// Requires backupHasShares.
		backupPausedShare bool
		hasLocalIdentity  bool
		wantSharesActive  bool
		// wantShareStatuses, when set, is the exact status histogram expected
		// afterwards — for the mixed backups, where no single status covers
		// every row.
		wantShareStatuses map[string]int
		wantIdentity      int // noIdentity, srcIdentity or localIdentity
	}{
		{
			name:              "keep leaves restored shares invalid",
			policy:            "keep",
			backupHasIdentity: true,
			backupHasShares:   true,
			hasLocalIdentity:  true,
			wantIdentity:      localIdentity,
		},
		{
			name:              "takeover adopts the backup identity and keeps shares active",
			policy:            "takeover",
			backupHasIdentity: true,
			backupHasShares:   true,
			hasLocalIdentity:  true,
			wantSharesActive:  true,
			wantIdentity:      srcIdentity,
		},
		{
			name:            "takeover without a backup identity invalidates",
			policy:          "takeover",
			backupHasShares: true,
		},
		{
			name:              "none adopts the backup identity when none is installed",
			policy:            "none",
			backupHasIdentity: true,
			backupHasShares:   true,
			wantSharesActive:  true,
			wantIdentity:      srcIdentity,
		},
		{
			name:              "none invalidates when a local identity blocks the adoption",
			policy:            "none",
			backupHasIdentity: true,
			backupHasShares:   true,
			hasLocalIdentity:  true,
			wantIdentity:      localIdentity,
		},
		{
			name:             "none invalidates when the backup carries no identity",
			policy:           "none",
			backupHasShares:  true,
			hasLocalIdentity: true,
			wantIdentity:     localIdentity,
		},
		{
			name:            "none invalidates with no identity anywhere",
			policy:          "none",
			backupHasShares: true,
		},
		{
			name:   "none with no shares restores cleanly",
			policy: "none",
		},
		{
			// The captured set is per-row, not "everything the backup had":
			// adoption puts each publication back the way the backup had it.
			// Reactivating the whole table instead would start publishing a
			// share the user had deliberately stopped.
			name:              "takeover reactivates the active share and leaves the paused one paused",
			policy:            "takeover",
			backupHasIdentity: true,
			backupHasShares:   true,
			backupPausedShare: true,
			hasLocalIdentity:  true,
			wantShareStatuses: map[string]int{"active": 1, "paused": 1},
			wantIdentity:      srcIdentity,
		},
		{
			// Nothing is adopted, so a paused row is no safer than an active
			// one: resuming it would publish under an identity its share
			// string does not name. Both go invalid.
			name:              "keep invalidates the paused share along with the active one",
			policy:            "keep",
			backupHasIdentity: true,
			backupHasShares:   true,
			backupPausedShare: true,
			hasLocalIdentity:  true,
			wantShareStatuses: map[string]int{"invalid": 2},
			wantIdentity:      localIdentity,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dataDir := restoreDataDir(t)
			identityPath := filepath.Join(dataDir, ShareIdentityFile)
			srcBytes := newIdentityBytes(t)
			localBytes := newIdentityBytes(t)

			// CreateBackup copies whatever identity is in the data dir, so the
			// source identity has to be in place before the backup is built.
			if tc.backupHasIdentity {
				if err := os.WriteFile(identityPath, srcBytes, 0600); err != nil {
					t.Fatalf("write src identity: %v", err)
				}
			}
			var backupZip string
			switch {
			case tc.backupPausedShare:
				backupZip = backupWithActiveAndPausedShares(t)
			case tc.backupHasShares:
				backupZip = backupWithShare(t, "policy-tag")
			default:
				backupZip = backupWithoutShares(t)
			}

			// Now dress the destination install: the local identity file is
			// what decides whether "none" can adopt anything.
			if err := os.RemoveAll(identityPath); err != nil {
				t.Fatalf("clear identity: %v", err)
			}
			if tc.hasLocalIdentity {
				if err := os.WriteFile(identityPath, localBytes, 0600); err != nil {
					t.Fatalf("write local identity: %v", err)
				}
			}

			dstDB := newBackupTestDB(t)
			// ctx stays nil: the deferred rebuild is a no-op, so no test in
			// this table starts a real libp2p host.
			if err := (&App{db: dstDB}).RestoreBackup(backupZip, tc.policy); err != nil {
				t.Fatalf("RestoreBackup(%s): %v", tc.policy, err)
			}

			switch {
			case tc.wantShareStatuses != nil:
				assertShareStatusCounts(t, dstDB, tc.wantShareStatuses)
			case !tc.backupHasShares:
				assertNoShares(t, dstDB)
			case tc.wantSharesActive:
				assertAllSharesActive(t, dstDB)
			default:
				assertAllSharesInvalid(t, dstDB)
			}

			got, err := os.ReadFile(identityPath)
			if tc.wantIdentity == noIdentity {
				if err == nil {
					t.Errorf("identity file exists with %d bytes, want no file", len(got))
				}
				return
			}
			if err != nil {
				t.Fatalf("read identity file: %v", err)
			}
			want, wantName := srcBytes, "the backup's"
			if tc.wantIdentity == localIdentity {
				want, wantName = localBytes, "the local"
			}
			if !bytes.Equal(got, want) {
				t.Errorf("identity file does not hold %s identity", wantName)
			}
			assertNoIdentityLeftovers(t, dataDir)
		})
	}
}

// shareStatusCounts returns how many shares rows hold each status.
func shareStatusCounts(db *sql.DB) (map[string]int, error) {
	rows, err := db.Query(`SELECT status, COUNT(*) FROM shares GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := make(map[string]int)
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}
		counts[status] = n
	}
	return counts, rows.Err()
}

func assertShareStatusCounts(t *testing.T, db *sql.DB, want map[string]int) {
	t.Helper()
	got, err := shareStatusCounts(db)
	if err != nil {
		t.Fatalf("count share statuses: %v", err)
	}
	if len(got) != len(want) {
		t.Errorf("share statuses = %v, want %v", got, want)
		return
	}
	for status, n := range want {
		if got[status] != n {
			t.Errorf("share statuses = %v, want %v", got, want)
			return
		}
	}
}

// TestRestoreCommitsAdoptingSharesInvalidBeforeIdentityInstall covers the crash
// window between the restore's commit and the identity install.
//
// The restored rows are on disk from the commit onwards, and the identity they
// belong to cannot be installed until after it — the install is a file rename,
// not part of the transaction. Committing the adopting rows as 'active' meant
// that a SIGKILL or a power cut in that window left them active on the next
// start under the identity the machine already had, resuming publications whose
// share strings name a different peer. Every repair for a failed install is an
// in-process error path, so none of them run when the process simply stops.
//
// A real SIGKILL is not something a unit test can take, so the invariant is
// asserted where it lives: at the instant the commit returns, no share is
// active and the old identity is still the one on disk. Everything a crash
// there could leave behind is exactly what the observer sees. The restore then
// finishes normally, which is what the second half checks — the honest resting
// state is only acceptable because the successful path does not stop there.
func TestRestoreCommitsAdoptingSharesInvalidBeforeIdentityInstall(t *testing.T) {
	dataDir := restoreDataDir(t)
	identityPath := filepath.Join(dataDir, ShareIdentityFile)
	srcBytes := newIdentityBytes(t)
	if err := os.WriteFile(identityPath, srcBytes, 0600); err != nil {
		t.Fatalf("write src identity: %v", err)
	}
	backupZip := backupWithShare(t, "crash-window-tag")

	// A different local identity, so "adopted" and "not adopted" are
	// distinguishable by the file's contents alone.
	localBytes := newIdentityBytes(t)
	if err := os.WriteFile(identityPath, localBytes, 0600); err != nil {
		t.Fatalf("write local identity: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "restore.db")
	app := &App{db: newCommitBlockBackupDBAt(t, dbPath)}

	// The observer needs its own connection: the app's pool is capped at one
	// and that one is inside the commit when the hook fires.
	observer, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open observer connection: %v", err)
	}
	defer observer.Close()

	// Only the restore's own commit is interesting; the reactivation commits
	// through the same driver a moment later.
	var (
		once             sync.Once
		observedStatuses map[string]int
		observedIdentity []byte
		observeErr       error
	)
	hook := func() {
		once.Do(func() {
			observedStatuses, observeErr = shareStatusCounts(observer)
			observedIdentity, _ = os.ReadFile(identityPath)
		})
	}
	commitBlockAfterHook.Store(&hook)
	t.Cleanup(func() { commitBlockAfterHook.Store(nil) })

	// Synchronous: the assertions below run on the test goroutine, not in the
	// hook, which only records.
	if err := app.RestoreBackup(backupZip, "takeover"); err != nil {
		t.Fatalf("RestoreBackup(takeover): %v", err)
	}

	if observeErr != nil {
		t.Fatalf("observer query at commit time: %v", observeErr)
	}
	if len(observedStatuses) == 0 {
		t.Fatal("the observer saw no shares at commit time: the restored rows were not committed yet")
	}
	if n := observedStatuses["invalid"]; n != 1 || len(observedStatuses) != 1 {
		t.Errorf("share statuses at commit = %v, want every row invalid: a crash there resumes them under the old identity", observedStatuses)
	}
	if !bytes.Equal(observedIdentity, localBytes) {
		t.Error("the identity was already installed at commit time, so the observation is not of the crash window")
	}

	// The window is the only place the rows are invalid: a restore that runs to
	// completion adopts the identity and puts the publications back.
	assertAllSharesActive(t, app.db)
	got, err := os.ReadFile(identityPath)
	if err != nil {
		t.Fatalf("read identity after restore: %v", err)
	}
	if !bytes.Equal(got, srcBytes) {
		t.Error("identity file does not hold the backup's identity after a successful takeover")
	}
}

// TestRestoreHandlesIdentityBeforeFalliblePluginSetup pins the post-commit
// ordering. The plugin directory setup can fail and return early, and it used
// to run before the identity was handled — leaving the restored shares active,
// and publishing, under the local identity their share strings do not name.
// The identity step now runs immediately after the commit, so a sealed data
// directory is caught there and the shares are invalidated to match.
//
// A read-only data directory fails both steps, and which failure the restore
// reports is what says which one ran first: naming the identity means the
// ordering holds, naming the plugins directory means the old order is back and
// the shares were left active under an identity that does not match them.
//
// The identity step fails here rather than adopting because the extraction is
// atomic — it writes a temp file beside the destination and renames it — and a
// sealed directory has nowhere to put one. That is the intended outcome: the
// local identity survives untouched and the restore says so, where the old
// in-place truncate would have overwritten it.
func TestRestoreHandlesIdentityBeforeFalliblePluginSetup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the read-only directory attribute does not block child creation on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("root ignores directory permissions, so the plugin setup cannot be made to fail this way")
	}
	dataDir := restoreDataDir(t)
	identityPath := filepath.Join(dataDir, ShareIdentityFile)
	srcIdentity := newIdentityBytes(t)
	localIdentity := newIdentityBytes(t)
	if err := os.WriteFile(identityPath, srcIdentity, 0600); err != nil {
		t.Fatalf("write src identity: %v", err)
	}
	backupZip := backupWithShare(t, "ordering-tag")

	if err := os.WriteFile(identityPath, localIdentity, 0600); err != nil {
		t.Fatalf("write local identity: %v", err)
	}

	// Remove the plugins directory and seal the data dir so recreating it
	// fails: that is the one fallible post-commit step that returns early.
	if err := os.RemoveAll(filepath.Join(dataDir, "plugins")); err != nil {
		t.Fatalf("remove plugins dir: %v", err)
	}
	if err := os.Chmod(dataDir, 0555); err != nil {
		t.Fatalf("chmod data dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dataDir, 0755) })

	dstDB := newBackupTestDB(t)
	err := (&App{db: dstDB}).RestoreBackup(backupZip, "takeover")
	if err == nil {
		t.Fatal("RestoreBackup(takeover) returned nil with the plugin directory unwritable")
	}
	if !strings.Contains(err.Error(), "identity file") {
		t.Errorf("error = %v, want it to name the identity step", err)
	}
	if strings.Contains(err.Error(), "plugins directory") {
		t.Errorf("error = %v: the plugin setup ran before the identity step", err)
	}

	// Nothing was adopted, so the shares must not have been left active under
	// an identity that does not match them.
	got, err := os.ReadFile(identityPath)
	if err != nil {
		t.Fatalf("read identity file: %v (the failed adoption destroyed it)", err)
	}
	if !bytes.Equal(got, localIdentity) {
		t.Error("identity file no longer holds the untouched local identity")
	}
	if bytes.Equal(got, srcIdentity) {
		t.Error("the backup identity was adopted from a directory the restore could not write atomically")
	}
	assertAllSharesInvalid(t, dstDB)
}

// TestRestoreReportsShareManagerRebuildFailure covers the failure that used to
// be a warning: the restore succeeded, the rebuild did not, a.shareManager
// stayed nil, and every later publication hook silently no-opped. Sharing was
// dead until the next app start and nothing said so.
func TestRestoreReportsShareManagerRebuildFailure(t *testing.T) {
	dataDir := restoreDataDir(t)
	backupZip := backupWithShare(t, "rebuild-tag")

	// A directory where the identity key belongs: LoadOrCreateIdentity cannot
	// read it and cannot replace it, so NewShareManager fails before it tries
	// to bring a libp2p host up.
	if err := os.Mkdir(filepath.Join(dataDir, ShareIdentityFile), 0755); err != nil {
		t.Fatalf("mkdir over identity path: %v", err)
	}

	dstDB := newBackupTestDB(t)
	dstApp := &App{db: dstDB, ctx: context.Background()}
	err := dstApp.RestoreBackup(backupZip, "keep")
	if err == nil {
		t.Fatal("RestoreBackup(keep) returned nil with the share manager rebuild failing")
	}
	if !strings.Contains(err.Error(), "share manager") || !strings.Contains(err.Error(), "sharing is now disabled") {
		t.Errorf("error = %v, want it to report that sharing is disabled because the share manager could not be restarted", err)
	}
	if dstApp.shareManager != nil {
		t.Error("shareManager is non-nil after a failed rebuild")
	}

	// The restore itself committed — the error is about what happened after.
	var backupCount int
	if err := dstDB.QueryRow(`SELECT COUNT(*) FROM clips WHERE name = 'from-backup'`).Scan(&backupCount); err != nil {
		t.Fatalf("count restored clips: %v", err)
	}
	if backupCount != 1 {
		t.Errorf("restored clip count = %d, want 1", backupCount)
	}
	// The gate is resumed even so: a suspension left outstanding here could
	// never be released, and would gag the manager a later restore brings up.
	assertHooksAdmitted(t, dstApp, "after a share manager rebuild failure")
}

// --- Admission spanning the tag transaction's commit ---------------------
//
// The admission a tagging call takes is what a restore's drain waits on, so
// every instant between the commit that decides there is something to publish
// and the registration of that publication has to be inside it. The tests below
// stop a real tagging call inside tx.Commit() and prove the drain is already
// pinned there.

// commitBlockHook runs inside tx.Commit() on a database opened through the
// commitBlock driver, before the commit is handed to SQLite. Tests set it to
// hold a tagging call in the one window Defect 1 was about.
var commitBlockHook atomic.Pointer[func()]

// commitBlockAfterHook runs after a successful tx.Commit() on the same driver,
// while the committing call is still inside Commit and has done nothing since.
// It is how a test observes the state a crash at that instant would leave on
// disk: the restore's data is committed, and nothing that runs after the commit
// has run yet. Firing it only for a commit that succeeded is deliberate —
// there is no committed state to observe otherwise.
var commitBlockAfterHook atomic.Pointer[func()]

// commitBlockDriverName is registered once per test binary; database/sql panics
// on a duplicate registration.
const commitBlockDriverName = "sqlite-commit-block"

var registerCommitBlockDriver = sync.OnceFunc(func() {
	base, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		panic("open base sqlite driver: " + err.Error())
	}
	d := base.Driver()
	_ = base.Close()
	sql.Register(commitBlockDriverName, commitBlockDriver{base: d})
})

type commitBlockDriver struct{ base driver.Driver }

func (d commitBlockDriver) Open(name string) (driver.Conn, error) {
	c, err := d.base.Open(name)
	if err != nil {
		return nil, err
	}
	return commitBlockConn{Conn: c}, nil
}

// commitBlockConn embeds the interface rather than the concrete connection, so
// the optional Execer/Queryer paths are deliberately not promoted: database/sql
// falls back to prepare+execute, and every statement takes the same route.
type commitBlockConn struct{ driver.Conn }

func (c commitBlockConn) Begin() (driver.Tx, error) {
	tx, err := c.Conn.Begin()
	if err != nil {
		return nil, err
	}
	return commitBlockTx{Tx: tx}, nil
}

type commitBlockTx struct{ driver.Tx }

func (t commitBlockTx) Commit() error {
	if fn := commitBlockHook.Load(); fn != nil {
		(*fn)()
	}
	err := t.Tx.Commit()
	if err == nil {
		if fn := commitBlockAfterHook.Load(); fn != nil {
			(*fn)()
		}
	}
	return err
}

// newTagCommitBlockApp returns an App whose commits run through
// commitBlockHook, seeded with one clip and one tag. deferredFK adds a deferred
// foreign key on clip_tags.clip_id so a tagging call against a clip id that
// does not exist inserts happily and then fails at commit — the one path where
// an admission is taken and the publication never happens.
//
// The app has no share manager, so an admitted hook releases its admission
// without any network work.
func newTagCommitBlockApp(t *testing.T, deferredFK bool) (*App, int64, int64) {
	t.Helper()
	registerCommitBlockDriver()

	dsn := "file:" + filepath.Join(t.TempDir(), "tags.db") + "?_pragma=foreign_keys(1)"
	db, err := sql.Open(commitBlockDriverName, dsn)
	if err != nil {
		t.Fatalf("open commit-block db: %v", err)
	}
	// One connection: the foreign-key pragma is per-connection, and the tests
	// below rely on every statement seeing the same one.
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	// PRIMARY KEY (clip_id, tag_id) mirrors production: it is what makes
	// INSERT OR IGNORE a genuine no-op on a re-add, which is the whole
	// distinction the repeat-add paths turn on.
	clipTags := `CREATE TABLE clip_tags (
		clip_id INTEGER NOT NULL,
		tag_id INTEGER NOT NULL,
		PRIMARY KEY (clip_id, tag_id))`
	if deferredFK {
		clipTags = `CREATE TABLE clip_tags (
			clip_id INTEGER NOT NULL,
			tag_id INTEGER NOT NULL,
			PRIMARY KEY (clip_id, tag_id),
			FOREIGN KEY (clip_id) REFERENCES clips(id) DEFERRABLE INITIALLY DEFERRED)`
	}
	for _, stmt := range []string{
		`CREATE TABLE clips (id INTEGER PRIMARY KEY, name TEXT)`,
		`CREATE TABLE tags (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT UNIQUE, color TEXT)`,
		clipTags,
		`INSERT INTO clips (id, name) VALUES (1, 'clip-one')`,
		`INSERT INTO tags (id, name, color) VALUES (10, 'pub', '#112233')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec %q: %v", stmt, err)
		}
	}
	return &App{db: db}, 1, 10
}

// blockOnce returns a hook function that releases the first caller into the
// returned entered channel and holds it until release is closed. Later calls
// pass straight through, so an unrelated commit cannot deadlock the test.
func blockOnce() (hook func(), entered <-chan struct{}, release chan<- struct{}) {
	enteredCh := make(chan struct{})
	releaseCh := make(chan struct{})
	var once sync.Once
	return func() {
		once.Do(func() {
			close(enteredCh)
			<-releaseCh
		})
	}, enteredCh, releaseCh
}

// assertDrainBlocked starts a restore-style drain and fails if it finishes,
// which would mean the WaitGroup was observed at zero. It returns a channel the
// caller reads the resume from once it has released whatever it was pinning on.
func assertDrainBlocked(t *testing.T, a *App, what string) <-chan func() {
	t.Helper()
	resumeCh := make(chan func(), 1)
	go func() { resumeCh <- a.quiesceShareHooksForRestore() }()
	select {
	case <-resumeCh:
		t.Fatalf("quiesceShareHooksForRestore drained while %s", what)
	case <-time.After(100 * time.Millisecond):
	}
	return resumeCh
}

func awaitResume(t *testing.T, resumeCh <-chan func(), what string) {
	t.Helper()
	select {
	case resume := <-resumeCh:
		resume()
	case <-time.After(shareHookShutdownTimeout + 2*time.Second):
		t.Fatalf("quiesceShareHooksForRestore never returned after %s", what)
	}
}

// TestAddTagToClipAdmissionSpansCommit is Defect 1 for the single-tag path.
// Taking the admission after the commit left the call free to stall in between,
// watch a whole RestoreBackup drain a WaitGroup it had not joined, and then
// publish clip/tag ids that the restore had since repointed at other rows. The
// tagging call here sits inside tx.Commit(); the drain must not get past it.
func TestAddTagToClipAdmissionSpansCommit(t *testing.T) {
	app, clipID, tagID := newTagCommitBlockApp(t, false)

	hook, entered, release := blockOnce()
	commitBlockHook.Store(&hook)
	t.Cleanup(func() { commitBlockHook.Store(nil) })

	tagged := make(chan error, 1)
	go func() { tagged <- app.AddTagToClip(clipID, tagID) }()
	<-entered

	resumeCh := assertDrainBlocked(t, app, "a tagging call sat inside tx.Commit()")

	close(release)
	if err := <-tagged; err != nil {
		t.Fatalf("AddTagToClip: %v", err)
	}
	awaitResume(t, resumeCh, "the tagging call finished")
}

// TestAddTagToClipCommitFailureReleasesAdmission audits the one path that takes
// an admission and then has nothing to publish. Leaking it would stall every
// later drain — a restore, or shutdown — for the full timeout and then blame a
// straggler that never existed.
func TestAddTagToClipCommitFailureReleasesAdmission(t *testing.T) {
	app, _, tagID := newTagCommitBlockApp(t, true)

	// Clip 404 does not exist. The deferred foreign key lets the insert through
	// and fails the commit, which is exactly the shape of the audited path.
	err := app.AddTagToClip(404, tagID)
	if err == nil {
		t.Fatal("AddTagToClip returned nil for a clip id with no clips row")
	}
	if !strings.Contains(err.Error(), "commit") {
		t.Fatalf("error = %v, want the commit failure (the test's injection missed)", err)
	}
	if !app.waitForShareHooks(2 * time.Second) {
		t.Fatal("a failed commit left its admission in the WaitGroup")
	}
}

// TestAddTagToClipRepeatAddNeitherPublishesNorAdmits covers the other no-op
// path: OR IGNORE makes a re-add of a tag the clip already has a no-op, so it
// must not publish, and must not take an admission it would then have to
// remember to release.
func TestAddTagToClipRepeatAddNeitherPublishesNorAdmits(t *testing.T) {
	app, clipID, tagID := newTagCommitBlockApp(t, false)

	var publications atomic.Int64
	seam := func() { publications.Add(1) }
	shareHookAdmittedHook.Store(&seam)
	t.Cleanup(func() { shareHookAdmittedHook.Store(nil) })

	if err := app.AddTagToClip(clipID, tagID); err != nil {
		t.Fatalf("first AddTagToClip: %v", err)
	}
	if err := app.AddTagToClip(clipID, tagID); err != nil {
		t.Fatalf("repeat AddTagToClip: %v", err)
	}

	if got := publications.Load(); got != 1 {
		t.Errorf("publications = %d, want 1: the repeat add re-published a clip followers cannot dedup", got)
	}
	if !app.waitForShareHooks(2 * time.Second) {
		t.Fatal("a repeat add left an admission in the WaitGroup")
	}
}

// TestBulkAddTagOperationAdmissionSpansCommit is Defect 1 for the bulk path.
// The per-clip admissions are taken after the commit, so without an
// operation-level admission held across it the whole batch could commit, stall,
// and publish against a manager a restore had rebuilt in the meantime.
func TestBulkAddTagOperationAdmissionSpansCommit(t *testing.T) {
	app, clipID, tagID := newTagCommitBlockApp(t, false)

	hook, entered, release := blockOnce()
	commitBlockHook.Store(&hook)
	t.Cleanup(func() { commitBlockHook.Store(nil) })

	tagged := make(chan error, 1)
	go func() { tagged <- app.BulkAddTag([]int64{clipID}, tagID) }()
	<-entered

	resumeCh := assertDrainBlocked(t, app, "a bulk tagging call sat inside tx.Commit()")

	close(release)
	if err := <-tagged; err != nil {
		t.Fatalf("BulkAddTag: %v", err)
	}
	awaitResume(t, resumeCh, "the bulk tagging call finished")
}

// TestBulkAddTagAdmissionLeakAudit walks the bulk path's two non-publishing
// exits — a failed commit, and a batch where no clip actually gained the tag —
// and requires the WaitGroup back at zero after each.
func TestBulkAddTagAdmissionLeakAudit(t *testing.T) {
	app, clipID, tagID := newTagCommitBlockApp(t, true)

	if err := app.BulkAddTag([]int64{404}, tagID); err == nil {
		t.Fatal("BulkAddTag returned nil for a clip id with no clips row")
	} else if !strings.Contains(err.Error(), "commit") {
		t.Fatalf("error = %v, want the commit failure (the test's injection missed)", err)
	}
	if !app.waitForShareHooks(2 * time.Second) {
		t.Fatal("a failed bulk commit left its admission in the WaitGroup")
	}

	if err := app.BulkAddTag([]int64{clipID}, tagID); err != nil {
		t.Fatalf("first BulkAddTag: %v", err)
	}
	if !app.waitForShareHooks(2 * time.Second) {
		t.Fatal("a published bulk batch left an admission in the WaitGroup")
	}

	var publications atomic.Int64
	seam := func() { publications.Add(1) }
	shareHookAdmittedHook.Store(&seam)
	t.Cleanup(func() { shareHookAdmittedHook.Store(nil) })

	if err := app.BulkAddTag([]int64{clipID}, tagID); err != nil {
		t.Fatalf("repeat BulkAddTag: %v", err)
	}
	if got := publications.Load(); got != 0 {
		t.Errorf("publications = %d, want 0: a batch that gained no tags published anyway", got)
	}
	if !app.waitForShareHooks(2 * time.Second) {
		t.Fatal("a bulk batch with nothing to publish left an admission in the WaitGroup")
	}
}

// --- Adopting an admission across the suspended gate ---------------------

// TestAdoptShareHookAdmissionIgnoresSuspension pins the difference between the
// two ways in. A caller holding an operation admission is, by construction, the
// thing a restore's drain is currently blocked on, so its children must be able
// to register — while the gate itself goes on refusing everyone else.
func TestAdoptShareHookAdmissionIgnoresSuspension(t *testing.T) {
	app := &App{}

	if !app.tryAddShareHook() {
		t.Fatal("tryAddShareHook refused the operation admission on a fresh gate")
	}
	app.suspendShareHooks()

	// Control: the gate is genuinely shut for anyone arriving from outside.
	if app.tryAddShareHook() {
		app.shareHookWG.Done()
		t.Fatal("tryAddShareHook admitted a hook while the gate was suspended")
	}

	if !app.adoptShareHookAdmission() {
		t.Fatal("adoptShareHookAdmission refused a child of an admitted operation while the gate was suspended: the operation's committed rows would reach no follower")
	}
	app.shareHookWG.Done() // the child finishes

	app.resumeShareHooks()
	app.shareHookWG.Done() // the operation finishes
	if !app.waitForShareHooks(2 * time.Second) {
		t.Fatal("the WaitGroup did not return to zero: an admission was leaked")
	}
}

// TestAdoptShareHookAdmissionRespectsShutdownClose pins the other half of the
// chosen invariant. A suspension is a restore that has to be handed the work;
// a close is a process on its way out, where the drain gives up after
// shareHookShutdownTimeout regardless, so admitting more work buys nothing and
// risks overrunning that bound.
func TestAdoptShareHookAdmissionRespectsShutdownClose(t *testing.T) {
	app := &App{}

	if !app.tryAddShareHook() {
		t.Fatal("tryAddShareHook refused the operation admission on a fresh gate")
	}
	app.closeShareHooks()

	if app.adoptShareHookAdmission() {
		app.shareHookWG.Done()
		t.Fatal("adoptShareHookAdmission registered a child after Shutdown closed the gate")
	}

	app.shareHookWG.Done()
	if !app.waitForShareHooks(2 * time.Second) {
		t.Fatal("the WaitGroup did not return to zero: an admission was leaked")
	}
}

// TestBulkAddTagPublishesChildrenWhileRestoreSuspendsGate is Defect 1
// end-to-end, and the deadlock-shaped bug it describes.
//
// BulkAddTag takes one operation admission before its commit. A restore that
// starts right after that commit suspends the gate and then blocks in its drain
// on that very admission — so when the per-clip loop went back through the gate
// it was refused by a restore that was, at that moment, waiting for the loop to
// finish. Every clip in the batch kept its committed tag row and published
// nothing, and if the restore then failed and rolled back, nothing anywhere
// re-scanned tag associations: those followers never saw those clips again.
func TestBulkAddTagPublishesChildrenWhileRestoreSuspendsGate(t *testing.T) {
	app, clipID, tagID := newTagCommitBlockApp(t, false)
	clipIDs := []int64{clipID, 2, 3}
	for _, id := range clipIDs[1:] {
		if _, err := app.db.Exec(`INSERT INTO clips (id, name) VALUES (?, ?)`, id, "clip"); err != nil {
			t.Fatalf("insert clip %d: %v", id, err)
		}
	}

	var publications atomic.Int64
	seam := func() { publications.Add(1) }
	shareHookAdmittedHook.Store(&seam)
	t.Cleanup(func() { shareHookAdmittedHook.Store(nil) })

	// Drive a restore into its drain while the batch sits inside tx.Commit() —
	// the exact instant the operation admission is held and the per-clip
	// admissions have not been taken yet. The hook does not return until the
	// gate is observably suspended, so the loop below runs against it.
	resumeCh := make(chan func(), 1)
	var once sync.Once
	hook := func() {
		once.Do(func() {
			go func() { resumeCh <- app.quiesceShareHooksForRestore() }()
			for !app.gateSuspendedForTest() {
				time.Sleep(time.Millisecond)
			}
		})
	}
	commitBlockHook.Store(&hook)
	t.Cleanup(func() { commitBlockHook.Store(nil) })

	if err := app.BulkAddTag(clipIDs, tagID); err != nil {
		t.Fatalf("BulkAddTag: %v", err)
	}

	if got, want := publications.Load(), int64(len(clipIDs)); got != want {
		t.Errorf("publications = %d, want %d: the suspended gate refused clips whose tag rows had already committed", got, want)
	}
	// Control: nothing about the transfer reopened the gate for other callers.
	if app.tryAddShareHook() {
		app.shareHookWG.Done()
		t.Error("tryAddShareHook admitted a hook while the restore still had the gate suspended")
	}

	awaitResume(t, resumeCh, "the bulk tagging call finished")
	assertHooksAdmitted(t, app, "after the restore resumed")
}

// TestBulkAddTagChildrenSuppressedByShutdownClose is the same interleaving with
// Shutdown's close instead of a restore's suspension, and pins the invariant
// the other way: children are suppressed, exactly as they were before the
// transfer existed, and no admission is left behind to stall the drain the
// quitting process is about to run.
func TestBulkAddTagChildrenSuppressedByShutdownClose(t *testing.T) {
	app, clipID, tagID := newTagCommitBlockApp(t, false)
	clipIDs := []int64{clipID, 2, 3}
	for _, id := range clipIDs[1:] {
		if _, err := app.db.Exec(`INSERT INTO clips (id, name) VALUES (?, ?)`, id, "clip"); err != nil {
			t.Fatalf("insert clip %d: %v", id, err)
		}
	}

	var publications atomic.Int64
	seam := func() { publications.Add(1) }
	shareHookAdmittedHook.Store(&seam)
	t.Cleanup(func() { shareHookAdmittedHook.Store(nil) })

	var once sync.Once
	hook := func() { once.Do(app.closeShareHooks) }
	commitBlockHook.Store(&hook)
	t.Cleanup(func() { commitBlockHook.Store(nil) })

	// The tag rows still commit: user data never fails because sharing is
	// quiescing.
	if err := app.BulkAddTag(clipIDs, tagID); err != nil {
		t.Fatalf("BulkAddTag: %v", err)
	}
	var tagged int
	if err := app.db.QueryRow(`SELECT COUNT(*) FROM clip_tags WHERE tag_id = ?`, tagID).Scan(&tagged); err != nil {
		t.Fatalf("count clip_tags: %v", err)
	}
	if tagged != len(clipIDs) {
		t.Errorf("tagged clips = %d, want %d: the closed gate failed the tag operation itself", tagged, len(clipIDs))
	}

	if got := publications.Load(); got != 0 {
		t.Errorf("publications = %d, want 0: the shutdown close was supposed to suppress the children", got)
	}
	if !app.waitForShareHooks(2 * time.Second) {
		t.Fatal("a suppressed batch left an admission in the WaitGroup: the shutdown drain would stall on a straggler that never existed")
	}
}

// gateSuspendedForTest reports whether any suspension is outstanding.
func (a *App) gateSuspendedForTest() bool {
	a.shareHookMu.Lock()
	defer a.shareHookMu.Unlock()
	return a.shareHookSuspends > 0
}

// --- Concurrent restores -------------------------------------------------

// TestConcurrentRestoresDoNotInterleave is Defect 2. RestoreBackup is a
// sequence of global swaps, and the desktop dialog and the REST endpoint can
// both start one. Interleaved, restore B walks past the share-manager teardown
// while A holds a.shareManager nil; A's deferred rebuild then installs manager
// A, and B replaces the database and the identity underneath it and installs
// manager B without ever stopping A — leaving manager A live against B's
// database with A's keys.
//
// The probe is the hook gate's suspension count. Every restore suspends the
// gate for its whole body, so while A is stopped inside its commit the count is
// 1 if B is queued on the restore mutex and 2 if B got in.
func TestConcurrentRestoresDoNotInterleave(t *testing.T) {
	restoreDataDir(t)
	backupZip := backupWithShare(t, "serialize-tag")

	// The restore's own commit is what gets stopped, so the app's database has
	// to run through the commit-block driver.
	app := &App{db: newCommitBlockBackupDB(t)}

	hook, entered, release := blockOnce()
	commitBlockHook.Store(&hook)
	t.Cleanup(func() { commitBlockHook.Store(nil) })

	firstDone := make(chan error, 1)
	go func() { firstDone <- app.RestoreBackup(backupZip, "keep") }()
	<-entered

	secondDone := make(chan error, 1)
	go func() { secondDone <- app.RestoreBackup(backupZip, "keep") }()

	// Give the second call more than enough time to reach its quiesce if the
	// mutex is not holding it back: everything before that point is validation
	// and a ZIP open.
	time.Sleep(300 * time.Millisecond)

	app.shareHookMu.Lock()
	suspends := app.shareHookSuspends
	app.shareHookMu.Unlock()
	if suspends != 1 {
		t.Fatalf("share hook suspensions = %d while a restore was mid-commit, want 1: a second restore entered the critical section", suspends)
	}
	select {
	case err := <-secondDone:
		t.Fatalf("the second RestoreBackup finished (err=%v) while the first was still inside its commit", err)
	default:
	}

	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first RestoreBackup: %v", err)
	}
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("second RestoreBackup: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the second RestoreBackup never finished after the first released")
	}

	assertHooksAdmitted(t, app, "after two serialized restores")
}

// newCommitBlockBackupDB is newBackupTestDB over the commit-block driver, so a
// restore's transaction commit can be stopped from a test.
func newCommitBlockBackupDB(t *testing.T) *sql.DB {
	t.Helper()
	return newCommitBlockBackupDBAt(t, filepath.Join(t.TempDir(), "restore.db"))
}

// newCommitBlockBackupDBAt is newCommitBlockBackupDB at a caller-chosen path,
// for the test that has to open a second connection to the same file.
func newCommitBlockBackupDBAt(t *testing.T, path string) *sql.DB {
	t.Helper()
	registerCommitBlockDriver()
	db := openBackupTestDBWithDriver(t, commitBlockDriverName, path)
	db.SetMaxOpenConns(1)
	return db
}

// TestConcurrentRestoresBothComplete runs two restores at once with nothing
// stopped, which is the shape the race detector has something to say about: the
// share manager pointer, the identity file and every table are written by both.
// Serialized, both must finish cleanly and leave the restored rows exactly once.
func TestConcurrentRestoresBothComplete(t *testing.T) {
	restoreDataDir(t)
	backupZip := backupWithShare(t, "parallel-tag")

	app := &App{db: newBackupTestDB(t)}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = app.RestoreBackup(backupZip, "keep")
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("RestoreBackup #%d: %v", i, err)
		}
	}

	// Each restore clears every table before reinserting, so whichever ran last
	// leaves exactly one copy of the backup's rows.
	var clips, shares int
	if err := app.db.QueryRow(`SELECT COUNT(*) FROM clips WHERE name = 'from-backup'`).Scan(&clips); err != nil {
		t.Fatalf("count restored clips: %v", err)
	}
	if clips != 1 {
		t.Errorf("restored clip count = %d, want 1: the two restores interleaved", clips)
	}
	if err := app.db.QueryRow(`SELECT COUNT(*) FROM shares`).Scan(&shares); err != nil {
		t.Fatalf("count shares: %v", err)
	}
	if shares != 1 {
		t.Errorf("restored shares count = %d, want 1: the two restores interleaved", shares)
	}
	assertAllSharesInvalid(t, app.db)
	assertHooksAdmitted(t, app, "after two concurrent restores")
}

// TestBackupAndRestoreShareOneMutex is Defect 2. CreateBackup reads two stores
// that only mean anything together — the database snapshot and the
// share_identity.key it copies in beside it — and used to take no lock at all.
// A takeover restore landing between those two reads replaced both, so the ZIP
// paired database A's active publications with install B's identity. Nothing
// about that backup looks wrong; it validates and restores. It fails much
// later, when someone takeover-restores it and every share string those
// publications ever handed out names a peer id the install no longer has.
//
// The probe is what each operation does first. Both take the mutex before
// anything else, and RestoreBackup suspends the hook gate immediately after —
// so with the mutex held by the test, a suspension count of zero is proof the
// restore never got in, and neither call returning is proof neither ran.
func TestBackupAndRestoreShareOneMutex(t *testing.T) {
	restoreDataDir(t)
	backupZip := backupWithShare(t, "serialize-backup-tag")
	freshZip := filepath.Join(t.TempDir(), "concurrent.zip")

	app := &App{db: newBackupTestDB(t)}
	app.backupRestoreMu.Lock()
	unlocked := false
	unlock := func() {
		if !unlocked {
			unlocked = true
			app.backupRestoreMu.Unlock()
		}
	}
	defer unlock()

	backupDone := make(chan error, 1)
	go func() { backupDone <- app.CreateBackup(freshZip) }()
	restoreDone := make(chan error, 1)
	go func() { restoreDone <- app.RestoreBackup(backupZip, "keep") }()

	// Long enough for either to run to completion if nothing were holding it:
	// both are a few file reads and a handful of small statements.
	time.Sleep(300 * time.Millisecond)

	select {
	case err := <-backupDone:
		t.Fatalf("CreateBackup finished (err=%v) while the backup/restore mutex was held: a restore can still replace the database and the identity mid-backup", err)
	case err := <-restoreDone:
		t.Fatalf("RestoreBackup finished (err=%v) while the backup/restore mutex was held", err)
	default:
	}
	if app.gateSuspendedForTest() {
		t.Fatal("RestoreBackup suspended the share hook gate while the backup/restore mutex was held: it entered its critical section")
	}

	unlock()

	for range 2 {
		select {
		case err := <-backupDone:
			if err != nil {
				t.Fatalf("CreateBackup after the mutex was released: %v", err)
			}
		case err := <-restoreDone:
			if err != nil {
				t.Fatalf("RestoreBackup after the mutex was released: %v", err)
			}
		case <-time.After(30 * time.Second):
			t.Fatal("an operation never finished after the backup/restore mutex was released")
		}
	}

	// Whichever order they ran in, the backup is a real one. Its contents are
	// deliberately not asserted: the release order is not defined, so it may
	// hold either the pre-restore or the restored rows — just never a mix.
	if _, err := ValidateBackup(freshZip); err != nil {
		t.Errorf("the serialized backup is not a valid backup: %v", err)
	}
	assertHooksAdmitted(t, app, "after a serialized backup and restore")
}

// --- Atomic identity extraction ------------------------------------------

// TestRestoreCorruptIdentityLeavesOldIdentityIntact is Defect 3. The identity
// file is the peer id every existing share string names and there is no second
// copy of it, so a corrupt archive entry must not be able to destroy it.
// Extraction used to truncate the destination and copy into it, which left a
// partial key behind when the copy failed partway; the write now lands on a
// temp file and is renamed over the destination only once it is complete.
func TestRestoreCorruptIdentityLeavesOldIdentityIntact(t *testing.T) {
	dataDir := restoreDataDir(t)
	identityPath := filepath.Join(dataDir, ShareIdentityFile)

	// A real key goes into the backup, so the only thing wrong with it is the
	// corruption applied below — the validation that now guards the install has
	// nothing to object to and the failure stays where this test aims it.
	if err := os.WriteFile(identityPath, newIdentityBytes(t), 0600); err != nil {
		t.Fatalf("write backup identity: %v", err)
	}
	backupZip := backupWithShare(t, "corrupt-identity-tag")

	// The identity on disk at restore time. This is what must survive.
	localIdentity := newIdentityBytes(t)
	if err := os.WriteFile(identityPath, localIdentity, 0600); err != nil {
		t.Fatalf("write local identity: %v", err)
	}

	corruptZipEntry(t, backupZip, ShareIdentityFile)

	dstDB := newBackupTestDB(t)
	err := (&App{db: dstDB}).RestoreBackup(backupZip, "takeover")
	if err == nil {
		t.Fatal("RestoreBackup(takeover) returned nil with a corrupt identity entry")
	}
	if !strings.Contains(err.Error(), "identity file") {
		t.Errorf("error = %v, want it to name the identity extraction failure", err)
	}
	if strings.Contains(err.Error(), "validation") {
		t.Errorf("error = %v: the corruption was caught by the content check rather than the extraction, so this no longer tests the atomic write", err)
	}

	got, rerr := os.ReadFile(identityPath)
	if rerr != nil {
		t.Fatalf("read identity after failed restore: %v (the old identity was destroyed)", rerr)
	}
	if !bytes.Equal(got, localIdentity) {
		t.Error("identity file no longer holds the untouched local identity")
	}
	assertNoIdentityLeftovers(t, dataDir)
	assertAllSharesInvalid(t, dstDB)
}

// TestRestoreInvalidIdentityLeavesOldIdentityIntact is Defect 3. The ZIP's CRC
// says nothing about whether an entry is a usable key, so a backup whose
// share_identity.key is intact-but-meaningless — truncated when it was written,
// or tampered with afterwards — used to be installed over the machine's only
// identity without a word. The damage only surfaced afterwards, in the deferred
// share-manager rebuild, and by then the old key was gone: every later app
// start failed the same way, and every share string this install ever handed
// out named a peer id no file on disk could produce.
//
// Validating the bytes before the rename turns that into an ordinary failed
// restore — old identity untouched, restored shares invalidated to match, and
// an error that says why.
func TestRestoreInvalidIdentityLeavesOldIdentityIntact(t *testing.T) {
	dataDir := restoreDataDir(t)
	identityPath := filepath.Join(dataDir, ShareIdentityFile)

	// Real garbage, not corruption: this is what the backup was written with,
	// so the ZIP entry's CRC is perfectly correct and extraction succeeds.
	if err := os.WriteFile(identityPath, []byte("not-a-marshalled-libp2p-private-key"), 0600); err != nil {
		t.Fatalf("write backup identity: %v", err)
	}
	backupZip := backupWithShare(t, "invalid-identity-tag")

	localIdentity := newIdentityBytes(t)
	if err := os.WriteFile(identityPath, localIdentity, 0600); err != nil {
		t.Fatalf("write local identity: %v", err)
	}

	dstDB := newBackupTestDB(t)
	err := (&App{db: dstDB}).RestoreBackup(backupZip, "takeover")
	if err == nil {
		t.Fatal("RestoreBackup(takeover) returned nil for a backup whose identity entry is not a key")
	}
	if !strings.Contains(err.Error(), "identity file") || !strings.Contains(err.Error(), "validation") {
		t.Errorf("error = %v, want it to name the identity validation failure", err)
	}

	got, rerr := os.ReadFile(identityPath)
	if rerr != nil {
		t.Fatalf("read identity after failed restore: %v (the old identity was destroyed)", rerr)
	}
	if !bytes.Equal(got, localIdentity) {
		t.Error("identity file no longer holds the untouched local identity")
	}
	// The surviving file is still loadable, which is the thing that was lost:
	// a restore that installed the garbage left every later start failing here.
	if _, lerr := LoadOrCreateIdentity(identityPath); lerr != nil {
		t.Errorf("LoadOrCreateIdentity on the surviving identity: %v", lerr)
	}
	assertNoIdentityLeftovers(t, dataDir)
	assertAllSharesInvalid(t, dstDB)
}

// corruptZipEntry flips a byte inside the compressed data of one entry, so
// reading it out fails on decompression or the CRC check. The ZIP stays
// structurally valid — the central directory is untouched — which is what makes
// the failure land inside the extraction rather than at open time.
func corruptZipEntry(t *testing.T, zipPath, entryName string) {
	t.Helper()

	r, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	var offset int64 = -1
	var size uint64
	for _, f := range r.File {
		if f.Name == entryName {
			off, oerr := f.DataOffset()
			if oerr != nil {
				t.Fatalf("data offset for %s: %v", entryName, oerr)
			}
			offset, size = off, f.CompressedSize64
			break
		}
	}
	r.Close()
	if offset < 0 {
		t.Fatalf("entry %s not found in %s", entryName, zipPath)
	}
	if size == 0 {
		t.Fatalf("entry %s has no compressed data to corrupt", entryName)
	}

	f, err := os.OpenFile(zipPath, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("reopen zip: %v", err)
	}
	defer f.Close()

	// Late in the stream: an early flip can make the decompressor bail before
	// it has produced anything, while a late one still fails but only after
	// bytes have been written — the case the atomic write exists for.
	pos := offset + int64(size) - 1
	buf := make([]byte, 1)
	if _, err := f.ReadAt(buf, pos); err != nil {
		t.Fatalf("read zip byte: %v", err)
	}
	buf[0] ^= 0xFF
	if _, err := f.WriteAt(buf, pos); err != nil {
		t.Fatalf("write zip byte: %v", err)
	}
}
