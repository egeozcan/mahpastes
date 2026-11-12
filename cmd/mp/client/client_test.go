package client

import "testing"

func clearClientEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"MP_API_URL", "MP_API_KEY", "MAHPASTES_API_URL", "MAHPASTES_API_KEY"} {
		t.Setenv(name, "")
	}
}

func TestNewPrefersMPVariables(t *testing.T) {
	clearClientEnv(t)
	t.Setenv("MP_API_URL", "http://example.test:1234/")
	t.Setenv("MP_API_KEY", "mp_primary")
	t.Setenv("MAHPASTES_API_URL", "http://ignored.test:1")
	t.Setenv("MAHPASTES_API_KEY", "mp_ignored")

	c, err := New()
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if c.BaseURL != "http://example.test:1234" {
		t.Errorf("BaseURL = %q, want trailing slash trimmed MP_API_URL", c.BaseURL)
	}
	if c.APIKey != "mp_primary" {
		t.Errorf("APIKey = %q, want MP_API_KEY", c.APIKey)
	}
}

func TestNewFallsBackToMahpastesVariables(t *testing.T) {
	clearClientEnv(t)
	t.Setenv("MAHPASTES_API_URL", "http://agent.test:9999")
	t.Setenv("MAHPASTES_API_KEY", "mp_agent")

	c, err := New()
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if c.BaseURL != "http://agent.test:9999" {
		t.Errorf("BaseURL = %q, want MAHPASTES_API_URL", c.BaseURL)
	}
	if c.APIKey != "mp_agent" {
		t.Errorf("APIKey = %q, want MAHPASTES_API_KEY", c.APIKey)
	}
}

func TestNewDefaultsURLAndRequiresKey(t *testing.T) {
	clearClientEnv(t)
	if _, err := New(); err == nil {
		t.Fatal("New() without any key: want error, got nil")
	}

	t.Setenv("MP_API_KEY", "mp_k")
	c, err := New()
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if c.BaseURL != "http://localhost:44557" {
		t.Errorf("BaseURL = %q, want default http://localhost:44557", c.BaseURL)
	}
}
