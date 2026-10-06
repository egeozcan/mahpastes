package app

import (
	"fmt"
	"strings"
	"testing"
)

// Every clips column but id and content_type sits after the data blob, so a
// listing that sorts or filters on them through the table reads every clip's
// bytes. The page query's inner, filtering pass must run off a covering index
// for the default and name sorts.
func TestClipListingUsesCoveringIndex(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()

	for _, sortField := range []string{"created", "name"} {
		for _, dir := range []string{"desc", "asc"} {
			q := a.buildClipListQuery(false, nil, nil, sortField, dir, true, nil, false)
			inner := fmt.Sprintf("SELECT c.id AS id, COUNT(*) OVER () AS total FROM clips c WHERE %s %s LIMIT 50", q.where, q.order)
			rows, err := a.db.Query("EXPLAIN QUERY PLAN "+inner, q.args...)
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
			if !strings.Contains(joined, "COVERING INDEX idx_clips_list_") {
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
	if err := a.db.QueryRow("SELECT "+clipPreviewExpr("")+" FROM clips WHERE filename = 'pic.png'").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw != nil {
		t.Errorf("image preview expression returned %q, want NULL", raw)
	}
}
