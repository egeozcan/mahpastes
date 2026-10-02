package app

import (
	"os"
	"path/filepath"
	"testing"

	"go-clipboard/plugin"
)

// applyPluginUpdate writes the new source to the plugins dir joined with the
// row's filename. A row restored from a crafted backup before restores checked
// that column can name a path out of the dir, and the update — whose source
// the same backup can choose through source_url — would then overwrite any
// file the user can write.
func TestPluginUpdateCannotWriteOutsideThePluginsDir(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()
	pm, pluginsDir := newTestPluginManager(t, app)
	defer pm.Shutdown()

	victim := filepath.Join(filepath.Dir(pluginsDir), "victim.lua")
	if err := os.WriteFile(victim, []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.db.Exec(`INSERT INTO plugins (id, filename, name, version) VALUES (77, '../victim.lua', 'Helper', '1.0')`); err != nil {
		t.Fatal(err)
	}

	src := `Plugin = { name = "Helper", version = "2.0.0" }`
	manifest, err := plugin.ParseManifest(src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.applyPluginUpdate(77, src, manifest, "https://example.invalid/helper.lua"); err == nil {
		t.Fatal("applyPluginUpdate accepted a filename outside the plugins dir")
	}
	if got, _ := os.ReadFile(victim); string(got) != "precious" {
		t.Fatalf("a plugin update overwrote a file outside the plugins dir: now %q", got)
	}

	// Where nothing existed, there is no old content to roll back to: the
	// write itself is the damage.
	planted := filepath.Join(filepath.Dir(pluginsDir), "planted.lua")
	if _, err := app.db.Exec(`INSERT INTO plugins (id, filename, name, version) VALUES (78, '../planted.lua', 'Helper2', '1.0')`); err != nil {
		t.Fatal(err)
	}
	if _, err := app.applyPluginUpdate(78, src, manifest, "https://example.invalid/helper.lua"); err == nil {
		t.Fatal("applyPluginUpdate accepted a filename outside the plugins dir")
	}
	if _, err := os.Stat(planted); err == nil {
		t.Fatal("a plugin update created a file outside the plugins dir")
	}
}
