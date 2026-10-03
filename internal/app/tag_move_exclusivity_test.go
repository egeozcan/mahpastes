package app

import (
	"reflect"
	"testing"
)

// Renaming a/b to x/b moves the subtree into tree x. A clip tagged a/b and x
// (legal: different trees) would end up with x and x/b — two tags in one root
// tree. The moved tag wins.
func TestUpdateTag_MoveIntoOtherTreeKeepsExclusivity(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()

	ab := mustCreateTag(t, a, "a/b")
	x := mustCreateTag(t, a, "x")
	clipID := insertTestClip(t, a, "c.txt", "text/plain", []byte("c"))
	if err := a.AddTagToClip(clipID, ab); err != nil {
		t.Fatal(err)
	}
	if err := a.AddTagToClip(clipID, x); err != nil {
		t.Fatal(err)
	}
	other := insertTestClip(t, a, "o.txt", "text/plain", []byte("o"))
	if err := a.AddTagToClip(other, x); err != nil {
		t.Fatal(err)
	}

	if err := a.UpdateTag(ab, "x/b", ""); err != nil {
		t.Fatalf("UpdateTag: %v", err)
	}

	if got := clipTagNames(t, a, clipID); !reflect.DeepEqual(got, []string{"x/b"}) {
		t.Fatalf("moved clip tags = %v, want [x/b]", got)
	}
	// A clip that never carried the moved tag is untouched.
	if got := clipTagNames(t, a, other); !reflect.DeepEqual(got, []string{"x"}) {
		t.Fatalf("unrelated clip tags = %v, want [x]", got)
	}
}

// The same holds for a clip tagged with a descendant of the moved tag.
func TestUpdateTag_MoveDescendantIntoOtherTreeKeepsExclusivity(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()

	mustCreateTag(t, a, "a/b")
	abc := mustCreateTag(t, a, "a/b/c")
	xy := mustCreateTag(t, a, "x/y")
	clipID := insertTestClip(t, a, "c.txt", "text/plain", []byte("c"))
	if err := a.AddTagToClip(clipID, abc); err != nil {
		t.Fatal(err)
	}
	if err := a.AddTagToClip(clipID, xy); err != nil {
		t.Fatal(err)
	}

	var abID int64
	if err := a.db.QueryRow(`SELECT id FROM tags WHERE name = 'a/b'`).Scan(&abID); err != nil {
		t.Fatal(err)
	}
	if err := a.UpdateTag(abID, "x/b", ""); err != nil {
		t.Fatalf("UpdateTag: %v", err)
	}
	if got := clipTagNames(t, a, clipID); !reflect.DeepEqual(got, []string{"x/b/c"}) {
		t.Fatalf("clip tags = %v, want [x/b/c]", got)
	}
}

// Merging a/b into x renames a/b/c to x/c. A clip tagged a/b/c and x/y
// would end up with x/c and x/y. The merged tag wins.
func TestMergeTag_DescendantMoveKeepsExclusivity(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()

	ab := mustCreateTag(t, a, "a/b")
	abc := mustCreateTag(t, a, "a/b/c")
	x := mustCreateTag(t, a, "x")
	xy := mustCreateTag(t, a, "x/y")
	clipID := insertTestClip(t, a, "c.txt", "text/plain", []byte("c"))
	if err := a.AddTagToClip(clipID, abc); err != nil {
		t.Fatal(err)
	}
	if err := a.AddTagToClip(clipID, xy); err != nil {
		t.Fatal(err)
	}
	other := insertTestClip(t, a, "o.txt", "text/plain", []byte("o"))
	if err := a.AddTagToClip(other, xy); err != nil {
		t.Fatal(err)
	}

	if err := a.MergeTag(ab, x); err != nil {
		t.Fatalf("MergeTag: %v", err)
	}

	if got := clipTagNames(t, a, clipID); !reflect.DeepEqual(got, []string{"x/c"}) {
		t.Fatalf("merged clip tags = %v, want [x/c]", got)
	}
	if got := clipTagNames(t, a, other); !reflect.DeepEqual(got, []string{"x/y"}) {
		t.Fatalf("unrelated clip tags = %v, want [x/y]", got)
	}
}

// The moved set is the rows the rename touched, not every tag that ends up
// under the new name: an orphan x/b/z (its parent x/b deleted) already sits
// under the destination. Selecting by name counted it as moved, so a clip
// tagged a/b and x/b/z kept both after a/b became x/b.
func TestUpdateTag_MoveOntoOrphanSubtreeKeepsExclusivity(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()

	ab := mustCreateTag(t, a, "a/b")
	res, err := a.db.Exec(`INSERT INTO tags (name, color) VALUES ('x/b/z', '#888888')`)
	if err != nil {
		t.Fatal(err)
	}
	orphan, _ := res.LastInsertId()
	clipID := insertTestClip(t, a, "c.txt", "text/plain", []byte("c"))
	if err := a.AddTagToClip(clipID, ab); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, ?)`, clipID, orphan); err != nil {
		t.Fatal(err)
	}

	if err := a.UpdateTag(ab, "x/b", ""); err != nil {
		t.Fatalf("UpdateTag: %v", err)
	}
	if got := clipTagNames(t, a, clipID); !reflect.DeepEqual(got, []string{"x/b"}) {
		t.Fatalf("clip tags = %v, want [x/b]", got)
	}
}
