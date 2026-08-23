package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// envelopeAt seals payload under seq and returns the length-prefixed frame,
// ready to concatenate into a synthetic stream. Driving consumeStream with a
// bytes.Buffer lets a session end at an exact frame boundary, which no amount
// of timing against a live libp2p stream can guarantee.
func envelopeAt(t *testing.T, symkey, shareID []byte, seq uint64, payload any) []byte {
	t.Helper()
	pt, err := MarshalPayload(payload)
	if err != nil {
		t.Fatal(err)
	}
	env, err := EncryptEnvelope(symkey, shareID, seq, pt)
	if err != nil {
		t.Fatal(err)
	}
	return env
}

// newTestFollow inserts a follows row and returns the in-memory follow that
// consumeStream operates on, wired to a canceled-on-cleanup context.
func newTestFollow(t *testing.T, db *sql.DB, localTagID int64, symkey []byte) *follow {
	t.Helper()
	now := time.Now().Unix()
	res, err := db.Exec(
		`INSERT INTO follows (remote_peer_id, symkey, local_tag_id, last_seq, last_seen_at, created_at) VALUES ('peer', ?, ?, 0, ?, ?)`,
		symkey, localTagID, now, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &follow{
		id: id, symkey: symkey, localTagID: localTagID, lastSeq: 0,
		ctx: ctx, cancel: cancel, reconnectSignal: make(chan struct{}, 1),
	}
}

// followCursorRow reads the durable resume point and delivery count.
func followCursorRow(t *testing.T, db *sql.DB, followID int64) (lastSeq, clipsReceived int64) {
	t.Helper()
	if err := db.QueryRow(
		`SELECT last_seq, clips_received FROM follows WHERE id = ?`, followID,
	).Scan(&lastSeq, &clipsReceived); err != nil {
		t.Fatal(err)
	}
	return lastSeq, clipsReceived
}

// clipRing builds the clip_start / clip_chunk / clip_end triple for a one-chunk
// clip occupying startSeq..startSeq+2, shaped as the ring rows the publisher
// would have retained. When corrupt is true the clip_end carries a SHA-256 the
// body cannot match.
func clipRing(t *testing.T, symkey, shareID []byte, startSeq, clipID uint64, body []byte, corrupt bool) []RingRow {
	t.Helper()
	sum := sha256.Sum256(body)
	want := sum[:]
	if corrupt {
		want = bytes.Repeat([]byte{0xFF}, 32)
	}
	return []RingRow{
		{Seq: startSeq, Kind: KindClipStart, EnvelopeBytes: envelopeAt(t, symkey, shareID, startSeq, ClipStartPayload{
			Seq: startSeq, TS: time.Now().UnixMilli(), Kind: KindClipStart, ClipID: clipID,
			Filename: "a.txt", ContentType: "text/plain", Metadata: map[string]string{},
			TotalSize: uint64(len(body)), ChunkCount: 1,
		})},
		{Seq: startSeq + 1, Kind: KindClipChunk, EnvelopeBytes: envelopeAt(t, symkey, shareID, startSeq+1, ClipChunkPayload{
			Seq: startSeq + 1, Kind: KindClipChunk, ClipID: clipID, Index: 0, Data: body,
		})},
		{Seq: startSeq + 2, Kind: KindClipEnd, EnvelopeBytes: envelopeAt(t, symkey, shareID, startSeq+2, ClipEndPayload{
			Seq: startSeq + 2, Kind: KindClipEnd, ClipID: clipID, SHA256: want,
		})},
	}
}

// streamOf concatenates ring rows into a session's worth of framed bytes.
func streamOf(rows ...[]RingRow) *bytes.Buffer {
	buf := &bytes.Buffer{}
	for _, group := range rows {
		for _, r := range group {
			buf.Write(r.EnvelopeBytes)
		}
	}
	return buf
}

// replayFrom models the publisher's catch-up for a follower handshaking at
// sinceSeq, through the very functions handlePublisherStream uses: ring rows
// above sinceSeq, leading orphans dropped, prefixed by a synthesized gap when
// eviction (or a mid-clip resume point) leaves a hole. Feeding the consumer
// this rather than a hand-picked replay is what makes the resume tests prove
// delivery instead of assuming it.
func replayFrom(t *testing.T, symkey, shareID []byte, ring []RingRow, sinceSeq, pubLastSeq uint64) *bytes.Buffer {
	t.Helper()
	// Mirror production's two steps: plan over metadata, then pull the blobs
	// of exactly the planned seqs.
	byteSeq := map[uint64][]byte{}
	var survivors []RingRowMeta
	for _, r := range ring {
		if r.Seq > sinceSeq {
			survivors = append(survivors, RingRowMeta{Seq: r.Seq, Kind: r.Kind, ByteLen: int64(len(r.EnvelopeBytes))})
			byteSeq[r.Seq] = r.EnvelopeBytes
		}
	}
	send, gapTarget := planRetransmit(survivors, sinceSeq, pubLastSeq)
	buf := &bytes.Buffer{}
	if gapTarget > 0 {
		gap, err := encodeGapEnvelope(symkey, shareID, sinceSeq+1, gapTarget)
		if err != nil {
			t.Fatal(err)
		}
		buf.Write(gap)
	}
	for _, r := range send {
		env, ok := byteSeq[r.Seq]
		if !ok {
			t.Fatalf("planner selected seq %d with no envelope behind it", r.Seq)
		}
		buf.Write(env)
	}
	return buf
}

// newConsumerFixture spins up a manager over a fresh in-memory DB with one
// local tag, and returns it alongside the follow that receives into that tag.
func newConsumerFixture(t *testing.T, symkey []byte) (*ShareManager, *sql.DB, *follow) {
	t.Helper()
	db := newTestDB(t)
	m, err := NewShareManager(context.Background(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Stop)
	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (7, 'inbox', '#aaa')`); err != nil {
		t.Fatal(err)
	}
	return m, db, newTestFollow(t, db, 7, symkey)
}

// setFollowBoundary plants a durable resume point on a fresh follow, standing
// in for a follow that has already synced that far.
func setFollowBoundary(t *testing.T, db *sql.DB, f *follow, seq uint64) {
	t.Helper()
	if _, err := db.Exec(`UPDATE follows SET last_seq = ? WHERE id = ?`, int64(seq), f.id); err != nil {
		t.Fatal(err)
	}
	f.lastSeq = seq
}

// TestFollowerResumesInterruptedClipFromBoundary is the regression for silent
// mid-clip data loss. The consumer used to persist follows.last_seq after every
// frame, so a session that died between clip_start and clip_end resumed at a
// mid-clip seq: the publisher then replayed from after the clip_start the fresh
// assembler never saw, and the interrupted clip was lost for good — reachable
// from the UI just by hitting Refresh during a slow transfer.
func TestFollowerResumesInterruptedClipFromBoundary(t *testing.T) {
	symkey := bytes.Repeat([]byte{0xC1}, 32)
	shareID := DeriveShareID(symkey)
	m, db, f := newConsumerFixture(t, symkey)

	body := []byte("bytes that must survive a reconnect")
	ring := clipRing(t, symkey, shareID, 1, 1, body, false)

	// Session 1 dies mid-clip: clip_start and the chunk arrive, clip_end never does.
	interrupted := streamOf(ring[:2])
	if err := m.consumeStream(context.Background(), f, interrupted); err == nil {
		t.Fatal("truncated session should have ended with a read error")
	}

	if f.lastSeq != 0 {
		t.Fatalf("in-memory resume point %d want 0 — mid-clip frames must not advance it", f.lastSeq)
	}
	lastSeq, clipsReceived := followCursorRow(t, db, f.id)
	if lastSeq != 0 {
		t.Fatalf("follows.last_seq %d want 0 — the next handshake must ask from before the clip_start", lastSeq)
	}
	if clipsReceived != 0 {
		t.Fatalf("clips_received %d want 0 — no clip completed", clipsReceived)
	}

	// Session 2 asks the publisher for exactly what the resume point says is
	// missing. From a clip boundary that is the whole clip, clip_start
	// included; from a mid-clip seq the publisher can only offer orphan tail
	// rows, drops them, and gaps past the clip — losing it forever.
	resumed := replayFrom(t, symkey, shareID, ring, f.lastSeq, 3)
	if err := m.consumeStream(context.Background(), f, resumed); err == nil {
		t.Fatal("expected EOF at the end of the replayed session")
	}

	var got []byte
	if err := db.QueryRow(`SELECT data FROM clips ORDER BY id DESC LIMIT 1`).Scan(&got); err != nil {
		t.Fatalf("interrupted clip was never assembled after the resume: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("assembled clip data %q want %q", got, body)
	}
	lastSeq, clipsReceived = followCursorRow(t, db, f.id)
	if lastSeq != 3 || clipsReceived != 1 {
		t.Fatalf("after resume last_seq=%d clips_received=%d want 3 and 1", lastSeq, clipsReceived)
	}
}

// TestClipEndPersistsCursorWithClipInsert locks the atomicity the boundary-only
// cursor depends on: the clip row, the delivery count, and the resume point all
// land in one transaction, so nothing can observe a stored clip whose cursor
// still asks for it (a duplicate on the next handshake).
func TestClipEndPersistsCursorWithClipInsert(t *testing.T) {
	symkey := bytes.Repeat([]byte{0xC2}, 32)
	shareID := DeriveShareID(symkey)
	m, db, f := newConsumerFixture(t, symkey)

	stream := streamOf(clipRing(t, symkey, shareID, 1, 1, []byte("one clip"), false))
	if err := m.consumeStream(context.Background(), f, stream); err == nil {
		t.Fatal("expected EOF once the stream is drained")
	}

	var clips int
	if err := db.QueryRow(`SELECT COUNT(*) FROM clips`).Scan(&clips); err != nil {
		t.Fatal(err)
	}
	if clips != 1 {
		t.Fatalf("clips rows %d want 1", clips)
	}
	lastSeq, clipsReceived := followCursorRow(t, db, f.id)
	if lastSeq != 3 {
		t.Fatalf("follows.last_seq %d want 3 (the clip_end seq)", lastSeq)
	}
	if clipsReceived != 1 {
		t.Fatalf("clips_received %d want 1", clipsReceived)
	}
	if f.lastSeq != 3 {
		t.Fatalf("in-memory resume point %d want 3", f.lastSeq)
	}
	var tagged int
	if err := db.QueryRow(`SELECT COUNT(*) FROM clip_tags WHERE tag_id = 7`).Scan(&tagged); err != nil {
		t.Fatal(err)
	}
	if tagged != 1 {
		t.Fatalf("clip_tags rows for the follow tag %d want 1", tagged)
	}
}

// TestFollowerSkipsPoisonedClipAndKeepsGoing covers the other half of the
// boundary policy: a clip that fails its integrity check is terminal, so the
// resume point must step past it. Holding the boundary there would redeliver
// the same bad clip on every reconnect, forever. The good clip that follows
// proves the session survives the poison rather than merely not looping.
func TestFollowerSkipsPoisonedClipAndKeepsGoing(t *testing.T) {
	symkey := bytes.Repeat([]byte{0xC3}, 32)
	shareID := DeriveShareID(symkey)
	m, db, f := newConsumerFixture(t, symkey)

	stream := streamOf(
		clipRing(t, symkey, shareID, 1, 1, []byte("corrupt on the wire"), true),
		clipRing(t, symkey, shareID, 4, 2, []byte("healthy clip"), false),
	)
	if err := m.consumeStream(context.Background(), f, stream); err == nil {
		t.Fatal("expected EOF once the stream is drained")
	}

	var clips int
	if err := db.QueryRow(`SELECT COUNT(*) FROM clips`).Scan(&clips); err != nil {
		t.Fatal(err)
	}
	if clips != 1 {
		t.Fatalf("clips rows %d want 1 — the poisoned clip must not be stored", clips)
	}
	var data []byte
	if err := db.QueryRow(`SELECT data FROM clips`).Scan(&data); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, []byte("healthy clip")) {
		t.Fatalf("stored clip data %q want the healthy clip", data)
	}
	lastSeq, clipsReceived := followCursorRow(t, db, f.id)
	if lastSeq != 6 {
		t.Fatalf("follows.last_seq %d want 6 — the boundary must clear both clips", lastSeq)
	}
	if clipsReceived != 1 {
		t.Fatalf("clips_received %d want 1 — only the healthy clip counts", clipsReceived)
	}
}

// TestGapAdvancesDurableBoundary keeps the gap contract explicit: a gap sits
// between clips by definition, so it moves the durable resume point, not just
// the session-local wire cursor. Without that, a session that ended right after
// a gap would re-handshake below it and the publisher would answer with the
// identical gap forever.
func TestGapAdvancesDurableBoundary(t *testing.T) {
	symkey := bytes.Repeat([]byte{0xC4}, 32)
	shareID := DeriveShareID(symkey)
	m, db, f := newConsumerFixture(t, symkey)

	stream := bytes.NewBuffer(envelopeAt(t, symkey, shareID, 1, GapPayload{Seq: 50, Kind: KindGap}))
	if err := m.consumeStream(context.Background(), f, stream); err == nil {
		t.Fatal("expected EOF once the stream is drained")
	}

	if f.lastSeq != 50 {
		t.Fatalf("in-memory resume point %d want 50", f.lastSeq)
	}
	if lastSeq, _ := followCursorRow(t, db, f.id); lastSeq != 50 {
		t.Fatalf("follows.last_seq %d want 50 (the gap target)", lastSeq)
	}
}

// TestRewindGapMovesDurableBoundaryDown is the follower half of the
// seq-regression fix in isolation: the handshake's first frame carries a gap
// pointing BELOW the durable boundary, and both cursors must follow it down.
// Every other gap is forward-only, so this is the one frame position where a
// backwards target is honoured.
func TestRewindGapMovesDurableBoundaryDown(t *testing.T) {
	symkey := bytes.Repeat([]byte{0xC5}, 32)
	shareID := DeriveShareID(symkey)
	m, db, f := newConsumerFixture(t, symkey)
	setFollowBoundary(t, db, f, 100)

	// Sealed at since_seq+1 — the exact seq this follower will try first.
	stream := bytes.NewBuffer(envelopeAt(t, symkey, shareID, 101, GapPayload{Seq: 50, Kind: KindGap}))
	if err := m.consumeStream(context.Background(), f, stream); err == nil {
		t.Fatal("expected EOF once the stream is drained")
	}

	if f.lastSeq != 50 {
		t.Fatalf("in-memory resume point %d want 50 — the rewind target, not the seq the gap arrived at", f.lastSeq)
	}
	if lastSeq, _ := followCursorRow(t, db, f.id); lastSeq != 50 {
		t.Fatalf("follows.last_seq %d want 50 — advanceFollowBoundary must be able to move it down", lastSeq)
	}
}

// TestFollowerRecoversFromPublisherSeqRegression is the end-to-end regression
// for the permanent brick. A publisher restored from an older backup under
// identity policy "takeover" keeps its share_id and symkey but its last_seq
// drops below what this follower already consumed; every clip it publishes
// from then on is sealed at a seq the follower will not try, so decrypt fails,
// the session dies, and the reconnect carries the identical since_seq.
//
// Pre-fix this test could not pass, for two independent reasons, and it is
// worth naming both because either alone still loses:
//
//   - planCatchupBatch had no regression branch, so the handshake produced no
//     gap at all — planRetransmit's pubLastSeq > sinceSeq guard is false when
//     the publisher is behind. TestPlanCatchupBatchRewindsOnPublisherRegression
//     demonstrates that directly against the pure function.
//   - consumeStream's gap handling was forward-only, so even a gap sent by hand
//     would have been ignored — and, worse, the rejected gap still advanced the
//     boundary to the seq it arrived at, creeping the follower one seq further
//     ahead per reconnect instead of bricking loudly.
//
// Both halves were verified by temporarily reverting each one alone: without
// the publisher branch this test stops at the plan.Rewind assertion, and
// without the follower branch the boundary lands at 101 (the creep) and the
// clip is never assembled.
func TestFollowerRecoversFromPublisherSeqRegression(t *testing.T) {
	symkey := bytes.Repeat([]byte{0xC6}, 32)
	shareID := DeriveShareID(symkey)
	m, db, f := newConsumerFixture(t, symkey)
	setFollowBoundary(t, db, f, 100)

	// Plan the handshake answer through the publisher's real code path rather
	// than hand-rolling the frame, so the two halves are pinned together.
	plan := planCatchupBatch(nil, f.lastSeq, 50, defaultCatchupCaps(), false)
	if !plan.Rewind {
		t.Fatal("publisher did not detect the regression — the follower can never be reached again")
	}
	gap, err := encodeGapEnvelope(symkey, shareID, f.lastSeq+1, plan.GapTarget)
	if err != nil {
		t.Fatal(err)
	}

	// The publisher's next clip reuses seqs 51..53, above its restored head.
	body := []byte("published after the restore")
	ring := clipRing(t, symkey, shareID, 51, 9, body, false)

	stream := &bytes.Buffer{}
	stream.Write(gap)
	for _, r := range ring {
		stream.Write(r.EnvelopeBytes)
	}
	if err := m.consumeStream(context.Background(), f, stream); err == nil {
		t.Fatal("expected EOF at the end of the session")
	}

	var got []byte
	if err := db.QueryRow(`SELECT data FROM clips ORDER BY id DESC LIMIT 1`).Scan(&got); err != nil {
		t.Fatalf("clip published after the rewind was never assembled: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("assembled clip data %q want %q", got, body)
	}
	if f.lastSeq != 53 {
		t.Fatalf("in-memory resume point %d want 53 (rewind to 50 + clip at 51..53)", f.lastSeq)
	}
	lastSeq, clipsReceived := followCursorRow(t, db, f.id)
	if lastSeq != 53 || clipsReceived != 1 {
		t.Fatalf("follows last_seq=%d clips_received=%d want 53 and 1", lastSeq, clipsReceived)
	}
}

// TestBackwardsGapRejectedMidSession keeps the anti-rewind property that the
// first-frame exception deliberately does not extend. Once a session has
// carried real traffic, a gap pointing backwards is a replay — honouring it
// would re-deliver clips already stored, on any publisher's say-so, at any
// point in the stream.
func TestBackwardsGapRejectedMidSession(t *testing.T) {
	symkey := bytes.Repeat([]byte{0xC7}, 32)
	shareID := DeriveShareID(symkey)
	m, db, f := newConsumerFixture(t, symkey)

	// A whole clip first, so the backwards gap at seq 4 is not the first frame.
	ring := clipRing(t, symkey, shareID, 1, 1, []byte("first"), false)
	stream := streamOf(ring)
	stream.Write(envelopeAt(t, symkey, shareID, 4, GapPayload{Seq: 1, Kind: KindGap}))

	if err := m.consumeStream(context.Background(), f, stream); err == nil {
		t.Fatal("expected EOF once the stream is drained")
	}

	if f.lastSeq != 4 {
		t.Fatalf("in-memory resume point %d want 4 — a mid-session backwards target must be ignored", f.lastSeq)
	}
	if lastSeq, _ := followCursorRow(t, db, f.id); lastSeq != 4 {
		t.Fatalf("follows.last_seq %d want 4 — the boundary must not be dragged back to 1", lastSeq)
	}
}

// TestRewindToZeroIsHonoured covers the one place where a gap target of 0 is a
// real instruction rather than the "no gap needed" sentinel: a publisher
// restored to a state with nothing published yet. The follower must go all the
// way back to 0, or the publisher's first clip lands at seq 1 while the
// follower is still trying to decrypt somewhere above it.
func TestRewindToZeroIsHonoured(t *testing.T) {
	symkey := bytes.Repeat([]byte{0xC8}, 32)
	shareID := DeriveShareID(symkey)
	m, db, f := newConsumerFixture(t, symkey)
	setFollowBoundary(t, db, f, 7)

	plan := planCatchupBatch(nil, f.lastSeq, 0, defaultCatchupCaps(), false)
	if !plan.Rewind || plan.GapTarget != 0 {
		t.Fatalf("plan Rewind=%v GapTarget=%d want true and 0", plan.Rewind, plan.GapTarget)
	}
	gap, err := encodeGapEnvelope(symkey, shareID, f.lastSeq+1, plan.GapTarget)
	if err != nil {
		t.Fatal(err)
	}

	body := []byte("first clip of the restored publisher")
	stream := &bytes.Buffer{}
	stream.Write(gap)
	for _, r := range clipRing(t, symkey, shareID, 1, 1, body, false) {
		stream.Write(r.EnvelopeBytes)
	}
	if err := m.consumeStream(context.Background(), f, stream); err == nil {
		t.Fatal("expected EOF at the end of the session")
	}

	var got []byte
	if err := db.QueryRow(`SELECT data FROM clips ORDER BY id DESC LIMIT 1`).Scan(&got); err != nil {
		t.Fatalf("clip at seq 1 was never assembled after the rewind to 0: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("assembled clip data %q want %q", got, body)
	}
	if lastSeq, _ := followCursorRow(t, db, f.id); lastSeq != 3 {
		t.Fatalf("follows.last_seq %d want 3 (rewind to 0 + clip at 1..3)", lastSeq)
	}
}
