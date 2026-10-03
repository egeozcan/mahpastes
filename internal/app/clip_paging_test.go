package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The gallery used to stop at defaultClipLimit with no way to see the rest:
// "50 clips" with 120 in the library. ListClipsPage pages through every
// listing mode and reports the total.
func TestListClipsPageWalksWholeListing(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()

	const n = 120
	for i := 0; i < n; i++ {
		insertTestClip(t, a, fmt.Sprintf("clip-%03d.txt", i), "text/plain", []byte(fmt.Sprintf("body %d", i)))
	}

	seen := map[int64]bool{}
	offset := 0
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("paging did not terminate")
		}
		page, err := a.ListClipsPage(ClipListRequest{Offset: offset, Limit: defaultClipLimit})
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != n {
			t.Fatalf("total = %d, want %d", page.Total, n)
		}
		for _, c := range page.Clips {
			if seen[c.ID] {
				t.Fatalf("clip %d returned on two pages", c.ID)
			}
			seen[c.ID] = true
		}
		offset += len(page.Clips)
		if !page.HasMore {
			break
		}
		if len(page.Clips) != defaultClipLimit {
			t.Fatalf("non-final page has %d clips, want %d", len(page.Clips), defaultClipLimit)
		}
	}
	if len(seen) != n {
		t.Fatalf("paged through %d clips, want %d", len(seen), n)
	}
}

func TestListClipsPageModesMatchUnpagedListings(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()

	work := mustCreateTag(t, a, "work")
	sub := mustCreateTag(t, a, "work/sub")
	for i := 0; i < 60; i++ {
		id := insertTestClip(t, a, fmt.Sprintf("w-%02d.txt", i), "text/plain", []byte("needle"))
		if err := a.AddTagToClip(id, work); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		id := insertTestClip(t, a, fmt.Sprintf("s-%02d.txt", i), "text/plain", []byte("x"))
		if err := a.AddTagToClip(id, sub); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 7; i++ {
		insertTestClip(t, a, fmt.Sprintf("u-%02d.txt", i), "text/plain", []byte("needle"))
	}

	cases := []struct {
		name string
		req  ClipListRequest
		want int
	}{
		{"all", ClipListRequest{}, 70},
		{"filtered includes descendants", ClipListRequest{TagIDs: []int64{work}}, 63},
		{"folder is exact level", ClipListRequest{Mode: "folder", FolderTagID: work}, 60},
		{"untagged", ClipListRequest{Mode: "untagged"}, 7},
		{"search in content", ClipListRequest{Mode: "search", Query: "needle", SearchContent: true}, 67},
	}
	for _, tc := range cases {
		page, err := a.ListClipsPage(tc.req)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if page.Total != tc.want {
			t.Errorf("%s: total = %d, want %d", tc.name, page.Total, tc.want)
		}
		wantLen := tc.want
		if wantLen > defaultClipLimit {
			wantLen = defaultClipLimit
		}
		if len(page.Clips) != wantLen || page.HasMore != (tc.want > defaultClipLimit) {
			t.Errorf("%s: got %d clips has_more=%v", tc.name, len(page.Clips), page.HasMore)
		}
	}

	// The unpaged folder listing is still the first page of the paged one.
	unpaged, err := a.GetFolderClips(false, work, "", "")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := a.ListClipsPage(ClipListRequest{Mode: "folder", FolderTagID: work})
	for i := range unpaged {
		if unpaged[i].ID != page.Clips[i].ID {
			t.Fatalf("folder page order differs from GetFolderClips at %d", i)
		}
	}

	if _, err := a.ListClipsPage(ClipListRequest{Mode: "bogus"}); err == nil {
		t.Fatal("unknown mode should be rejected")
	}
}

// Server mode's gallery pages through GET /api/v1/clips with offset; the
// via-app path used to slice a listing already capped at 50, so any offset
// past it came back empty with total 50.
func TestListClipsViaAppPagesPastDefaultLimit(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()
	for i := 0; i < 70; i++ {
		insertTestClip(t, a, fmt.Sprintf("r-%02d.txt", i), "text/plain", []byte("x"))
	}
	manager := NewAPIManager(a)

	req := httptest.NewRequest("GET", "/api/v1/clips?sort=created_at&dir=desc&untagged=true&offset=50&limit=50", nil)
	rec := httptest.NewRecorder()
	manager.handleListClipsViaApp(rec, req, 50, 50, &apiKeyContext{KeyID: 1, Role: "viewer"})
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d: %s", rec.Code, rec.Body.String())
	}
	var resp apiClipListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Total != 70 || len(resp.Clips) != 20 || resp.Offset != 50 {
		t.Fatalf("total=%d clips=%d offset=%d, want 70/20/50", resp.Total, len(resp.Clips), resp.Offset)
	}
}

// REST folder and untagged listings with a deep search used to drop the
// search: the paged branch took them but never applied the query.
func TestListClipsViaAppFolderAndUntaggedHonourDeepSearch(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()
	work := mustCreateTag(t, a, "work")
	for _, n := range []string{"needle-a.txt", "hay-a.txt"} {
		id := insertTestClip(t, a, n, "text/plain", []byte("x"))
		if err := a.AddTagToClip(id, work); err != nil {
			t.Fatal(err)
		}
	}
	insertTestClip(t, a, "needle-u.txt", "text/plain", []byte("x"))
	insertTestClip(t, a, "hay-u.txt", "text/plain", []byte("x"))
	manager := NewAPIManager(a)

	for _, tc := range []struct{ query, want string }{
		{fmt.Sprintf("folder_tag=%d", work), "needle-a.txt"},
		{"untagged=true", "needle-u.txt"},
	} {
		req := httptest.NewRequest("GET", "/api/v1/clips?"+tc.query+"&search=needle&search_content=true", nil)
		rec := httptest.NewRecorder()
		manager.handleListClipsViaApp(rec, req, 50, 0, &apiKeyContext{KeyID: 1, Role: "viewer"})
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: code = %d: %s", tc.query, rec.Code, rec.Body.String())
		}
		var resp apiClipListResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.Total != 1 || len(resp.Clips) != 1 || resp.Clips[0].Filename != tc.want {
			t.Fatalf("%s: got total=%d clips=%+v, want only %s", tc.query, resp.Total, resp.Clips, tc.want)
		}
	}
}

// A page past the end still reports the listing's size.
func TestListClipsPageTotalPastTheEnd(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()
	for i := 0; i < 5; i++ {
		insertTestClip(t, a, fmt.Sprintf("e-%d.txt", i), "text/plain", []byte("x"))
	}
	page, err := a.ListClipsPage(ClipListRequest{Offset: 10, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 5 || len(page.Clips) != 0 || page.HasMore {
		t.Fatalf("total=%d clips=%d hasMore=%v, want 5/0/false", page.Total, len(page.Clips), page.HasMore)
	}
	page, err = a.ListClipsPage(ClipListRequest{Offset: 2, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 5 || len(page.Clips) != 2 || !page.HasMore {
		t.Fatalf("total=%d clips=%d hasMore=%v, want 5/2/true", page.Total, len(page.Clips), page.HasMore)
	}
}
