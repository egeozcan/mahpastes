package main

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// handleJSONAPI is the main entry point for /_api/* requests on a tag server.
func (sm *ServeManager) handleJSONAPI(w http.ResponseWriter, r *http.Request, ts *tagServer) {
	// CORS preflight for the JSON API.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Accept")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// If API access is disabled, act as if the endpoint doesn't exist.
	if ts.apiAccess == "none" {
		http.NotFound(w, r)
		return
	}

	// Validate cookie authentication.
	cookie, err := r.Cookie("_mp_serve_key")
	if err != nil || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(ts.serveKey)) != 1 {
		jsonAPIError(w, http.StatusUnauthorized, "unauthorized: missing or invalid serve key")
		return
	}

	// Read-only mode: reject non-GET methods.
	if ts.apiAccess == "read" && r.Method != http.MethodGet {
		jsonAPIError(w, http.StatusForbidden, "forbidden: API is read-only")
		return
	}

	// Parse path: /_api/{clipFilename}/{jsonPath...}
	apiPath := strings.TrimPrefix(r.URL.Path, "/_api/")
	apiPath = strings.TrimPrefix(apiPath, "/_api")
	apiPath = strings.TrimPrefix(apiPath, "/")

	if apiPath == "" {
		jsonAPIError(w, http.StatusBadRequest, "missing clip filename")
		return
	}

	// First segment is the clip stem, rest is the JSON path.
	// The stem maps to a clip named "{stem}.json" in the tag.
	parts := strings.SplitN(apiPath, "/", 2)
	clipFilename := parts[0] + ".json"
	jsonPath := ""
	if len(parts) > 1 {
		jsonPath = parts[1]
	}
	// Remove trailing slash from jsonPath.
	jsonPath = strings.TrimSuffix(jsonPath, "/")

	switch r.Method {
	case http.MethodGet:
		sm.handleJSONGet(w, r, ts, clipFilename, jsonPath)
	case http.MethodPost:
		sm.handleJSONPost(w, r, ts, clipFilename, jsonPath)
	case http.MethodPut:
		sm.handleJSONPut(w, r, ts, clipFilename, jsonPath)
	case http.MethodPatch:
		sm.handleJSONPatch(w, r, ts, clipFilename, jsonPath)
	case http.MethodDelete:
		sm.handleJSONDelete(w, r, ts, clipFilename, jsonPath)
	default:
		jsonAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleJSONGet reads a JSON clip and returns the value at the given path.
func (sm *ServeManager) handleJSONGet(w http.ResponseWriter, r *http.Request, ts *tagServer, clipFilename, jsonPath string) {
	data, err := sm.readJSONClip(ts.tagID, clipFilename)
	if err != nil {
		jsonAPIError(w, http.StatusNotFound, fmt.Sprintf("clip not found: %s", clipFilename))
		return
	}

	if jsonPath == "" {
		jsonAPIResponse(w, http.StatusOK, data)
		return
	}

	value, err := navigateJSON(data, jsonPath)
	if err != nil {
		jsonAPIError(w, http.StatusNotFound, err.Error())
		return
	}

	jsonAPIResponse(w, http.StatusOK, value)
}

// handleJSONPost appends an element to an array at the given path, auto-assigning an id.
func (sm *ServeManager) handleJSONPost(w http.ResponseWriter, r *http.Request, ts *tagServer, clipFilename, jsonPath string) {
	body, err := readJSONBody(r)
	if err != nil {
		jsonAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Body must be an object.
	obj, ok := body.(map[string]interface{})
	if !ok {
		jsonAPIError(w, http.StatusBadRequest, "request body must be a JSON object")
		return
	}

	mu := getClipMutex(ts, clipFilename)
	mu.Lock()
	defer mu.Unlock()

	data, err := sm.readJSONClip(ts.tagID, clipFilename)
	if err != nil {
		jsonAPIError(w, http.StatusNotFound, fmt.Sprintf("clip not found: %s", clipFilename))
		return
	}

	// Navigate to the target (must be an array).
	var target interface{}
	if jsonPath == "" {
		target = data
	} else {
		target, err = navigateJSON(data, jsonPath)
		if err != nil {
			jsonAPIError(w, http.StatusNotFound, err.Error())
			return
		}
	}

	arr, ok := target.([]interface{})
	if !ok {
		jsonAPIError(w, http.StatusBadRequest, "target must be an array")
		return
	}

	// Auto-increment id.
	newID := nextID(arr)
	obj["id"] = newID

	// Append the new element.
	arr = append(arr, obj)

	// Write back.
	if jsonPath == "" {
		data = arr
	} else {
		if err := setJSON(&data, jsonPath, arr); err != nil {
			jsonAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if err := sm.writeJSONClip(ts.tagID, clipFilename, data); err != nil {
		jsonAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	jsonAPIResponse(w, http.StatusCreated, obj)
}

// handleJSONPut replaces a value at the given path (upsert).
func (sm *ServeManager) handleJSONPut(w http.ResponseWriter, r *http.Request, ts *tagServer, clipFilename, jsonPath string) {
	body, err := readJSONBody(r)
	if err != nil {
		jsonAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	mu := getClipMutex(ts, clipFilename)
	mu.Lock()
	defer mu.Unlock()

	data, err := sm.readJSONClip(ts.tagID, clipFilename)
	if err != nil {
		jsonAPIError(w, http.StatusNotFound, fmt.Sprintf("clip not found: %s", clipFilename))
		return
	}

	// If jsonPath is empty, replace entire clip content.
	if jsonPath == "" {
		data = body
	} else {
		if err := setJSON(&data, jsonPath, body); err != nil {
			jsonAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if err := sm.writeJSONClip(ts.tagID, clipFilename, data); err != nil {
		jsonAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	jsonAPIResponse(w, http.StatusOK, body)
}

// handleJSONPatch merges an object into the value at the given path (RFC 7396).
func (sm *ServeManager) handleJSONPatch(w http.ResponseWriter, r *http.Request, ts *tagServer, clipFilename, jsonPath string) {
	body, err := readJSONBody(r)
	if err != nil {
		jsonAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Body must be an object.
	patch, ok := body.(map[string]interface{})
	if !ok {
		jsonAPIError(w, http.StatusBadRequest, "request body must be a JSON object")
		return
	}

	mu := getClipMutex(ts, clipFilename)
	mu.Lock()
	defer mu.Unlock()

	data, err := sm.readJSONClip(ts.tagID, clipFilename)
	if err != nil {
		jsonAPIError(w, http.StatusNotFound, fmt.Sprintf("clip not found: %s", clipFilename))
		return
	}

	// Navigate to the target (must be an object).
	var target interface{}
	if jsonPath == "" {
		target = data
	} else {
		target, err = navigateJSON(data, jsonPath)
		if err != nil {
			jsonAPIError(w, http.StatusNotFound, err.Error())
			return
		}
	}

	obj, ok := target.(map[string]interface{})
	if !ok {
		jsonAPIError(w, http.StatusBadRequest, "target must be an object")
		return
	}

	// RFC 7396 merge: recurse into nested objects, null deletes keys.
	mergePatch(obj, patch)

	// Write back.
	if jsonPath == "" {
		data = obj
	} else {
		if err := setJSON(&data, jsonPath, obj); err != nil {
			jsonAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if err := sm.writeJSONClip(ts.tagID, clipFilename, data); err != nil {
		jsonAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	jsonAPIResponse(w, http.StatusOK, obj)
}

// handleJSONDelete removes a key from an object or an element from an array by id.
func (sm *ServeManager) handleJSONDelete(w http.ResponseWriter, r *http.Request, ts *tagServer, clipFilename, jsonPath string) {
	if jsonPath == "" {
		jsonAPIError(w, http.StatusBadRequest, "cannot delete root — use PUT to replace")
		return
	}

	mu := getClipMutex(ts, clipFilename)
	mu.Lock()
	defer mu.Unlock()

	data, err := sm.readJSONClip(ts.tagID, clipFilename)
	if err != nil {
		jsonAPIError(w, http.StatusNotFound, fmt.Sprintf("clip not found: %s", clipFilename))
		return
	}

	if err := deleteJSON(&data, jsonPath); err != nil {
		jsonAPIError(w, http.StatusNotFound, err.Error())
		return
	}

	if err := sm.writeJSONClip(ts.tagID, clipFilename, data); err != nil {
		jsonAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- Database helpers ---

// resolveClipID finds the clip ID for a served filename by using the same
// buildFileList alias map that the normal file handler uses. This correctly
// handles duplicate filenames (e.g. "users.json" vs "users (2).json").
func (sm *ServeManager) resolveClipID(tagID int64, filename string) (int64, error) {
	files, err := sm.buildFileList(tagID)
	if err != nil {
		return 0, err
	}
	for _, f := range files {
		if f.filename == filename {
			return f.clipID, nil
		}
	}
	return 0, fmt.Errorf("clip not found")
}

// readJSONClip queries the DB for a clip's data by resolved clip ID, and unmarshals it.
func (sm *ServeManager) readJSONClip(tagID int64, filename string) (interface{}, error) {
	clipID, err := sm.resolveClipID(tagID, filename)
	if err != nil {
		return nil, err
	}

	var data []byte
	err = sm.app.db.QueryRow(`SELECT data FROM clips WHERE id = ?`, clipID).Scan(&data)
	if err != nil {
		return nil, fmt.Errorf("clip not found")
	}

	var result interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("clip data is not valid JSON: %w", err)
	}
	return result, nil
}

// writeJSONClip marshals data and updates the clip by resolved clip ID.
func (sm *ServeManager) writeJSONClip(tagID int64, filename string, data interface{}) error {
	clipID, err := sm.resolveClipID(tagID, filename)
	if err != nil {
		return err
	}

	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	_, err = sm.app.db.Exec(`UPDATE clips SET data = ? WHERE id = ?`, raw, clipID)
	if err != nil {
		return fmt.Errorf("failed to write clip: %w", err)
	}
	return nil
}

// getClipMutex returns the per-clip mutex for the given filename, creating one if needed.
func getClipMutex(ts *tagServer, filename string) *sync.Mutex {
	ts.clipMu.Lock()
	defer ts.clipMu.Unlock()

	mu, ok := ts.clipMutexes[filename]
	if !ok {
		mu = &sync.Mutex{}
		ts.clipMutexes[filename] = mu
	}
	return mu
}

// --- JSON navigation helpers ---

// navigateJSON walks a slash-separated path through nested maps and arrays.
// For maps: key lookup. For arrays: find element where "id" field matches the numeric segment.
func navigateJSON(data interface{}, path string) (interface{}, error) {
	if path == "" {
		return data, nil
	}

	segments := strings.Split(path, "/")
	current := data

	for _, seg := range segments {
		switch v := current.(type) {
		case map[string]interface{}:
			val, ok := v[seg]
			if !ok {
				return nil, fmt.Errorf("key %q not found", seg)
			}
			current = val
		case []interface{}:
			id, ok := toFloat(seg)
			if !ok {
				return nil, fmt.Errorf("array segment %q is not a valid id", seg)
			}
			found := false
			for _, elem := range v {
				if obj, ok := elem.(map[string]interface{}); ok {
					if elemID, ok := toFloat(obj["id"]); ok && elemID == id {
						current = obj
						found = true
						break
					}
				}
			}
			if !found {
				return nil, fmt.Errorf("no element with id %v in array", seg)
			}
		default:
			return nil, fmt.Errorf("cannot navigate into %T at segment %q", current, seg)
		}
	}

	return current, nil
}

// setJSON navigates to the parent of the final path segment and sets the value.
// Creates intermediate objects as needed (upsert behavior).
// For arrays, finds the element by id and replaces its contents.
func setJSON(data *interface{}, path string, value interface{}) error {
	if path == "" {
		*data = value
		return nil
	}

	segments := strings.Split(path, "/")
	finalKey := segments[len(segments)-1]
	parentPath := segments[:len(segments)-1]

	// Navigate to the parent.
	parent := *data
	for i, seg := range parentPath {
		switch v := parent.(type) {
		case map[string]interface{}:
			next, ok := v[seg]
			if !ok {
				// Create intermediate object.
				newObj := make(map[string]interface{})
				v[seg] = newObj
				parent = newObj
			} else {
				parent = next
			}
		case []interface{}:
			id, ok := toFloat(seg)
			if !ok {
				return fmt.Errorf("array segment %q is not a valid id", seg)
			}
			found := false
			for _, elem := range v {
				if obj, ok := elem.(map[string]interface{}); ok {
					if elemID, ok := toFloat(obj["id"]); ok && elemID == id {
						parent = obj
						found = true
						break
					}
				}
			}
			if !found {
				return fmt.Errorf("no element with id %v at path segment %d", seg, i)
			}
		default:
			return fmt.Errorf("cannot navigate into %T at segment %q", parent, seg)
		}
	}

	// Set the value on the parent.
	switch v := parent.(type) {
	case map[string]interface{}:
		v[finalKey] = value
	case []interface{}:
		id, ok := toFloat(finalKey)
		if !ok {
			return fmt.Errorf("array segment %q is not a valid id", finalKey)
		}
		found := false
		for i, elem := range v {
			if obj, ok := elem.(map[string]interface{}); ok {
				if elemID, ok := toFloat(obj["id"]); ok && elemID == id {
					// If value is an object, replace the element's contents.
					if newObj, ok := value.(map[string]interface{}); ok {
						// Preserve the id.
						newObj["id"] = obj["id"]
						v[i] = newObj
					} else {
						v[i] = value
					}
					found = true
					break
				}
			}
		}
		if !found {
			return fmt.Errorf("no element with id %v in array", finalKey)
		}
	default:
		return fmt.Errorf("cannot set value on %T", parent)
	}

	return nil
}

// deleteJSON navigates to the parent of the final path segment and removes the key/element.
func deleteJSON(data *interface{}, path string) error {
	if path == "" {
		return fmt.Errorf("cannot delete root")
	}

	segments := strings.Split(path, "/")
	finalKey := segments[len(segments)-1]
	parentPath := segments[:len(segments)-1]

	// Navigate to the parent.
	var parent interface{}
	if len(parentPath) == 0 {
		parent = *data
	} else {
		var err error
		parent, err = navigateJSON(*data, strings.Join(parentPath, "/"))
		if err != nil {
			return err
		}
	}

	switch v := parent.(type) {
	case map[string]interface{}:
		if _, ok := v[finalKey]; !ok {
			return fmt.Errorf("key %q not found", finalKey)
		}
		delete(v, finalKey)
	case []interface{}:
		id, ok := toFloat(finalKey)
		if !ok {
			return fmt.Errorf("array segment %q is not a valid id", finalKey)
		}
		found := false
		for i, elem := range v {
			if obj, ok := elem.(map[string]interface{}); ok {
				if elemID, ok := toFloat(obj["id"]); ok && elemID == id {
					// Splice the element out using a clean copy to avoid
				// corrupting the original backing array.
					newArr := make([]interface{}, 0, len(v)-1)
					newArr = append(newArr, v[:i]...)
					newArr = append(newArr, v[i+1:]...)
					// Write the modified slice back to the parent via setJSON.
					if len(parentPath) == 0 {
						*data = newArr
					} else {
						if err := setJSON(data, strings.Join(parentPath, "/"), newArr); err != nil {
							return err
						}
					}
					found = true
					break
				}
			}
		}
		if !found {
			return fmt.Errorf("no element with id %v in array", finalKey)
		}
	default:
		return fmt.Errorf("cannot delete from %T", parent)
	}

	return nil
}

// mergePatch applies RFC 7396 JSON Merge Patch recursively.
// null values delete keys; nested objects are merged recursively; all else overwrites.
func mergePatch(target, patch map[string]interface{}) {
	for k, v := range patch {
		if v == nil {
			delete(target, k)
			continue
		}
		// If both sides are objects, recurse.
		if patchObj, ok := v.(map[string]interface{}); ok {
			if targetObj, ok := target[k].(map[string]interface{}); ok {
				mergePatch(targetObj, patchObj)
				continue
			}
		}
		target[k] = v
	}
}

// nextID scans an array of objects for the maximum "id" field and returns max+1.
func nextID(arr []interface{}) float64 {
	var maxID float64
	for _, elem := range arr {
		if obj, ok := elem.(map[string]interface{}); ok {
			if id, ok := toFloat(obj["id"]); ok && id > maxID {
				maxID = id
			}
		}
	}
	return maxID + 1
}

// toFloat converts a value to float64. Handles float64 (JSON numbers), string numeric values, etc.
func toFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case string:
		var f float64
		_, err := fmt.Sscanf(n, "%f", &f)
		return f, err == nil
	default:
		return 0, false
	}
}

// readJSONBody reads and unmarshals the request body (10MB limit).
func readJSONBody(r *http.Request) (interface{}, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %w", err)
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("empty request body")
	}

	var result interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return result, nil
}

// jsonAPIError writes a JSON error response.
func jsonAPIError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// jsonAPIResponse writes a JSON success response.
func jsonAPIResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
