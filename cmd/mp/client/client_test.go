package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeKeyFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	return path
}

func TestNewReadsKeyFile(t *testing.T) {
	t.Setenv("MP_API_KEY", "")
	t.Setenv("MP_API_KEY_FILE", writeKeyFile(t, "mp_abc\n"))

	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.APIKey != "mp_abc" {
		t.Fatalf("APIKey = %q, want mp_abc", c.APIKey)
	}
}

func TestNewEnvKeyWinsOverKeyFile(t *testing.T) {
	t.Setenv("MP_API_KEY", "mp_env")
	// A file that cannot be read proves it is not even opened.
	t.Setenv("MP_API_KEY_FILE", filepath.Join(t.TempDir(), "missing"))

	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.APIKey != "mp_env" {
		t.Fatalf("APIKey = %q, want mp_env", c.APIKey)
	}
}

func TestNewRejectsBadKeyFiles(t *testing.T) {
	cases := map[string]struct {
		path    func(t *testing.T) string
		wantErr string
	}{
		"missing": {
			path:    func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing") },
			wantErr: "cannot read MP_API_KEY_FILE",
		},
		"empty": {
			path:    func(t *testing.T) string { return writeKeyFile(t, "") },
			wantErr: "is empty",
		},
		"whitespace only": {
			path:    func(t *testing.T) string { return writeKeyFile(t, " \n\t\n") },
			wantErr: "is empty",
		},
		"two lines": {
			path:    func(t *testing.T) string { return writeKeyFile(t, "mp_abc\nmp_def\n") },
			wantErr: "only the API key",
		},
		"too large": {
			path:    func(t *testing.T) string { return writeKeyFile(t, strings.Repeat("a", maxKeyFileSize+1)) },
			wantErr: "too large",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("MP_API_KEY", "")
			t.Setenv("MP_API_KEY_FILE", tc.path(t))
			c, err := New()
			if err == nil {
				t.Fatalf("New succeeded with key %q, want an error", c.APIKey)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

func TestNewRequiresAKey(t *testing.T) {
	t.Setenv("MP_API_KEY", "")
	t.Setenv("MP_API_KEY_FILE", "")
	if _, err := New(); err == nil || !strings.Contains(err.Error(), "MP_API_KEY_FILE") {
		t.Fatalf("error = %v, want one that mentions MP_API_KEY_FILE", err)
	}
}
