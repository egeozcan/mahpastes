package app

import (
	"strings"
	"testing"
)

// A library dominated by one repeated filename (every capture called
// screenshot.png), all of it tagged, makes idx_clips_by_filename look
// unselective after ANALYZE. Left to itself the planner then resolves a
// tagged Markdown reference by walking the tag's clip_tags rows and reading
// each clip by rowid — filename, expires_at and is_archived come off the
// table, past every blob. Both candidate queries must stay on the covering
// filename index whatever the statistics say.
func TestMarkdownReferenceQueriesKeepFilenameIndexOnRepeatedNames(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()

	shots, err := a.CreateTag("shots")
	if err != nil {
		t.Fatal(err)
	}
	other, err := a.CreateTag("other")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := a.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	blob := []byte(strings.Repeat("x", 20_000))
	for i := 0; i < 400; i++ {
		res, err := tx.Exec(`INSERT INTO clips (filename, content_type, data, content_hash)
			VALUES ('screenshot.png', 'image/png', ?, 'h')`, blob)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		tagID := other.ID
		if i%20 == 0 {
			tagID = shots.ID
		}
		if _, err := tx.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, ?)`, id, tagID); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec("ANALYZE"); err != nil {
		t.Fatal(err)
	}

	for _, q := range []struct {
		name string
		sql  string
		args []interface{}
	}{
		{"untagged", markdownUntaggedCandidatesSQL, []interface{}{"screenshot.png", 9}},
		{"tagged", markdownTaggedCandidatesSQL, []interface{}{"shots", "screenshot.png", 9}},
	} {
		if past := clipsColumnsReadPastBlob(t, a.db, q.sql, q.args...); len(past) > 0 {
			t.Errorf("markdown %s candidates read %v through the clips table", q.name, past)
		}
	}

	// The forced index must not change the answer.
	got, err := a.findMarkdownReferenceCandidates("", "screenshot.png")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("untagged lookup found %d tagged clips", len(got))
	}
	got, err = a.findMarkdownReferenceCandidates("shots", "screenshot.png")
	if err != nil {
		t.Fatal(err)
	}
	want := 400 / 20
	if want > maxMarkdownReferenceCandidates+1 {
		want = maxMarkdownReferenceCandidates + 1
	}
	if len(got) != want {
		t.Errorf("tagged lookup found %d, want %d", len(got), want)
	}
}
