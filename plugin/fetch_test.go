package plugin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchPluginSource(t *testing.T) {
	pluginSrc := `Plugin = { name = "Test", version = "1.0.0", description = "A test plugin", author = "Tester" }`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(pluginSrc))
	}))
	defer server.Close()

	source, err := FetchPluginSource(server.URL + "/test.lua")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if source != pluginSrc {
		t.Errorf("got %q, want %q", source, pluginSrc)
	}
}

func TestFetchPluginSource_TooLarge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("x", 2*1024*1024)))
	}))
	defer server.Close()

	_, err := FetchPluginSource(server.URL + "/big.lua")
	if err == nil {
		t.Error("expected error for oversized response")
	}
}

func TestFetchPluginSource_InvalidURL(t *testing.T) {
	_, err := FetchPluginSource("ftp://not-http.com/file.lua")
	if err == nil {
		t.Error("expected error for non-HTTP URL")
	}
}

func TestFetchPluginSource_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, err := FetchPluginSource(server.URL + "/missing.lua")
	if err == nil {
		t.Error("expected error for 404 response")
	}
}
