package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPI_MergeTag(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	// Create two tags
	src, err := app.CreateTag("api-src")
	if err != nil {
		t.Fatalf("CreateTag src failed: %v", err)
	}
	dst, err := app.CreateTag("api-dst")
	if err != nil {
		t.Fatalf("CreateTag dst failed: %v", err)
	}

	// Create an admin API key for testing
	testKey := "test-admin-key-12345"
	hash := sha256.Sum256([]byte(testKey))
	keyHash := hex.EncodeToString(hash[:])
	_, err = app.db.Exec(
		`INSERT INTO api_keys (name, key_hash, key_prefix, role, is_revoked) VALUES (?, ?, ?, ?, ?)`,
		"test-key", keyHash, "test-", "admin", false,
	)
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	// Create an APIManager with the app
	am := &APIManager{app: app}

	// Build the mux with routes (inline the mux building logic)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/tags/{id}/merge", am.authMiddleware(am.requireRole("admin", am.handleMergeTag)))

	// Also add the auth and CORS middleware
	handler := am.corsMiddleware(mux)

	// Create the request
	body := map[string]int64{"dest_id": dst.ID}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/tags/%d/merge", src.ID), bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", testKey))

	// Record the response
	rec := httptest.NewRecorder()

	// Serve through the mux (which will handle routing and auth)
	handler.ServeHTTP(rec, req)

	// Verify response status
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	// Verify source tag is deleted
	var count int
	err = app.db.QueryRow(`SELECT COUNT(*) FROM tags WHERE id = ?`, src.ID).Scan(&count)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("source tag should be deleted, but found %d", count)
	}

	// Verify destination tag still exists
	err = app.db.QueryRow(`SELECT COUNT(*) FROM tags WHERE id = ?`, dst.ID).Scan(&count)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("destination tag should exist, but found %d", count)
	}
}
