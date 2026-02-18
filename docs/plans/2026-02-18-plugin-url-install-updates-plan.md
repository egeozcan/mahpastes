# Plugin URL Install & Update System — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Enable plugin installation from URLs, permission review on all installs, periodic update checking, and manual update with permission re-review.

**Architecture:** Two-phase install (preview manifest → user approves → confirm install). Background UpdateChecker goroutine fetches source URLs on a timer, compares semver, emits Wails events. Frontend shows review modal for installs and permission-changing updates.

**Tech Stack:** Go (Wails v2, net/http, SQLite), Vanilla JS (Wails runtime events), Playwright e2e tests.

---

### Task 1: Database Migrations — source_url column and app_settings table

**Files:**
- Modify: `database.go:183` (after plugin_storage table creation)

**Step 1: Add the migration code**

After the plugin_storage `CREATE TABLE` block (line 183), add:

```go
	// Migrate: Add source_url column to plugins if it doesn't exist
	_, _ = db.Exec("ALTER TABLE plugins ADD COLUMN source_url TEXT DEFAULT ''")

	// Create app_settings table
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS app_settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`); err != nil {
		log.Printf("Warning: Failed to create app_settings table: %v", err)
	}
```

**Step 2: Run the app to verify migration**

Run: `cd /Users/egecan/Code/mahpastes && ~/go/bin/wails build`
Expected: Builds successfully. SQLite ALTER TABLE is idempotent (silently fails if column already exists).

**Step 3: Commit**

```bash
git add database.go
git commit -m "feat: add source_url column and app_settings table migration"
```

---

### Task 2: Semver comparison utility

**Files:**
- Create: `plugin/semver.go`
- Create: `plugin/semver_test.go`

**Step 1: Write the failing test**

```go
package plugin

import "testing"

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		a, b string
		want int // -1, 0, 1
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "1.0.1", -1},
		{"1.0.1", "1.0.0", 1},
		{"1.1.0", "1.0.9", 1},
		{"2.0.0", "1.99.99", 1},
		{"0.1.0", "0.0.9", 1},
		{"1.0", "1.0.0", 0},     // Missing patch = 0
		{"1", "1.0.0", 0},       // Missing minor+patch = 0
		{"bad", "1.0.0", 0},     // Unparseable = 0
		{"1.0.0", "bad", 0},     // Unparseable = 0
	}
	for _, tt := range tests {
		got := CompareSemver(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("CompareSemver(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestIsNewerVersion(t *testing.T) {
	if !IsNewerVersion("1.0.0", "1.0.1") {
		t.Error("1.0.1 should be newer than 1.0.0")
	}
	if IsNewerVersion("1.0.1", "1.0.0") {
		t.Error("1.0.0 should not be newer than 1.0.1")
	}
	if IsNewerVersion("1.0.0", "1.0.0") {
		t.Error("same version should not be newer")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/egecan/Code/mahpastes && go test ./plugin/ -run TestCompareSemver -v`
Expected: FAIL — `CompareSemver` not defined

**Step 3: Write minimal implementation**

```go
package plugin

import (
	"strconv"
	"strings"
)

// CompareSemver compares two semver strings.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
// Handles missing parts (e.g., "1.0" treated as "1.0.0").
// Returns 0 for unparseable versions.
func CompareSemver(a, b string) int {
	ap := parseSemverParts(a)
	bp := parseSemverParts(b)
	if ap == nil || bp == nil {
		return 0
	}
	for i := 0; i < 3; i++ {
		if ap[i] < bp[i] {
			return -1
		}
		if ap[i] > bp[i] {
			return 1
		}
	}
	return 0
}

// IsNewerVersion returns true if remote is a newer version than current.
func IsNewerVersion(current, remote string) bool {
	return CompareSemver(current, remote) == -1
}

func parseSemverParts(v string) []int {
	// Strip leading 'v' if present
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	result := make([]int, 3)
	for i, p := range parts {
		if i >= 3 {
			break
		}
		// Strip anything after a hyphen (pre-release) or plus (build metadata)
		p = strings.SplitN(p, "-", 2)[0]
		p = strings.SplitN(p, "+", 2)[0]
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		result[i] = n
	}
	return result
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/egecan/Code/mahpastes && go test ./plugin/ -run "TestCompareSemver|TestIsNewerVersion" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add plugin/semver.go plugin/semver_test.go
git commit -m "feat: add semver comparison utility"
```

---

### Task 3: Permission diff utility

**Files:**
- Create: `plugin/permission_diff.go`
- Create: `plugin/permission_diff_test.go`

**Step 1: Write the failing test**

```go
package plugin

import "testing"

func TestManifestsHavePermissionChanges(t *testing.T) {
	base := &Manifest{
		Network:    map[string][]string{"api.example.com": {"GET"}},
		Filesystem: FilesystemPerms{Read: true, Write: false},
		Clipboard:  false,
		Events:     []string{"clip:created"},
	}

	// Identical = no changes
	same := &Manifest{
		Network:    map[string][]string{"api.example.com": {"GET"}},
		Filesystem: FilesystemPerms{Read: true, Write: false},
		Clipboard:  false,
		Events:     []string{"clip:created"},
	}
	if ManifestsHavePermissionChanges(base, same) {
		t.Error("identical manifests should have no permission changes")
	}

	// New network domain
	newDomain := &Manifest{
		Network:    map[string][]string{"api.example.com": {"GET"}, "evil.com": {"POST"}},
		Filesystem: FilesystemPerms{Read: true, Write: false},
		Clipboard:  false,
		Events:     []string{"clip:created"},
	}
	if !ManifestsHavePermissionChanges(base, newDomain) {
		t.Error("added network domain should be a permission change")
	}

	// New HTTP method on existing domain
	newMethod := &Manifest{
		Network:    map[string][]string{"api.example.com": {"GET", "POST"}},
		Filesystem: FilesystemPerms{Read: true, Write: false},
		Clipboard:  false,
		Events:     []string{"clip:created"},
	}
	if !ManifestsHavePermissionChanges(base, newMethod) {
		t.Error("added HTTP method should be a permission change")
	}

	// Filesystem write added
	newFS := &Manifest{
		Network:    map[string][]string{"api.example.com": {"GET"}},
		Filesystem: FilesystemPerms{Read: true, Write: true},
		Clipboard:  false,
		Events:     []string{"clip:created"},
	}
	if !ManifestsHavePermissionChanges(base, newFS) {
		t.Error("added filesystem write should be a permission change")
	}

	// Clipboard added
	newClip := &Manifest{
		Network:    map[string][]string{"api.example.com": {"GET"}},
		Filesystem: FilesystemPerms{Read: true, Write: false},
		Clipboard:  true,
		Events:     []string{"clip:created"},
	}
	if !ManifestsHavePermissionChanges(base, newClip) {
		t.Error("added clipboard should be a permission change")
	}

	// New event
	newEvent := &Manifest{
		Network:    map[string][]string{"api.example.com": {"GET"}},
		Filesystem: FilesystemPerms{Read: true, Write: false},
		Clipboard:  false,
		Events:     []string{"clip:created", "clip:deleted"},
	}
	if !ManifestsHavePermissionChanges(base, newEvent) {
		t.Error("added event should be a permission change")
	}

	// Removed domain = also a change (permissions are different)
	removedDomain := &Manifest{
		Network:    map[string][]string{},
		Filesystem: FilesystemPerms{Read: true, Write: false},
		Clipboard:  false,
		Events:     []string{"clip:created"},
	}
	if !ManifestsHavePermissionChanges(base, removedDomain) {
		t.Error("removed network domain should be a permission change")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/egecan/Code/mahpastes && go test ./plugin/ -run TestManifestsHavePermissionChanges -v`
Expected: FAIL — `ManifestsHavePermissionChanges` not defined

**Step 3: Write minimal implementation**

```go
package plugin

import "sort"

// ManifestsHavePermissionChanges returns true if the two manifests differ
// in any permission-related field (network, filesystem, clipboard, events).
func ManifestsHavePermissionChanges(current, remote *Manifest) bool {
	// Compare network domains
	if !networkEqual(current.Network, remote.Network) {
		return true
	}

	// Compare filesystem
	if current.Filesystem != remote.Filesystem {
		return true
	}

	// Compare clipboard
	if current.Clipboard != remote.Clipboard {
		return true
	}

	// Compare events
	if !stringSliceEqual(current.Events, remote.Events) {
		return true
	}

	return false
}

func networkEqual(a, b map[string][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for domain, methodsA := range a {
		methodsB, ok := b[domain]
		if !ok {
			return false
		}
		if !stringSliceEqual(methodsA, methodsB) {
			return false
		}
	}
	return true
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aSorted := make([]string, len(a))
	bSorted := make([]string, len(b))
	copy(aSorted, a)
	copy(bSorted, b)
	sort.Strings(aSorted)
	sort.Strings(bSorted)
	for i := range aSorted {
		if aSorted[i] != bSorted[i] {
			return false
		}
	}
	return true
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/egecan/Code/mahpastes && go test ./plugin/ -run TestManifestsHavePermissionChanges -v`
Expected: PASS

**Step 5: Commit**

```bash
git add plugin/permission_diff.go plugin/permission_diff_test.go
git commit -m "feat: add manifest permission diff utility"
```

---

### Task 4: URL fetch utility

**Files:**
- Create: `plugin/fetch.go`
- Create: `plugin/fetch_test.go`

**Step 1: Write the failing test**

```go
package plugin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchPluginSource(t *testing.T) {
	pluginSrc := `Plugin = { name = "Test", version = "1.0.0", description = "A test plugin", author = "Tester" }`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(pluginSrc))
	}))
	defer server.Close()

	source, err := FetchPluginSource(server.URL + "/test.lua")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if source != pluginSrc {
		t.Errorf("got %q, want %q", source, pluginSrc)
	}
}

func TestFetchPluginSource_TooLarge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Write 2MB of data
		w.Write([]byte(strings.Repeat("x", 2*1024*1024)))
	}))
	defer server.Close()

	_, err := FetchPluginSource(server.URL + "/big.lua")
	if err == nil {
		t.Error("expected error for oversized response")
	}
}

func TestFetchPluginSource_InvalidURL(t *testing.T) {
	_, err := FetchPluginSource("ftp://not-http.com/file.lua")
	if err == nil {
		t.Error("expected error for non-HTTP URL")
	}
}

func TestFetchPluginSource_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, err := FetchPluginSource(server.URL + "/missing.lua")
	if err == nil {
		t.Error("expected error for 404 response")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/egecan/Code/mahpastes && go test ./plugin/ -run TestFetchPluginSource -v`
Expected: FAIL — `FetchPluginSource` not defined

**Step 3: Write minimal implementation**

```go
package plugin

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	maxPluginSize  = 1 << 20 // 1MB
	fetchTimeout   = 30 * time.Second
)

// FetchPluginSource downloads a plugin .lua file from a URL.
// Validates URL scheme (HTTP/HTTPS only) and enforces a 1MB size limit.
func FetchPluginSource(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return "", fmt.Errorf("unsupported URL scheme %q: only http and https are allowed", parsed.Scheme)
	}

	client := &http.Client{Timeout: fetchTimeout}
	resp, err := client.Get(rawURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch plugin: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server returned HTTP %d", resp.StatusCode)
	}

	// Read with size limit
	limited := io.LimitReader(resp.Body, maxPluginSize+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}
	if len(body) > maxPluginSize {
		return "", fmt.Errorf("plugin source exceeds maximum size of %d bytes", maxPluginSize)
	}

	return string(body), nil
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/egecan/Code/mahpastes && go test ./plugin/ -run TestFetchPluginSource -v`
Expected: PASS

**Step 5: Commit**

```bash
git add plugin/fetch.go plugin/fetch_test.go
git commit -m "feat: add URL fetch utility for plugin sources"
```

---

### Task 5: PluginPreview type and preview methods on Manager

**Files:**
- Modify: `plugin/manager.go` (add `PluginPreview` type, `PreviewFromSource`, `PreviewFromURL` methods)

**Step 1: Write the failing test**

Add to a new test file `plugin/preview_test.go`:

```go
package plugin

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const testPluginSource = `
Plugin = {
	name = "Preview Test",
	version = "2.0.0",
	description = "A test plugin for preview",
	author = "Test Author",
	network = {
		["api.example.com"] = {"GET", "POST"},
	},
	clipboard = true,
	events = {"clip:created", "app:startup"},
}

function on_clip_created(data) end
function on_startup() end
`

func TestPreviewFromSource(t *testing.T) {
	preview, err := PreviewFromSource(testPluginSource, "/path/to/test.lua")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if preview.Name != "Preview Test" {
		t.Errorf("Name = %q, want %q", preview.Name, "Preview Test")
	}
	if preview.Version != "2.0.0" {
		t.Errorf("Version = %q, want %q", preview.Version, "2.0.0")
	}
	if preview.Author != "Test Author" {
		t.Errorf("Author = %q, want %q", preview.Author, "Test Author")
	}
	if !preview.Clipboard {
		t.Error("Clipboard should be true")
	}
	if len(preview.Network) != 1 {
		t.Errorf("Network should have 1 domain, got %d", len(preview.Network))
	}
	if preview.Source != "/path/to/test.lua" {
		t.Errorf("Source = %q, want %q", preview.Source, "/path/to/test.lua")
	}
}

func TestPreviewFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-plugin.lua")
	os.WriteFile(path, []byte(testPluginSource), 0644)

	preview, err := PreviewFromFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if preview.Name != "Preview Test" {
		t.Errorf("Name = %q, want %q", preview.Name, "Preview Test")
	}
	if preview.Source != path {
		t.Errorf("Source = %q, want %q", preview.Source, path)
	}
}

func TestPreviewFromURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(testPluginSource))
	}))
	defer server.Close()

	preview, err := PreviewFromURL(server.URL + "/test.lua")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if preview.Name != "Preview Test" {
		t.Errorf("Name = %q, want %q", preview.Name, "Preview Test")
	}
	if preview.Source != server.URL+"/test.lua" {
		t.Errorf("Source = %q, want %q", preview.Source, server.URL+"/test.lua")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/egecan/Code/mahpastes && go test ./plugin/ -run "TestPreviewFrom" -v`
Expected: FAIL — `PluginPreview`, `PreviewFromSource` not defined

**Step 3: Write minimal implementation**

Add to `plugin/manager.go` (after the `Plugin` struct, before `NewManager`):

```go
// PluginPreview represents a parsed plugin manifest for review before install/update.
type PluginPreview struct {
	Name        string              `json:"name"`
	Version     string              `json:"version"`
	Description string              `json:"description"`
	Author      string              `json:"author"`
	Network     map[string][]string `json:"network"`
	Filesystem  FilesystemPerms     `json:"filesystem"`
	Clipboard   bool                `json:"clipboard"`
	Events      []string            `json:"events"`
	Source      string              `json:"source"`
}

// PreviewFromSource parses a plugin source string and returns a preview.
func PreviewFromSource(source, sourcePath string) (*PluginPreview, error) {
	manifest, err := ParseManifest(source)
	if err != nil {
		return nil, fmt.Errorf("invalid plugin: %w", err)
	}
	return &PluginPreview{
		Name:        manifest.Name,
		Version:     manifest.Version,
		Description: manifest.Description,
		Author:      manifest.Author,
		Network:     manifest.Network,
		Filesystem:  manifest.Filesystem,
		Clipboard:   manifest.Clipboard,
		Events:      manifest.Events,
		Source:      sourcePath,
	}, nil
}

// PreviewFromFile reads a plugin file and returns a preview.
func PreviewFromFile(path string) (*PluginPreview, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read plugin file: %w", err)
	}
	return PreviewFromSource(string(source), path)
}

// PreviewFromURL fetches a plugin from a URL and returns a preview.
func PreviewFromURL(rawURL string) (*PluginPreview, error) {
	source, err := FetchPluginSource(rawURL)
	if err != nil {
		return nil, err
	}
	return PreviewFromSource(source, rawURL)
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/egecan/Code/mahpastes && go test ./plugin/ -run "TestPreviewFrom" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add plugin/manager.go plugin/preview_test.go
git commit -m "feat: add PluginPreview type and preview functions"
```

---

### Task 6: ImportPluginFromURL on Manager + ConfirmInstall refactor

**Files:**
- Modify: `plugin/manager.go` — add `ImportPluginFromURL` method, refactor `ImportPlugin` to accept source_url param

The existing `ImportPlugin(sourcePath string)` needs a counterpart for URLs. We also need the DB insert to include `source_url`.

**Step 1: Update the ImportPlugin DB insert to include source_url**

In `plugin/manager.go`, the INSERT at line 345-354 currently is:

```go
_, err = m.db.Exec(`
    INSERT INTO plugins (filename, name, version, enabled, status)
    VALUES (?, ?, ?, 1, 'enabled')
    ON CONFLICT(filename) DO UPDATE SET
        name = excluded.name,
        version = excluded.version,
        enabled = 1,
        status = 'enabled',
        error_count = 0
`, filename, manifest.Name, manifest.Version)
```

Change to:

```go
_, err = m.db.Exec(`
    INSERT INTO plugins (filename, name, version, enabled, status, source_url)
    VALUES (?, ?, ?, 1, 'enabled', ?)
    ON CONFLICT(filename) DO UPDATE SET
        name = excluded.name,
        version = excluded.version,
        enabled = 1,
        status = 'enabled',
        error_count = 0,
        source_url = excluded.source_url
`, filename, manifest.Name, manifest.Version, "")
```

**Step 2: Add ImportPluginFromURL method**

Add after `ImportPlugin`:

```go
// ImportPluginFromURL imports a plugin from a URL.
// The URL is stored as source_url for future update checks.
func (m *Manager) ImportPluginFromURL(rawURL string) (*Plugin, error) {
	source, err := FetchPluginSource(rawURL)
	if err != nil {
		return nil, err
	}

	manifest, err := ParseManifest(source)
	if err != nil {
		return nil, fmt.Errorf("invalid plugin: %w", err)
	}

	// Derive filename from URL path, falling back to manifest name
	filename := filenameFromURL(rawURL, manifest.Name)
	destPath := filepath.Join(m.pluginsDir, filename)

	if err := os.WriteFile(destPath, []byte(source), 0644); err != nil {
		return nil, fmt.Errorf("failed to write plugin: %w", err)
	}

	_, err = m.db.Exec(`
		INSERT INTO plugins (filename, name, version, enabled, status, source_url)
		VALUES (?, ?, ?, 1, 'enabled', ?)
		ON CONFLICT(filename) DO UPDATE SET
			name = excluded.name,
			version = excluded.version,
			enabled = 1,
			status = 'enabled',
			error_count = 0,
			source_url = excluded.source_url
	`, filename, manifest.Name, manifest.Version, rawURL)
	if err != nil {
		return nil, fmt.Errorf("failed to register plugin: %w", err)
	}

	var id int64
	err = m.db.QueryRow("SELECT id FROM plugins WHERE filename = ?", filename).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("failed to get plugin ID: %w", err)
	}

	p := &Plugin{
		ID:       id,
		Filename: filename,
		Name:     manifest.Name,
		Version:  manifest.Version,
		Enabled:  true,
		Status:   "enabled",
	}

	if err := m.loadPlugin(p); err != nil {
		return nil, fmt.Errorf("failed to load plugin: %w", err)
	}

	return p, nil
}

func filenameFromURL(rawURL, fallbackName string) string {
	parsed, err := url.Parse(rawURL)
	if err == nil {
		base := filepath.Base(parsed.Path)
		if strings.HasSuffix(base, ".lua") {
			return base
		}
	}
	// Sanitize fallback name
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, fallbackName)
	return strings.ToLower(safe) + ".lua"
}
```

You'll need to add `"net/url"` to the imports if not already present.

**Step 3: Verify build compiles**

Run: `cd /Users/egecan/Code/mahpastes && go build ./...`
Expected: PASS

**Step 4: Commit**

```bash
git add plugin/manager.go
git commit -m "feat: add ImportPluginFromURL and source_url tracking"
```

---

### Task 7: PluginService preview and confirm methods

**Files:**
- Modify: `plugin_service.go` — add `PreviewPluginFromURL`, `PreviewPluginFromPath`, `ConfirmPluginInstall`, update `ImportPlugin` to return preview

**Step 1: Add PluginPreview re-export and new methods**

Add these types and methods to `plugin_service.go`:

```go
// PluginPreview is an alias for plugin.PluginPreview
type PluginPreview = plugin.PluginPreview

// PreviewPluginFromURL fetches a plugin URL and returns a preview for review
func (s *PluginService) PreviewPluginFromURL(rawURL string) (*PluginPreview, error) {
	return plugin.PreviewFromURL(rawURL)
}

// PreviewPluginFromPath reads a plugin file and returns a preview for review
func (s *PluginService) PreviewPluginFromPath(path string) (*PluginPreview, error) {
	return plugin.PreviewFromFile(path)
}

// ConfirmPluginInstall installs a plugin after user has reviewed permissions.
// Source can be a URL (http:// or https://) or a local file path.
func (s *PluginService) ConfirmPluginInstall(source string) (*PluginInfo, error) {
	if s.app.pluginManager == nil {
		return nil, fmt.Errorf("plugin manager not initialized")
	}

	var p *plugin.Plugin
	var err error

	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		p, err = s.app.pluginManager.ImportPluginFromURL(source)
	} else {
		p, err = s.app.pluginManager.ImportPlugin(source)
	}
	if err != nil {
		return nil, err
	}

	return pluginToInfo(p), nil
}
```

Add `"strings"` to the imports.

**Step 2: Update ImportPlugin to return PluginPreview (two-phase flow)**

Replace the existing `ImportPlugin()` method (lines 107-132):

```go
// ImportPlugin opens a file dialog and returns a preview for review.
// The frontend should call ConfirmPluginInstall(path) after user approves.
func (s *PluginService) ImportPlugin() (*PluginPreview, error) {
	// Open file dialog
	path, err := runtime.OpenFileDialog(s.app.ctx, runtime.OpenDialogOptions{
		Title: "Select Plugin File",
		Filters: []runtime.FileFilter{
			{DisplayName: "Lua Scripts", Pattern: "*.lua"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open file dialog: %w", err)
	}
	if path == "" {
		return nil, nil // User cancelled
	}

	return plugin.PreviewFromFile(path)
}
```

**Step 3: Keep ImportPluginFromPath as backwards compat alias**

The existing `ImportPluginFromPath` (line 262) stays unchanged — it calls `ImportPlugin` on the manager directly, which is what e2e tests use. No changes needed.

**Step 4: Regenerate Wails bindings**

Run: `cd /Users/egecan/Code/mahpastes && make bindings`
Expected: New methods appear in `frontend/wailsjs/go/main/PluginService.js`

**Step 5: Verify build**

Run: `cd /Users/egecan/Code/mahpastes && go build ./...`
Expected: PASS

**Step 6: Commit**

```bash
git add plugin_service.go frontend/wailsjs/
git commit -m "feat: add two-phase plugin install (preview + confirm)"
```

---

### Task 8: Update checker goroutine

**Files:**
- Create: `plugin/update_checker.go`
- Create: `plugin/update_checker_test.go`

**Step 1: Write the failing test**

```go
package plugin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUpdateChecker_DetectsNewerVersion(t *testing.T) {
	remoteSrc := `Plugin = { name = "Test", version = "2.0.0", description = "Updated", author = "A" }`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(remoteSrc))
	}))
	defer server.Close()

	current := &Manifest{
		Name: "Test", Version: "1.0.0", Description: "Old", Author: "A",
	}

	info, err := CheckPluginUpdate(server.URL+"/test.lua", "1.0.0", current)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected update info, got nil")
	}
	if info.NewVersion != "2.0.0" {
		t.Errorf("NewVersion = %q, want %q", info.NewVersion, "2.0.0")
	}
	if info.HasPermissionChanges {
		t.Error("should not have permission changes (same permissions)")
	}
}

func TestUpdateChecker_NoUpdateWhenSameVersion(t *testing.T) {
	remoteSrc := `Plugin = { name = "Test", version = "1.0.0", description = "Same", author = "A" }`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(remoteSrc))
	}))
	defer server.Close()

	current := &Manifest{Name: "Test", Version: "1.0.0"}

	info, err := CheckPluginUpdate(server.URL+"/test.lua", "1.0.0", current)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info != nil {
		t.Error("expected nil (no update), got info")
	}
}

func TestUpdateChecker_DetectsPermissionChanges(t *testing.T) {
	remoteSrc := `Plugin = {
		name = "Test", version = "2.0.0", description = "Updated", author = "A",
		network = { ["evil.com"] = {"POST"} },
	}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(remoteSrc))
	}))
	defer server.Close()

	current := &Manifest{Name: "Test", Version: "1.0.0"}

	info, err := CheckPluginUpdate(server.URL+"/test.lua", "1.0.0", current)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected update info")
	}
	if !info.HasPermissionChanges {
		t.Error("should detect permission changes (new network domain)")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/egecan/Code/mahpastes && go test ./plugin/ -run TestUpdateChecker -v`
Expected: FAIL — `CheckPluginUpdate` not defined

**Step 3: Write minimal implementation**

```go
package plugin

import (
	"context"
	"database/sql"
	"log"
	"sync"
	"time"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// PluginUpdateInfo represents an available update for a plugin.
type PluginUpdateInfo struct {
	PluginID             int64  `json:"plugin_id"`
	CurrentVersion       string `json:"current_version"`
	NewVersion           string `json:"new_version"`
	HasPermissionChanges bool   `json:"has_permission_changes"`
}

// CheckPluginUpdate fetches the latest version from a URL and compares it
// against the current version. Returns nil if no update is available.
func CheckPluginUpdate(sourceURL, currentVersion string, currentManifest *Manifest) (*PluginUpdateInfo, error) {
	source, err := FetchPluginSource(sourceURL)
	if err != nil {
		return nil, err
	}

	remoteManifest, err := ParseManifest(source)
	if err != nil {
		return nil, err
	}

	if !IsNewerVersion(currentVersion, remoteManifest.Version) {
		return nil, nil
	}

	hasChanges := ManifestsHavePermissionChanges(currentManifest, remoteManifest)

	return &PluginUpdateInfo{
		CurrentVersion:       currentVersion,
		NewVersion:           remoteManifest.Version,
		HasPermissionChanges: hasChanges,
	}, nil
}

// UpdateChecker periodically checks for plugin updates.
type UpdateChecker struct {
	ctx     context.Context
	db      *sql.DB
	manager *Manager
	mu      sync.Mutex
	cancel  context.CancelFunc
	updates map[int64]*PluginUpdateInfo
}

// NewUpdateChecker creates a new update checker.
func NewUpdateChecker(ctx context.Context, db *sql.DB, manager *Manager) *UpdateChecker {
	return &UpdateChecker{
		ctx:     ctx,
		db:      db,
		manager: manager,
		updates: make(map[int64]*PluginUpdateInfo),
	}
}

// Start begins periodic update checking with the given interval.
// Pass 0 for one-shot check (startup only).
func (uc *UpdateChecker) Start(interval time.Duration) {
	uc.Stop() // Stop any existing timer

	ctx, cancel := context.WithCancel(uc.ctx)
	uc.mu.Lock()
	uc.cancel = cancel
	uc.mu.Unlock()

	go func() {
		// Initial check after a short delay to let app finish starting
		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			return
		}

		uc.checkAll()

		if interval <= 0 {
			return // One-shot mode
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				uc.checkAll()
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Stop cancels the periodic checker.
func (uc *UpdateChecker) Stop() {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	if uc.cancel != nil {
		uc.cancel()
		uc.cancel = nil
	}
}

// GetUpdates returns all currently known available updates.
func (uc *UpdateChecker) GetUpdates() []*PluginUpdateInfo {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	result := make([]*PluginUpdateInfo, 0, len(uc.updates))
	for _, info := range uc.updates {
		result = append(result, info)
	}
	return result
}

// ClearUpdate removes an update entry (after it's been applied).
func (uc *UpdateChecker) ClearUpdate(pluginID int64) {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	delete(uc.updates, pluginID)
}

func (uc *UpdateChecker) checkAll() {
	rows, err := uc.db.Query(`
		SELECT id, version, source_url FROM plugins
		WHERE source_url != '' AND enabled = 1
	`)
	if err != nil {
		log.Printf("UpdateChecker: failed to query plugins: %v", err)
		return
	}
	defer rows.Close()

	type pluginRow struct {
		id        int64
		version   string
		sourceURL string
	}
	var plugins []pluginRow
	for rows.Next() {
		var p pluginRow
		if err := rows.Scan(&p.id, &p.version, &p.sourceURL); err != nil {
			continue
		}
		plugins = append(plugins, p)
	}

	for _, p := range plugins {
		uc.manager.mu.RLock()
		loaded, ok := uc.manager.plugins[p.id]
		uc.manager.mu.RUnlock()

		var currentManifest *Manifest
		if ok && loaded.Manifest != nil {
			currentManifest = loaded.Manifest
		} else {
			currentManifest = &Manifest{Version: p.version}
		}

		info, err := CheckPluginUpdate(p.sourceURL, p.version, currentManifest)
		if err != nil {
			log.Printf("UpdateChecker: failed to check %d: %v", p.id, err)
			continue
		}

		if info != nil {
			info.PluginID = p.id
			uc.mu.Lock()
			uc.updates[p.id] = info
			uc.mu.Unlock()

			wailsRuntime.EventsEmit(uc.ctx, "plugin:update_available", info)
			log.Printf("UpdateChecker: update available for plugin %d: %s -> %s", p.id, info.CurrentVersion, info.NewVersion)
		}
	}
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/egecan/Code/mahpastes && go test ./plugin/ -run TestUpdateChecker -v`
Expected: PASS

**Step 5: Commit**

```bash
git add plugin/update_checker.go plugin/update_checker_test.go
git commit -m "feat: add plugin update checker with periodic background checking"
```

---

### Task 9: Wire UpdateChecker into Manager and app startup

**Files:**
- Modify: `plugin/manager.go` — add `UpdateChecker` field, start it after `LoadPlugins`
- Modify: `app.go` — start update checker after plugin loading
- Modify: `plugin_service.go` — add settings methods and update/confirm methods

**Step 1: Add UpdateChecker to Manager struct**

In `plugin/manager.go`, add field to Manager struct (line ~75):

```go
updateChecker *UpdateChecker
```

Add method to Manager:

```go
// GetUpdateChecker returns the update checker instance.
func (m *Manager) GetUpdateChecker() *UpdateChecker {
	return m.updateChecker
}

// SetUpdateChecker sets the update checker.
func (m *Manager) SetUpdateChecker(uc *UpdateChecker) {
	m.updateChecker = uc
}
```

In the `Shutdown` method (line ~499), add before closing sandboxes:

```go
	// Stop update checker
	if m.updateChecker != nil {
		m.updateChecker.Stop()
	}
```

**Step 2: Start update checker in app.go**

In `app.go` after `pm.EmitEvent("app:startup", nil)` (line 163), add:

```go
		// Start plugin update checker
		uc := plugin.NewUpdateChecker(a.ctx, a.db, pm)
		pm.SetUpdateChecker(uc)
		interval := a.getUpdateCheckInterval()
		if interval != "disabled" {
			uc.Start(parseUpdateInterval(interval))
		}
```

Add helper functions to `app.go`:

```go
func (a *App) getUpdateCheckInterval() string {
	var value string
	err := a.db.QueryRow("SELECT value FROM app_settings WHERE key = 'plugin_update_interval'").Scan(&value)
	if err != nil {
		return "24h" // default
	}
	return value
}

func parseUpdateInterval(interval string) time.Duration {
	switch interval {
	case "startup":
		return 0 // one-shot
	case "6h":
		return 6 * time.Hour
	case "24h":
		return 24 * time.Hour
	default:
		return 24 * time.Hour
	}
}
```

Add `"time"` to `app.go` imports if needed.

**Step 3: Add update/settings methods to PluginService**

Add to `plugin_service.go`:

```go
// GetUpdateCheckInterval returns the current plugin update check interval setting.
func (s *PluginService) GetUpdateCheckInterval() (string, error) {
	if s.app.db == nil {
		return "24h", nil
	}
	var value string
	err := s.app.db.QueryRow("SELECT value FROM app_settings WHERE key = 'plugin_update_interval'").Scan(&value)
	if err != nil {
		return "24h", nil // default
	}
	return value, nil
}

// SetUpdateCheckInterval updates the plugin update check interval and restarts the checker.
func (s *PluginService) SetUpdateCheckInterval(interval string) error {
	valid := map[string]bool{"startup": true, "6h": true, "24h": true, "disabled": true}
	if !valid[interval] {
		return fmt.Errorf("invalid interval %q: must be startup, 6h, 24h, or disabled", interval)
	}

	if s.app.db == nil {
		return fmt.Errorf("database not initialized")
	}

	_, err := s.app.db.Exec(`
		INSERT INTO app_settings (key, value) VALUES ('plugin_update_interval', ?)
		ON CONFLICT(key) DO UPDATE SET value = ?
	`, interval, interval)
	if err != nil {
		return err
	}

	// Restart update checker with new interval
	if s.app.pluginManager != nil {
		uc := s.app.pluginManager.GetUpdateChecker()
		if uc != nil {
			if interval == "disabled" {
				uc.Stop()
			} else {
				uc.Start(parseUpdateInterval(interval))
			}
		}
	}

	return nil
}

// CheckForUpdates triggers an immediate update check and returns results.
func (s *PluginService) CheckForUpdates() ([]*plugin.PluginUpdateInfo, error) {
	if s.app.pluginManager == nil {
		return []*plugin.PluginUpdateInfo{}, nil
	}
	uc := s.app.pluginManager.GetUpdateChecker()
	if uc == nil {
		return []*plugin.PluginUpdateInfo{}, nil
	}
	// Trigger check synchronously by calling checkAll, then return results
	uc.checkAll()
	return uc.GetUpdates(), nil
}

// UpdatePlugin attempts to update a plugin from its source URL.
type UpdateResult struct {
	Success     bool           `json:"success"`
	NeedsReview bool           `json:"needs_review"`
	Preview     *PluginPreview `json:"preview,omitempty"`
	PluginInfo  *PluginInfo    `json:"plugin_info,omitempty"`
	Error       string         `json:"error,omitempty"`
}

// UpdatePlugin fetches the latest version from the source URL, compares permissions,
// and either applies the update or returns a preview for permission re-review.
func (s *PluginService) UpdatePlugin(pluginID int64) (*UpdateResult, error) {
	if s.app.pluginManager == nil {
		return &UpdateResult{Error: "plugin manager not initialized"}, nil
	}

	// Get plugin source URL from DB
	var sourceURL, currentVersion string
	err := s.app.db.QueryRow("SELECT source_url, version FROM plugins WHERE id = ?", pluginID).Scan(&sourceURL, &currentVersion)
	if err != nil {
		return &UpdateResult{Error: "plugin not found"}, nil
	}
	if sourceURL == "" {
		return &UpdateResult{Error: "plugin was not installed from a URL"}, nil
	}

	// Fetch latest source
	source, err := plugin.FetchPluginSource(sourceURL)
	if err != nil {
		return &UpdateResult{Error: fmt.Sprintf("failed to fetch update: %v", err)}, nil
	}

	remoteManifest, err := plugin.ParseManifest(source)
	if err != nil {
		return &UpdateResult{Error: fmt.Sprintf("invalid remote plugin: %v", err)}, nil
	}

	// Get current manifest for permission comparison
	s.app.pluginManager.mu.RLock()
	loaded, ok := s.app.pluginManager.plugins[pluginID]
	s.app.pluginManager.mu.RUnlock()

	var currentManifest *plugin.Manifest
	if ok && loaded.Manifest != nil {
		currentManifest = loaded.Manifest
	} else {
		currentManifest = &plugin.Manifest{}
	}

	hasChanges := plugin.ManifestsHavePermissionChanges(currentManifest, remoteManifest)

	if hasChanges {
		// Return preview for re-review — don't apply yet
		preview := &PluginPreview{
			Name:        remoteManifest.Name,
			Version:     remoteManifest.Version,
			Description: remoteManifest.Description,
			Author:      remoteManifest.Author,
			Network:     remoteManifest.Network,
			Filesystem:  remoteManifest.Filesystem,
			Clipboard:   remoteManifest.Clipboard,
			Events:      remoteManifest.Events,
			Source:      sourceURL,
		}
		// Store the pending source for ConfirmPluginUpdate
		s.app.pluginManager.StorePendingUpdate(pluginID, source)
		return &UpdateResult{NeedsReview: true, Preview: preview}, nil
	}

	// No permission changes — apply update directly
	info, err := s.applyPluginUpdate(pluginID, source, remoteManifest, sourceURL)
	if err != nil {
		return &UpdateResult{Error: err.Error()}, nil
	}

	// Clear update from checker
	if uc := s.app.pluginManager.GetUpdateChecker(); uc != nil {
		uc.ClearUpdate(pluginID)
	}

	return &UpdateResult{Success: true, PluginInfo: info}, nil
}

// ConfirmPluginUpdate applies a pending update after the user approves permission changes.
func (s *PluginService) ConfirmPluginUpdate(pluginID int64) (*PluginInfo, error) {
	if s.app.pluginManager == nil {
		return nil, fmt.Errorf("plugin manager not initialized")
	}

	source := s.app.pluginManager.PopPendingUpdate(pluginID)
	if source == "" {
		return nil, fmt.Errorf("no pending update for plugin %d", pluginID)
	}

	manifest, err := plugin.ParseManifest(source)
	if err != nil {
		return nil, fmt.Errorf("invalid plugin source: %w", err)
	}

	var sourceURL string
	s.app.db.QueryRow("SELECT source_url FROM plugins WHERE id = ?", pluginID).Scan(&sourceURL)

	info, err := s.applyPluginUpdate(pluginID, source, manifest, sourceURL)
	if err != nil {
		return nil, err
	}

	if uc := s.app.pluginManager.GetUpdateChecker(); uc != nil {
		uc.ClearUpdate(pluginID)
	}

	return info, nil
}

func (s *PluginService) applyPluginUpdate(pluginID int64, source string, manifest *plugin.Manifest, sourceURL string) (*PluginInfo, error) {
	// Get filename
	var filename string
	err := s.app.db.QueryRow("SELECT filename FROM plugins WHERE id = ?", pluginID).Scan(&filename)
	if err != nil {
		return nil, fmt.Errorf("plugin not found: %w", err)
	}

	// Unload current plugin
	s.app.pluginManager.UnloadPlugin(pluginID)

	// Overwrite file
	destPath := filepath.Join(s.app.pluginManager.pluginsDir, filename)
	if err := os.WriteFile(destPath, []byte(source), 0644); err != nil {
		return nil, fmt.Errorf("failed to write updated plugin: %w", err)
	}

	// Update DB
	_, err = s.app.db.Exec(`
		UPDATE plugins SET name = ?, version = ?, enabled = 1, status = 'enabled', error_count = 0, source_url = ?
		WHERE id = ?
	`, manifest.Name, manifest.Version, sourceURL, pluginID)
	if err != nil {
		return nil, fmt.Errorf("failed to update plugin record: %w", err)
	}

	// Reload plugin
	p := &plugin.Plugin{
		ID: pluginID, Filename: filename, Name: manifest.Name,
		Version: manifest.Version, Enabled: true, Status: "enabled",
	}
	if err := s.app.pluginManager.LoadPluginPublic(p); err != nil {
		return nil, fmt.Errorf("failed to reload plugin: %w", err)
	}

	return pluginToInfo(p), nil
}
```

Add `"os"` and `"path/filepath"` to `plugin_service.go` imports.

**Step 4: Add pending update storage and LoadPluginPublic to Manager**

In `plugin/manager.go`:

```go
// Add field to Manager struct:
pendingUpdates map[int64]string // pluginID -> source content

// Initialize in NewManager:
pendingUpdates: make(map[int64]string),

// Add methods:
func (m *Manager) StorePendingUpdate(pluginID int64, source string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pendingUpdates[pluginID] = source
}

func (m *Manager) PopPendingUpdate(pluginID int64) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	source := m.pendingUpdates[pluginID]
	delete(m.pendingUpdates, pluginID)
	return source
}

// LoadPluginPublic is a public wrapper around loadPlugin for use by PluginService.
func (m *Manager) LoadPluginPublic(p *Plugin) error {
	return m.loadPlugin(p)
}
```

Also make `checkAll` on UpdateChecker exported (rename to `CheckAll`) so PluginService can call it for `CheckForUpdates()`. Update references accordingly.

**Step 5: Regenerate bindings**

Run: `cd /Users/egecan/Code/mahpastes && make bindings`

**Step 6: Verify build**

Run: `cd /Users/egecan/Code/mahpastes && go build ./...`
Expected: PASS

**Step 7: Commit**

```bash
git add plugin/manager.go plugin/update_checker.go plugin_service.go app.go frontend/wailsjs/
git commit -m "feat: wire update checker into app lifecycle with settings and update methods"
```

---

### Task 10: Permission Review Modal — HTML

**Files:**
- Modify: `frontend/index.html` — add review modal markup after plugins modal (after line 848)

**Step 1: Add the modal HTML**

After the closing `</div>` of the plugins modal (line 848), add:

```html
    <!-- Plugin Review Modal -->
    <div id="plugin-review-modal" role="dialog" aria-modal="true" aria-labelledby="plugin-review-title"
        data-testid="plugin-review-modal"
        class="fixed inset-0 z-[60] flex items-center justify-center p-4 bg-stone-900/40 backdrop-blur-sm transition-opacity duration-200 opacity-0 pointer-events-none">
        <div class="bg-white rounded-lg shadow-xl max-w-lg w-full max-h-[80vh] flex flex-col overflow-hidden transform transition-transform duration-200 scale-95">
            <div class="p-5 border-b border-stone-100">
                <h2 id="plugin-review-title" class="text-sm font-semibold text-stone-800">Review Plugin</h2>
            </div>

            <div id="plugin-review-content" class="flex-1 overflow-y-auto p-5 space-y-4">
                <!-- Warning banner (updates with permission changes only) -->
                <div id="plugin-review-warning" class="hidden p-3 bg-amber-50 border border-amber-200 rounded-md">
                    <p class="text-[11px] text-amber-700 font-medium">This update requires new permissions. Review all permissions before updating.</p>
                </div>

                <!-- Plugin info -->
                <div>
                    <div class="flex items-center gap-2 mb-1">
                        <h3 id="plugin-review-name" class="text-sm font-medium text-stone-700"></h3>
                        <span id="plugin-review-version" class="text-[10px] text-stone-400 font-mono"></span>
                    </div>
                    <p id="plugin-review-author" class="text-[11px] text-stone-400"></p>
                    <p id="plugin-review-description" class="text-[11px] text-stone-500 mt-1"></p>
                </div>

                <!-- Network Access -->
                <div id="plugin-review-network-section" class="hidden">
                    <h4 class="text-[10px] font-semibold text-stone-500 uppercase tracking-wider mb-2">Network Access</h4>
                    <div id="plugin-review-network" class="space-y-1"></div>
                </div>

                <!-- Filesystem -->
                <div id="plugin-review-fs-section" class="hidden">
                    <h4 class="text-[10px] font-semibold text-stone-500 uppercase tracking-wider mb-2">Filesystem</h4>
                    <div id="plugin-review-fs" class="flex gap-2"></div>
                </div>

                <!-- Clipboard -->
                <div id="plugin-review-clipboard-section" class="hidden">
                    <h4 class="text-[10px] font-semibold text-stone-500 uppercase tracking-wider mb-2">Clipboard</h4>
                    <p class="text-[11px] text-stone-500">Can write to system clipboard</p>
                </div>

                <!-- Events -->
                <div id="plugin-review-events-section" class="hidden">
                    <h4 class="text-[10px] font-semibold text-stone-500 uppercase tracking-wider mb-2">Events</h4>
                    <div id="plugin-review-events" class="flex flex-wrap gap-1"></div>
                </div>
            </div>

            <div class="bg-stone-50 px-5 py-3 flex justify-end gap-2 border-t border-stone-100">
                <button id="plugin-review-cancel" data-testid="plugin-review-cancel"
                    class="border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-600 text-xs font-medium py-2 px-4 rounded-md transition-colors">
                    Cancel
                </button>
                <button id="plugin-review-approve" data-testid="plugin-review-approve"
                    class="bg-stone-800 hover:bg-stone-700 text-white text-xs font-medium py-2 px-4 rounded-md transition-colors">
                    Approve & Install
                </button>
            </div>
        </div>
    </div>
```

**Step 2: Add the `plugin-review.js` script tag**

In the scripts section (line ~969), add after `plugins.js`:

```html
    <script src="js/plugin-review.js"></script>
```

**Step 3: Commit**

```bash
git add frontend/index.html
git commit -m "feat: add plugin review modal HTML"
```

---

### Task 11: Permission Review Modal — JavaScript

**Files:**
- Create: `frontend/js/plugin-review.js`

**Step 1: Write the review modal JS**

```javascript
// --- Plugin Review Module ---

// State
let reviewResolve = null; // Promise resolve for review result
let reviewSource = null;  // URL or file path being reviewed

// Elements
const reviewModal = document.getElementById('plugin-review-modal');
const reviewTitle = document.getElementById('plugin-review-title');
const reviewWarning = document.getElementById('plugin-review-warning');
const reviewName = document.getElementById('plugin-review-name');
const reviewVersion = document.getElementById('plugin-review-version');
const reviewAuthor = document.getElementById('plugin-review-author');
const reviewDescription = document.getElementById('plugin-review-description');
const reviewNetworkSection = document.getElementById('plugin-review-network-section');
const reviewNetwork = document.getElementById('plugin-review-network');
const reviewFSSection = document.getElementById('plugin-review-fs-section');
const reviewFS = document.getElementById('plugin-review-fs');
const reviewClipboardSection = document.getElementById('plugin-review-clipboard-section');
const reviewEventsSection = document.getElementById('plugin-review-events-section');
const reviewEvents = document.getElementById('plugin-review-events');
const reviewCancelBtn = document.getElementById('plugin-review-cancel');
const reviewApproveBtn = document.getElementById('plugin-review-approve');

/**
 * Show the plugin review modal and return a promise that resolves to true (approved) or false (cancelled).
 * @param {object} preview - PluginPreview object from backend
 * @param {'install'|'update'} mode
 * @param {string} [currentVersion] - Current version (for update mode)
 */
function showPluginReview(preview, mode, currentVersion) {
    return new Promise((resolve) => {
        reviewResolve = resolve;
        reviewSource = preview.source;

        // Title
        reviewTitle.textContent = mode === 'update' ? 'Review Update' : 'Review Plugin';

        // Warning banner
        if (mode === 'update') {
            reviewWarning.classList.remove('hidden');
        } else {
            reviewWarning.classList.add('hidden');
        }

        // Plugin info
        reviewName.textContent = preview.name || 'Unknown';
        if (mode === 'update' && currentVersion) {
            reviewVersion.textContent = `v${currentVersion} → v${preview.version || '?'}`;
        } else {
            reviewVersion.textContent = `v${preview.version || '0.0.0'}`;
        }
        reviewAuthor.textContent = preview.author ? `by ${preview.author}` : '';
        reviewDescription.textContent = preview.description || '';

        // Network
        const network = preview.network || {};
        const domains = Object.keys(network);
        if (domains.length > 0) {
            reviewNetworkSection.classList.remove('hidden');
            reviewNetwork.innerHTML = domains.map(domain => {
                const methods = (network[domain] || []).join(', ');
                return `<div class="flex items-center gap-2 p-2 bg-white rounded border border-stone-200">
                    <span class="text-[11px] font-mono text-stone-600">${escapeHTML(domain)}</span>
                    <span class="text-[9px] text-stone-400">(${escapeHTML(methods)})</span>
                </div>`;
            }).join('');
        } else {
            reviewNetworkSection.classList.add('hidden');
        }

        // Filesystem
        if (preview.filesystem && (preview.filesystem.Read || preview.filesystem.Write)) {
            reviewFSSection.classList.remove('hidden');
            let badges = '';
            if (preview.filesystem.Read) {
                badges += '<span class="inline-flex items-center px-1.5 py-0.5 rounded text-[9px] font-medium uppercase bg-blue-100 text-blue-700">read</span>';
            }
            if (preview.filesystem.Write) {
                badges += '<span class="inline-flex items-center px-1.5 py-0.5 rounded text-[9px] font-medium uppercase bg-amber-100 text-amber-700">write</span>';
            }
            reviewFS.innerHTML = badges;
        } else {
            reviewFSSection.classList.add('hidden');
        }

        // Clipboard
        if (preview.clipboard) {
            reviewClipboardSection.classList.remove('hidden');
        } else {
            reviewClipboardSection.classList.add('hidden');
        }

        // Events
        const events = preview.events || [];
        if (events.length > 0) {
            reviewEventsSection.classList.remove('hidden');
            reviewEvents.innerHTML = events.map(event =>
                `<span class="inline-flex items-center px-1.5 py-0.5 rounded text-[9px] font-medium bg-stone-200 text-stone-600">${escapeHTML(event)}</span>`
            ).join('');
        } else {
            reviewEventsSection.classList.add('hidden');
        }

        // Button text
        reviewApproveBtn.textContent = mode === 'update' ? 'Approve & Update' : 'Approve & Install';
        reviewApproveBtn.disabled = false;

        // Show modal
        reviewModal.classList.remove('opacity-0', 'pointer-events-none');
        reviewModal.classList.add('opacity-100');
        reviewModal.querySelector(':scope > div').classList.remove('scale-95');
        reviewModal.querySelector(':scope > div').classList.add('scale-100');
    });
}

function closePluginReview(approved) {
    reviewModal.classList.add('opacity-0', 'pointer-events-none');
    reviewModal.classList.remove('opacity-100');
    reviewModal.querySelector(':scope > div').classList.add('scale-95');
    reviewModal.querySelector(':scope > div').classList.remove('scale-100');

    if (reviewResolve) {
        reviewResolve(approved);
        reviewResolve = null;
    }
    reviewSource = null;
}

// Event listeners
reviewCancelBtn.addEventListener('click', () => closePluginReview(false));
reviewApproveBtn.addEventListener('click', () => {
    reviewApproveBtn.disabled = true;
    reviewApproveBtn.textContent = 'Installing...';
    closePluginReview(true);
});
reviewModal.addEventListener('click', (e) => {
    if (e.target === reviewModal) closePluginReview(false);
});
document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && !reviewModal.classList.contains('opacity-0')) {
        e.stopImmediatePropagation();
        closePluginReview(false);
    }
});
```

**Step 2: Commit**

```bash
git add frontend/js/plugin-review.js
git commit -m "feat: add plugin review modal JavaScript"
```

---

### Task 12: Update plugins.js — two-phase import and URL install

**Files:**
- Modify: `frontend/js/plugins.js` — update `importPlugin()`, add `installFromURL()`, add update badge and button to cards

**Step 1: Add URL install UI state and update tracking**

At the top of `plugins.js`, after the existing state variables (line ~12), add:

```javascript
let pluginUpdates = {}; // pluginID -> PluginUpdateInfo
let showingURLInput = false;
```

**Step 2: Update importPlugin() to use two-phase flow**

Replace the existing `importPlugin()` function (lines 479-493):

```javascript
async function importPlugin() {
    try {
        const preview = await window.go.main.PluginService.ImportPlugin();
        if (!preview) return; // User cancelled file dialog

        const approved = await showPluginReview(preview, 'install');
        if (!approved) return;

        const result = await window.go.main.PluginService.ConfirmPluginInstall(preview.source);
        if (result) {
            showToast(`Installed: ${result.name}`);
            await loadPlugins();
            await loadPluginUIActions();
            loadClips();
        }
    } catch (error) {
        console.error('Failed to import plugin:', error);
        showToast('Failed to import plugin: ' + (error.message || 'Unknown error'));
    }
}
```

**Step 3: Add installFromURL function**

```javascript
async function installFromURL() {
    const urlInput = document.getElementById('plugin-url-input');
    const url = urlInput?.value?.trim();
    if (!url) {
        showToast('Please enter a URL');
        return;
    }

    try {
        const installBtn = document.getElementById('plugin-url-install-btn');
        if (installBtn) {
            installBtn.disabled = true;
            installBtn.textContent = 'Fetching...';
        }

        const preview = await window.go.main.PluginService.PreviewPluginFromURL(url);
        if (!preview) {
            showToast('Failed to fetch plugin from URL');
            return;
        }

        const approved = await showPluginReview(preview, 'install');
        if (!approved) return;

        const result = await window.go.main.PluginService.ConfirmPluginInstall(url);
        if (result) {
            showToast(`Installed: ${result.name}`);
            hideURLInput();
            await loadPlugins();
            await loadPluginUIActions();
            loadClips();
        }
    } catch (error) {
        console.error('Failed to install from URL:', error);
        showToast('Failed to install: ' + (error.message || 'Unknown error'));
    } finally {
        const installBtn = document.getElementById('plugin-url-install-btn');
        if (installBtn) {
            installBtn.disabled = false;
            installBtn.textContent = 'Install';
        }
    }
}

function toggleURLInput() {
    showingURLInput = !showingURLInput;
    renderURLInput();
}

function hideURLInput() {
    showingURLInput = false;
    renderURLInput();
}

function renderURLInput() {
    const container = document.getElementById('plugin-url-container');
    if (!container) return;

    if (showingURLInput) {
        container.classList.remove('hidden');
        container.innerHTML = `
            <div class="flex gap-2 mt-2">
                <input id="plugin-url-input" type="url" placeholder="https://example.com/plugin.lua"
                    data-testid="plugin-url-input"
                    class="flex-1 border border-stone-200 rounded-md text-xs bg-white px-2 py-1.5 placeholder-stone-400 focus:outline-none focus:border-stone-400 focus:ring-1 focus:ring-stone-400/20 transition-colors"
                    autocomplete="off">
                <button id="plugin-url-install-btn" data-testid="plugin-url-install-btn"
                    class="bg-stone-800 hover:bg-stone-700 text-white text-xs font-medium py-1.5 px-3 rounded-md transition-colors whitespace-nowrap"
                    onclick="installFromURL()">Install</button>
            </div>
        `;
        container.querySelector('input')?.focus();
        // Allow Enter to submit
        container.querySelector('input')?.addEventListener('keydown', (e) => {
            if (e.key === 'Enter') installFromURL();
        });
    } else {
        container.classList.add('hidden');
        container.innerHTML = '';
    }
}
```

**Step 4: Add update badge and button to createPluginCard**

In `createPluginCard()`, after the version span (line ~82), add update badge logic. And add an Update button next to the enable toggle.

In the card header HTML template, replace the version span:

```javascript
const updateInfo = pluginUpdates[plugin.id];
const versionHTML = updateInfo
    ? `<span class="text-[10px] text-stone-400 font-mono">v${escapeHTML(plugin.version || '0.0.0')}</span>
       <span class="text-[9px] text-amber-600 font-medium ml-1">Update available</span>`
    : `<span class="text-[10px] text-stone-400 font-mono">v${escapeHTML(plugin.version || '0.0.0')}</span>`;
```

Add Update button in the controls area (before the toggle):

```javascript
const updateBtnHTML = updateInfo
    ? `<button data-action="update" data-testid="update-plugin-${plugin.id}"
              class="border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-600 text-[10px] font-medium py-1 px-2 rounded-md transition-colors">
          Update
       </button>`
    : '';
```

**Step 5: Add update button handler**

In the event listeners section of `createPluginCard`, after the remove button listener:

```javascript
    const updateBtn = li.querySelector('[data-action="update"]');
    if (updateBtn) {
        updateBtn.addEventListener('click', async (e) => {
            e.stopPropagation();
            await updatePlugin(plugin.id, plugin.name);
        });
    }
```

**Step 6: Add updatePlugin function**

```javascript
async function updatePlugin(pluginId, pluginName) {
    try {
        const result = await window.go.main.PluginService.UpdatePlugin(pluginId);

        if (result.needs_review && result.preview) {
            // Get current version for the review modal
            const plugin = pluginsCache.find(p => p.id === pluginId);
            const currentVersion = plugin?.version || '?';
            const approved = await showPluginReview(result.preview, 'update', currentVersion);
            if (!approved) return;

            const updated = await window.go.main.PluginService.ConfirmPluginUpdate(pluginId);
            if (updated) {
                showToast(`Updated ${pluginName} to v${updated.version}`);
                delete pluginUpdates[pluginId];
                await loadPlugins();
                await loadPluginUIActions();
                loadClips();
            }
        } else if (result.success && result.plugin_info) {
            showToast(`Updated ${pluginName} to v${result.plugin_info.version}`);
            delete pluginUpdates[pluginId];
            await loadPlugins();
            await loadPluginUIActions();
            loadClips();
        } else if (result.error) {
            showToast(result.error, 'error');
        }
    } catch (error) {
        console.error('Failed to update plugin:', error);
        showToast('Failed to update plugin: ' + (error.message || 'Unknown error'));
    }
}
```

**Step 7: Listen for update_available events**

At the bottom of `plugins.js`, add:

```javascript
// Listen for plugin update events
if (window.runtime && window.runtime.EventsOn) {
    window.runtime.EventsOn('plugin:update_available', (info) => {
        if (info && info.plugin_id) {
            pluginUpdates[info.plugin_id] = info;
            // Re-render if plugins modal is open
            if (!pluginsModal.classList.contains('opacity-0')) {
                renderPluginsList();
            }
        }
    });
}
```

**Step 8: Commit**

```bash
git add frontend/js/plugins.js
git commit -m "feat: add two-phase install, URL install, and update UI to plugins"
```

---

### Task 13: Update plugin modal header HTML — add URL button and container

**Files:**
- Modify: `frontend/index.html` — add "Install from URL" button and URL input container in plugins modal header

**Step 1: Update the plugins modal header**

Replace the current button group (lines 807-814) with:

```html
                <div class="flex items-center gap-2">
                    <button id="install-url-btn" data-testid="install-url-btn"
                        class="border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-600 text-xs font-medium py-2 px-3 rounded-md transition-colors flex items-center gap-1.5"
                        onclick="toggleURLInput()">
                        <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M13.828 10.172a4 4 0 00-5.656 0l-4 4a4 4 0 105.656 5.656l1.102-1.101m-.758-4.899a4 4 0 005.656 0l4-4a4 4 0 00-5.656-5.656l-1.1 1.1" />
                        </svg>
                        URL
                    </button>
                    <button id="import-plugin-btn" data-testid="import-plugin-btn"
                        class="bg-stone-800 hover:bg-stone-700 text-white text-xs font-medium py-2 px-4 rounded-md transition-colors flex items-center gap-1.5">
                        <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M12 4v16m8-8H4" />
                        </svg>
                        Import
                    </button>
```

After the header div, before `plugins-list-container` (around line 822), add:

```html
            <!-- URL input (shown/hidden by JS) -->
            <div id="plugin-url-container" class="hidden px-5 pb-3 border-b border-stone-100"></div>
```

**Step 2: Commit**

```bash
git add frontend/index.html
git commit -m "feat: add Install from URL button and URL input container"
```

---

### Task 14: Settings modal — update check interval

**Files:**
- Modify: `frontend/index.html` — add Plugin Updates section to settings modal
- Modify: `frontend/js/settings.js` — add interval load/save logic

**Step 1: Add Plugin Updates section to settings modal**

In `frontend/index.html`, after the Backup & Restore section (after line 789, before the closing `</div>` of `p-5 space-y-6`), add:

```html
                <!-- Plugin Updates -->
                <div class="pt-4 border-t border-stone-100">
                    <h3 class="text-xs font-semibold text-stone-600 uppercase tracking-wider mb-3 flex items-center gap-2">
                        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
                        </svg>
                        Plugin Updates
                    </h3>
                    <p class="text-[11px] text-stone-500 mb-3">
                        Automatically check installed plugins for new versions.
                    </p>
                    <div class="flex items-center gap-3">
                        <label for="update-interval-select" class="text-[11px] text-stone-600 font-medium">Check for updates</label>
                        <select id="update-interval-select" data-testid="update-interval-select"
                            class="border border-stone-200 rounded-md text-xs bg-white px-2 py-1.5 focus:outline-none focus:border-stone-400 focus:ring-1 focus:ring-stone-400/20 transition-colors">
                            <option value="startup">On startup only</option>
                            <option value="6h">Every 6 hours</option>
                            <option value="24h" selected>Every 24 hours</option>
                            <option value="disabled">Disabled</option>
                        </select>
                    </div>
                </div>
```

**Step 2: Add load/save logic to settings.js**

At the bottom of `settings.js` (before the closing), add:

```javascript
// --- Plugin Update Interval ---
const updateIntervalSelect = document.getElementById('update-interval-select');

async function loadUpdateInterval() {
    try {
        const interval = await window.go.main.PluginService.GetUpdateCheckInterval();
        if (updateIntervalSelect && interval) {
            updateIntervalSelect.value = interval;
        }
    } catch (error) {
        console.error('Failed to load update interval:', error);
    }
}

if (updateIntervalSelect) {
    updateIntervalSelect.addEventListener('change', async () => {
        try {
            await window.go.main.PluginService.SetUpdateCheckInterval(updateIntervalSelect.value);
            showToast('Update check interval saved');
        } catch (error) {
            console.error('Failed to save update interval:', error);
            showToast('Failed to save setting');
        }
    });
}
```

Update the `openSettings()` function to also load the interval:

```javascript
function openSettings() {
    renderHiddenTagsSettings();
    loadUpdateInterval();
    settingsModal.classList.remove('opacity-0', 'pointer-events-none');
    // ...rest unchanged
}
```

**Step 3: Commit**

```bash
git add frontend/index.html frontend/js/settings.js
git commit -m "feat: add plugin update check interval to settings"
```

---

### Task 15: Regenerate bindings and verify full build

**Files:**
- Modify: `frontend/wailsjs/` (auto-generated)

**Step 1: Regenerate all Wails bindings**

Run: `cd /Users/egecan/Code/mahpastes && make bindings`
Expected: New files generated for all new PluginService methods.

**Step 2: Verify full build**

Run: `cd /Users/egecan/Code/mahpastes && ~/go/bin/wails build`
Expected: Build succeeds with no errors.

**Step 3: Commit bindings**

```bash
git add frontend/wailsjs/
git commit -m "chore: regenerate wails bindings for new plugin service methods"
```

---

### Task 16: Add selectors for new UI elements

**Files:**
- Modify: `e2e/helpers/selectors.ts` — add selectors for review modal, URL input, update button, settings interval

**Step 1: Add new selectors**

After the existing `plugins` section (line ~221), add:

```typescript
  // Plugin review modal
  pluginReview: {
    modal: '[data-testid="plugin-review-modal"]',
    cancelButton: '[data-testid="plugin-review-cancel"]',
    approveButton: '[data-testid="plugin-review-approve"]',
    name: '#plugin-review-name',
    version: '#plugin-review-version',
    warning: '#plugin-review-warning',
    networkSection: '#plugin-review-network-section',
    fsSection: '#plugin-review-fs-section',
    clipboardSection: '#plugin-review-clipboard-section',
    eventsSection: '#plugin-review-events-section',
  },

  // Plugin URL install
  pluginURL: {
    installButton: '[data-testid="install-url-btn"]',
    input: '[data-testid="plugin-url-input"]',
    submitButton: '[data-testid="plugin-url-install-btn"]',
    container: '#plugin-url-container',
  },

  // Plugin update
  pluginUpdate: {
    button: (id: number) => `[data-testid="update-plugin-${id}"]`,
  },
```

In the existing `settings` selectors (if any, or in the settings area), add:

```typescript
  // Settings - update interval
  settingsUpdateInterval: {
    select: '[data-testid="update-interval-select"]',
  },
```

**Step 2: Commit**

```bash
git add e2e/helpers/selectors.ts
git commit -m "test: add selectors for plugin review modal, URL install, and update UI"
```

---

### Task 17: Add test helpers to AppHelper

**Files:**
- Modify: `e2e/fixtures/test-fixtures.ts` — add helper methods for review modal, URL install, update

**Step 1: Add new helper methods**

Add to the AppHelper class:

```typescript
  // --- Plugin Review Helpers ---

  async isReviewModalOpen(): Promise<boolean> {
    const modal = this.page.locator('[data-testid="plugin-review-modal"]');
    const opacity = await modal.evaluate(el => !el.classList.contains('opacity-0'));
    return opacity;
  }

  async approvePluginReview(): Promise<void> {
    await this.page.locator('[data-testid="plugin-review-approve"]').click();
    // Wait for modal to close
    await expect(this.page.locator('[data-testid="plugin-review-modal"]')).toHaveClass(/opacity-0/, { timeout: 3000 });
  }

  async cancelPluginReview(): Promise<void> {
    await this.page.locator('[data-testid="plugin-review-cancel"]').click();
    await expect(this.page.locator('[data-testid="plugin-review-modal"]')).toHaveClass(/opacity-0/, { timeout: 3000 });
  }

  async getReviewPluginName(): Promise<string> {
    return this.page.locator('#plugin-review-name').textContent() ?? '';
  }

  // --- Plugin URL Install Helpers ---

  async installPluginFromURL(url: string): Promise<void> {
    await this.openPluginsModal();
    await this.page.locator('[data-testid="install-url-btn"]').click();
    await this.page.locator('[data-testid="plugin-url-input"]').fill(url);
    await this.page.locator('[data-testid="plugin-url-install-btn"]').click();
  }

  // --- Plugin Update Helpers ---

  async previewPluginFromURL(url: string): Promise<any> {
    return this.page.evaluate(async (u) => {
      // @ts-ignore
      return window.go.main.PluginService.PreviewPluginFromURL(u);
    }, url);
  }

  async confirmPluginInstall(source: string): Promise<any> {
    return this.page.evaluate(async (s) => {
      // @ts-ignore
      return window.go.main.PluginService.ConfirmPluginInstall(s);
    }, source);
  }

  async getUpdateCheckInterval(): Promise<string> {
    return this.page.evaluate(async () => {
      // @ts-ignore
      return window.go.main.PluginService.GetUpdateCheckInterval();
    });
  }

  async setUpdateCheckInterval(interval: string): Promise<void> {
    await this.page.evaluate(async (i) => {
      // @ts-ignore
      await window.go.main.PluginService.SetUpdateCheckInterval(i);
    }, interval);
  }
```

**Step 2: Commit**

```bash
git add e2e/fixtures/test-fixtures.ts
git commit -m "test: add helper methods for review modal, URL install, and updates"
```

---

### Task 18: E2E tests — plugin install from file with review

**Files:**
- Create: `e2e/tests/plugins/plugin-review.spec.ts`

**Step 1: Write tests**

```typescript
import { test, expect } from '../../fixtures/test-fixtures';
import { generateTestImage, createTempFile } from '../../helpers/test-data';
import * as path from 'path';
import * as fs from 'fs';

const simplePlugin = `
Plugin = {
    name = "Review Test Plugin",
    version = "1.0.0",
    description = "A plugin for testing the review flow",
    author = "Test Author",
    events = {"clip:created"},
}

function on_clip_created(data) end
`;

const networkPlugin = `
Plugin = {
    name = "Network Plugin",
    version = "1.0.0",
    description = "Plugin with network access",
    author = "Test Author",
    network = {
        ["api.example.com"] = {"GET", "POST"},
    },
    clipboard = true,
    events = {"clip:created"},
}

function on_clip_created(data) end
`;

test.describe('Plugin Review Flow', () => {
    test.beforeEach(async ({ app }) => {
        await app.deleteAllPlugins();
    });

    test('should show review modal with plugin details on file import', async ({ app }) => {
        const pluginPath = await createTempFile(Buffer.from(simplePlugin), 'lua');

        // Use the preview API directly (can't trigger file dialog in e2e)
        const preview = await app.page.evaluate(async (p) => {
            // @ts-ignore
            return window.go.main.PluginService.PreviewPluginFromPath(p);
        }, pluginPath);

        expect(preview).toBeTruthy();
        expect(preview.name).toBe('Review Test Plugin');
        expect(preview.version).toBe('1.0.0');
        expect(preview.author).toBe('Test Author');
        expect(preview.source).toBe(pluginPath);
    });

    test('should install plugin after confirm', async ({ app }) => {
        const pluginPath = await createTempFile(Buffer.from(simplePlugin), 'lua');

        const result = await app.confirmPluginInstall(pluginPath);
        expect(result).toBeTruthy();
        expect(result.name).toBe('Review Test Plugin');

        await app.expectPluginCount(1);
    });

    test('should show network permissions in preview', async ({ app }) => {
        const pluginPath = await createTempFile(Buffer.from(networkPlugin), 'lua');

        const preview = await app.page.evaluate(async (p) => {
            // @ts-ignore
            return window.go.main.PluginService.PreviewPluginFromPath(p);
        }, pluginPath);

        expect(preview.network).toBeTruthy();
        expect(preview.network['api.example.com']).toContain('GET');
        expect(preview.network['api.example.com']).toContain('POST');
        expect(preview.clipboard).toBe(true);
    });
});
```

**Step 2: Run tests**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test -- --grep "Plugin Review"`
Expected: PASS

**Step 3: Commit**

```bash
git add e2e/tests/plugins/plugin-review.spec.ts
git commit -m "test: add e2e tests for plugin review flow"
```

---

### Task 19: E2E tests — plugin install from URL

**Files:**
- Create: `e2e/tests/plugins/plugin-url-install.spec.ts`

This test needs a local HTTP server to serve the plugin file. Use Node's `http` module in the test setup.

**Step 1: Write tests**

```typescript
import { test, expect } from '../../fixtures/test-fixtures';
import * as http from 'http';

const urlPlugin = `
Plugin = {
    name = "URL Test Plugin",
    version = "1.0.0",
    description = "Installed from URL",
    author = "URL Author",
    events = {"clip:created"},
}

function on_clip_created(data) end
`;

let server: http.Server;
let serverURL: string;

test.describe('Plugin URL Install', () => {
    test.beforeAll(async () => {
        server = http.createServer((req, res) => {
            res.writeHead(200, { 'Content-Type': 'text/plain' });
            res.end(urlPlugin);
        });
        await new Promise<void>((resolve) => {
            server.listen(0, '127.0.0.1', () => {
                const addr = server.address();
                if (addr && typeof addr !== 'string') {
                    serverURL = `http://127.0.0.1:${addr.port}/test-plugin.lua`;
                }
                resolve();
            });
        });
    });

    test.afterAll(async () => {
        if (server) {
            await new Promise<void>((resolve) => server.close(() => resolve()));
        }
    });

    test.beforeEach(async ({ app }) => {
        await app.deleteAllPlugins();
    });

    test('should preview plugin from URL', async ({ app }) => {
        const preview = await app.previewPluginFromURL(serverURL);
        expect(preview).toBeTruthy();
        expect(preview.name).toBe('URL Test Plugin');
        expect(preview.version).toBe('1.0.0');
        expect(preview.source).toBe(serverURL);
    });

    test('should install plugin from URL after confirm', async ({ app }) => {
        const result = await app.confirmPluginInstall(serverURL);
        expect(result).toBeTruthy();
        expect(result.name).toBe('URL Test Plugin');
        await app.expectPluginCount(1);
    });

    test('should reject invalid URL', async ({ app }) => {
        let error: string | null = null;
        try {
            await app.previewPluginFromURL('ftp://invalid.com/plugin.lua');
        } catch (e: any) {
            error = e.message || String(e);
        }
        expect(error).toBeTruthy();
    });

    test('should reject non-existent URL', async ({ app }) => {
        let error: string | null = null;
        try {
            await app.previewPluginFromURL('http://127.0.0.1:1/nonexistent.lua');
        } catch (e: any) {
            error = e.message || String(e);
        }
        expect(error).toBeTruthy();
    });
});
```

**Step 2: Run tests**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test -- --grep "Plugin URL Install"`
Expected: PASS

**Step 3: Commit**

```bash
git add e2e/tests/plugins/plugin-url-install.spec.ts
git commit -m "test: add e2e tests for plugin URL install"
```

---

### Task 20: E2E tests — update checking and settings

**Files:**
- Create: `e2e/tests/plugins/plugin-updates.spec.ts`

**Step 1: Write tests**

```typescript
import { test, expect } from '../../fixtures/test-fixtures';

test.describe('Plugin Update Settings', () => {
    test('should get default update check interval', async ({ app }) => {
        const interval = await app.getUpdateCheckInterval();
        expect(interval).toBe('24h');
    });

    test('should set and get update check interval', async ({ app }) => {
        await app.setUpdateCheckInterval('6h');
        const interval = await app.getUpdateCheckInterval();
        expect(interval).toBe('6h');

        // Reset to default
        await app.setUpdateCheckInterval('24h');
    });

    test('should reject invalid interval', async ({ app }) => {
        let error: string | null = null;
        try {
            await app.setUpdateCheckInterval('invalid');
        } catch (e: any) {
            error = e.message || String(e);
        }
        expect(error).toBeTruthy();
    });
});
```

**Step 2: Run tests**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test -- --grep "Plugin Update Settings"`
Expected: PASS

**Step 3: Commit**

```bash
git add e2e/tests/plugins/plugin-updates.spec.ts
git commit -m "test: add e2e tests for plugin update settings"
```

---

### Task 21: Run full e2e test suite and fix

**Step 1: Run all tests**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test`
Expected: All tests pass. If any fail:
- The `ImportPlugin()` return type changed from `*PluginInfo` to `*PluginPreview`. Check if any existing tests call `ImportPlugin` via the UI and expect `PluginInfo` fields.
- The `ImportPluginFromPath` still works as before (backwards compat), so test helpers using it should be fine.
- Fix any breakage, then re-run.

**Step 2: Fix any failures and commit**

```bash
git add -A
git commit -m "fix: ensure all e2e tests pass with new plugin install flow"
```

---

### Task 22: Run Go unit tests

**Step 1: Run all Go tests**

Run: `cd /Users/egecan/Code/mahpastes && go test ./... -v`
Expected: All pass.

**Step 2: Fix any failures and commit if needed**

---

### Task 23: Manual smoke test

**Step 1: Start dev server**

Run: `cd /Users/egecan/Code/mahpastes && make dev`

**Step 2: Test the full flow manually**

1. Open Plugins modal — verify "URL" button appears next to "Import"
2. Click "URL" — verify input field appears
3. Paste a valid plugin URL, click Install — verify review modal appears with permissions
4. Click "Approve & Install" — verify plugin appears in list
5. Open Settings — verify "Plugin Updates" section with dropdown
6. Change interval — verify toast "Update check interval saved"
7. Import a plugin from file — verify review modal appears before install

**Step 3: Final commit if any last fixes needed**

```bash
git add -A
git commit -m "fix: final adjustments from manual smoke test"
```
