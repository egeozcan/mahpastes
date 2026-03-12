package main

import (
	"crypto/subtle"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

const maxUploadSize = 10 * 1024 * 1024 // 10 MB

// validateSubtagPath validates a relative tag path and returns the full tag name.
// An empty relativeTag returns the servedTagName unchanged.
// Rejects "..", ".", empty segments, and "_api" segments.
func validateSubtagPath(servedTagName, relativeTag string) (string, error) {
	relativeTag = strings.TrimSpace(relativeTag)
	if relativeTag == "" {
		return servedTagName, nil
	}

	segments := strings.Split(relativeTag, "/")
	for _, seg := range segments {
		if seg == "" {
			return "", fmt.Errorf("invalid tag path: empty segment")
		}
		if seg == ".." || seg == "." {
			return "", fmt.Errorf("invalid tag path: %q not allowed", seg)
		}
		if seg == "_api" {
			return "", fmt.Errorf("invalid tag path: '_api' is a reserved segment")
		}
	}

	return servedTagName + "/" + relativeTag, nil
}

// handleFileUpload handles POST /_api/_upload for file uploads via the serve API.
func (sm *ServeManager) handleFileUpload(w http.ResponseWriter, r *http.Request, ts *tagServer) {
	// CORS preflight.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Only POST is allowed.
	if r.Method != http.MethodPost {
		jsonAPIError(w, http.StatusMethodNotAllowed, "method not allowed: use POST")
		return
	}

	// Must be readwrite mode.
	if ts.apiAccess != "readwrite" {
		if ts.apiAccess == "read" {
			jsonAPIError(w, http.StatusForbidden, "forbidden: API is read-only")
		} else {
			http.NotFound(w, r)
		}
		return
	}

	// Validate cookie authentication.
	cookie, err := r.Cookie("_mp_serve_key")
	if err != nil || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(ts.serveKey)) != 1 {
		jsonAPIError(w, http.StatusUnauthorized, "unauthorized: missing or invalid serve key")
		return
	}

	// Parse multipart form with size limit.
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		jsonAPIError(w, http.StatusRequestEntityTooLarge, "file too large (max 10 MB)")
		return
	}

	// Extract the file.
	file, header, err := r.FormFile("file")
	if err != nil {
		jsonAPIError(w, http.StatusBadRequest, "missing required 'file' field")
		return
	}
	defer file.Close()

	// Read file data (enforce size limit).
	data, err := io.ReadAll(io.LimitReader(file, maxUploadSize+1))
	if err != nil {
		jsonAPIError(w, http.StatusInternalServerError, "failed to read file data")
		return
	}
	if int64(len(data)) > maxUploadSize {
		jsonAPIError(w, http.StatusRequestEntityTooLarge, "file too large (max 10 MB)")
		return
	}

	// Determine content type: form field > multipart header > sniffing.
	contentType := r.FormValue("content_type")
	if contentType == "" {
		contentType = header.Header.Get("Content-Type")
	}
	if contentType == "" || contentType == "application/octet-stream" || contentType == "text/plain" {
		if contentType == "text/plain" || contentType == "" {
			trimmed := strings.TrimSpace(string(data))
			if strings.HasPrefix(trimmed, "<!DOCTYPE html") || strings.HasPrefix(trimmed, "<!doctype html") {
				contentType = "text/html"
			} else if isJSON(trimmed) {
				contentType = "application/json"
			} else if contentType == "" {
				contentType = "application/octet-stream"
			}
		}
	}

	// Resolve target tag.
	relativeTag := r.FormValue("tag")
	fullTagName, err := validateSubtagPath(ts.tagName, relativeTag)
	if err != nil {
		jsonAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Resolve or create the target tag.
	var targetTagID int64
	if fullTagName == ts.tagName {
		targetTagID = ts.tagID
	} else {
		tag, err := sm.app.CreateTag(fullTagName)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE constraint") || strings.Contains(err.Error(), "already exists") {
				var id int64
				lookupErr := sm.app.db.QueryRow("SELECT id FROM tags WHERE name = ?", fullTagName).Scan(&id)
				if lookupErr != nil {
					jsonAPIError(w, http.StatusInternalServerError, fmt.Sprintf("failed to resolve tag: %v", lookupErr))
					return
				}
				targetTagID = id
			} else {
				jsonAPIError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create tag: %v", err))
				return
			}
		} else {
			targetTagID = tag.ID
		}
	}

	// Insert clip.
	filename := header.Filename
	contentHash := computeContentHash(data)
	result, err := sm.app.db.Exec(
		"INSERT INTO clips (content_type, data, filename, content_hash) VALUES (?, ?, ?, ?)",
		contentType, data, filename, contentHash,
	)
	if err != nil {
		jsonAPIError(w, http.StatusInternalServerError, fmt.Sprintf("failed to insert clip: %v", err))
		return
	}
	clipID, _ := result.LastInsertId()

	// Tag the clip.
	if err := sm.app.AddTagToClip(clipID, targetTagID); err != nil {
		log.Printf("serve upload: failed to tag clip %d with tag %d: %v", clipID, targetTagID, err)
	}

	// Emit plugin event.
	if sm.app.pluginManager != nil {
		sm.app.pluginManager.EmitEvent("clip:created", map[string]interface{}{
			"id":           clipID,
			"content_type": contentType,
			"filename":     filename,
		})
	}

	// Return success.
	jsonAPIResponse(w, http.StatusCreated, map[string]interface{}{
		"id":           clipID,
		"filename":     filename,
		"content_type": contentType,
		"tag":          fullTagName,
		"tag_id":       targetTagID,
	})
}
