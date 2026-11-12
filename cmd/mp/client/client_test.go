package client

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewKeyFile(t *testing.T) {
	t.Setenv("MP_API_KEY", "")
	f := filepath.Join(t.TempDir(), "k")
	os.WriteFile(f, []byte("mp_abc\n"), 0o600)
	t.Setenv("MP_API_KEY_FILE", f)
	c, err := New()
	if err != nil || c.APIKey != "mp_abc" {
		t.Fatalf("got %+v %v", c, err)
	}
	t.Setenv("MP_API_KEY", "mp_env")
	if c, _ = New(); c.APIKey != "mp_env" {
		t.Fatal("env must win")
	}
	t.Setenv("MP_API_KEY", "")
	t.Setenv("MP_API_KEY_FILE", filepath.Join(t.TempDir(), "missing"))
	if _, err := New(); err == nil {
		t.Fatal("missing file must error")
	}
}
