package app

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openTestDBWithShareRing(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE shares (id INTEGER PRIMARY KEY, tag_id INTEGER, symkey BLOB, share_id BLOB, last_seq INTEGER, status TEXT, created_at INTEGER);
CREATE TABLE share_ring (id INTEGER PRIMARY KEY, publication_id INTEGER, seq INTEGER, kind TEXT, envelope_bytes BLOB, ts INTEGER, FOREIGN KEY(publication_id) REFERENCES shares(id) ON DELETE CASCADE);
CREATE UNIQUE INDEX idx_share_ring_pub_seq ON share_ring(publication_id, seq);
CREATE INDEX idx_share_ring_ts ON share_ring(ts);`); err != nil {
		t.Fatal(err)
	}
	db.Exec(`INSERT INTO shares (id, tag_id, symkey, share_id, last_seq, status, created_at) VALUES (1, 1, X'00', X'00', 0, 'active', 0)`)
	return db
}

func TestRingRetransmitMetaReturnsSizesWithoutBlobs(t *testing.T) {
	db := openTestDBWithShareRing(t)
	now := time.Now().Unix()
	for seq := uint64(1); seq <= 5; seq++ {
		if err := RingInsert(db, 1, seq, KindClipChunk, make([]byte, 10*int(seq)), now); err != nil {
			t.Fatal(err)
		}
	}
	rows, _, err := RingRetransmitMeta(db, 1, 2, now, CatchupMetaRowCap)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows=%d want 3", len(rows))
	}
	// Seq order 3,4,5, each carrying the length of the blob it stands for.
	for i, r := range rows {
		wantSeq := uint64(i + 3)
		if r.Seq != wantSeq {
			t.Fatalf("row %d seq %d want %d", i, r.Seq, wantSeq)
		}
		if r.Kind != KindClipChunk {
			t.Fatalf("row %d kind %q want %q", i, r.Kind, KindClipChunk)
		}
		if r.ByteLen != int64(10*wantSeq) {
			t.Fatalf("row %d byte len %d want %d", i, r.ByteLen, 10*wantSeq)
		}
	}
}

func TestRingRetransmitMetaRespectsTTL(t *testing.T) {
	db := openTestDBWithShareRing(t)
	now := time.Now().Unix()
	// Old row (2h ago)
	RingInsert(db, 1, 1, KindClipChunk, []byte{1}, now-2*3600)
	// Fresh row
	RingInsert(db, 1, 2, KindClipChunk, []byte{2}, now)
	rows, _, err := RingRetransmitMeta(db, 1, 0, now, CatchupMetaRowCap)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Seq != 2 {
		t.Fatalf("expected only fresh row, got %+v", rows)
	}
}

// TestRingFetchRangeLoadsOnlyTheRequestedSpan is the memory bound itself: the
// catch-up blob fetch must read the planned batch and nothing else, however
// much of the ring survived around it. A range query that ignored its bounds
// would reintroduce the whole-ring load the planner split apart.
func TestRingFetchRangeLoadsOnlyTheRequestedSpan(t *testing.T) {
	db := openTestDBWithShareRing(t)
	now := time.Now().Unix()
	const total = 40
	for seq := uint64(1); seq <= total; seq++ {
		if err := RingInsert(db, 1, seq, KindClipChunk, []byte{byte(seq)}, now); err != nil {
			t.Fatal(err)
		}
	}
	// A second publication's rows must not leak into the span either.
	db.Exec(`INSERT INTO shares (id, tag_id, symkey, share_id, last_seq, status, created_at) VALUES (2, 2, X'00', X'01', 0, 'active', 0)`)
	for seq := uint64(1); seq <= total; seq++ {
		if err := RingInsert(db, 2, seq, KindClipChunk, []byte{0xFF}, now); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := RingFetchRange(db, 1, 10, 14, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 {
		t.Fatalf("fetched %d rows for seqs 10..14, want 5 (the whole ring holds %d per publication)", len(rows), total)
	}
	for i, r := range rows {
		wantSeq := uint64(i + 10)
		if r.Seq != wantSeq {
			t.Fatalf("row %d seq %d want %d", i, r.Seq, wantSeq)
		}
		if len(r.EnvelopeBytes) != 1 || r.EnvelopeBytes[0] != byte(wantSeq) {
			t.Fatalf("row at seq %d carries %v, want the blob published for this publication", r.Seq, r.EnvelopeBytes)
		}
	}
}

func TestRingFetchRangeRespectsTTL(t *testing.T) {
	db := openTestDBWithShareRing(t)
	now := time.Now().Unix()
	RingInsert(db, 1, 1, KindClipChunk, []byte{1}, now-2*3600)
	RingInsert(db, 1, 2, KindClipChunk, []byte{2}, now)
	rows, err := RingFetchRange(db, 1, 1, 2, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Seq != 2 {
		t.Fatalf("expected only the fresh row, got %+v", rows)
	}
}

func TestRingEvictAgeAndCap(t *testing.T) {
	db := openTestDBWithShareRing(t)
	now := time.Now().Unix()
	// Age: 5 old rows
	for i := uint64(1); i <= 5; i++ {
		RingInsert(db, 1, i, KindClipChunk, []byte{1}, now-2*3600)
	}
	// Cap: 3 fresh rows, each 200KB
	big := make([]byte, 200*1024)
	for i := uint64(6); i <= 8; i++ {
		RingInsert(db, 1, i, KindClipChunk, big, now)
	}
	if err := RingEvict(db, now, 500*1024); err != nil { // cap 500KB
		t.Fatal(err)
	}
	var total int
	var count int
	db.QueryRow("SELECT COUNT(*), COALESCE(SUM(LENGTH(envelope_bytes)),0) FROM share_ring").Scan(&count, &total)
	// Expect all 5 old rows deleted by age; then oldest of the fresh rows
	// removed until cap satisfied (≤500KB).
	if count == 0 || total > 500*1024 {
		t.Fatalf("after evict count=%d total=%d", count, total)
	}
}
