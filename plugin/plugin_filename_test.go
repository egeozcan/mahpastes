package plugin

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"go-clipboard/internal/wailsbridge"

	_ "modernc.org/sqlite"
)

// A plugins row's filename is joined to the plugins dir to read the plugin and,
// on removal, to delete its file. Rows restored from a crafted backup before
// restores checked it can still name a path out of that dir; the manager must
// neither read nor delete through one.
func TestPluginFilenameCannotLeaveThePluginsDir(t *testing.T) {
	root := t.TempDir()
	pluginsDir := filepath.Join(root, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(root, "victim.lua")
	if err := os.WriteFile(victim, []byte(`Plugin = { name = "Victim", version = "1.0.0" }`), 0o644); err != nil {
		t.Fatal(err)
	}

	// A file, not :memory: — LoadPlugins keeps its row cursor open while
	// loading, so it needs more than one connection to the same database.
	db, err := sql.Open("sqlite", filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE plugins (id INTEGER PRIMARY KEY, filename TEXT, name TEXT, version TEXT,
		enabled INTEGER DEFAULT 1, status TEXT DEFAULT 'enabled', error_count INTEGER DEFAULT 0);
		INSERT INTO plugins (id, filename, name, version) VALUES (1, '../victim.lua', 'Helper', '1.0')`); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(context.Background(), wailsbridge.NewForTesting(), db, pluginsDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.LoadPlugins(); err != nil {
		t.Fatal(err)
	}
	if n := len(m.GetPlugins()); n != 0 {
		t.Fatalf("loaded %d plugin(s) from a file outside the plugins dir", n)
	}

	if err := m.RemovePlugin(1); err != nil {
		t.Fatalf("RemovePlugin: %v", err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("removing the plugin deleted a file outside the plugins dir: %v", err)
	}
}
