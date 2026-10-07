package app

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The size-checked reads (GetClipText, handleGetClipText, GetMarkdownImage,
// ThumbnailCache.generate) measure a clip with octet_length(data) and then read
// it in a second statement. These tests grow the clip in the gap between the
// two — from inside the driver, as the read statement is prepared — and assert
// that no blob over the reader's limit ever came back from the database. A
// reply-level check alone cannot tell a guarded read from a read-then-refuse.

const growDriverName = "sqlite-grow-between-statements"

var (
	growMu       sync.Mutex
	growArmed    func()   // runs once, before the first blob-selecting statement
	growMaxBlob  int      // largest []byte value any row returned
	growBlobRead []string // statements that returned that blob
)

var registerGrowDriver = sync.OnceFunc(func() {
	base, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		panic("open base sqlite driver: " + err.Error())
	}
	d := base.Driver()
	_ = base.Close()
	sql.Register(growDriverName, growDriver{base: d})
})

type growDriver struct{ base driver.Driver }

func (d growDriver) Open(name string) (driver.Conn, error) {
	c, err := d.base.Open(name)
	if err != nil {
		return nil, err
	}
	return growConn{Conn: c}, nil
}

// Embeds the interfaces, not the concrete types, so database/sql falls back to
// Prepare + Stmt.Query and every statement and row passes through here.
type growConn struct{ driver.Conn }

func selectsBlobColumn(q string) bool {
	n := strings.Join(strings.Fields(q), " ")
	return strings.Contains(n, "SELECT data") || strings.Contains(n, ", data,") || strings.Contains(n, ", data FROM")
}

func (c growConn) Prepare(query string) (driver.Stmt, error) {
	if selectsBlobColumn(query) {
		growMu.Lock()
		fn := growArmed
		growArmed = nil
		growMu.Unlock()
		if fn != nil {
			fn() // on another pooled connection; this one holds no transaction yet
		}
	}
	s, err := c.Conn.Prepare(query)
	if err != nil {
		return nil, err
	}
	return growStmt{Stmt: s, query: query}, nil
}

type growStmt struct {
	driver.Stmt
	query string
}

//nolint:staticcheck // the deprecated non-context form is the fallback database/sql takes here
func (s growStmt) Query(args []driver.Value) (driver.Rows, error) {
	r, err := s.Stmt.Query(args) //nolint:staticcheck
	if err != nil {
		return nil, err
	}
	return growRows{Rows: r, query: s.query}, nil
}

type growRows struct {
	driver.Rows
	query string
}

func (r growRows) Next(dest []driver.Value) error {
	if err := r.Rows.Next(dest); err != nil {
		return err
	}
	for _, v := range dest {
		if b, ok := v.([]byte); ok {
			growMu.Lock()
			if len(b) > growMaxBlob {
				growMaxBlob = len(b)
				growBlobRead = append(growBlobRead, r.query)
			}
			growMu.Unlock()
		}
	}
	return nil
}

func newGrowDB(t *testing.T) *sql.DB {
	t.Helper()
	registerGrowDriver()
	db, err := sql.Open(growDriverName, filepath.Join(t.TempDir(), "grow.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE clips (
		id INTEGER PRIMARY KEY, content_type TEXT, filename TEXT, data BLOB,
		expires_at DATETIME, content_hash TEXT)`); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if _, err := db.Exec(`CREATE INDEX idx_clips_content_hash ON clips(content_hash)`); err != nil {
		t.Fatalf("index: %v", err)
	}
	t.Cleanup(func() {
		growMu.Lock()
		growArmed, growMaxBlob, growBlobRead = nil, 0, nil
		growMu.Unlock()
	})
	return db
}

// armGrowth makes the next blob-selecting statement first replace clip id's
// bytes with a zero blob of size bytes (and a fresh content hash).
func armGrowth(t *testing.T, db *sql.DB, id int64, size int) {
	t.Helper()
	growMu.Lock()
	growMaxBlob, growBlobRead = 0, nil
	growArmed = func() {
		if _, err := db.Exec(`UPDATE clips SET data = zeroblob(?), content_hash = ? WHERE id = ?`,
			size, strings.Repeat("b", 64), id); err != nil {
			t.Errorf("grow clip: %v", err)
		}
	}
	growMu.Unlock()
}

func assertNoBlobOver(t *testing.T, limit int) {
	t.Helper()
	growMu.Lock()
	defer growMu.Unlock()
	if growArmed != nil {
		t.Fatal("growth never fired: no blob-selecting statement ran")
	}
	if growMaxBlob > limit {
		t.Fatalf("read a %d-byte blob past the %d-byte limit via %q", growMaxBlob, limit, growBlobRead)
	}
}

func TestGetClipTextGuardsReadAgainstConcurrentGrowth(t *testing.T) {
	db := newGrowDB(t)
	app := &App{db: db}
	if _, err := db.Exec(`INSERT INTO clips (id, content_type, filename, data) VALUES (1, 'text/plain', 'a.txt', CAST('hello' AS BLOB))`); err != nil {
		t.Fatal(err)
	}
	armGrowth(t, db, 1, maxEditableTextBytes+1)

	clip, err := app.GetClipText(1)
	if err != nil {
		t.Fatalf("GetClipText: %v", err)
	}
	if !clip.TooLarge || clip.Data != "" || clip.Size != maxEditableTextBytes+1 {
		t.Fatalf("grown clip: too_large=%v size=%d data=%d bytes; want too_large with no data",
			clip.TooLarge, clip.Size, len(clip.Data))
	}
	assertNoBlobOver(t, maxEditableTextBytes)
}

func TestHandleGetClipTextGuardsReadAgainstConcurrentGrowth(t *testing.T) {
	db := newGrowDB(t)
	manager := NewAPIManager(&App{db: db})
	if _, err := db.Exec(`INSERT INTO clips (id, content_type, filename, data) VALUES (1, 'text/plain', 'a.txt', CAST('hello' AS BLOB))`); err != nil {
		t.Fatal(err)
	}
	armGrowth(t, db, 1, maxEditableTextBytes+1)

	req := httptest.NewRequest("GET", "/api/v1/clips/1/text", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	manager.handleGetClipText(rec, withKey(req, &apiKeyContext{KeyID: 1, Role: "viewer"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %.200s)", rec.Code, rec.Body.String())
	}
	var got apiClipTextResponse
	body, _ := io.ReadAll(rec.Body)
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.TooLarge || got.Data != "" {
		t.Fatalf("grown clip: too_large=%v data=%d bytes; want too_large with no data", got.TooLarge, len(got.Data))
	}
	assertNoBlobOver(t, maxEditableTextBytes)
}

func TestGetMarkdownImageGuardsReadAgainstConcurrentGrowth(t *testing.T) {
	db := newGrowDB(t)
	app := &App{db: db}
	if _, err := db.Exec(`INSERT INTO clips (id, content_type, filename, data) VALUES (1, 'image/png', 'a.png', ?)`,
		encodeTestPNG(t, 2, 2)); err != nil {
		t.Fatal(err)
	}
	armGrowth(t, db, 1, maxMarkdownImageBytes+1)

	if _, err := app.GetMarkdownImage(1); err == nil || !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("err = %v, want byte-limit refusal", err)
	}
	assertNoBlobOver(t, maxMarkdownImageBytes)
}

func TestThumbnailGenerateGuardsReadAgainstConcurrentGrowth(t *testing.T) {
	db := newGrowDB(t)
	cache, err := NewThumbnailCache(db, nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO clips (id, content_type, filename, data, content_hash) VALUES (1, 'image/png', 'a.png', ?, ?)`,
		encodeTestPNG(t, 2, 2), strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	armGrowth(t, db, 1, thumbMaxSourceBytes+1)

	res, err := cache.generate(t.Context(), 1)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !res.entry.original || res.hash != strings.Repeat("b", 64) {
		t.Fatalf("grown clip: entry=%+v hash=%s; want passthrough of the grown revision", res.entry, res.hash)
	}
	assertNoBlobOver(t, thumbMaxSourceBytes)
}
