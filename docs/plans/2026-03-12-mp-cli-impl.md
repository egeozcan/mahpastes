# mahpastes CLI (`mp`) Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build the `mp` CLI binary and extend the REST API to full feature parity, enabling scripting, power-user, and agentic use of mahpastes.

**Architecture:** Thin Cobra CLI → HTTP client → REST API (`/api/v1/*`) → existing App methods. Two workstreams: (1) extend `api_manager.go` with ~45 new endpoints, (2) build `cmd/mp/` as a stateless HTTP client.

**Tech Stack:** Go, Cobra CLI framework, `net/http` client, existing `api_manager.go` patterns.

**Design doc:** `docs/plans/2026-03-12-mp-cli-design.md`

---

## Task 1: CLI Scaffold & HTTP Client

Set up the `cmd/mp/` directory, root Cobra command, HTTP client package, output helpers, and Makefile targets.

**Files:**
- Create: `cmd/mp/main.go`
- Create: `cmd/mp/client/client.go`
- Create: `cmd/mp/output.go`
- Modify: `Makefile`
- Modify: `go.mod` (add cobra dependency)

**Step 1: Add Cobra dependency**

```bash
go get github.com/spf13/cobra@latest
```

**Step 2: Create HTTP client package**

Create `cmd/mp/client/client.go`:

```go
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
)

type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return e.Message
}

func New() (*Client, error) {
	baseURL := os.Getenv("MP_API_URL")
	if baseURL == "" {
		baseURL = "http://localhost:44557"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	apiKey := os.Getenv("MP_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("MP_API_KEY environment variable is required.\n\nSet it with:\n  export MP_API_KEY=mp_your_key_here\n\nCreate a key in the mahpastes app under Settings > API.")
	}

	return &Client{
		BaseURL:    baseURL,
		APIKey:     apiKey,
		HTTPClient: &http.Client{},
	}, nil
}

func (c *Client) do(method, path string, body io.Reader, contentType string) (*http.Response, error) {
	url := c.BaseURL + path
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		if strings.Contains(err.Error(), "connection refused") ||
			strings.Contains(err.Error(), "dial tcp") {
			return nil, &APIError{
				StatusCode: 0,
				Message:    fmt.Sprintf("Cannot connect to mahpastes at %s. Is the app running? Is the API started?", c.BaseURL),
			}
		}
		return nil, err
	}

	if resp.StatusCode == 401 {
		resp.Body.Close()
		return nil, &APIError{StatusCode: 401, Message: "Authentication failed. Check MP_API_KEY."}
	}
	if resp.StatusCode == 403 {
		resp.Body.Close()
		return nil, &APIError{StatusCode: 403, Message: "Insufficient permissions for this operation."}
	}

	return resp, nil
}

// Get performs a GET request and returns the response body.
func (c *Client) Get(path string) (*http.Response, error) {
	return c.do("GET", path, nil, "")
}

// GetJSON performs a GET and decodes JSON into dest.
func (c *Client) GetJSON(path string, dest interface{}) error {
	resp, err := c.Get(path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return c.parseError(resp)
	}
	return json.NewDecoder(resp.Body).Decode(dest)
}

// PostJSON sends a JSON body and decodes the response.
func (c *Client) PostJSON(path string, body interface{}, dest interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	resp, err := c.do("POST", path, bytes.NewReader(data), "application/json")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return c.parseError(resp)
	}
	if dest != nil && resp.StatusCode != 204 {
		return json.NewDecoder(resp.Body).Decode(dest)
	}
	return nil
}

// PutJSON sends a JSON body via PUT.
func (c *Client) PutJSON(path string, body interface{}, dest interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	resp, err := c.do("PUT", path, bytes.NewReader(data), "application/json")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return c.parseError(resp)
	}
	if dest != nil && resp.StatusCode != 204 {
		return json.NewDecoder(resp.Body).Decode(dest)
	}
	return nil
}

// PatchJSON sends a JSON body via PATCH.
func (c *Client) PatchJSON(path string, body interface{}, dest interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	resp, err := c.do("PATCH", path, bytes.NewReader(data), "application/json")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return c.parseError(resp)
	}
	if dest != nil && resp.StatusCode != 204 {
		return json.NewDecoder(resp.Body).Decode(dest)
	}
	return nil
}

// Delete sends a DELETE request.
func (c *Client) Delete(path string) error {
	resp, err := c.do("DELETE", path, nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return c.parseError(resp)
	}
	return nil
}

// DeleteJSON sends a DELETE and decodes response.
func (c *Client) DeleteJSON(path string, dest interface{}) error {
	resp, err := c.do("DELETE", path, nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return c.parseError(resp)
	}
	if dest != nil && resp.StatusCode != 204 {
		return json.NewDecoder(resp.Body).Decode(dest)
	}
	return nil
}

// UploadFile uploads a file via multipart/form-data POST.
func (c *Client) UploadFile(path, filename string, data io.Reader, contentType string, queryParams map[string]string) (*http.Response, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, data); err != nil {
		return nil, err
	}
	writer.Close()

	fullPath := path
	if len(queryParams) > 0 {
		params := make([]string, 0, len(queryParams))
		for k, v := range queryParams {
			params = append(params, k+"="+v)
		}
		fullPath += "?" + strings.Join(params, "&")
	}

	return c.do("POST", fullPath, &buf, writer.FormDataContentType())
}

// GetRaw performs a GET and returns the raw response (for binary data).
func (c *Client) GetRaw(path string) (*http.Response, error) {
	resp, err := c.Get(path)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		return nil, c.parseError(resp)
	}
	return resp, nil
}

func (c *Client) parseError(resp *http.Response) error {
	var errResp struct {
		Error string `json:"error"`
	}
	body, _ := io.ReadAll(resp.Body)
	if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
		return &APIError{StatusCode: resp.StatusCode, Message: errResp.Error}
	}
	return &APIError{StatusCode: resp.StatusCode, Message: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body))}
}
```

**Step 3: Create output helpers**

Create `cmd/mp/output.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
)

var jsonOutput bool

func printJSON(v interface{}) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

func printTable(headers []string, rows [][]string) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, strings.Join(headers, "\t"))
	fmt.Fprintln(w, strings.Repeat("-\t", len(headers)))
	for _, row := range rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}
	w.Flush()
}

func printKeyValue(pairs [][2]string) {
	maxKey := 0
	for _, p := range pairs {
		if len(p[0]) > maxKey {
			maxKey = len(p[0])
		}
	}
	for _, p := range pairs {
		fmt.Printf("%-*s  %s\n", maxKey, p[0]+":", p[1])
	}
}

func errorf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
}

func copyToStdout(r io.Reader) error {
	_, err := io.Copy(os.Stdout, r)
	return err
}
```

**Step 4: Create root command**

Create `cmd/mp/main.go`:

```go
package main

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "mp",
	Short: "mahpastes CLI — manage clips, tags, and more from the terminal",
	Long: `mp is the command-line interface for mahpastes, a desktop clipboard manager.

It communicates with the mahpastes REST API to manage clips, tags, watch folders,
plugins, backups, and more. Designed for scripting, automation, and agentic use.

Get started:
  1. Start the API in mahpastes: Settings > API > Start
  2. Create an API key: Settings > API > New Key (admin role recommended)
  3. Export your key: export MP_API_KEY=mp_your_key_here
  4. Try it: mp clip list`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		errorf("%s", err)
		// Exit code based on error type
		if apiErr, ok := err.(interface{ StatusCode() int }); ok {
			switch apiErr.StatusCode() {
			case 0:
				os.Exit(2) // connection error
			case 401:
				os.Exit(3) // auth error
			}
		}
		os.Exit(1)
	}
}
```

**Step 5: Add Makefile targets**

Add to `Makefile` before the `## Testing` section:

```makefile
## CLI

mp: ## Build mp CLI for current platform
	go build -o build/bin/mp ./cmd/mp

mp-install: mp ## Install mp to /usr/local/bin (macOS/Linux)
ifeq ($(OS),Windows_NT)
	copy build\bin\mp.exe $(USERPROFILE)\go\bin\mp.exe
else
	cp build/bin/mp /usr/local/bin/mp
endif

mp-cross: ## Cross-compile mp for all platforms
	GOOS=darwin GOARCH=amd64 go build -o build/bin/mp-darwin-amd64 ./cmd/mp
	GOOS=darwin GOARCH=arm64 go build -o build/bin/mp-darwin-arm64 ./cmd/mp
	GOOS=linux GOARCH=amd64 go build -o build/bin/mp-linux-amd64 ./cmd/mp
	GOOS=windows GOARCH=amd64 go build -o build/bin/mp-windows-amd64.exe ./cmd/mp
```

**Step 6: Verify it compiles**

```bash
go get github.com/spf13/cobra@latest && go build ./cmd/mp
```

**Step 7: Commit**

```bash
git add cmd/mp/ Makefile go.mod go.sum
git commit -m "feat: scaffold mp CLI with root command, HTTP client, and output helpers"
```

---

## Task 2: API — Clip Extended Endpoints

Add rename, expiration, and bulk operation endpoints to `api_manager.go`.

**Files:**
- Modify: `api_manager.go` (add route registrations and handler functions)

**Step 1: Register new routes**

In `api_manager.go`, inside the `Start` method, after the existing `mux.HandleFunc` lines (after line 116), add:

```go
// Clip extended operations
mux.HandleFunc("PATCH /api/v1/clips/{id}", am.authMiddleware(am.requireRole("editor", am.handleRenameClip)))
mux.HandleFunc("PUT /api/v1/clips/{id}/expiration", am.authMiddleware(am.requireRole("editor", am.handleSetExpiration)))
mux.HandleFunc("DELETE /api/v1/clips/{id}/expiration", am.authMiddleware(am.requireRole("editor", am.handleCancelExpiration)))
mux.HandleFunc("POST /api/v1/clips/bulk/delete", am.authMiddleware(am.requireRole("editor", am.handleBulkDelete)))
mux.HandleFunc("POST /api/v1/clips/bulk/archive", am.authMiddleware(am.requireRole("editor", am.handleBulkArchive)))
mux.HandleFunc("POST /api/v1/clips/bulk/unarchive", am.authMiddleware(am.requireRole("editor", am.handleBulkUnarchive)))
mux.HandleFunc("POST /api/v1/clips/bulk/expire", am.authMiddleware(am.requireRole("editor", am.handleBulkSetExpiration)))
mux.HandleFunc("POST /api/v1/clips/bulk/cancel-expire", am.authMiddleware(am.requireRole("editor", am.handleBulkCancelExpiration)))
mux.HandleFunc("POST /api/v1/clips/bulk/tag", am.authMiddleware(am.requireRole("editor", am.handleBulkAddTag)))
mux.HandleFunc("POST /api/v1/clips/bulk/untag", am.authMiddleware(am.requireRole("editor", am.handleBulkRemoveTag)))
mux.HandleFunc("POST /api/v1/clips/bulk/download", am.authMiddleware(am.requireRole("viewer", am.handleBulkDownload)))
mux.HandleFunc("POST /api/v1/clips/bulk/copy", am.authMiddleware(am.requireRole("editor", am.handleBulkCopyToClipboard)))
```

**Step 2: Implement handlers**

Add after the existing clip handlers in `api_manager.go`:

```go
func (am *APIManager) handleRenameClip(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid clip id")
		return
	}
	if err := am.enforceTagScope(keyCtx, id); err != nil {
		am.jsonError(w, http.StatusForbidden, err.Error())
		return
	}
	var body struct {
		Filename string `json:"filename"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Filename == "" {
		am.jsonError(w, http.StatusBadRequest, "filename is required")
		return
	}
	if err := am.app.RenameClip(id, body.Filename); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleSetExpiration(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid clip id")
		return
	}
	if err := am.enforceTagScope(keyCtx, id); err != nil {
		am.jsonError(w, http.StatusForbidden, err.Error())
		return
	}
	var body struct {
		Minutes int `json:"minutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Minutes <= 0 {
		am.jsonError(w, http.StatusBadRequest, "minutes must be a positive integer")
		return
	}
	if err := am.app.SetExpiration(id, body.Minutes); err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleCancelExpiration(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid clip id")
		return
	}
	if err := am.enforceTagScope(keyCtx, id); err != nil {
		am.jsonError(w, http.StatusForbidden, err.Error())
		return
	}
	if err := am.app.CancelExpiration(id); err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleBulkDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.IDs) == 0 {
		am.jsonError(w, http.StatusBadRequest, "ids array is required")
		return
	}
	if err := am.app.BulkDelete(body.IDs); err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	am.jsonOK(w, map[string]int{"deleted": len(body.IDs)})
}

func (am *APIManager) handleBulkArchive(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.IDs) == 0 {
		am.jsonError(w, http.StatusBadRequest, "ids array is required")
		return
	}
	if err := am.app.BulkArchive(body.IDs); err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	am.jsonOK(w, map[string]int{"archived": len(body.IDs)})
}

func (am *APIManager) handleBulkUnarchive(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.IDs) == 0 {
		am.jsonError(w, http.StatusBadRequest, "ids array is required")
		return
	}
	// Unarchive each clip individually (no BulkUnarchive method exists)
	for _, id := range body.IDs {
		am.app.db.Exec("UPDATE clips SET is_archived = 0 WHERE id = ?", id)
	}
	am.jsonOK(w, map[string]int{"unarchived": len(body.IDs)})
}

func (am *APIManager) handleBulkSetExpiration(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs     []int64 `json:"ids"`
		Minutes int     `json:"minutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.IDs) == 0 || body.Minutes <= 0 {
		am.jsonError(w, http.StatusBadRequest, "ids and positive minutes are required")
		return
	}
	if err := am.app.BulkSetExpiration(body.IDs, body.Minutes); err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	am.jsonOK(w, map[string]int{"updated": len(body.IDs)})
}

func (am *APIManager) handleBulkCancelExpiration(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.IDs) == 0 {
		am.jsonError(w, http.StatusBadRequest, "ids array is required")
		return
	}
	if err := am.app.BulkCancelExpiration(body.IDs); err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	am.jsonOK(w, map[string]int{"updated": len(body.IDs)})
}

func (am *APIManager) handleBulkAddTag(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs   []int64 `json:"ids"`
		TagID int64   `json:"tag_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.IDs) == 0 || body.TagID == 0 {
		am.jsonError(w, http.StatusBadRequest, "ids and tag_id are required")
		return
	}
	if err := am.app.BulkAddTag(body.IDs, body.TagID); err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	am.jsonOK(w, map[string]int{"tagged": len(body.IDs)})
}

func (am *APIManager) handleBulkRemoveTag(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs   []int64 `json:"ids"`
		TagID int64   `json:"tag_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.IDs) == 0 || body.TagID == 0 {
		am.jsonError(w, http.StatusBadRequest, "ids and tag_id are required")
		return
	}
	if err := am.app.BulkRemoveTag(body.IDs, body.TagID); err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	am.jsonOK(w, map[string]int{"untagged": len(body.IDs)})
}

func (am *APIManager) handleBulkDownload(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.IDs) == 0 {
		am.jsonError(w, http.StatusBadRequest, "ids array is required")
		return
	}
	// Build ZIP in memory and stream to response
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=\"clips.zip\"")
	zipWriter := zip.NewWriter(w)
	defer zipWriter.Close()
	for _, id := range body.IDs {
		var data []byte
		var filename sql.NullString
		err := am.app.db.QueryRow("SELECT data, filename FROM clips WHERE id = ?", id).Scan(&data, &filename)
		if err != nil {
			continue
		}
		name := filename.String
		if name == "" {
			name = fmt.Sprintf("clip_%d", id)
		}
		f, err := zipWriter.Create(name)
		if err != nil {
			continue
		}
		f.Write(data)
	}
}

func (am *APIManager) handleBulkCopyToClipboard(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.IDs) == 0 {
		am.jsonError(w, http.StatusBadRequest, "ids array is required")
		return
	}
	if am.app.clipboardService == nil {
		am.jsonError(w, http.StatusInternalServerError, "clipboard service not available")
		return
	}
	if err := am.app.clipboardService.BulkCopyFilesToClipboard(body.IDs); err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

Note: The `handleBulkDownload` handler needs the `archive/zip` import added to the file. Also check that `am.app.clipboardService` exists — if not, access clipboard service through the App struct. Adjust field name based on how `ClipboardService` is wired (check `main.go` for how services are attached to `App`).

**Step 3: Verify it compiles**

```bash
go build .
```

**Step 4: Commit**

```bash
git add api_manager.go
git commit -m "feat(api): add clip rename, expiration, and bulk operation endpoints"
```

---

## Task 3: API — Metadata, Tags Extended, Dedup Endpoints

**Files:**
- Modify: `api_manager.go`

**Step 1: Register routes**

Add after the bulk routes:

```go
// Clip metadata
mux.HandleFunc("GET /api/v1/clips/{id}/metadata", am.authMiddleware(am.requireRole("viewer", am.handleGetMetadata)))
mux.HandleFunc("PUT /api/v1/clips/{id}/metadata", am.authMiddleware(am.requireRole("editor", am.handleSetMetadataBulk)))
mux.HandleFunc("PUT /api/v1/clips/{id}/metadata/{key}", am.authMiddleware(am.requireRole("editor", am.handleSetMetadata)))
mux.HandleFunc("DELETE /api/v1/clips/{id}/metadata/{key}", am.authMiddleware(am.requireRole("editor", am.handleDeleteMetadata)))

// Tags extended
mux.HandleFunc("GET /api/v1/tags/{id}/children", am.authMiddleware(am.requireRole("viewer", am.handleGetChildTags)))
mux.HandleFunc("GET /api/v1/tags/{id}/clips", am.authMiddleware(am.requireRole("viewer", am.handleGetTagClips)))
mux.HandleFunc("GET /api/v1/tags/hidden", am.authMiddleware(am.requireRole("viewer", am.handleGetHiddenTags)))
mux.HandleFunc("PUT /api/v1/tags/hidden", am.authMiddleware(am.requireRole("admin", am.handleSetHiddenTags)))

// Deduplication
mux.HandleFunc("GET /api/v1/dedup", am.authMiddleware(am.requireRole("viewer", am.handleListDuplicates)))
mux.HandleFunc("POST /api/v1/dedup/{clipId}/merge", am.authMiddleware(am.requireRole("editor", am.handleMergeDuplicates)))
mux.HandleFunc("POST /api/v1/dedup/all", am.authMiddleware(am.requireRole("admin", am.handleDeduplicateAll)))
```

**Step 2: Implement metadata handlers**

```go
func (am *APIManager) handleGetMetadata(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid clip id")
		return
	}
	if err := am.enforceTagScope(keyCtx, id); err != nil {
		am.jsonError(w, http.StatusForbidden, err.Error())
		return
	}
	metadata, err := am.app.GetClipMetadata(id)
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if metadata == nil {
		metadata = map[string]string{}
	}
	am.jsonOK(w, metadata)
}

func (am *APIManager) handleSetMetadataBulk(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid clip id")
		return
	}
	if err := am.enforceTagScope(keyCtx, id); err != nil {
		am.jsonError(w, http.StatusForbidden, err.Error())
		return
	}
	var metadata map[string]string
	if err := json.NewDecoder(r.Body).Decode(&metadata); err != nil {
		am.jsonError(w, http.StatusBadRequest, "expected JSON object with string values")
		return
	}
	if err := am.app.SetClipMetadataBulk(id, metadata); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleSetMetadata(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid clip id")
		return
	}
	key := r.PathValue("key")
	if key == "" {
		am.jsonError(w, http.StatusBadRequest, "metadata key is required")
		return
	}
	if err := am.enforceTagScope(keyCtx, id); err != nil {
		am.jsonError(w, http.StatusForbidden, err.Error())
		return
	}
	var body struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "expected JSON body with value field")
		return
	}
	if err := am.app.SetClipMetadata(id, key, body.Value); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleDeleteMetadata(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid clip id")
		return
	}
	key := r.PathValue("key")
	if err := am.enforceTagScope(keyCtx, id); err != nil {
		am.jsonError(w, http.StatusForbidden, err.Error())
		return
	}
	if err := am.app.DeleteClipMetadata(id, key); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

**Step 3: Implement tag extended handlers**

```go
func (am *APIManager) handleGetChildTags(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid tag id")
		return
	}
	children, err := am.app.GetChildTags(id)
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if children == nil {
		children = []Tag{}
	}
	am.jsonOK(w, children)
}

func (am *APIManager) handleGetTagClips(w http.ResponseWriter, r *http.Request) {
	keyCtx := getKeyContext(r)
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid tag id")
		return
	}

	// Reuse the list clips logic but with a forced tag filter
	limit := 50
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	// Scope check
	if keyCtx.ScopedTagID > 0 {
		var tagName string
		if err := am.app.db.QueryRow("SELECT name FROM tags WHERE id = ?", id).Scan(&tagName); err != nil {
			am.jsonOK(w, apiClipListResponse{Clips: []apiClipResponse{}, Total: 0, Limit: limit, Offset: offset})
			return
		}
		if !am.isTagInScope(tagName, keyCtx.ScopedTagID) {
			am.jsonError(w, http.StatusForbidden, "tag not in scope")
			return
		}
	}

	var totalCount int
	am.app.db.QueryRow(`SELECT COUNT(*) FROM clip_tags WHERE tag_id = ?`, id).Scan(&totalCount)

	rows, err := am.app.db.Query(`
		SELECT c.id, c.content_type, c.filename, LENGTH(c.data), c.is_archived, c.created_at
		FROM clips c
		JOIN clip_tags ct ON c.id = ct.clip_id
		WHERE ct.tag_id = ?
		ORDER BY c.created_at DESC
		LIMIT ? OFFSET ?`, id, limit, offset)
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to query clips")
		return
	}
	defer rows.Close()

	clips := []apiClipResponse{}
	for rows.Next() {
		var c apiClipResponse
		var filename sql.NullString
		var isArchived int
		if err := rows.Scan(&c.ID, &c.ContentType, &filename, &c.Size, &isArchived, &c.CreatedAt); err != nil {
			continue
		}
		c.Filename = filename.String
		c.IsArchived = isArchived == 1
		c.Tags, _ = am.app.GetClipTags(c.ID)
		if c.Tags == nil {
			c.Tags = []Tag{}
		}
		clips = append(clips, c)
	}
	am.jsonOK(w, apiClipListResponse{Clips: clips, Total: totalCount, Limit: limit, Offset: offset})
}

func (am *APIManager) handleGetHiddenTags(w http.ResponseWriter, r *http.Request) {
	ids, err := am.app.GetHiddenTags()
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if ids == nil {
		ids = []int64{}
	}
	am.jsonOK(w, map[string][]int64{"ids": ids})
}

func (am *APIManager) handleSetHiddenTags(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "expected JSON body with ids array")
		return
	}
	if err := am.app.SetHiddenTags(body.IDs); err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

**Step 4: Implement dedup handlers**

```go
func (am *APIManager) handleListDuplicates(w http.ResponseWriter, r *http.Request) {
	groups, err := am.app.GetDuplicateGroups()
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if groups == nil {
		groups = []DuplicateGroup{}
	}
	am.jsonOK(w, map[string]interface{}{"groups": groups, "total": len(groups)})
}

func (am *APIManager) handleMergeDuplicates(w http.ResponseWriter, r *http.Request) {
	clipID, err := parseIntParam(r.PathValue("clipId"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid clip id")
		return
	}
	if err := am.app.MergeDuplicates(clipID); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleDeduplicateAll(w http.ResponseWriter, r *http.Request) {
	count, err := am.app.DeduplicateAll()
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	am.jsonOK(w, map[string]int{"removed": count})
}
```

**Step 5: Verify and commit**

```bash
go build .
git add api_manager.go
git commit -m "feat(api): add metadata, tag children/clips, hidden tags, and dedup endpoints"
```

---

## Task 4: API — Watch Folder Endpoints

**Files:**
- Modify: `api_manager.go`

**Step 1: Register routes**

```go
// Watch folders
mux.HandleFunc("GET /api/v1/watch", am.authMiddleware(am.requireRole("viewer", am.handleListWatchFolders)))
mux.HandleFunc("GET /api/v1/watch/status", am.authMiddleware(am.requireRole("viewer", am.handleWatchStatus)))
mux.HandleFunc("GET /api/v1/watch/{id}", am.authMiddleware(am.requireRole("viewer", am.handleGetWatchFolder)))
mux.HandleFunc("POST /api/v1/watch", am.authMiddleware(am.requireRole("admin", am.handleAddWatchFolder)))
mux.HandleFunc("PUT /api/v1/watch/{id}", am.authMiddleware(am.requireRole("admin", am.handleUpdateWatchFolder)))
mux.HandleFunc("DELETE /api/v1/watch/{id}", am.authMiddleware(am.requireRole("admin", am.handleRemoveWatchFolder)))
mux.HandleFunc("PUT /api/v1/watch/{id}/pause", am.authMiddleware(am.requireRole("admin", am.handlePauseWatchFolder)))
mux.HandleFunc("DELETE /api/v1/watch/{id}/pause", am.authMiddleware(am.requireRole("admin", am.handleResumeWatchFolder)))
mux.HandleFunc("PUT /api/v1/watch/global-pause", am.authMiddleware(am.requireRole("admin", am.handleGlobalPause)))
mux.HandleFunc("DELETE /api/v1/watch/global-pause", am.authMiddleware(am.requireRole("admin", am.handleGlobalResume)))
mux.HandleFunc("POST /api/v1/watch/{id}/process", am.authMiddleware(am.requireRole("admin", am.handleProcessExisting)))
```

**Important route ordering note:** `GET /api/v1/watch/status` must be registered BEFORE `GET /api/v1/watch/{id}` since Go 1.22 mux matches more specific patterns first, but it's good practice to register static paths first. Same for `PUT /api/v1/watch/global-pause` vs `PUT /api/v1/watch/{id}/pause`.

**Step 2: Implement handlers**

```go
func (am *APIManager) handleListWatchFolders(w http.ResponseWriter, r *http.Request) {
	folders, err := am.app.GetWatchedFolders()
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if folders == nil {
		folders = []WatchedFolder{}
	}
	am.jsonOK(w, map[string]interface{}{"folders": folders, "total": len(folders)})
}

func (am *APIManager) handleWatchStatus(w http.ResponseWriter, r *http.Request) {
	status := am.app.GetWatchStatus()
	am.jsonOK(w, status)
}

func (am *APIManager) handleGetWatchFolder(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid folder id")
		return
	}
	folder, err := am.app.GetWatchedFolderByID(id)
	if err != nil {
		am.jsonError(w, http.StatusNotFound, "watch folder not found")
		return
	}
	am.jsonOK(w, folder)
}

func (am *APIManager) handleAddWatchFolder(w http.ResponseWriter, r *http.Request) {
	var config WatchedFolderConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if config.Path == "" {
		am.jsonError(w, http.StatusBadRequest, "path is required")
		return
	}
	folder, err := am.app.AddWatchedFolder(config)
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(folder)
}

func (am *APIManager) handleUpdateWatchFolder(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid folder id")
		return
	}
	var config WatchedFolderConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := am.app.UpdateWatchedFolder(id, config); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleRemoveWatchFolder(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid folder id")
		return
	}
	if err := am.app.RemoveWatchedFolder(id); err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handlePauseWatchFolder(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid folder id")
		return
	}
	if err := am.app.SetFolderPaused(id, true); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleResumeWatchFolder(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid folder id")
		return
	}
	if err := am.app.SetFolderPaused(id, false); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleGlobalPause(w http.ResponseWriter, r *http.Request) {
	if err := am.app.SetGlobalWatchPaused(true); err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleGlobalResume(w http.ResponseWriter, r *http.Request) {
	if err := am.app.SetGlobalWatchPaused(false); err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleProcessExisting(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid folder id")
		return
	}
	if err := am.app.ProcessExistingFilesInFolder(id); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

**Step 3: Verify and commit**

```bash
go build .
git add api_manager.go
git commit -m "feat(api): add watch folder CRUD, pause/resume, and process endpoints"
```

---

## Task 5: API — Plugin Endpoints

**Files:**
- Modify: `api_manager.go`

**Step 1: Register routes**

```go
// Plugins
mux.HandleFunc("GET /api/v1/plugins", am.authMiddleware(am.requireRole("viewer", am.handleListPlugins)))
mux.HandleFunc("GET /api/v1/plugins/actions", am.authMiddleware(am.requireRole("viewer", am.handleListPluginActions)))
mux.HandleFunc("POST /api/v1/plugins", am.authMiddleware(am.requireRole("admin", am.handleInstallPlugin)))
mux.HandleFunc("DELETE /api/v1/plugins/{id}", am.authMiddleware(am.requireRole("admin", am.handleRemovePlugin)))
mux.HandleFunc("PUT /api/v1/plugins/{id}/enable", am.authMiddleware(am.requireRole("admin", am.handleEnablePlugin)))
mux.HandleFunc("PUT /api/v1/plugins/{id}/disable", am.authMiddleware(am.requireRole("admin", am.handleDisablePlugin)))
mux.HandleFunc("GET /api/v1/plugins/{id}/storage", am.authMiddleware(am.requireRole("admin", am.handleGetPluginStorageAll)))
mux.HandleFunc("GET /api/v1/plugins/{id}/storage/{key}", am.authMiddleware(am.requireRole("admin", am.handleGetPluginStorage)))
mux.HandleFunc("PUT /api/v1/plugins/{id}/storage/{key}", am.authMiddleware(am.requireRole("admin", am.handleSetPluginStorage)))
mux.HandleFunc("POST /api/v1/plugins/{id}/actions/{actionId}", am.authMiddleware(am.requireRole("editor", am.handleExecutePluginAction)))
mux.HandleFunc("POST /api/v1/plugins/check-updates", am.authMiddleware(am.requireRole("admin", am.handleCheckPluginUpdates)))
mux.HandleFunc("POST /api/v1/plugins/{id}/update", am.authMiddleware(am.requireRole("admin", am.handleUpdatePlugin)))
```

**Step 2: Implement handlers**

Note: Plugin handlers delegate to `PluginService` methods. Since `PluginService` is a separate struct, the APIManager needs access to it. Check how `App` references it — it may be `am.app.pluginService` or need to be added. If `App` doesn't hold a reference to `PluginService`, create the plugin service operations inline using `am.app.pluginManager` directly.

```go
func (am *APIManager) handleListPlugins(w http.ResponseWriter, r *http.Request) {
	if am.app.pluginManager == nil {
		am.jsonOK(w, map[string]interface{}{"plugins": []interface{}{}, "total": 0})
		return
	}
	plugins := am.app.pluginManager.ListPlugins()
	if plugins == nil {
		plugins = []PluginInfo{}
	}
	am.jsonOK(w, map[string]interface{}{"plugins": plugins, "total": len(plugins)})
}

func (am *APIManager) handleListPluginActions(w http.ResponseWriter, r *http.Request) {
	if am.app.pluginManager == nil {
		am.jsonOK(w, &UIActionsResponse{})
		return
	}
	// Delegate to pluginService if available, or inline the logic
	actions, err := am.app.getPluginUIActions()
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	am.jsonOK(w, actions)
}

func (am *APIManager) handleInstallPlugin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source string `json:"source"` // URL or file path
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Source == "" {
		am.jsonError(w, http.StatusBadRequest, "source (URL or path) is required")
		return
	}
	info, err := am.app.installPluginFromSource(body.Source)
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(info)
}

func (am *APIManager) handleRemovePlugin(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid plugin id")
		return
	}
	if am.app.pluginManager == nil {
		am.jsonError(w, http.StatusInternalServerError, "plugin manager not available")
		return
	}
	if err := am.app.pluginManager.RemovePlugin(id); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleEnablePlugin(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid plugin id")
		return
	}
	if am.app.pluginManager == nil {
		am.jsonError(w, http.StatusInternalServerError, "plugin manager not available")
		return
	}
	if err := am.app.pluginManager.EnablePlugin(id); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleDisablePlugin(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid plugin id")
		return
	}
	if am.app.pluginManager == nil {
		am.jsonError(w, http.StatusInternalServerError, "plugin manager not available")
		return
	}
	if err := am.app.pluginManager.DisablePlugin(id); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleGetPluginStorageAll(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid plugin id")
		return
	}
	storage, err := am.app.getAllPluginStorage(id)
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	am.jsonOK(w, storage)
}

func (am *APIManager) handleGetPluginStorage(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid plugin id")
		return
	}
	key := r.PathValue("key")
	value, err := am.app.getPluginStorage(id, key)
	if err != nil {
		am.jsonError(w, http.StatusNotFound, err.Error())
		return
	}
	am.jsonOK(w, map[string]string{"key": key, "value": value})
}

func (am *APIManager) handleSetPluginStorage(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid plugin id")
		return
	}
	key := r.PathValue("key")
	var body struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		am.jsonError(w, http.StatusBadRequest, "expected JSON body with value field")
		return
	}
	if err := am.app.setPluginStorage(id, key, body.Value); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleExecutePluginAction(w http.ResponseWriter, r *http.Request) {
	pluginID, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid plugin id")
		return
	}
	actionID := r.PathValue("actionId")
	var body struct {
		ClipIDs []int64                `json:"clip_ids"`
		Options map[string]interface{} `json:"options"`
	}
	json.NewDecoder(r.Body).Decode(&body) // optional body
	result, err := am.app.executePluginAction(pluginID, actionID, body.ClipIDs, body.Options)
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	am.jsonOK(w, result)
}

func (am *APIManager) handleCheckPluginUpdates(w http.ResponseWriter, r *http.Request) {
	if am.app.pluginManager == nil {
		am.jsonOK(w, map[string]interface{}{"updates": []interface{}{}})
		return
	}
	updates, err := am.app.pluginManager.CheckForUpdates()
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	am.jsonOK(w, map[string]interface{}{"updates": updates})
}

func (am *APIManager) handleUpdatePlugin(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r.PathValue("id"))
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "invalid plugin id")
		return
	}
	result, err := am.app.updatePlugin(id)
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	am.jsonOK(w, result)
}
```

**Important implementation note:** Some plugin methods live on `PluginService` not `App`. You'll need to either:
- Add helper methods to `App` that delegate to `PluginService` (e.g., `app.getPluginUIActions()`, `app.installPluginFromSource()`, `app.executePluginAction()`, etc.), OR
- Store a reference to `PluginService` on `App` or `APIManager`

Check `main.go` to see how `PluginService` is constructed and wired. The cleanest approach is adding thin delegation methods to `App` that call through to the plugin manager.

**Step 3: Verify and commit**

```bash
go build .
git add api_manager.go
git commit -m "feat(api): add plugin management, storage, actions, and update endpoints"
```

---

## Task 6: API — Backup and Clipboard Endpoints

**Files:**
- Modify: `api_manager.go`

**Step 1: Register routes**

```go
// Backup
mux.HandleFunc("GET /api/v1/backup", am.authMiddleware(am.requireRole("admin", am.handleCreateBackup)))
mux.HandleFunc("POST /api/v1/backup/restore", am.authMiddleware(am.requireRole("admin", am.handleRestoreBackup)))

// Clipboard
mux.HandleFunc("POST /api/v1/clipboard/copy", am.authMiddleware(am.requireRole("editor", am.handleCopyToClipboard)))
mux.HandleFunc("POST /api/v1/clipboard/copy-file", am.authMiddleware(am.requireRole("editor", am.handleCopyFileToClipboard)))
```

**Step 2: Implement handlers**

```go
func (am *APIManager) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	// Create backup to a temp file, then stream it
	tmpFile, err := os.CreateTemp("", "mahpastes-backup-*.zip")
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to create temp file")
		return
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	if err := am.app.CreateBackup(tmpPath); err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=\"mahpastes-backup.zip\"")
	http.ServeFile(w, r, tmpPath)
}

func (am *APIManager) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	// Accept multipart upload of backup ZIP
	ct := r.Header.Get("Content-Type")
	mediaType, params, _ := mime.ParseMediaType(ct)
	if !strings.HasPrefix(mediaType, "multipart/") {
		am.jsonError(w, http.StatusBadRequest, "expected multipart/form-data with backup ZIP")
		return
	}

	reader := multipart.NewReader(r.Body, params["boundary"])
	part, err := reader.NextPart()
	if err != nil {
		am.jsonError(w, http.StatusBadRequest, "failed to read upload")
		return
	}

	tmpFile, err := os.CreateTemp("", "mahpastes-restore-*.zip")
	if err != nil {
		am.jsonError(w, http.StatusInternalServerError, "failed to create temp file")
		return
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmpFile, part); err != nil {
		tmpFile.Close()
		am.jsonError(w, http.StatusBadRequest, "failed to save upload")
		return
	}
	tmpFile.Close()

	if err := am.app.ConfirmRestoreBackup(tmpPath); err != nil {
		am.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	am.jsonOK(w, map[string]string{"status": "restored"})
}

func (am *APIManager) handleCopyToClipboard(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ClipID int64 `json:"clip_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ClipID == 0 {
		am.jsonError(w, http.StatusBadRequest, "clip_id is required")
		return
	}
	if am.app.clipboardService == nil {
		am.jsonError(w, http.StatusInternalServerError, "clipboard service not available")
		return
	}
	if err := am.app.clipboardService.CopyClipContents(body.ClipID); err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (am *APIManager) handleCopyFileToClipboard(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ClipID int64 `json:"clip_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ClipID == 0 {
		am.jsonError(w, http.StatusBadRequest, "clip_id is required")
		return
	}
	if am.app.clipboardService == nil {
		am.jsonError(w, http.StatusInternalServerError, "clipboard service not available")
		return
	}
	if err := am.app.clipboardService.CopyFileToClipboard(body.ClipID); err != nil {
		am.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

**Important note:** The backup and clipboard handlers reference `am.app.clipboardService`. Check whether `App` stores a reference to `ClipboardService`. If not, you need to either:
- Add `clipboardService *ClipboardService` to the `App` struct and set it during startup in `main.go`
- Or construct it inline from the `App` reference

Same for `os` and `mime` and `multipart` imports — ensure they're included.

**Step 3: Verify and commit**

```bash
go build .
git add api_manager.go
git commit -m "feat(api): add backup download/restore and clipboard copy endpoints"
```

---

## Task 7: CLI — Clip Commands

Implement all `mp clip` subcommands.

**Files:**
- Create: `cmd/mp/clip.go`

**Step 1: Create clip command file**

This is a large file. Create `cmd/mp/clip.go` with all clip subcommands. Each command:
- Creates a `client.New()` to get the HTTP client
- Makes the appropriate API call
- Formats output based on `--json` flag

Key commands and their API mappings:
- `mp clip list` → `GET /api/v1/clips?tag=X&archived=true&limit=N&offset=N`
- `mp clip get <id>` → `GET /api/v1/clips/{id}`
- `mp clip data <id>` → `GET /api/v1/clips/{id}/data` (raw to stdout)
- `mp clip upload <files...>` → `POST /api/v1/clips` (multipart)
- `mp clip delete <ids...>` → `DELETE /api/v1/clips/{id}` or `POST /api/v1/clips/bulk/delete`
- `mp clip rename <id> <name>` → `PATCH /api/v1/clips/{id}`
- `mp clip archive <ids...>` → `PUT /api/v1/clips/{id}/archive` or `POST /api/v1/clips/bulk/archive`
- `mp clip unarchive <ids...>` → same pattern
- `mp clip expire <ids...> --duration 30m` → `PUT /api/v1/clips/{id}/expiration` or bulk
- `mp clip download <ids...>` → `POST /api/v1/clips/bulk/download` or `GET /api/v1/clips/{id}/data`
- `mp clip open <id>` → This requires the app to open locally — may need a new API endpoint or note this is desktop-only
- `mp clip metadata list/get/set/delete` → metadata endpoints

Each command needs:
- `Use`, `Short`, `Long`, `Example` fields
- Flag definitions
- `RunE` function with error handling
- `--stdin` support for bulk ID commands (read IDs line-by-line from stdin)

The `upload` command should:
- Accept file paths as args
- Read from stdin when no args (requires `--filename` and optionally `--content-type`)
- Support `--tag` to auto-tag (resolve tag name to ID first via `GET /api/v1/tags`)
- Support `--expire` duration

For tag name resolution, add a helper to the client:

```go
// In client/client.go
func (c *Client) ResolveTagID(nameOrID string) (int64, error) {
	// Try parsing as int first
	if id, err := strconv.ParseInt(nameOrID, 10, 64); err == nil {
		return id, nil
	}
	// Fetch tags and find by name
	var tags []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := c.GetJSON("/api/v1/tags", &tags); err != nil {
		return 0, err
	}
	for _, t := range tags {
		if t.Name == nameOrID {
			return t.ID, nil
		}
	}
	return 0, fmt.Errorf("tag not found: %s", nameOrID)
}
```

**Step 2: Wire into root command**

In `main.go` `init()`, add: `rootCmd.AddCommand(clipCmd)` (where `clipCmd` is the parent command defined in `clip.go`).

**Step 3: Verify it compiles and test basic commands**

```bash
go build -o build/bin/mp ./cmd/mp
./build/bin/mp clip --help
./build/bin/mp clip list --help
./build/bin/mp clip upload --help
```

**Step 4: Commit**

```bash
git add cmd/mp/clip.go cmd/mp/main.go
git commit -m "feat(cli): add clip commands (list, get, data, upload, delete, rename, archive, expire, download, metadata)"
```

---

## Task 8: CLI — Tag Commands

**Files:**
- Create: `cmd/mp/tag.go`

API mappings:
- `mp tag list` → `GET /api/v1/tags` (with `--children-of` → `GET /api/v1/tags/{id}/children`)
- `mp tag create <name>` → `POST /api/v1/tags`
- `mp tag update <id> --name X --color X` → `PUT /api/v1/tags/{id}`
- `mp tag delete <id>` → `DELETE /api/v1/tags/{id}`
- `mp tag assign <clip-ids...> --tag X` → `PUT /api/v1/clips/{id}/tags/{tagId}` or `POST /api/v1/clips/bulk/tag`
- `mp tag remove <clip-ids...> --tag X` → `DELETE /api/v1/clips/{id}/tags/{tagId}` or `POST /api/v1/clips/bulk/untag`
- `mp tag clips <tag>` → `GET /api/v1/tags/{id}/clips`
- `mp tag hidden` → `GET /api/v1/tags/hidden` (no args) or `PUT /api/v1/tags/hidden` (with `--set`)

Commit: `feat(cli): add tag commands (list, create, update, delete, assign, remove, clips, hidden)`

---

## Task 9: CLI — Dedup, Watch, Plugin Commands

**Files:**
- Create: `cmd/mp/dedup.go`
- Create: `cmd/mp/watch.go`
- Create: `cmd/mp/plugin.go`

**Dedup commands:**
- `mp dedup list` → `GET /api/v1/dedup`
- `mp dedup merge <clip-id>` → `POST /api/v1/dedup/{clipId}/merge`
- `mp dedup all` → `POST /api/v1/dedup/all`

**Watch commands:**
- `mp watch list` → `GET /api/v1/watch`
- `mp watch add <path>` → `POST /api/v1/watch` (with flags: `--filter`, `--presets`, `--regex`, `--auto-tag`, `--auto-archive`, `--process-existing`)
- `mp watch update <id>` → `PUT /api/v1/watch/{id}`
- `mp watch remove <id>` → `DELETE /api/v1/watch/{id}`
- `mp watch pause <id>` / `mp watch pause --global` → `PUT /api/v1/watch/{id}/pause` or `PUT /api/v1/watch/global-pause`
- `mp watch resume <id>` / `mp watch resume --global` → `DELETE /api/v1/watch/{id}/pause` or `DELETE /api/v1/watch/global-pause`
- `mp watch status` → `GET /api/v1/watch/status`
- `mp watch process <id>` → `POST /api/v1/watch/{id}/process`

**Plugin commands:**
- `mp plugin list` → `GET /api/v1/plugins`
- `mp plugin install <source>` → `POST /api/v1/plugins`
- `mp plugin remove <id>` → `DELETE /api/v1/plugins/{id}`
- `mp plugin enable <id>` → `PUT /api/v1/plugins/{id}/enable`
- `mp plugin disable <id>` → `PUT /api/v1/plugins/{id}/disable`
- `mp plugin run <plugin-id> <action-id>` → `POST /api/v1/plugins/{id}/actions/{actionId}` (with `--clip` flags and `--option key=value` flags)
- `mp plugin storage list <id>` → `GET /api/v1/plugins/{id}/storage`
- `mp plugin storage get <id> <key>` → `GET /api/v1/plugins/{id}/storage/{key}`
- `mp plugin storage set <id> <key> <value>` → `PUT /api/v1/plugins/{id}/storage/{key}`
- `mp plugin update --check` → `POST /api/v1/plugins/check-updates`
- `mp plugin update <id>` → `POST /api/v1/plugins/{id}/update`

Commit: `feat(cli): add dedup, watch, and plugin commands`

---

## Task 10: CLI — Serve, API, Backup, Clipboard Commands

**Files:**
- Create: `cmd/mp/serve.go`
- Create: `cmd/mp/api.go`
- Create: `cmd/mp/backup.go`
- Create: `cmd/mp/clipboard.go`

**Serve commands:**
- `mp serve list` → `GET /api/v1/serve`
- `mp serve start <tag> --port N --bind-all --api-access readwrite` → `POST /api/v1/serve`
- `mp serve stop <tag-id>` → `DELETE /api/v1/serve/{tagId}`

**API commands:**
- `mp api status` → `GET /api/v1/status` (or just confirm connectivity)
- `mp api start --port N --bind-all` → needs new endpoint or note that this is done in-app
- `mp api stop` → same note
- `mp api key create <name> --role admin --scope tag-id` → forward to `POST` key creation endpoint
- `mp api key list` → `GET` key list
- `mp api key revoke <id>` → `DELETE` key

**Note on `mp api start/stop`:** These control the API server itself. Since the CLI connects TO the API, `mp api start` is a chicken-and-egg problem. Two options:
1. Skip these commands (user starts API from the GUI)
2. Document that the API must already be running for all other commands; `mp api start/stop` would need a different mechanism (e.g., a control socket). **Recommend option 1 for now** — omit `mp api start/stop` and document that the API is managed from the mahpastes GUI.

**Backup commands:**
- `mp backup create <path>` → `GET /api/v1/backup` (save response to file, or `-` for stdout)
- `mp backup restore <path>` → `POST /api/v1/backup/restore` (upload file, or `-` for stdin)

**Clipboard commands:**
- `mp clipboard copy <id>` → `POST /api/v1/clipboard/copy`
- `mp clipboard copy-file <id>` → `POST /api/v1/clipboard/copy-file`

Commit: `feat(cli): add serve, api, backup, and clipboard commands`

---

## Task 11: Help Text & Shell Completion

Go back through every command and ensure rich help text.

**Files:**
- Modify: all `cmd/mp/*.go` files

**Step 1: Audit every command for help text**

Every command must have:
- `Short`: one-line (shown in parent help listing)
- `Long`: 2-3 sentences explaining purpose, when to use
- `Example`: 2-4 real usage examples with `#` comments

Example pattern:

```go
var clipListCmd = &cobra.Command{
	Use:   "list",
	Short: "List clips with optional filtering",
	Long: `List clips stored in mahpastes with optional tag, archive, and content type filters.
Results are paginated. Use --limit and --offset for pagination, or --all to fetch everything.
Supports sorting by created_at (default), filename, or size.`,
	Example: `  # List recent clips
  mp clip list

  # List archived clips tagged "screenshots"
  mp clip list --tag screenshots --archived

  # Get all clip IDs as JSON for scripting
  mp clip list --json --all | jq -r '.clips[].id'

  # List with pagination
  mp clip list --limit 10 --offset 20`,
	RunE: runClipList,
}
```

**Step 2: Add shell completion command**

Cobra provides this for free. In `main.go`:

```go
var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion scripts",
	Long: `Generate shell completion scripts for mp.

To load completions:

  Bash:
    source <(mp completion bash)
    # or persist:
    mp completion bash > /etc/bash_completion.d/mp

  Zsh:
    mp completion zsh > "${fpath[1]}/_mp"

  Fish:
    mp completion fish > ~/.config/fish/completions/mp.fish

  PowerShell:
    mp completion powershell | Out-String | Invoke-Expression`,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.ExactValidArgs(1),
	DisableFlagsInUseLine: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return rootCmd.GenBashCompletion(os.Stdout)
		case "zsh":
			return rootCmd.GenZshCompletion(os.Stdout)
		case "fish":
			return rootCmd.GenFishCompletion(os.Stdout, true)
		case "powershell":
			return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
		default:
			return fmt.Errorf("unsupported shell: %s", args[0])
		}
	},
}
```

**Step 3: Verify help output**

```bash
go build -o build/bin/mp ./cmd/mp
./build/bin/mp --help
./build/bin/mp clip --help
./build/bin/mp clip upload --help
./build/bin/mp tag --help
./build/bin/mp completion --help
```

**Step 4: Commit**

```bash
git add cmd/mp/
git commit -m "feat(cli): add comprehensive help text and shell completion support"
```

---

## Task 12: Makefile & Build Verification

**Files:**
- Modify: `Makefile`

**Step 1: Verify Makefile targets work**

```bash
make mp
make mp-cross
ls -la build/bin/mp*
```

**Step 2: Test the binary end-to-end**

Start the mahpastes app, start the API, create a key, then:

```bash
export MP_API_KEY=mp_your_test_key
./build/bin/mp clip list
./build/bin/mp clip list --json
./build/bin/mp tag list
./build/bin/mp clip upload test-file.png
./build/bin/mp clip data <id> > /tmp/test-output.png
```

**Step 3: Update CLAUDE.md**

Add CLI section to CLAUDE.md documenting the `mp` binary, its build targets, and basic usage.

**Step 4: Commit**

```bash
git add Makefile CLAUDE.md
git commit -m "feat: finalize mp CLI build targets and documentation"
```

---

## Task 13: API Wiring — Ensure App Has References to All Services

Before the new API endpoints will compile, verify that `App` has access to `ClipboardService` and plugin-related helpers.

**Files:**
- Modify: `app.go` (add fields if needed)
- Modify: `main.go` (wire references)

**Step 1: Check what `App` already has**

Look at the `App` struct and `main.go` to see how services are constructed. Specifically:
- Does `App` have `clipboardService *ClipboardService`?
- How does `PluginService` access the plugin manager?

**Step 2: Add missing references**

If `App` doesn't have `clipboardService`, add:

```go
// In app.go, App struct
clipboardService *ClipboardService
```

And in `main.go`, after creating `ClipboardService`, assign it: `app.clipboardService = clipService`

**Step 3: Add helper methods for plugin operations**

If plugin endpoints call through `PluginService` methods that aren't directly on `App`, add thin wrappers:

```go
func (a *App) getPluginUIActions() (*UIActionsResponse, error) {
	// Delegate to whatever mechanism PluginService uses
}

func (a *App) installPluginFromSource(source string) (*PluginInfo, error) {
	// URL or path install
}

func (a *App) executePluginAction(pluginID int64, actionID string, clipIDs []int64, options map[string]interface{}) (*ActionResult, error) {
	// Delegate to plugin manager
}

func (a *App) getAllPluginStorage(pluginID int64) (map[string]string, error) {
	// Query plugin_storage table
}

func (a *App) getPluginStorage(pluginID int64, key string) (string, error) {
	// Query single key
}

func (a *App) setPluginStorage(pluginID int64, key, value string) error {
	// Upsert
}

func (a *App) updatePlugin(pluginID int64) (*UpdateResult, error) {
	// Delegate to plugin manager
}
```

Check the actual `PluginService` method implementations to see what they delegate to, and replicate that pattern.

**Step 4: Verify everything compiles**

```bash
go build .
go build ./cmd/mp
```

**Step 5: Commit**

```bash
git add app.go main.go
git commit -m "refactor: wire service references for API endpoint access"
```

---

## Task Ordering Notes

**Recommended execution order:**
1. **Task 13** first (wiring) — ensures the App struct has all needed references
2. **Task 1** (CLI scaffold) — can be done in parallel with API work
3. **Tasks 2-6** (API endpoints) — do these in order, each builds on the same file
4. **Tasks 7-10** (CLI commands) — do these after API endpoints exist
5. **Task 11** (help text) — polish pass after commands work
6. **Task 12** (build verification) — final integration check

Tasks 2-6 are independent of Tasks 7-10, so API and CLI work can be parallelized across agents if desired. However, CLI commands depend on their corresponding API endpoints existing, so don't try to test CLI commands until the API is done.

---

## Testing Strategy

Since the existing test infrastructure is Playwright e2e testing the Wails app, and the CLI is a separate binary:

1. **Manual smoke testing** against a running mahpastes instance is the primary validation
2. **API endpoint testing** can be done with `curl` commands after starting the API
3. **CLI integration tests** can be a future addition (shell script or Go test that starts the API, runs `mp` commands, and verifies output)
4. **Existing e2e tests must still pass** — the API changes should not break the Wails app

Run `cd e2e && npm test | tail -50` before and after to verify no regressions.
