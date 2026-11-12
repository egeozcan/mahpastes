package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	if _, _, err := stdinFilename("x.md", "", strings.NewReader(""), now); err != errEmptyStdin {
		t.Fatalf("empty stdin with --filename: err = %v", err)
	}
}

// Every sniffable type maps to a fixed extension, not to whatever the host's
// mime tables list first.
func TestExtensionForContentTypeIsDeterministic(t *testing.T) {
	cases := map[string]string{
		"application/x-gzip":           ".gz",
		"audio/mpeg":                   ".mp3",
		"video/mp4":                    ".mp4",
		"audio/wave":                   ".wav",
		"image/x-icon":                 ".ico",
		"text/plain; charset=utf-16be": ".txt",
		"text/markdown":                ".md",
		"TEXT/HTML; charset=utf-8":     ".html",
		"":                             ".txt",
	}
	for ct, want := range cases {
		if got := extensionForContentType(ct); got != want {
			t.Errorf("%q -> %q, want %q", ct, got, want)
		}
	}
	// What http.DetectContentType can report for these magic numbers.
	sniffs := map[string]string{
		"\x1f\x8b\x08":                    ".gz",
		"ID3\x03\x00":                     ".mp3",
		"wOFF":                            ".woff",
		"\x00asm":                         ".wasm",
		"BM" + strings.Repeat("\x00", 20): ".bmp",
	}
	for head, want := range sniffs {
		if got := sniffExtension([]byte(head)); got != want {
			t.Errorf("sniff %q -> %q, want %q", head, got, want)
		}
	}
}

func TestLooksBinary(t *testing.T) {
	if looksBinary([]byte("plain text")) || !looksBinary([]byte("a\x00b")) {
		t.Fatal("looksBinary wrong")
	}
}

func TestLatestClipID(t *testing.T) {
	var q url.Values
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
	if err != nil || id != 42 || q.Get("limit") != "1" || q.Get("archived") != "false" {
		t.Fatalf("id=%d err=%v q=%v", id, err, q)
	}
	empty = true
	if _, err := latestClipID(c, ""); err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestVersionFlag(t *testing.T) {
	if rootCmd.Version == "" || rootCmd.Version != versionString() {
		t.Fatalf("rootCmd.Version = %q", rootCmd.Version)
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
