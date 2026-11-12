package client

import (
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
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
