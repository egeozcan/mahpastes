package plugin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckPluginUpdate_DetectsNewerVersion(t *testing.T) {
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
		t.Error("should not have permission changes")
	}
}

func TestCheckPluginUpdate_NoUpdateWhenSameVersion(t *testing.T) {
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

func TestCheckPluginUpdate_DetectsPermissionChanges(t *testing.T) {
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
		t.Error("should detect permission changes")
	}
}
