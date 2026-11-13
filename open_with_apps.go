package main

import (
	"encoding/json"
	"fmt"
	"os"
	posixpath "path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// OpenWithApp is one application the operating system offers for opening a
// clip's file type, as listed in the Open With submenu.
type OpenWithApp struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	IsDefault bool   `json:"is_default"`
}

// openWithAppLister lists the applications registered for the file at
// probePath. It is nil on platforms without such a list (see
// TransferCapabilities.OpenWithApps); open_darwin.go sets it.
var openWithAppLister func(probePath string) ([]OpenWithApp, error)

// The handler list depends only on the file type, and building it starts a
// helper process, so it is cached per extension for a short while: hovering
// Open With again does not wait, and a newly installed application still
// shows up within a minute.
const openWithAppsCacheTTL = time.Minute

type openWithAppsCacheEntry struct {
	apps    []OpenWithApp
	expires time.Time
}

var openWithAppsCache = struct {
	sync.Mutex
	entries map[string]openWithAppsCacheEntry
}{entries: map[string]openWithAppsCacheEntry{}}

// openWithAppsNow is the clock for the cache, replaced in tests.
var openWithAppsNow = time.Now

// lookupOpenWithApps lists the applications for files with extension ext
// (".png", or "" for none). The lookup is made against an empty probe file
// of that type rather than the clip itself, so a large clip is not written
// to disk just to show a menu; the system decides by extension either way.
func lookupOpenWithApps(ext string) ([]OpenWithApp, error) {
	if openWithAppLister == nil {
		return nil, fmt.Errorf("listing applications is not supported on this platform")
	}
	key := strings.ToLower(ext)
	now := openWithAppsNow()
	openWithAppsCache.Lock()
	if e, ok := openWithAppsCache.entries[key]; ok && now.Before(e.expires) {
		openWithAppsCache.Unlock()
		return append([]OpenWithApp(nil), e.apps...), nil
	}
	openWithAppsCache.Unlock()

	dir, err := os.MkdirTemp("", "mahpastes-openwith-")
	if err != nil {
		return nil, fmt.Errorf("failed to create probe directory: %w", err)
	}
	defer os.RemoveAll(dir)
	probe := filepath.Join(dir, "clip"+ext)
	if err := os.WriteFile(probe, nil, 0o600); err != nil {
		return nil, fmt.Errorf("failed to create probe file: %w", err)
	}

	apps, err := openWithAppLister(probe)
	if err != nil {
		return nil, err
	}

	openWithAppsCache.Lock()
	for k, e := range openWithAppsCache.entries {
		if !now.Before(e.expires) {
			delete(openWithAppsCache.entries, k)
		}
	}
	openWithAppsCache.entries[key] = openWithAppsCacheEntry{apps: apps, expires: now.Add(openWithAppsCacheTTL)}
	openWithAppsCache.Unlock()
	return append([]OpenWithApp(nil), apps...), nil
}

// openWithScriptApp and openWithScriptResult are the JSON the macOS lookup
// script prints.
type openWithScriptApp struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

type openWithScriptResult struct {
	DefaultApp *openWithScriptApp  `json:"defaultApp"`
	Apps       []openWithScriptApp `json:"apps"`
}

// openWithAppsFromScript turns the lookup script's output into the menu's
// list: the default application first, then the rest by name. The same
// application reported twice is listed once, and entries without a path are
// dropped.
func openWithAppsFromScript(out []byte) ([]OpenWithApp, error) {
	var res openWithScriptResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &res); err != nil {
		return nil, fmt.Errorf("unexpected application list: %w", err)
	}
	seen := map[string]bool{}
	var apps []OpenWithApp
	add := func(a openWithScriptApp, isDefault bool) {
		path := strings.TrimSpace(a.Path)
		if path == "" {
			return
		}
		// The script reports macOS paths: clean them as slash paths whatever
		// the host, so the parser behaves (and tests) the same everywhere.
		path = posixpath.Clean(path)
		if seen[path] {
			return
		}
		seen[path] = true
		apps = append(apps, OpenWithApp{Name: openWithAppName(a.Name, path), Path: path, IsDefault: isDefault})
	}
	if res.DefaultApp != nil {
		add(*res.DefaultApp, true)
	}
	start := len(apps)
	for _, a := range res.Apps {
		add(a, false)
	}
	rest := apps[start:]
	sort.SliceStable(rest, func(i, j int) bool {
		ni, nj := strings.ToLower(rest[i].Name), strings.ToLower(rest[j].Name)
		if ni != nj {
			return ni < nj
		}
		return rest[i].Path < rest[j].Path
	})
	if apps == nil {
		apps = []OpenWithApp{}
	}
	return apps, nil
}

// openWithAppName is the label for an application: its display name without
// a trailing ".app" (shown when Finder is set to show all extensions), or the
// bundle's file name when the system gave none.
func openWithAppName(displayName, path string) string {
	name := strings.TrimSpace(displayName)
	if name == "" {
		name = posixpath.Base(path)
	}
	if strings.HasSuffix(strings.ToLower(name), ".app") {
		name = name[:len(name)-len(".app")]
	}
	return name
}
