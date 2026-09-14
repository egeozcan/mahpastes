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

func itemNamed(items []Item, name string) (Item, bool) {
	for _, item := range items {
		if item.Name == name {
			return item, true
		}
	}
	return Item{}, false
}

func fileIn(items []Item) (Item, bool) {
	for _, item := range items {
		if !item.Folder {
			return item, true
		}
	}
	return Item{}, false
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

func TestTagFoldersExposeHierarchyAliasesAndHiddenState(t *testing.T) {
	s, db := testStore(t)
	ctx := context.Background()
	insertClip(t, db, "brief.txt", []byte("brief"))
	execSQL(t, db, `INSERT INTO tags(id,name) VALUES(10,'projects'),(11,'projects/acme'),(12,'reference')`)
	// Multiple root trees are legal. The aliases must have distinct Finder IDs
	// even though they stream the same clip data.
	execSQL(t, db, `INSERT INTO clip_tags(clip_id,tag_id) VALUES(1,11),(1,12)`)

	root := enumerate(t, s, "root")
	if _, found := itemNamed(root.Items, "Tags"); !found {
		t.Fatalf("Tags root missing: %+v", root.Items)
	}
	tags := enumerate(t, s, "tags")
	projects, found := itemNamed(tags.Items, "projects")
	if !found || projects.Parent != "tags" || !projects.Folder {
		t.Fatalf("projects tag root: %+v", tags.Items)
	}
	reference, found := itemNamed(tags.Items, "reference")
	if !found {
		t.Fatalf("reference tag root: %+v", tags.Items)
	}
	projectAnchor := projects.ID
	children := enumerate(t, s, projectAnchor)
	acme, found := itemNamed(children.Items, "acme")
	if !found || acme.Parent != projectAnchor {
		t.Fatalf("projects children: %+v", children.Items)
	}
	acmeItems := enumerate(t, s, acme.ID)
	acmeFile, found := fileIn(acmeItems.Items)
	if !found || acmeFile.Parent != acme.ID {
		t.Fatalf("acme contents: %+v", acmeItems.Items)
	}
	referenceItems := enumerate(t, s, reference.ID)
	referenceFile, found := fileIn(referenceItems.Items)
	if !found || referenceFile.ID == acmeFile.ID {
		t.Fatalf("tag aliases were not independent: acme=%+v reference=%+v", acmeFile, referenceFile)
	}
	scopes, err := s.Scopes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"tags", projects.ID, acme.ID, reference.ID} {
		if !contains(scopes, expected) {
			t.Fatalf("signal scope %q missing from %v", expected, scopes)
		}
	}
	var contents bytes.Buffer
	if err := s.Content(ctx, acmeFile.ID, acmeFile.ContentVersion, func(Item) (io.Writer, error) { return &contents, nil }); err != nil {
		t.Fatal(err)
	}
	if contents.String() != "brief" {
		t.Fatalf("tag alias content = %q", contents.String())
	}
	active := enumerate(t, s, "active")
	canonical, found := fileIn(active.Items)
	if !found {
		t.Fatalf("canonical Active item missing: %+v", active.Items)
	}

	// Hiding a parent keeps its entire folder branch addressable but marks it
	// hidden for Finder. Its aliases remain available in that hidden branch;
	// the canonical Active/Archive projection retains the existing hidden-clip
	// behavior.
	execSQL(t, db, `INSERT INTO settings(key,value) VALUES('hidden_tags','[10]')`)
	if _, err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	tagChanges, err := s.Changes(ctx, "tags", tags.Anchor)
	if err != nil {
		t.Fatal(err)
	}
	changedProjects, found := itemNamed(tagChanges.Items, "projects")
	if !found || !changedProjects.Hidden {
		t.Fatalf("hidden directory update missing: %+v", tagChanges)
	}
	projects, err = s.Item(ctx, projects.ID)
	if err != nil || !projects.Hidden {
		t.Fatalf("hidden projects folder = %+v, %v", projects, err)
	}
	acme, err = s.Item(ctx, acme.ID)
	if err != nil || !acme.Hidden {
		t.Fatalf("hidden descendant folder = %+v, %v", acme, err)
	}
	if _, err = s.Item(ctx, acmeFile.ID); err != nil {
		t.Fatalf("hidden-tag alias no longer readable: %v", err)
	}
	if _, err = s.Item(ctx, referenceFile.ID); err != nil {
		t.Fatalf("visible-tag alias no longer readable: %v", err)
	}
	if _, err = s.Item(ctx, canonical.ID); !errors.Is(err, ErrNoSuchItem) {
		t.Fatalf("clip carrying a hidden tag remained in Active: %v", err)
	}
}

func TestV1ProjectionMigratesToTagFolders(t *testing.T) {
	_, db := testStore(t)
	insertClip(t, db, "legacy.txt", []byte("legacy"))
	execSQL(t, db, `INSERT INTO tags(id,name) VALUES(5,'legacy'); INSERT INTO clip_tags(clip_id,tag_id) VALUES(1,5)`)
	// Recreate the v1 shape: one unique fp_items row per clip. Open must
	// rebuild it rather than retaining that constraint and dropping aliases.
	execSQL(t, db, `DROP TABLE fp_items;
CREATE TABLE fp_items (id TEXT PRIMARY KEY, clip_id INTEGER UNIQUE, parent TEXT NOT NULL, item TEXT NOT NULL);
UPDATE fp_state SET version=1`)
	s, err := Open(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	var version int
	if err = db.QueryRow(`SELECT version FROM fp_state`).Scan(&version); err != nil || version != 2 {
		t.Fatalf("projection migration version = %d, %v", version, err)
	}
	tags := enumerate(t, s, "tags")
	legacy, found := itemNamed(tags.Items, "legacy")
	if !found {
		t.Fatalf("migrated Tags root: %+v", tags.Items)
	}
	if _, found = fileIn(enumerate(t, s, legacy.ID).Items); !found {
		t.Fatal("migrated tag alias missing")
	}
}

func TestTagFoldersSynthesizeAncestorsAndUseSafeSiblingNames(t *testing.T) {
	s, db := testStore(t)
	// UpdateTag and legacy/plugin SQL can leave a hierarchical row without a
	// parent. Finder must still show its full path rather than flattening it.
	execSQL(t, db, `INSERT INTO tags(id,name) VALUES(20,'projects/acme'),(21,'Work'),(22,'work'),(23,'.')`)
	tags := enumerate(t, s, "tags")
	projects, found := itemNamed(tags.Items, "projects")
	if !found {
		t.Fatalf("synthetic projects folder missing: %+v", tags.Items)
	}
	children := enumerate(t, s, projects.ID)
	if _, found = itemNamed(children.Items, "acme"); !found {
		t.Fatalf("orphan tag was flattened instead of nested: %+v", children.Items)
	}

	seen := map[string]bool{}
	for _, item := range tags.Items {
		if item.Name == "." || item.Name == ".." || strings.ContainsAny(item.Name, "/\\:\x00") || seen[strings.ToLower(item.Name)] {
			t.Fatalf("unsafe or colliding Finder name: %+v", tags.Items)
		}
		seen[strings.ToLower(item.Name)] = true
	}
}

func TestTagFolderCollisionRenamesUpdateMetadataVersion(t *testing.T) {
	s, db := testStore(t)
	ctx := context.Background()
	execSQL(t, db, `INSERT INTO tags(id,name) VALUES(30,'Work')`)
	before, found := itemNamed(enumerate(t, s, "tags").Items, "Work")
	if !found {
		t.Fatal("initial Work folder missing")
	}

	execSQL(t, db, `INSERT INTO tags(id,name) VALUES(31,'work')`)
	if _, err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	during, err := s.Item(ctx, before.ID)
	if err != nil || during.Name == "Work" || during.MetadataVersion == before.MetadataVersion {
		t.Fatalf("collision rename did not update metadata: before=%+v during=%+v err=%v", before, during, err)
	}

	execSQL(t, db, `DELETE FROM tags WHERE id=31`)
	if _, err = s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := s.Item(ctx, before.ID)
	if err != nil || after.Name != "Work" || after.MetadataVersion == during.MetadataVersion {
		t.Fatalf("collision removal did not update metadata: during=%+v after=%+v err=%v", during, after, err)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
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

func TestDisabledV1RestoreCreatesTagInvalidationTable(t *testing.T) {
	_, db := testStore(t)
	// Simulate an old enrolled projection that is disabled before the app
	// starts again: restore calls ResetAfterRestore before Open can migrate it.
	execSQL(t, db, `DROP TABLE fp_tags_dirty; UPDATE fp_state SET version=1`)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = ResetAfterRestore(tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM fp_tags_dirty`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("tag invalidation table after disabled v1 restore = %d, %v", count, err)
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

func TestEmptyRestoreSignalsAndInvalidatesOldState(t *testing.T) {
	s, db := testStore(t)
	ctx := context.Background()
	insertClip(t, db, "one.txt", []byte("one"))
	p := enumerate(t, s, "active")
	before, err := s.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`DELETE FROM clips`); err != nil {
		t.Fatal(err)
	}
	if err = ResetAfterRestore(tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := s.Head(ctx)
	if err != nil || after == before {
		t.Fatal("empty restore did not change notification head", err)
	}
	if _, err = s.Changes(ctx, "active", p.Anchor); !errors.Is(err, ErrAnchor) {
		t.Fatal(err)
	}
	if len(enumerate(t, s, "active").Items) != 0 {
		t.Fatal("restored empty database enumerates old clips")
	}
}
