package client

import (
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

func TestResponseHeaderTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "k", HTTPClient: &http.Client{Transport: newTransport(50 * time.Millisecond)}}
	if _, err := c.Get("/"); err == nil {
		t.Fatal("expected timeout error")
	}
}
