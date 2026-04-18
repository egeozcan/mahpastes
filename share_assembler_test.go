package main

import (
	"bytes"
	"crypto/sha256"
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
	asm := newClipAssembler(dir)

	data := bytes.Repeat([]byte("hello"), 1000)
	h := sha256.Sum256(data)
	meta := map[string]string{"from": "pub"}

	asm.onStart(ClipStartPayload{
		ClipID: 1, Filename: "m.txt", ContentType: "text/plain",
		Metadata: meta, TotalSize: uint64(len(data)), ChunkCount: 1,
	})
	asm.onChunk(ClipChunkPayload{ClipID: 1, Index: 0, Data: data})
	if _, err := asm.onEnd(ClipEndPayload{ClipID: 1, SHA256: h[:]}, db, 42); err != nil {
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

func TestAssemblerRejectsWrongSHA256(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (42, 'x', '#888')`); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	asm := newClipAssembler(dir)
	asm.onStart(ClipStartPayload{ClipID: 1, TotalSize: 5, ChunkCount: 1})
	asm.onChunk(ClipChunkPayload{ClipID: 1, Index: 0, Data: []byte("hello")})
	wrong := bytes.Repeat([]byte{0}, 32)
	if _, err := asm.onEnd(ClipEndPayload{ClipID: 1, SHA256: wrong}, db, 42); err == nil {
		t.Fatal("expected sha mismatch error")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("staging leak on error: %v", entries)
	}
}
