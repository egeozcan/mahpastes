//go:build linux && !bindings

package fpfuse

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	_ "modernc.org/sqlite"

	"go-clipboard/internal/fileprovider"
)

func testMount(t *testing.T) (*sql.DB, string) {
	t.Helper()
	if err := Available(); err != nil {
		t.Skip(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "clips.db")+"?_pragma=journal_mode%3Dwal&_pragma=foreign_keys%3Don&_pragma=busy_timeout%3D5000")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err = db.Exec(`CREATE TABLE clips(id INTEGER PRIMARY KEY AUTOINCREMENT,data BLOB NOT NULL,filename TEXT,content_type TEXT NOT NULL,created_at TEXT DEFAULT CURRENT_TIMESTAMP,is_archived INTEGER DEFAULT 0,expires_at TEXT,content_hash TEXT DEFAULT '',metadata TEXT DEFAULT '{}'); CREATE TABLE settings(key TEXT PRIMARY KEY,value TEXT); CREATE TABLE tags(id INTEGER PRIMARY KEY,name TEXT); CREATE TABLE clip_tags(clip_id INTEGER REFERENCES clips(id) ON DELETE CASCADE,tag_id INTEGER REFERENCES tags(id) ON DELETE CASCADE,PRIMARY KEY(clip_id,tag_id));`); err != nil {
		t.Fatal(err)
	}
	store, err := fileprovider.Open(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "Mahpastes")
	m, err := Start(context.Background(), store, dir)
	if err != nil {
		t.Skipf("FUSE mount unavailable here: %v", err)
	}
	t.Cleanup(m.Close)
	return db, dir
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}

// eventually retries until the projection worker and kernel caches catch up.
func eventually(t *testing.T, what string, check func() error) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := check()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: %v", what, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func onlyFile(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	if len(entries) != 1 {
		return "", errors.New("want exactly one entry")
	}
	return filepath.Join(dir, entries[0].Name()), nil
}

func TestMountServesReadOnlyProjection(t *testing.T) {
	db, dir := testMount(t)
	data := bytes.Repeat([]byte("mahpastes "), 500_000)
	mustExec(t, db, `INSERT INTO clips(data,filename,content_type) VALUES(?,?,'text/plain')`, data, "big.txt")
	mustExec(t, db, `INSERT INTO tags(id,name) VALUES(1,'work'),(2,'secret')`)
	mustExec(t, db, `INSERT INTO clip_tags VALUES(1,1)`)

	var path string
	eventually(t, "clip appears in Active", func() (err error) {
		path, err = onlyFile(filepath.Join(dir, "Active"))
		return err
	})
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("read %d bytes (%v), want %d", len(got), err, len(data))
	}
	if !strings.HasPrefix(filepath.Base(path), "big [") {
		t.Fatalf("unexpected name %q", path)
	}
	if _, err = onlyFile(filepath.Join(dir, "Tags", "work")); err != nil {
		t.Fatalf("tag alias: %v", err)
	}

	for name, op := range map[string]func() error{
		"write":  func() error { return os.WriteFile(path, []byte("x"), 0o644) },
		"create": func() error { return os.WriteFile(filepath.Join(dir, "Active", "new.txt"), nil, 0o644) },
		"rename": func() error { return os.Rename(path, path+".bak") },
		"remove": func() error { return os.Remove(path) },
		"mkdir":  func() error { return os.Mkdir(filepath.Join(dir, "Active", "d"), 0o755) },
	} {
		if err := op(); !errors.Is(err, syscall.EROFS) {
			t.Errorf("%s error = %v, want EROFS", name, err)
		}
	}

	mustExec(t, db, `UPDATE clips SET data=? WHERE id=1`, []byte("edited"))
	eventually(t, "edit visible", func() error {
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if string(b) != "edited" {
			return errors.New("still old content")
		}
		return nil
	})

	// Hidden tags become dot folders; their canonical clips leave Active.
	mustExec(t, db, `INSERT INTO clip_tags VALUES(1,2)`)
	mustExec(t, db, `INSERT INTO settings VALUES('hidden_tags','[2]')`)
	eventually(t, "hidden tag projected", func() error {
		if _, err := os.Stat(filepath.Join(dir, "Tags", ".secret")); err != nil {
			return err
		}
		entries, err := os.ReadDir(filepath.Join(dir, "Active"))
		if err != nil {
			return err
		}
		if len(entries) != 0 {
			return errors.New("hidden clip still in Active")
		}
		return nil
	})
}

func TestStartRefusesNonEmptyDirectory(t *testing.T) {
	if err := Available(); err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := prepare(dir); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("prepare error = %v", err)
	}
}

func TestStartRefusesLiveMount(t *testing.T) {
	_, dir := testMount(t)
	if !mounted(dir) {
		t.Fatal("mounted does not recognise the live mount")
	}
	if _, err := prepare(dir); err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("prepare error = %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("live mount was detached: %v", err)
	}
}

func TestReadsLargeClip(t *testing.T) {
	db, dir := testMount(t)
	data := bytes.Repeat([]byte("0123456789"), 300_000)
	mustExec(t, db, `INSERT INTO clips(data,filename,content_type) VALUES(?,?,?)`, data, "big.txt", "text/plain")
	var path string
	eventually(t, "large clip listed", func() (err error) {
		path, err = onlyFile(filepath.Join(dir, "Active"))
		return err
	})
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("read %d bytes, want %d identical bytes", len(got), len(data))
	}
}

func TestReadAfterChangeFailsInsteadOfTruncating(t *testing.T) {
	db, dir := testMount(t)
	mustExec(t, db, `INSERT INTO clips(data,filename,content_type) VALUES(?,?,?)`, bytes.Repeat([]byte("x"), 1<<20), "big.txt", "text/plain")
	var path string
	eventually(t, "clip listed", func() (err error) {
		path, err = onlyFile(filepath.Join(dir, "Active"))
		return err
	})
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err = f.Read(make([]byte, 4096)); err != nil {
		t.Fatal(err)
	}
	// The new revision still extends past the read position; reads beyond the
	// new size end at EOF in the kernel before reaching the server.
	mustExec(t, db, `UPDATE clips SET data=? WHERE id=1`, bytes.Repeat([]byte("y"), 10_000))
	eventually(t, "size follows the change", func() error {
		st, err := os.Stat(path)
		if err != nil {
			return err
		}
		if st.Size() != 10_000 {
			return errors.New("size not updated yet")
		}
		return nil
	})
	// The handler answers ESTALE; a buffered read reports it as EIO.
	if _, err = io.ReadAll(f); !errors.Is(err, syscall.ESTALE) && !errors.Is(err, syscall.EIO) {
		t.Fatalf("read after change error = %v, want ESTALE or EIO", err)
	}
}

func TestRelativeMountDir(t *testing.T) {
	if err := Available(); err != nil {
		t.Skip(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "clips.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	mustExec(t, db, `CREATE TABLE clips(id INTEGER PRIMARY KEY AUTOINCREMENT,data BLOB NOT NULL,filename TEXT,content_type TEXT NOT NULL,created_at TEXT DEFAULT CURRENT_TIMESTAMP,is_archived INTEGER DEFAULT 0,expires_at TEXT,content_hash TEXT DEFAULT '',metadata TEXT DEFAULT '{}'); CREATE TABLE settings(key TEXT PRIMARY KEY,value TEXT); CREATE TABLE tags(id INTEGER PRIMARY KEY,name TEXT); CREATE TABLE clip_tags(clip_id INTEGER,tag_id INTEGER,PRIMARY KEY(clip_id,tag_id));`)
	store, err := fileprovider.Open(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	m, err := Start(context.Background(), store, "Rel")
	if err != nil {
		t.Skipf("FUSE mount unavailable here: %v", err)
	}
	if !m.Alive() {
		m.Close()
		t.Fatal("relative mount reported as not alive")
	}
	m.Close()
	if mounted(m.Dir()) {
		t.Fatal("still mounted after Close")
	}
}

func entryWithPrefix(t *testing.T, dir, prefix string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			return filepath.Join(dir, e.Name())
		}
	}
	t.Fatalf("no entry starting with %q in %s", prefix, dir)
	return ""
}

// waitEvent waits for an fsnotify event on name with the given operation.
func waitEvent(t *testing.T, events <-chan fsnotify.Event, what string, match func(fsnotify.Event) bool) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case e := <-events:
			if match(e) {
				return
			}
		case <-timeout:
			t.Fatalf("no %s event", what)
		}
	}
}

func TestDirectoryWatchersReceiveProjectionChanges(t *testing.T) {
	db, dir := testMount(t)
	mustExec(t, db, `INSERT INTO clips(data,filename,content_type) VALUES('first','first.txt','text/plain')`)
	active := filepath.Join(dir, "Active")
	tags := filepath.Join(dir, "Tags")
	eventually(t, "first clip listed", func() error {
		_, err := onlyFile(active)
		return err
	})
	if _, err := os.ReadDir(tags); err != nil {
		t.Fatal(err)
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for _, d := range []string{active, tags} {
		if err := w.Add(d); err != nil {
			t.Fatal(err)
		}
	}
	isFile := func(op fsnotify.Op, stem string) func(fsnotify.Event) bool {
		return func(e fsnotify.Event) bool {
			return e.Has(op) && filepath.Dir(e.Name) == active && strings.HasPrefix(filepath.Base(e.Name), stem+" [")
		}
	}

	before, err := os.Stat(active)
	if err != nil {
		t.Fatal(err)
	}
	if before.ModTime().Before(time.Now().Add(-time.Minute)) {
		t.Fatalf("Active reports mtime %v", before.ModTime())
	}
	mustExec(t, db, `INSERT INTO clips(data,filename,content_type) VALUES('second','second.txt','text/plain')`)
	waitEvent(t, w.Events, "create", isFile(fsnotify.Create, "second"))
	eventually(t, "Active mtime advances", func() error {
		after, err := os.Stat(active)
		if err != nil {
			return err
		}
		if !after.ModTime().After(before.ModTime()) {
			return errors.New("mtime unchanged")
		}
		return nil
	})
	second := entryWithPrefix(t, active, "second [")
	if b, err := os.ReadFile(second); err != nil || string(b) != "second" {
		t.Fatalf("created entry reads %q, %v", b, err)
	}

	mustExec(t, db, `UPDATE clips SET data='first, edited' WHERE id=1`)
	waitEvent(t, w.Events, "modify", isFile(fsnotify.Write, "first"))
	first := entryWithPrefix(t, active, "first [")
	if b, err := os.ReadFile(first); err != nil || string(b) != "first, edited" {
		t.Fatalf("edited entry reads %q, %v", b, err)
	}

	mustExec(t, db, `DELETE FROM clips WHERE id=2`)
	waitEvent(t, w.Events, "remove", isFile(fsnotify.Remove, "second"))
	if _, err := os.Stat(second); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed entry stat = %v", err)
	}

	mustExec(t, db, `INSERT INTO tags(id,name) VALUES(1,'projects')`)
	mustExec(t, db, `INSERT INTO clip_tags VALUES(1,1)`)
	waitEvent(t, w.Events, "tag folder create", func(e fsnotify.Event) bool {
		return e.Has(fsnotify.Create) && e.Name == filepath.Join(tags, "projects")
	})
	if _, err := onlyFile(filepath.Join(tags, "projects")); err != nil {
		t.Fatal(err)
	}
}

// Changes from other processes reach the handlers now that the kernel mount
// is not flagged read-only; every one must still fail.
func TestOtherProcessesCannotChangeTheMount(t *testing.T) {
	db, dir := testMount(t)
	mustExec(t, db, `INSERT INTO clips(data,filename,content_type) VALUES('keep','keep.txt','text/plain')`)
	var path string
	eventually(t, "clip listed", func() (err error) {
		path, err = onlyFile(filepath.Join(dir, "Active"))
		return err
	})
	active := filepath.Join(dir, "Active")
	for _, script := range []string{
		`rm -f "$1"`,
		`touch "$1"`,
		`touch "$2/new.txt"`,
		`mkdir "$2/folder"`,
		`mv "$1" "$2/renamed.txt"`,
		`ln "$1" "$2/hardlink"`,
		`ln -s "$1" "$2/symlink"`,
		`mknod "$2/node" p`,
		`printf x >> "$1"`,
		`chmod 666 "$1"`,
		`test -w "$1"`,
	} {
		out, err := exec.Command("sh", "-c", script, "sh", path, active).CombinedOutput()
		if err == nil {
			t.Errorf("%s succeeded", script)
		} else if script != `test -w "$1"` && !strings.Contains(string(out), "Read-only file system") {
			t.Errorf("%s: %s", script, out)
		}
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "keep" {
		t.Fatalf("file changed: %q, %v", b, err)
	}
	entries, err := os.ReadDir(active)
	if err != nil || len(entries) != 1 {
		t.Fatalf("Active now has %d entries (%v)", len(entries), err)
	}
}
