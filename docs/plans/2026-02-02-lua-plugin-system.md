# Lua Plugin System Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a sandboxed Lua plugin system that enables automation, integration, and transformation workflows for clips.

**Architecture:** Go-based plugin manager using gopher-lua for VM isolation. Each plugin runs in its own Lua state with restricted API access. Plugins declare permissions in a manifest table, and the system enforces network domain/method restrictions and runtime filesystem approval.

**Tech Stack:** Go, gopher-lua, SQLite (existing), Wails events (existing)

---

## Task 1: Add gopher-lua Dependency

**Files:**
- Modify: `go.mod`
- Modify: `go.sum` (auto-generated)

**Step 1: Add the dependency**

Run:
```bash
cd /Users/egecan/conductor/workspaces/mahpastes/chengdu && go get github.com/yuin/gopher-lua
```

**Step 2: Verify installation**

Run:
```bash
cd /Users/egecan/conductor/workspaces/mahpastes/chengdu && go mod tidy && grep gopher-lua go.mod
```

Expected: Line containing `github.com/yuin/gopher-lua`

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add gopher-lua for plugin system"
```

---

## Task 2: Create Plugin Database Schema

**Files:**
- Modify: `database.go:92-118` (after settings table creation)

**Step 1: Write the schema migration code**

Add to `database.go` after the settings table creation (around line 97):

```go
	// Create plugins table
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS plugins (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		filename TEXT UNIQUE NOT NULL,
		name TEXT NOT NULL,
		version TEXT,
		enabled INTEGER DEFAULT 1,
		status TEXT DEFAULT 'enabled',
		error_count INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		log.Printf("Warning: Failed to create plugins table: %v", err)
	}

	// Create plugin_permissions table
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS plugin_permissions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		plugin_id INTEGER NOT NULL,
		permission_type TEXT NOT NULL,
		path TEXT NOT NULL,
		granted_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (plugin_id) REFERENCES plugins(id) ON DELETE CASCADE
	)`); err != nil {
		log.Printf("Warning: Failed to create plugin_permissions table: %v", err)
	}

	// Create plugin_storage table
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS plugin_storage (
		plugin_id INTEGER NOT NULL,
		key TEXT NOT NULL,
		value BLOB,
		PRIMARY KEY (plugin_id, key),
		FOREIGN KEY (plugin_id) REFERENCES plugins(id) ON DELETE CASCADE
	)`); err != nil {
		log.Printf("Warning: Failed to create plugin_storage table: %v", err)
	}
```

**Step 2: Verify the app compiles**

Run:
```bash
cd /Users/egecan/conductor/workspaces/mahpastes/chengdu && go build .
```

Expected: No errors

**Step 3: Commit**

```bash
git add database.go
git commit -m "schema: add plugin tables for storage and permissions"
```

---

## Task 3: Create Plugin Manifest Parser

**Files:**
- Create: `plugin/manifest.go`

**Step 1: Create the plugin directory**

Run:
```bash
mkdir -p /Users/egecan/conductor/workspaces/mahpastes/chengdu/plugin
```

**Step 2: Write the manifest parser**

Create `plugin/manifest.go`:

```go
package plugin

import (
	"fmt"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

// Manifest represents a parsed plugin manifest
type Manifest struct {
	Name        string
	Version     string
	Description string
	Author      string
	Network     map[string][]string // domain -> allowed methods
	Filesystem  FilesystemPerms
	Events      []string
	Schedules   []Schedule
}

// FilesystemPerms represents filesystem permission requests
type FilesystemPerms struct {
	Read  bool
	Write bool
}

// Schedule represents a scheduled task
type Schedule struct {
	Name     string
	Interval int // seconds
}

// ParseManifest extracts the Plugin table from Lua source
func ParseManifest(source string) (*Manifest, error) {
	L := lua.NewState()
	defer L.Close()

	// Execute the source to populate the Plugin global
	if err := L.DoString(source); err != nil {
		return nil, fmt.Errorf("failed to parse plugin: %w", err)
	}

	// Get the Plugin table
	pluginTable := L.GetGlobal("Plugin")
	if pluginTable == lua.LNil {
		return nil, fmt.Errorf("plugin must define a Plugin table")
	}

	tbl, ok := pluginTable.(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("Plugin must be a table")
	}

	manifest := &Manifest{
		Network: make(map[string][]string),
	}

	// Parse required fields
	if name := tbl.RawGetString("name"); name != lua.LNil {
		manifest.Name = name.String()
	} else {
		return nil, fmt.Errorf("plugin must have a name")
	}

	// Parse optional fields
	if version := tbl.RawGetString("version"); version != lua.LNil {
		manifest.Version = version.String()
	}
	if desc := tbl.RawGetString("description"); desc != lua.LNil {
		manifest.Description = desc.String()
	}
	if author := tbl.RawGetString("author"); author != lua.LNil {
		manifest.Author = author.String()
	}

	// Parse network permissions
	if network := tbl.RawGetString("network"); network != lua.LNil {
		if netTbl, ok := network.(*lua.LTable); ok {
			netTbl.ForEach(func(domain, methods lua.LValue) {
				domainStr := domain.String()
				var methodList []string
				if methodsTbl, ok := methods.(*lua.LTable); ok {
					methodsTbl.ForEach(func(_, method lua.LValue) {
						methodList = append(methodList, strings.ToUpper(method.String()))
					})
				}
				manifest.Network[domainStr] = methodList
			})
		}
	}

	// Parse filesystem permissions
	if fs := tbl.RawGetString("filesystem"); fs != lua.LNil {
		if fsTbl, ok := fs.(*lua.LTable); ok {
			if read := fsTbl.RawGetString("read"); read == lua.LTrue {
				manifest.Filesystem.Read = true
			}
			if write := fsTbl.RawGetString("write"); write == lua.LTrue {
				manifest.Filesystem.Write = true
			}
		}
	}

	// Parse events
	if events := tbl.RawGetString("events"); events != lua.LNil {
		if eventsTbl, ok := events.(*lua.LTable); ok {
			eventsTbl.ForEach(func(_, event lua.LValue) {
				manifest.Events = append(manifest.Events, event.String())
			})
		}
	}

	// Parse schedules
	if schedules := tbl.RawGetString("schedules"); schedules != lua.LNil {
		if schedulesTbl, ok := schedules.(*lua.LTable); ok {
			schedulesTbl.ForEach(func(_, sched lua.LValue) {
				if schedTbl, ok := sched.(*lua.LTable); ok {
					schedule := Schedule{}
					if name := schedTbl.RawGetString("name"); name != lua.LNil {
						schedule.Name = name.String()
					}
					if interval := schedTbl.RawGetString("interval"); interval != lua.LNil {
						if num, ok := interval.(lua.LNumber); ok {
							schedule.Interval = int(num)
						}
					}
					if schedule.Name != "" && schedule.Interval > 0 {
						manifest.Schedules = append(manifest.Schedules, schedule)
					}
				}
			})
		}
	}

	return manifest, nil
}

// ValidEvents returns the list of valid event names
func ValidEvents() []string {
	return []string{
		"app:startup",
		"app:shutdown",
		"clip:created",
		"clip:deleted",
		"clip:archived",
		"watch:file_detected",
		"watch:import_complete",
	}
}

// IsValidEvent checks if an event name is valid
func IsValidEvent(event string) bool {
	for _, valid := range ValidEvents() {
		if event == valid {
			return true
		}
	}
	return false
}
```

**Step 3: Verify compilation**

Run:
```bash
cd /Users/egecan/conductor/workspaces/mahpastes/chengdu && go build ./plugin/...
```

Expected: No errors

**Step 4: Commit**

```bash
git add plugin/manifest.go
git commit -m "feat(plugin): add manifest parser for Plugin table"
```

---

## Task 4: Create Lua VM Sandbox

**Files:**
- Create: `plugin/sandbox.go`

**Step 1: Write the sandbox implementation**

Create `plugin/sandbox.go`:

```go
package plugin

import (
	"context"
	"fmt"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"
)

const (
	MaxExecutionTime = 30 * time.Second
	MaxMemoryMB      = 50
)

// Sandbox wraps a Lua state with resource limits
type Sandbox struct {
	L        *lua.LState
	manifest *Manifest
	pluginID int64
	mu       sync.Mutex
	cancel   context.CancelFunc
}

// NewSandbox creates a new sandboxed Lua environment
func NewSandbox(manifest *Manifest, pluginID int64) *Sandbox {
	L := lua.NewState(lua.Options{
		SkipOpenLibs: true,
	})

	// Open only safe libraries
	lua.OpenBase(L)
	lua.OpenTable(L)
	lua.OpenString(L)
	lua.OpenMath(L)

	// Remove dangerous functions from base
	L.SetGlobal("dofile", lua.LNil)
	L.SetGlobal("loadfile", lua.LNil)
	L.SetGlobal("load", lua.LNil)
	L.SetGlobal("loadstring", lua.LNil)
	L.SetGlobal("rawequal", lua.LNil)
	L.SetGlobal("rawget", lua.LNil)
	L.SetGlobal("rawset", lua.LNil)
	L.SetGlobal("collectgarbage", lua.LNil)

	return &Sandbox{
		L:        L,
		manifest: manifest,
		pluginID: pluginID,
	}
}

// Close shuts down the sandbox
func (s *Sandbox) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cancel != nil {
		s.cancel()
	}
	s.L.Close()
}

// LoadSource loads and executes the plugin source
func (s *Sandbox) LoadSource(source string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.L.DoString(source)
}

// CallHandler calls a handler function with timeout
func (s *Sandbox) CallHandler(name string, args ...lua.LValue) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	fn := s.L.GetGlobal(name)
	if fn == lua.LNil {
		return nil // Handler not defined, skip silently
	}

	if _, ok := fn.(*lua.LFunction); !ok {
		return fmt.Errorf("%s is not a function", name)
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), MaxExecutionTime)
	s.cancel = cancel
	defer func() {
		cancel()
		s.cancel = nil
	}()

	// Set up cancellation check
	s.L.SetContext(ctx)

	// Push function and arguments
	s.L.Push(fn)
	for _, arg := range args {
		s.L.Push(arg)
	}

	// Call with error handling
	err := s.L.PCall(len(args), 0, nil)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("handler %s timed out after %v", name, MaxExecutionTime)
		}
		return fmt.Errorf("handler %s failed: %w", name, err)
	}

	return nil
}

// SetGlobalTable sets a table as a global
func (s *Sandbox) SetGlobalTable(name string, tbl *lua.LTable) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.L.SetGlobal(name, tbl)
}

// SetGlobalFunction sets a function as a global
func (s *Sandbox) SetGlobalFunction(name string, fn lua.LGFunction) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.L.SetGlobal(name, s.L.NewFunction(fn))
}

// GetState returns the underlying Lua state (for API registration)
func (s *Sandbox) GetState() *lua.LState {
	return s.L
}

// GetManifest returns the plugin manifest
func (s *Sandbox) GetManifest() *Manifest {
	return s.manifest
}

// GetPluginID returns the plugin database ID
func (s *Sandbox) GetPluginID() int64 {
	return s.pluginID
}
```

**Step 2: Verify compilation**

Run:
```bash
cd /Users/egecan/conductor/workspaces/mahpastes/chengdu && go build ./plugin/...
```

Expected: No errors

**Step 3: Commit**

```bash
git add plugin/sandbox.go
git commit -m "feat(plugin): add sandboxed Lua VM with resource limits"
```

---

## Task 5: Create Storage API

**Files:**
- Create: `plugin/api_storage.go`

**Step 1: Write the storage API**

Create `plugin/api_storage.go`:

```go
package plugin

import (
	"database/sql"

	lua "github.com/yuin/gopher-lua"
)

// StorageAPI provides plugin-local key-value storage
type StorageAPI struct {
	db       *sql.DB
	pluginID int64
}

// NewStorageAPI creates a new storage API instance
func NewStorageAPI(db *sql.DB, pluginID int64) *StorageAPI {
	return &StorageAPI{
		db:       db,
		pluginID: pluginID,
	}
}

// Register adds the storage module to the Lua state
func (s *StorageAPI) Register(L *lua.LState) {
	storageMod := L.NewTable()

	storageMod.RawSetString("get", L.NewFunction(s.get))
	storageMod.RawSetString("set", L.NewFunction(s.set))
	storageMod.RawSetString("delete", L.NewFunction(s.delete))

	L.SetGlobal("storage", storageMod)
}

func (s *StorageAPI) get(L *lua.LState) int {
	key := L.CheckString(1)

	var value []byte
	err := s.db.QueryRow(
		"SELECT value FROM plugin_storage WHERE plugin_id = ? AND key = ?",
		s.pluginID, key,
	).Scan(&value)

	if err == sql.ErrNoRows {
		L.Push(lua.LNil)
		return 1
	}
	if err != nil {
		L.Push(lua.LNil)
		return 1
	}

	L.Push(lua.LString(string(value)))
	return 1
}

func (s *StorageAPI) set(L *lua.LState) int {
	key := L.CheckString(1)
	value := L.CheckString(2)

	_, err := s.db.Exec(`
		INSERT INTO plugin_storage (plugin_id, key, value)
		VALUES (?, ?, ?)
		ON CONFLICT(plugin_id, key) DO UPDATE SET value = excluded.value
	`, s.pluginID, key, []byte(value))

	if err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LTrue)
	return 1
}

func (s *StorageAPI) delete(L *lua.LState) int {
	key := L.CheckString(1)

	_, err := s.db.Exec(
		"DELETE FROM plugin_storage WHERE plugin_id = ? AND key = ?",
		s.pluginID, key,
	)

	if err != nil {
		L.Push(lua.LFalse)
		return 1
	}

	L.Push(lua.LTrue)
	return 1
}
```

**Step 2: Verify compilation**

Run:
```bash
cd /Users/egecan/conductor/workspaces/mahpastes/chengdu && go build ./plugin/...
```

Expected: No errors

**Step 3: Commit**

```bash
git add plugin/api_storage.go
git commit -m "feat(plugin): add storage API for plugin-local persistence"
```

---

## Task 6: Create Clips API

**Files:**
- Create: `plugin/api_clips.go`

**Step 1: Write the clips API**

Create `plugin/api_clips.go`:

```go
package plugin

import (
	"database/sql"
	"encoding/base64"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// ClipsAPI provides clip CRUD operations to plugins
type ClipsAPI struct {
	db *sql.DB
}

// NewClipsAPI creates a new clips API instance
func NewClipsAPI(db *sql.DB) *ClipsAPI {
	return &ClipsAPI{db: db}
}

// Register adds the clips module to the Lua state
func (c *ClipsAPI) Register(L *lua.LState) {
	clipsMod := L.NewTable()

	clipsMod.RawSetString("list", L.NewFunction(c.list))
	clipsMod.RawSetString("get", L.NewFunction(c.get))
	clipsMod.RawSetString("create", L.NewFunction(c.create))
	clipsMod.RawSetString("update", L.NewFunction(c.update))
	clipsMod.RawSetString("delete", L.NewFunction(c.deleteClip))
	clipsMod.RawSetString("delete_many", L.NewFunction(c.deleteMany))
	clipsMod.RawSetString("archive", L.NewFunction(c.archive))
	clipsMod.RawSetString("unarchive", L.NewFunction(c.unarchive))

	L.SetGlobal("clips", clipsMod)
}

func (c *ClipsAPI) list(L *lua.LState) int {
	// Optional filter table
	var contentTypeFilter string
	if L.GetTop() >= 1 {
		if filter, ok := L.Get(1).(*lua.LTable); ok {
			if ct := filter.RawGetString("content_type"); ct != lua.LNil {
				contentTypeFilter = ct.String()
			}
		}
	}

	query := `SELECT id, content_type, filename, created_at, is_archived
	          FROM clips WHERE (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)`
	args := []interface{}{}

	if contentTypeFilter != "" {
		query += " AND content_type LIKE ?"
		args = append(args, contentTypeFilter)
	}
	query += " ORDER BY created_at DESC LIMIT 100"

	rows, err := c.db.Query(query, args...)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	defer rows.Close()

	result := L.NewTable()
	for rows.Next() {
		var id int64
		var contentType string
		var filename sql.NullString
		var createdAt time.Time
		var isArchived int

		if err := rows.Scan(&id, &contentType, &filename, &createdAt, &isArchived); err != nil {
			continue
		}

		clip := L.NewTable()
		clip.RawSetString("id", lua.LNumber(id))
		clip.RawSetString("content_type", lua.LString(contentType))
		clip.RawSetString("filename", lua.LString(filename.String))
		clip.RawSetString("created_at", lua.LNumber(createdAt.Unix()))
		clip.RawSetString("is_archived", lua.LBool(isArchived == 1))

		result.Append(clip)
	}

	L.Push(result)
	return 1
}

func (c *ClipsAPI) get(L *lua.LState) int {
	id := L.CheckInt64(1)

	var contentType string
	var data []byte
	var filename sql.NullString
	var createdAt time.Time
	var isArchived int

	err := c.db.QueryRow(`
		SELECT content_type, data, filename, created_at, is_archived
		FROM clips WHERE id = ?
	`, id).Scan(&contentType, &data, &filename, &createdAt, &isArchived)

	if err == sql.ErrNoRows {
		L.Push(lua.LNil)
		return 1
	}
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	clip := L.NewTable()
	clip.RawSetString("id", lua.LNumber(id))
	clip.RawSetString("content_type", lua.LString(contentType))
	clip.RawSetString("filename", lua.LString(filename.String))
	clip.RawSetString("created_at", lua.LNumber(createdAt.Unix()))
	clip.RawSetString("is_archived", lua.LBool(isArchived == 1))

	// For text content, return as-is; for binary, base64 encode
	if strings.HasPrefix(contentType, "text/") || contentType == "application/json" {
		clip.RawSetString("data", lua.LString(string(data)))
	} else {
		clip.RawSetString("data", lua.LString(base64.StdEncoding.EncodeToString(data)))
		clip.RawSetString("data_encoding", lua.LString("base64"))
	}

	L.Push(clip)
	return 1
}

func (c *ClipsAPI) create(L *lua.LState) int {
	opts := L.CheckTable(1)

	dataVal := opts.RawGetString("data")
	if dataVal == lua.LNil {
		L.Push(lua.LNil)
		L.Push(lua.LString("data is required"))
		return 2
	}
	dataStr := dataVal.String()

	contentType := "application/octet-stream"
	if ct := opts.RawGetString("content_type"); ct != lua.LNil {
		contentType = ct.String()
	}

	var filename string
	if fn := opts.RawGetString("filename"); fn != lua.LNil {
		filename = fn.String()
	}

	// Decode if base64 encoded
	var data []byte
	if enc := opts.RawGetString("data_encoding"); enc != lua.LNil && enc.String() == "base64" {
		var err error
		data, err = base64.StdEncoding.DecodeString(dataStr)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("invalid base64 data"))
			return 2
		}
	} else {
		data = []byte(dataStr)
	}

	result, err := c.db.Exec(
		"INSERT INTO clips (content_type, data, filename) VALUES (?, ?, ?)",
		contentType, data, filename,
	)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	id, _ := result.LastInsertId()
	L.Push(lua.LNumber(id))
	return 1
}

func (c *ClipsAPI) update(L *lua.LState) int {
	id := L.CheckInt64(1)
	opts := L.CheckTable(2)

	// Only allow updating is_archived for now
	if archived := opts.RawGetString("is_archived"); archived != lua.LNil {
		archivedInt := 0
		if archived == lua.LTrue {
			archivedInt = 1
		}
		_, err := c.db.Exec("UPDATE clips SET is_archived = ? WHERE id = ?", archivedInt, id)
		if err != nil {
			L.Push(lua.LFalse)
			L.Push(lua.LString(err.Error()))
			return 2
		}
	}

	L.Push(lua.LTrue)
	return 1
}

func (c *ClipsAPI) deleteClip(L *lua.LState) int {
	id := L.CheckInt64(1)

	_, err := c.db.Exec("DELETE FROM clips WHERE id = ?", id)
	if err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LTrue)
	return 1
}

func (c *ClipsAPI) deleteMany(L *lua.LState) int {
	ids := L.CheckTable(1)

	var idList []int64
	ids.ForEach(func(_, v lua.LValue) {
		if num, ok := v.(lua.LNumber); ok {
			idList = append(idList, int64(num))
		}
	})

	if len(idList) == 0 {
		L.Push(lua.LTrue)
		return 1
	}

	// Build query with placeholders
	placeholders := make([]string, len(idList))
	args := make([]interface{}, len(idList))
	for i, id := range idList {
		placeholders[i] = "?"
		args[i] = id
	}

	query := "DELETE FROM clips WHERE id IN (" + strings.Join(placeholders, ",") + ")"
	_, err := c.db.Exec(query, args...)
	if err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LTrue)
	return 1
}

func (c *ClipsAPI) archive(L *lua.LState) int {
	id := L.CheckInt64(1)

	_, err := c.db.Exec("UPDATE clips SET is_archived = 1 WHERE id = ?", id)
	if err != nil {
		L.Push(lua.LFalse)
		return 1
	}

	L.Push(lua.LTrue)
	return 1
}

func (c *ClipsAPI) unarchive(L *lua.LState) int {
	id := L.CheckInt64(1)

	_, err := c.db.Exec("UPDATE clips SET is_archived = 0 WHERE id = ?", id)
	if err != nil {
		L.Push(lua.LFalse)
		return 1
	}

	L.Push(lua.LTrue)
	return 1
}
```

**Step 2: Verify compilation**

Run:
```bash
cd /Users/egecan/conductor/workspaces/mahpastes/chengdu && go build ./plugin/...
```

Expected: No errors

**Step 3: Commit**

```bash
git add plugin/api_clips.go
git commit -m "feat(plugin): add clips API for CRUD operations"
```

---

## Task 7: Create HTTP API with Domain/Method Restrictions

**Files:**
- Create: `plugin/api_http.go`

**Step 1: Write the HTTP API**

Create `plugin/api_http.go`:

```go
package plugin

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"
)

const (
	HTTPRequestsPerMinute = 100
	HTTPTimeout           = 30 * time.Second
)

// HTTPAPI provides restricted HTTP access to plugins
type HTTPAPI struct {
	allowedDomains map[string][]string // domain -> allowed methods
	client         *http.Client
	requestCount   int
	lastReset      time.Time
	mu             sync.Mutex
}

// NewHTTPAPI creates a new HTTP API with domain restrictions
func NewHTTPAPI(allowedDomains map[string][]string) *HTTPAPI {
	return &HTTPAPI{
		allowedDomains: allowedDomains,
		client: &http.Client{
			Timeout: HTTPTimeout,
		},
		lastReset: time.Now(),
	}
}

// Register adds the http module to the Lua state
func (h *HTTPAPI) Register(L *lua.LState) {
	httpMod := L.NewTable()

	httpMod.RawSetString("get", L.NewFunction(h.makeRequest("GET")))
	httpMod.RawSetString("post", L.NewFunction(h.makeRequest("POST")))
	httpMod.RawSetString("put", L.NewFunction(h.makeRequest("PUT")))
	httpMod.RawSetString("patch", L.NewFunction(h.makeRequest("PATCH")))
	httpMod.RawSetString("delete", L.NewFunction(h.makeRequest("DELETE")))

	L.SetGlobal("http", httpMod)
}

func (h *HTTPAPI) checkRateLimit() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now()
	if now.Sub(h.lastReset) >= time.Minute {
		h.requestCount = 0
		h.lastReset = now
	}

	if h.requestCount >= HTTPRequestsPerMinute {
		return fmt.Errorf("rate limit exceeded: %d requests per minute", HTTPRequestsPerMinute)
	}

	h.requestCount++
	return nil
}

func (h *HTTPAPI) checkDomainPermission(urlStr, method string) error {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	domain := parsed.Host
	// Strip port if present
	if idx := strings.LastIndex(domain, ":"); idx != -1 {
		domain = domain[:idx]
	}

	allowedMethods, ok := h.allowedDomains[domain]
	if !ok {
		return fmt.Errorf("domain not in allowlist: %s", domain)
	}

	for _, allowed := range allowedMethods {
		if strings.EqualFold(allowed, method) {
			return nil
		}
	}

	return fmt.Errorf("%s not allowed for domain %s (allowed: %v)", method, domain, allowedMethods)
}

func (h *HTTPAPI) makeRequest(method string) lua.LGFunction {
	return func(L *lua.LState) int {
		urlStr := L.CheckString(1)

		// Check permissions
		if err := h.checkDomainPermission(urlStr, method); err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}

		// Check rate limit
		if err := h.checkRateLimit(); err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}

		// Parse options
		var body io.Reader
		headers := make(map[string]string)

		if L.GetTop() >= 2 {
			if opts, ok := L.Get(2).(*lua.LTable); ok {
				// Get body
				if bodyVal := opts.RawGetString("body"); bodyVal != lua.LNil {
					body = strings.NewReader(bodyVal.String())
				}

				// Get headers
				if headersVal := opts.RawGetString("headers"); headersVal != lua.LNil {
					if headersTbl, ok := headersVal.(*lua.LTable); ok {
						headersTbl.ForEach(func(k, v lua.LValue) {
							headers[k.String()] = v.String()
						})
					}
				}
			}
		}

		// Create request
		req, err := http.NewRequest(method, urlStr, body)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}

		// Set headers
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		// Execute request
		resp, err := h.client.Do(req)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		defer resp.Body.Close()

		// Read body
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}

		// Build response table
		result := L.NewTable()
		result.RawSetString("status", lua.LNumber(resp.StatusCode))
		result.RawSetString("body", lua.LString(string(respBody)))

		// Response headers
		respHeaders := L.NewTable()
		for k, v := range resp.Header {
			if len(v) > 0 {
				respHeaders.RawSetString(k, lua.LString(v[0]))
			}
		}
		result.RawSetString("headers", respHeaders)

		L.Push(result)
		return 1
	}
}

// Ensure bytes package is used (for future binary body support)
var _ = bytes.NewReader
```

**Step 2: Verify compilation**

Run:
```bash
cd /Users/egecan/conductor/workspaces/mahpastes/chengdu && go build ./plugin/...
```

Expected: No errors

**Step 3: Commit**

```bash
git add plugin/api_http.go
git commit -m "feat(plugin): add HTTP API with domain/method restrictions"
```

---

## Task 8: Create Filesystem API with Runtime Approval

**Files:**
- Create: `plugin/api_fs.go`

**Step 1: Write the filesystem API**

Create `plugin/api_fs.go`:

```go
package plugin

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"
)

const (
	FSOperationsPerMinute = 50
)

// PermissionCallback is called when a plugin needs filesystem access
// Returns the approved path (may be different from requested) or empty string if denied
type PermissionCallback func(pluginName string, permType string, requestedPath string) string

// FilesystemAPI provides restricted filesystem access to plugins
type FilesystemAPI struct {
	db               *sql.DB
	pluginID         int64
	pluginName       string
	wantsRead        bool
	wantsWrite       bool
	permCallback     PermissionCallback
	approvedPaths    map[string]string // permType:path -> approved path
	operationCount   int
	lastReset        time.Time
	mu               sync.Mutex
}

// NewFilesystemAPI creates a new filesystem API
func NewFilesystemAPI(db *sql.DB, pluginID int64, pluginName string, perms FilesystemPerms, callback PermissionCallback) *FilesystemAPI {
	api := &FilesystemAPI{
		db:            db,
		pluginID:      pluginID,
		pluginName:    pluginName,
		wantsRead:     perms.Read,
		wantsWrite:    perms.Write,
		permCallback:  callback,
		approvedPaths: make(map[string]string),
		lastReset:     time.Now(),
	}

	// Load existing permissions from DB
	api.loadPermissions()

	return api
}

func (f *FilesystemAPI) loadPermissions() {
	rows, err := f.db.Query(
		"SELECT permission_type, path FROM plugin_permissions WHERE plugin_id = ?",
		f.pluginID,
	)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var permType, path string
		if err := rows.Scan(&permType, &path); err != nil {
			continue
		}
		f.approvedPaths[permType+":"+path] = path
	}
}

func (f *FilesystemAPI) checkRateLimit() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	now := time.Now()
	if now.Sub(f.lastReset) >= time.Minute {
		f.operationCount = 0
		f.lastReset = now
	}

	if f.operationCount >= FSOperationsPerMinute {
		return fmt.Errorf("rate limit exceeded: %d operations per minute", FSOperationsPerMinute)
	}

	f.operationCount++
	return nil
}

func (f *FilesystemAPI) checkPermission(permType, path string) (string, error) {
	// Normalize path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}

	// Check if plugin declared this permission type
	if permType == "fs_read" && !f.wantsRead {
		return "", fmt.Errorf("plugin did not declare filesystem.read permission")
	}
	if permType == "fs_write" && !f.wantsWrite {
		return "", fmt.Errorf("plugin did not declare filesystem.write permission")
	}

	// Check if path is already approved
	key := permType + ":" + absPath
	if approved, ok := f.approvedPaths[key]; ok {
		return approved, nil
	}

	// Check if any parent directory is approved
	dir := absPath
	for {
		parentKey := permType + ":" + dir
		if approved, ok := f.approvedPaths[parentKey]; ok {
			// Path is under an approved directory
			return absPath, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// Need to request permission
	if f.permCallback == nil {
		return "", fmt.Errorf("filesystem access not available")
	}

	approved := f.permCallback(f.pluginName, permType, absPath)
	if approved == "" {
		return "", fmt.Errorf("permission denied for %s", absPath)
	}

	// Save to DB and cache
	_, err = f.db.Exec(
		"INSERT INTO plugin_permissions (plugin_id, permission_type, path) VALUES (?, ?, ?)",
		f.pluginID, permType, approved,
	)
	if err == nil {
		f.approvedPaths[permType+":"+approved] = approved
	}

	// Check if the requested path is under the approved path
	if !isSubPath(approved, absPath) {
		return "", fmt.Errorf("approved path %s does not cover requested path %s", approved, absPath)
	}

	return absPath, nil
}

func isSubPath(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return !filepath.IsAbs(rel) && rel != ".." && !startsWithParent(rel)
}

func startsWithParent(path string) bool {
	return len(path) >= 2 && path[:2] == ".."
}

// Register adds the fs module to the Lua state
func (f *FilesystemAPI) Register(L *lua.LState) {
	fsMod := L.NewTable()

	fsMod.RawSetString("read", L.NewFunction(f.read))
	fsMod.RawSetString("write", L.NewFunction(f.write))
	fsMod.RawSetString("list", L.NewFunction(f.list))
	fsMod.RawSetString("exists", L.NewFunction(f.exists))

	L.SetGlobal("fs", fsMod)
}

func (f *FilesystemAPI) read(L *lua.LState) int {
	path := L.CheckString(1)

	if err := f.checkRateLimit(); err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	approvedPath, err := f.checkPermission("fs_read", path)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	data, err := os.ReadFile(approvedPath)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LString(string(data)))
	return 1
}

func (f *FilesystemAPI) write(L *lua.LState) int {
	path := L.CheckString(1)
	content := L.CheckString(2)

	if err := f.checkRateLimit(); err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	approvedPath, err := f.checkPermission("fs_write", path)
	if err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	// Ensure parent directory exists
	dir := filepath.Dir(approvedPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	if err := os.WriteFile(approvedPath, []byte(content), 0644); err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LTrue)
	return 1
}

func (f *FilesystemAPI) list(L *lua.LState) int {
	path := L.CheckString(1)

	if err := f.checkRateLimit(); err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	approvedPath, err := f.checkPermission("fs_read", path)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	entries, err := os.ReadDir(approvedPath)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	result := L.NewTable()
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		item := L.NewTable()
		item.RawSetString("name", lua.LString(entry.Name()))
		item.RawSetString("is_dir", lua.LBool(entry.IsDir()))
		item.RawSetString("size", lua.LNumber(info.Size()))
		item.RawSetString("modified", lua.LNumber(info.ModTime().Unix()))

		result.Append(item)
	}

	L.Push(result)
	return 1
}

func (f *FilesystemAPI) exists(L *lua.LState) int {
	path := L.CheckString(1)

	// exists() doesn't require permission - just returns bool
	absPath, err := filepath.Abs(path)
	if err != nil {
		L.Push(lua.LFalse)
		return 1
	}

	_, err = os.Stat(absPath)
	L.Push(lua.LBool(err == nil))
	return 1
}
```

**Step 2: Verify compilation**

Run:
```bash
cd /Users/egecan/conductor/workspaces/mahpastes/chengdu && go build ./plugin/...
```

Expected: No errors

**Step 3: Commit**

```bash
git add plugin/api_fs.go
git commit -m "feat(plugin): add filesystem API with runtime permission approval"
```

---

## Task 9: Create Utility APIs (log, json, base64)

**Files:**
- Create: `plugin/api_utils.go`

**Step 1: Write the utility APIs**

Create `plugin/api_utils.go`:

```go
package plugin

import (
	"encoding/base64"
	"encoding/json"
	"log"

	lua "github.com/yuin/gopher-lua"
)

// UtilsAPI provides utility functions to plugins
type UtilsAPI struct {
	pluginName string
}

// NewUtilsAPI creates a new utils API
func NewUtilsAPI(pluginName string) *UtilsAPI {
	return &UtilsAPI{pluginName: pluginName}
}

// Register adds utility functions to the Lua state
func (u *UtilsAPI) Register(L *lua.LState) {
	// log function
	L.SetGlobal("log", L.NewFunction(u.logFn))

	// json module
	jsonMod := L.NewTable()
	jsonMod.RawSetString("encode", L.NewFunction(u.jsonEncode))
	jsonMod.RawSetString("decode", L.NewFunction(u.jsonDecode))
	L.SetGlobal("json", jsonMod)

	// base64 module
	b64Mod := L.NewTable()
	b64Mod.RawSetString("encode", L.NewFunction(u.base64Encode))
	b64Mod.RawSetString("decode", L.NewFunction(u.base64Decode))
	L.SetGlobal("base64", b64Mod)
}

func (u *UtilsAPI) logFn(L *lua.LState) int {
	msg := L.CheckString(1)
	log.Printf("[plugin:%s] %s", u.pluginName, msg)
	return 0
}

func (u *UtilsAPI) jsonEncode(L *lua.LState) int {
	val := L.Get(1)
	goVal := luaToGo(val)

	data, err := json.Marshal(goVal)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LString(string(data)))
	return 1
}

func (u *UtilsAPI) jsonDecode(L *lua.LState) int {
	str := L.CheckString(1)

	var goVal interface{}
	if err := json.Unmarshal([]byte(str), &goVal); err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(goToLua(L, goVal))
	return 1
}

func (u *UtilsAPI) base64Encode(L *lua.LState) int {
	data := L.CheckString(1)
	encoded := base64.StdEncoding.EncodeToString([]byte(data))
	L.Push(lua.LString(encoded))
	return 1
}

func (u *UtilsAPI) base64Decode(L *lua.LState) int {
	encoded := L.CheckString(1)
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	L.Push(lua.LString(string(data)))
	return 1
}

// luaToGo converts a Lua value to a Go value
func luaToGo(val lua.LValue) interface{} {
	switch v := val.(type) {
	case lua.LBool:
		return bool(v)
	case lua.LNumber:
		return float64(v)
	case lua.LString:
		return string(v)
	case *lua.LTable:
		// Check if it's an array or map
		isArray := true
		maxIndex := 0
		v.ForEach(func(k, _ lua.LValue) {
			if num, ok := k.(lua.LNumber); ok {
				idx := int(num)
				if idx > maxIndex {
					maxIndex = idx
				}
			} else {
				isArray = false
			}
		})

		if isArray && maxIndex > 0 {
			arr := make([]interface{}, maxIndex)
			v.ForEach(func(k, val lua.LValue) {
				if num, ok := k.(lua.LNumber); ok {
					idx := int(num) - 1 // Lua is 1-indexed
					if idx >= 0 && idx < maxIndex {
						arr[idx] = luaToGo(val)
					}
				}
			})
			return arr
		}

		m := make(map[string]interface{})
		v.ForEach(func(k, val lua.LValue) {
			m[k.String()] = luaToGo(val)
		})
		return m
	case *lua.LNilType:
		return nil
	default:
		return nil
	}
}

// goToLua converts a Go value to a Lua value
func goToLua(L *lua.LState, val interface{}) lua.LValue {
	switch v := val.(type) {
	case nil:
		return lua.LNil
	case bool:
		return lua.LBool(v)
	case float64:
		return lua.LNumber(v)
	case string:
		return lua.LString(v)
	case []interface{}:
		tbl := L.NewTable()
		for i, item := range v {
			tbl.RawSetInt(i+1, goToLua(L, item))
		}
		return tbl
	case map[string]interface{}:
		tbl := L.NewTable()
		for k, item := range v {
			tbl.RawSetString(k, goToLua(L, item))
		}
		return tbl
	default:
		return lua.LNil
	}
}
```

**Step 2: Verify compilation**

Run:
```bash
cd /Users/egecan/conductor/workspaces/mahpastes/chengdu && go build ./plugin/...
```

Expected: No errors

**Step 3: Commit**

```bash
git add plugin/api_utils.go
git commit -m "feat(plugin): add utility APIs (log, json, base64)"
```

---

## Task 10: Create Scheduler for Scheduled Tasks

**Files:**
- Create: `plugin/scheduler.go`

**Step 1: Write the scheduler**

Create `plugin/scheduler.go`:

```go
package plugin

import (
	"log"
	"sync"
	"time"
)

// ScheduledTask represents a running scheduled task
type ScheduledTask struct {
	name     string
	interval time.Duration
	sandbox  *Sandbox
	stopCh   chan struct{}
	running  bool
	mu       sync.Mutex
}

// Scheduler manages scheduled tasks for plugins
type Scheduler struct {
	tasks map[string]*ScheduledTask // key: pluginID:taskName
	mu    sync.RWMutex
}

// NewScheduler creates a new scheduler
func NewScheduler() *Scheduler {
	return &Scheduler{
		tasks: make(map[string]*ScheduledTask),
	}
}

// AddTask adds a scheduled task
func (s *Scheduler) AddTask(pluginID int64, taskName string, interval int, sandbox *Sandbox) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := taskKey(pluginID, taskName)

	// Stop existing task if any
	if existing, ok := s.tasks[key]; ok {
		existing.Stop()
	}

	task := &ScheduledTask{
		name:     taskName,
		interval: time.Duration(interval) * time.Second,
		sandbox:  sandbox,
		stopCh:   make(chan struct{}),
	}

	s.tasks[key] = task
	go task.run()
}

// RemovePluginTasks removes all tasks for a plugin
func (s *Scheduler) RemovePluginTasks(pluginID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	prefix := taskKeyPrefix(pluginID)
	for key, task := range s.tasks {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			task.Stop()
			delete(s.tasks, key)
		}
	}
}

// StopAll stops all scheduled tasks
func (s *Scheduler) StopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, task := range s.tasks {
		task.Stop()
	}
	s.tasks = make(map[string]*ScheduledTask)
}

func taskKey(pluginID int64, taskName string) string {
	return string(rune(pluginID)) + ":" + taskName
}

func taskKeyPrefix(pluginID int64) string {
	return string(rune(pluginID)) + ":"
}

func (t *ScheduledTask) run() {
	t.mu.Lock()
	t.running = true
	t.mu.Unlock()

	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()

	for {
		select {
		case <-t.stopCh:
			return
		case <-ticker.C:
			t.execute()
		}
	}
}

func (t *ScheduledTask) execute() {
	t.mu.Lock()
	if !t.running {
		t.mu.Unlock()
		return
	}
	t.mu.Unlock()

	// Call the handler function named after the task
	if err := t.sandbox.CallHandler(t.name); err != nil {
		log.Printf("Scheduled task %s failed: %v", t.name, err)
	}
}

// Stop stops the scheduled task
func (t *ScheduledTask) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.running {
		t.running = false
		close(t.stopCh)
	}
}
```

**Step 2: Verify compilation**

Run:
```bash
cd /Users/egecan/conductor/workspaces/mahpastes/chengdu && go build ./plugin/...
```

Expected: No errors

**Step 3: Commit**

```bash
git add plugin/scheduler.go
git commit -m "feat(plugin): add scheduler for periodic tasks"
```

---

## Task 11: Create Plugin Manager

**Files:**
- Create: `plugin/manager.go`

**Step 1: Write the plugin manager**

Create `plugin/manager.go`:

```go
package plugin

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	lua "github.com/yuin/gopher-lua"
)

const (
	MaxConsecutiveErrors = 3
)

// Plugin represents a loaded plugin
type Plugin struct {
	ID       int64
	Filename string
	Name     string
	Version  string
	Enabled  bool
	Status   string
	Manifest *Manifest
	Sandbox  *Sandbox
}

// Manager manages all plugins
type Manager struct {
	ctx              context.Context
	db               *sql.DB
	plugins          map[int64]*Plugin
	eventSubscribers map[string][]int64 // event -> plugin IDs
	scheduler        *Scheduler
	permCallback     PermissionCallback
	mu               sync.RWMutex
	pluginsDir       string
}

// NewManager creates a new plugin manager
func NewManager(ctx context.Context, db *sql.DB, pluginsDir string) (*Manager, error) {
	// Ensure plugins directory exists
	if err := os.MkdirAll(pluginsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create plugins directory: %w", err)
	}

	m := &Manager{
		ctx:              ctx,
		db:               db,
		plugins:          make(map[int64]*Plugin),
		eventSubscribers: make(map[string][]int64),
		scheduler:        NewScheduler(),
		pluginsDir:       pluginsDir,
	}

	return m, nil
}

// SetPermissionCallback sets the callback for filesystem permission requests
func (m *Manager) SetPermissionCallback(callback PermissionCallback) {
	m.permCallback = callback
}

// LoadPlugins loads all enabled plugins from the database
func (m *Manager) LoadPlugins() error {
	rows, err := m.db.Query(`
		SELECT id, filename, name, version, enabled, status
		FROM plugins WHERE enabled = 1 AND status != 'error'
	`)
	if err != nil {
		return fmt.Errorf("failed to query plugins: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var p Plugin
		var enabled int
		if err := rows.Scan(&p.ID, &p.Filename, &p.Name, &p.Version, &enabled, &p.Status); err != nil {
			log.Printf("Failed to scan plugin row: %v", err)
			continue
		}
		p.Enabled = enabled == 1

		if err := m.loadPlugin(&p); err != nil {
			log.Printf("Failed to load plugin %s: %v", p.Name, err)
			m.incrementErrorCount(p.ID)
			continue
		}
	}

	return nil
}

func (m *Manager) loadPlugin(p *Plugin) error {
	// Read plugin source
	sourcePath := filepath.Join(m.pluginsDir, p.Filename)
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to read plugin file: %w", err)
	}

	// Parse manifest
	manifest, err := ParseManifest(string(source))
	if err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}
	p.Manifest = manifest

	// Create sandbox
	sandbox := NewSandbox(manifest, p.ID)

	// Register APIs
	clipsAPI := NewClipsAPI(m.db)
	clipsAPI.Register(sandbox.GetState())

	storageAPI := NewStorageAPI(m.db, p.ID)
	storageAPI.Register(sandbox.GetState())

	httpAPI := NewHTTPAPI(manifest.Network)
	httpAPI.Register(sandbox.GetState())

	fsAPI := NewFilesystemAPI(m.db, p.ID, manifest.Name, manifest.Filesystem, m.permCallback)
	fsAPI.Register(sandbox.GetState())

	utilsAPI := NewUtilsAPI(manifest.Name)
	utilsAPI.Register(sandbox.GetState())

	// Load the plugin source
	if err := sandbox.LoadSource(string(source)); err != nil {
		sandbox.Close()
		return fmt.Errorf("failed to load source: %w", err)
	}

	p.Sandbox = sandbox

	// Register plugin
	m.mu.Lock()
	m.plugins[p.ID] = p

	// Subscribe to events
	for _, event := range manifest.Events {
		m.eventSubscribers[event] = append(m.eventSubscribers[event], p.ID)
	}
	m.mu.Unlock()

	// Register scheduled tasks
	for _, sched := range manifest.Schedules {
		m.scheduler.AddTask(p.ID, sched.Name, sched.Interval, sandbox)
	}

	log.Printf("Loaded plugin: %s v%s", manifest.Name, manifest.Version)
	return nil
}

// UnloadPlugin unloads a plugin
func (m *Manager) UnloadPlugin(pluginID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	p, ok := m.plugins[pluginID]
	if !ok {
		return
	}

	// Stop scheduled tasks
	m.scheduler.RemovePluginTasks(pluginID)

	// Close sandbox
	if p.Sandbox != nil {
		p.Sandbox.Close()
	}

	// Remove from event subscribers
	for event, subscribers := range m.eventSubscribers {
		newSubscribers := make([]int64, 0, len(subscribers))
		for _, id := range subscribers {
			if id != pluginID {
				newSubscribers = append(newSubscribers, id)
			}
		}
		m.eventSubscribers[event] = newSubscribers
	}

	delete(m.plugins, pluginID)
	log.Printf("Unloaded plugin: %s", p.Name)
}

// EmitEvent sends an event to all subscribed plugins
func (m *Manager) EmitEvent(event string, data interface{}) {
	m.mu.RLock()
	subscribers := m.eventSubscribers[event]
	m.mu.RUnlock()

	for _, pluginID := range subscribers {
		m.mu.RLock()
		p, ok := m.plugins[pluginID]
		m.mu.RUnlock()

		if !ok || p.Sandbox == nil {
			continue
		}

		// Convert event name to handler name: "clip:created" -> "on_clip_created"
		handlerName := eventToHandler(event)

		// Convert data to Lua value
		var args []lua.LValue
		if data != nil {
			args = append(args, goToLua(p.Sandbox.GetState(), data))
		}

		if err := p.Sandbox.CallHandler(handlerName, args...); err != nil {
			log.Printf("Plugin %s handler %s failed: %v", p.Name, handlerName, err)
			m.incrementErrorCount(pluginID)
		} else {
			m.resetErrorCount(pluginID)
		}
	}
}

func eventToHandler(event string) string {
	// "clip:created" -> "on_clip_created"
	// "app:startup" -> "on_startup"
	result := "on_"
	for _, c := range event {
		if c == ':' {
			result += "_"
		} else {
			result += string(c)
		}
	}
	return result
}

func (m *Manager) incrementErrorCount(pluginID int64) {
	result, err := m.db.Exec(
		"UPDATE plugins SET error_count = error_count + 1 WHERE id = ?",
		pluginID,
	)
	if err != nil {
		return
	}

	// Check if we need to disable the plugin
	var errorCount int
	m.db.QueryRow("SELECT error_count FROM plugins WHERE id = ?", pluginID).Scan(&errorCount)

	if errorCount >= MaxConsecutiveErrors {
		m.db.Exec("UPDATE plugins SET status = 'error' WHERE id = ?", pluginID)
		m.UnloadPlugin(pluginID)
		log.Printf("Plugin %d disabled after %d consecutive errors", pluginID, errorCount)
	}

	_ = result // silence unused warning
}

func (m *Manager) resetErrorCount(pluginID int64) {
	m.db.Exec("UPDATE plugins SET error_count = 0 WHERE id = ?", pluginID)
}

// ImportPlugin imports a plugin from a file path
func (m *Manager) ImportPlugin(sourcePath string) (*Plugin, error) {
	// Read source
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read plugin file: %w", err)
	}

	// Parse manifest to validate
	manifest, err := ParseManifest(string(source))
	if err != nil {
		return nil, fmt.Errorf("invalid plugin: %w", err)
	}

	// Copy to plugins directory
	filename := filepath.Base(sourcePath)
	destPath := filepath.Join(m.pluginsDir, filename)

	if err := os.WriteFile(destPath, source, 0644); err != nil {
		return nil, fmt.Errorf("failed to copy plugin: %w", err)
	}

	// Insert into database
	result, err := m.db.Exec(`
		INSERT INTO plugins (filename, name, version, enabled, status)
		VALUES (?, ?, ?, 1, 'enabled')
		ON CONFLICT(filename) DO UPDATE SET
			name = excluded.name,
			version = excluded.version,
			enabled = 1,
			status = 'enabled',
			error_count = 0
	`, filename, manifest.Name, manifest.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to register plugin: %w", err)
	}

	id, _ := result.LastInsertId()

	// Load the plugin
	p := &Plugin{
		ID:       id,
		Filename: filename,
		Name:     manifest.Name,
		Version:  manifest.Version,
		Enabled:  true,
		Status:   "enabled",
	}

	if err := m.loadPlugin(p); err != nil {
		return nil, fmt.Errorf("failed to load plugin: %w", err)
	}

	return p, nil
}

// GetPlugins returns all plugins
func (m *Manager) GetPlugins() []*Plugin {
	m.mu.RLock()
	defer m.mu.RUnlock()

	plugins := make([]*Plugin, 0, len(m.plugins))
	for _, p := range m.plugins {
		plugins = append(plugins, p)
	}
	return plugins
}

// EnablePlugin enables a plugin
func (m *Manager) EnablePlugin(pluginID int64) error {
	_, err := m.db.Exec(
		"UPDATE plugins SET enabled = 1, status = 'enabled', error_count = 0 WHERE id = ?",
		pluginID,
	)
	if err != nil {
		return err
	}

	// Reload the plugin
	var p Plugin
	err = m.db.QueryRow(`
		SELECT id, filename, name, version, enabled, status
		FROM plugins WHERE id = ?
	`, pluginID).Scan(&p.ID, &p.Filename, &p.Name, &p.Version, &p.Enabled, &p.Status)
	if err != nil {
		return err
	}
	p.Enabled = true

	return m.loadPlugin(&p)
}

// DisablePlugin disables a plugin
func (m *Manager) DisablePlugin(pluginID int64) error {
	m.UnloadPlugin(pluginID)
	_, err := m.db.Exec("UPDATE plugins SET enabled = 0 WHERE id = ?", pluginID)
	return err
}

// RemovePlugin removes a plugin completely
func (m *Manager) RemovePlugin(pluginID int64) error {
	m.mu.RLock()
	p, ok := m.plugins[pluginID]
	filename := ""
	if ok {
		filename = p.Filename
	} else {
		// Get filename from DB
		m.db.QueryRow("SELECT filename FROM plugins WHERE id = ?", pluginID).Scan(&filename)
	}
	m.mu.RUnlock()

	m.UnloadPlugin(pluginID)

	// Delete from database (cascades to permissions and storage)
	if _, err := m.db.Exec("DELETE FROM plugins WHERE id = ?", pluginID); err != nil {
		return err
	}

	// Delete file
	if filename != "" {
		os.Remove(filepath.Join(m.pluginsDir, filename))
	}

	return nil
}

// Shutdown stops all plugins
func (m *Manager) Shutdown() {
	// Emit shutdown event
	m.EmitEvent("app:shutdown", nil)

	// Stop scheduler
	m.scheduler.StopAll()

	// Close all sandboxes
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, p := range m.plugins {
		if p.Sandbox != nil {
			p.Sandbox.Close()
		}
	}

	m.plugins = make(map[int64]*Plugin)
	m.eventSubscribers = make(map[string][]int64)
}
```

**Step 2: Verify compilation**

Run:
```bash
cd /Users/egecan/conductor/workspaces/mahpastes/chengdu && go build ./plugin/...
```

Expected: No errors

**Step 3: Commit**

```bash
git add plugin/manager.go
git commit -m "feat(plugin): add plugin manager with load/unload/events"
```

---

## Task 12: Integrate Plugin Manager with App

**Files:**
- Modify: `app.go:24-31` (App struct)
- Modify: `app.go:59-96` (startup function)
- Modify: `app.go:98-110` (shutdown function)

**Step 1: Add pluginManager field to App struct**

In `app.go`, add to the App struct (around line 24-31):

```go
// App struct holds the application state
type App struct {
	ctx            context.Context
	db             *sql.DB
	tempDir        string
	mu             sync.Mutex
	watcherManager *WatcherManager
	taskManager    *TaskManager
	pluginManager  *plugin.Manager
}
```

**Step 2: Add import for plugin package**

At the top of `app.go`, add to imports:

```go
import (
	// ... existing imports
	"go-clipboard/plugin"
)
```

**Step 3: Initialize plugin manager in startup**

In the `startup` function (around line 59-96), add after taskManager initialization:

```go
	// Initialize task manager
	a.taskManager = NewTaskManager(a)

	// Initialize plugin manager
	dataDir, _ := getDataDir()
	pluginsDir := filepath.Join(dataDir, "plugins")
	pm, err := plugin.NewManager(ctx, a.db, pluginsDir)
	if err != nil {
		log.Printf("Warning: Failed to initialize plugin manager: %v", err)
	} else {
		a.pluginManager = pm
		// Set up permission callback for filesystem access
		pm.SetPermissionCallback(func(pluginName, permType, requestedPath string) string {
			// For now, use a simple dialog - in production this would use Wails runtime dialog
			title := "Plugin Permission Request"
			message := fmt.Sprintf("Plugin '%s' wants %s access", pluginName, permType)

			path, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
				Title:                title,
				DefaultDirectory:     filepath.Dir(requestedPath),
				CanCreateDirectories: permType == "fs_write",
			})
			if err != nil || path == "" {
				return ""
			}
			return path
		})

		// Load plugins
		if err := pm.LoadPlugins(); err != nil {
			log.Printf("Warning: Failed to load plugins: %v", err)
		}

		// Emit startup event
		pm.EmitEvent("app:startup", nil)
	}
```

**Step 4: Shutdown plugin manager**

In the `shutdown` function (around line 98-110), add before other cleanup:

```go
// shutdown is called when the app is closing
func (a *App) shutdown(ctx context.Context) {
	// Shutdown plugins first
	if a.pluginManager != nil {
		a.pluginManager.Shutdown()
	}

	// Stop watcher
	if a.watcherManager != nil {
		a.watcherManager.Stop()
	}

	// ... rest of existing shutdown code
}
```

**Step 5: Verify compilation**

Run:
```bash
cd /Users/egecan/conductor/workspaces/mahpastes/chengdu && go build .
```

Expected: No errors

**Step 6: Commit**

```bash
git add app.go
git commit -m "feat: integrate plugin manager with app lifecycle"
```

---

## Task 13: Add Plugin Events to Existing Operations

**Files:**
- Modify: `app.go` (UploadFiles, DeleteClip, ToggleArchive functions)
- Modify: `watcher.go` (importFile function)

**Step 1: Add clip:created event to UploadFiles**

In `app.go`, modify `UploadFiles` function (around line 303-343) to emit events:

```go
// UploadFiles handles file uploads
func (a *App) UploadFiles(files []FileData, expirationMinutes int) error {
	var expiresAt *time.Time
	if expirationMinutes > 0 {
		t := time.Now().Add(time.Duration(expirationMinutes) * time.Minute)
		expiresAt = &t
	}

	for _, file := range files {
		// Decode base64 data
		data, err := base64.StdEncoding.DecodeString(file.Data)
		if err != nil {
			log.Printf("Failed to decode base64 data for file %s: %v", file.Name, err)
			continue
		}

		contentType := file.ContentType

		// Special handling for text
		if contentType == "text/plain" || contentType == "" {
			textData := string(data)
			trimmedText := strings.TrimSpace(textData)

			if strings.HasPrefix(trimmedText, "<!DOCTYPE html") {
				contentType = "text/html"
			} else if isJSON(trimmedText) {
				contentType = "application/json"
			} else {
				contentType = "text/plain"
			}
		}

		result, err := a.db.Exec("INSERT INTO clips (content_type, data, filename, expires_at) VALUES (?, ?, ?, ?)",
			contentType, data, file.Name, expiresAt)
		if err != nil {
			log.Printf("Failed to insert into db: %v\n", err)
			continue
		}

		// Emit plugin event
		if a.pluginManager != nil {
			clipID, _ := result.LastInsertId()
			a.pluginManager.EmitEvent("clip:created", map[string]interface{}{
				"id":           clipID,
				"content_type": contentType,
				"filename":     file.Name,
			})
		}
	}

	return nil
}
```

**Step 2: Add clip:deleted event to DeleteClip**

Modify `DeleteClip` function (around line 346-352):

```go
// DeleteClip deletes a clip by ID
func (a *App) DeleteClip(id int64) error {
	_, err := a.db.Exec("DELETE FROM clips WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete clip: %w", err)
	}

	// Emit plugin event
	if a.pluginManager != nil {
		a.pluginManager.EmitEvent("clip:deleted", id)
	}

	return nil
}
```

**Step 3: Add clip:archived event to ToggleArchive**

Modify `ToggleArchive` function (around line 354-361):

```go
// ToggleArchive toggles the archived status of a clip
func (a *App) ToggleArchive(id int64) error {
	_, err := a.db.Exec("UPDATE clips SET is_archived = NOT is_archived WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to toggle archive: %w", err)
	}

	// Emit plugin event
	if a.pluginManager != nil {
		// Get current archived state
		var isArchived int
		a.db.QueryRow("SELECT is_archived FROM clips WHERE id = ?", id).Scan(&isArchived)
		a.pluginManager.EmitEvent("clip:archived", map[string]interface{}{
			"id":          id,
			"is_archived": isArchived == 1,
		})
	}

	return nil
}
```

**Step 4: Add watch events to watcher.go**

In `watcher.go`, modify `importFile` function (around line 282-306) to emit events:

```go
// importFile reads a file and imports it as a clip, returns the clip ID
func (w *WatcherManager) importFile(filePath string, folder *WatchedFolder) (int64, error) {
	fileData, err := w.app.ReadFileFromPath(filePath)
	if err != nil {
		return 0, err
	}

	// Emit watch:file_detected event before import
	if w.app.pluginManager != nil {
		w.app.pluginManager.EmitEvent("watch:file_detected", map[string]interface{}{
			"path":      filePath,
			"folder_id": folder.ID,
		})
	}

	// Upload and get the clip ID
	clipID, err := w.app.UploadFileAndGetID(*fileData)
	if err != nil {
		return 0, err
	}

	// Auto-archive if configured
	if folder.AutoArchive {
		if err := w.app.ToggleArchive(clipID); err != nil {
			log.Printf("Failed to auto-archive clip %d: %v", clipID, err)
		}
	}

	// Emit import event for UI refresh
	w.app.emitWatchImport(fileData.Name)

	// Emit watch:import_complete event
	if w.app.pluginManager != nil {
		w.app.pluginManager.EmitEvent("watch:import_complete", map[string]interface{}{
			"clip_id":     clipID,
			"source_path": filePath,
			"folder_id":   folder.ID,
		})
	}

	return clipID, nil
}
```

**Step 5: Verify compilation**

Run:
```bash
cd /Users/egecan/conductor/workspaces/mahpastes/chengdu && go build .
```

Expected: No errors

**Step 6: Commit**

```bash
git add app.go watcher.go
git commit -m "feat: emit plugin events for clip and watch operations"
```

---

## Task 14: Add Plugin Management API for Frontend

**Files:**
- Modify: `app.go` (add new exported methods)

**Step 1: Add plugin management methods**

Add to `app.go` (at the end of the file):

```go
// PluginInfo represents a plugin for the frontend
type PluginInfo struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	Enabled     bool     `json:"enabled"`
	Status      string   `json:"status"`
	Events      []string `json:"events"`
}

// GetPlugins returns all plugins
func (a *App) GetPlugins() ([]PluginInfo, error) {
	rows, err := a.db.Query(`
		SELECT id, name, version, enabled, status
		FROM plugins ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query plugins: %w", err)
	}
	defer rows.Close()

	var plugins []PluginInfo
	for rows.Next() {
		var p PluginInfo
		var enabled int
		if err := rows.Scan(&p.ID, &p.Name, &p.Version, &enabled, &p.Status); err != nil {
			continue
		}
		p.Enabled = enabled == 1

		// Get additional info from loaded plugin if available
		if a.pluginManager != nil {
			for _, loaded := range a.pluginManager.GetPlugins() {
				if loaded.ID == p.ID && loaded.Manifest != nil {
					p.Description = loaded.Manifest.Description
					p.Author = loaded.Manifest.Author
					p.Events = loaded.Manifest.Events
					break
				}
			}
		}

		plugins = append(plugins, p)
	}

	if plugins == nil {
		plugins = []PluginInfo{}
	}
	return plugins, nil
}

// ImportPlugin imports a plugin from a file path
func (a *App) ImportPlugin() (*PluginInfo, error) {
	if a.pluginManager == nil {
		return nil, fmt.Errorf("plugin manager not initialized")
	}

	// Open file dialog
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Plugin File",
		Filters: []runtime.FileFilter{
			{DisplayName: "Lua Scripts", Pattern: "*.lua"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open file dialog: %w", err)
	}
	if path == "" {
		return nil, nil // User cancelled
	}

	p, err := a.pluginManager.ImportPlugin(path)
	if err != nil {
		return nil, err
	}

	info := &PluginInfo{
		ID:      p.ID,
		Name:    p.Name,
		Version: p.Version,
		Enabled: p.Enabled,
		Status:  p.Status,
	}
	if p.Manifest != nil {
		info.Description = p.Manifest.Description
		info.Author = p.Manifest.Author
		info.Events = p.Manifest.Events
	}

	return info, nil
}

// EnablePlugin enables a plugin
func (a *App) EnablePlugin(id int64) error {
	if a.pluginManager == nil {
		return fmt.Errorf("plugin manager not initialized")
	}
	return a.pluginManager.EnablePlugin(id)
}

// DisablePlugin disables a plugin
func (a *App) DisablePlugin(id int64) error {
	if a.pluginManager == nil {
		return fmt.Errorf("plugin manager not initialized")
	}
	return a.pluginManager.DisablePlugin(id)
}

// RemovePlugin removes a plugin
func (a *App) RemovePlugin(id int64) error {
	if a.pluginManager == nil {
		return fmt.Errorf("plugin manager not initialized")
	}
	return a.pluginManager.RemovePlugin(id)
}

// GetPluginPermissions returns permissions granted to a plugin
func (a *App) GetPluginPermissions(id int64) ([]map[string]string, error) {
	rows, err := a.db.Query(`
		SELECT permission_type, path, granted_at
		FROM plugin_permissions WHERE plugin_id = ?
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var perms []map[string]string
	for rows.Next() {
		var permType, path string
		var grantedAt string
		if err := rows.Scan(&permType, &path, &grantedAt); err != nil {
			continue
		}
		perms = append(perms, map[string]string{
			"type":       permType,
			"path":       path,
			"granted_at": grantedAt,
		})
	}

	if perms == nil {
		perms = []map[string]string{}
	}
	return perms, nil
}

// RevokePluginPermission revokes a filesystem permission
func (a *App) RevokePluginPermission(pluginID int64, permType, path string) error {
	_, err := a.db.Exec(`
		DELETE FROM plugin_permissions
		WHERE plugin_id = ? AND permission_type = ? AND path = ?
	`, pluginID, permType, path)
	return err
}
```

**Step 2: Verify compilation**

Run:
```bash
cd /Users/egecan/conductor/workspaces/mahpastes/chengdu && go build .
```

Expected: No errors

**Step 3: Commit**

```bash
git add app.go
git commit -m "feat: add plugin management API for frontend"
```

---

## Task 15: Generate Wails Bindings

**Files:**
- Modified: `frontend/wailsjs/` (auto-generated)

**Step 1: Generate bindings**

Run:
```bash
cd /Users/egecan/conductor/workspaces/mahpastes/chengdu && wails generate module
```

Expected: Bindings generated successfully

**Step 2: Verify the bindings were created**

Run:
```bash
ls -la /Users/egecan/conductor/workspaces/mahpastes/chengdu/frontend/wailsjs/go/main/
```

Expected: See `App.js` and `App.d.ts` files with updated timestamps

**Step 3: Commit**

```bash
git add frontend/wailsjs/
git commit -m "chore: regenerate Wails bindings for plugin API"
```

---

## Task 16: Create Example Plugin

**Files:**
- Create: `examples/plugins/hello-world.lua`

**Step 1: Create examples directory**

Run:
```bash
mkdir -p /Users/egecan/conductor/workspaces/mahpastes/chengdu/examples/plugins
```

**Step 2: Write example plugin**

Create `examples/plugins/hello-world.lua`:

```lua
-- Hello World Plugin for mahpastes
-- Demonstrates the plugin API

Plugin = {
    name = "Hello World",
    version = "1.0.0",
    description = "A simple example plugin that logs clip events",
    author = "mahpastes",

    -- No network access needed
    network = {},

    -- No filesystem access needed
    filesystem = {
        read = false,
        write = false,
    },

    -- Subscribe to clip events
    events = {"app:startup", "app:shutdown", "clip:created", "clip:deleted"},

    -- No scheduled tasks
    schedules = {},
}

-- Called when the app starts
function on_startup()
    log("Hello World plugin started!")

    -- Count existing clips
    local clips = clips.list()
    log("Found " .. #clips .. " existing clips")
end

-- Called when the app is shutting down
function on_shutdown()
    log("Hello World plugin shutting down. Goodbye!")
end

-- Called when a new clip is created
function on_clip_created(clip)
    log("New clip created: " .. (clip.filename or "unnamed") .. " (" .. clip.content_type .. ")")

    -- Store count in plugin storage
    local count = storage.get("clip_count") or "0"
    count = tonumber(count) + 1
    storage.set("clip_count", tostring(count))
    log("Total clips created this session: " .. count)
end

-- Called when a clip is deleted
function on_clip_deleted(clip_id)
    log("Clip deleted: ID " .. clip_id)
end
```

**Step 3: Commit**

```bash
git add examples/plugins/hello-world.lua
git commit -m "docs: add hello-world example plugin"
```

---

## Task 17: Create Auto-Archive Plugin Example

**Files:**
- Create: `examples/plugins/auto-archive-old.lua`

**Step 1: Write auto-archive plugin**

Create `examples/plugins/auto-archive-old.lua`:

```lua
-- Auto-Archive Old Clips Plugin
-- Archives clips older than 24 hours

Plugin = {
    name = "Auto Archive Old",
    version = "1.0.0",
    description = "Automatically archives clips older than 24 hours",
    author = "mahpastes",

    network = {},
    filesystem = {
        read = false,
        write = false,
    },

    events = {"app:startup"},

    -- Run every hour
    schedules = {
        {name = "archive_old_clips", interval = 3600},
    },
}

local HOURS_THRESHOLD = 24

function on_startup()
    log("Auto Archive plugin started - will archive clips older than " .. HOURS_THRESHOLD .. " hours")
    -- Run immediately on startup
    archive_old_clips()
end

function archive_old_clips()
    local now = os.time()
    local threshold = now - (HOURS_THRESHOLD * 60 * 60)

    local all_clips = clips.list()
    local archived_count = 0

    for _, clip in ipairs(all_clips) do
        -- Skip already archived clips
        if not clip.is_archived then
            if clip.created_at < threshold then
                local success = clips.archive(clip.id)
                if success then
                    archived_count = archived_count + 1
                end
            end
        end
    end

    if archived_count > 0 then
        log("Archived " .. archived_count .. " old clips")
    end
end
```

**Step 2: Commit**

```bash
git add examples/plugins/auto-archive-old.lua
git commit -m "docs: add auto-archive example plugin"
```

---

## Task 18: Build and Test the Application

**Step 1: Build the application**

Run:
```bash
cd /Users/egecan/conductor/workspaces/mahpastes/chengdu && wails build
```

Expected: Build succeeds

**Step 2: Run e2e tests to ensure no regressions**

Run:
```bash
cd /Users/egecan/conductor/workspaces/mahpastes/chengdu/e2e && npm test
```

Expected: All existing tests pass

**Step 3: Commit any fixes if needed**

If tests fail, fix issues and commit.

---

## Task 19: Final Integration Test

**Step 1: Run the app in dev mode**

Run:
```bash
cd /Users/egecan/conductor/workspaces/mahpastes/chengdu && wails dev
```

**Step 2: Manual testing checklist**

- [ ] App starts without errors
- [ ] Can import hello-world.lua plugin via API (use browser console to call `go.main.App.ImportPlugin()`)
- [ ] Plugin appears in GetPlugins() list
- [ ] Creating a clip triggers plugin log output
- [ ] Plugin can be disabled/enabled
- [ ] Plugin can be removed
- [ ] App shuts down cleanly

**Step 3: Final commit if any changes**

```bash
git add -A
git commit -m "fix: integration test fixes"
```

---

## Summary

This plan creates a complete Lua plugin system with:

1. **Core infrastructure**: Manifest parser, sandboxed Lua VM, plugin manager
2. **APIs**: clips, storage, http (domain-restricted), filesystem (permission-prompted), utilities
3. **Event system**: Lifecycle events, clip events, watch events, scheduled tasks
4. **Security**: 30s execution timeout, rate limits, domain allowlist, runtime filesystem approval
5. **Management**: Import, enable/disable, remove plugins, revoke permissions
6. **Examples**: Hello world and auto-archive plugins

Total tasks: 19
Estimated implementation time varies based on familiarity with Go and Lua integration.
