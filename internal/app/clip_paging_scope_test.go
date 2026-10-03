package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A tag-scoped key listing through the paged REST branch sees only its own
// subtree: in the total, and on every page — including pages past the first
// defaultClipLimit, which the old capped listing never reached.
func TestListClipsViaAppPagedBranchStaysInKeyScope(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()

	work := mustCreateTag(t, a, "work")
	sub := mustCreateTag(t, a, "work/sub")
	personal := mustCreateTag(t, a, "personal")
	file := func(prefix string, n int, tagID int64, contentType string) {
		for i := 0; i < n; i++ {
			id := insertTestClip(t, a, fmt.Sprintf("%s-%02d.txt", prefix, i), contentType, []byte("needle"))
			if tagID != 0 {
				if err := a.AddTagToClip(id, tagID); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	file("w", 60, work, "text/plain")
	file("wj", 4, work, "application/json")
	file("s", 5, sub, "text/plain")
	file("p", 40, personal, "text/plain")
	file("u", 20, 0, "text/plain")

	manager := NewAPIManager(a)
	key := &apiKeyContext{KeyID: 1, Role: "viewer", ScopedTagID: work}

	cases := []struct {
		name  string
		query string
		total int
	}{
		{"sorted listing", "sort=created_at&dir=desc", 69},
		{"deep search", "sort=created_at&search=needle&search_content=true", 69},
		{"deep search by name only", "search=needle&search_content=false", 0},
		{"content type", "sort=name&content_type=text/plain", 65},
		{"folder", fmt.Sprintf("folder_tag=%d", work), 64},
		{"folder with deep search", fmt.Sprintf("folder_tag=%d&search=needle&search_content=true", work), 64},
		{"folder below the scope", fmt.Sprintf("folder_tag=%d", sub), 5},
		{"folder outside the scope", fmt.Sprintf("folder_tag=%d", personal), 0},
		{"tag filter outside the scope", fmt.Sprintf("tag=%d&tag=%d", work, personal), 0},
		{"untagged", "untagged=true", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seen := 0
			for offset := 0; ; offset += 50 {
				req := httptest.NewRequest("GET", "/api/v1/clips?"+tc.query+fmt.Sprintf("&offset=%d&limit=50", offset), nil)
				rec := httptest.NewRecorder()
				manager.handleListClips(rec, req.WithContext(context.WithValue(req.Context(), apiKeyContextKey, key)))
				if rec.Code != http.StatusOK {
					t.Fatalf("offset %d: code = %d: %s", offset, rec.Code, rec.Body.String())
				}
				var resp apiClipListResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
					t.Fatal(err)
				}
				if resp.Total != tc.total {
					t.Fatalf("offset %d: total = %d, want %d", offset, resp.Total, tc.total)
				}
				for _, c := range resp.Clips {
					if !strings.HasPrefix(c.Filename, "w") && !strings.HasPrefix(c.Filename, "s-") {
						t.Fatalf("offset %d: clip %q is outside the key's scope", offset, c.Filename)
					}
				}
				seen += len(resp.Clips)
				if len(resp.Clips) < 50 {
					break
				}
			}
			if seen != tc.total {
				t.Fatalf("paged through %d clips, want %d", seen, tc.total)
			}
		})
	}
}

// A folder deleted while it is open (orphan auto-delete, another client)
// lists as empty, not as a server error.
func TestListClipsPageMissingFolderIsEmpty(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()
	insertTestClip(t, a, "x.txt", "text/plain", []byte("x"))

	page, err := a.ListClipsPage(ClipListRequest{Mode: "folder", FolderTagID: 987654})
	if err != nil {
		t.Fatalf("ListClipsPage: %v", err)
	}
	if page.Total != 0 || len(page.Clips) != 0 || page.HasMore {
		t.Fatalf("total=%d clips=%d hasMore=%v, want an empty page", page.Total, len(page.Clips), page.HasMore)
	}
}

// A row that fails to scan is an error, not a silently shorter page: a short
// page under-reports has_more and the gallery stops loading.
// One unreadable row (a stray value from an old restore, say) must not take
// the whole gallery down: it is skipped, and has_more still counts it, so
// paging past it neither stops early nor loops.
func TestListClipsPageSkipsUnreadableRow(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()
	insertTestClip(t, a, "good1.txt", "text/plain", []byte("x"))
	bad := insertTestClip(t, a, "bad.txt", "text/plain", []byte("x"))
	insertTestClip(t, a, "good2.txt", "text/plain", []byte("x"))
	if _, err := a.db.Exec(`UPDATE clips SET created_at = 'not a time' WHERE id = ?`, bad); err != nil {
		t.Fatal(err)
	}

	page, err := a.ListClipsPage(ClipListRequest{})
	if err != nil {
		t.Fatalf("ListClipsPage failed over one unreadable row: %v", err)
	}
	if len(page.Clips) != 2 || page.Total != 3 || page.HasMore {
		t.Fatalf("got %d clips, total %d, has_more %v; want 2, 3, false", len(page.Clips), page.Total, page.HasMore)
	}

	seen := 0
	for offset, pages := 0, 0; ; pages++ {
		if pages > 5 {
			t.Fatal("paging one clip at a time never ended")
		}
		p, err := a.ListClipsPage(ClipListRequest{Offset: offset, Limit: 1})
		if err != nil {
			t.Fatalf("page at offset %d: %v", offset, err)
		}
		seen += len(p.Clips)
		if !p.HasMore {
			break
		}
		offset++
	}
	if seen != 2 {
		t.Fatalf("paged through %d readable clips, want 2", seen)
	}
}
