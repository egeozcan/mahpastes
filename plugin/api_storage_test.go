package plugin

import (
	"database/sql"
	"testing"

	lua "github.com/yuin/gopher-lua"
	_ "modernc.org/sqlite"
)

// newStorageTestDB creates the tables SetStorageWithGrant and StorageAPI use.
func newStorageTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db := newPolicyTestDB(t)
	if _, err := db.Exec(`CREATE TABLE plugin_storage (
		plugin_id INTEGER NOT NULL,
		key TEXT NOT NULL,
		value BLOB,
		PRIMARY KEY (plugin_id, key)
	)`); err != nil {
		t.Fatalf("create plugin_storage: %v", err)
	}
	return db
}

func networkRows(t *testing.T, db *sql.DB, pluginID int64) []string {
	t.Helper()
	rows, err := db.Query(
		"SELECT path FROM plugin_permissions WHERE plugin_id = ? AND permission_type = 'network' ORDER BY path",
		pluginID,
	)
	if err != nil {
		t.Fatalf("query network rows: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, p)
	}
	return out
}

func newGrantManager(db *sql.DB, manifest *Manifest) (*Manager, *NetworkPolicy) {
	np := NewNetworkPolicy(db, 1, manifest)
	m := &Manager{
		db:               db,
		plugins:          make(map[int64]*Plugin),
		eventSubscribers: make(map[string][]int64),
	}
	m.plugins[1] = &Plugin{ID: 1, Manifest: manifest, networkPolicy: np}
	return m, np
}

func TestSetStorageWithGrant_GrantsRetargetsRevokes(t *testing.T) {
	db := newStorageTestDB(t)
	manifest := &Manifest{
		Settings: []SettingField{
			{Key: "server_url", Type: "url", GrantsNetwork: []string{"GET", "POST"}},
		},
	}
	m, np := newGrantManager(db, manifest)

	// Saving the url setting grants the parsed host, normalized.
	if err := m.SetStorageWithGrant(1, "server_url", "https://Old.Example.:8443/path"); err != nil {
		t.Fatalf("save: %v", err)
	}
	if rows := networkRows(t, db, 1); len(rows) != 1 || rows[0] != "old.example" {
		t.Fatalf("expected exactly [old.example], got %v", rows)
	}
	if err := np.Allowed("old.example", "POST"); err != nil {
		t.Fatalf("granted host should be allowed: %v", err)
	}

	// Retargeting revokes the old host and grants the new one in one save.
	if err := m.SetStorageWithGrant(1, "server_url", "http://new.example:9000"); err != nil {
		t.Fatalf("retarget: %v", err)
	}
	if rows := networkRows(t, db, 1); len(rows) != 1 || rows[0] != "new.example" {
		t.Fatalf("expected exactly [new.example] after retarget, got %v", rows)
	}
	np.Invalidate()
	if err := np.Allowed("old.example", "GET"); err == nil {
		t.Fatal("old host should be denied after retarget")
	}
	if err := np.Allowed("new.example", "GET"); err != nil {
		t.Fatalf("new host should be allowed: %v", err)
	}

	// A wildcard value is rejected and changes nothing.
	if err := m.SetStorageWithGrant(1, "server_url", "https://*.zone.example"); err == nil {
		t.Fatal("wildcard host must be rejected")
	}
	if rows := networkRows(t, db, 1); len(rows) != 1 || rows[0] != "new.example" {
		t.Fatalf("rejected save must not change grants, got %v", rows)
	}

	// An empty value revokes everything the settings granted.
	if err := m.SetStorageWithGrant(1, "server_url", ""); err != nil {
		t.Fatalf("empty save: %v", err)
	}
	if rows := networkRows(t, db, 1); len(rows) != 0 {
		t.Fatalf("empty value should revoke all setting grants, got %v", rows)
	}
	np.Invalidate()
	if err := np.Allowed("new.example", "GET"); err == nil {
		t.Fatal("host should be denied after empty-value revoke")
	}
}

func TestSetStorageWithGrant_ReconcilesOtherURLSettings(t *testing.T) {
	db := newStorageTestDB(t)
	manifest := &Manifest{
		Settings: []SettingField{
			{Key: "server_url", Type: "url", GrantsNetwork: []string{"GET"}},
			{Key: "mirror_url", Type: "url", GrantsNetwork: []string{"GET"}},
			{Key: "note", Type: "text", Label: "Note"},
		},
	}
	m, _ := newGrantManager(db, manifest)

	if err := m.SetStorageWithGrant(1, "server_url", "https://primary.example"); err != nil {
		t.Fatalf("save primary: %v", err)
	}
	if err := m.SetStorageWithGrant(1, "mirror_url", "https://mirror.example"); err != nil {
		t.Fatalf("save mirror: %v", err)
	}
	rows := networkRows(t, db, 1)
	if len(rows) != 2 || rows[0] != "mirror.example" || rows[1] != "primary.example" {
		t.Fatalf("expected both hosts granted, got %v", rows)
	}

	// A plain (non-url) setting write must not disturb grants.
	if err := m.SetStorageWithGrant(1, "note", "hello"); err != nil {
		t.Fatalf("save note: %v", err)
	}
	if rows := networkRows(t, db, 1); len(rows) != 2 {
		t.Fatalf("non-url write must not change grants, got %v", rows)
	}

	// Retargeting the primary revokes only the primary.
	if err := m.SetStorageWithGrant(1, "server_url", "https://moved.example"); err != nil {
		t.Fatalf("move primary: %v", err)
	}
	rows = networkRows(t, db, 1)
	if len(rows) != 2 || rows[0] != "mirror.example" || rows[1] != "moved.example" {
		t.Fatalf("expected [mirror.example moved.example], got %v", rows)
	}
}

func TestStorageAPI_UrlKeySetRejectedFromLua(t *testing.T) {
	db := newStorageTestDB(t)
	// Seed the user-owned value the way the settings panel would.
	if _, err := db.Exec(
		"INSERT INTO plugin_storage (plugin_id, key, value) VALUES (1, 'server_url', 'https://user.example')",
	); err != nil {
		t.Fatalf("seed: %v", err)
	}

	L := lua.NewState()
	defer L.Close()
	NewStorageAPI(db, 1, map[string]bool{"server_url": true}).Register(L)

	err := L.DoString(`
		local ok, err = storage.set("server_url", "https://evil.example")
		assert(not ok, "storage.set on a url key must fail")
		assert(err and err ~= "", "set must report a reason")
		-- storage.get stays untouched: the plugin still reads its URL.
		local v = storage.get("server_url")
		assert(v == "https://user.example", "expected user value, got " .. tostring(v))
		-- Ordinary keys are unaffected.
		local ok2, err2 = storage.set("other", "fine")
		assert(ok2, err2)
	`)
	if err != nil {
		t.Fatalf("storage API behavior: %v", err)
	}
}

func TestSetStorageWithGrant_ReactivatesPendingRow(t *testing.T) {
	db := newStorageTestDB(t)
	manifest := &Manifest{
		Settings: []SettingField{
			{Key: "server_url", Type: "url", GrantsNetwork: []string{"GET"}},
		},
	}
	m, np := newGrantManager(db, manifest)

	// A restored backup flags every permission row pending; the policy must
	// deny it (mirroring FilesystemAPI.loadPermissions).
	grantNetwork(t, db, 1, "restored.example", 1)
	if err := np.Allowed("restored.example", "GET"); err == nil {
		t.Fatal("pending row must be denied before re-approval")
	}

	// The user explicitly re-saving the URL setting re-approves: the row's
	// pending flag clears and the host is reachable again.
	if err := m.SetStorageWithGrant(1, "server_url", "https://restored.example/"); err != nil {
		t.Fatalf("save: %v", err)
	}
	var pending int
	if err := db.QueryRow(
		"SELECT COALESCE(pending_reconfirm, 0) FROM plugin_permissions WHERE plugin_id = 1 AND permission_type = 'network' AND path = 'restored.example'",
	).Scan(&pending); err != nil {
		t.Fatalf("query pending: %v", err)
	}
	if pending != 0 {
		t.Fatalf("save must clear pending_reconfirm, got %d", pending)
	}
	if err := np.Allowed("restored.example", "GET"); err != nil {
		t.Fatalf("granted host should be allowed after re-approval: %v", err)
	}
}

func TestSetStorageWithGrant_RejectedValueNotPersisted(t *testing.T) {
	db := newStorageTestDB(t)
	manifest := &Manifest{
		Settings: []SettingField{
			{Key: "server_url", Type: "url", GrantsNetwork: []string{"GET"}},
		},
	}
	m, _ := newGrantManager(db, manifest)

	// First save a good value.
	if err := m.SetStorageWithGrant(1, "server_url", "https://good.example"); err != nil {
		t.Fatalf("save good: %v", err)
	}
	// Then a wildcard value: the whole save must fail WITHOUT persisting the
	// rejected value into plugin_storage.
	if err := m.SetStorageWithGrant(1, "server_url", "https://*.zone.example"); err == nil {
		t.Fatal("wildcard value must be rejected")
	}
	var v string
	if err := db.QueryRow(
		"SELECT value FROM plugin_storage WHERE plugin_id = 1 AND key = 'server_url'",
	).Scan(&v); err != nil {
		t.Fatalf("query storage: %v", err)
	}
	if v != "https://good.example" {
		t.Fatalf("rejected value must not overwrite storage, got %q", v)
	}
}

func TestSetStorageWithGrant_SiblingHostsNotApprovedBySiblingSave(t *testing.T) {
	db := newStorageTestDB(t)
	manifest := &Manifest{
		Settings: []SettingField{
			{Key: "server_url", Type: "url", GrantsNetwork: []string{"GET"}},
			{Key: "mirror_url", Type: "url", GrantsNetwork: []string{"GET"}},
		},
	}
	m, np := newGrantManager(db, manifest)

	// Both fields saved; each host gets its own grant.
	if err := m.SetStorageWithGrant(1, "server_url", "https://primary.example"); err != nil {
		t.Fatalf("save primary: %v", err)
	}
	if err := m.SetStorageWithGrant(1, "mirror_url", "https://mirror.example"); err != nil {
		t.Fatalf("save mirror: %v", err)
	}

	// Simulate a backup restore: every row flagged pending, so nothing works.
	if _, err := db.Exec("UPDATE plugin_permissions SET pending_reconfirm = 1"); err != nil {
		t.Fatalf("flag pending: %v", err)
	}
	np.Invalidate()
	if err := np.Allowed("primary.example", "GET"); err == nil {
		t.Fatal("primary must be denied while pending")
	}

	// Re-saving ONLY server_url re-approves primary — it must not silently
	// approve the sibling mirror host.
	if err := m.SetStorageWithGrant(1, "server_url", "https://primary.example"); err != nil {
		t.Fatalf("re-save primary: %v", err)
	}
	np.Invalidate()
	if err := np.Allowed("primary.example", "GET"); err != nil {
		t.Fatalf("primary should be re-approved: %v", err)
	}
	var mirrorPending int
	if err := db.QueryRow(
		"SELECT COALESCE(pending_reconfirm, 0) FROM plugin_permissions WHERE plugin_id = 1 AND permission_type = 'network' AND path = 'mirror.example'",
	).Scan(&mirrorPending); err != nil {
		t.Fatalf("query mirror row: %v", err)
	}
	if mirrorPending != 1 {
		t.Fatalf("saving primary must not re-approve the sibling mirror host (pending=%d)", mirrorPending)
	}
	if err := np.Allowed("mirror.example", "GET"); err == nil {
		t.Fatal("mirror must stay denied while its own field was not re-saved")
	}

	// A sibling grant revoked from the card must not be resurrected by
	// re-saving the other field.
	if _, err := db.Exec(
		"DELETE FROM plugin_permissions WHERE plugin_id = 1 AND permission_type = 'network' AND path = 'mirror.example'",
	); err != nil {
		t.Fatalf("revoke mirror: %v", err)
	}
	np.Invalidate()
	if err := m.SetStorageWithGrant(1, "server_url", "https://primary.example"); err != nil {
		t.Fatalf("re-save primary again: %v", err)
	}
	var count int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM plugin_permissions WHERE plugin_id = 1 AND permission_type = 'network' AND path = 'mirror.example'",
	).Scan(&count); err != nil {
		t.Fatalf("count mirror rows: %v", err)
	}
	if count != 0 {
		t.Fatal("saving primary must not resurrect a revoked sibling grant")
	}
}
