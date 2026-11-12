package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"go-clipboard/cmd/mp/client"
)

func exitCodeOf(err error) int {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ExitCode()
	}
	return client.ExitError
}

func TestAPIStatusDistinguishesBadKeyFromUnreachable(t *testing.T) {
	var statusHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/status":
			statusHits++
			w.Write([]byte(`{"running":true,"port":44557}`))
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	t.Setenv("MP_API_URL", srv.URL)
	t.Setenv("MP_API_KEY", "bad")

	err := runAPIStatus(apiStatusCmd, nil)
	if statusHits != 1 {
		t.Fatalf("status endpoint hits = %d, want 1", statusHits)
	}
	if code := exitCodeOf(err); code != client.ExitAuth {
		t.Fatalf("bad key: exit code = %d (%v), want %d", code, err, client.ExitAuth)
	}

	srv.Close()
	err = runAPIStatus(apiStatusCmd, nil)
	if code := exitCodeOf(err); code != client.ExitConnection {
		t.Fatalf("unreachable: exit code = %d (%v), want %d", code, err, client.ExitConnection)
	}
}

func TestBulkDownloadReportsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("missing auth header")
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	t.Setenv("MP_API_URL", srv.URL)
	t.Setenv("MP_API_KEY", "k")
	c, err := client.New()
	if err != nil {
		t.Fatal(err)
	}
	err = downloadBulkClips(c, []int64{1, 2}, filepath.Join(t.TempDir(), "out.zip"))
	if code := exitCodeOf(err); code != client.ExitAuth {
		t.Fatalf("exit code = %d (%v), want %d", code, err, client.ExitAuth)
	}
}
