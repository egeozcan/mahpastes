package app

import (
	"sync"
	"testing"
)

type runtimeEvent struct {
	name string
	data []interface{}
}

func captureRuntimeEvents(t *testing.T, a *App) func(name string) []runtimeEvent {
	t.Helper()
	var mu sync.Mutex
	var events []runtimeEvent
	setTestEventSink(t, a, func(name string, data ...interface{}) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, runtimeEvent{name: name, data: data})
	})
	return func(name string) []runtimeEvent {
		mu.Lock()
		defer mu.Unlock()
		var out []runtimeEvent
		for _, e := range events {
			if e.name == name {
				out = append(out, e)
			}
		}
		return out
	}
}

func eventPayload(t *testing.T, e runtimeEvent) map[string]any {
	t.Helper()
	if len(e.data) != 1 {
		t.Fatalf("%s: want one payload, got %d", e.name, len(e.data))
	}
	m, ok := e.data[0].(map[string]any)
	if !ok {
		t.Fatalf("%s: payload is %T, want map", e.name, e.data[0])
	}
	return m
}

// A tag created by anyone other than the frontend itself (a plugin, the REST
// API, a share follow) never reached the frontend's tag cache: CreateTag only
// told plugins. Auto-created ancestors count too.
func TestCreateTagEmitsRuntimeEventsForTagAndAncestors(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()
	events := captureRuntimeEvents(t, a)

	tag, err := a.CreateTag("work/client/project")
	if err != nil {
		t.Fatal(err)
	}

	got := events("tag:created")
	var names []string
	for _, e := range got {
		names = append(names, eventPayload(t, e)["name"].(string))
	}
	want := []string{"work", "work/client", "work/client/project"}
	if len(names) != len(want) {
		t.Fatalf("tag:created names = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("tag:created names = %v, want %v", names, want)
		}
	}
	if id := eventPayload(t, got[2])["id"]; id != tag.ID {
		t.Fatalf("last tag:created id = %v, want %d", id, tag.ID)
	}
}

// Removing the last clip from a root tag auto-deletes it; the frontend kept
// showing (and filtering by) the deleted tag because no runtime event fired.
func TestOrphanedTagAutoDeleteEmitsRuntimeEvent(t *testing.T) {
	a, cleanup := setupTestApp(t)
	defer cleanup()

	clipID := insertTestClip(t, a, "a.txt", "text/plain", []byte("a"))
	tagID := mustCreateTag(t, a, "ephemeral")
	if err := a.AddTagToClip(clipID, tagID); err != nil {
		t.Fatal(err)
	}

	events := captureRuntimeEvents(t, a)
	if err := a.BulkRemoveTag([]int64{clipID}, tagID); err != nil {
		t.Fatal(err)
	}

	var exists bool
	if err := a.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM tags WHERE id = ?)`, tagID).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("precondition: emptied root tag should have been auto-deleted")
	}

	got := events("tag:deleted")
	if len(got) != 1 {
		t.Fatalf("tag:deleted runtime events = %d, want 1", len(got))
	}
	p := eventPayload(t, got[0])
	if p["id"] != tagID || p["name"] != "ephemeral" || p["auto"] != true {
		t.Fatalf("tag:deleted payload = %v, want id %d name ephemeral auto true", p, tagID)
	}
}
