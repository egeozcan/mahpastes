package app

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

// clipsColumnsReadPastBlob runs EXPLAIN on query and returns the clips
// columns any cursor on the clips *table* reads that are stored after the
// data blob. Reading one of those walks the row's whole overflow chain, so a
// listing query must get them from a covering index instead. Columns read
// through an index cursor do not count; LENGTH(data) and content_type do not
// either (record header and pre-blob column).
func clipsColumnsReadPastBlob(t *testing.T, db *sql.DB, query string, args ...interface{}) []string {
	t.Helper()
	var root int64
	if err := db.QueryRow(`SELECT rootpage FROM sqlite_master WHERE type = 'table' AND name = 'clips'`).Scan(&root); err != nil {
		t.Fatal(err)
	}
	colRows, err := db.Query(`SELECT cid, name FROM pragma_table_info('clips')`)
	if err != nil {
		t.Fatal(err)
	}
	names := map[int64]string{}
	dataCol := int64(-1)
	for colRows.Next() {
		var cid int64
		var name string
		if err := colRows.Scan(&cid, &name); err != nil {
			t.Fatal(err)
		}
		names[cid] = name
		if name == "data" {
			dataCol = cid
		}
	}
	colRows.Close()

	rows, err := db.Query("EXPLAIN "+query, args...)
	if err != nil {
		t.Fatalf("explain: %v\n%s", err, query)
	}
	defer rows.Close()
	tableCursors := map[int64]bool{}
	var past []string
	for rows.Next() {
		var addr, p1, p2, p3, p5 sql.NullInt64
		var op string
		var p4, comment sql.NullString
		if err := rows.Scan(&addr, &op, &p1, &p2, &p3, &p4, &p5, &comment); err != nil {
			t.Fatal(err)
		}
		switch op {
		case "OpenRead", "ReopenIdx":
			// p4 is the column count for a table cursor, a key description
			// for an index cursor.
			if p2.Int64 == root {
				tableCursors[p1.Int64] = true
			}
		case "Column":
			if tableCursors[p1.Int64] && p2.Int64 > dataCol {
				past = append(past, names[p2.Int64])
			}
		}
	}
	return past
}

// Every clips column but id and content_type sits after the data blob, so a
// listing that sorts or filters on them through the table reads every clip's
// bytes. The page query's inner, filtering pass must run off a covering index
// for every sort order.
func TestClipListingUsesCoveringIndex(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()

	for _, sortField := range []string{"created", "name", "type", "size"} {
		for _, dir := range []string{"desc", "asc"} {
			q := a.buildClipListQuery(false, nil, nil, sortField, dir, true, nil, false)
			rows, err := a.db.Query("EXPLAIN QUERY PLAN "+clipPageSQL(q, 0, 50), q.args...)
			if err != nil {
				t.Fatal(err)
			}
			var plan []string
			for rows.Next() {
				var id, parent, notused int
				var detail string
				if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
					t.Fatal(err)
				}
				plan = append(plan, detail)
			}
			rows.Close()
			joined := strings.Join(plan, "\n")
			if !strings.Contains(joined, "COVERING INDEX idx_clips_page_") {
				t.Errorf("sort %s %s: listing does not use a covering index:\n%s", sortField, dir, joined)
			}
		}
	}
}

// The preview is only kept for text-like clips; the query must not hand back
// (and so must not read) the bytes of anything else.
func TestClipPreviewOnlyForTextLikeClips(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()

	insertTestClip(t, a, "note.txt", "text/plain", []byte("hello text"))
	insertTestClip(t, a, "doc.json", "application/json", []byte(`{"a":1}`))
	insertTestClip(t, a, "pic.png", "image/png", []byte("\x89PNG not really"))

	page, err := a.ListClipsPage(ClipListRequest{Limit: defaultClipLimit})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, c := range page.Clips {
		got[c.Filename] = c.Preview
	}
	want := map[string]string{"note.txt": "hello text", "doc.json": `{"a":1}`, "pic.png": ""}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s preview = %q, want %q", name, got[name], w)
		}
	}

	var raw []byte
	if err := a.db.QueryRow("SELECT " + clipPreviewExpr("") + " FROM clips WHERE filename = 'pic.png'").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw != nil {
		t.Errorf("image preview expression returned %q, want NULL", raw)
	}
}

// No listing query may read a column stored after the blob through the clips
// table: not the page query (inner filter or outer join, any sort, any mode),
// not the counts that sit beside it.
func TestClipListingQueriesNeverReadPastBlob(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()

	parent, err := a.CreateTag("work")
	if err != nil {
		t.Fatal(err)
	}
	child, err := a.CreateTag("work/sub")
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := a.CreateTag("secret")
	if err != nil {
		t.Fatal(err)
	}

	type listing struct {
		name string
		q    clipListQuery
	}
	var listings []listing
	for _, sortField := range []string{"created", "name", "type", "size"} {
		for _, dir := range []string{"desc", "asc"} {
			tag := fmt.Sprintf("%s/%s", sortField, dir)
			listings = append(listings,
				listing{"all " + tag, a.buildClipListQuery(false, nil, nil, sortField, dir, true, nil, false)},
				listing{"filtered " + tag, a.buildClipListQuery(false, []int64{parent.ID}, []int64{hidden.ID}, sortField, dir, true, nil, false)},
				listing{"folder " + tag, a.buildClipListQuery(true, []int64{parent.ID}, []int64{child.ID}, sortField, dir, false, nil, false)},
				listing{"untagged " + tag, a.buildClipListQuery(false, nil, nil, sortField, dir, false, nil, true)},
				listing{"search " + tag, a.buildClipListQuery(false, nil, nil, sortField, dir, true, &clipSearchSpec{Query: "rep"}, false)},
			)
		}
	}
	for _, l := range listings {
		if past := clipsColumnsReadPastBlob(t, a.db, clipPageSQL(l.q, 0, 50), l.q.args...); len(past) > 0 {
			t.Errorf("%s: page query reads %v through the clips table", l.name, past)
		}
		if past := clipsColumnsReadPastBlob(t, a.db, "SELECT COUNT(*) FROM clips c WHERE "+l.q.where, l.q.args...); len(past) > 0 {
			t.Errorf("%s: count reads %v through the clips table", l.name, past)
		}
	}
}

// The counts and lookups beside the listing obey the same rule: the folder
// card counts, the hidden-clip note and Markdown reference resolution.
func TestClipSideQueriesNeverReadPastBlob(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()

	where := "c.is_archived = ? AND (c.expires_at IS NULL OR c.expires_at > CURRENT_TIMESTAMP)" +
		" AND EXISTS (SELECT 1 FROM clip_tags ct WHERE ct.clip_id = c.id AND ct.tag_id IN (?))"
	queries := []struct {
		name string
		sql  string
		args []interface{}
	}{
		{"descendant counts", descendantClipCountsSQL(2), []interface{}{1, 2, 0}},
		{"hidden count", hiddenClipCountSQL(where), []interface{}{0, 3}},
		{"hidden tag names", hiddenClipTagsSQL(where, "?"), []interface{}{0, 3, 3}},
		{"markdown untagged", markdownUntaggedCandidatesSQL, []interface{}{"a.png", 9}},
		{"markdown tagged", markdownTaggedCandidatesSQL, []interface{}{"docs", "a.png", 9}},
	}
	for _, q := range queries {
		if past := clipsColumnsReadPastBlob(t, a.db, q.sql, q.args...); len(past) > 0 {
			t.Errorf("%s reads %v through the clips table", q.name, past)
		}
	}
}
