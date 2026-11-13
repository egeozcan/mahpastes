//go:build windows

package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A program the user opened a clip in (Word, Acrobat) keeps the temp file
// open without FILE_SHARE_DELETE, so Windows refuses to delete it. That one
// file must not fail the prune every Open, Copy as File and drag-out runs
// first — otherwise nothing can be opened until that program is closed.
func TestPrune_FileOpenInAnotherProgramDoesNotFailThePrune(t *testing.T) {
	store, db := newConcurrencyTestStore(t)
	if _, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (1, 'text/plain', 'a', 'a.txt'), (2, 'text/plain', 'b', 'b.txt')`); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(store.dir, "1_a.txt")
	orphan := filepath.Join(store.dir, "999_gone.txt")
	fresh := filepath.Join(store.dir, "2_b.txt")
	for _, f := range []string{stale, orphan, fresh} {
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	// os.Open shares read and write but not delete, like most editors.
	var held []*os.File
	for _, f := range []string{stale, orphan} {
		h, err := os.Open(f)
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, h)
	}
	closeHeld := func() {
		for _, h := range held {
			h.Close()
		}
		held = nil
	}
	defer closeHeld()

	if err := store.Prune(true); err != nil {
		t.Fatalf("prune failed because a file was in use: %v", err)
	}
	for _, f := range []string{stale, orphan} {
		if _, err := os.Stat(f); err != nil {
			t.Fatalf("%s should still be on disk while it is open: %v", filepath.Base(f), err)
		}
	}
	// Neither may be handed out again: one's lease is over, the other's clip is gone.
	if got, err := store.FindExistingClipFile(1); err != nil || got != nil {
		t.Fatalf("expired file still offered: %+v, %v", got, err)
	}
	if got, err := store.FindExistingClipFile(2); err != nil || got == nil {
		t.Fatalf("leased file lost from the index: %+v, %v", got, err)
	}
	// Other clips still prepare.
	if _, err := store.PrepareClipFile(2); err != nil {
		t.Fatalf("PrepareClipFile for another clip: %v", err)
	}

	// Once the program lets go, the next prune removes them.
	closeHeld()
	if err := store.Prune(true); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{stale, orphan} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Fatalf("%s not removed after it was closed: %v", filepath.Base(f), err)
		}
	}
}

// Clip names come from macOS and Linux peers, backups and REST clients, where
// ? * : " < > | are ordinary characters. Windows refuses them in a file name
// (and reads "a: b.txt" as a stream of the file "a"), so the temp copy that
// Open, Open With, Copy as File and drag-out hand out needs a name it accepts.
func TestPrepareClipFile_NamesWindowsRefuses(t *testing.T) {
	store, db := newConcurrencyTestStore(t)
	names := []string{`Q3 plan?.md`, `notes: todo.txt`, `a<b>c|d"e*.txt`, `trailing dot.`, `tab	inside.txt`}
	for i, name := range names {
		id := int64(i + 1)
		if _, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES (?, 'text/plain', ?, ?)`, id, []byte("body of "+name), name); err != nil {
			t.Fatal(err)
		}
		prepared, err := store.PrepareClipFile(id)
		if err != nil {
			t.Errorf("PrepareClipFile(%q): %v", name, err)
			continue
		}
		got, err := os.ReadFile(prepared.AbsPath)
		if err != nil {
			t.Errorf("read prepared file for %q: %v", name, err)
			continue
		}
		if string(got) != "body of "+name {
			t.Errorf("prepared file for %q holds %q", name, got)
		}
		if clipID, ok := parseClipIDFromTempFilename(filepath.Base(prepared.AbsPath)); !ok || clipID != id {
			t.Errorf("prepared name %q does not parse back to clip %d", filepath.Base(prepared.AbsPath), id)
		}
	}
}
