package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func addTestWatchFolder(t *testing.T, a *App, cfg WatchedFolderConfig) *WatchedFolder {
	t.Helper()
	cfg.Path = t.TempDir()
	f, err := a.AddWatchedFolder(cfg)
	if err != nil {
		t.Fatalf("AddWatchedFolder: %v", err)
	}
	return f
}

// The desktop edit modal sends the whole config. Turning auto-archive off,
// clearing the auto-tag or clearing the regex must stick — previously only
// truthy values overwrote the stored ones.
func TestUpdateWatchedFolderClearsFields(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()

	tag, err := a.CreateTag("inbox")
	if err != nil {
		t.Fatal(err)
	}
	tagID := tag.ID
	f := addTestWatchFolder(t, a, WatchedFolderConfig{
		FilterMode:    "custom",
		FilterPresets: []string{"images"},
		FilterRegex:   `\.png$`,
		AutoArchive:   true,
		AutoTagID:     &tagID,
	})

	if err := a.UpdateWatchedFolder(f.ID, WatchedFolderConfig{
		FilterMode:    "all",
		FilterPresets: []string{},
		FilterRegex:   "",
		AutoArchive:   false,
		AutoTagID:     nil,
	}); err != nil {
		t.Fatalf("UpdateWatchedFolder: %v", err)
	}

	got, err := a.GetWatchedFolderByID(f.ID)
	if err != nil || got == nil {
		t.Fatalf("GetWatchedFolderByID: %v", err)
	}
	if got.FilterMode != "all" {
		t.Errorf("FilterMode = %q, want all", got.FilterMode)
	}
	if len(got.FilterPresets) != 0 {
		t.Errorf("FilterPresets = %v, want empty", got.FilterPresets)
	}
	if got.FilterRegex != "" {
		t.Errorf("FilterRegex = %q, want empty", got.FilterRegex)
	}
	if got.AutoArchive {
		t.Error("AutoArchive stayed true after unchecking")
	}
	if got.AutoTagID != nil {
		t.Errorf("AutoTagID = %d, want nil", *got.AutoTagID)
	}
}

// An invalid regex used to be stored and then silently matched nothing.
func TestWatchedFolderRejectsInvalidRegex(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()

	_, err := a.AddWatchedFolder(WatchedFolderConfig{Path: t.TempDir(), FilterMode: "custom", FilterRegex: "a(b"})
	if err == nil || !strings.Contains(err.Error(), "invalid filter regex") {
		t.Fatalf("AddWatchedFolder with bad regex: err = %v, want invalid filter regex", err)
	}

	f := addTestWatchFolder(t, a, WatchedFolderConfig{FilterMode: "custom", FilterRegex: `\.txt$`})

	err = a.UpdateWatchedFolder(f.ID, WatchedFolderConfig{FilterMode: "custom", FilterRegex: "[unclosed"})
	if err == nil || !strings.Contains(err.Error(), "invalid filter regex") {
		t.Fatalf("UpdateWatchedFolder with bad regex: err = %v", err)
	}

	err = a.UpdateWatchedFolderPartial(f.ID, map[string]json.RawMessage{"filter_regex": json.RawMessage(`"*bad"`)})
	if err == nil || !strings.Contains(err.Error(), "invalid filter regex") {
		t.Fatalf("UpdateWatchedFolderPartial with bad regex: err = %v", err)
	}

	got, _ := a.GetWatchedFolderByID(f.ID)
	if got.FilterRegex != `\.txt$` {
		t.Fatalf("stored regex changed to %q after rejected updates", got.FilterRegex)
	}
}

// A partial update that does not touch filter_regex still refuses a stored
// regex that no longer compiles (one restored from a backup, say), but says
// it is the stored value — the request itself sent nothing wrong. A newly
// supplied bad regex keeps the plain message, and supplying a valid one is
// how the stored value gets fixed.
func TestUpdateWatchedFolderPartialNamesStoredBadRegex(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()

	f := addTestWatchFolder(t, a, WatchedFolderConfig{FilterMode: "all"})
	if _, err := a.db.Exec(`UPDATE watched_folders SET filter_regex = '(' WHERE id = ?`, f.ID); err != nil {
		t.Fatal(err)
	}

	err := a.UpdateWatchedFolderPartial(f.ID, map[string]json.RawMessage{"auto_archive": json.RawMessage(`true`)})
	if err == nil || !strings.Contains(err.Error(), "stored filter regex") {
		t.Fatalf("untouched bad regex: err = %v, want one naming the stored filter regex", err)
	}

	err = a.UpdateWatchedFolderPartial(f.ID, map[string]json.RawMessage{"filter_regex": json.RawMessage(`"["`)})
	if err == nil || strings.Contains(err.Error(), "stored") || !strings.Contains(err.Error(), "invalid filter regex") {
		t.Fatalf("new bad regex: err = %v, want a plain invalid-regex error", err)
	}

	if err := a.UpdateWatchedFolderPartial(f.ID, map[string]json.RawMessage{
		"filter_regex": json.RawMessage(`"\\.png$"`),
		"auto_archive": json.RawMessage(`true`),
	}); err != nil {
		t.Fatalf("fixing the regex: %v", err)
	}
}
