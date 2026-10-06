package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetDescendantClipCounts(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()

	mk := func(name string) int64 {
		tag, err := a.CreateTag(name)
		if err != nil {
			t.Fatalf("CreateTag %s: %v", name, err)
		}
		return tag.ID
	}
	work := mk("work")
	sub := mk("work/sub")
	deep := mk("work/sub/deep")
	underscore := mk("my_tag")
	lookalike := mk("my-tag/x") // must not count under my_tag
	other := mk("other")

	add := func(name string, tags ...int64) int64 {
		id := insertTestClip(t, a, name, "text/plain", []byte(name))
		for _, tag := range tags {
			if _, err := a.db.Exec("INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, ?)", id, tag); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	add("w1", work)
	add("w2", sub)
	add("w3", deep, other) // counted once under work, and under other
	archived := add("w4", sub)
	expired := add("w5", deep)
	add("u1", underscore)
	add("l1", lookalike)
	if _, err := a.db.Exec("UPDATE clips SET is_archived = 1 WHERE id = ?", archived); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec("UPDATE clips SET expires_at = datetime('now', '-1 hour') WHERE id = ?", expired); err != nil {
		t.Fatal(err)
	}

	const missing = int64(99999)
	ids := []int64{work, sub, deep, underscore, lookalike, other, missing, work}
	for _, arch := range []bool{false, true} {
		counts, err := a.GetDescendantClipCounts(ids, arch)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range ids {
			if id == missing {
				if counts[id] != 0 {
					t.Errorf("missing tag count = %d, want 0", counts[id])
				}
				continue
			}
			single, err := a.GetDescendantClipCount(id, arch)
			if err != nil {
				t.Fatal(err)
			}
			if counts[id] != single {
				t.Errorf("archived=%v tag %d: batch %d != single %d", arch, id, counts[id], single)
			}
		}
	}

	counts, _ := a.GetDescendantClipCounts(ids, false)
	want := map[int64]int{work: 3, sub: 2, deep: 1, underscore: 1, lookalike: 1, other: 1, missing: 0}
	for id, w := range want {
		if counts[id] != w {
			t.Errorf("live count for tag %d = %d, want %d", id, counts[id], w)
		}
	}
	archCounts, _ := a.GetDescendantClipCounts([]int64{work, sub}, true)
	if archCounts[work] != 1 || archCounts[sub] != 1 {
		t.Errorf("archived counts = %v, want work=1 sub=1", archCounts)
	}

	// More tags than one chunk binds.
	many := make([]int64, 0, descendantClipCountsChunk+5)
	for i := 0; i < descendantClipCountsChunk+4; i++ {
		many = append(many, int64(100000+i))
	}
	many = append(many, work)
	big, err := a.GetDescendantClipCounts(many, false)
	if err != nil {
		t.Fatal(err)
	}
	if big[work] != 3 || len(big) != len(many) {
		t.Errorf("chunked counts: work=%d len=%d, want 3 and %d", big[work], len(big), len(many))
	}
}

func TestAPI_TagClipCounts_ScopedKey(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()

	work, _ := a.CreateTag("work")
	sub, _ := a.CreateTag("work/sub")
	personal, _ := a.CreateTag("personal")
	deep, _ := a.CreateTag("work/sub/deep")
	lookalike, _ := a.CreateTag("work/subx") // shares a prefix, not the subtree
	for _, tag := range []int64{work.ID, sub.ID, personal.ID, deep.ID, lookalike.ID} {
		id := insertTestClip(t, a, fmt.Sprintf("c%d", tag), "text/plain", []byte("x"))
		if _, err := a.db.Exec("INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, ?)", id, tag); err != nil {
			t.Fatal(err)
		}
	}
	scopedKey := "scoped-count-key-99999"
	insertScopedAdminKey(t, a, scopedKey, sub.ID)

	am := &APIManager{app: a}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/tags/clip-counts", am.authMiddleware(am.requireRole("viewer", am.handleTagClipCounts)))
	handler := am.corsMiddleware(mux)

	url := fmt.Sprintf("/api/v1/tags/clip-counts?archived=false&tag=%d&tag=%d&tag=%d&tag=%d&tag=%d&tag=99999",
		work.ID, sub.ID, personal.ID, deep.ID, lookalike.ID)
	req := httptest.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+scopedKey)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got map[string]int
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]int{
		fmt.Sprint(work.ID):      0, // ancestor of the scope: outside it
		fmt.Sprint(sub.ID):       2, // its own clip and deep's
		fmt.Sprint(personal.ID):  0,
		fmt.Sprint(deep.ID):      1,
		fmt.Sprint(lookalike.ID): 0,
		"99999":                  0, // no such tag
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("count[%s] = %d, want %d (all: %v)", k, got[k], w, got)
		}
	}

	bad := httptest.NewRequest("GET", "/api/v1/tags/clip-counts?tag=abc", nil)
	bad.Header.Set("Authorization", "Bearer "+scopedKey)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, bad)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad id status = %d, want 400", rec.Code)
	}
}
