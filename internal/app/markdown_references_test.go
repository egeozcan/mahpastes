package app

import (
	"encoding/base64"
	"testing"
)

func uploadReferenceTestClip(t *testing.T, app *App, filename, contentType string, tagIDs ...int64) int64 {
	t.Helper()
	id, err := app.UploadFileAndGetID(FileData{
		Name:        filename,
		ContentType: contentType,
		Data:        base64.StdEncoding.EncodeToString([]byte("content")),
	})
	if err != nil {
		t.Fatalf("upload %s: %v", filename, err)
	}
	for _, tagID := range tagIDs {
		if err := app.AddTagToClip(id, tagID); err != nil {
			t.Fatalf("tag %s: %v", filename, err)
		}
	}
	return id
}

func TestResolveMarkdownReferencesFindsSameAndDescendantTagTargets(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	docs, err := app.CreateTag("docs")
	if err != nil {
		t.Fatalf("CreateTag docs: %v", err)
	}
	assets, err := app.CreateTag("docs/assets")
	if err != nil {
		t.Fatalf("CreateTag assets: %v", err)
	}
	sourceID := uploadReferenceTestClip(t, app, "readme.md", "text/plain", docs.ID)
	sameID := uploadReferenceTestClip(t, app, "same.txt", "text/plain", docs.ID)
	descendantID := uploadReferenceTestClip(t, app, "chart.png", "image/png", assets.ID)

	results, err := app.ResolveMarkdownReferences(sourceID, []string{"same.txt", "assets/chart.png"})
	if err != nil {
		t.Fatalf("ResolveMarkdownReferences: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results len = %d, want 2", len(results))
	}
	if results[0].Status != "unique" || len(results[0].Candidates) != 1 || results[0].Candidates[0].ClipID != sameID {
		t.Fatalf("same-tag result = %+v", results[0])
	}
	if results[1].Status != "unique" || len(results[1].Candidates) != 1 || results[1].Candidates[0].ClipID != descendantID {
		t.Fatalf("descendant result = %+v", results[1])
	}
	if got := results[1].Candidates[0].MatchedTagPaths; len(got) != 1 || got[0] != "docs/assets" {
		t.Fatalf("matched tag paths = %v, want [docs/assets]", got)
	}
}

func TestResolveMarkdownReferencesHandlesRootAmbiguityArchiveAndInvalidPaths(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	docs, _ := app.CreateTag("docs")
	status, _ := app.CreateTag("status/draft")
	images, _ := app.CreateTag("images")
	sourceID := uploadReferenceTestClip(t, app, "multi.md", "text/plain", docs.ID, status.ID)
	sharedID := uploadReferenceTestClip(t, app, "shared.txt", "text/plain", docs.ID, status.ID)
	otherID := uploadReferenceTestClip(t, app, "ambiguous.txt", "text/plain", status.ID)
	firstAmbiguousID := uploadReferenceTestClip(t, app, "ambiguous.txt", "text/plain", docs.ID)
	archivedID := uploadReferenceTestClip(t, app, "archived.png", "image/png", docs.ID)
	if _, err := app.db.Exec(`UPDATE clips SET is_archived = 1 WHERE id = ?`, archivedID); err != nil {
		t.Fatalf("archive target: %v", err)
	}
	expiredID := uploadReferenceTestClip(t, app, "expired.png", "image/png", docs.ID)
	if _, err := app.db.Exec(`UPDATE clips SET expires_at = '2000-01-01 00:00:00' WHERE id = ?`, expiredID); err != nil {
		t.Fatalf("expire target: %v", err)
	}

	results, err := app.ResolveMarkdownReferences(sourceID, []string{
		"shared.txt", "ambiguous.txt", "archived.png", "expired.png", "../escape.png", "child.png?size=2", "Shared.txt",
	})
	if err != nil {
		t.Fatalf("ResolveMarkdownReferences: %v", err)
	}
	if results[0].Status != "unique" || results[0].Candidates[0].ClipID != sharedID || len(results[0].Candidates[0].MatchedTagPaths) != 2 {
		t.Fatalf("deduplicated result = %+v", results[0])
	}
	if results[1].Status != "ambiguous" || len(results[1].Candidates) != 2 {
		t.Fatalf("ambiguous result = %+v", results[1])
	}
	gotIDs := map[int64]bool{}
	for _, candidate := range results[1].Candidates {
		gotIDs[candidate.ClipID] = true
	}
	if !gotIDs[firstAmbiguousID] || !gotIDs[otherID] {
		t.Fatalf("ambiguous candidate IDs = %v", gotIDs)
	}
	if results[2].Status != "unique" || results[2].Candidates[0].ClipID != archivedID {
		t.Fatalf("archived result = %+v", results[2])
	}
	if results[3].Status != "missing" {
		t.Fatalf("expired result = %+v", results[3])
	}
	if results[4].Status != "invalid" || results[5].Status != "invalid" {
		t.Fatalf("invalid results = %+v / %+v", results[4], results[5])
	}
	if results[6].Status != "missing" {
		t.Fatalf("case-sensitive result = %+v", results[6])
	}

	untaggedSourceID := uploadReferenceTestClip(t, app, "root.md", "text/plain")
	untaggedTargetID := uploadReferenceTestClip(t, app, "root.txt", "text/plain")
	childTargetID := uploadReferenceTestClip(t, app, "child.png", "image/png", images.ID)
	rootResults, err := app.ResolveMarkdownReferences(untaggedSourceID, []string{"root.txt", "images/child.png"})
	if err != nil {
		t.Fatalf("resolve root references: %v", err)
	}
	if rootResults[0].Status != "unique" || rootResults[0].Candidates[0].ClipID != untaggedTargetID {
		t.Fatalf("untagged result = %+v", rootResults[0])
	}
	if rootResults[1].Status != "unique" || rootResults[1].Candidates[0].ClipID != childTargetID {
		t.Fatalf("root descendant result = %+v", rootResults[1])
	}
}
