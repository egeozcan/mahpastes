package app

import (
	"encoding/base64"
	"os"
	"testing"
)

// Replacing a clip's bytes must invalidate its leased temp file. The name is
// derived from the clip ID, and media playback reuses a leased file rather than
// recopying, so without invalidation the previous revision stays playable for
// the rest of the lease.
func TestPrepareClipMediaItemRebuildsAfterClipUpdate(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()
	dir := t.TempDir()
	app.tempDir = dir
	app.tempStore = NewTempClipStore(app.db, dir, 0, 0)
	app.transferHandler = NewTransferFileHandler(app)

	id, err := app.createTestClip("v.mp4", "video/mp4", []byte("ORIGINAL"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := app.PrepareClipMediaItem(id)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(first.AbsPath); string(b) != "ORIGINAL" {
		t.Fatalf("temp file = %q", b)
	}

	if err := app.UpdateClipData(id, "video/mp4", base64.StdEncoding.EncodeToString([]byte("REPLACED")), "v.mp4"); err != nil {
		t.Fatal(err)
	}
	second, err := app.PrepareClipMediaItem(id)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(second.AbsPath)
	if string(b) != "REPLACED" {
		t.Fatalf("stale bytes served after update: %q", b)
	}
}
