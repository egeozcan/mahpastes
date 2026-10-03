package app

import (
	"fmt"
	"slices"
	"testing"
)

// --- Exclusivity drops caused by a tag move are announced -------------------
//
// A rename or merge that moves tags into another root tree drops the clips'
// other tags in that tree. Unlike AddTagToClip's own exclusivity removals —
// implied by the tag:added_to_clip event of the tag that displaced them — no
// add event accompanies a move, so without a removal event a plugin tracking
// x/y never learns that a clip left it.

func TestUpdateTagMoveAnnouncesDroppedTags(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	ab := mustCreateTag(t, app, "a/b")
	xy := mustCreateTag(t, app, "x/y")
	clipID := tagOneClip(t, app, ab)
	if err := app.AddTagToClip(clipID, xy); err != nil {
		t.Fatal(err)
	}
	pm, pluginID := loadWiredTestPlugin(t, app, tagEventLogPluginSrc)
	defer pm.Shutdown()

	if err := app.UpdateTag(ab, "x/b", ""); err != nil {
		t.Fatalf("UpdateTag: %v", err)
	}
	if got, want := clipTagNames(t, app, clipID), []string{"x/b"}; !slices.Equal(got, want) {
		t.Fatalf("clip tags = %v, want %v", got, want)
	}
	if got, want := tagEventLog(t, app, pluginID, "removed"), []string{eventEntry(clipID, xy)}; !slices.Equal(got, want) {
		t.Fatalf("tag:removed_from_clip events = %v, want %v", got, want)
	}
}

func TestMergeTagAnnouncesDroppedTags(t *testing.T) {
	t.Run("clip holding the source", func(t *testing.T) {
		app, cleanup := setupTestApp(t)
		defer cleanup()
		ab := mustCreateTag(t, app, "a/b")
		x := mustCreateTag(t, app, "x")
		xy := mustCreateTag(t, app, "x/y")
		clipID := tagOneClip(t, app, ab)
		if err := app.AddTagToClip(clipID, xy); err != nil {
			t.Fatal(err)
		}
		pm, pluginID := loadWiredTestPlugin(t, app, tagEventLogPluginSrc)
		defer pm.Shutdown()

		if err := app.MergeTag(ab, x); err != nil {
			t.Fatalf("MergeTag: %v", err)
		}
		if got, want := clipTagNames(t, app, clipID), []string{"x"}; !slices.Equal(got, want) {
			t.Fatalf("clip tags = %v, want %v", got, want)
		}
		// The source's own rows go with the source (tag:merged covers them);
		// only x/y left a tag that still exists.
		if got, want := tagEventLog(t, app, pluginID, "removed"), []string{eventEntry(clipID, xy)}; !slices.Equal(got, want) {
			t.Fatalf("tag:removed_from_clip events = %v, want %v", got, want)
		}
	})
	t.Run("clip holding a source descendant", func(t *testing.T) {
		app, cleanup := setupTestApp(t)
		defer cleanup()
		ab := mustCreateTag(t, app, "a/b")
		abc := mustCreateTag(t, app, "a/b/c")
		x := mustCreateTag(t, app, "x")
		xy := mustCreateTag(t, app, "x/y")
		clipID := tagOneClip(t, app, abc)
		if err := app.AddTagToClip(clipID, xy); err != nil {
			t.Fatal(err)
		}
		pm, pluginID := loadWiredTestPlugin(t, app, tagEventLogPluginSrc)
		defer pm.Shutdown()

		if err := app.MergeTag(ab, x); err != nil {
			t.Fatalf("MergeTag: %v", err)
		}
		if got, want := clipTagNames(t, app, clipID), []string{"x/c"}; !slices.Equal(got, want) {
			t.Fatalf("clip tags = %v, want %v", got, want)
		}
		if got, want := tagEventLog(t, app, pluginID, "removed"), []string{eventEntry(clipID, xy)}; !slices.Equal(got, want) {
			t.Fatalf("tag:removed_from_clip events = %v, want %v", got, want)
		}
	})
}

// A cross-tree move binds the moved ids once (as a JSON array), not once per
// use: binding them twice failed past ~16k descendants with "too many SQL
// variables" and made a large subtree impossible to move.
func TestUpdateTagMovesLargeSubtreeAcrossTrees(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	ab := mustCreateTag(t, app, "a/b")
	xy := mustCreateTag(t, app, "x/y")

	const descendants = 17000
	tx, err := app.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO tags (name, color) VALUES (?, '#78716c')`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < descendants; i++ {
		if _, err := stmt.Exec(fmt.Sprintf("a/b/c%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	stmt.Close()
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var leaf int64
	if err := app.db.QueryRow(`SELECT id FROM tags WHERE name = 'a/b/c0'`).Scan(&leaf); err != nil {
		t.Fatal(err)
	}
	clipID := tagOneClip(t, app, leaf)
	if err := app.AddTagToClip(clipID, xy); err != nil {
		t.Fatal(err)
	}

	if err := app.UpdateTag(ab, "x/b", ""); err != nil {
		t.Fatalf("UpdateTag: %v", err)
	}
	if got, want := clipTagNames(t, app, clipID), []string{"x/b/c0"}; !slices.Equal(got, want) {
		t.Fatalf("clip tags = %v, want %v", got, want)
	}
}
