package main

import (
	"testing"
)

func TestCheckTagReferencePreconditions_NoBlockers(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	tag, err := app.CreateTag("unreferenced")
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	blockers, err := app.checkTagReferencePreconditions(tag.ID)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(blockers) != 0 {
		t.Fatalf("expected no blockers, got %v", blockers)
	}
}

func TestCheckTagReferencePreconditions_BlockedByFollow(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	tag, err := app.CreateTag("subscribed")
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if _, err := app.db.Exec(`INSERT INTO follows
		(remote_peer_id, symkey, local_tag_id, created_at)
		VALUES ('peer', X'00', ?, strftime('%s','now'))`, tag.ID); err != nil {
		t.Fatalf("insert follow: %v", err)
	}
	blockers, err := app.checkTagReferencePreconditions(tag.ID)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(blockers) == 0 {
		t.Fatalf("expected follow blocker, got empty")
	}
}
