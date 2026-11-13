package app

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

// UploadFiles used to swallow a per-file failure and return nil, so the UI
// announced "N uploaded" for a file that was never stored. It still stores the
// rest of the batch, and now says which files it could not.
func TestUploadFilesReportsFilesItCouldNotStore(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	good := func(name string) FileData {
		return FileData{Name: name, ContentType: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte("body of " + name))}
	}
	err := app.UploadFiles([]FileData{
		good("a.txt"),
		{Name: "broken.bin", ContentType: "application/octet-stream", Data: "%%% not base64 %%%"},
		good("b.txt"),
	}, 0, 0)

	var upErr *UploadFilesError
	if !errors.As(err, &upErr) {
		t.Fatalf("UploadFiles error = %v, want an *UploadFilesError", err)
	}
	if upErr.Total != 3 || len(upErr.Failed) != 1 || upErr.Failed[0].Name != "broken.bin" {
		t.Fatalf("error = %+v, want 1 of 3 failed: broken.bin", upErr)
	}
	if got := err.Error(); !strings.HasPrefix(got, "1 of 3 files could not be stored: broken.bin (") {
		t.Fatalf("error text = %q", got)
	}

	// The other two were stored.
	var n int
	if err := app.db.QueryRow("SELECT COUNT(*) FROM clips WHERE filename IN ('a.txt', 'b.txt')").Scan(&n); err != nil || n != 2 {
		t.Fatalf("stored good files = %d (err %v), want 2", n, err)
	}

	// A batch with nothing wrong still returns nil.
	if err := app.UploadFiles([]FileData{good("c.txt")}, 0, 0); err != nil {
		t.Fatalf("clean batch: %v", err)
	}
}
