package fileprovider

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func testStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "clips.db")+"?_pragma=journal_mode%3Dwal&_pragma=foreign_keys%3Don&_pragma=busy_timeout%3D5000")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`CREATE TABLE clips(id INTEGER PRIMARY KEY AUTOINCREMENT,data BLOB NOT NULL,filename TEXT,content_type TEXT NOT NULL,created_at TEXT DEFAULT CURRENT_TIMESTAMP,is_archived INTEGER DEFAULT 0,expires_at TEXT,content_hash TEXT DEFAULT '',metadata TEXT DEFAULT '{}'); CREATE TABLE settings(key TEXT PRIMARY KEY,value TEXT); CREATE TABLE tags(id INTEGER PRIMARY KEY,name TEXT); CREATE TABLE clip_tags(clip_id INTEGER REFERENCES clips(id) ON DELETE CASCADE,tag_id INTEGER REFERENCES tags(id) ON DELETE CASCADE,PRIMARY KEY(clip_id,tag_id));`)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	return s, db
}
func execSQL(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}
func insertClip(t *testing.T, db *sql.DB, name string, data []byte) {
	t.Helper()
	execSQL(t, db, `INSERT INTO clips(data,filename,content_type) VALUES(?,?,'text/plain')`, data, name)
}
func enumerate(t *testing.T, s *Store, scope string) Page {
	t.Helper()
	p, err := s.Enumerate(context.Background(), scope, "")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDirectSQLVersionsVisibilityAndMoves(t *testing.T) {
	s, db := testStore(t)
	ctx := context.Background()
	insertClip(t, db, "hello.txt", []byte("one"))
	p := enumerate(t, s, "active")
	if len(p.Items) != 1 {
		t.Fatal(p)
	}
	original := p.Items[0]
	// The JSON serving path updates data without updating content_hash.
	execSQL(t, db, `UPDATE clips SET data=? WHERE id=1`, []byte("two"))
	i, err := s.Item(ctx, original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if i.ID != original.ID || i.ContentVersion == original.ContentVersion {
		t.Fatal("content identity/version not preserved")
	}
	if err = s.Content(ctx, i.ID, original.ContentVersion, func(Item) (io.Writer, error) { t.Fatal("stale version streamed"); return nil, nil }); !errors.Is(err, ErrVersion) {
		t.Fatal(err)
	}
	execSQL(t, db, `UPDATE clips SET filename='renamed.txt',is_archived=1 WHERE id=1`)
	changes, err := s.Changes(ctx, "active", p.Anchor)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes.Deleted) != 1 || changes.Deleted[0] != i.ID {
		t.Fatal(changes)
	}
	archived := enumerate(t, s, "archive").Items[0]
	if archived.ID != i.ID || archived.ContentVersion != i.ContentVersion || archived.MetadataVersion == i.MetadataVersion {
		t.Fatal(archived)
	}
	execSQL(t, db, `INSERT INTO tags VALUES(1,'secret'),(2,'secret/child'); INSERT INTO clip_tags VALUES(1,2); INSERT INTO settings VALUES('hidden_tags','[1]')`)
	if _, err = s.Item(ctx, i.ID); !errors.Is(err, ErrNoSuchItem) {
		t.Fatal("hidden item remains readable", err)
	}
	if len(enumerate(t, s, "archive").Items) != 0 {
		t.Fatal("hidden descendant remains enumerated")
	}
	execSQL(t, db, `UPDATE settings SET value='[]' WHERE key='hidden_tags'`)
	if len(enumerate(t, s, "archive").Items) != 1 {
		t.Fatal("unhide lost item")
	}
	execSQL(t, db, `UPDATE clips SET expires_at=datetime('now','-1 second') WHERE id=1`)
	if _, err = s.Item(ctx, i.ID); !errors.Is(err, ErrNoSuchItem) {
		t.Fatal("expired item readable", err)
	}
	if len(enumerate(t, s, "archive").Items) != 0 {
		t.Fatal("expired item enumerated")
	}
}

func TestMaterializationUsesOneSnapshot(t *testing.T) {
	s, db := testStore(t)
	before := bytes.Repeat([]byte("a"), 3<<20)
	after := bytes.Repeat([]byte("b"), 3<<20)
	insertClip(t, db, "large.txt", before)
	i := enumerate(t, s, "active").Items[0]
	var got bytes.Buffer
	err := s.Content(context.Background(), i.ID, i.ContentVersion, func(metadata Item) (io.Writer, error) {
		if metadata.Size != int64(len(before)) {
			t.Fatal(metadata)
		}
		execSQL(t, db, `UPDATE clips SET data=? WHERE id=1`, after)
		return &got, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), before) {
		t.Fatal("mixed content revisions")
	}
	err = s.Content(context.Background(), i.ID, i.ContentVersion, func(Item) (io.Writer, error) { return &got, nil })
	if !errors.Is(err, ErrVersion) {
		t.Fatal(err)
	}
}

func TestEnumerationAndChangesHaveFixedHighWaterMarks(t *testing.T) {
	s, db := testStore(t)
	ctx := context.Background()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 450; n++ {
		if _, err = tx.Exec(`INSERT INTO clips(data,filename,content_type) VALUES(?,?,'text/plain')`, []byte("old"), fmt.Sprintf("clip-%d.txt", n)); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	p := enumerate(t, s, "active")
	if len(p.Items) != PageSize || p.Next == "" {
		t.Fatal(p)
	}
	execSQL(t, db, `UPDATE clips SET filename='renamed.txt'`)
	all := append([]Item{}, p.Items...)
	next := p.Next
	for next != "" {
		page, e := s.Enumerate(ctx, "active", next)
		if e != nil {
			t.Fatal(e)
		}
		all = append(all, page.Items...)
		next = page.Next
	}
	if len(all) != 450 {
		t.Fatal(len(all))
	}
	seen := map[string]bool{}
	for _, i := range all {
		if seen[i.ID] || strings.HasPrefix(i.Name, "renamed") {
			t.Fatal("snapshot changed between pages", i)
		}
		seen[i.ID] = true
	}
	page, err := s.Changes(ctx, "active", p.Anchor)
	if err != nil {
		t.Fatal(err)
	}
	if !page.More {
		t.Fatal("changes did not paginate")
	}
	execSQL(t, db, `UPDATE clips SET filename='later.txt'`)
	count := len(page.Items)
	for page.More {
		page, err = s.Changes(ctx, "active", page.Anchor)
		if err != nil {
			t.Fatal(err)
		}
		count += len(page.Items)
		for _, i := range page.Items {
			if !strings.HasPrefix(i.Name, "renamed") {
				t.Fatal("journal joined newer live metadata", i)
			}
		}
	}
	if count != 450 {
		t.Fatal(count)
	}
	later, err := s.Changes(ctx, "active", page.Anchor)
	if err != nil || len(later.Items) == 0 || !strings.HasPrefix(later.Items[0].Name, "later") {
		t.Fatal(later, err)
	}
	if _, err = s.Enumerate(ctx, "archive", p.Next); !errors.Is(err, ErrPage) {
		t.Fatal("cross-scope page accepted", err)
	}
	execSQL(t, db, `UPDATE fp_snapshots SET expires=0`)
	if _, err = s.Enumerate(ctx, "active", p.Next); !errors.Is(err, ErrPage) {
		t.Fatal(err)
	}
}

func TestRestoreAndNumericIDReuseCannotAlias(t *testing.T) {
	s, db := testStore(t)
	ctx := context.Background()
	insertClip(t, db, "before.txt", []byte("before"))
	p := enumerate(t, s, "active")
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`DELETE FROM clips;INSERT INTO clips(id,data,filename,content_type) VALUES(1,x'61','after.txt','text/plain')`); err != nil {
		t.Fatal(err)
	}
	if err = ResetAfterRestore(tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Item(ctx, p.Items[0].ID); !errors.Is(err, ErrNoSuchItem) {
		t.Fatal("old ID resolved after restore", err)
	}
	if _, err = s.Changes(ctx, "active", p.Anchor); !errors.Is(err, ErrAnchor) {
		t.Fatal("old anchor survived restore", err)
	}
	after := enumerate(t, s, "active").Items[0]
	execSQL(t, db, `DELETE FROM clips;INSERT INTO clips(id,data,filename,content_type) VALUES(1,x'62','reuse.txt','text/plain')`)
	changes, err := s.Changes(ctx, "active", enumerate(t, s, "root").Anchor)
	if !errors.Is(err, ErrAnchor) {
		t.Fatal("scope mismatch", changes, err)
	}
	if _, err = s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Item(ctx, after.ID); !errors.Is(err, ErrNoSuchItem) {
		t.Fatal("numeric reuse aliased UUID", err)
	}
	if got := enumerate(t, s, "active"); len(got.Items) != 1 || got.Items[0].ID == after.ID {
		t.Fatal(got)
	}
}

func TestRollbackAndRetainedTracking(t *testing.T) {
	s, db := testStore(t)
	insertClip(t, db, "one.txt", []byte("one"))
	p := enumerate(t, s, "active")
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`UPDATE clips SET data=x'62'`); err != nil {
		t.Fatal(err)
	}
	tx.Rollback()
	i, err := s.Item(context.Background(), p.Items[0].ID)
	if err != nil || i.ContentVersion != p.Items[0].ContentVersion {
		t.Fatal(i, err)
	}
	// A disabled/non-native build still has triggers; reopening catches up.
	execSQL(t, db, `UPDATE clips SET data=x'63'`)
	s, err = Open(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	p2 := enumerate(t, s, "active")
	if p2.Items[0].ID != i.ID || p2.Items[0].ContentVersion == i.ContentVersion {
		t.Fatal(p2)
	}
	execSQL(t, db, `DELETE FROM clips`)
	change, err := s.Changes(context.Background(), "active", p.Anchor)
	if err != nil || len(change.Deleted) != 1 || len(change.Items) != 0 {
		t.Fatal(change, err)
	}
}

func TestProtocolAuthAndBinaryContent(t *testing.T) {
	s, db := testStore(t)
	insertClip(t, db, "one.txt", []byte{0, 1, 2, 255})
	item := enumerate(t, s, "active").Items[0]
	h := handler(s, "domain", "secret")
	for _, tc := range []struct {
		method, path, auth, origin string
		status                     int
	}{
		{"GET", "/v1/item?domain=domain&id=" + url.QueryEscape(item.ID), "", "", 401},
		{"GET", "/v1/item?domain=domain&id=" + url.QueryEscape(item.ID), "Bearer secret", "https://example.com", 401},
		{"GET", "/v1/item?domain=other", "Bearer secret", "", 403},
		{"PUT", "/v1/content?domain=domain", "Bearer secret", "", 405},
		{"GET", "/v1/content?domain=domain&id=" + url.QueryEscape(item.ID) + "&version=0", "Bearer secret", "", 409},
		{"GET", "/v1/content?domain=domain&id=" + url.QueryEscape(item.ID), "Bearer secret", "", 200},
	} {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		r.Header.Set("Authorization", tc.auth)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatal(tc, w.Code, w.Body.String())
		}
		if w.Code == 200 && (!bytes.Equal(w.Body.Bytes(), []byte{0, 1, 2, 255}) || w.Header().Get("X-Mahpastes-Item") == "") {
			t.Fatal(w)
		}
	}
	server, err := Start(context.Background(), s, "domain", strings.Repeat("s", 32), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if !strings.HasPrefix(server.Discovery.Endpoint, "https://127.0.0.1:") || len(server.Discovery.CertificateSHA256) != 64 {
		t.Fatal(server.Discovery)
	}
	b, _ := json.Marshal(server.Discovery)
	if bytes.Contains(b, []byte(strings.Repeat("s", 32))) {
		t.Fatal("discovery contains secret")
	}
	// An ordinary trust store cannot authenticate the ephemeral certificate.
	if resp, err := http.Get(server.Discovery.Endpoint); err == nil {
		resp.Body.Close()
		t.Fatal("unpinned TLS accepted")
	}
}

func TestSafeNames(t *testing.T) {
	uuid := "1234567890abcdef1234567890abcdef"
	for _, name := range []string{"../a/b\\c:d\x00.txt", ".", "", strings.Repeat("é", 300) + ".txt"} {
		got := Filename(name, "text/plain", uuid)
		if strings.ContainsAny(got, "/\\:\x00") || strings.HasPrefix(got, ".") || len(got) > 255 || !strings.Contains(got, uuid) {
			t.Fatal(got)
		}
	}
	if Filename("é.txt", "text/plain", uuid) != Filename("e\u0301.txt", "text/plain", uuid) {
		t.Fatal("normalization differs")
	}
	if Filename("same.txt", "text/plain", uuid) == Filename("same.txt", "text/plain", strings.Repeat("a", 32)) {
		t.Fatal("name collision")
	}
}
