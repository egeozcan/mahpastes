package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestOpenWithAppsFromScriptOrdersDefaultFirst(t *testing.T) {
	out := []byte(`{"defaultApp":{"path":"/System/Applications/Preview.app","name":"Preview"},
		"apps":[
			{"path":"/Applications/Pixelmator Pro.app","name":"Pixelmator Pro.app"},
			{"path":"/System/Applications/Preview.app","name":"Preview"},
			{"path":"/Applications/Affinity Photo.app","name":"Affinity Photo"},
			{"path":"","name":"Nameless"},
			{"path":"/Applications/Old/Affinity Photo.app/","name":"affinity photo"}
		]}` + "\n")
	got, err := openWithAppsFromScript(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []OpenWithApp{
		{Name: "Preview", Path: "/System/Applications/Preview.app", IsDefault: true},
		{Name: "Affinity Photo", Path: "/Applications/Affinity Photo.app"},
		{Name: "affinity photo", Path: "/Applications/Old/Affinity Photo.app"},
		{Name: "Pixelmator Pro", Path: "/Applications/Pixelmator Pro.app"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestOpenWithAppsFromScriptWithoutDefaultOrApps(t *testing.T) {
	got, err := openWithAppsFromScript([]byte(`{"defaultApp":null,"apps":[]}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("got %#v, want an empty (non-nil) list", got)
	}

	// Pre-macOS 12: only the default application is known.
	got, err = openWithAppsFromScript([]byte(`{"defaultApp":{"path":"/Applications/TextEdit.app","name":""},"apps":[]}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 1 || got[0].Name != "TextEdit" || !got[0].IsDefault {
		t.Fatalf("got %+v, want TextEdit named from its bundle", got)
	}
}

func TestOpenWithAppsFromScriptRejectsGarbage(t *testing.T) {
	if _, err := openWithAppsFromScript([]byte("execution error: oops (-2700)")); err == nil {
		t.Fatal("non-JSON output must be an error")
	}
}

// fakeOpenWithLister swaps the platform lister and the cache for a test.
func fakeOpenWithLister(t *testing.T, fn func(probe string) ([]OpenWithApp, error)) {
	t.Helper()
	origLister, origNow := openWithAppLister, openWithAppsNow
	openWithAppsCache.Lock()
	origEntries := openWithAppsCache.entries
	openWithAppsCache.entries = map[string]openWithAppsCacheEntry{}
	openWithAppsCache.Unlock()
	t.Cleanup(func() {
		openWithAppLister, openWithAppsNow = origLister, origNow
		openWithAppsCache.Lock()
		openWithAppsCache.entries = origEntries
		openWithAppsCache.Unlock()
	})
	openWithAppLister = fn
}

func TestLookupOpenWithAppsProbesByExtensionAndCaches(t *testing.T) {
	calls := 0
	var probes []string
	fakeOpenWithLister(t, func(probe string) ([]OpenWithApp, error) {
		calls++
		probes = append(probes, probe)
		info, err := os.Stat(probe)
		if err != nil {
			t.Errorf("probe %s does not exist during the lookup: %v", probe, err)
		} else if info.Size() != 0 {
			t.Errorf("probe has %d bytes, want an empty file", info.Size())
		}
		return []OpenWithApp{{Name: "Preview", Path: "/System/Applications/Preview.app", IsDefault: true}}, nil
	})
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	openWithAppsNow = func() time.Time { return now }

	apps, err := lookupOpenWithApps(".PNG")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if len(apps) != 1 || apps[0].Name != "Preview" {
		t.Fatalf("apps = %+v", apps)
	}
	if filepath.Ext(probes[0]) != ".PNG" {
		t.Fatalf("probe %q does not carry the clip's extension", probes[0])
	}
	if _, err := os.Stat(filepath.Dir(probes[0])); !os.IsNotExist(err) {
		t.Fatalf("probe directory left behind: %v", err)
	}

	// Mutating the returned slice must not reach the cache.
	apps[0].Name = "changed"
	again, err := lookupOpenWithApps(".png")
	if err != nil || calls != 1 {
		t.Fatalf("cached lookup: err=%v calls=%d, want a cache hit", err, calls)
	}
	if again[0].Name != "Preview" {
		t.Fatalf("cache was mutated through a returned slice: %+v", again)
	}

	now = now.Add(openWithAppsCacheTTL + time.Second)
	if _, err := lookupOpenWithApps(".png"); err != nil || calls != 2 {
		t.Fatalf("expired lookup: err=%v calls=%d, want a fresh lookup", err, calls)
	}

	if _, err := lookupOpenWithApps(""); err != nil || calls != 3 {
		t.Fatalf("extensionless lookup: err=%v calls=%d", err, calls)
	}
	if base := filepath.Base(probes[2]); base != "clip" {
		t.Fatalf("extensionless probe = %q, want clip", base)
	}
}

func TestLookupOpenWithAppsDoesNotCacheFailures(t *testing.T) {
	calls := 0
	fakeOpenWithLister(t, func(string) ([]OpenWithApp, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("osascript timed out")
		}
		return []OpenWithApp{}, nil
	})
	if _, err := lookupOpenWithApps(".txt"); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want the lister's error", err)
	}
	if _, err := lookupOpenWithApps(".txt"); err != nil || calls != 2 {
		t.Fatalf("retry: err=%v calls=%d, want the failure not cached", err, calls)
	}
}

func TestLookupOpenWithAppsUnsupported(t *testing.T) {
	fakeOpenWithLister(t, nil)
	if _, err := lookupOpenWithApps(".txt"); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("err = %v, want unsupported", err)
	}
}

func TestListOpenWithAppsUsesClipExtension(t *testing.T) {
	tempDir := t.TempDir()
	db := newTransferServiceTestDB(t, tempDir)
	if _, err := db.Exec(`INSERT INTO clips (id, content_type, data, filename) VALUES
		(1, 'image/jpeg', 'x', 'holiday.jpeg'),
		(2, 'application/pdf', 'x', NULL)`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	core := newTransferServiceTestApp(t, db, tempDir)
	d := &App{desktopCore: core, core: core}

	var exts []string
	fakeOpenWithLister(t, func(probe string) ([]OpenWithApp, error) {
		exts = append(exts, filepath.Ext(probe))
		return []OpenWithApp{}, nil
	})
	for _, id := range []int64{1, 2} {
		if _, err := d.ListOpenWithApps(id); err != nil {
			t.Fatalf("ListOpenWithApps(%d): %v", id, err)
		}
	}
	if !reflect.DeepEqual(exts, []string{".jpeg", ".pdf"}) {
		t.Fatalf("probe extensions = %v, want [.jpeg .pdf]", exts)
	}
	if _, err := d.ListOpenWithApps(99); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want clip not found", err)
	}
}
