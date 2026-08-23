package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

func TestAssemblerWritesClipAndTag(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (42, 'inbox', '#888')`); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	asm := newClipAssembler(dir, 7)

	data := bytes.Repeat([]byte("hello"), 1000)
	h := sha256.Sum256(data)
	meta := map[string]string{"from": "pub"}

	asm.onStart(ClipStartPayload{
		ClipID: 1, Filename: "m.txt", ContentType: "text/plain",
		Metadata: meta, TotalSize: uint64(len(data)), ChunkCount: 1,
	})
	asm.onChunk(ClipChunkPayload{ClipID: 1, Index: 0, Data: data})
	if _, err := asm.onEnd(ClipEndPayload{ClipID: 1, SHA256: h[:]}, db, 42, nil); err != nil {
		t.Fatal(err)
	}

	var clipID int64
	var content []byte
	var filename, ctype, metaJSON string
	if err := db.QueryRow(`SELECT id, data, filename, content_type, metadata FROM clips LIMIT 1`).Scan(&clipID, &content, &filename, &ctype, &metaJSON); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(content, data) {
		t.Fatalf("content mismatch")
	}
	if filename != "m.txt" {
		t.Fatalf("filename %q", filename)
	}
	if ctype != "text/plain" {
		t.Fatalf("ctype %q", ctype)
	}
	var gotMeta map[string]string
	if err := json.Unmarshal([]byte(metaJSON), &gotMeta); err != nil {
		t.Fatal(err)
	}
	if gotMeta["from"] != "pub" {
		t.Fatalf("meta %+v", gotMeta)
	}

	var tagID int64
	if err := db.QueryRow(`SELECT tag_id FROM clip_tags WHERE clip_id = ?`, clipID).Scan(&tagID); err != nil {
		t.Fatal(err)
	}
	if tagID != 42 {
		t.Fatalf("tagID %d", tagID)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("staging dir not empty: %v", entries)
	}
}

func TestAssemblerPromotesMarkdownFilename(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (42, 'docs', '#888')`); err != nil {
		t.Fatal(err)
	}
	asm := newClipAssembler(t.TempDir(), 7)
	data := []byte("# Shared")
	hash := sha256.Sum256(data)

	asm.onStart(ClipStartPayload{
		ClipID: 1, Filename: "shared.MD", ContentType: "application/octet-stream",
		TotalSize: uint64(len(data)), ChunkCount: 1,
	})
	asm.onChunk(ClipChunkPayload{ClipID: 1, Index: 0, Data: data})
	id, err := asm.onEnd(ClipEndPayload{ClipID: 1, SHA256: hash[:]}, db, 42, nil)
	if err != nil {
		t.Fatalf("onEnd: %v", err)
	}

	var contentType string
	if err := db.QueryRow(`SELECT content_type FROM clips WHERE id = ?`, id).Scan(&contentType); err != nil {
		t.Fatalf("query content type: %v", err)
	}
	if contentType != "text/markdown" {
		t.Fatalf("content type = %q, want text/markdown", contentType)
	}
}

func TestAssemblerRejectsWrongSHA256(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (42, 'x', '#888')`); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	asm := newClipAssembler(dir, 7)
	asm.onStart(ClipStartPayload{ClipID: 1, TotalSize: 5, ChunkCount: 1})
	asm.onChunk(ClipChunkPayload{ClipID: 1, Index: 0, Data: []byte("hello")})
	wrong := bytes.Repeat([]byte{0}, 32)
	if _, err := asm.onEnd(ClipEndPayload{ClipID: 1, SHA256: wrong}, db, 42, nil); err == nil {
		t.Fatal("expected sha mismatch error")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("staging leak on error: %v", entries)
	}
}

// Two follows staging their own remote clip 1 at the same time used to share
// one staging path, so each os.Create truncated the other's in-flight file and
// the writes interleaved through separate fds. Nothing caught it: the integrity
// check hashes the wire bytes, not the file, so the trampled body went into the
// clips table looking valid. The interleave below is the same sequence the two
// stream readers would produce, driven deterministically.
func TestAssemblersWithSameRemoteClipIDDoNotCollide(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (42, 'a', '#888'), (43, 'b', '#888')`); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	asmA := newClipAssembler(dir, 1)
	asmB := newClipAssembler(dir, 2)

	// Equal lengths so a collision overwrites rather than truncates — the
	// quietest version of the corruption, and the one worth pinning down.
	dataA := bytes.Repeat([]byte("aaaa"), 2000)
	dataB := bytes.Repeat([]byte("bbbb"), 2000)
	hashA := sha256.Sum256(dataA)
	hashB := sha256.Sum256(dataB)

	asmA.onStart(ClipStartPayload{ClipID: 1, Filename: "a.txt", ContentType: "text/plain", TotalSize: uint64(len(dataA)), ChunkCount: 1})
	asmB.onStart(ClipStartPayload{ClipID: 1, Filename: "b.txt", ContentType: "text/plain", TotalSize: uint64(len(dataB)), ChunkCount: 1})
	if asmA.filePath == asmB.filePath {
		t.Fatalf("both assemblers staged to %s", asmA.filePath)
	}

	asmA.onChunk(ClipChunkPayload{ClipID: 1, Index: 0, Data: dataA})
	asmB.onChunk(ClipChunkPayload{ClipID: 1, Index: 0, Data: dataB})

	idA, err := asmA.onEnd(ClipEndPayload{ClipID: 1, SHA256: hashA[:]}, db, 42, nil)
	if err != nil {
		t.Fatalf("onEnd A: %v", err)
	}
	idB, err := asmB.onEnd(ClipEndPayload{ClipID: 1, SHA256: hashB[:]}, db, 43, nil)
	if err != nil {
		t.Fatalf("onEnd B: %v", err)
	}

	for _, tc := range []struct {
		name string
		id   int64
		want []byte
	}{{"A", idA, dataA}, {"B", idB, dataB}} {
		var got []byte
		if err := db.QueryRow(`SELECT data FROM clips WHERE id = ?`, tc.id).Scan(&got); err != nil {
			t.Fatalf("query clip %s: %v", tc.name, err)
		}
		if !bytes.Equal(got, tc.want) {
			t.Fatalf("clip %s: stored %d bytes, want %d matching bytes", tc.name, len(got), len(tc.want))
		}
	}

	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("staging dir not empty: %v", entries)
	}
}

// Restaging the same remote clip — what every reconnect mid-clip does — must
// not reuse the name of a file some other in-flight assembler still holds open.
func TestAssemblerStagingNamesAreUniquePerClip(t *testing.T) {
	dir := t.TempDir()
	asm := newClipAssembler(dir, 1)
	seen := map[string]bool{}
	for i := 0; i < 5; i++ {
		asm.onStart(ClipStartPayload{ClipID: 1, TotalSize: 1, ChunkCount: 1})
		if asm.filePath == "" {
			t.Fatal("onStart did not stage a file")
		}
		if seen[asm.filePath] {
			t.Fatalf("staging path %s reused", asm.filePath)
		}
		seen[asm.filePath] = true
	}
	asm.cleanup()
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("staging dir not empty: %v", entries)
	}
}

// A received clip must carry the same content_hash an upload of the same bytes
// would produce, or it is invisible to dedup — the duplicate group never forms
// and a re-published copy cannot be detected.
func TestAssemblerRecordsUploadCompatibleContentHash(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (42, 'inbox', '#888')`); err != nil {
		t.Fatal(err)
	}
	asm := newClipAssembler(t.TempDir(), 3)

	data := bytes.Repeat([]byte("dedupe me"), 512)
	h := sha256.Sum256(data)
	// Spelled out rather than calling computeContentHash, so the test pins the
	// encoding (lowercase hex) instead of tracking whatever that helper does.
	want := hex.EncodeToString(h[:])

	asm.onStart(ClipStartPayload{
		ClipID: 1, Filename: "d.txt", ContentType: "text/plain",
		TotalSize: uint64(len(data)), ChunkCount: 1,
	})
	asm.onChunk(ClipChunkPayload{ClipID: 1, Index: 0, Data: data})
	clipID, err := asm.onEnd(ClipEndPayload{ClipID: 1, SHA256: h[:]}, db, 42, nil)
	if err != nil {
		t.Fatal(err)
	}

	var got string
	if err := db.QueryRow(`SELECT content_hash FROM clips WHERE id = ?`, clipID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("content_hash %q want %q", got, want)
	}
	if got != computeContentHash(data) {
		t.Fatalf("content_hash %q disagrees with computeContentHash %q", got, computeContentHash(data))
	}
}
