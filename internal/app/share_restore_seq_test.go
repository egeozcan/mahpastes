package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newRestorableShareDB is newBackupTestDB widened to what a running
// ShareManager reads, so one database can go through CreateBackup,
// RestoreBackup and then serve followers: emission reads clips.metadata, the
// follower-side assembler writes clips.content_hash, and ResumeAll reads
// follows.paused. The tag_id index mirrors production's one-share-per-tag rule.
func newRestorableShareDB(t *testing.T) *sql.DB {
	t.Helper()
	db := newBackupTestDB(t)
	widenForShareManager(t, db)
	return db
}

// widenForShareManager applies newRestorableShareDB's widening to a database
// opened some other way.
func widenForShareManager(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, s := range []string{
		`ALTER TABLE clips ADD COLUMN metadata TEXT DEFAULT '{}'`,
		`ALTER TABLE clips ADD COLUMN content_hash TEXT DEFAULT ''`,
		`CREATE UNIQUE INDEX idx_shares_tag_id ON shares(tag_id)`,
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("widen schema: %v\nSQL: %s", err, s)
		}
	}
}

// seedRestorablePublication inserts a tag and one publication with a real
// symkey (the follower has to be able to decrypt what it publishes) at
// lastSeq, plus one whole clip's ring rows ending at lastSeq when withRing is
// set. It returns the publication id and its symkey.
func seedRestorablePublication(t *testing.T, db *sql.DB, tagName, status string, lastSeq int64, withRing bool) (int64, []byte) {
	t.Helper()
	res, err := db.Exec(`INSERT INTO tags (name, color) VALUES (?, '#778899')`, tagName)
	if err != nil {
		t.Fatalf("insert tag %q: %v", tagName, err)
	}
	tagID, _ := res.LastInsertId()
	symkey := make([]byte, 32)
	if _, err := rand.Read(symkey); err != nil {
		t.Fatal(err)
	}
	res, err = db.Exec(
		`INSERT INTO shares (tag_id, symkey, share_id, last_seq, clips_sent, status, created_at) VALUES (?,?,?,?,0,?,?)`,
		tagID, symkey, DeriveShareID(symkey), lastSeq, status, time.Now().Unix(),
	)
	if err != nil {
		t.Fatalf("insert share: %v", err)
	}
	pubID, _ := res.LastInsertId()
	if withRing {
		// Fresh timestamps: this history is inside the ring TTL, so it is
		// exactly the kind a handshake would still replay if it survived.
		now := time.Now().Unix()
		for i, kind := range []string{KindClipStart, KindClipChunk, KindClipEnd} {
			if _, err := db.Exec(
				`INSERT INTO share_ring (publication_id, seq, kind, envelope_bytes, ts) VALUES (?,?,?,?,?)`,
				pubID, lastSeq-2+int64(i), kind, []byte("backup-history"), now,
			); err != nil {
				t.Fatalf("insert ring row: %v", err)
			}
		}
	}
	return pubID, symkey
}

// writeIdentity puts a freshly generated identity into dataDir and returns its
// bytes.
func writeIdentity(t *testing.T, dataDir string) []byte {
	t.Helper()
	b := newIdentityBytes(t)
	if err := os.WriteFile(filepath.Join(dataDir, ShareIdentityFile), b, 0600); err != nil {
		t.Fatalf("write identity: %v", err)
	}
	return b
}

func shareSeqAndStatus(t *testing.T, db *sql.DB, pubID int64) (uint64, string) {
	t.Helper()
	var seq int64
	var status string
	if err := db.QueryRow(`SELECT last_seq, status FROM shares WHERE id = ?`, pubID).Scan(&seq, &status); err != nil {
		t.Fatalf("read share %d: %v", pubID, err)
	}
	if seq < 0 {
		t.Fatalf("share %d last_seq %d is negative — the seq overflowed int64", pubID, seq)
	}
	return uint64(seq), status
}

func ringRowCount(t *testing.T, db *sql.DB, pubID int64) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM share_ring WHERE publication_id = ?`, pubID).Scan(&n); err != nil {
		t.Fatalf("count ring rows: %v", err)
	}
	return n
}

// TestRestoredPublisherDeliversPostRestoreClipsToFollowerAheadOfBackup is the
// end-to-end regression for clips lost after a disaster-recovery restore.
//
// The backup was taken at last_seq=100. The original machine kept publishing,
// its followers kept consuming past 100, and then it died. The backup is
// restored onto a fresh install ("none" adopts the identity, so the
// publication stays active) and the user publishes clips before the follower
// reconnects. Both shapes used to lose them:
//
//   - follower ahead of the restored head: the publisher saw pubLastSeq <
//     since, rewound the follower to pubLastSeq and sent no rows — the clips
//     sat in the ring at seqs below the follower's since and were skipped.
//   - restored head overtakes the follower: no rewind at all; the follower was
//     sent only seq > since, so every clip at (100, since] was dropped, and the
//     one straddling since lost its clip_start and was gapped over.
//
// Nothing is mocked between the backup and the wire: a real CreateBackup, a
// real RestoreBackup, and a real ShareManager on the restored database
// serving a real follower over libp2p.
func TestRestoredPublisherDeliversPostRestoreClipsToFollowerAheadOfBackup(t *testing.T) {
	cases := []struct {
		name          string
		followerSince int64
		clips         int
	}{
		{
			// Seqs 101..103 against a follower at 150: the rewind case.
			name:          "follower ahead of the restored head",
			followerSince: 150,
			clips:         1,
		},
		{
			// Seqs 101..109 against a follower at 105: clip 1 is below since,
			// clip 2's clip_start is, and only clip 3 used to arrive.
			name:          "restored head overtakes the follower before it reconnects",
			followerSince: 105,
			clips:         3,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			// --- The original machine, as captured by its last backup. ---
			srcDir := restoreDataDir(t)
			writeIdentity(t, srcDir)
			srcDB := newRestorableShareDB(t)
			pubID, symkey := seedRestorablePublication(t, srcDB, "recipes", "active", 100, true)
			backupZip := filepath.Join(t.TempDir(), "backup.zip")
			if err := (&App{db: srcDB}).CreateBackup(backupZip); err != nil {
				t.Fatalf("CreateBackup: %v", err)
			}

			// --- Disaster recovery onto a fresh install. ---
			dstDir := restoreDataDir(t)
			dstDB := newRestorableShareDB(t)
			if err := (&App{db: dstDB}).RestoreBackup(backupZip, "none"); err != nil {
				t.Fatalf("RestoreBackup(none): %v", err)
			}
			if _, status := shareSeqAndStatus(t, dstDB, pubID); status != "active" {
				t.Fatalf("restored share status %q, want active — the identity was not adopted, so this is not the case under test", status)
			}

			// What rebuildShareManagerAfterRestore does, minus the App.
			pubM, err := NewShareManager(ctx, dstDB, dstDir)
			if err != nil {
				t.Fatal(err)
			}
			defer pubM.Stop()
			if err := pubM.ResumeAll(); err != nil {
				t.Fatal(err)
			}
			var tagID int64
			if err := dstDB.QueryRow(`SELECT tag_id FROM shares WHERE id = ?`, pubID).Scan(&tagID); err != nil {
				t.Fatal(err)
			}

			// --- The user publishes while the follower is still offline. ---
			for i := 0; i < tc.clips; i++ {
				r, err := dstDB.Exec(
					`INSERT INTO clips (content_type, data, filename, metadata) VALUES ('text/plain', ?, ?, '{}')`,
					fmt.Sprintf("post-restore-%d", i), fmt.Sprintf("p%d.txt", i),
				)
				if err != nil {
					t.Fatal(err)
				}
				clipID, _ := r.LastInsertId()
				if _, err := dstDB.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, ?)`, clipID, tagID); err != nil {
					t.Fatal(err)
				}
				if err := pubM.OnClipCreated(clipID, []int64{tagID}); err != nil {
					t.Fatalf("OnClipCreated: %v", err)
				}
			}
			pubHead, _ := shareSeqAndStatus(t, dstDB, pubID)

			// --- A follower that consumed past the backup reconnects. ---
			fDB := newTestDB(t)
			if _, err := fDB.Exec(`INSERT INTO tags (id, name, color) VALUES (99, 'inbox', '#aaa')`); err != nil {
				t.Fatal(err)
			}
			if _, err := fDB.Exec(
				`INSERT INTO follows (id, remote_peer_id, symkey, local_tag_id, last_seq, created_at) VALUES (1, ?, ?, 99, ?, ?)`,
				pubM.Host().ID().String(), symkey, tc.followerSince, time.Now().Unix(),
			); err != nil {
				t.Fatal(err)
			}
			fM, err := NewShareManager(ctx, fDB, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer fM.Stop()
			fM.Host().Peerstore().AddAddrs(pubM.Host().ID(), pubM.Host().Addrs(), time.Hour)
			if err := fM.ResumeAll(); err != nil {
				t.Fatal(err)
			}

			deadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(deadline) {
				var got int
				if err := fDB.QueryRow(`SELECT COUNT(*) FROM clips WHERE CAST(data AS TEXT) LIKE 'post-restore-%'`).Scan(&got); err != nil {
					t.Fatal(err)
				}
				var followSeq int64
				if err := fDB.QueryRow(`SELECT last_seq FROM follows WHERE id = 1`).Scan(&followSeq); err != nil {
					t.Fatal(err)
				}
				if got == tc.clips && uint64(followSeq) == pubHead {
					return
				}
				// The follower standing on the publisher's head with clips
				// missing is the loss itself: nothing will ever resend them.
				if uint64(followSeq) == pubHead && got < tc.clips {
					t.Fatalf("follower reached the publisher's head (seq %d) holding %d of %d post-restore clips — the rest were skipped, not delivered", pubHead, got, tc.clips)
				}
				time.Sleep(50 * time.Millisecond)
			}
			var got int
			_ = fDB.QueryRow(`SELECT COUNT(*) FROM clips WHERE CAST(data AS TEXT) LIKE 'post-restore-%'`).Scan(&got)
			t.Fatalf("follower received %d of %d post-restore clips before the deadline", got, tc.clips)
		})
	}
}

// publishTaggedClip stores a text clip under tagID and hands it to m the way
// the upload path does, so m emits it to tagID's publication.
func publishTaggedClip(t *testing.T, db *sql.DB, m *ShareManager, tagID int64, body string) {
	t.Helper()
	r, err := db.Exec(
		`INSERT INTO clips (content_type, data, filename, metadata) VALUES ('text/plain', ?, ?, '{}')`,
		body, body+".txt",
	)
	if err != nil {
		t.Fatal(err)
	}
	clipID, _ := r.LastInsertId()
	if _, err := db.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, ?)`, clipID, tagID); err != nil {
		t.Fatal(err)
	}
	if err := m.OnClipCreated(clipID, []int64{tagID}); err != nil {
		t.Fatalf("OnClipCreated: %v", err)
	}
}

// waitForFollowerAtHead waits until the follower holds a clip with each of
// bodies and its durable boundary stands on the publication's head. It fails
// at once if the boundary reaches the head with a clip missing: nothing will
// ever resend it, so that is the loss itself rather than slowness.
func waitForFollowerAtHead(t *testing.T, fDB *sql.DB, followID int64, pubDB *sql.DB, pubID int64, bodies ...string) {
	t.Helper()
	missing := func() []string {
		var out []string
		for _, b := range bodies {
			var n int
			if err := fDB.QueryRow(`SELECT COUNT(*) FROM clips WHERE CAST(data AS TEXT) = ?`, b).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n == 0 {
				out = append(out, b)
			}
		}
		return out
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		// The boundary is read first. A clip and the boundary past it commit
		// together, so whatever the boundary covers is already readable.
		var followSeq int64
		if err := fDB.QueryRow(`SELECT last_seq FROM follows WHERE id = ?`, followID).Scan(&followSeq); err != nil {
			t.Fatal(err)
		}
		gone := missing()
		pubHead, _ := shareSeqAndStatus(t, pubDB, pubID)
		if uint64(followSeq) == pubHead {
			if len(gone) > 0 {
				t.Fatalf("follower reached the publisher's head (seq %d) without %v — skipped, not delivered", pubHead, gone)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	var followSeq int64
	_ = fDB.QueryRow(`SELECT last_seq FROM follows WHERE id = ?`, followID).Scan(&followSeq)
	pubHead, _ := shareSeqAndStatus(t, pubDB, pubID)
	t.Fatalf("follower at seq %d, publisher at %d, still missing %v at the deadline", followSeq, pubHead, missing())
}

// TestRestoredPublisherDeliversBackupBacklogToLaggingFollower covers the
// follower on the other side of the backup point: one that was BEHIND the
// backup when it was taken. That is the ordinary follower when a share moves
// to a new machine, or rolls back, within the ring's hour: a laptop asleep
// while the original machine published, a backup restored minutes later.
//
// The restore resumes the publication a stride above the backup's head. It
// used to purge the backup's ring along with that, so this follower's
// handshake found nothing to replay and was gapped straight to the new head,
// past a clip the backup still held. The ring is kept now. Catch-up stops at
// the hole the stride leaves and drain-closes, the follower reconnects from
// the backup's head, and the head gap carries it over.
//
// Both orders are covered: a clip published after the restore can land in the
// same catch-up window as the backlog, or reach the follower live once it has
// caught up. The stream must cross the hole by a gap either way. A follower
// that only got across by failing to decrypt and reconnecting fails the test.
func TestRestoredPublisherDeliversBackupBacklogToLaggingFollower(t *testing.T) {
	cases := []struct {
		name         string
		publishFirst bool
	}{
		{name: "post-restore clip published before the follower reconnects", publishFirst: true},
		{name: "post-restore clip published live after the follower caught up"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			// --- The original machine publishes a clip the follower misses. ---
			srcDir := restoreDataDir(t)
			writeIdentity(t, srcDir)
			srcDB := newRestorableShareDB(t)
			pubID, symkey := seedRestorablePublication(t, srcDB, "recipes", "active", 0, false)
			var tagID int64
			if err := srcDB.QueryRow(`SELECT tag_id FROM shares WHERE id = ?`, pubID).Scan(&tagID); err != nil {
				t.Fatal(err)
			}
			srcM, err := NewShareManager(ctx, srcDB, srcDir)
			if err != nil {
				t.Fatal(err)
			}
			if err := srcM.ResumeAll(); err != nil {
				t.Fatal(err)
			}
			publishTaggedClip(t, srcDB, srcM, tagID, "backlog-clip")
			srcM.Stop()
			if seq, _ := shareSeqAndStatus(t, srcDB, pubID); seq != 3 || ringRowCount(t, srcDB, pubID) != 3 {
				t.Fatalf("source publication at last_seq %d with %d ring rows, want one clip at 1..3", seq, ringRowCount(t, srcDB, pubID))
			}
			backupZip := filepath.Join(t.TempDir(), "backup.zip")
			if err := (&App{db: srcDB}).CreateBackup(backupZip); err != nil {
				t.Fatalf("CreateBackup: %v", err)
			}

			// --- Restored onto a fresh install, inside the ring's TTL. ---
			dstDir := restoreDataDir(t)
			dstDB := newRestorableShareDB(t)
			if err := (&App{db: dstDB}).RestoreBackup(backupZip, "none"); err != nil {
				t.Fatalf("RestoreBackup(none): %v", err)
			}
			if _, status := shareSeqAndStatus(t, dstDB, pubID); status != "active" {
				t.Fatalf("restored share status %q, want active — the identity was not adopted, so this is not the case under test", status)
			}
			pubM, err := NewShareManager(ctx, dstDB, dstDir)
			if err != nil {
				t.Fatal(err)
			}
			defer pubM.Stop()
			if err := pubM.ResumeAll(); err != nil {
				t.Fatal(err)
			}
			if tc.publishFirst {
				publishTaggedClip(t, dstDB, pubM, tagID, "post-restore-clip")
			}

			// --- The follower that never saw the backlog clip reconnects. ---
			fDB := newTestDB(t)
			if _, err := fDB.Exec(`INSERT INTO tags (id, name, color) VALUES (99, 'inbox', '#aaa')`); err != nil {
				t.Fatal(err)
			}
			if _, err := fDB.Exec(
				`INSERT INTO follows (id, remote_peer_id, symkey, local_tag_id, last_seq, created_at) VALUES (1, ?, ?, 99, 0, ?)`,
				pubM.Host().ID().String(), symkey, time.Now().Unix(),
			); err != nil {
				t.Fatal(err)
			}
			fM, err := NewShareManager(ctx, fDB, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer fM.Stop()
			fM.Host().Peerstore().AddAddrs(pubM.Host().ID(), pubM.Host().Addrs(), time.Hour)
			if err := fM.ResumeAll(); err != nil {
				t.Fatal(err)
			}

			if tc.publishFirst {
				waitForFollowerAtHead(t, fDB, 1, dstDB, pubID, "backlog-clip", "post-restore-clip")
			} else {
				waitForFollowerAtHead(t, fDB, 1, dstDB, pubID, "backlog-clip")
				publishTaggedClip(t, dstDB, pubM, tagID, "post-restore-clip")
				waitForFollowerAtHead(t, fDB, 1, dstDB, pubID, "backlog-clip", "post-restore-clip")
			}
			for _, e := range fM.GetShareLogs(1, 0) {
				if strings.Contains(e.Message, "decrypt failed") {
					t.Fatalf("follower desynced on the way (%q): it crossed the seq hole by failing, not by a gap", e.Message)
				}
			}
		})
	}
}

// TestRestoreWaitsOutConcurrentWriter covers the restore transaction under
// another writer: a REST upload, a plugin's storage write, the cleanup job.
// An adopting restore reads this install's share heads before it deletes
// anything, so from a plain deferred Begin its first write had to upgrade a
// read snapshot, which SQLite refuses at once while another connection holds
// the write lock. It failed in microseconds with SQLITE_BUSY where keep, whose
// first statement is the DELETE, waited the writer out. See beginWriteTx.
func TestRestoreWaitsOutConcurrentWriter(t *testing.T) {
	cases := []struct {
		policy           string
		hasLocalIdentity bool
		wantStatus       string
	}{
		{policy: "none", wantStatus: "active"},
		{policy: "takeover", hasLocalIdentity: true, wantStatus: "active"},
		{policy: "keep", hasLocalIdentity: true, wantStatus: "invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.policy, func(t *testing.T) {
			srcDir := restoreDataDir(t)
			writeIdentity(t, srcDir)
			srcDB := newRestorableShareDB(t)
			pubID, _ := seedRestorablePublication(t, srcDB, "recipes", "active", 100, true)
			backupZip := filepath.Join(t.TempDir(), "backup.zip")
			if err := (&App{db: srcDB}).CreateBackup(backupZip); err != nil {
				t.Fatalf("CreateBackup: %v", err)
			}

			dstDir := restoreDataDir(t)
			if tc.hasLocalIdentity {
				writeIdentity(t, dstDir)
			}
			// initDB's pragmas and an unrestricted pool, so the lock holder
			// gets a connection of its own.
			dsn := filepath.Join(t.TempDir(), "restore.db") +
				"?_pragma=busy_timeout%3D5000&_pragma=journal_mode%3Dwal&_pragma=foreign_keys%3Don"
			dstDB := openBackupTestDB(t, dsn)
			widenForShareManager(t, dstDB)

			underConcurrentWriter(t, dstDB, func() error {
				return (&App{db: dstDB}).RestoreBackup(backupZip, tc.policy)
			})

			if _, status := shareSeqAndStatus(t, dstDB, pubID); status != tc.wantStatus {
				t.Fatalf("restored share status %q, want %q", status, tc.wantStatus)
			}
		})
	}
}

// TestRestoreAdvancesAdoptedShareSeqsPastBackupHistory pins the restore half
// of the fix: a publication that comes back usable resumes far above the
// backup's head with the backup's ring history kept, and one that comes back
// invalid is left exactly as the backup had it.
//
// The stride is what makes post-restore seqs disjoint from anything the
// original machine handed out after the backup was taken. The history stays
// because a follower that was behind the backup still needs it; catch-up
// stops at the hole between it and the new head and gaps over it on the next
// connection (TestRestoredPublisherDeliversBackupBacklogToLaggingFollower).
//
// A paused publication gets the same treatment as an active one — resuming it
// later reconnects the same followers.
func TestRestoreAdvancesAdoptedShareSeqsPastBackupHistory(t *testing.T) {
	const minStride = uint64(1) << 32

	cases := []struct {
		name             string
		policy           string
		hasLocalIdentity bool
		adopts           bool
	}{
		{name: "none on a fresh install adopts", policy: "none", adopts: true},
		{name: "takeover over another identity adopts", policy: "takeover", hasLocalIdentity: true, adopts: true},
		{name: "keep leaves the invalidated rows alone", policy: "keep", hasLocalIdentity: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srcDir := restoreDataDir(t)
			writeIdentity(t, srcDir)
			srcDB := newRestorableShareDB(t)
			activeID, _ := seedRestorablePublication(t, srcDB, "active-tag", "active", 100, true)
			pausedID, _ := seedRestorablePublication(t, srcDB, "paused-tag", "paused", 40, true)
			backupZip := filepath.Join(t.TempDir(), "backup.zip")
			if err := (&App{db: srcDB}).CreateBackup(backupZip); err != nil {
				t.Fatalf("CreateBackup: %v", err)
			}

			dstDir := restoreDataDir(t)
			if tc.hasLocalIdentity {
				writeIdentity(t, dstDir)
			}
			dstDB := newRestorableShareDB(t)
			if err := (&App{db: dstDB}).RestoreBackup(backupZip, tc.policy); err != nil {
				t.Fatalf("RestoreBackup(%s): %v", tc.policy, err)
			}

			for _, pub := range []struct {
				id         int64
				backupHead uint64
				wantStatus string
			}{
				{activeID, 100, "active"},
				{pausedID, 40, "paused"},
			} {
				seq, status := shareSeqAndStatus(t, dstDB, pub.id)
				ring := ringRowCount(t, dstDB, pub.id)
				if !tc.adopts {
					if status != "invalid" {
						t.Errorf("share %d status %q, want invalid", pub.id, status)
					}
					if seq != pub.backupHead || ring != 3 {
						t.Errorf("invalidated share %d: last_seq=%d ring rows=%d, want the backup's %d and 3 — it can never publish again, so nothing about it should move", pub.id, seq, ring, pub.backupHead)
					}
					continue
				}
				if status != pub.wantStatus {
					t.Errorf("share %d status %q, want %q", pub.id, status, pub.wantStatus)
				}
				if seq < pub.backupHead+minStride {
					t.Errorf("share %d resumes at last_seq=%d, want at least %d above the backup's head %d — the original machine's followers may already be past it", pub.id, seq, minStride, pub.backupHead)
				}
				if ring != 3 {
					t.Errorf("share %d kept %d ring rows of backup history, want the backup's 3 — a follower behind the backup point needs them", pub.id, ring)
				}
			}
		})
	}
}

// TestRestoreSameBackupTwiceNeverReusesSeqs covers the repeat: the first
// restore resumed the publication past a stride and published from there, and
// then the same backup is restored again over it — the plain same-identity
// "takeover". The backup alone would put the second restore exactly where the
// first one started, below what that first incarnation's followers consumed,
// which is the original bug one restore later.
func TestRestoreSameBackupTwiceNeverReusesSeqs(t *testing.T) {
	srcDir := restoreDataDir(t)
	writeIdentity(t, srcDir)
	srcDB := newRestorableShareDB(t)
	pubID, _ := seedRestorablePublication(t, srcDB, "twice-tag", "active", 100, true)
	backupZip := filepath.Join(t.TempDir(), "backup.zip")
	if err := (&App{db: srcDB}).CreateBackup(backupZip); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}

	restoreDataDir(t)
	dstDB := newRestorableShareDB(t)
	app := &App{db: dstDB}
	if err := app.RestoreBackup(backupZip, "none"); err != nil {
		t.Fatalf("first RestoreBackup(none): %v", err)
	}
	firstBase, _ := shareSeqAndStatus(t, dstDB, pubID)

	// The first incarnation publishes one clip, which its followers consume.
	consumed := firstBase + 3
	if _, err := dstDB.Exec(`UPDATE shares SET last_seq = ? WHERE id = ?`, int64(consumed), pubID); err != nil {
		t.Fatal(err)
	}

	// Same identity on both sides: the backup's, adopted by the first restore.
	if err := app.RestoreBackup(backupZip, "takeover"); err != nil {
		t.Fatalf("second RestoreBackup(takeover): %v", err)
	}
	secondBase, status := shareSeqAndStatus(t, dstDB, pubID)
	if status != "active" {
		t.Fatalf("share status %q after the second restore, want active", status)
	}
	if secondBase <= consumed {
		t.Fatalf("second restore resumes at last_seq=%d, not above %d that the first restore's followers already consumed (first restore resumed at %d)", secondBase, consumed, firstBase)
	}
}

// TestRestoreInvalidatesShareWhoseSeqCannotAdvance covers backup SQL that is
// corrupt or hostile: a last_seq so close to the top of int64 that adding the
// stride would wrap it negative. That publication must not come back active —
// its next emission would seal under a seq that no longer round-trips through
// the int64 column — while its neighbours restore normally.
func TestRestoreInvalidatesShareWhoseSeqCannotAdvance(t *testing.T) {
	srcDir := restoreDataDir(t)
	writeIdentity(t, srcDir)
	srcDB := newRestorableShareDB(t)
	hugeID, _ := seedRestorablePublication(t, srcDB, "huge-tag", "active", int64(1)<<62+1, false)
	fineID, _ := seedRestorablePublication(t, srcDB, "fine-tag", "active", 100, false)
	backupZip := filepath.Join(t.TempDir(), "backup.zip")
	if err := (&App{db: srcDB}).CreateBackup(backupZip); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}

	restoreDataDir(t)
	dstDB := newRestorableShareDB(t)
	if err := (&App{db: dstDB}).RestoreBackup(backupZip, "none"); err != nil {
		t.Fatalf("RestoreBackup(none): %v", err)
	}

	if _, status := shareSeqAndStatus(t, dstDB, hugeID); status != "invalid" {
		t.Errorf("share with an unadvanceable last_seq has status %q, want invalid", status)
	}
	if seq, status := shareSeqAndStatus(t, dstDB, fineID); status != "active" || seq <= 100 {
		t.Errorf("neighbouring share: status %q last_seq %d, want active and advanced past 100", status, seq)
	}
}

// TestRestoredShareSeqBaseOutrunsAnEarlierRestoreElsewhere covers the repeat
// this install cannot see: the same backup restored a second time on a
// different machine from the first restore. Neither the backup nor the new
// install holds any trace of how far the first restore's incarnation
// published, so both heads are identical to the first restore's. Only the wall
// clock has moved, so the clock is what has to carry the second restore past
// the first one's output.
func TestRestoredShareSeqBaseOutrunsAnEarlierRestoreElsewhere(t *testing.T) {
	const backupHead = 100
	// Far beyond any real publication: 100,000 seqs a second is ~100 GB/s of
	// 1 MiB chunks, sustained.
	const generousSeqsPerSecond = 100_000

	t1 := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	first, ok := restoredShareSeqBase(backupHead, 0, t1)
	if !ok {
		t.Fatal("first restore refused a small head")
	}
	for _, elapsed := range []time.Duration{time.Second, time.Hour, 30 * 24 * time.Hour} {
		second, ok := restoredShareSeqBase(backupHead, 0, t1.Add(elapsed))
		if !ok {
			t.Fatalf("restore %v later refused a small head", elapsed)
		}
		published := uint64(elapsed.Seconds() * generousSeqsPerSecond)
		if second <= first+published {
			t.Errorf("restore %v after the first resumes at %d, not above %d that the first restore's incarnation could have reached (first resumed at %d)", elapsed, second, first+published, first)
		}
	}

	// ShareInfo.LastSeq reaches the frontend as a JS number, exact only up to
	// 2^53. Today's floor has to sit well inside that.
	if first >= 1<<53 {
		t.Errorf("a restore today resumes at %d, beyond 2^53 — the UI would round it", first)
	}
}

// TestRestoredShareSeqBaseWithoutAUsableClock pins that the clock only ever
// raises the base. A clock at or before 1970, or one too far in the future to
// scale, still leaves the stride over the backup and prior heads, which never
// depended on it.
func TestRestoredShareSeqBaseWithoutAUsableClock(t *testing.T) {
	for _, now := range []time.Time{
		{},              // zero time, year 1
		time.Unix(0, 0), // the epoch
		time.Date(200000, 1, 1, 0, 0, 0, 0, time.UTC),
	} {
		base, ok := restoredShareSeqBase(100, 7_000, now)
		if !ok {
			t.Errorf("clock %v: restore refused a small head", now)
			continue
		}
		if base < 7_000+restoredShareSeqStride {
			t.Errorf("clock %v: resumes at %d, below the prior head plus the stride (%d)", now, base, 7_000+restoredShareSeqStride)
		}
	}

	if _, ok := restoredShareSeqBase(maxRestorableShareSeq+1, 0, time.Now()); ok {
		t.Error("a head above maxRestorableShareSeq was advanced instead of refused")
	}
	if base, ok := restoredShareSeqBase(maxRestorableShareSeq, 0, time.Now()); !ok || base <= maxRestorableShareSeq || int64(base) < 0 {
		t.Errorf("head at the bound: base=%d ok=%v, want a positive int64 above it", base, ok)
	}
}
