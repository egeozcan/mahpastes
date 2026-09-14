package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"go-clipboard/internal/fpnative"
)

func TestRemoveStaleDomainsKeepsCurrentEnrollment(t *testing.T) {
	current := "0123456789abcdef0123456789abcdef"
	staleOne := "11111111111111111111111111111111"
	staleTwo := "22222222222222222222222222222222"
	var removed []string
	service := &FileProviderService{
		enrollment: providerEnrollment{Domain: current, Enabled: true},
		native: func(operation, domain string) (fpnative.Result, error) {
			if operation != "remove" {
				t.Fatalf("native operation = %q, want remove", operation)
			}
			removed = append(removed, domain)
			return fpnative.Result{}, nil
		},
	}

	if err := service.removeStaleDomains([]string{staleOne, current, staleTwo}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(removed, []string{staleOne, staleTwo}) {
		t.Fatalf("removed = %v", removed)
	}
}

func TestStaleDomainCleanupFailureDoesNotBlockCurrentSignal(t *testing.T) {
	current := "0123456789abcdef0123456789abcdef"
	stale := "11111111111111111111111111111111"
	var operations []string
	service := &FileProviderService{
		enrollment: providerEnrollment{Domain: current, Enabled: true},
		native: func(operation, domain string) (fpnative.Result, error) {
			operations = append(operations, operation+":"+domain)
			if operation == "remove" {
				return fpnative.Result{}, errors.New("transient File Provider failure")
			}
			if operation == "signal" && domain == current {
				return fpnative.Result{}, nil
			}
			t.Fatalf("unexpected native operation %q for %q", operation, domain)
			return fpnative.Result{}, nil
		},
	}

	if err := service.cleanupStaleDomainsAndSignal([]string{current, stale}); err != nil {
		t.Fatal(err)
	}
	want := []string{"remove:" + stale, "signal:" + current}
	if !reflect.DeepEqual(operations, want) {
		t.Fatalf("operations = %v, want %v", operations, want)
	}
}

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
