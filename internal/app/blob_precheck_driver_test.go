package app

import (
	"database/sql"
	"database/sql/driver"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The over-limit pre-checks in GetMarkdownImage and GetClipText exist so an
// oversize blob is never selected. Their replies alone cannot show that — a
// read-then-refuse returns the same error — so these tests run the App on a
// driver that records every statement it prepares and assert that no
// statement selecting `data` itself ran.

const recordingDriverName = "sqlite-statement-recorder"

var (
	recordedMu         sync.Mutex
	recordedStatements []string
)

var registerRecordingDriver = sync.OnceFunc(func() {
	base, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		panic("open base sqlite driver: " + err.Error())
	}
	d := base.Driver()
	_ = base.Close()
	sql.Register(recordingDriverName, recordingDriver{base: d})
})

type recordingDriver struct{ base driver.Driver }

func (d recordingDriver) Open(name string) (driver.Conn, error) {
	c, err := d.base.Open(name)
	if err != nil {
		return nil, err
	}
	return recordingConn{Conn: c}, nil
}

// Embeds the interface, not the concrete connection, so database/sql falls
// back to Prepare for every query and each one is recorded.
type recordingConn struct{ driver.Conn }

func (c recordingConn) Prepare(query string) (driver.Stmt, error) {
	recordedMu.Lock()
	recordedStatements = append(recordedStatements, query)
	recordedMu.Unlock()
	return c.Conn.Prepare(query)
}

func newRecordingApp(t *testing.T) *App {
	t.Helper()
	registerRecordingDriver()
	db, err := sql.Open(recordingDriverName, filepath.Join(t.TempDir(), "rec.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE clips (
		id INTEGER PRIMARY KEY, content_type TEXT, filename TEXT, data BLOB, expires_at DATETIME)`); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return &App{db: db}
}

func resetRecordedStatements() {
	recordedMu.Lock()
	recordedStatements = nil
	recordedMu.Unlock()
}

// selectsBlob reports whether any recorded statement selects the data column
// (as opposed to measuring it with octet_length/LENGTH).
func selectsBlob() (string, bool) {
	recordedMu.Lock()
	defer recordedMu.Unlock()
	for _, q := range recordedStatements {
		normalized := strings.Join(strings.Fields(q), " ")
		if strings.Contains(normalized, "SELECT data") || strings.Contains(normalized, ", data,") ||
			strings.Contains(normalized, ", data FROM") {
			return q, true
		}
	}
	return "", false
}

func TestGetMarkdownImageNeverSelectsOverLimitBlob(t *testing.T) {
	app := newRecordingApp(t)
	if _, err := app.db.Exec(`INSERT INTO clips (id, content_type, filename, data) VALUES (1, 'image/png', 'big.png', zeroblob(?))`,
		maxMarkdownImageBytes+1); err != nil {
		t.Fatal(err)
	}
	resetRecordedStatements()
	if _, err := app.GetMarkdownImage(1); err == nil || !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("err = %v, want byte-limit refusal", err)
	}
	if q, ok := selectsBlob(); ok {
		t.Fatalf("over-limit image selected its blob: %s", q)
	}

	// Control: an in-limit clip does select it, so the detector works.
	if _, err := app.db.Exec(`INSERT INTO clips (id, content_type, filename, data) VALUES (2, 'image/png', 'ok.png', ?)`,
		encodeTestPNG(t, 2, 2)); err != nil {
		t.Fatal(err)
	}
	resetRecordedStatements()
	if _, err := app.GetMarkdownImage(2); err != nil {
		t.Fatalf("in-limit image: %v", err)
	}
	if _, ok := selectsBlob(); !ok {
		t.Fatal("detector saw no blob select for an in-limit image")
	}
}

func TestGetClipTextNeverSelectsOverCapBlob(t *testing.T) {
	app := newRecordingApp(t)
	if _, err := app.db.Exec(`INSERT INTO clips (id, content_type, filename, data) VALUES (1, 'text/plain', 'huge.log', zeroblob(?))`,
		maxEditableTextBytes+1); err != nil {
		t.Fatal(err)
	}
	resetRecordedStatements()
	clip, err := app.GetClipText(1)
	if err != nil || !clip.TooLarge {
		t.Fatalf("GetClipText = %+v, %v; want too_large", clip, err)
	}
	if q, ok := selectsBlob(); ok {
		t.Fatalf("over-cap text selected its blob: %s", q)
	}
}

func TestGetClipTextBlobDetectorControl(t *testing.T) {
	app := newRecordingApp(t)
	if _, err := app.db.Exec(`INSERT INTO clips (id, content_type, filename, data) VALUES (1, 'text/plain', 'a.txt', CAST('hello' AS BLOB))`); err != nil {
		t.Fatal(err)
	}
	resetRecordedStatements()
	if _, err := app.GetClipText(1); err != nil {
		t.Fatalf("in-cap GetClipText: %v", err)
	}
	if _, ok := selectsBlob(); !ok {
		t.Fatal("detector saw no blob select for in-cap text")
	}
}
