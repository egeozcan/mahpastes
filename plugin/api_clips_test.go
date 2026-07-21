package plugin

import (
	"database/sql"
	"testing"

	lua "github.com/yuin/gopher-lua"
	_ "modernc.org/sqlite"
)

func TestClipsCreatePromotesMarkdownFilename(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open DB: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE clips (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		content_type TEXT NOT NULL,
		data BLOB NOT NULL,
		filename TEXT,
		content_hash TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		is_archived INTEGER DEFAULT 0
	)`); err != nil {
		t.Fatalf("create clips table: %v", err)
	}

	L := lua.NewState()
	defer L.Close()
	NewClipsAPI(db, nil).Register(L)
	if err := L.DoString(`
		local clip, err = clips.create({
			filename = "plugin.MARKDOWN",
			content_type = "text/plain",
			data = "# Plugin"
		})
		assert(clip, err)
	`); err != nil {
		t.Fatalf("clips.create: %v", err)
	}

	var contentType string
	if err := db.QueryRow(`SELECT content_type FROM clips WHERE filename = 'plugin.MARKDOWN'`).Scan(&contentType); err != nil {
		t.Fatalf("query clip: %v", err)
	}
	if contentType != "text/markdown" {
		t.Fatalf("content type = %q, want text/markdown", contentType)
	}
}
