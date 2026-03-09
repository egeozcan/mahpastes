# JSON API for Served Tags — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Enable HTML clips served from a tag to read/write JSON clips under the same tag via REST semantics at the `/_api` prefix.

**Architecture:** Extend `serve_manager.go`'s per-tag HTTP handler with a new `/_api` route that maps URL paths to JSON clip content. Cookie-based auth (HTTP-only, SameSite=Strict) gates access. A per-tag `apiAccess` setting (none/read/readwrite) controls permissions.

**Tech Stack:** Go stdlib `net/http`, `encoding/json`, `crypto/rand`; Vanilla JS frontend; Playwright e2e tests.

---

### Task 1: Reserve `_api` in Tag Names

Block tag creation where any path segment is `_api`.

**Files:**
- Modify: `app.go:1066-1073` (CreateTag validation)
- Test: `e2e/tests/tags/tag-crud.spec.ts` (add reservation test)

**Step 1: Write the failing e2e test**

Add to the end of the existing tag CRUD test file:

```typescript
test('should reject tag names containing _api segment', async ({ app }) => {
  // Direct _api
  const result1 = await app.page.evaluate(async () => {
    try {
      // @ts-ignore
      await window.go.main.App.CreateTag('_api');
      return { error: null };
    } catch (e: any) {
      return { error: e.message || String(e) };
    }
  });
  expect(result1.error).toContain('reserved');

  // Nested _api
  const result2 = await app.page.evaluate(async () => {
    try {
      // @ts-ignore
      await window.go.main.App.CreateTag('work/_api/stuff');
      return { error: null };
    } catch (e: any) {
      return { error: e.message || String(e) };
    }
  });
  expect(result2.error).toContain('reserved');

  // _api as suffix segment
  const result3 = await app.page.evaluate(async () => {
    try {
      // @ts-ignore
      await window.go.main.App.CreateTag('docs/_api');
      return { error: null };
    } catch (e: any) {
      return { error: e.message || String(e) };
    }
  });
  expect(result3.error).toContain('reserved');

  // _api as substring should be allowed (e.g., "my_api_stuff")
  // @ts-ignore
  await app.createTag('my_api_stuff');
  const tags = await app.page.evaluate(async () => {
    // @ts-ignore
    return await window.go.main.App.GetTags();
  });
  expect(tags.some((t: any) => t.name === 'my_api_stuff')).toBe(true);
});
```

**Step 2: Run test to verify it fails**

Run: `cd e2e && npx playwright test tests/tags/tag-crud.spec.ts -g "should reject tag names containing _api segment" --reporter=list`
Expected: FAIL — CreateTag does not reject `_api` yet.

**Step 3: Add `_api` validation to CreateTag**

In `app.go`, add validation after the length check (after line 1073), before the transaction begins:

```go
	// Reserve "_api" as a path segment — used by tag serve JSON API.
	for _, seg := range strings.Split(name, "/") {
		if seg == "_api" {
			return nil, fmt.Errorf("tag name contains reserved segment '_api'")
		}
	}
```

Insert this block between line 1073 (`}` closing the length check) and line 1075 (`// Use transaction`).

**Step 4: Run test to verify it passes**

Run: `cd e2e && npx playwright test tests/tags/tag-crud.spec.ts -g "should reject tag names containing _api segment" --reporter=list`
Expected: PASS

**Step 5: Commit**

```bash
git add app.go e2e/tests/tags/tag-crud.spec.ts
git commit -m "feat: reserve _api as tag name segment for serve JSON API"
```

---

### Task 2: Add `apiAccess` Parameter to StartServing

Thread the new `apiAccess` param through ServeService → ServeManager → tagServer struct. Generate and store cookie token.

**Files:**
- Modify: `serve_manager.go:32-39` (tagServer struct), `serve_manager.go:410-471` (StartServing method), `serve_manager.go:20-29` (ServeInfo struct)
- Modify: `serve_service.go:16-21` (StartServing wrapper)
- Modify: `api_manager.go:1146-1204` (handleStartServer)

**Step 1: Update `tagServer` struct in `serve_manager.go`**

Add three fields to the `tagServer` struct (after line 38, before the closing `}`):

```go
type tagServer struct {
	tagID        int64
	tagName      string
	port         int
	bindAll      bool
	server       *http.Server
	requestCount int64 // accessed atomically
	apiAccess    string // "none", "read", "readwrite"
	serveKey     string // random token for cookie auth
	clipMutexes  map[string]*sync.Mutex // per-clip filename mutex
	clipMu       sync.Mutex            // protects clipMutexes map
}
```

**Step 2: Add `ApiAccess` field to `ServeInfo` struct**

Update `ServeInfo` in `serve_manager.go` (lines 20-29):

```go
type ServeInfo struct {
	TagID        int64  `json:"tag_id"`
	TagName      string `json:"tag_name"`
	Port         int    `json:"port"`
	BindAll      bool   `json:"bind_all"`
	URL          string `json:"url"`
	Running      bool   `json:"running"`
	RequestCount int64  `json:"request_count"`
	ApiAccess    string `json:"api_access"`
}
```

**Step 3: Add `crypto/rand` import**

Add `"crypto/rand"` and `"encoding/hex"` to the import block in `serve_manager.go` (line 4 area).

**Step 4: Update `ServeManager.StartServing` to accept and use `apiAccess`**

Change the signature at line 411 and update the method body:

```go
func (sm *ServeManager) StartServing(tagID int64, port int, bindAll bool, apiAccess string) (ServeInfo, error) {
```

After the `ts := &tagServer{` block (around lines 432-437), add the new fields:

```go
	// Default to "none" if empty or invalid.
	if apiAccess != "read" && apiAccess != "readwrite" {
		apiAccess = "none"
	}

	// Generate a random cookie token for this server instance.
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return ServeInfo{}, fmt.Errorf("failed to generate serve key: %w", err)
	}

	ts := &tagServer{
		tagID:       tagID,
		tagName:     tagName,
		port:        port,
		bindAll:     bindAll,
		apiAccess:   apiAccess,
		serveKey:    hex.EncodeToString(tokenBytes),
		clipMutexes: make(map[string]*sync.Mutex),
	}
```

Update the return `ServeInfo` (around line 462) to include `ApiAccess`:

```go
	return ServeInfo{
		TagID:        tagID,
		TagName:      tagName,
		Port:         actualPort,
		BindAll:      bindAll,
		URL:          displayServerURL(actualPort),
		Running:      true,
		RequestCount: 0,
		ApiAccess:    apiAccess,
	}, nil
```

Also update `GetStatus` (around line 503) to include `ApiAccess: ts.apiAccess` in the ServeInfo.

**Step 5: Update `ServeService.StartServing` in `serve_service.go`**

Change the wrapper at lines 16-21:

```go
func (s *ServeService) StartServing(tagID int64, port int, bindAll bool, apiAccess string) (ServeInfo, error) {
	if s.app.serveManager == nil {
		return ServeInfo{}, fmt.Errorf("serve manager not initialized")
	}
	return s.app.serveManager.StartServing(tagID, port, bindAll, apiAccess)
}
```

**Step 6: Update `handleStartServer` in `api_manager.go`**

Update the body struct (around line 1149) to include `ApiAccess`:

```go
	var body struct {
		TagID     int64  `json:"tag_id"`
		Port      int    `json:"port"`
		BindAll   bool   `json:"bind_all"`
		ApiAccess string `json:"api_access"`
	}
```

Update the call at line 1186:

```go
	info, err := am.app.serveManager.StartServing(body.TagID, port, body.BindAll, body.ApiAccess)
```

**Step 7: Verify it compiles**

Run: `cd /Users/egecan/Code/mahpastes && ~/go/bin/wails build 2>&1 | head -30`
Expected: Build succeeds (or only frontend binding warnings — bindings will be regenerated in Task 4).

**Step 8: Commit**

```bash
git add serve_manager.go serve_service.go api_manager.go
git commit -m "feat: add apiAccess parameter to StartServing with cookie token generation"
```

---

### Task 3: Implement the `/_api` JSON Handler

The core logic: route `/_api` requests, navigate JSON paths, perform CRUD operations.

**Files:**
- Create: `serve_json_api.go` (new file — all JSON API handler logic)
- Modify: `serve_manager.go:182-296` (makeHandler — add `/_api` route before existing logic)

**Step 1: Create `serve_json_api.go` with the handler and helpers**

Create a new file `/Users/egecan/Code/mahpastes/serve_json_api.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// handleJSONAPI processes /_api/* requests for the tag server.
// It maps URL paths to JSON clip content and supports GET/POST/PUT/PATCH/DELETE.
func (sm *ServeManager) handleJSONAPI(w http.ResponseWriter, r *http.Request, ts *tagServer) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Handle CORS preflight for all methods.
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Accept")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Check apiAccess permission level.
	if ts.apiAccess == "none" {
		http.NotFound(w, r)
		return
	}

	// Validate cookie auth.
	cookie, err := r.Cookie("_mp_serve_key")
	if err != nil || cookie.Value != ts.serveKey {
		sm.jsonAPIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Block writes in read-only mode.
	if ts.apiAccess == "read" && r.Method != http.MethodGet {
		sm.jsonAPIError(w, http.StatusForbidden, "read-only access")
		return
	}

	// Parse the path: /_api/{clipStem}/{jsonPath...}
	apiPath := strings.TrimPrefix(r.URL.Path, "/_api/")
	apiPath = strings.TrimSuffix(apiPath, "/")
	if apiPath == "" {
		sm.jsonAPIError(w, http.StatusBadRequest, "missing clip name in path")
		return
	}

	segments := strings.SplitN(apiPath, "/", 2)
	clipStem := segments[0]
	jsonPath := ""
	if len(segments) > 1 {
		jsonPath = segments[1]
	}

	clipFilename := clipStem + ".json"

	switch r.Method {
	case http.MethodGet:
		sm.handleJSONGet(w, ts, clipFilename, jsonPath)
	case http.MethodPost:
		sm.handleJSONPost(w, r, ts, clipFilename, jsonPath)
	case http.MethodPut:
		sm.handleJSONPut(w, r, ts, clipFilename, jsonPath)
	case http.MethodPatch:
		sm.handleJSONPatch(w, r, ts, clipFilename, jsonPath)
	case http.MethodDelete:
		sm.handleJSONDelete(w, ts, clipFilename, jsonPath)
	default:
		sm.jsonAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// --- GET ---

func (sm *ServeManager) handleJSONGet(w http.ResponseWriter, ts *tagServer, clipFilename, jsonPath string) {
	data, err := sm.readJSONClip(ts.tagID, clipFilename)
	if err != nil {
		sm.jsonAPIError(w, http.StatusNotFound, fmt.Sprintf("clip %q not found", clipFilename))
		return
	}

	result, err := navigateJSON(data, jsonPath)
	if err != nil {
		sm.jsonAPIError(w, http.StatusNotFound, err.Error())
		return
	}

	sm.jsonAPIResponse(w, http.StatusOK, result)
}

// --- POST ---

func (sm *ServeManager) handleJSONPost(w http.ResponseWriter, r *http.Request, ts *tagServer, clipFilename, jsonPath string) {
	body, err := readJSONBody(r)
	if err != nil {
		sm.jsonAPIError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	mu := sm.getClipMutex(ts, clipFilename)
	mu.Lock()
	defer mu.Unlock()

	data, err := sm.readJSONClip(ts.tagID, clipFilename)
	if err != nil {
		sm.jsonAPIError(w, http.StatusNotFound, fmt.Sprintf("clip %q not found", clipFilename))
		return
	}

	// Navigate to the target (must be an array).
	target, err := navigateJSON(data, jsonPath)
	if err != nil {
		sm.jsonAPIError(w, http.StatusNotFound, err.Error())
		return
	}

	arr, ok := target.([]interface{})
	if !ok {
		sm.jsonAPIError(w, http.StatusBadRequest, "POST target is not an array")
		return
	}

	// Ensure body is a map so we can assign an ID.
	bodyMap, ok := body.(map[string]interface{})
	if !ok {
		sm.jsonAPIError(w, http.StatusBadRequest, "POST body must be a JSON object")
		return
	}

	// Auto-increment ID.
	newID := nextID(arr)
	bodyMap["id"] = newID

	// Append to array.
	arr = append(arr, bodyMap)

	// Set the updated array back into the root data.
	if err := setJSON(data, jsonPath, arr); err != nil {
		sm.jsonAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := sm.writeJSONClip(ts.tagID, clipFilename, data); err != nil {
		sm.jsonAPIError(w, http.StatusInternalServerError, "failed to save")
		return
	}

	sm.jsonAPIResponse(w, http.StatusCreated, bodyMap)
}

// --- PUT ---

func (sm *ServeManager) handleJSONPut(w http.ResponseWriter, r *http.Request, ts *tagServer, clipFilename, jsonPath string) {
	body, err := readJSONBody(r)
	if err != nil {
		sm.jsonAPIError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	mu := sm.getClipMutex(ts, clipFilename)
	mu.Lock()
	defer mu.Unlock()

	data, err := sm.readJSONClip(ts.tagID, clipFilename)
	if err != nil {
		sm.jsonAPIError(w, http.StatusNotFound, fmt.Sprintf("clip %q not found", clipFilename))
		return
	}

	if jsonPath == "" {
		// Replace the entire clip content.
		data = body
	} else {
		if err := setJSON(data, jsonPath, body); err != nil {
			sm.jsonAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	if err := sm.writeJSONClip(ts.tagID, clipFilename, data); err != nil {
		sm.jsonAPIError(w, http.StatusInternalServerError, "failed to save")
		return
	}

	sm.jsonAPIResponse(w, http.StatusOK, body)
}

// --- PATCH ---

func (sm *ServeManager) handleJSONPatch(w http.ResponseWriter, r *http.Request, ts *tagServer, clipFilename, jsonPath string) {
	patch, err := readJSONBody(r)
	if err != nil {
		sm.jsonAPIError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	patchMap, ok := patch.(map[string]interface{})
	if !ok {
		sm.jsonAPIError(w, http.StatusBadRequest, "PATCH body must be a JSON object")
		return
	}

	mu := sm.getClipMutex(ts, clipFilename)
	mu.Lock()
	defer mu.Unlock()

	data, err := sm.readJSONClip(ts.tagID, clipFilename)
	if err != nil {
		sm.jsonAPIError(w, http.StatusNotFound, fmt.Sprintf("clip %q not found", clipFilename))
		return
	}

	target, err := navigateJSON(data, jsonPath)
	if err != nil {
		sm.jsonAPIError(w, http.StatusNotFound, err.Error())
		return
	}

	targetMap, ok := target.(map[string]interface{})
	if !ok {
		sm.jsonAPIError(w, http.StatusBadRequest, "PATCH target is not an object")
		return
	}

	// JSON Merge Patch (RFC 7396): merge keys from patch into target.
	for k, v := range patchMap {
		if v == nil {
			delete(targetMap, k)
		} else {
			targetMap[k] = v
		}
	}

	// The targetMap is a reference into data, so data is already updated.
	if err := sm.writeJSONClip(ts.tagID, clipFilename, data); err != nil {
		sm.jsonAPIError(w, http.StatusInternalServerError, "failed to save")
		return
	}

	sm.jsonAPIResponse(w, http.StatusOK, targetMap)
}

// --- DELETE ---

func (sm *ServeManager) handleJSONDelete(w http.ResponseWriter, ts *tagServer, clipFilename, jsonPath string) {
	if jsonPath == "" {
		sm.jsonAPIError(w, http.StatusBadRequest, "cannot DELETE the root — use PUT to replace")
		return
	}

	mu := sm.getClipMutex(ts, clipFilename)
	mu.Lock()
	defer mu.Unlock()

	data, err := sm.readJSONClip(ts.tagID, clipFilename)
	if err != nil {
		sm.jsonAPIError(w, http.StatusNotFound, fmt.Sprintf("clip %q not found", clipFilename))
		return
	}

	if err := deleteJSON(data, jsonPath); err != nil {
		sm.jsonAPIError(w, http.StatusNotFound, err.Error())
		return
	}

	if err := sm.writeJSONClip(ts.tagID, clipFilename, data); err != nil {
		sm.jsonAPIError(w, http.StatusInternalServerError, "failed to save")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- DB helpers ---

// readJSONClip reads a JSON clip's data from the database and unmarshals it.
func (sm *ServeManager) readJSONClip(tagID int64, filename string) (interface{}, error) {
	var data []byte
	err := sm.app.db.QueryRow(`
		SELECT c.data FROM clips c
		JOIN clip_tags ct ON c.id = ct.clip_id
		WHERE ct.tag_id = ? AND c.filename = ? AND c.is_archived = 0
		LIMIT 1
	`, tagID, filename).Scan(&data)
	if err != nil {
		return nil, fmt.Errorf("clip not found")
	}

	var parsed interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("clip is not valid JSON")
	}
	return parsed, nil
}

// writeJSONClip serializes data and writes it back to the clip in the database.
func (sm *ServeManager) writeJSONClip(tagID int64, filename string, data interface{}) error {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	result, err := sm.app.db.Exec(`
		UPDATE clips SET data = ?
		WHERE id = (
			SELECT c.id FROM clips c
			JOIN clip_tags ct ON c.id = ct.clip_id
			WHERE ct.tag_id = ? AND c.filename = ? AND c.is_archived = 0
			LIMIT 1
		)
	`, jsonBytes, tagID, filename)
	if err != nil {
		return fmt.Errorf("failed to update clip: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("clip not found for update")
	}
	return nil
}

// getClipMutex returns (or creates) the mutex for a given clip filename on a tagServer.
func (sm *ServeManager) getClipMutex(ts *tagServer, filename string) *sync.Mutex {
	ts.clipMu.Lock()
	defer ts.clipMu.Unlock()
	if ts.clipMutexes == nil {
		ts.clipMutexes = make(map[string]*sync.Mutex)
	}
	mu, ok := ts.clipMutexes[filename]
	if !ok {
		mu = &sync.Mutex{}
		ts.clipMutexes[filename] = mu
	}
	return mu
}

// --- JSON navigation helpers ---

// navigateJSON traverses a parsed JSON value by slash-separated path.
// For arrays, path segments that are numeric match by the "id" field of objects in the array.
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
			id, err := strconv.ParseFloat(seg, 64)
			if err != nil {
				return nil, fmt.Errorf("array index %q is not a number", seg)
			}
			found := false
			for _, elem := range v {
				if obj, ok := elem.(map[string]interface{}); ok {
					if objID, ok := obj["id"]; ok {
						if toFloat(objID) == id {
							current = obj
							found = true
							break
						}
					}
				}
			}
			if !found {
				return nil, fmt.Errorf("no element with id=%v", seg)
			}
		default:
			return nil, fmt.Errorf("cannot navigate into %T", current)
		}
	}

	return current, nil
}

// setJSON sets a value at a slash-separated path in a parsed JSON structure.
// Creates intermediate objects as needed (upsert). For arrays, matches by id field.
func setJSON(data interface{}, path string, value interface{}) error {
	if path == "" {
		return fmt.Errorf("cannot set root via setJSON — replace data directly")
	}

	segments := strings.Split(path, "/")
	current := data

	for i := 0; i < len(segments)-1; i++ {
		seg := segments[i]
		switch v := current.(type) {
		case map[string]interface{}:
			next, ok := v[seg]
			if !ok {
				// Create intermediate object.
				newObj := make(map[string]interface{})
				v[seg] = newObj
				current = newObj
			} else {
				current = next
			}
		case []interface{}:
			id, err := strconv.ParseFloat(seg, 64)
			if err != nil {
				return fmt.Errorf("array index %q is not a number", seg)
			}
			found := false
			for _, elem := range v {
				if obj, ok := elem.(map[string]interface{}); ok {
					if objID, ok := obj["id"]; ok {
						if toFloat(objID) == id {
							current = obj
							found = true
							break
						}
					}
				}
			}
			if !found {
				return fmt.Errorf("no element with id=%v in array", seg)
			}
		default:
			return fmt.Errorf("cannot navigate into %T at segment %q", current, seg)
		}
	}

	// Set the final segment.
	lastSeg := segments[len(segments)-1]
	switch v := current.(type) {
	case map[string]interface{}:
		v[lastSeg] = value
	case []interface{}:
		id, err := strconv.ParseFloat(lastSeg, 64)
		if err != nil {
			return fmt.Errorf("array index %q is not a number", lastSeg)
		}
		for _, elem := range v {
			if obj, ok := elem.(map[string]interface{}); ok {
				if objID, ok := obj["id"]; ok {
					if toFloat(objID) == id {
						// Replace the entire element.
						if newObj, ok := value.(map[string]interface{}); ok {
							for k := range obj {
								delete(obj, k)
							}
							for k, val := range newObj {
								obj[k] = val
							}
							return nil
						}
						return fmt.Errorf("PUT on array element requires object body")
					}
				}
			}
		}
		return fmt.Errorf("no element with id=%v in array", lastSeg)
	default:
		return fmt.Errorf("cannot set on %T", current)
	}
	return nil
}

// deleteJSON removes a key or array element at a slash-separated path.
func deleteJSON(data interface{}, path string) error {
	if path == "" {
		return fmt.Errorf("cannot delete root")
	}

	segments := strings.Split(path, "/")

	// Navigate to the parent.
	parent := data
	for i := 0; i < len(segments)-1; i++ {
		next, err := navigateJSON(parent, segments[i])
		if err != nil {
			return err
		}
		parent = next
	}

	lastSeg := segments[len(segments)-1]
	switch v := parent.(type) {
	case map[string]interface{}:
		if _, ok := v[lastSeg]; !ok {
			return fmt.Errorf("key %q not found", lastSeg)
		}
		delete(v, lastSeg)
	case []interface{}:
		id, err := strconv.ParseFloat(lastSeg, 64)
		if err != nil {
			return fmt.Errorf("array index %q is not a number", lastSeg)
		}
		found := false
		for i, elem := range v {
			if obj, ok := elem.(map[string]interface{}); ok {
				if objID, ok := obj["id"]; ok {
					if toFloat(objID) == id {
						// We need to modify the parent's reference to the array.
						// Since Go slices are values, we must set back via setJSON on the grandparent.
						// For simplicity, modify in place by shifting.
						copy(v[i:], v[i+1:])
						v[len(v)-1] = nil

						// We need to update the parent slice reference.
						// Navigate to grandparent and set.
						if len(segments) >= 2 {
							grandParentPath := strings.Join(segments[:len(segments)-1], "/")
							newSlice := v[:len(v)-1]
							return setJSONSlice(data, grandParentPath, newSlice)
						}
						found = true
						break
					}
				}
			}
		}
		if !found {
			return fmt.Errorf("no element with id=%v", lastSeg)
		}
	default:
		return fmt.Errorf("cannot delete from %T", parent)
	}
	return nil
}

// setJSONSlice replaces a slice at a given path. Used by deleteJSON for array element removal.
func setJSONSlice(data interface{}, path string, slice []interface{}) error {
	if path == "" {
		return fmt.Errorf("cannot set root slice")
	}

	segments := strings.Split(path, "/")
	current := data

	for i := 0; i < len(segments)-1; i++ {
		seg := segments[i]
		switch v := current.(type) {
		case map[string]interface{}:
			current = v[seg]
		case []interface{}:
			id, _ := strconv.ParseFloat(seg, 64)
			for _, elem := range v {
				if obj, ok := elem.(map[string]interface{}); ok {
					if objID, ok := obj["id"]; ok {
						if toFloat(objID) == id {
							current = obj
							break
						}
					}
				}
			}
		}
	}

	lastSeg := segments[len(segments)-1]
	if m, ok := current.(map[string]interface{}); ok {
		m[lastSeg] = slice
		return nil
	}
	return fmt.Errorf("cannot set slice at path %q", path)
}

// --- Utility helpers ---

// nextID scans an array for the max "id" field and returns max+1.
func nextID(arr []interface{}) float64 {
	var maxID float64
	for _, elem := range arr {
		if obj, ok := elem.(map[string]interface{}); ok {
			if id, ok := obj["id"]; ok {
				if f := toFloat(id); f > maxID {
					maxID = f
				}
			}
		}
	}
	return maxID + 1
}

// toFloat converts a JSON number (float64) or integer-like value to float64.
func toFloat(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}

// readJSONBody reads and parses the request body as JSON.
func readJSONBody(r *http.Request) (interface{}, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20)) // 10MB limit
	if err != nil {
		return nil, err
	}
	var parsed interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

// jsonAPIError writes a JSON error response.
func (sm *ServeManager) jsonAPIError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// jsonAPIResponse writes a JSON success response.
func (sm *ServeManager) jsonAPIResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
```

**Step 2: Wire `/_api` into `makeHandler` in `serve_manager.go`**

In `serve_manager.go`, inside `makeHandler` (line 183), add the `/_api` check right after the CORS/OPTIONS handling (after line 193, before line 195 where `reqPath` is set):

```go
		// Route /_api/* requests to the JSON API handler.
		if strings.HasPrefix(r.URL.Path, "/_api/") || r.URL.Path == "/_api" {
			sm.handleJSONAPI(w, r, ts)
			return
		}
```

Also update the CORS methods on line 187 to include all JSON API methods:

```go
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Accept")
```

**Step 3: Add cookie setting on HTML responses**

In `serve_manager.go`, in the `serveClipData` method (line 167), add cookie setting when serving HTML and apiAccess is not "none". But since `serveClipData` doesn't have access to the `tagServer`, we need to set the cookie at the handler level.

Better approach: in `makeHandler`, after the `/_api` check and before any response is sent, set the cookie on every response when `apiAccess != "none"`:

Add this right after the `/_api` routing block (and before the `reqPath` parsing):

```go
		// Set auth cookie on all responses when JSON API is enabled.
		if ts.apiAccess != "none" {
			http.SetCookie(w, &http.Cookie{
				Name:     "_mp_serve_key",
				Value:    ts.serveKey,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})
		}
```

**Step 4: Verify it compiles**

Run: `cd /Users/egecan/Code/mahpastes && ~/go/bin/wails build 2>&1 | head -30`
Expected: Build succeeds.

**Step 5: Commit**

```bash
git add serve_json_api.go serve_manager.go
git commit -m "feat: implement /_api JSON handler for served tags"
```

---

### Task 4: Update Frontend — API Access Dropdown and Bindings

**Files:**
- Modify: `frontend/js/serve.js:82-134` (renderServeCard — add dropdown)
- Modify: `frontend/js/serve.js:267-278` (startServingTag — pass apiAccess)
- Modify: `frontend/js/serve.js:8` (add apiAccessPreferences map)
- Regenerate: `frontend/wailsjs/` (run `wails generate module`)

**Step 1: Regenerate Wails bindings**

Run: `cd /Users/egecan/Code/mahpastes && ~/go/bin/wails generate module`

This updates `frontend/wailsjs/go/main/ServeService.js` and `.d.ts` to include the new 4th parameter.

**Step 2: Add `apiAccessPreferences` state to `serve.js`**

At line 8 (after `bindPreferences`), add:

```javascript
const apiAccessPreferences = new Map(); // tagID -> "none" | "read" | "readwrite"
```

**Step 3: Update `renderServeCard` to include API access dropdown**

In `renderServeCard` (around line 82), add the API access dropdown between the bind toggle and the start/stop button. After the `bindLabel` / `bindDisabled` declarations (around line 105), add:

```javascript
    const apiAccess = apiAccessPreferences.get(info.tag_id) || 'none';
    const apiAccessDisabled = info.running ? 'pointer-events-none opacity-50' : '';
```

In the HTML template, insert the dropdown after the bind toggle button and before the start/stop button:

```html
            <select class="serve-api-access text-[10px] font-medium py-1 px-1.5 rounded border border-stone-200 text-stone-500 bg-white transition-colors ${apiAccessDisabled}"
                    data-tag-id="${info.tag_id}" ${info.running ? 'disabled' : ''} title="JSON API access level">
                <option value="none" ${apiAccess === 'none' ? 'selected' : ''}>No API</option>
                <option value="read" ${apiAccess === 'read' ? 'selected' : ''}>API Read</option>
                <option value="readwrite" ${apiAccess === 'readwrite' ? 'selected' : ''}>API R/W</option>
            </select>
```

**Step 4: Add change handler for API access dropdown**

In the `serveList.addEventListener('click', ...)` block (around line 331), the `select` element uses `change` not `click`, so add a separate event listener after the click handler (around line 390):

```javascript
serveList.addEventListener('change', (e) => {
    const select = e.target.closest('.serve-api-access');
    if (select) {
        const tagID = parseInt(select.dataset.tagId, 10);
        apiAccessPreferences.set(tagID, select.value);
    }
});
```

**Step 5: Update `startServingTag` to pass `apiAccess`**

Update the function (around line 267):

```javascript
async function startServingTag(tagID, bindAll = false) {
    try {
        const apiAccess = apiAccessPreferences.get(tagID) || 'none';
        const port = await window.go.main.ServeService.GetRandomPort();
        await window.go.main.ServeService.StartServing(tagID, port, bindAll, apiAccess);
        showToast('Tag server started');
        await loadServeStatus();
        updateServeIndicator();
    } catch (error) {
        console.error('Failed to start serving:', error);
        showToast('Failed to start server: ' + error.message);
    }
}
```

**Step 6: Update `loadServeStatus` to preserve `apiAccess` from running servers**

In `loadServeStatus` (around line 137), when building configured entries from running servers, save the apiAccess:

In the loop where running servers are added to configured entries (around line 144), add:

```javascript
            if (s.api_access && s.api_access !== 'none') {
                apiAccessPreferences.set(s.tag_id, s.api_access);
            }
```

**Step 7: Verify dev mode works**

Run: `cd /Users/egecan/Code/mahpastes && make dev` (test manually that dropdown appears and works)

**Step 8: Commit**

```bash
git add frontend/js/serve.js frontend/wailsjs/
git commit -m "feat: add API access dropdown to serve UI and update bindings"
```

---

### Task 5: Update Test Fixtures for New Parameter

**Files:**
- Modify: `e2e/fixtures/test-fixtures.ts:2447-2459` (startServingTag)

**Step 1: Update `startServingTag` fixture method**

Add optional `apiAccess` parameter:

```typescript
  async startServingTag(tagName: string, apiAccess: string = 'none'): Promise<{ port: number; url: string }> {
    return this.page.evaluate(async ({ name, access }: { name: string; access: string }) => {
      // @ts-ignore - Wails runtime
      const tags = await window.go.main.App.GetTags();
      const tag = tags.find((t: any) => t.name === name);
      if (!tag) throw new Error(`Tag "${name}" not found`);
      // @ts-ignore - Wails runtime
      const port = await window.go.main.ServeService.GetRandomPort();
      // @ts-ignore - Wails runtime
      const info = await window.go.main.ServeService.StartServing(tag.id, port, false, access);
      return { port: info.port, url: info.url };
    }, { name: tagName, access: apiAccess });
  }
```

**Step 2: Run existing serve tests to verify nothing broke**

Run: `cd e2e && npx playwright test tests/serve/ --reporter=list`
Expected: All existing serve tests pass (they use default `'none'` apiAccess).

**Step 3: Commit**

```bash
git add e2e/fixtures/test-fixtures.ts
git commit -m "feat: add apiAccess parameter to startServingTag test fixture"
```

---

### Task 6: Write E2E Tests for JSON API

**Files:**
- Create: `e2e/tests/serve/json-api.spec.ts`

**Step 1: Write the full test file**

Create `e2e/tests/serve/json-api.spec.ts`:

```typescript
import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile } from '../../helpers/test-data';

/**
 * Helper: upload a JSON clip with given filename and content, tag it, and return its info.
 */
async function uploadJSONClip(app: any, content: object | any[], filename: string, tagName: string) {
  const jsonStr = JSON.stringify(content);
  const filePath = await createTempFile(Buffer.from(jsonStr), 'json');
  // Upload with specific filename
  await app.page.evaluate(async ({ path, name }: { path: string; name: string }) => {
    // @ts-ignore
    const result = await window.go.main.App.UploadFiles([path], 0, 0);
    // Rename to desired filename
    if (result && result.length > 0) {
      // @ts-ignore
      await window.go.main.App.RenameClip(result[0].id, name);
    }
    return result;
  }, { path: filePath, name: filename });
  // Tag it
  await app.addTagToClip(filename, tagName);
}

/**
 * Helper: make a fetch request from the browser context to the served tag.
 */
async function apiFetch(
  page: any,
  port: number,
  path: string,
  options: { method?: string; body?: any; getCookie?: boolean } = {}
) {
  return page.evaluate(
    async ({ port, path, method, body }: any) => {
      const url = `http://127.0.0.1:${port}/_api/${path}`;
      const opts: RequestInit = {
        method: method || 'GET',
        credentials: 'include' as RequestCredentials,
      };
      if (body !== undefined) {
        opts.headers = { 'Content-Type': 'application/json' };
        opts.body = JSON.stringify(body);
      }
      const res = await fetch(url, opts);
      const text = await res.text();
      let json = null;
      try { json = JSON.parse(text); } catch {}
      return { status: res.status, body: json, text };
    },
    { port, path, method: options.method, body: options.body }
  );
}

test.describe('Serve - JSON API', () => {
  test.afterEach(async ({ app }) => {
    await app.stopAllServers();
  });

  test('should return 404 when apiAccess is none', async ({ app }) => {
    await app.createTag('api-none-test');
    const content = [{ id: 1, name: 'Alice' }];
    await uploadJSONClip(app, content, 'users.json', 'api-none-test');

    const { port } = await app.startServingTag('api-none-test', 'none');

    // First visit the root to get the cookie (even though apiAccess is none, no cookie is set)
    await app.page.evaluate(async (url: string) => {
      await fetch(url, { credentials: 'include' });
    }, `http://127.0.0.1:${port}/`);

    const result = await apiFetch(app.page, port, 'users');
    expect(result.status).toBe(404);
  });

  test('should require cookie auth', async ({ app }) => {
    await app.createTag('api-auth-test');
    const content = [{ id: 1, name: 'Alice' }];
    await uploadJSONClip(app, content, 'users.json', 'api-auth-test');

    const { port } = await app.startServingTag('api-auth-test', 'readwrite');

    // Request WITHOUT visiting the page first (no cookie)
    const result = await app.page.evaluate(async (port: number) => {
      const res = await fetch(`http://127.0.0.1:${port}/_api/users`, {
        credentials: 'omit',
      });
      return { status: res.status };
    }, port);
    expect(result.status).toBe(401);
  });

  test('should GET full JSON clip', async ({ app }) => {
    await app.createTag('api-get-test');
    const content = [{ id: 1, name: 'Alice' }, { id: 2, name: 'Bob' }];
    await uploadJSONClip(app, content, 'users.json', 'api-get-test');

    const { port } = await app.startServingTag('api-get-test', 'readwrite');

    // Visit root to get cookie
    await app.page.evaluate(async (url: string) => {
      await fetch(url, { credentials: 'include' });
    }, `http://127.0.0.1:${port}/`);

    const result = await apiFetch(app.page, port, 'users');
    expect(result.status).toBe(200);
    expect(result.body).toHaveLength(2);
    expect(result.body[0].name).toBe('Alice');
  });

  test('should GET element by id', async ({ app }) => {
    await app.createTag('api-getid-test');
    const content = [{ id: 1, name: 'Alice' }, { id: 2, name: 'Bob' }];
    await uploadJSONClip(app, content, 'users.json', 'api-getid-test');

    const { port } = await app.startServingTag('api-getid-test', 'readwrite');
    await app.page.evaluate(async (url: string) => {
      await fetch(url, { credentials: 'include' });
    }, `http://127.0.0.1:${port}/`);

    const result = await apiFetch(app.page, port, 'users/2');
    expect(result.status).toBe(200);
    expect(result.body.name).toBe('Bob');
  });

  test('should POST to array with auto-increment id', async ({ app }) => {
    await app.createTag('api-post-test');
    const content = [{ id: 1, name: 'Alice' }];
    await uploadJSONClip(app, content, 'users.json', 'api-post-test');

    const { port } = await app.startServingTag('api-post-test', 'readwrite');
    await app.page.evaluate(async (url: string) => {
      await fetch(url, { credentials: 'include' });
    }, `http://127.0.0.1:${port}/`);

    const result = await apiFetch(app.page, port, 'users', {
      method: 'POST',
      body: { name: 'Bob' },
    });
    expect(result.status).toBe(201);
    expect(result.body.id).toBe(2);
    expect(result.body.name).toBe('Bob');

    // Verify it persisted
    const getResult = await apiFetch(app.page, port, 'users');
    expect(getResult.body).toHaveLength(2);
  });

  test('should PUT to replace element', async ({ app }) => {
    await app.createTag('api-put-test');
    const content = [{ id: 1, name: 'Alice', role: 'dev' }];
    await uploadJSONClip(app, content, 'users.json', 'api-put-test');

    const { port } = await app.startServingTag('api-put-test', 'readwrite');
    await app.page.evaluate(async (url: string) => {
      await fetch(url, { credentials: 'include' });
    }, `http://127.0.0.1:${port}/`);

    const result = await apiFetch(app.page, port, 'users/1', {
      method: 'PUT',
      body: { id: 1, name: 'Alice Updated', role: 'lead' },
    });
    expect(result.status).toBe(200);

    const getResult = await apiFetch(app.page, port, 'users/1');
    expect(getResult.body.name).toBe('Alice Updated');
    expect(getResult.body.role).toBe('lead');
  });

  test('should PATCH with merge semantics', async ({ app }) => {
    await app.createTag('api-patch-test');
    const content = [{ id: 1, name: 'Alice', role: 'dev', team: 'backend' }];
    await uploadJSONClip(app, content, 'users.json', 'api-patch-test');

    const { port } = await app.startServingTag('api-patch-test', 'readwrite');
    await app.page.evaluate(async (url: string) => {
      await fetch(url, { credentials: 'include' });
    }, `http://127.0.0.1:${port}/`);

    const result = await apiFetch(app.page, port, 'users/1', {
      method: 'PATCH',
      body: { role: 'lead' },
    });
    expect(result.status).toBe(200);
    expect(result.body.role).toBe('lead');
    expect(result.body.name).toBe('Alice'); // unchanged
    expect(result.body.team).toBe('backend'); // unchanged
  });

  test('should DELETE element from array', async ({ app }) => {
    await app.createTag('api-del-test');
    const content = [{ id: 1, name: 'Alice' }, { id: 2, name: 'Bob' }];
    await uploadJSONClip(app, content, 'users.json', 'api-del-test');

    const { port } = await app.startServingTag('api-del-test', 'readwrite');
    await app.page.evaluate(async (url: string) => {
      await fetch(url, { credentials: 'include' });
    }, `http://127.0.0.1:${port}/`);

    const result = await apiFetch(app.page, port, 'users/1', {
      method: 'DELETE',
    });
    expect(result.status).toBe(204);

    const getResult = await apiFetch(app.page, port, 'users');
    expect(getResult.body).toHaveLength(1);
    expect(getResult.body[0].name).toBe('Bob');
  });

  test('should enforce read-only mode', async ({ app }) => {
    await app.createTag('api-readonly-test');
    const content = [{ id: 1, name: 'Alice' }];
    await uploadJSONClip(app, content, 'users.json', 'api-readonly-test');

    const { port } = await app.startServingTag('api-readonly-test', 'read');
    await app.page.evaluate(async (url: string) => {
      await fetch(url, { credentials: 'include' });
    }, `http://127.0.0.1:${port}/`);

    // GET should work
    const getResult = await apiFetch(app.page, port, 'users');
    expect(getResult.status).toBe(200);

    // POST should be forbidden
    const postResult = await apiFetch(app.page, port, 'users', {
      method: 'POST',
      body: { name: 'Bob' },
    });
    expect(postResult.status).toBe(403);

    // PUT should be forbidden
    const putResult = await apiFetch(app.page, port, 'users/1', {
      method: 'PUT',
      body: { id: 1, name: 'Updated' },
    });
    expect(putResult.status).toBe(403);

    // DELETE should be forbidden
    const delResult = await apiFetch(app.page, port, 'users/1', {
      method: 'DELETE',
    });
    expect(delResult.status).toBe(403);
  });

  test('should navigate nested object paths', async ({ app }) => {
    await app.createTag('api-nested-test');
    const content = { theme: { colors: { primary: '#000', secondary: '#fff' } } };
    await uploadJSONClip(app, content, 'config.json', 'api-nested-test');

    const { port } = await app.startServingTag('api-nested-test', 'readwrite');
    await app.page.evaluate(async (url: string) => {
      await fetch(url, { credentials: 'include' });
    }, `http://127.0.0.1:${port}/`);

    // GET nested path
    const result = await apiFetch(app.page, port, 'config/theme/colors/primary');
    expect(result.status).toBe(200);
    expect(result.body).toBe('#000');

    // PUT nested path
    const putResult = await apiFetch(app.page, port, 'config/theme/colors/primary', {
      method: 'PUT',
      body: '#ff0000',
    });
    expect(putResult.status).toBe(200);

    // Verify update
    const verify = await apiFetch(app.page, port, 'config/theme/colors/primary');
    expect(verify.body).toBe('#ff0000');
  });
});
```

**Step 2: Run tests**

Run: `cd e2e && npx playwright test tests/serve/json-api.spec.ts --reporter=list`
Expected: All tests pass. If any fail, debug and fix the Go handler accordingly.

**Step 3: Commit**

```bash
git add e2e/tests/serve/json-api.spec.ts
git commit -m "test: add e2e tests for serve JSON API"
```

---

### Task 7: Update Documentation — Docusaurus Docs

**Files:**
- Modify: `docs/docs/features/tag-serve.md`
- Modify: `docs/docs/features/rest-api.md`
- Modify: `docs/docs/developers/architecture.md`

**Step 1: Add JSON API section to `tag-serve.md`**

After the "Subtag Folder Navigation" section (before "Activity Indicators"), add:

```markdown
## JSON API

Serve a tag's JSON clips as a RESTful API that HTML pages in the same tag can read and write.

### Enabling the API

When starting a tag server, select an API access level:

| Level | Description |
|-------|-------------|
| **No API** | `/_api` returns 404 (default) |
| **API Read** | GET only — HTML can read JSON data |
| **API R/W** | Full CRUD — HTML can read, create, update, and delete |

The access level can only be changed while the server is stopped.

### Authentication

When the API is enabled, the tag server sets an HTTP-only cookie (`_mp_serve_key`) on every response. HTML pages served from the same tag automatically receive this cookie, so `fetch()` calls to `/_api/` just work with `credentials: 'include'`.

Requests without a valid cookie receive `401 Unauthorized`.

### URL Structure

```
/_api/{clipName}/{jsonPath...}
```

- `{clipName}` maps to a clip named `{clipName}.json` in the tag
- `{jsonPath}` navigates into the JSON structure by key (objects) or by `id` field (arrays)

### HTTP Methods

| Method | Path | Behavior |
|--------|------|----------|
| `GET` | `/_api/users` | Return full contents of `users.json` |
| `GET` | `/_api/users/3` | Return array element where `id` is 3 |
| `POST` | `/_api/users` | Append to array, auto-assign `id` |
| `PUT` | `/_api/users/3` | Replace element with `id` 3 |
| `PATCH` | `/_api/users/3` | Merge fields into element (RFC 7396) |
| `DELETE` | `/_api/users/3` | Remove element from array |
| `PUT` | `/_api/config/theme` | Set nested key in `config.json` |

### Example

Given a clip named `todos.json` containing:

```json
[{"id": 1, "text": "Buy milk", "done": false}]
```

From an `index.html` in the same tag:

```javascript
// Read all todos
const todos = await fetch('/_api/todos', { credentials: 'include' }).then(r => r.json());

// Add a new todo (id auto-assigned)
const newTodo = await fetch('/_api/todos', {
  method: 'POST',
  credentials: 'include',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ text: 'Walk dog', done: false })
}).then(r => r.json());
// newTodo = { id: 2, text: "Walk dog", done: false }

// Mark as done
await fetch('/_api/todos/2', {
  method: 'PATCH',
  credentials: 'include',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ done: true })
});
```

### Reserved Tag Names

Tag names cannot contain `_api` as a path segment (e.g., `_api`, `work/_api`, `docs/_api/foo`). This prevents conflicts with the API route prefix. Names where `_api` appears as a substring are allowed (e.g., `my_api_utils`).
```

**Step 2: Update `rest-api.md` POST /serve section**

In the `POST /api/v1/serve` section (around line 139), update the JSON body example:

```json
{
  "tag_id": 5,
  "port": 0,
  "bind_all": false,
  "api_access": "none"
}
```

Add a bullet: `api_access`: `"none"` (default), `"read"`, or `"readwrite"` — controls the [JSON API](tag-serve.md#json-api) on the tag server.

**Step 3: Update `architecture.md`**

In the backend components list (around line 72), update the serve_manager.go line:

```
serve_manager.go     Tag serve HTTP server management, JSON API handler
```

**Step 4: Commit**

```bash
git add docs/docs/features/tag-serve.md docs/docs/features/rest-api.md docs/docs/developers/architecture.md
git commit -m "docs: document JSON API for served tags"
```

---

### Task 8: Update CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`

**Step 1: Add "Tag Serve JSON API" section**

After the "Tag Hierarchy & Folder Mode" section (and before "Plugin UI Actions"), add:

```markdown
### Tag Serve JSON API

HTML clips served from a tag can read/write JSON clips in the same tag via REST semantics at the `/_api` prefix.

**Routing**: `/_api/{clipStem}/{jsonPath...}` — first segment maps to `{clipStem}.json` clip, remaining segments navigate into JSON (object keys by name, array elements by `id` field).

**HTTP verbs**: GET (read), POST (append to array with auto-increment id), PUT (replace/upsert), PATCH (JSON Merge Patch RFC 7396), DELETE (remove key/element).

**Cookie auth**: Tag server sets `_mp_serve_key` HTTP-only SameSite=Strict cookie on every response when API is enabled. `/_api` handler validates this cookie — 401 without it.

**Permission model**: `StartServing(tagID, port, bindAll, apiAccess)` where `apiAccess` is `"none"` (404, default), `"read"` (GET only), or `"readwrite"` (full CRUD).

**Concurrency**: Per-clip `sync.Mutex` serializes writes to the same JSON clip.

**Reserved tag names**: `CreateTag` rejects any tag where a path segment equals `_api` (e.g., `_api`, `work/_api`). Substrings are fine (e.g., `my_api_stuff`).

**Key files**: `serve_json_api.go` (JSON handler, path navigation, CRUD operations), `serve_manager.go` (cookie setting, `/_api` routing, `tagServer` struct fields).
```

**Step 2: Update File Structure**

In the file structure section, add `serve_json_api.go` after `serve_manager.go`:

```
├── serve_json_api.go     # JSON API handler for served tags (/_api prefix)
```

**Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: add Tag Serve JSON API section to CLAUDE.md"
```

---

### Task 9: Update Skill, References, and Commands

**Files:**
- Modify: `skills/mahpastes/SKILL.md`
- Modify: `skills/mahpastes/references/api-reference.md`
- Modify: `commands/serve.md`

**Step 1: Update `SKILL.md`**

In the description (line 3), add JSON API triggers:

```yaml
description: This skill should be used when Claude Code needs to store generated artifacts, host websites or static files via HTTP, manage a research knowledge base, or create interactive web apps with JSON data persistence using the mahpastes clipboard manager's REST API and tag-serve system. Triggers on "save to mahpastes", "upload to mahpastes", "store in mahpastes", "host this site", "serve these files", "deploy to mahpastes", "search mahpastes", "find in mahpastes", "persist this to mahpastes", "tag this in mahpastes", "make this available over HTTP via mahpastes", "retrieve from mahpastes", "create interactive app in mahpastes", "JSON API with mahpastes", or "serve data from mahpastes".
```

Update the "Start Tag-Serve" curl example to include `api_access`:

```bash
curl -s -X POST \
  -H "Authorization: Bearer $MAHPASTES_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"tag_id":TAG_ID,"port":PORT,"bind_all":false,"api_access":"none"}' \
  "$MAHPASTES_API_URL/api/v1/serve"
```

Add `api_access` note: Set `api_access` to `"read"` or `"readwrite"` to enable the JSON API on the served tag. When enabled, HTML clips can fetch `/_api/{clipName}/{path}` to read/write JSON clips.

Add a new workflow pattern after pattern 3:

```markdown
### 4. Interactive Web App with JSON API

Build self-contained web apps that persist data via JSON clips.

1. Create a tag for the app (e.g., `todo-app`)
2. Upload `index.html` with the app UI (uses `fetch('/_api/...', { credentials: 'include' })`)
3. Upload `todos.json` with initial data (e.g., `[]`)
4. Tag both clips with the app tag
5. Start serving with read-write API: `POST /api/v1/serve` with `"api_access": "readwrite"`
6. The HTML can now GET/POST/PUT/PATCH/DELETE via `/_api/todos/{id}`

**With a scoped key**: Skip steps 1 and 4. Use the scoped tag ID for serving.
```

**Step 2: Update `references/api-reference.md`**

In the "Start Serving" body table (around line 220), add:

```markdown
| api_access | string | No | JSON API access: `"none"` (default), `"read"`, `"readwrite"` |
```

Add a note: When `api_access` is `"read"` or `"readwrite"`, the tag server exposes a JSON API at `/_api/` that HTML clips can use to read/write JSON clips in the same tag. See the main skill doc for usage patterns.

**Step 3: Update `commands/serve.md`**

In "Parse Arguments", add `$3`:

```markdown
- `$3` — API access mode: `none`, `read`, or `readwrite` (optional, default: `none`)
```

Update the Start Serving curl:

```bash
curl -s -X POST \
  -H "Authorization: Bearer $MAHPASTES_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"tag_id":TAG_ID,"port":PORT,"bind_all":false,"api_access":"API_ACCESS"}' \
  "$MAHPASTES_API_URL/api/v1/serve"
```

After the "Report" section, add:

```markdown
## JSON API Note

When `api_access` is `read` or `readwrite`, the served tag exposes a JSON API at the `/_api/` path. HTML files in the tag can use `fetch('/_api/{clipName}/{path}', { credentials: 'include' })` to read and write JSON clips. The auth cookie is set automatically by the server.
```

**Step 4: Commit**

```bash
git add skills/mahpastes/SKILL.md skills/mahpastes/references/api-reference.md commands/serve.md
git commit -m "docs: update skill, API reference, and serve command for JSON API"
```

---

### Task 10: Run All Tests and Fix

**Step 1: Run all e2e tests**

Run: `cd e2e && npm test`
Expected: All tests pass.

**Step 2: Fix any failures**

If any tests fail, investigate and fix. Common issues:
- Existing serve tests may need updating if the 4th parameter caused binding changes
- The Wails bindings may need regeneration (`wails generate module`)

**Step 3: Final commit if fixes were needed**

```bash
git add -A
git commit -m "fix: resolve test issues after JSON API integration"
```

---

Plan complete and saved to `docs/plans/2026-03-09-json-api-for-served-tags-impl.md`. Two execution options:

**1. Subagent-Driven (this session)** — I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Parallel Session (separate)** — Open new session with executing-plans, batch execution with checkpoints

Which approach?
