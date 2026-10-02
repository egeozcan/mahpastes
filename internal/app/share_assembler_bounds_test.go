package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"os"
	"testing"
)

// The publisher is untrusted from the follower's side — anyone can hand out a
// share string — so a clip must be held to the shape its own clip_start
// declares. These tests drive the assembler with streams no honest publisher
// produces and require each one to end as a poisoned clip: nothing stored, and
// (at the consumer level) the durable boundary stepped past it rather than a
// reconnect loop that pulls the same clip forever.

func newBoundsAssembler(t *testing.T) (*sql.DB, string, *clipAssembler) {
	t.Helper()
	db := newTestDB(t)
	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (42, 'inbox', '#888')`); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	return db, dir, newClipAssembler(dir, 7)
}

func stagingFileCount(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

func clipRowCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM clips`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// shaOf hashes the concatenation of chunks — the digest a publisher would seal
// into clip_end for exactly the bytes it sent.
func shaOf(chunks ...[]byte) []byte {
	h := sha256.New()
	for _, c := range chunks {
		h.Write(c)
	}
	return h.Sum(nil)
}

// expectPoisoned finishes a clip and requires the terminal verdict.
func expectPoisoned(t *testing.T, asm *clipAssembler, db *sql.DB, clipID uint64, sum []byte) {
	t.Helper()
	_, err := asm.onEnd(ClipEndPayload{ClipID: clipID, SHA256: sum}, db, 42, nil)
	if !errors.Is(err, errClipPoisoned) {
		t.Fatalf("onEnd err = %v, want errClipPoisoned — anything retryable makes the follower re-pull this clip on every reconnect", err)
	}
	if n := clipRowCount(t, db); n != 0 {
		t.Fatalf("%d clips stored, want 0", n)
	}
}

func TestAssemblerRefusesOversizeClipAtStart(t *testing.T) {
	db, dir, asm := newBoundsAssembler(t)
	total := uint64(MaxShareableClipBytes) + 1
	asm.onStart(ClipStartPayload{
		ClipID: 1, Filename: "huge.bin", ContentType: "application/octet-stream",
		TotalSize: total, ChunkCount: uint32((total + ChunkSize - 1) / ChunkSize),
	})
	if n := stagingFileCount(t, dir); n != 0 {
		t.Fatalf("%d staging files for a clip over the share ceiling, want 0 — it must be refused before touching disk", n)
	}

	data := []byte("tiny")
	asm.onChunk(ClipChunkPayload{ClipID: 1, Index: 0, Data: data})
	if n := stagingFileCount(t, dir); n != 0 {
		t.Fatalf("%d staging files after a chunk of a refused clip, want 0", n)
	}
	expectPoisoned(t, asm, db, 1, shaOf(data))
}

// ChunkCount is fixed by TotalSize: the publisher always emits
// max(1, ceil(total/ChunkSize)) chunks. Any other count is a stream that
// cannot have come from an honest emission.
func TestAssemblerRefusesChunkCountThatContradictsSize(t *testing.T) {
	cases := []struct {
		name   string
		total  uint64
		chunks uint32
	}{
		{"more chunks than the size needs", 5, 3},
		{"fewer chunks than the size needs", ChunkSize + 1, 1},
		{"zero chunks for a non-empty clip", 5, 0},
		{"zero chunks for an empty clip", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, dir, asm := newBoundsAssembler(t)
			asm.onStart(ClipStartPayload{ClipID: 1, TotalSize: tc.total, ChunkCount: tc.chunks})
			if n := stagingFileCount(t, dir); n != 0 {
				t.Fatalf("%d staging files for an inconsistent clip_start, want 0", n)
			}
			// One chunk carrying exactly the declared bytes, so only the count
			// can be what is wrong.
			data := bytes.Repeat([]byte{'z'}, int(tc.total))
			asm.onChunk(ClipChunkPayload{ClipID: 1, Index: 0, Data: data})
			expectPoisoned(t, asm, db, 1, shaOf(data))
		})
	}
}

// The verifier's repro: a clip declaring 4 bytes that is fed 12. The running
// hash matches what was sent, so only the declared size can stop it.
func TestAssemblerRefusesChunkPastDeclaredSize(t *testing.T) {
	db, dir, asm := newBoundsAssembler(t)
	asm.onStart(ClipStartPayload{ClipID: 1, Filename: "m.bin", TotalSize: 4, ChunkCount: 1})
	data := []byte("aaaabbbbcccc")
	asm.onChunk(ClipChunkPayload{ClipID: 1, Index: 0, Data: data})
	if n := stagingFileCount(t, dir); n != 0 {
		t.Fatalf("%d staging files after an overflowing chunk, want 0 — writing must stop at the first violation", n)
	}
	// More chunks of the same clip must not reopen anything.
	asm.onChunk(ClipChunkPayload{ClipID: 1, Index: 1, Data: data})
	if n := stagingFileCount(t, dir); n != 0 {
		t.Fatalf("%d staging files after further chunks of a refused clip, want 0", n)
	}
	expectPoisoned(t, asm, db, 1, shaOf(data, data))
}

func TestAssemblerRefusesOutOfOrderChunks(t *testing.T) {
	big := bytes.Repeat([]byte{'b'}, ChunkSize)
	tail := []byte("tail")
	cases := []struct {
		name   string
		total  uint64
		chunks uint32
		feed   []ClipChunkPayload
	}{
		{
			name: "second chunk first", total: ChunkSize + 4, chunks: 2,
			feed: []ClipChunkPayload{
				{ClipID: 1, Index: 1, Data: tail},
				{ClipID: 1, Index: 0, Data: big},
			},
		},
		{
			name: "same index twice", total: 8, chunks: 1,
			feed: []ClipChunkPayload{
				{ClipID: 1, Index: 0, Data: tail},
				{ClipID: 1, Index: 0, Data: tail},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, _, asm := newBoundsAssembler(t)
			asm.onStart(ClipStartPayload{ClipID: 1, TotalSize: tc.total, ChunkCount: tc.chunks})
			var sent [][]byte
			for _, c := range tc.feed {
				asm.onChunk(c)
				sent = append(sent, c.Data)
			}
			expectPoisoned(t, asm, db, 1, shaOf(sent...))
		})
	}
}

// Zero-length extras keep the byte total exact, so the chunk count is the only
// rule this stream breaks.
func TestAssemblerRefusesChunksBeyondDeclaredCount(t *testing.T) {
	db, _, asm := newBoundsAssembler(t)
	data := []byte("four")
	asm.onStart(ClipStartPayload{ClipID: 1, TotalSize: 4, ChunkCount: 1})
	asm.onChunk(ClipChunkPayload{ClipID: 1, Index: 0, Data: data})
	asm.onChunk(ClipChunkPayload{ClipID: 1, Index: 1, Data: nil})
	asm.onChunk(ClipChunkPayload{ClipID: 1, Index: 2, Data: nil})
	expectPoisoned(t, asm, db, 1, shaOf(data))
}

func TestAssemblerRefusesShortClip(t *testing.T) {
	t.Run("fewer bytes than declared", func(t *testing.T) {
		db, _, asm := newBoundsAssembler(t)
		data := []byte("four")
		asm.onStart(ClipStartPayload{ClipID: 1, TotalSize: 10, ChunkCount: 1})
		asm.onChunk(ClipChunkPayload{ClipID: 1, Index: 0, Data: data})
		expectPoisoned(t, asm, db, 1, shaOf(data))
	})
	t.Run("fewer chunks than declared", func(t *testing.T) {
		db, _, asm := newBoundsAssembler(t)
		// Every declared byte arrives, but in one chunk of the two promised.
		data := bytes.Repeat([]byte{'s'}, ChunkSize+4)
		asm.onStart(ClipStartPayload{ClipID: 1, TotalSize: ChunkSize + 4, ChunkCount: 2})
		asm.onChunk(ClipChunkPayload{ClipID: 1, Index: 0, Data: data})
		expectPoisoned(t, asm, db, 1, shaOf(data))
	})
}

// A clip_end with no clip_start behind it at all is a different animal: the
// session most likely lost the start (a local staging failure), so the clip is
// still worth asking for again and the error must stay retryable.
func TestAssemblerClipEndWithoutStartStaysRetryable(t *testing.T) {
	db, _, asm := newBoundsAssembler(t)
	if _, err := asm.onEnd(ClipEndPayload{ClipID: 1, SHA256: shaOf()}, db, 42, nil); err == nil || errors.Is(err, errClipPoisoned) {
		t.Fatalf("clip_end with no clip_start: err = %v, want a retryable (non-poisoned) error", err)
	}

	// A refused clip poisons only its own clip_end, not some other clip's.
	asm.onStart(ClipStartPayload{ClipID: 1, TotalSize: uint64(MaxShareableClipBytes) + 1, ChunkCount: 31})
	if _, err := asm.onEnd(ClipEndPayload{ClipID: 2, SHA256: shaOf()}, db, 42, nil); err == nil || errors.Is(err, errClipPoisoned) {
		t.Fatalf("clip_end for a clip that never started: err = %v, want a retryable (non-poisoned) error", err)
	}
}

func TestAssemblerLandsMultiChunkClip(t *testing.T) {
	db, dir, asm := newBoundsAssembler(t)
	c0 := bytes.Repeat([]byte{'0'}, ChunkSize)
	c1 := bytes.Repeat([]byte{'1'}, ChunkSize)
	c2 := []byte("tail!")
	total := uint64(len(c0) + len(c1) + len(c2))
	asm.onStart(ClipStartPayload{ClipID: 1, Filename: "multi.bin", ContentType: "application/octet-stream", TotalSize: total, ChunkCount: 3})
	asm.onChunk(ClipChunkPayload{ClipID: 1, Index: 0, Data: c0})
	asm.onChunk(ClipChunkPayload{ClipID: 1, Index: 1, Data: c1})
	asm.onChunk(ClipChunkPayload{ClipID: 1, Index: 2, Data: c2})
	id, err := asm.onEnd(ClipEndPayload{ClipID: 1, SHA256: shaOf(c0, c1, c2)}, db, 42, nil)
	if err != nil {
		t.Fatalf("onEnd: %v", err)
	}
	var got []byte
	if err := db.QueryRow(`SELECT data FROM clips WHERE id = ?`, id).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, bytes.Join([][]byte{c0, c1, c2}, nil)) {
		t.Fatalf("stored %d bytes, want the %d-byte concatenation of all three chunks", len(got), total)
	}
	if n := stagingFileCount(t, dir); n != 0 {
		t.Fatalf("staging dir holds %d files after a clean landing", n)
	}
}

// The publisher emits one zero-length chunk for an empty clip
// (OnClipCreated's chunkCount = 1 floor), so that shape must keep landing.
func TestAssemblerLandsEmptyClip(t *testing.T) {
	db, _, asm := newBoundsAssembler(t)
	asm.onStart(ClipStartPayload{ClipID: 1, Filename: "empty.txt", ContentType: "text/plain", TotalSize: 0, ChunkCount: 1})
	asm.onChunk(ClipChunkPayload{ClipID: 1, Index: 0, Data: []byte{}})
	id, err := asm.onEnd(ClipEndPayload{ClipID: 1, SHA256: shaOf()}, db, 42, nil)
	if err != nil {
		t.Fatalf("onEnd: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT LENGTH(data) FROM clips WHERE id = ?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("empty clip stored with %d bytes", n)
	}
}

// End to end through consumeStream: each misshapen clip is dropped, the
// boundary moves past it in the same session, and a healthy clip behind them
// still lands — the poisoned path, not a reconnect loop.
func TestFollowerStepsPastClipsThatBreakTheirDeclaredShape(t *testing.T) {
	symkey := bytes.Repeat([]byte{0xB7}, 32)
	shareID := DeriveShareID(symkey)
	m, db, f := newConsumerFixture(t, symkey)

	stream := &bytes.Buffer{}
	seq := uint64(0)
	put := func(payload any) {
		seq++
		stream.Write(envelopeAt(t, symkey, shareID, seq, payload))
	}

	// Clip 1 declares more than any share may carry.
	tiny := []byte("x")
	oversize := uint64(MaxShareableClipBytes) + 1
	put(ClipStartPayload{Seq: 1, Kind: KindClipStart, ClipID: 1, Filename: "huge.bin", ContentType: "application/octet-stream",
		Metadata: map[string]string{}, TotalSize: oversize, ChunkCount: uint32((oversize + ChunkSize - 1) / ChunkSize)})
	put(ClipChunkPayload{Seq: 2, Kind: KindClipChunk, ClipID: 1, Index: 0, Data: tiny})
	put(ClipEndPayload{Seq: 3, Kind: KindClipEnd, ClipID: 1, SHA256: shaOf(tiny)})

	// Clip 2 declares 4 bytes and streams 12 under one index.
	body := []byte("aaaabbbbcccc")
	put(ClipStartPayload{Seq: 4, Kind: KindClipStart, ClipID: 2, Filename: "m.bin", ContentType: "application/octet-stream",
		Metadata: map[string]string{}, TotalSize: 4, ChunkCount: 1})
	for i := 0; i < 3; i++ {
		put(ClipChunkPayload{Seq: seq + 1, Kind: KindClipChunk, ClipID: 2, Index: 0, Data: body[i*4 : i*4+4]})
	}
	put(ClipEndPayload{Seq: 8, Kind: KindClipEnd, ClipID: 2, SHA256: shaOf(body)})

	// Clip 3 is honest.
	healthy := []byte("healthy clip")
	for _, r := range clipRing(t, symkey, shareID, 9, 3, healthy, false) {
		stream.Write(r.EnvelopeBytes)
	}

	if err := m.consumeStream(context.Background(), f, stream); err == nil {
		t.Fatal("expected EOF once the stream is drained")
	}

	if n := clipRowCount(t, db); n != 1 {
		t.Fatalf("%d clips stored, want 1 — only the healthy clip", n)
	}
	var got []byte
	if err := db.QueryRow(`SELECT data FROM clips`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, healthy) {
		t.Fatalf("stored %q, want the healthy clip", got)
	}
	lastSeq, received := followCursorRow(t, db, f.id)
	if lastSeq != 11 || received != 1 {
		t.Fatalf("follows last_seq=%d clips_received=%d, want 11 and 1", lastSeq, received)
	}
}
