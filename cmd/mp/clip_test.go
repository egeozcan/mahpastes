package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClipListEncodesQuery(t *testing.T) {
	var got map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.Write([]byte(`{"clips":[],"total":0}`))
	}))
	defer srv.Close()
	t.Setenv("MP_API_URL", srv.URL)
	t.Setenv("MP_API_KEY", "k")

	clipListSearch = "beta & gamma+x"
	clipListContentType = "text/plain; charset=utf-8"
	clipListLimit = 5
	defer func() { clipListSearch, clipListContentType, clipListLimit = "", "", 50 }()
	if err := runClipList(clipListCmd, nil); err != nil {
		t.Fatal(err)
	}
	if got["search"][0] != "beta & gamma+x" || got["content_type"][0] != "text/plain; charset=utf-8" || got["limit"][0] != "5" {
		t.Fatalf("query = %v", got)
	}
}

func TestClipMetadataKeyIsPathEscaped(t *testing.T) {
	var gotKey string
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/clips/{id}/metadata/{key}", func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.PathValue("key")
		w.Write([]byte("{}"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Setenv("MP_API_URL", srv.URL)
	t.Setenv("MP_API_KEY", "k")

	key := "a/b c?d#e"
	if err := runClipMetadataSet(clipMetadataSetCmd, []string{"7", key, "v"}); err != nil {
		t.Fatal(err)
	}
	if gotKey != key {
		t.Fatalf("server saw key %q, want %q", gotKey, key)
	}
}
