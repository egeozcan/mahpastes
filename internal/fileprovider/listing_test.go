package fileprovider

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

func TestListReadsProjectionWithoutSnapshots(t *testing.T) {
	s, db := testStore(t)
	ctx := context.Background()
	insertClip(t, db, "hello.txt", []byte("hello"))
	execSQL(t, db, `INSERT INTO tags(id,name) VALUES(1,'work/notes')`)
	execSQL(t, db, `INSERT INTO clip_tags VALUES(1,1)`)
	if _, err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	root, err := s.List(ctx, "root")
	if err != nil {
		t.Fatal(err)
	}
	if len(root) != 3 || root[0].Name != "Active" || root[1].Name != "Archive" || root[2].Name != "Tags" {
		t.Fatalf("root = %+v", root)
	}
	active, err := s.List(ctx, "active")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := fileIn(active); !ok || len(active) != 1 {
		t.Fatalf("active = %+v", active)
	}
	tags, err := s.List(ctx, "tags")
	if err != nil {
		t.Fatal(err)
	}
	work, ok := itemNamed(tags, "work")
	if !ok || !work.Folder {
		t.Fatalf("tags = %+v", tags)
	}
	notes, err := s.List(ctx, work.ID)
	if err != nil {
		t.Fatal(err)
	}
	notesFolder, ok := itemNamed(notes, "notes")
	if !ok {
		t.Fatalf("work = %+v", notes)
	}
	aliases, err := s.List(ctx, notesFolder.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := fileIn(aliases); !ok {
		t.Fatalf("notes = %+v", aliases)
	}

	var snapshots int
	if err = db.QueryRow(`SELECT count(*) FROM fp_snapshots`).Scan(&snapshots); err != nil {
		t.Fatal(err)
	}
	if snapshots != 0 {
		t.Fatalf("List created %d snapshots", snapshots)
	}
	for _, scope := range []string{"working", "tag:missing", "nonsense"} {
		if _, err = s.List(ctx, scope); !errors.Is(err, ErrNoSuchItem) {
			t.Fatalf("List(%q) error = %v", scope, err)
		}
	}
}

func TestContentPinsVersion(t *testing.T) {
	s, db := testStore(t)
	ctx := context.Background()
	data := bytes.Repeat([]byte("0123456789"), 300_000)
	insertClip(t, db, "big.txt", data)
	if _, err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	active, err := s.List(ctx, "active")
	if err != nil {
		t.Fatal(err)
	}
	item, _ := fileIn(active)
	read := func(version string) ([]byte, error) {
		var buf bytes.Buffer
		err := s.Content(ctx, item.ID, version, func(Item) (io.Writer, error) { return &buf, nil })
		return buf.Bytes(), err
	}

	got, err := read(item.ContentVersion)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("read %d bytes, want %d identical bytes", len(got), len(data))
	}

	execSQL(t, db, `UPDATE clips SET data=? WHERE id=1`, []byte("changed"))
	if _, err = read(item.ContentVersion); !errors.Is(err, ErrVersion) {
		t.Fatalf("stale version read error = %v", err)
	}
	fresh, err := s.Item(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got, err = read(fresh.ContentVersion); err != nil || string(got) != "changed" {
		t.Fatalf("fresh read = %q, %v", got, err)
	}

	execSQL(t, db, `UPDATE clips SET expires_at='2000-01-01 00:00:00' WHERE id=1`)
	if _, err = read(""); !errors.Is(err, ErrNoSuchItem) {
		t.Fatalf("expired read error = %v", err)
	}
}
