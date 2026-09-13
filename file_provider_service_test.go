package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"go-clipboard/internal/fpnative"
)

func TestDisableKeepsEnrollmentOnPersistenceFailure(t *testing.T) {
	dir := t.TempDir()
	service := &FileProviderService{dataDir: dir, groupPath: dir, enrollment: providerEnrollment{Domain: "0123456789abcdef0123456789abcdef", Enabled: true}}
	if err := service.saveEnrollment(); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join(dir, "file-provider.json"))
	if err != nil {
		t.Fatal(err)
	}
	service.native = func(operation, domain string) (fpnative.Result, error) {
		switch operation {
		case "list":
			return fpnative.Result{Domains: []string{domain}}, nil
		case "remove":
			// Force the later atomic enrollment write to fail, after native removal.
			// A file used as the data directory works even when tests run as root.
			service.dataDir = filepath.Join(dir, "not-a-directory")
			if err := os.WriteFile(service.dataDir, []byte("fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			return fpnative.Result{RecoveryPath: "/test/recovery"}, nil
		default:
			t.Fatalf("unexpected native operation %q", operation)
			return fpnative.Result{}, nil
		}
	}
	discovery := service.discoveryPath()
	if err := os.WriteFile(discovery, []byte("test discovery"), 0600); err != nil {
		t.Fatal(err)
	}
	status, err := service.Disable()
	if err == nil {
		t.Fatal("disable unexpectedly succeeded")
	}
	if !status.Enabled {
		t.Error("failed disable must retain the persisted enabled state")
	}
	if _, err := os.Stat(discovery); err != nil {
		t.Error("failed disable disconnected the enrolled provider:", err)
	}
	current, err := os.ReadFile(filepath.Join(dir, "file-provider.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != string(original) {
		t.Fatal("persisted enrollment changed on failed write")
	}
	// After the filesystem recovers, disabling retries persistence even though
	// macOS has already removed the domain.
	service.dataDir = dir
	service.native = func(operation, domain string) (fpnative.Result, error) {
		if operation != "list" {
			t.Fatalf("unexpected retry operation %q", operation)
		}
		return fpnative.Result{}, nil
	}
	status, err = service.Disable()
	if err != nil || status.Enabled {
		t.Fatalf("disable retry: %+v, %v", status, err)
	}
	current, err = os.ReadFile(filepath.Join(dir, "file-provider.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved providerEnrollment
	if err := json.Unmarshal(current, &saved); err != nil || saved.Enabled {
		t.Fatalf("disable not persisted: %s, %v", current, err)
	}
	if _, err := os.Stat(discovery); !os.IsNotExist(err) {
		t.Fatalf("successful disable left discovery behind: %v", err)
	}
}
