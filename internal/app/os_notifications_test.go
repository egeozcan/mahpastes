package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOSNotificationsSettingDefaultsOff(t *testing.T) {
	app := newSettingsTestApp(t)
	if app.OSNotificationsEnabled() {
		t.Fatal("fresh install: notifications enabled, want off")
	}
	if err := app.SetOSNotificationsEnabled(true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !app.OSNotificationsEnabled() {
		t.Fatal("after enable: still off")
	}
	if err := app.SetOSNotificationsEnabled(false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if app.OSNotificationsEnabled() {
		t.Fatal("after disable: still on")
	}
	if (&App{}).OSNotificationsEnabled() {
		t.Fatal("no database: want off")
	}
}

// The Serve view's remembered list names ports, bind modes and API access:
// it stays on the machine that chose them.
func TestServeEntriesSettingIsMachineLocal(t *testing.T) {
	if !isSensitiveSetting(SettingServeEntries) {
		t.Error("serve_entries would be included in backups")
	}
	app := newSettingsTestApp(t)
	manager := NewAPIManager(app)
	req := httptest.NewRequest("PUT", "/api/v1/settings/"+SettingServeEntries, strings.NewReader(`{"value":"[]"}`))
	req.SetPathValue("key", SettingServeEntries)
	rec := httptest.NewRecorder()
	manager.handleSetSetting(rec, withKey(req, &apiKeyContext{KeyID: 1, Role: "admin"}))
	if rec.Code != http.StatusForbidden {
		t.Errorf("PUT serve_entries: code = %d, want 403", rec.Code)
	}
	if v, _ := app.GetSetting(SettingServeEntries); v != "" {
		t.Errorf("PUT serve_entries stored %q despite the refusal", v)
	}
}

func TestOSNotificationsSettingIsMachineLocal(t *testing.T) {
	if !isSensitiveSetting(SettingOSNotifications) {
		t.Error("os_notifications would be included in backups")
	}

	app := newSettingsTestApp(t)
	manager := NewAPIManager(app)
	req := httptest.NewRequest("PUT", "/api/v1/settings/"+SettingOSNotifications, strings.NewReader(`{"value":"true"}`))
	req.SetPathValue("key", SettingOSNotifications)
	rec := httptest.NewRecorder()
	manager.handleSetSetting(rec, withKey(req, &apiKeyContext{KeyID: 1, Role: "admin"}))
	if rec.Code != http.StatusForbidden {
		t.Errorf("PUT os_notifications: code = %d, want 403", rec.Code)
	}
	if app.OSNotificationsEnabled() {
		t.Error("PUT os_notifications stored the value despite the refusal")
	}
}

func TestClassifyBackgroundEvent(t *testing.T) {
	app := newSettingsTestApp(t)
	if _, err := app.db.Exec(`INSERT INTO tags (id, name, color) VALUES (7, 'inbox', '#000')`); err != nil {
		t.Fatal(err)
	}
	if _, err := app.db.Exec(`CREATE TABLE plugins (id INTEGER PRIMARY KEY, filename TEXT, name TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := app.db.Exec(`INSERT INTO plugins (id, filename, name) VALUES (3, 'x.lua', 'Auto Tagger')`); err != nil {
		t.Fatal(err)
	}

	type want struct {
		key, title, body string
	}
	cases := []struct {
		name  string
		event string
		data  any
		count int
		want  want
	}{
		{"import", "watch:import", ClipPreview{Filename: "photo.png"}, 1,
			want{"import", "Clip imported", "Imported photo.png"}},
		{"import pointer burst", "watch:import", &ClipPreview{Filename: "b.png"}, 12,
			want{"import", "Clips imported", "Imported 12 clips"}},
		{"import error", "watch:error", map[string]string{"file": "a.pdf", "error": "too large"}, 1,
			want{"import-error", "Import failed", "Could not import a.pdf: too large"}},
		{"share", "share:clip-received", map[string]any{"clip_id": int64(1), "local_tag_id": int64(7), "follow_id": int64(2)}, 3,
			want{"share:7", "Shared clips received", "3 new clips in inbox"}},
		{"share unknown tag", "share:clip-received", map[string]any{"local_tag_id": int64(99)}, 1,
			want{"share:99", "Shared clip received", "1 new clip from a followed share"}},
		{"task done", "plugin:task:completed", map[string]interface{}{"task_id": int64(1), "plugin_id": int64(3), "name": "Tagging"}, 1,
			want{"task-done:3", "Auto Tagger", "Finished: Tagging"}},
		{"task failed", "plugin:task:failed", map[string]interface{}{"plugin_id": int64(3), "name": "Upload", "error": "timeout"}, 2,
			want{"task-failed:3", "Auto Tagger", "2 tasks failed. Latest: Upload: timeout"}},
		{"toast", "plugin:toast", map[string]string{"message": "Saved &amp; tagged", "type": "info", "plugin_id": "3"}, 1,
			want{"toast:3", "Auto Tagger", "Saved & tagged"}},
		{"toast without plugin", "plugin:toast", map[string]string{"message": "Cannot show result", "type": "error"}, 1,
			want{"toast:0", "Plugin", "Cannot show result"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, ok := app.ClassifyBackgroundEvent(tc.event, tc.data)
			if !ok {
				t.Fatalf("%s not classified", tc.event)
			}
			if n.Key != tc.want.key {
				t.Errorf("key = %q, want %q", n.Key, tc.want.key)
			}
			label := ""
			if n.Label != nil {
				label = n.Label()
			}
			title, body := n.Format(tc.count, label, n.Detail)
			if title != tc.want.title || body != tc.want.body {
				t.Errorf("got (%q, %q), want (%q, %q)", title, body, tc.want.title, tc.want.body)
			}
		})
	}

	for _, ev := range []string{"clip:created", "plugin:task:progress", "share:follow-updated", "watch:status", "tag:created"} {
		if _, ok := app.ClassifyBackgroundEvent(ev, map[string]any{}); ok {
			t.Errorf("%s classified as a background notification", ev)
		}
	}
	// Malformed payloads must not panic.
	for _, ev := range []string{"watch:import", "watch:error", "share:clip-received", "plugin:task:completed", "plugin:toast"} {
		app.ClassifyBackgroundEvent(ev)
		app.ClassifyBackgroundEvent(ev, 42)
	}
}
