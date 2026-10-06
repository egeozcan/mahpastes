package app

import (
	"encoding/json"
	"testing"
)

// The gallery patches a card from GetClipPreview after a single-clip change
// and later compares that card's signature with the next listing's row. Every
// field the card shows must therefore serialize exactly as the listing does,
// including what the backend decided on its own (expiry on its clock, the
// Markdown type a .md rename gets).
func TestGetClipPreviewMatchesListingRow(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()

	id := insertTestClip(t, a, "notes.txt", "text/plain", []byte("hello preview"))
	tag := mustCreateTag(t, a, "proj/a")
	if err := a.SetExpiration(id, 90); err != nil {
		t.Fatal(err)
	}
	if err := a.RenameClip(id, "notes.md"); err != nil {
		t.Fatal(err)
	}
	if err := a.AddTagToClip(id, tag); err != nil {
		t.Fatal(err)
	}

	page, err := a.ListClipsPage(ClipListRequest{Limit: defaultClipLimit})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Clips) != 1 {
		t.Fatalf("listing has %d clips, want 1", len(page.Clips))
	}
	row, err := a.GetClipPreview(id)
	if err != nil {
		t.Fatal(err)
	}
	if row.ExpiresAt == nil {
		t.Fatal("GetClipPreview lost expires_at")
	}

	fields := func(c ClipPreview) map[string]json.RawMessage {
		raw, _ := json.Marshal(c)
		var m map[string]json.RawMessage
		_ = json.Unmarshal(raw, &m)
		delete(m, "duplicate_count") // not computed for one row
		return m
	}
	want, got := fields(page.Clips[0]), fields(*row)
	for k, v := range want {
		if string(got[k]) != string(v) {
			t.Errorf("%s: GetClipPreview %s, listing %s", k, got[k], v)
		}
	}
	if _, err := a.GetClipPreview(id + 1000); err == nil {
		t.Error("GetClipPreview of a missing clip succeeded")
	}
}
