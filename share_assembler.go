package main

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
)

// clipAssembler buffers one in-flight clip (across clip_start / clip_chunk /
// clip_end) to a staging file, then atomically inserts the finished clip
// into SQLite if the SHA-256 matches. A staging file is always removed on
// cleanup — success, SHA mismatch, or mid-stream drop — so the directory
// never leaks bytes.
type clipAssembler struct {
	stagingDir string

	active    bool
	clipID    uint64
	filename  string
	cType     string
	metadata  map[string]string
	totalSize uint64
	chunks    uint32

	file         *os.File
	filePath     string
	writtenBytes uint64
	hasher       hash.Hash
}

func newClipAssembler(stagingDir string) *clipAssembler {
	_ = os.MkdirAll(stagingDir, 0o755)
	return &clipAssembler{stagingDir: stagingDir}
}

func (a *clipAssembler) onStart(p ClipStartPayload) {
	// If a prior clip was mid-stream (missing clip_end), discard it cleanly.
	a.cleanup()
	a.active = true
	a.clipID = p.ClipID
	a.filename = p.Filename
	a.cType = p.ContentType
	a.metadata = p.Metadata
	a.totalSize = p.TotalSize
	a.chunks = p.ChunkCount

	a.filePath = filepath.Join(a.stagingDir, fmt.Sprintf("%d.bin", p.ClipID))
	f, err := os.Create(a.filePath)
	if err != nil {
		a.active = false
		return
	}
	a.file = f
	a.hasher = sha256.New()
	a.writtenBytes = 0
}

func (a *clipAssembler) onChunk(p ClipChunkPayload) {
	if !a.active || p.ClipID != a.clipID || a.file == nil {
		return
	}
	if _, err := a.file.Write(p.Data); err != nil {
		a.cleanup()
		return
	}
	a.hasher.Write(p.Data)
	a.writtenBytes += uint64(len(p.Data))
}

// onEnd finalises a clip: verifies the running SHA-256 against p.SHA256,
// writes the assembled bytes to the clips table, and tags the new clip
// with localTagID. Returns the new clip's ID on success so the caller
// can fetch a preview and notify the frontend.
func (a *clipAssembler) onEnd(p ClipEndPayload, db *sql.DB, localTagID int64) (int64, error) {
	defer a.cleanup()
	if !a.active || p.ClipID != a.clipID || a.file == nil {
		return 0, fmt.Errorf("clip_end without active clip_start")
	}
	if err := a.file.Sync(); err != nil {
		return 0, err
	}
	if _, err := a.file.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	got := a.hasher.Sum(nil)
	if !bytes.Equal(got, p.SHA256) {
		return 0, fmt.Errorf("sha256 mismatch: got %x want %x", got, p.SHA256)
	}
	body, err := io.ReadAll(a.file)
	if err != nil {
		return 0, err
	}
	metaJSON, _ := json.Marshal(a.metadata)

	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(
		`INSERT INTO clips (content_type, data, filename, metadata) VALUES (?, ?, ?, ?)`,
		a.cType, body, a.filename, string(metaJSON),
	)
	if err != nil {
		return 0, err
	}
	newClipID, _ := res.LastInsertId()
	if _, err := tx.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, ?)`, newClipID, localTagID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return newClipID, nil
}

func (a *clipAssembler) cleanup() {
	if a.file != nil {
		a.file.Close()
		_ = os.Remove(a.filePath)
	}
	a.active = false
	a.file = nil
	a.filePath = ""
	a.hasher = nil
}
