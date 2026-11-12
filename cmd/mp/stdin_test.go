package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-clipboard/cmd/mp/client"
)

func TestStdinFilenameSniffsExtension(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("x", 600)
	cases := []struct{ in, want string }{
		{"hello", "paste-20260102-030405.txt"},
		{"", "paste-20260102-030405.txt"},
		{png, "paste-20260102-030405.png"},
		{"\x00\x01\x02\x03binary", "paste-20260102-030405.bin"},
	}
	for _, c := range cases {
		name, r, err := stdinFilename("", "", strings.NewReader(c.in), now)
		if err != nil || name != c.want {
			t.Fatalf("%q: name=%q err=%v want %q", c.in[:min(len(c.in), 8)], name, err, c.want)
		}
		b, _ := io.ReadAll(r)
		if string(b) != c.in {
			t.Fatalf("reader did not replay input")
		}
	}
	name, _, _ := stdinFilename("x.md", "", strings.NewReader("a"), now)
	if name != "x.md" {
		t.Fatalf("explicit name lost: %s", name)
	}
	name, _, _ = stdinFilename("", "application/pdf", strings.NewReader("a"), now)
	if !strings.HasSuffix(name, ".pdf") {
		t.Fatalf("content-type ext: %s", name)
	}
}

func TestLooksBinary(t *testing.T) {
	if looksBinary([]byte("plain text")) || !looksBinary([]byte("a\x00b")) {
		t.Fatal("looksBinary wrong")
	}
}

func TestLatestClipID(t *testing.T) {
	var q map[string][]string
	empty := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.Query()
		if empty {
			w.Write([]byte(`{"clips":[]}`))
			return
		}
		w.Write([]byte(`{"clips":[{"id":42}]}`))
	}))
	defer srv.Close()
	t.Setenv("MP_API_URL", srv.URL)
	t.Setenv("MP_API_KEY", "k")
	c := newTestClient(t)
	id, err := latestClipID(c, "")
	if err != nil || id != 42 || q["limit"][0] != "1" {
		t.Fatalf("id=%d err=%v q=%v", id, err, q)
	}
	empty = true
	if _, err := latestClipID(c, ""); err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestVersionString(t *testing.T) {
	if versionString() == "" || rootCmd.Version == "" {
		t.Fatal("version not set")
	}
}

func newTestClient(t *testing.T) *client.Client {
	t.Helper()
	c, err := client.New()
	if err != nil {
		t.Fatal(err)
	}
	return c
}
