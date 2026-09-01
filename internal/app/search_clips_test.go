package app

import (
	"testing"
)

// helper: names of the clips a search returned, for order-insensitive checks.
func searchNames(t *testing.T, app *App, query string, inContent bool, hidden []int64) map[string]bool {
	t.Helper()
	clips, err := app.SearchClips(false, nil, hidden, query, inContent, "date", "desc")
	if err != nil {
		t.Fatalf("SearchClips(%q, content=%v): %v", query, inContent, err)
	}
	got := make(map[string]bool, len(clips))
	for _, c := range clips {
		got[c.Filename] = true
	}
	return got
}

func TestSearchClipsMatchesFilenameAndType(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	if _, err := app.createTestClip("Report-Q1.txt", "text/plain", []byte("nothing here")); err != nil {
		t.Fatal(err)
	}
	if _, err := app.createTestClip("diagram.png", "image/png", []byte{0x89, 'P', 'N', 'G'}); err != nil {
		t.Fatal(err)
	}

	// Filename match is case-insensitive, mirroring the client-side filter.
	if got := searchNames(t, app, "report", false, nil); !got["Report-Q1.txt"] || got["diagram.png"] {
		t.Errorf("filename search returned %v", got)
	}
	// Content type stays searchable — "png" must still find the image.
	if got := searchNames(t, app, "png", false, nil); !got["diagram.png"] {
		t.Errorf("content-type search returned %v", got)
	}
}

func TestSearchClipsMatchesNonASCIIFilename(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	if _, err := app.createTestClip("Äpfel.txt", "text/plain", []byte("Obst")); err != nil {
		t.Fatal(err)
	}

	if got := searchNames(t, app, "ÄPFEL", false, nil); !got["Äpfel.txt"] {
		t.Errorf("non-ASCII filename search returned %v", got)
	}
}

func TestSearchClipsContentOnlyWhenRequested(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	if _, err := app.createTestClip("notes.txt", "text/plain", []byte("the PASSPHRASE is swordfish")); err != nil {
		t.Fatal(err)
	}
	if _, err := app.createTestClip("config.json", "application/json", []byte(`{"key":"swordfish"}`)); err != nil {
		t.Fatal(err)
	}

	if got := searchNames(t, app, "swordfish", false, nil); len(got) != 0 {
		t.Errorf("content match leaked into a filename-only search: %v", got)
	}
	got := searchNames(t, app, "swordfish", true, nil)
	if !got["notes.txt"] || !got["config.json"] {
		t.Errorf("content search returned %v, want both text clips", got)
	}
}

func TestSearchClipsSkipsBinaryContent(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	// A PNG whose bytes happen to spell the query. Scanning binary blobs would
	// produce hits a user can neither see nor explain.
	if _, err := app.createTestClip("photo.png", "image/png", []byte("\x89PNG secretword trailing")); err != nil {
		t.Fatal(err)
	}

	if got := searchNames(t, app, "secretword", true, nil); len(got) != 0 {
		t.Errorf("binary clip matched a content search: %v", got)
	}
}

func TestSearchClipsTreatsWildcardsAsLiterals(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	if _, err := app.createTestClip("100%-done.txt", "text/plain", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := app.createTestClip("plain.txt", "text/plain", []byte("x")); err != nil {
		t.Fatal(err)
	}

	got := searchNames(t, app, "100%", false, nil)
	if !got["100%-done.txt"] {
		t.Errorf("literal %% did not match its own filename: %v", got)
	}
	if got["plain.txt"] {
		t.Errorf("%% was treated as a wildcard: %v", got)
	}

	// "_" is the single-character wildcard; as a literal it must not match "ab".
	if got := searchNames(t, app, "_", false, nil); len(got) != 0 {
		t.Errorf("_ was treated as a wildcard: %v", got)
	}
}

func TestSearchClipsHonorsHiddenTags(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	clipID, err := app.createTestClip("secret-notes.txt", "text/plain", []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	tagID := createTestTag(t, app.db, "private", "#000000")
	if _, err := app.db.Exec("INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, ?)", clipID, tagID); err != nil {
		t.Fatal(err)
	}

	if got := searchNames(t, app, "secret", false, []int64{tagID}); len(got) != 0 {
		t.Errorf("hidden clip surfaced while its tag was hidden: %v", got)
	}
	// An empty hidden list is how the UI's "show hidden clips" option asks for them.
	if got := searchNames(t, app, "secret", false, nil); !got["secret-notes.txt"] {
		t.Errorf("hidden clip missing when hiding was waived: %v", got)
	}
}

func TestSearchClipsEmptyQueryListsEverything(t *testing.T) {
	app, cleanup := setupTestDBWithTags(t)
	defer cleanup()

	if _, err := app.createTestClip("a.txt", "text/plain", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := app.createTestClip("b.txt", "text/plain", []byte("y")); err != nil {
		t.Fatal(err)
	}

	if got := searchNames(t, app, "   ", true, nil); len(got) != 2 {
		t.Errorf("blank query filtered the listing: %v", got)
	}
}
