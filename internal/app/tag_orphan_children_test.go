package app

import (
	"reflect"
	"testing"
)

func tagNamesOf(tags []Tag) []string {
	names := []string{}
	for _, t := range tags {
		names = append(names, t.Name)
	}
	return names
}

// Deleting the mid-level tag a/b leaves a/b/c behind. Folder mode lists
// children of a, and a's card count already includes a/b/c's clips, so the
// listing must reach a/b/c through its nearest existing ancestor (a) rather
// than drop it.
func TestGetChildTags_DescendantOfDeletedTagShowsUnderNearestAncestor(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()

	aID := mustCreateTag(t, a, "a")
	mustCreateTag(t, a, "a/b/c")
	mustCreateTag(t, a, "a/b/c/d") // grandchild of the gap: stays under a/b/c
	mustCreateTag(t, a, "a/e")
	var abID int64
	if err := a.db.QueryRow(`SELECT id FROM tags WHERE name = 'a/b'`).Scan(&abID); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteTag(abID); err != nil {
		t.Fatalf("DeleteTag(a/b): %v", err)
	}

	children, err := a.GetChildTags(aID)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := tagNamesOf(children), []string{"a/b/c", "a/e"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("children of a = %v, want %v", got, want)
	}
}

// With no existing ancestor at all, the tag belongs at the top level.
func TestGetTopLevelTags_IncludesTagsWithNoExistingAncestor(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()

	mustCreateTag(t, a, "p/q")
	mustCreateTag(t, a, "p/q/r")
	mustCreateTag(t, a, "z")
	var pID int64
	if err := a.db.QueryRow(`SELECT id FROM tags WHERE name = 'p'`).Scan(&pID); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteTag(pID); err != nil {
		t.Fatalf("DeleteTag(p): %v", err)
	}

	top, err := a.GetTopLevelTags()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := tagNamesOf(top), []string{"p/q", "z"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("top-level tags = %v, want %v", got, want)
	}
}
