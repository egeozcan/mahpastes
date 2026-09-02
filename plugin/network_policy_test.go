package plugin

import (
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func newPolicyTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open DB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE plugin_permissions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		plugin_id INTEGER NOT NULL,
		permission_type TEXT NOT NULL,
		path TEXT NOT NULL,
		granted_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		pending_reconfirm INTEGER DEFAULT 0
	)`); err != nil {
		t.Fatalf("create plugin_permissions: %v", err)
	}
	return db
}

func grantNetwork(t *testing.T, db *sql.DB, pluginID int64, host string, pending int) {
	t.Helper()
	if _, err := db.Exec(
		"INSERT INTO plugin_permissions (plugin_id, permission_type, path, pending_reconfirm) VALUES (?, 'network', ?, ?)",
		pluginID, host, pending,
	); err != nil {
		t.Fatalf("grant: %v", err)
	}
}

func TestNetworkPolicy_ManifestHostStillAllowed(t *testing.T) {
	manifest := &Manifest{
		Network: map[string][]string{"api.example.com": {"GET", "POST"}},
		Settings: []SettingField{
			{Key: "server_url", Type: "url", GrantsNetwork: []string{"GET"}},
		},
	}
	np := NewNetworkPolicy(nil, 1, manifest)

	if err := np.Allowed("api.example.com", "GET"); err != nil {
		t.Fatalf("manifest GET should be allowed: %v", err)
	}
	if err := np.Allowed("api.example.com", "DELETE"); err == nil {
		t.Fatal("manifest DELETE should be denied")
	}
	if err := np.Allowed("other.example.com", "GET"); err == nil {
		t.Fatal("unlisted host should be denied")
	}
}

func TestNetworkPolicy_GrantedHostAllowedForDeclaredMethodsOnly(t *testing.T) {
	db := newPolicyTestDB(t)
	manifest := &Manifest{
		Settings: []SettingField{
			{Key: "server_url", Type: "url", GrantsNetwork: []string{"GET", "POST"}},
		},
	}
	np := NewNetworkPolicy(db, 1, manifest)
	grantNetwork(t, db, 1, "myserver.example", 0)

	// Granted host works for every declared method, regardless of case.
	if err := np.Allowed("myserver.example", "GET"); err != nil {
		t.Fatalf("granted GET should be allowed: %v", err)
	}
	if err := np.Allowed("myserver.example", "post"); err != nil {
		t.Fatalf("granted POST should be allowed: %v", err)
	}
	// A method the manifest never asked for is denied even though a row
	// exists — grants derive from the manifest, not the DB.
	if err := np.Allowed("myserver.example", "DELETE"); err == nil {
		t.Fatal("undeclared DELETE should be denied on a granted host")
	}

	// The union across several url settings is what a granted host gets.
	db2 := newPolicyTestDB(t)
	manifest2 := &Manifest{
		Settings: []SettingField{
			{Key: "server_url", Type: "url", GrantsNetwork: []string{"GET"}},
			{Key: "mirror_url", Type: "url", GrantsNetwork: []string{"PUT"}},
		},
	}
	np2 := NewNetworkPolicy(db2, 1, manifest2)
	grantNetwork(t, db2, 1, "host.example", 0)
	if err := np2.Allowed("host.example", "PUT"); err != nil {
		t.Fatalf("union PUT should be allowed: %v", err)
	}
}

func TestNetworkPolicy_UngrantedHostDenied(t *testing.T) {
	db := newPolicyTestDB(t)
	manifest := &Manifest{
		Settings: []SettingField{
			{Key: "server_url", Type: "url", GrantsNetwork: []string{"GET"}},
		},
	}
	np := NewNetworkPolicy(db, 1, manifest)
	grantNetwork(t, db, 1, "granted.example", 0)

	err := np.Allowed("elsewhere.example", "GET")
	if err == nil {
		t.Fatal("ungranted host should be denied")
	}
	if !strings.Contains(err.Error(), "domain not in allowlist") {
		t.Fatalf("expected allowlist error text, got: %v", err)
	}
}

func TestNetworkPolicy_PendingReconfirmRowDenied(t *testing.T) {
	db := newPolicyTestDB(t)
	manifest := &Manifest{
		Settings: []SettingField{
			{Key: "server_url", Type: "url", GrantsNetwork: []string{"GET"}},
		},
	}
	np := NewNetworkPolicy(db, 1, manifest)
	grantNetwork(t, db, 1, "restored.example", 1)

	if err := np.Allowed("restored.example", "GET"); err == nil {
		t.Fatal("a pending_reconfirm row must not silently re-grant after a backup restore")
	}
}

func TestNetworkPolicy_GrantedWildcardHostNeverMatchesAsWildcard(t *testing.T) {
	db := newPolicyTestDB(t)
	manifest := &Manifest{
		Settings: []SettingField{
			{Key: "server_url", Type: "url", GrantsNetwork: []string{"GET"}},
		},
	}
	np := NewNetworkPolicy(db, 1, manifest)
	// A row containing `*` (e.g. hand-inserted into the DB) must only ever
	// match the literal string, which url.Hostname() cannot produce — it must
	// not behave like the manifest's `*.` wildcard.
	grantNetwork(t, db, 1, "*.zone.example", 0)

	if err := np.Allowed("sub.zone.example", "GET"); err == nil {
		t.Fatal("a granted `*.zone.example` row must not match sub.zone.example")
	}
	// The manifest wildcard still works, for contrast.
	manifest.Network = map[string][]string{"*.zone.example": {"GET"}}
	np = NewNetworkPolicy(db, 1, manifest)
	if err := np.Allowed("sub.zone.example", "GET"); err != nil {
		t.Fatalf("manifest wildcard should still match: %v", err)
	}
}

func TestNormalizeGrantHost(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"https://MyServer.Example/", "myserver.example", false},
		{"http://localhost:8181", "localhost", false},
		{"myserver.example:8181", "myserver.example", false}, // scheme-less parses as http
		{"https://host.example.", "host.example", false},     // trailing dot stripped
		{"https://[2001:DB8::1]:8080/", "2001:db8::1", false},
		{"", "", true},
		{"   ", "", true},
		{"https://*.zone.example/", "", true},
		{"https://user@host.example/", "host.example", false},
		{"https://", "", true},
	}
	for _, c := range cases {
		got, err := NormalizeGrantHost(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("NormalizeGrantHost(%q) = %q, want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizeGrantHost(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("NormalizeGrantHost(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNetworkPolicy_InvalidatePicksUpNewGrants(t *testing.T) {
	db := newPolicyTestDB(t)
	manifest := &Manifest{
		Settings: []SettingField{
			{Key: "server_url", Type: "url", GrantsNetwork: []string{"GET"}},
		},
	}
	np := NewNetworkPolicy(db, 1, manifest)

	// Prime the cache with no grants.
	if err := np.Allowed("late.example", "GET"); err == nil {
		t.Fatal("should be denied before the grant exists")
	}

	grantNetwork(t, db, 1, "late.example", 0)
	// Without Invalidate the cached set may still hide the row — call it the
	// way the grant path does, and the next request must see it.
	np.Invalidate()
	if err := np.Allowed("late.example", "GET"); err != nil {
		t.Fatalf("grant should be visible after Invalidate: %v", err)
	}
}

func TestNetworkPolicy_RequestHostNormalizedLikeGrant(t *testing.T) {
	db := newPolicyTestDB(t)
	manifest := &Manifest{
		Settings: []SettingField{
			{Key: "server_url", Type: "url", GrantsNetwork: []string{"GET"}},
		},
	}
	np := NewNetworkPolicy(db, 1, manifest)
	grantNetwork(t, db, 1, "host.example", 0)

	// A saved URL with a trailing dot ("https://host.example./") normalizes
	// to the same stored grant; the request host must be normalized the same
	// way or the card says granted while requests are denied.
	if err := np.Allowed("host.example.", "GET"); err != nil {
		t.Fatalf("trailing-dot request host should match the grant: %v", err)
	}

	// IPv6: grants store the unbracketed literal (url.Hostname()); requests
	// must match it exactly. The grant path invalidates the cache; mirror it.
	grantNetwork(t, db, 1, "2001:db8::1", 0)
	np.Invalidate()
	if err := np.Allowed("2001:db8::1", "GET"); err != nil {
		t.Fatalf("IPv6 grant should match: %v", err)
	}
}
