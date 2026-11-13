package client

import (
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUploadFileCarriesContentTypeOnFilePart(t *testing.T) {
	var gotType, gotName, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		mr := multipart.NewReader(r.Body, params["boundary"])
		p, err := mr.NextPart()
		if err != nil {
			t.Error(err)
			return
		}
		b, _ := io.ReadAll(p)
		gotType, gotName, gotBody = p.Header.Get("Content-Type"), p.FileName(), string(b)
		w.Write([]byte("{}"))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	err := c.UploadFile("/x", `we"ird.txt`, strings.NewReader("hello world"), map[string]string{"content_type": "text/plain"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if gotBody != "hello world" || gotType != "text/plain" || gotName != `we"ird.txt` {
		t.Fatalf("file part = (%q, %q, %q)", gotBody, gotType, gotName)
	}
}

func TestTimeoutFromEnv(t *testing.T) {
	cases := map[string]time.Duration{"": defaultTimeout, "5s": 5 * time.Second, "7": 7 * time.Second, "junk": defaultTimeout, "-1": defaultTimeout}
	for in, want := range cases {
		t.Setenv("MP_TIMEOUT", in)
		if got := timeoutFromEnv(); got != want {
			t.Errorf("MP_TIMEOUT=%q: %v, want %v", in, got, want)
		}
	}
}

// exitCodeOf returns the CLI exit code main() would use for err.
func exitCodeOf(err error) int {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.ExitCode()
	}
	return ExitError
}

func TestResponseHeaderTimeoutExitsWithConnectionCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "k", HTTPClient: &http.Client{Transport: newTransport(50 * time.Millisecond)}}
	_, err := c.Get("/")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if code := exitCodeOf(err); code != ExitConnection {
		t.Fatalf("exit code = %d (%v), want %d", code, err, ExitConnection)
	}
	if !strings.Contains(err.Error(), "MP_TIMEOUT") {
		t.Fatalf("timeout message should mention MP_TIMEOUT: %v", err)
	}
}

func TestLongClientHasNoResponseHeaderTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	tr := newTransport(50 * time.Millisecond)
	c := &Client{BaseURL: srv.URL, APIKey: "k", HTTPClient: &http.Client{Transport: tr}}

	var out struct{ OK bool }
	if err := c.Long().GetJSON("/", &out); err != nil || !out.OK {
		t.Fatalf("long request: ok=%v err=%v", out.OK, err)
	}
	lt := c.Long().HTTPClient.Transport.(*http.Transport)
	if lt.ResponseHeaderTimeout != 0 || lt.TLSHandshakeTimeout != tr.TLSHandshakeTimeout || lt.DialContext == nil {
		t.Fatalf("long transport = %+v", lt)
	}
	if tr.ResponseHeaderTimeout != 50*time.Millisecond {
		t.Fatal("Long() must not modify the original transport")
	}
}

func TestConnectionRefusedExitsWithConnectionCode(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	c := &Client{BaseURL: url, APIKey: "k", HTTPClient: &http.Client{Transport: newTransport(time.Second)}}
	_, err := c.Get("/")
	if code := exitCodeOf(err); code != ExitConnection {
		t.Fatalf("exit code = %d (%v), want %d", code, err, ExitConnection)
	}
}

func TestNewWithoutAPIKeyExitsWithAuthCode(t *testing.T) {
	clearClientEnv(t)
	_, err := New()
	if code := exitCodeOf(err); code != ExitAuth {
		t.Fatalf("exit code = %d (%v), want %d", code, err, ExitAuth)
	}
	if !strings.Contains(err.Error(), "MP_API_KEY") {
		t.Fatalf("message = %q", err)
	}
}

func TestUploadFileReportsServerRejection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Reject without reading the body, like MaxBytesReader on a huge upload.
		w.Header().Set("Connection", "close")
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		w.Write([]byte(`{"error":"file too large"}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	big := strings.NewReader(strings.Repeat("x", 8<<20))
	err := c.UploadFile("/x", "big.bin", big, nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusRequestEntityTooLarge || apiErr.Message != "file too large" {
		t.Fatalf("err = %v, want the server's 413 message", err)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("disk on fire") }

func TestUploadFileReportsLocalReadError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Write([]byte("{}"))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	err := c.UploadFile("/x", "f.bin", failingReader{}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "disk on fire") {
		t.Fatalf("err = %v, want the local read error", err)
	}
}

func TestDoReturnsAPIErrorAndSetsHeaders(t *testing.T) {
	var auth, ua string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, ua = r.Header.Get("Authorization"), r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	req, _ := http.NewRequest("POST", srv.URL+"/x", strings.NewReader("{}"))
	_, err := c.Do(req)
	if code := exitCodeOf(err); code != ExitAuth {
		t.Fatalf("exit code = %d (%v), want %d", code, err, ExitAuth)
	}
	if auth != "Bearer k" || ua == "" {
		t.Fatalf("headers: auth=%q ua=%q", auth, ua)
	}
}

func clearClientEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"MP_API_URL", "MP_API_KEY", "MP_API_KEY_FILE", "MAHPASTES_API_URL", "MAHPASTES_API_KEY"} {
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

func writeKeyFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	return path
}

func TestNewReadsKeyFile(t *testing.T) {
	clearClientEnv(t)
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
	clearClientEnv(t)
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
			clearClientEnv(t)
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
	clearClientEnv(t)
	if _, err := New(); err == nil || !strings.Contains(err.Error(), "MP_API_KEY_FILE") {
		t.Fatalf("error = %v, want one that mentions MP_API_KEY_FILE", err)
	}
}

func TestNewKeyFileWinsOverMahpastesKey(t *testing.T) {
	clearClientEnv(t)
	t.Setenv("MP_API_KEY_FILE", writeKeyFile(t, "mp_file\n"))
	t.Setenv("MAHPASTES_API_KEY", "mp_agent")

	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.APIKey != "mp_file" {
		t.Fatalf("APIKey = %q, want the MP_API_KEY_FILE key", c.APIKey)
	}
}

// Windows PowerShell 5.1 writes files as UTF-16 with a byte order mark by
// default (`"mp_..." > key.txt`, Out-File), and as UTF-8 with one for
// -Encoding utf8. Both hold just the key.
func TestNewReadsKeyFilesWrittenByPowerShell(t *testing.T) {
	utf16le := []byte{0xFF, 0xFE}
	for _, r := range "mp_abc\r\n" {
		utf16le = append(utf16le, byte(r), 0)
	}
	utf16be := []byte{0xFE, 0xFF}
	for _, r := range "mp_abc\r\n" {
		utf16be = append(utf16be, 0, byte(r))
	}
	for name, content := range map[string][]byte{
		"UTF-8 with BOM":  append([]byte{0xEF, 0xBB, 0xBF}, "mp_abc\r\n"...),
		"UTF-16 LE (BOM)": utf16le,
		"UTF-16 BE (BOM)": utf16be,
	} {
		t.Run(name, func(t *testing.T) {
			clearClientEnv(t)
			t.Setenv("MP_API_KEY_FILE", writeKeyFile(t, string(content)))
			c, err := New()
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if c.APIKey != "mp_abc" {
				t.Fatalf("APIKey = %q, want mp_abc", c.APIKey)
			}
		})
	}
}

// A filename is written into the part header by hand; CR and LF in it must be
// escaped as mime/multipart's CreateFormFile does, or the server cannot parse
// the upload and a crafted name adds header lines.
func TestWriteMultipartEscapesFilenameLineBreaks(t *testing.T) {
	for _, name := range []string{
		"new\nline.txt",
		"a\r\nX-Injected: yes\r\nY: .txt",
		`q"uo\te.txt`,
	} {
		var buf strings.Builder
		w := multipart.NewWriter(&buf)
		if err := writeMultipart(w, name, strings.NewReader("hello"), map[string]string{"content_type": "text/plain"}); err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		p, err := multipart.NewReader(strings.NewReader(buf.String()), w.Boundary()).NextPart()
		if err != nil {
			t.Errorf("%q: server cannot read the part: %v", name, err)
			continue
		}
		b, _ := io.ReadAll(p)
		if p.FormName() != "file" || p.FileName() == "" || string(b) != "hello" || p.Header.Get("Content-Type") != "text/plain" {
			t.Errorf("%q: part = (%q, %q, %q, %q)", name, p.FormName(), p.FileName(), b, p.Header.Get("Content-Type"))
		}
		if len(p.Header) != 2 {
			t.Errorf("%q: part headers = %v, want only disposition and type", name, p.Header)
		}
	}
}

func TestWriteMultipartRejectsContentTypeLineBreak(t *testing.T) {
	var buf strings.Builder
	err := writeMultipart(multipart.NewWriter(&buf), "a.txt", strings.NewReader("x"), map[string]string{"content_type": "text/plain\r\nX-Injected: yes"})
	if err == nil || strings.Contains(buf.String(), "X-Injected") {
		t.Fatalf("err = %v, body = %q; want the content type refused", err, buf.String())
	}
}

func TestUploadFileStatusReportsCreatedOrExisting(t *testing.T) {
	for _, want := range []int{http.StatusCreated, http.StatusOK} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.Copy(io.Discard, r.Body)
			w.WriteHeader(want)
			w.Write([]byte(`{"id":7}`))
		}))
		c := &Client{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
		var got struct{ ID int64 }
		status, err := c.UploadFileStatus("/x", "a.txt", strings.NewReader("x"), nil, &got)
		srv.Close()
		if err != nil || status != want || got.ID != 7 {
			t.Fatalf("status = %d, id = %d, err = %v; want %d", status, got.ID, err, want)
		}
	}
}
