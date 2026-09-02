package plugin

import (
	"testing"
)

func TestMatchDomain_DotCountSemantics(t *testing.T) {
	cases := []struct {
		pattern string
		domain  string
		want    bool
	}{
		{"example.com", "example.com", true},
		{"example.com", "sub.example.com", false}, // exact pattern never matches deeper
		{"*.fal.media", "v3.fal.media", true},
		{"*.fal.media", "v3b.fal.media", true},
		{"*.fal.media", "fal.media", false},     // bare apex: suffix needs the dot
		{"*.fal.media", "a.b.fal.media", false}, // exactly one extra label only
		{"*.fal.media", "evilsim.fal.media.example", false},
		{"*.fal.media", "notfal.media", false},
		{"*.example.com", "example.com", false},
	}
	for _, c := range cases {
		if got := MatchDomain(c.pattern, c.domain); got != c.want {
			t.Errorf("MatchDomain(%q, %q) = %v, want %v", c.pattern, c.domain, got, c.want)
		}
	}
}

func TestCheckDomainPermission_AgainstPolicy(t *testing.T) {
	manifest := &Manifest{
		Network: map[string][]string{"manifest.example": {"GET"}},
		Settings: []SettingField{
			{Key: "server_url", Type: "url", GrantsNetwork: []string{"POST"}},
		},
	}
	api := NewHTTPAPI(NewNetworkPolicy(nil, 1, manifest))

	// Manifest host, allowed method.
	if err := api.checkDomainPermission("https://manifest.example/x", "GET"); err != nil {
		t.Fatalf("manifest GET: %v", err)
	}
	// Manifest host, wrong method.
	if err := api.checkDomainPermission("https://manifest.example/x", "POST"); err == nil {
		t.Fatal("manifest host must not gain url-setting methods")
	}
	// Granted host, declared method.
	if err := api.checkDomainPermission("https://granted.example/x", "POST"); err == nil {
		t.Fatal("granted host must be denied without a row")
	}
}

func TestCheckDomainPermission_GrantedHost(t *testing.T) {
	db := newPolicyTestDB(t)
	manifest := &Manifest{
		Settings: []SettingField{
			{Key: "server_url", Type: "url", GrantsNetwork: []string{"GET", "POST"}},
		},
	}
	api := NewHTTPAPI(NewNetworkPolicy(db, 1, manifest))
	grantNetwork(t, db, 1, "granted.example", 0)

	if err := api.checkDomainPermission("https://granted.example:8443/x", "POST"); err != nil {
		t.Fatalf("granted host POST: %v", err)
	}
	if err := api.checkDomainPermission("https://granted.example/x", "DELETE"); err == nil {
		t.Fatal("undeclared method must be denied on granted host")
	}
	if err := api.checkDomainPermission("https://granted.example/x", "GET"); err != nil {
		t.Fatalf("granted host GET: %v", err)
	}

	// The error text is asserted on by e2e/tests/plugins/plugin-http-api.spec.ts.
	err := api.checkDomainPermission("https://elsewhere.example/x", "GET")
	if err == nil || err.Error() != "domain not in allowlist: elsewhere.example" {
		t.Fatalf("expected exact allowlist error, got: %v", err)
	}
}
