package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootstrapAPIKeyWritesFileOnceAndIsIdempotent(t *testing.T) {
	db := newServerTestDB(t)
	app := &App{db: db}
	dir := t.TempDir()
	manager := NewAPIManager(app, dir)

	BootstrapAPIKey(manager)

	keys, err := manager.ListKeys()
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected exactly 1 bootstrap key, got %d", len(keys))
	}
	if keys[0].Role != "admin" {
		t.Fatalf("bootstrap key role = %q, want admin", keys[0].Role)
	}

	// The secret must be persisted to a 0600 file, not just logged.
	path := filepath.Join(dir, bootstrapKeyFile)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("bootstrap key file missing: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("bootstrap key file mode = %o, want 600", perm)
	}
	body, _ := os.ReadFile(path)
	if !strings.HasPrefix(strings.TrimSpace(string(body)), "mp_") {
		t.Fatalf("bootstrap key file does not contain a key: %q", body)
	}

	// Running again must NOT mint a second key (keys already exist).
	BootstrapAPIKey(manager)
	keys, _ = manager.ListKeys()
	if len(keys) != 1 {
		t.Fatalf("bootstrap must be one-shot, got %d keys", len(keys))
	}
}

func TestBootstrapAPIKeyRespectsFalseyOptOut(t *testing.T) {
	for _, v := range []string{"false", "0", "FALSE", "off"} {
		t.Run(v, func(t *testing.T) {
			if v == "off" {
				// ParseBool does not accept "off"; it is not a valid opt-out and
				// should leave bootstrapping enabled. Asserted below.
			}
			db := newServerTestDB(t)
			app := &App{db: db}
			manager := NewAPIManager(app, t.TempDir())
			t.Setenv("MAHPASTESD_BOOTSTRAP_KEY", v)

			BootstrapAPIKey(manager)

			keys, _ := manager.ListKeys()
			parsedFalsey := v == "false" || v == "0" || v == "FALSE"
			if parsedFalsey && len(keys) != 0 {
				t.Fatalf("opt-out %q should skip bootstrap, got %d keys", v, len(keys))
			}
			if !parsedFalsey && len(keys) != 1 {
				t.Fatalf("non-boolean %q should NOT disable bootstrap, got %d keys", v, len(keys))
			}
		})
	}
}
