package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"testing"
	"unicode/utf8"

	"github.com/fxamacker/cbor/v2"
)

// A frame that decrypts is the publisher's own bytes, sealed at the seq the
// follower asked for, so whatever is wrong with its payload is wrong the same
// way on every replay. Ending the session on it holds the durable boundary in
// front of the frame, and every reconnect pulls it again: the follow wedges
// until the ring's TTL evicts the clip, with every later clip queued behind
// it. These tests require an undecodable payload to cost at most its own clip.

// The verifier's repro: a Latin-1 filename read raw from a Linux watch folder
// is CBOR-encoded verbatim, and a strict decoder rejects the whole clip_start.
// Already-deployed publishers send exactly this, so the follower must accept
// it — with the text made valid before it reaches the database.
func TestFollowerStoresClipWithInvalidUTF8Text(t *testing.T) {
	symkey := bytes.Repeat([]byte{0xD4}, 32)
	shareID := DeriveShareID(symkey)
	m, db, f := newConsumerFixture(t, symkey)

	stream := &bytes.Buffer{}
	seq := uint64(0)
	put := func(payload any) {
		seq++
		stream.Write(envelopeAt(t, symkey, shareID, seq, payload))
	}
	body := []byte("named in latin-1")
	sum := sha256.Sum256(body)
	put(ClipStartPayload{Seq: 1, Kind: KindClipStart, ClipID: 1,
		Filename: "caf\xe9.txt", ContentType: "text/plain\xff",
		Metadata:  map[string]string{"k\xfe": "v\xfd"},
		TotalSize: uint64(len(body)), ChunkCount: 1})
	put(ClipChunkPayload{Seq: 2, Kind: KindClipChunk, ClipID: 1, Index: 0, Data: body})
	put(ClipEndPayload{Seq: 3, Kind: KindClipEnd, ClipID: 1, SHA256: sum[:]})
	for _, r := range clipRing(t, symkey, shareID, 4, 2, []byte("healthy clip"), false) {
		stream.Write(r.EnvelopeBytes)
	}

	if err := m.consumeStream(context.Background(), f, stream); !errors.Is(err, io.EOF) {
		t.Fatalf("consumeStream ended with %v, want EOF once drained — any other error is replayed on every reconnect", err)
	}
	if n := clipRowCount(t, db); n != 2 {
		t.Fatalf("%d clips stored, want 2", n)
	}
	var filename, contentType, metadata string
	if err := db.QueryRow(`SELECT filename, content_type, metadata FROM clips WHERE data = ?`, body).
		Scan(&filename, &contentType, &metadata); err != nil {
		t.Fatal(err)
	}
	if filename != "caf�.txt" {
		t.Fatalf("filename %q, want the invalid byte replaced with U+FFFD", filename)
	}
	if contentType != "text/plain�" {
		t.Fatalf("content_type %q, want the invalid byte replaced with U+FFFD", contentType)
	}
	if !utf8.ValidString(metadata) {
		t.Fatalf("metadata %q is not valid UTF-8", metadata)
	}
	lastSeq, received := followCursorRow(t, db, f.id)
	if lastSeq != 6 || received != 2 {
		t.Fatalf("follows last_seq=%d clips_received=%d, want 6 and 2", lastSeq, received)
	}
}

// Payloads that decrypt but cannot be decoded at all — a field of the wrong
// CBOR type, or not a map. No honest publisher emits these, but one that does
// emits them identically on every replay. Each must poison only the clip it
// sits in (the boundary steps past that clip's clip_end, as for a SHA
// mismatch), and the healthy clip behind it must still land in the same
// session.
func TestFollowerStepsPastUndecodableFrames(t *testing.T) {
	healthy := []byte("healthy clip")
	body := []byte("doomed")
	sum := sha256.Sum256(body)
	goodStart := func(seq uint64) any {
		return ClipStartPayload{Seq: seq, Kind: KindClipStart, ClipID: 1, Filename: "d.txt",
			ContentType: "text/plain", Metadata: map[string]string{},
			TotalSize: uint64(len(body)), ChunkCount: 1}
	}
	goodChunk := func(seq uint64) any {
		return ClipChunkPayload{Seq: seq, Kind: KindClipChunk, ClipID: 1, Index: 0, Data: body}
	}
	goodEnd := func(seq uint64) any {
		return ClipEndPayload{Seq: seq, Kind: KindClipEnd, ClipID: 1, SHA256: sum[:]}
	}

	cases := []struct {
		name string
		// frames returns the payloads that precede the healthy clip, one per
		// seq starting at 1.
		frames       []func(seq uint64) any
		wantClips    int
		wantLastSeq  int64
		wantReceived int64
	}{
		{
			name: "clip_start with a field of the wrong type",
			frames: []func(uint64) any{
				func(seq uint64) any {
					return map[string]any{"seq": seq, "kind": KindClipStart, "clip_id": 1,
						"filename": 42, "content_type": "text/plain", "metadata": map[string]string{},
						"total_size": len(body), "chunk_count": 1}
				},
				goodChunk, goodEnd,
			},
			wantClips: 1, wantLastSeq: 6, wantReceived: 1,
		},
		{
			name: "clip_chunk with a field of the wrong type",
			frames: []func(uint64) any{
				goodStart,
				func(seq uint64) any {
					return map[string]any{"seq": seq, "kind": KindClipChunk, "clip_id": 1, "index": "zero", "data": body}
				},
				goodEnd,
			},
			wantClips: 1, wantLastSeq: 6, wantReceived: 1,
		},
		{
			name: "clip_end with a field of the wrong type",
			frames: []func(uint64) any{
				goodStart, goodChunk,
				func(seq uint64) any {
					return map[string]any{"seq": seq, "kind": KindClipEnd, "clip_id": 1, "sha256": 7}
				},
			},
			wantClips: 1, wantLastSeq: 6, wantReceived: 1,
		},
		{
			name: "clip_start that is not a map",
			frames: []func(uint64) any{
				func(uint64) any { return 42 },
				goodChunk, goodEnd,
			},
			wantClips: 1, wantLastSeq: 6, wantReceived: 1,
		},
		{
			name: "kind of the wrong type",
			frames: []func(uint64) any{
				goodStart,
				func(seq uint64) any { return map[string]any{"seq": seq, "kind": 3} },
				goodEnd,
			},
			wantClips: 1, wantLastSeq: 6, wantReceived: 1,
		},
		{
			// Between clips nothing is in flight, so the frame costs nothing:
			// the clip on either side of it lands.
			name: "gap with a field of the wrong type, between clips",
			frames: []func(uint64) any{
				goodStart, goodChunk, goodEnd,
				func(uint64) any { return map[string]any{"seq": "later", "kind": KindGap} },
			},
			wantClips: 2, wantLastSeq: 7, wantReceived: 2,
		},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			symkey := bytes.Repeat([]byte{0xE0 + byte(i)}, 32)
			shareID := DeriveShareID(symkey)
			m, db, f := newConsumerFixture(t, symkey)

			stream := &bytes.Buffer{}
			seq := uint64(0)
			for _, frame := range tc.frames {
				seq++
				stream.Write(envelopeAt(t, symkey, shareID, seq, frame(seq)))
			}
			for _, r := range clipRing(t, symkey, shareID, seq+1, 2, healthy, false) {
				stream.Write(r.EnvelopeBytes)
			}

			if err := m.consumeStream(context.Background(), f, stream); !errors.Is(err, io.EOF) {
				t.Fatalf("consumeStream ended with %v, want EOF once drained — any other error is replayed on every reconnect", err)
			}
			if n := clipRowCount(t, db); n != tc.wantClips {
				t.Fatalf("%d clips stored, want %d", n, tc.wantClips)
			}
			var got []byte
			if err := db.QueryRow(`SELECT data FROM clips ORDER BY id DESC LIMIT 1`).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, healthy) {
				t.Fatalf("last stored clip %q, want the healthy clip", got)
			}
			lastSeq, received := followCursorRow(t, db, f.id)
			if lastSeq != tc.wantLastSeq || received != tc.wantReceived {
				t.Fatalf("follows last_seq=%d clips_received=%d, want %d and %d",
					lastSeq, received, tc.wantLastSeq, tc.wantReceived)
			}
		})
	}
}

// The publisher half: a clip_start must reach the wire as valid UTF-8, because
// followers already in the field decode with the strict default and cannot be
// changed by this one. Decoded here with plain cbor.Unmarshal — the deployed
// decoder — rather than UnmarshalPayload.
func TestEmittedClipStartIsValidUTF8(t *testing.T) {
	m, db, info := newSharedPublisher(t)

	r, err := db.Exec(
		`INSERT INTO clips (content_type, data, filename, metadata) VALUES (?, 'body', ?, '{}')`,
		"text/plain\xff", "caf\xe9.txt",
	)
	if err != nil {
		t.Fatal(err)
	}
	clipID, _ := r.LastInsertId()
	var stored string
	if err := db.QueryRow(`SELECT filename FROM clips WHERE id = ?`, clipID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if utf8.ValidString(stored) {
		t.Fatalf("precondition: the database normalized the filename to %q; the test needs raw bytes", stored)
	}
	if _, err := db.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, 1)`, clipID); err != nil {
		t.Fatal(err)
	}
	if err := m.OnClipCreated(clipID, []int64{1}); err != nil {
		t.Fatal(err)
	}

	var seq int64
	var env []byte
	if err := db.QueryRow(
		`SELECT seq, envelope_bytes FROM share_ring WHERE publication_id = ? AND kind = ?`,
		info.ID, KindClipStart,
	).Scan(&seq, &env); err != nil {
		t.Fatal(err)
	}
	m.mu.RLock()
	pub := m.publications[info.ID]
	m.mu.RUnlock()
	pt, err := DecryptEnvelope(pub.symkey, pub.shareID, uint64(seq), env)
	if err != nil {
		t.Fatal(err)
	}
	var start ClipStartPayload
	if err := cbor.Unmarshal(pt, &start); err != nil {
		t.Fatalf("a strict follower cannot decode the emitted clip_start: %v", err)
	}
	if start.Filename != "caf�.txt" {
		t.Fatalf("filename %q, want the invalid byte replaced with U+FFFD", start.Filename)
	}
	if start.ContentType != "text/plain�" {
		t.Fatalf("content_type %q, want the invalid byte replaced with U+FFFD", start.ContentType)
	}
}
