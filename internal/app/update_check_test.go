package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func releaseServer(t *testing.T, status int, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("missing User-Agent")
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("request must be anonymous")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestFetchLatestRelease(t *testing.T) {
	newer := `{"tag_name":"v1.6.0","html_url":"https://github.com/egeozcan/mahpastes/releases/tag/v1.6.0"}`
	cases := []struct {
		name    string
		status  int
		body    string
		current string
		want    string // expected Version, "" for none
		wantErr bool
	}{
		{"newer", 200, newer, "v1.5.0", "1.6.0", false},
		{"same", 200, newer, "v1.6.0", "", false},
		{"older remote", 200, newer, "v1.7.0", "", false},
		{"describe suffix of same tag", 200, newer, "v1.6.0-3-gabc123", "", false},
		{"dev build never matches", 200, newer, "dev", "", false},
		{"prerelease ignored", 200, `{"tag_name":"v2.0.0","prerelease":true}`, "v1.5.0", "", false},
		{"draft ignored", 200, `{"tag_name":"v2.0.0","draft":true}`, "v1.5.0", "", false},
		{"foreign url falls back", 200, `{"tag_name":"v1.6.0","html_url":"https://evil.example/x"}`, "v1.5.0", "1.6.0", false},
		{"http error", 403, `{}`, "v1.5.0", "", true},
		{"bad json", 200, `nope`, "v1.5.0", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rel, err := FetchLatestRelease(context.Background(), nil, releaseServer(t, c.status, c.body), c.current)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			got := ""
			if rel != nil {
				got = rel.Version
			}
			if got != c.want {
				t.Fatalf("version = %q, want %q", got, c.want)
			}
			if rel != nil && rel.URL[:19] != "https://github.com/" {
				t.Fatalf("url = %q", rel.URL)
			}
		})
	}
}
