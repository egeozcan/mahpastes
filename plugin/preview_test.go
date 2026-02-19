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
