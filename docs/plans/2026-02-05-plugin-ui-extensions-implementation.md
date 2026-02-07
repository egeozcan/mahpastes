# Plugin UI Extensions Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Enable plugins to add UI elements (lightbox buttons, card actions) and process clips, allowing fal.ai functionality to be extracted as a plugin.

**Architecture:** Extend the manifest parser to handle a new `ui` section with declarative button/action definitions. Add Lua APIs for clip data access (`clips.get_data`), clip creation from data/URL, and task queue integration. Frontend renders plugin UI elements and routes clicks through `PluginService.ExecutePluginAction`.

**Tech Stack:** Go (Wails), gopher-lua, Vanilla JS, Playwright e2e tests

---

## Task 1: Extend Manifest Types

**Files:**
- Modify: `plugin/manifest.go`

**Step 1: Add UI manifest structs**

Add after line 43 (after SettingField):

```go
// UIManifest represents plugin UI declarations
type UIManifest struct {
	LightboxButtons []UIAction `json:"lightbox_buttons,omitempty"`
	CardActions     []UIAction `json:"card_actions,omitempty"`
}

// UIAction represents a plugin-defined action button
type UIAction struct {
	ID      string      `json:"id"`
	Label   string      `json:"label"`
	Icon    string      `json:"icon,omitempty"`
	Options []FormField `json:"options,omitempty"`
}

// FormField represents a form field in an options dialog
type FormField struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"` // text, password, checkbox, select, range
	Label    string   `json:"label"`
	Required bool     `json:"required,omitempty"`
	Default  any      `json:"default,omitempty"`
	Choices  []Choice `json:"choices,omitempty"` // for select
	Min      float64  `json:"min,omitempty"`     // for range
	Max      float64  `json:"max,omitempty"`     // for range
	Step     float64  `json:"step,omitempty"`    // for range
}

// Choice represents a select option
type Choice struct {
	Value string `json:"value"`
	Label string `json:"label"`
}
```

**Step 2: Add UI field to Manifest struct**

Add to the Manifest struct (around line 11):

```go
type Manifest struct {
	Name        string
	Version     string
	Description string
	Author      string
	Network     map[string][]string
	Filesystem  FilesystemPerms
	Events      []string
	Schedules   []Schedule
	Settings    []SettingField
	UI          *UIManifest // Add this line
}
```

**Step 3: Commit**

```bash
cd /Users/egecan/Code/mahpastes/.worktrees/plugin-ui-extensions
git add plugin/manifest.go
git commit -m "$(cat <<'EOF'
feat(plugin): add UI manifest types for buttons and actions

Add UIManifest, UIAction, FormField, and Choice structs to support
declarative UI element definitions in plugin manifests.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Parse UI Section in Manifest

**Files:**
- Modify: `plugin/manifest.go`

**Step 1: Add extractUI function**

Add after `extractSettings` function (around line 418):

```go
// extractUI extracts UI declarations from the manifest
// Format: ui = { lightbox_buttons = {...}, card_actions = {...} }
func extractUI(block string) *UIManifest {
	// Find the ui block
	uiPattern := regexp.MustCompile(`ui\s*=\s*\{`)
	loc := uiPattern.FindStringIndex(block)
	if loc == nil {
		return nil
	}

	start := loc[1] - 1
	uiBlock := extractNestedBrace(block[start:])
	if uiBlock == "" {
		return nil
	}

	ui := &UIManifest{}
	ui.LightboxButtons = extractUIActions(uiBlock, "lightbox_buttons")
	ui.CardActions = extractUIActions(uiBlock, "card_actions")

	// Return nil if no actions defined
	if len(ui.LightboxButtons) == 0 && len(ui.CardActions) == 0 {
		return nil
	}

	return ui
}

// extractUIActions extracts an array of UI actions
func extractUIActions(block, field string) []UIAction {
	var result []UIAction

	// Find the field block
	fieldPattern := regexp.MustCompile(regexp.QuoteMeta(field) + `\s*=\s*\{`)
	loc := fieldPattern.FindStringIndex(block)
	if loc == nil {
		return result
	}

	start := loc[1] - 1
	actionsBlock := extractNestedBrace(block[start:])
	if actionsBlock == "" {
		return result
	}

	// Find each action entry: {id = "...", label = "...", ...}
	depth := 0
	entryStart := -1

	for i := 1; i < len(actionsBlock)-1; i++ {
		c := actionsBlock[i]
		if c == '{' {
			if depth == 0 {
				entryStart = i
			}
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 && entryStart >= 0 {
				entry := actionsBlock[entryStart : i+1]
				action := parseUIAction(entry)
				if action.ID != "" && action.Label != "" {
					result = append(result, action)
				}
				entryStart = -1
			}
		}
	}

	return result
}

// parseUIAction parses a single UI action entry
func parseUIAction(entry string) UIAction {
	var action UIAction

	action.ID = extractStringField(entry, "id")
	action.Label = extractStringField(entry, "label")
	action.Icon = extractStringField(entry, "icon")

	// Parse options if present
	action.Options = extractFormFields(entry)

	return action
}

// extractFormFields extracts form field definitions from an action
func extractFormFields(block string) []FormField {
	var result []FormField

	// Find the options block
	optionsPattern := regexp.MustCompile(`options\s*=\s*\{`)
	loc := optionsPattern.FindStringIndex(block)
	if loc == nil {
		return result
	}

	start := loc[1] - 1
	optionsBlock := extractNestedBrace(block[start:])
	if optionsBlock == "" {
		return result
	}

	// Find each field entry
	depth := 0
	entryStart := -1

	for i := 1; i < len(optionsBlock)-1; i++ {
		c := optionsBlock[i]
		if c == '{' {
			if depth == 0 {
				entryStart = i
			}
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 && entryStart >= 0 {
				entry := optionsBlock[entryStart : i+1]
				field := parseFormField(entry)
				if field.ID != "" && field.Type != "" && field.Label != "" {
					result = append(result, field)
				}
				entryStart = -1
			}
		}
	}

	return result
}

// parseFormField parses a single form field entry
func parseFormField(entry string) FormField {
	var field FormField

	field.ID = extractStringField(entry, "id")
	field.Type = extractStringField(entry, "type")
	field.Label = extractStringField(entry, "label")

	// Parse required
	requiredPattern := regexp.MustCompile(`required\s*=\s*(true|false)`)
	if matches := requiredPattern.FindStringSubmatch(entry); len(matches) >= 2 {
		field.Required = matches[1] == "true"
	}

	// Parse default value
	field.Default = extractDefaultValue(entry)

	// Parse choices for select type
	field.Choices = extractChoices(entry)

	// Parse range options
	field.Min = extractFloatField(entry, "min")
	field.Max = extractFloatField(entry, "max")
	field.Step = extractFloatField(entry, "step")

	return field
}

// extractChoices extracts choices array for select fields
func extractChoices(block string) []Choice {
	var result []Choice

	// Find choices block
	choicesPattern := regexp.MustCompile(`choices\s*=\s*\{`)
	loc := choicesPattern.FindStringIndex(block)
	if loc == nil {
		return result
	}

	start := loc[1] - 1
	choicesBlock := extractNestedBrace(block[start:])
	if choicesBlock == "" {
		return result
	}

	// Find each choice entry
	depth := 0
	entryStart := -1

	for i := 1; i < len(choicesBlock)-1; i++ {
		c := choicesBlock[i]
		if c == '{' {
			if depth == 0 {
				entryStart = i
			}
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 && entryStart >= 0 {
				entry := choicesBlock[entryStart : i+1]
				value := extractStringField(entry, "value")
				label := extractStringField(entry, "label")
				if value != "" && label != "" {
					result = append(result, Choice{Value: value, Label: label})
				}
				entryStart = -1
			}
		}
	}

	return result
}

// extractFloatField extracts a float64 field value
func extractFloatField(block, field string) float64 {
	pattern := fmt.Sprintf(`%s\s*=\s*([0-9.]+)`, regexp.QuoteMeta(field))
	re := regexp.MustCompile(pattern)
	matches := re.FindStringSubmatch(block)
	if len(matches) >= 2 {
		val, err := strconv.ParseFloat(matches[1], 64)
		if err == nil {
			return val
		}
	}
	return 0
}
```

**Step 2: Call extractUI in ParseManifest**

In `ParseManifest` function, add before the return statement (around line 84):

```go
	// Parse UI declarations
	manifest.UI = extractUI(pluginBlock)

	return manifest, nil
```

**Step 3: Commit**

```bash
git add plugin/manifest.go
git commit -m "$(cat <<'EOF'
feat(plugin): parse UI section in manifest

Add extractUI, extractUIActions, parseUIAction, extractFormFields,
parseFormField, extractChoices, and extractFloatField functions to
parse the ui section of plugin manifests.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Add clips.get_data() API

**Files:**
- Modify: `plugin/api_clips.go`

**Step 1: Add get_data function**

Add after the `get` function (around line 165):

```go
// getData returns raw clip data (base64 for binary, plain for text)
// Returns: data, mime_type or nil, error
func (c *ClipsAPI) getData(L *lua.LState) int {
	id := L.CheckInt64(1)

	var contentType string
	var data []byte

	err := c.db.QueryRow(`
		SELECT content_type, data FROM clips WHERE id = ?
	`, id).Scan(&contentType, &data)

	if err == sql.ErrNoRows {
		L.Push(lua.LNil)
		L.Push(lua.LString("clip not found"))
		return 2
	}
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	// For text content, return as-is; for binary, base64 encode
	if strings.HasPrefix(contentType, "text/") || contentType == "application/json" {
		L.Push(lua.LString(string(data)))
	} else {
		L.Push(lua.LString(base64.StdEncoding.EncodeToString(data)))
	}
	L.Push(lua.LString(contentType))
	return 2
}
```

**Step 2: Register get_data in Register function**

In the `Register` function (around line 34), add:

```go
	clipsMod.RawSetString("get_data", L.NewFunction(c.getData))
```

**Step 3: Commit**

```bash
git add plugin/api_clips.go
git commit -m "$(cat <<'EOF'
feat(plugin): add clips.get_data() API

Returns raw clip content (base64 for binary, plain text for text types)
and mime type. Plugins can use this to access clip data for processing.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Extend clips.create() with data parameter

**Files:**
- Modify: `plugin/api_clips.go`

**Step 1: Update create function to support name and mime_type params**

Replace the create function (around line 167-228):

```go
func (c *ClipsAPI) create(L *lua.LState) int {
	opts := L.CheckTable(1)

	dataVal := opts.RawGetString("data")
	if dataVal == lua.LNil {
		L.Push(lua.LNil)
		L.Push(lua.LString("data is required"))
		return 2
	}
	dataStr := dataVal.String()

	// Check size before processing
	if len(dataStr) > MaxClipDataSize {
		L.Push(lua.LNil)
		L.Push(lua.LString(fmt.Sprintf("data too large: %d bytes (max %d)", len(dataStr), MaxClipDataSize)))
		return 2
	}

	// Support both content_type and mime_type for flexibility
	contentType := "application/octet-stream"
	if ct := opts.RawGetString("content_type"); ct != lua.LNil {
		contentType = ct.String()
	} else if mt := opts.RawGetString("mime_type"); mt != lua.LNil {
		contentType = mt.String()
	}

	// Support both filename and name for flexibility
	var filename string
	if fn := opts.RawGetString("filename"); fn != lua.LNil {
		filename = fn.String()
	} else if nm := opts.RawGetString("name"); nm != lua.LNil {
		filename = nm.String()
	}

	// Determine if data is base64 encoded
	// Check explicit encoding flag or auto-detect for binary content types
	isBase64 := false
	if enc := opts.RawGetString("data_encoding"); enc != lua.LNil && enc.String() == "base64" {
		isBase64 = true
	} else if !strings.HasPrefix(contentType, "text/") && contentType != "application/json" {
		// For binary content types, assume base64 if not explicitly text
		isBase64 = true
	}

	var data []byte
	if isBase64 {
		var err error
		data, err = base64.StdEncoding.DecodeString(dataStr)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString("invalid base64 data: " + err.Error()))
			return 2
		}
		// Check decoded size as well
		if len(data) > MaxClipDataSize {
			L.Push(lua.LNil)
			L.Push(lua.LString(fmt.Sprintf("decoded data too large: %d bytes (max %d)", len(data), MaxClipDataSize)))
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

	// Return a table with clip info (matching design spec)
	clip := L.NewTable()
	clip.RawSetString("id", lua.LNumber(id))
	L.Push(clip)
	return 1
}
```

**Step 2: Commit**

```bash
git add plugin/api_clips.go
git commit -m "$(cat <<'EOF'
feat(plugin): extend clips.create() with name/mime_type params

Support 'name' as alias for 'filename' and 'mime_type' as alias for
'content_type'. Auto-detect base64 encoding for binary content types.
Return table with clip id instead of just the id number.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Add clips.create_from_url() API

**Files:**
- Modify: `plugin/api_clips.go`

**Step 1: Add imports for HTTP**

Add to imports at top of file:

```go
import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"path"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"
)
```

**Step 2: Add createFromURL function**

Add after the create function:

```go
// createFromURL downloads content from a URL and creates a clip
func (c *ClipsAPI) createFromURL(L *lua.LState) int {
	url := L.CheckString(1)

	var opts *lua.LTable
	if L.GetTop() >= 2 {
		opts = L.OptTable(2, nil)
	}

	// Create HTTP client with timeout
	client := &http.Client{Timeout: 60 * time.Second}

	resp, err := client.Get(url)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("failed to fetch URL: " + err.Error()))
		return 2
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		L.Push(lua.LNil)
		L.Push(lua.LString(fmt.Sprintf("HTTP error: %d %s", resp.StatusCode, resp.Status)))
		return 2
	}

	// Read body with size limit
	limitedReader := io.LimitReader(resp.Body, MaxClipDataSize+1)
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("failed to read response: " + err.Error()))
		return 2
	}

	if len(data) > MaxClipDataSize {
		L.Push(lua.LNil)
		L.Push(lua.LString(fmt.Sprintf("response too large: exceeds %d bytes", MaxClipDataSize)))
		return 2
	}

	// Determine content type
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	// Strip charset if present
	if idx := strings.Index(contentType, ";"); idx > 0 {
		contentType = strings.TrimSpace(contentType[:idx])
	}

	// Override with options if provided
	if opts != nil {
		if mt := opts.RawGetString("mime_type"); mt != lua.LNil {
			contentType = mt.String()
		} else if ct := opts.RawGetString("content_type"); ct != lua.LNil {
			contentType = ct.String()
		}
	}

	// Determine filename
	filename := ""
	if opts != nil {
		if nm := opts.RawGetString("name"); nm != lua.LNil {
			filename = nm.String()
		} else if fn := opts.RawGetString("filename"); fn != lua.LNil {
			filename = fn.String()
		}
	}
	// Fallback to URL path
	if filename == "" {
		filename = path.Base(url)
		if filename == "." || filename == "/" {
			filename = "downloaded"
		}
	}

	// Insert into database
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

	// Return a table with clip info
	clip := L.NewTable()
	clip.RawSetString("id", lua.LNumber(id))
	L.Push(clip)
	return 1
}
```

**Step 3: Register create_from_url in Register function**

Add to the Register function:

```go
	clipsMod.RawSetString("create_from_url", L.NewFunction(c.createFromURL))
```

**Step 4: Commit**

```bash
git add plugin/api_clips.go
git commit -m "$(cat <<'EOF'
feat(plugin): add clips.create_from_url() API

Downloads content from a URL and creates a clip. Supports optional
name/filename and mime_type/content_type overrides. Respects
MaxClipDataSize limit.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Create Task Queue API

**Files:**
- Create: `plugin/api_task.go`

**Step 1: Create the task API file**

```go
package plugin

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	lua "github.com/yuin/gopher-lua"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// TaskAPI provides task queue integration for plugins
type TaskAPI struct {
	ctx       context.Context
	tasks     map[int64]*pluginTask
	taskMu    sync.RWMutex
	nextID    int64
	pluginID  int64
}

type pluginTask struct {
	ID       int64
	Name     string
	Total    int
	Current  int
	Status   string // "running", "completed", "failed"
	Error    string
}

// NewTaskAPI creates a new task API instance
func NewTaskAPI(ctx context.Context, pluginID int64) *TaskAPI {
	return &TaskAPI{
		ctx:      ctx,
		tasks:    make(map[int64]*pluginTask),
		pluginID: pluginID,
	}
}

// Register adds the task module to the Lua state
func (t *TaskAPI) Register(L *lua.LState) {
	taskMod := L.NewTable()

	taskMod.RawSetString("start", L.NewFunction(t.start))
	taskMod.RawSetString("progress", L.NewFunction(t.progress))
	taskMod.RawSetString("complete", L.NewFunction(t.complete))
	taskMod.RawSetString("fail", L.NewFunction(t.fail))

	L.SetGlobal("task", taskMod)
}

func (t *TaskAPI) start(L *lua.LState) int {
	name := L.CheckString(1)
	total := L.OptInt(2, 1)

	taskID := atomic.AddInt64(&t.nextID, 1)

	task := &pluginTask{
		ID:      taskID,
		Name:    name,
		Total:   total,
		Current: 0,
		Status:  "running",
	}

	t.taskMu.Lock()
	t.tasks[taskID] = task
	t.taskMu.Unlock()

	// Emit task started event to frontend
	if t.ctx != nil {
		runtime.EventsEmit(t.ctx, "plugin:task:started", map[string]interface{}{
			"task_id":   taskID,
			"plugin_id": t.pluginID,
			"name":      name,
			"total":     total,
		})
	}

	L.Push(lua.LNumber(taskID))
	return 1
}

func (t *TaskAPI) progress(L *lua.LState) int {
	taskID := L.CheckInt64(1)
	current := L.CheckInt(2)

	t.taskMu.Lock()
	task, ok := t.tasks[taskID]
	if ok {
		task.Current = current
	}
	t.taskMu.Unlock()

	if !ok {
		L.Push(lua.LFalse)
		L.Push(lua.LString("task not found"))
		return 2
	}

	// Emit progress event to frontend
	if t.ctx != nil {
		runtime.EventsEmit(t.ctx, "plugin:task:progress", map[string]interface{}{
			"task_id":   taskID,
			"plugin_id": t.pluginID,
			"current":   current,
			"total":     task.Total,
			"name":      task.Name,
		})
	}

	L.Push(lua.LTrue)
	return 1
}

func (t *TaskAPI) complete(L *lua.LState) int {
	taskID := L.CheckInt64(1)

	t.taskMu.Lock()
	task, ok := t.tasks[taskID]
	if ok {
		task.Status = "completed"
		task.Current = task.Total
	}
	t.taskMu.Unlock()

	if !ok {
		L.Push(lua.LFalse)
		L.Push(lua.LString("task not found"))
		return 2
	}

	// Emit completion event to frontend
	if t.ctx != nil {
		runtime.EventsEmit(t.ctx, "plugin:task:completed", map[string]interface{}{
			"task_id":   taskID,
			"plugin_id": t.pluginID,
			"name":      task.Name,
		})
	}

	L.Push(lua.LTrue)
	return 1
}

func (t *TaskAPI) fail(L *lua.LState) int {
	taskID := L.CheckInt64(1)
	errMsg := L.OptString(2, "Unknown error")

	t.taskMu.Lock()
	task, ok := t.tasks[taskID]
	if ok {
		task.Status = "failed"
		task.Error = errMsg
	}
	t.taskMu.Unlock()

	if !ok {
		L.Push(lua.LFalse)
		L.Push(lua.LString("task not found"))
		return 2
	}

	// Emit failure event to frontend
	if t.ctx != nil {
		runtime.EventsEmit(t.ctx, "plugin:task:failed", map[string]interface{}{
			"task_id":   taskID,
			"plugin_id": t.pluginID,
			"name":      task.Name,
			"error":     errMsg,
		})
	}

	L.Push(lua.LTrue)
	return 1
}

// GetTask returns a task by ID (for testing)
func (t *TaskAPI) GetTask(taskID int64) (*pluginTask, bool) {
	t.taskMu.RLock()
	defer t.taskMu.RUnlock()
	task, ok := t.tasks[taskID]
	return task, ok
}
```

**Step 2: Commit**

```bash
git add plugin/api_task.go
git commit -m "$(cat <<'EOF'
feat(plugin): add task queue API

Provides task.start(), task.progress(), task.complete(), and task.fail()
functions for plugins to integrate with the task queue. Emits events
to the frontend for UI updates.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Register Task API in Manager

**Files:**
- Modify: `plugin/manager.go`

**Step 1: Find where APIs are registered**

Look for where ClipsAPI, StorageAPI etc are registered and add TaskAPI registration.

In the `loadPluginSandbox` or similar function, add:

```go
	// Register task API
	taskAPI := NewTaskAPI(m.ctx, p.ID)
	taskAPI.Register(L)
```

**Step 2: Store TaskAPI reference in Plugin struct if needed**

If plugins need to access task API later, add to Plugin struct:

```go
type Plugin struct {
	// ... existing fields
	TaskAPI *TaskAPI
}
```

**Step 3: Commit**

```bash
git add plugin/manager.go
git commit -m "$(cat <<'EOF'
feat(plugin): register task API in plugin sandbox

Each plugin now gets its own TaskAPI instance registered as the 'task'
global table in Lua.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: Add PluginService.GetPluginUIActions()

**Files:**
- Modify: `plugin_service.go`

**Step 1: Add UIActionsResponse type**

Add after PluginInfo struct:

```go
// PluginUIAction represents a UI action with plugin context
type PluginUIAction struct {
	PluginID   int64                `json:"plugin_id"`
	PluginName string               `json:"plugin_name"`
	ID         string               `json:"id"`
	Label      string               `json:"label"`
	Icon       string               `json:"icon,omitempty"`
	Options    []plugin.FormField   `json:"options,omitempty"`
}

// UIActionsResponse contains all plugin UI actions
type UIActionsResponse struct {
	LightboxButtons []PluginUIAction `json:"lightbox_buttons"`
	CardActions     []PluginUIAction `json:"card_actions"`
}
```

**Step 2: Add GetPluginUIActions method**

```go
// GetPluginUIActions returns all UI actions from enabled plugins
func (s *PluginService) GetPluginUIActions() (*UIActionsResponse, error) {
	if s.app.pluginManager == nil {
		return &UIActionsResponse{
			LightboxButtons: []PluginUIAction{},
			CardActions:     []PluginUIAction{},
		}, nil
	}

	response := &UIActionsResponse{
		LightboxButtons: []PluginUIAction{},
		CardActions:     []PluginUIAction{},
	}

	plugins := s.app.pluginManager.GetPlugins()
	for _, p := range plugins {
		if !p.Enabled || p.Manifest == nil || p.Manifest.UI == nil {
			continue
		}

		// Add lightbox buttons
		for _, btn := range p.Manifest.UI.LightboxButtons {
			response.LightboxButtons = append(response.LightboxButtons, PluginUIAction{
				PluginID:   p.ID,
				PluginName: p.Name,
				ID:         btn.ID,
				Label:      btn.Label,
				Icon:       btn.Icon,
				Options:    btn.Options,
			})
		}

		// Add card actions
		for _, action := range p.Manifest.UI.CardActions {
			response.CardActions = append(response.CardActions, PluginUIAction{
				PluginID:   p.ID,
				PluginName: p.Name,
				ID:         action.ID,
				Label:      action.Label,
				Icon:       action.Icon,
				Options:    action.Options,
			})
		}
	}

	return response, nil
}
```

**Step 3: Commit**

```bash
git add plugin_service.go
git commit -m "$(cat <<'EOF'
feat(plugin): add GetPluginUIActions() to PluginService

Returns all lightbox buttons and card actions from enabled plugins
with their plugin context (ID, name). Frontend uses this to render
plugin UI elements.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: Add PluginService.ExecutePluginAction()

**Files:**
- Modify: `plugin_service.go`
- Modify: `plugin/manager.go`

**Step 1: Add ActionResult type**

```go
// ActionResult represents the result of a plugin action execution
type ActionResult struct {
	Success      bool   `json:"success"`
	Error        string `json:"error,omitempty"`
	ResultClipID int64  `json:"result_clip_id,omitempty"`
}
```

**Step 2: Add ExecutePluginAction method to PluginService**

```go
// ExecutePluginAction calls a plugin's on_ui_action handler
func (s *PluginService) ExecutePluginAction(pluginID int64, actionID string, clipIDs []int64, options map[string]interface{}) (*ActionResult, error) {
	if s.app.pluginManager == nil {
		return &ActionResult{Success: false, Error: "plugin manager not initialized"}, nil
	}

	result, err := s.app.pluginManager.ExecuteUIAction(pluginID, actionID, clipIDs, options)
	if err != nil {
		return &ActionResult{Success: false, Error: err.Error()}, nil
	}

	return result, nil
}
```

**Step 3: Add ExecuteUIAction method to Manager**

In `plugin/manager.go`:

```go
// ExecuteUIAction calls a plugin's on_ui_action handler
func (m *Manager) ExecuteUIAction(pluginID int64, actionID string, clipIDs []int64, options map[string]interface{}) (*ActionResult, error) {
	m.mu.RLock()
	p, ok := m.plugins[pluginID]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("plugin not found: %d", pluginID)
	}

	if !p.Enabled {
		return nil, fmt.Errorf("plugin is disabled: %s", p.Name)
	}

	if p.Sandbox == nil {
		return nil, fmt.Errorf("plugin sandbox not initialized: %s", p.Name)
	}

	// Call on_ui_action(action_id, clip_ids, options)
	L := p.Sandbox.L

	fn := L.GetGlobal("on_ui_action")
	if fn == lua.LNil {
		return nil, fmt.Errorf("plugin does not implement on_ui_action: %s", p.Name)
	}

	// Convert clip_ids to Lua table
	clipIDsTable := L.NewTable()
	for _, id := range clipIDs {
		clipIDsTable.Append(lua.LNumber(id))
	}

	// Convert options to Lua table
	optionsTable := L.NewTable()
	for k, v := range options {
		switch val := v.(type) {
		case string:
			optionsTable.RawSetString(k, lua.LString(val))
		case float64:
			optionsTable.RawSetString(k, lua.LNumber(val))
		case bool:
			optionsTable.RawSetString(k, lua.LBool(val))
		case int:
			optionsTable.RawSetString(k, lua.LNumber(val))
		}
	}

	// Call the function
	if err := L.CallByParam(lua.P{
		Fn:      fn,
		NRet:    1,
		Protect: true,
	}, lua.LString(actionID), clipIDsTable, optionsTable); err != nil {
		return nil, fmt.Errorf("plugin action failed: %w", err)
	}

	// Get return value
	result := &ActionResult{Success: true}
	ret := L.Get(-1)
	L.Pop(1)

	if tbl, ok := ret.(*lua.LTable); ok {
		if clipID := tbl.RawGetString("result_clip_id"); clipID != lua.LNil {
			if num, ok := clipID.(lua.LNumber); ok {
				result.ResultClipID = int64(num)
			}
		}
	}

	return result, nil
}

// ActionResult type (add near top of file with other types)
type ActionResult struct {
	Success      bool   `json:"success"`
	Error        string `json:"error,omitempty"`
	ResultClipID int64  `json:"result_clip_id,omitempty"`
}
```

**Step 4: Commit**

```bash
git add plugin_service.go plugin/manager.go
git commit -m "$(cat <<'EOF'
feat(plugin): add ExecutePluginAction() for UI action dispatch

PluginService.ExecutePluginAction() calls the plugin's on_ui_action
handler with action ID, clip IDs, and options. Returns ActionResult
with success status and optional result_clip_id.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 10: Regenerate Wails Bindings

**Files:**
- Generated: `frontend/wailsjs/`

**Step 1: Run wails generate**

```bash
cd /Users/egecan/Code/mahpastes/.worktrees/plugin-ui-extensions
~/go/bin/wails generate module
```

**Step 2: Verify new methods appear in bindings**

Check `frontend/wailsjs/go/main/PluginService.js` for:
- `GetPluginUIActions`
- `ExecutePluginAction`

**Step 3: Commit**

```bash
git add frontend/wailsjs/
git commit -m "$(cat <<'EOF'
chore: regenerate Wails bindings

Add GetPluginUIActions and ExecutePluginAction bindings.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 11: Create Test Plugin

**Files:**
- Create: `plugins/test-plugin.lua`

**Step 1: Write the test plugin**

```lua
-- Test Plugin for E2E Testing
-- Exercises all plugin UI extension APIs

Plugin = {
  name = "Test Plugin",
  version = "1.0.0",
  description = "E2E test plugin for UI extensions",
  author = "mahpastes",

  settings = {
    {key = "prefix", type = "text", label = "Output prefix", default = "processed"},
  },

  ui = {
    lightbox_buttons = {
      {id = "test_simple", label = "Test Simple", icon = "sparkles"},
      {id = "test_options", label = "Test Options", icon = "pencil",
        options = {
          {id = "suffix", type = "text", label = "Suffix", default = "_modified"},
          {id = "uppercase", type = "checkbox", label = "Uppercase", default = false},
        }
      },
    },
    card_actions = {
      {id = "test_simple", label = "Test Simple", icon = "sparkles"},
      {id = "test_bulk", label = "Test Bulk", icon = "refresh"},
    },
  },
}

function on_ui_action(action_id, clip_ids, options)
  local settings_json = storage.get("settings") or "{}"
  local settings = json.decode(settings_json) or {}
  local prefix = settings.prefix or "processed"

  local task_id = task.start("Test Processing", #clip_ids)
  local last_clip = nil

  for i, clip_id in ipairs(clip_ids) do
    local data, mime_type = clips.get_data(clip_id)
    local clip = clips.get(clip_id)

    local new_name = prefix .. "_" .. (clip.filename or "clip")

    if options.uppercase then
      new_name = string.upper(new_name)
    end

    if options.suffix then
      new_name = new_name .. options.suffix
    end

    -- Create a copy with modified name
    last_clip = clips.create({
      name = new_name,
      data = data,
      mime_type = mime_type,
    })

    task.progress(task_id, i)
  end

  task.complete(task_id)

  if last_clip then
    return {result_clip_id = last_clip.id}
  end
  return {}
end
```

**Step 2: Commit**

```bash
git add plugins/test-plugin.lua
git commit -m "$(cat <<'EOF'
feat(plugin): add test plugin for e2e testing

Test plugin exercises:
- Lightbox buttons (simple and with options)
- Card actions (simple and bulk)
- clips.get_data() and clips.create()
- task.start/progress/complete
- Settings and storage

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 12: Add Icon Set Constants

**Files:**
- Create: `frontend/js/plugin-icons.js`

**Step 1: Create the icon definitions**

```javascript
// Plugin Icon Set
// Curated set of icons available for plugin UI elements

const PLUGIN_ICONS = {
  // Magic/AI related
  'wand': '<path stroke-linecap="round" stroke-linejoin="round" d="M15.042 21.672L13.684 16.6m0 0l-2.51 2.225.569-9.47 5.227 7.917-3.286-.672zM12 2.25V4.5m5.834.166l-1.591 1.591M20.25 10.5H18M7.757 14.743l-1.59 1.59M6 10.5H3.75m4.007-4.243l-1.59-1.59"/>',
  'sparkles': '<path stroke-linecap="round" stroke-linejoin="round" d="M9.813 15.904L9 18.75l-.813-2.846a4.5 4.5 0 00-3.09-3.09L2.25 12l2.846-.813a4.5 4.5 0 003.09-3.09L9 5.25l.813 2.846a4.5 4.5 0 003.09 3.09L15.75 12l-2.846.813a4.5 4.5 0 00-3.09 3.09zM18.259 8.715L18 9.75l-.259-1.035a3.375 3.375 0 00-2.455-2.456L14.25 6l1.036-.259a3.375 3.375 0 002.455-2.456L18 2.25l.259 1.035a3.375 3.375 0 002.456 2.456L21.75 6l-1.035.259a3.375 3.375 0 00-2.456 2.456zM16.5 20.25h-9"/>',

  // Editing
  'pencil': '<path stroke-linecap="round" stroke-linejoin="round" d="M16.862 4.487l1.687-1.688a1.875 1.875 0 112.652 2.652L10.582 16.07a4.5 4.5 0 01-1.897 1.13L6 18l.8-2.685a4.5 4.5 0 011.13-1.897l8.932-8.931zm0 0L19.5 7.125M18 14v4.75A2.25 2.25 0 0115.75 21H5.25A2.25 2.25 0 013 18.75V8.25A2.25 2.25 0 015.25 6H10"/>',

  // Sizing
  'arrows-expand': '<path stroke-linecap="round" stroke-linejoin="round" d="M3.75 3.75v4.5m0-4.5h4.5m-4.5 0L9 9M3.75 20.25v-4.5m0 4.5h4.5m-4.5 0L9 15M20.25 3.75h-4.5m4.5 0v4.5m0-4.5L15 9m5.25 11.25h-4.5m4.5 0v-4.5m0 4.5L15 15"/>',

  // Refresh/Process
  'refresh': '<path stroke-linecap="round" stroke-linejoin="round" d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0l3.181 3.183a8.25 8.25 0 0013.803-3.7M4.031 9.865a8.25 8.25 0 0113.803-3.7l3.181 3.182m0-4.991v4.99"/>',

  // Image related
  'photo': '<path stroke-linecap="round" stroke-linejoin="round" d="M2.25 15.75l5.159-5.159a2.25 2.25 0 013.182 0l5.159 5.159m-1.5-1.5l1.409-1.409a2.25 2.25 0 013.182 0l2.909 2.909m-18 3.75h16.5a1.5 1.5 0 001.5-1.5V6a1.5 1.5 0 00-1.5-1.5H3.75A1.5 1.5 0 002.25 6v12a1.5 1.5 0 001.5 1.5zm10.5-11.25h.008v.008h-.008V8.25zm.375 0a.375.375 0 11-.75 0 .375.375 0 01.75 0z"/>',
  'vector': '<path stroke-linecap="round" stroke-linejoin="round" d="M9.53 16.122a3 3 0 00-5.78 1.128 2.25 2.25 0 01-2.4 2.245 4.5 4.5 0 008.4-2.245c0-.399-.078-.78-.22-1.128zm0 0a15.998 15.998 0 003.388-1.62m-5.043-.025a15.994 15.994 0 011.622-3.395m3.42 3.42a15.995 15.995 0 004.764-4.648l3.876-5.814a1.151 1.151 0 00-1.597-1.597L14.146 6.32a15.996 15.996 0 00-4.649 4.763m3.42 3.42a6.776 6.776 0 00-3.42-3.42"/>',

  // Color
  'color-swatch': '<path stroke-linecap="round" stroke-linejoin="round" d="M4.098 19.902a3.75 3.75 0 005.304 0l6.401-6.402M6.75 21A3.75 3.75 0 013 17.25V4.125C3 3.504 3.504 3 4.125 3h5.25c.621 0 1.125.504 1.125 1.125v4.072M6.75 21a3.75 3.75 0 003.75-3.75V8.197M6.75 21h13.125c.621 0 1.125-.504 1.125-1.125v-5.25c0-.621-.504-1.125-1.125-1.125h-4.072M10.5 8.197l2.88-2.88c.438-.439 1.15-.439 1.59 0l3.712 3.713c.44.44.44 1.152 0 1.59l-2.879 2.88M6.75 17.25h.008v.008H6.75v-.008z"/>',

  // File operations
  'download': '<path stroke-linecap="round" stroke-linejoin="round" d="M3 16.5v2.25A2.25 2.25 0 005.25 21h13.5A2.25 2.25 0 0021 18.75V16.5M16.5 12L12 16.5m0 0L7.5 12m4.5 4.5V3"/>',
  'upload': '<path stroke-linecap="round" stroke-linejoin="round" d="M3 16.5v2.25A2.25 2.25 0 005.25 21h13.5A2.25 2.25 0 0021 18.75V16.5m-13.5-9L12 3m0 0l4.5 4.5M12 3v13.5"/>',

  // UI elements
  'plus': '<path stroke-linecap="round" stroke-linejoin="round" d="M12 4.5v15m7.5-7.5h-15"/>',
  'minus': '<path stroke-linecap="round" stroke-linejoin="round" d="M19.5 12h-15"/>',
  'check': '<path stroke-linecap="round" stroke-linejoin="round" d="M4.5 12.75l6 6 9-13.5"/>',
  'x-mark': '<path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12"/>',

  // Misc
  'cog': '<path stroke-linecap="round" stroke-linejoin="round" d="M9.594 3.94c.09-.542.56-.94 1.11-.94h2.593c.55 0 1.02.398 1.11.94l.213 1.281c.063.374.313.686.645.87.074.04.147.083.22.127.324.196.72.257 1.075.124l1.217-.456a1.125 1.125 0 011.37.49l1.296 2.247a1.125 1.125 0 01-.26 1.431l-1.003.827c-.293.24-.438.613-.431.992a6.759 6.759 0 010 .255c-.007.378.138.75.43.99l1.005.828c.424.35.534.954.26 1.43l-1.298 2.247a1.125 1.125 0 01-1.369.491l-1.217-.456c-.355-.133-.75-.072-1.076.124a6.57 6.57 0 01-.22.128c-.331.183-.581.495-.644.869l-.213 1.28c-.09.543-.56.941-1.11.941h-2.594c-.55 0-1.02-.398-1.11-.94l-.213-1.281c-.062-.374-.312-.686-.644-.87a6.52 6.52 0 01-.22-.127c-.325-.196-.72-.257-1.076-.124l-1.217.456a1.125 1.125 0 01-1.369-.49l-1.297-2.247a1.125 1.125 0 01.26-1.431l1.004-.827c.292-.24.437-.613.43-.992a6.932 6.932 0 010-.255c.007-.378-.138-.75-.43-.99l-1.004-.828a1.125 1.125 0 01-.26-1.43l1.297-2.247a1.125 1.125 0 011.37-.491l1.216.456c.356.133.751.072 1.076-.124.072-.044.146-.087.22-.128.332-.183.582-.495.644-.869l.214-1.281z"/><path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"/>',
  'bolt': '<path stroke-linecap="round" stroke-linejoin="round" d="M3.75 13.5l10.5-11.25L12 10.5h8.25L9.75 21.75 12 13.5H3.75z"/>',
};

// Get SVG for an icon name
function getPluginIcon(name) {
  const path = PLUGIN_ICONS[name];
  if (!path) return null;
  return `<svg class="w-4 h-4" fill="none" stroke="currentColor" stroke-width="1.5" viewBox="0 0 24 24">${path}</svg>`;
}
```

**Step 2: Add script to index.html**

Add before `</body>`:

```html
<script src="js/plugin-icons.js"></script>
```

**Step 3: Commit**

```bash
git add frontend/js/plugin-icons.js frontend/index.html
git commit -m "$(cat <<'EOF'
feat(frontend): add plugin icon set

Curated set of ~20 icons for plugin UI elements. Icons use consistent
stroke style matching the app's design system.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 13: Add Card Menu Dropdown

**Files:**
- Modify: `frontend/js/ui.js`
- Modify: `frontend/css/main.css`

**Step 1: Update createClipCard to use dropdown menu**

Replace the action buttons section in createClipCard (around line 76-104):

```javascript
                <div class="flex gap-0.5">
                    <button class="card-menu-trigger p-1 text-stone-400 hover:text-stone-600 hover:bg-stone-100 rounded transition-colors"
                            data-action="menu"
                            aria-label="Actions"
                            aria-haspopup="true"
                            aria-expanded="false">
                        <svg class="w-3 h-3" fill="none" stroke="currentColor" stroke-width="1.5" viewBox="0 0 24 24">
                            <path stroke-linecap="round" stroke-linejoin="round" d="M12 6.75a.75.75 0 110-1.5.75.75 0 010 1.5zM12 12.75a.75.75 0 110-1.5.75.75 0 010 1.5zM12 18.75a.75.75 0 110-1.5.75.75 0 010 1.5z" />
                        </svg>
                    </button>
                </div>
```

**Step 2: Add menu rendering function**

```javascript
// Plugin actions cache
let pluginUIActions = null;

async function loadPluginUIActions() {
    try {
        pluginUIActions = await window.go.main.PluginService.GetPluginUIActions();
    } catch (e) {
        console.error('Failed to load plugin UI actions:', e);
        pluginUIActions = { lightbox_buttons: [], card_actions: [] };
    }
}

function renderCardMenu(clipId, button) {
    // Remove any existing menu
    const existingMenu = document.querySelector('.card-menu-dropdown');
    if (existingMenu) existingMenu.remove();

    const menu = document.createElement('div');
    menu.className = 'card-menu-dropdown';
    menu.setAttribute('role', 'menu');

    // Built-in actions
    const builtInActions = [
        { id: 'copy-path', label: 'Copy Path', icon: 'clipboard' },
        { id: 'save-file', label: 'Save', icon: 'download' },
        { id: 'archive', label: isViewingArchive ? 'Restore' : 'Archive', icon: 'archive' },
        { id: 'delete', label: 'Delete', icon: 'trash', danger: true },
    ];

    builtInActions.forEach(action => {
        const item = document.createElement('button');
        item.className = `card-menu-item ${action.danger ? 'card-menu-item-danger' : ''}`;
        item.setAttribute('role', 'menuitem');
        item.dataset.action = action.id;
        item.dataset.clipId = clipId;
        item.innerHTML = `<span class="card-menu-icon">${getMenuIcon(action.icon)}</span>${action.label}`;
        menu.appendChild(item);
    });

    // Plugin actions
    if (pluginUIActions && pluginUIActions.card_actions.length > 0) {
        const divider = document.createElement('hr');
        divider.className = 'card-menu-divider';
        divider.setAttribute('role', 'separator');
        menu.appendChild(divider);

        pluginUIActions.card_actions.forEach(action => {
            const item = document.createElement('button');
            item.className = 'card-menu-item';
            item.setAttribute('role', 'menuitem');
            item.dataset.action = 'plugin';
            item.dataset.pluginId = action.plugin_id;
            item.dataset.actionId = action.id;
            item.dataset.clipId = clipId;
            const icon = action.icon ? getPluginIcon(action.icon) : '';
            item.innerHTML = `<span class="card-menu-icon">${icon || ''}</span>${action.label}`;
            menu.appendChild(item);
        });
    }

    // Position menu
    const rect = button.getBoundingClientRect();
    menu.style.position = 'fixed';
    menu.style.top = `${rect.bottom + 4}px`;
    menu.style.right = `${window.innerWidth - rect.right}px`;

    document.body.appendChild(menu);

    // Setup keyboard navigation
    setupMenuKeyboard(menu);

    // Close on click outside
    const closeHandler = (e) => {
        if (!menu.contains(e.target) && e.target !== button) {
            menu.remove();
            document.removeEventListener('click', closeHandler);
        }
    };
    setTimeout(() => document.addEventListener('click', closeHandler), 0);

    return menu;
}

function getMenuIcon(name) {
    const icons = {
        clipboard: '<path stroke-linecap="round" stroke-linejoin="round" d="M8 5H6a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2v-1M8 5a2 2 0 002 2h2a2 2 0 002-2M8 5a2 2 0 012-2h2a2 2 0 012 2"/>',
        download: '<path stroke-linecap="round" stroke-linejoin="round" d="M4 16v1a2 2 0 002 2h12a2 2 0 002-2v-1m-4-4l-4 4m0 0l-4-4m4 4V4"/>',
        archive: '<path stroke-linecap="round" stroke-linejoin="round" d="M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4"/>',
        trash: '<path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/>',
    };
    const path = icons[name] || '';
    return `<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" stroke-width="1.5" viewBox="0 0 24 24">${path}</svg>`;
}

function setupMenuKeyboard(menu) {
    const items = menu.querySelectorAll('[role="menuitem"]');
    let currentIndex = 0;

    items[0]?.focus();

    menu.addEventListener('keydown', (e) => {
        if (e.key === 'ArrowDown') {
            e.preventDefault();
            currentIndex = (currentIndex + 1) % items.length;
            items[currentIndex].focus();
        } else if (e.key === 'ArrowUp') {
            e.preventDefault();
            currentIndex = (currentIndex - 1 + items.length) % items.length;
            items[currentIndex].focus();
        } else if (e.key === 'Escape') {
            menu.remove();
        } else if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            items[currentIndex].click();
        }
    });
}
```

**Step 3: Add CSS for dropdown**

In `frontend/css/main.css`:

```css
/* Card Menu Dropdown */
.card-menu-dropdown {
    @apply bg-white rounded-md shadow-lg border border-stone-200 py-1 z-50 min-w-[140px];
}

.card-menu-item {
    @apply w-full px-3 py-1.5 text-xs font-medium text-stone-700 flex items-center gap-2 hover:bg-stone-100 transition-colors text-left;
}

.card-menu-item:focus {
    @apply bg-stone-100 outline-none;
}

.card-menu-item-danger {
    @apply text-red-600 hover:bg-red-50;
}

.card-menu-icon {
    @apply w-3.5 h-3.5 text-stone-400;
}

.card-menu-item-danger .card-menu-icon {
    @apply text-red-400;
}

.card-menu-divider {
    @apply border-stone-200 my-1;
}
```

**Step 4: Add event delegation for menu actions**

```javascript
// Handle card menu clicks (add to app.js initialization)
document.addEventListener('click', async (e) => {
    const menuTrigger = e.target.closest('[data-action="menu"]');
    if (menuTrigger) {
        e.stopPropagation();
        const card = menuTrigger.closest('[data-id]');
        if (card) {
            renderCardMenu(card.dataset.id, menuTrigger);
        }
        return;
    }

    const menuItem = e.target.closest('.card-menu-item');
    if (menuItem) {
        e.stopPropagation();
        const menu = menuItem.closest('.card-menu-dropdown');
        const action = menuItem.dataset.action;
        const clipId = Number(menuItem.dataset.clipId);

        if (action === 'plugin') {
            await executePluginAction(
                Number(menuItem.dataset.pluginId),
                menuItem.dataset.actionId,
                [clipId]
            );
        } else {
            handleCardAction(action, clipId);
        }

        menu?.remove();
    }
});

function handleCardAction(action, clipId) {
    switch (action) {
        case 'copy-path':
            saveTempFile(clipId);
            break;
        case 'save-file':
            saveClipToFile(clipId);
            break;
        case 'archive':
            toggleArchiveClip(clipId);
            break;
        case 'delete':
            deleteClip(clipId);
            break;
    }
}
```

**Step 5: Commit**

```bash
git add frontend/js/ui.js frontend/css/main.css
git commit -m "$(cat <<'EOF'
feat(frontend): add card menu dropdown with plugin actions

Consolidate card action buttons into dropdown menu:
- Built-in actions: Copy Path, Save, Archive, Delete
- Divider separator
- Plugin actions from enabled plugins

Includes keyboard navigation (arrow keys, Enter, Escape) and
proper ARIA attributes for accessibility.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 14: Add Lightbox Plugin Buttons

**Files:**
- Modify: `frontend/js/modals.js`

**Step 1: Add plugin button rendering function**

```javascript
// Render plugin buttons in lightbox
async function renderLightboxPluginButtons() {
    const container = document.getElementById('lightbox-plugin-actions');
    if (!container) return;

    container.innerHTML = '';

    if (!pluginUIActions || pluginUIActions.lightbox_buttons.length === 0) {
        container.classList.add('hidden');
        return;
    }

    pluginUIActions.lightbox_buttons.forEach(action => {
        const btn = document.createElement('button');
        btn.className = 'lightbox-plugin-btn';
        btn.dataset.pluginId = action.plugin_id;
        btn.dataset.actionId = action.id;
        btn.dataset.hasOptions = action.options && action.options.length > 0 ? 'true' : 'false';

        const icon = action.icon ? getPluginIcon(action.icon) : '';
        btn.innerHTML = `${icon}<span>${action.label}</span>`;

        btn.addEventListener('click', () => handleLightboxPluginAction(action));
        container.appendChild(btn);
    });

    container.classList.remove('hidden');
}

async function handleLightboxPluginAction(action) {
    const clip = imageClips[currentLightboxIndex];
    if (!clip) return;

    if (action.options && action.options.length > 0) {
        // Show options dialog
        openPluginOptionsDialog(action, [clip.id]);
    } else {
        // Execute directly
        await executePluginAction(action.plugin_id, action.id, [clip.id]);
    }
}
```

**Step 2: Add container to lightbox HTML**

In `frontend/index.html`, add after `lightbox-ai-actions` div (around line 453):

```html
            <div id="lightbox-plugin-actions" class="lightbox-plugin-actions hidden">
                <!-- Plugin buttons rendered by JS -->
            </div>
```

**Step 3: Add CSS for lightbox plugin buttons**

In `frontend/css/modals.css`:

```css
/* Lightbox Plugin Buttons */
.lightbox-plugin-actions {
    display: flex;
    gap: 0.5rem;
    margin-right: 1rem;
}

.lightbox-plugin-btn {
    @apply flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium text-stone-300 bg-stone-800/50 hover:bg-stone-700/50 rounded-md transition-colors;
}

.lightbox-plugin-btn svg {
    @apply w-3.5 h-3.5;
}
```

**Step 4: Call renderLightboxPluginButtons in openLightbox**

In `openLightbox` function, add:

```javascript
    // Render plugin buttons
    renderLightboxPluginButtons();
```

**Step 5: Commit**

```bash
git add frontend/js/modals.js frontend/index.html frontend/css/modals.css
git commit -m "$(cat <<'EOF'
feat(frontend): add plugin buttons to lightbox

Plugin buttons appear in lightbox bottom bar. Buttons with options
open a dialog; buttons without options execute directly.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 15: Add Plugin Options Dialog

**Files:**
- Modify: `frontend/js/modals.js`
- Modify: `frontend/index.html`
- Modify: `frontend/css/modals.css`

**Step 1: Add dialog HTML**

In `frontend/index.html`, add before `</body>`:

```html
    <!-- Plugin Options Dialog -->
    <div id="plugin-options-modal" class="modal-backdrop opacity-0 pointer-events-none" role="dialog" aria-modal="true">
        <div class="modal-content max-w-md">
            <div class="modal-header">
                <h3 id="plugin-options-title" class="text-sm font-semibold text-stone-800">Action Options</h3>
                <button id="plugin-options-close" class="p-1 hover:bg-stone-100 rounded transition-colors" aria-label="Close">
                    <svg class="w-4 h-4 text-stone-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M6 18L18 6M6 6l12 12"/>
                    </svg>
                </button>
            </div>
            <form id="plugin-options-form" class="modal-body">
                <!-- Form fields rendered by JS -->
            </form>
            <div class="modal-footer">
                <button type="button" id="plugin-options-cancel" class="btn-secondary">Cancel</button>
                <button type="submit" form="plugin-options-form" class="btn-primary">Process</button>
            </div>
        </div>
    </div>
```

**Step 2: Add dialog JavaScript**

```javascript
// Plugin options dialog state
let currentPluginAction = null;
let currentActionClipIds = [];

function openPluginOptionsDialog(action, clipIds) {
    currentPluginAction = action;
    currentActionClipIds = clipIds;

    const modal = document.getElementById('plugin-options-modal');
    const title = document.getElementById('plugin-options-title');
    const form = document.getElementById('plugin-options-form');

    // Set title
    const clipCount = clipIds.length;
    title.textContent = `${action.label} - ${clipCount} ${clipCount === 1 ? 'clip' : 'clips'}`;

    // Render form fields
    form.innerHTML = '';
    action.options.forEach(field => {
        const wrapper = document.createElement('div');
        wrapper.className = 'form-field';

        const label = document.createElement('label');
        label.className = 'form-label';
        label.textContent = field.label;
        if (field.required) {
            label.innerHTML += '<span class="text-red-500 ml-1">*</span>';
        }
        label.setAttribute('for', `plugin-opt-${field.id}`);

        let input;
        switch (field.type) {
            case 'checkbox':
                input = document.createElement('input');
                input.type = 'checkbox';
                input.className = 'form-checkbox';
                input.checked = field.default === true;
                break;

            case 'select':
                input = document.createElement('select');
                input.className = 'form-select';
                field.choices?.forEach(choice => {
                    const opt = document.createElement('option');
                    opt.value = choice.value;
                    opt.textContent = choice.label;
                    if (choice.value === field.default) opt.selected = true;
                    input.appendChild(opt);
                });
                break;

            case 'range':
                input = document.createElement('input');
                input.type = 'range';
                input.className = 'form-range';
                input.min = field.min || 0;
                input.max = field.max || 1;
                input.step = field.step || 0.1;
                input.value = field.default || field.min || 0;

                const valueDisplay = document.createElement('span');
                valueDisplay.className = 'form-range-value';
                valueDisplay.textContent = input.value;
                input.addEventListener('input', () => valueDisplay.textContent = input.value);

                wrapper.appendChild(label);
                const rangeWrapper = document.createElement('div');
                rangeWrapper.className = 'form-range-wrapper';
                rangeWrapper.appendChild(input);
                rangeWrapper.appendChild(valueDisplay);
                wrapper.appendChild(rangeWrapper);
                input.id = `plugin-opt-${field.id}`;
                input.name = field.id;
                form.appendChild(wrapper);
                return;

            default: // text, password
                input = document.createElement('input');
                input.type = field.type === 'password' ? 'password' : 'text';
                input.className = 'form-input';
                input.value = field.default || '';
                input.placeholder = field.label;
        }

        input.id = `plugin-opt-${field.id}`;
        input.name = field.id;
        if (field.required) input.required = true;

        wrapper.appendChild(label);
        if (field.type === 'checkbox') {
            const checkWrapper = document.createElement('div');
            checkWrapper.className = 'form-checkbox-wrapper';
            checkWrapper.appendChild(input);
            wrapper.appendChild(checkWrapper);
        } else {
            wrapper.appendChild(input);
        }
        form.appendChild(wrapper);
    });

    // Show modal
    modal.classList.remove('opacity-0', 'pointer-events-none');
    modal.classList.add('opacity-100');
}

function closePluginOptionsDialog() {
    const modal = document.getElementById('plugin-options-modal');
    modal.classList.remove('opacity-100');
    modal.classList.add('opacity-0', 'pointer-events-none');
    currentPluginAction = null;
    currentActionClipIds = [];
}

// Initialize dialog event listeners
document.getElementById('plugin-options-close')?.addEventListener('click', closePluginOptionsDialog);
document.getElementById('plugin-options-cancel')?.addEventListener('click', closePluginOptionsDialog);

document.getElementById('plugin-options-form')?.addEventListener('submit', async (e) => {
    e.preventDefault();

    if (!currentPluginAction) return;

    // Gather form values
    const formData = new FormData(e.target);
    const options = {};

    currentPluginAction.options.forEach(field => {
        const value = formData.get(field.id);
        switch (field.type) {
            case 'checkbox':
                options[field.id] = document.getElementById(`plugin-opt-${field.id}`).checked;
                break;
            case 'range':
                options[field.id] = parseFloat(value);
                break;
            default:
                options[field.id] = value;
        }
    });

    closePluginOptionsDialog();
    await executePluginAction(currentPluginAction.plugin_id, currentPluginAction.id, currentActionClipIds, options);
});
```

**Step 3: Add CSS for dialog**

```css
/* Plugin Options Dialog */
.form-field {
    @apply mb-4;
}

.form-label {
    @apply block text-xs font-medium text-stone-700 mb-1;
}

.form-input {
    @apply w-full px-3 py-2 text-sm border border-stone-200 rounded-md focus:outline-none focus:border-stone-400 focus:ring-1 focus:ring-stone-400/20;
}

.form-select {
    @apply w-full px-3 py-2 text-sm border border-stone-200 rounded-md bg-white focus:outline-none focus:border-stone-400;
}

.form-checkbox-wrapper {
    @apply flex items-center;
}

.form-checkbox {
    @apply w-4 h-4 rounded border-stone-300 text-stone-800 focus:ring-stone-400/20;
}

.form-range-wrapper {
    @apply flex items-center gap-3;
}

.form-range {
    @apply flex-1 h-2 bg-stone-200 rounded-lg appearance-none cursor-pointer;
}

.form-range-value {
    @apply text-xs font-medium text-stone-600 min-w-[3rem] text-right;
}
```

**Step 4: Commit**

```bash
git add frontend/js/modals.js frontend/index.html frontend/css/modals.css
git commit -m "$(cat <<'EOF'
feat(frontend): add plugin options dialog

Dialog for plugin actions that require options:
- Text, password, checkbox, select, and range inputs
- Title shows action name and clip count
- Form validation with required field indicators

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 16: Add executePluginAction Function

**Files:**
- Create: `frontend/js/plugins-ui.js`

**Step 1: Create the plugin UI coordination file**

```javascript
// Plugin UI Coordination
// Handles plugin action execution and result handling

async function executePluginAction(pluginId, actionId, clipIds, options = {}) {
    try {
        // Show loading state
        showToast(`Processing ${clipIds.length} clip${clipIds.length === 1 ? '' : 's'}...`, 'info');

        const result = await window.go.main.PluginService.ExecutePluginAction(
            pluginId,
            actionId,
            clipIds,
            options
        );

        if (result.success) {
            showToast('Processing complete', 'success');

            // Refresh clips to show new results
            if (typeof loadClips === 'function') {
                await loadClips();
            }

            // If result has a clip ID, offer to view it
            if (result.result_clip_id) {
                // Find the new clip and optionally open it
                // For now, just scroll to it
                setTimeout(() => {
                    const newCard = document.querySelector(`[data-id="${result.result_clip_id}"]`);
                    if (newCard) {
                        newCard.scrollIntoView({ behavior: 'smooth', block: 'center' });
                        newCard.classList.add('ring-2', 'ring-stone-400');
                        setTimeout(() => newCard.classList.remove('ring-2', 'ring-stone-400'), 2000);
                    }
                }, 500);
            }
        } else {
            showToast(result.error || 'Action failed', 'error');
        }
    } catch (e) {
        console.error('Plugin action failed:', e);
        showToast('Plugin action failed: ' + e.message, 'error');
    }
}

// Listen for plugin task events
if (typeof runtime !== 'undefined') {
    runtime.EventsOn('plugin:task:started', (data) => {
        console.log('Plugin task started:', data);
        // Could show task in task queue UI
    });

    runtime.EventsOn('plugin:task:progress', (data) => {
        console.log('Plugin task progress:', data);
        // Could update progress UI
    });

    runtime.EventsOn('plugin:task:completed', (data) => {
        console.log('Plugin task completed:', data);
    });

    runtime.EventsOn('plugin:task:failed', (data) => {
        console.error('Plugin task failed:', data);
    });
}

// Initialize plugin UI on app load
async function initPluginUI() {
    await loadPluginUIActions();
}

// Call on app ready
if (typeof window.__appReady !== 'undefined' && window.__appReady) {
    initPluginUI();
} else {
    window.addEventListener('load', () => {
        setTimeout(initPluginUI, 100);
    });
}
```

**Step 2: Add script to index.html**

Add after plugin-icons.js:

```html
<script src="js/plugins-ui.js"></script>
```

**Step 3: Commit**

```bash
git add frontend/js/plugins-ui.js frontend/index.html
git commit -m "$(cat <<'EOF'
feat(frontend): add plugin action execution

executePluginAction() handles:
- Calling PluginService.ExecutePluginAction
- Showing loading/success/error toasts
- Refreshing clips after processing
- Highlighting result clip in gallery

Also listens for plugin task events for potential progress UI.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 17: Add E2E Tests for Plugin UI

**Files:**
- Create: `e2e/tests/plugins/ui-actions.spec.ts`
- Modify: `e2e/helpers/selectors.ts`
- Modify: `e2e/fixtures/test-fixtures.ts`

**Step 1: Add selectors**

In `e2e/helpers/selectors.ts`:

```typescript
  pluginUI: {
    cardMenuTrigger: '[data-action="menu"]',
    cardMenuDropdown: '.card-menu-dropdown',
    cardMenuItem: '.card-menu-item',
    cardMenuPluginItem: '.card-menu-item[data-action="plugin"]',
    lightboxPluginActions: '#lightbox-plugin-actions',
    lightboxPluginBtn: '.lightbox-plugin-btn',
    optionsModal: '#plugin-options-modal',
    optionsForm: '#plugin-options-form',
    optionsCancel: '#plugin-options-cancel',
    optionsSubmit: '#plugin-options-form button[type="submit"]',
  },
```

**Step 2: Add test fixtures methods**

In `e2e/fixtures/test-fixtures.ts`:

```typescript
  // ==================== Plugin UI ====================

  async openCardMenu(filename: string): Promise<void> {
    const clip = await this.getClipByFilename(filename);
    await clip.hover();
    await clip.locator(selectors.pluginUI.cardMenuTrigger).click();
    await this.page.waitForSelector(selectors.pluginUI.cardMenuDropdown);
  }

  async clickCardMenuAction(actionId: string): Promise<void> {
    const menu = this.page.locator(selectors.pluginUI.cardMenuDropdown);
    await menu.locator(`[data-action="${actionId}"]`).click();
  }

  async clickCardMenuPluginAction(pluginId: number, actionId: string): Promise<void> {
    const menu = this.page.locator(selectors.pluginUI.cardMenuDropdown);
    await menu.locator(`[data-plugin-id="${pluginId}"][data-action-id="${actionId}"]`).click();
  }

  async clickLightboxPluginButton(actionId: string): Promise<void> {
    await this.page.locator(`${selectors.pluginUI.lightboxPluginBtn}[data-action-id="${actionId}"]`).click();
  }

  async getPluginUIActions(): Promise<{ lightbox_buttons: any[], card_actions: any[] }> {
    return this.page.evaluate(async () => {
      // @ts-ignore
      return await window.go.main.PluginService.GetPluginUIActions();
    });
  }

  async executePluginActionViaAPI(pluginId: number, actionId: string, clipIds: number[], options: any = {}): Promise<any> {
    return this.page.evaluate(async ({ pId, aId, cIds, opts }) => {
      // @ts-ignore
      return await window.go.main.PluginService.ExecutePluginAction(pId, aId, cIds, opts);
    }, { pId: pluginId, aId: actionId, cIds: clipIds, opts: options });
  }
```

**Step 3: Create test file**

```typescript
import { test, expect } from '../../fixtures/test-fixtures.js';
import { createTempFile, generateTestImage } from '../../helpers/test-data.js';
import * as path from 'path';
import * as fs from 'fs/promises';

const TEST_PLUGIN_PATH = path.resolve(__dirname, '../../../plugins/test-plugin.lua');

test.describe('Plugin UI Extensions', () => {
  test.describe('Plugin UI Actions API', () => {
    test('should return empty actions when no plugins enabled', async ({ app }) => {
      const actions = await app.getPluginUIActions();
      expect(actions.lightbox_buttons).toHaveLength(0);
      expect(actions.card_actions).toHaveLength(0);
    });

    test('should return actions from enabled plugin', async ({ app }) => {
      // Import and enable test plugin
      const plugin = await app.importPluginFromPath(TEST_PLUGIN_PATH);
      expect(plugin).not.toBeNull();

      await app.enablePlugin(plugin!.id);
      await app.page.waitForTimeout(500);

      const actions = await app.getPluginUIActions();
      expect(actions.lightbox_buttons.length).toBeGreaterThan(0);
      expect(actions.card_actions.length).toBeGreaterThan(0);

      // Verify action structure
      const lightboxBtn = actions.lightbox_buttons[0];
      expect(lightboxBtn.plugin_id).toBe(plugin!.id);
      expect(lightboxBtn.id).toBeDefined();
      expect(lightboxBtn.label).toBeDefined();
    });
  });

  test.describe('Card Menu', () => {
    test('should show dropdown menu on card', async ({ app }) => {
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      await app.openCardMenu(path.basename(imagePath));

      // Verify menu appears
      const menu = app.page.locator('.card-menu-dropdown');
      await expect(menu).toBeVisible();

      // Verify built-in actions
      await expect(menu.locator('[data-action="copy-path"]')).toBeVisible();
      await expect(menu.locator('[data-action="delete"]')).toBeVisible();
    });

    test('should show plugin actions in menu when plugin enabled', async ({ app }) => {
      // Setup plugin
      const plugin = await app.importPluginFromPath(TEST_PLUGIN_PATH);
      await app.enablePlugin(plugin!.id);
      await app.page.reload();
      await app.waitForReady();

      // Upload clip
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);

      // Open menu
      await app.openCardMenu(path.basename(imagePath));

      // Verify plugin actions appear
      const menu = app.page.locator('.card-menu-dropdown');
      const pluginItems = menu.locator('[data-action="plugin"]');
      await expect(pluginItems.first()).toBeVisible();
    });
  });

  test.describe('Lightbox Plugin Buttons', () => {
    test('should show plugin buttons in lightbox', async ({ app }) => {
      // Setup plugin
      const plugin = await app.importPluginFromPath(TEST_PLUGIN_PATH);
      await app.enablePlugin(plugin!.id);
      await app.page.reload();
      await app.waitForReady();

      // Upload image
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);

      // Open lightbox
      await app.openLightbox(path.basename(imagePath));

      // Verify plugin buttons
      const pluginActions = app.page.locator('#lightbox-plugin-actions');
      await expect(pluginActions).toBeVisible();
      await expect(pluginActions.locator('.lightbox-plugin-btn').first()).toBeVisible();
    });
  });

  test.describe('Plugin Action Execution', () => {
    test('should execute simple action and create new clip', async ({ app }) => {
      // Setup plugin
      const plugin = await app.importPluginFromPath(TEST_PLUGIN_PATH);
      await app.enablePlugin(plugin!.id);

      // Upload clip
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);
      await app.expectClipCount(1);

      // Get clip ID
      const clips = await app.page.evaluate(async () => {
        // @ts-ignore
        return await window.go.main.App.GetClips(false, []);
      });
      const clipId = clips[0].id;

      // Execute action
      const result = await app.executePluginActionViaAPI(plugin!.id, 'test_simple', [clipId]);
      expect(result.success).toBe(true);
      expect(result.result_clip_id).toBeGreaterThan(0);

      // Verify new clip created
      await app.page.reload();
      await app.waitForReady();
      await app.expectClipCount(2);
    });

    test('should execute action with options', async ({ app }) => {
      // Setup plugin
      const plugin = await app.importPluginFromPath(TEST_PLUGIN_PATH);
      await app.enablePlugin(plugin!.id);

      // Upload clip
      const imagePath = await createTempFile(generateTestImage(), 'png');
      await app.uploadFile(imagePath);

      const clips = await app.page.evaluate(async () => {
        // @ts-ignore
        return await window.go.main.App.GetClips(false, []);
      });

      // Execute with options
      const result = await app.executePluginActionViaAPI(
        plugin!.id,
        'test_options',
        [clips[0].id],
        { suffix: '_custom', uppercase: true }
      );

      expect(result.success).toBe(true);

      // Verify new clip has expected name
      await app.page.reload();
      await app.waitForReady();

      const newClips = await app.page.evaluate(async () => {
        // @ts-ignore
        return await window.go.main.App.GetClips(false, []);
      });

      const newClip = newClips.find((c: any) => c.id === result.result_clip_id);
      expect(newClip).toBeDefined();
      expect(newClip.filename).toContain('_CUSTOM');
    });

    test('should handle bulk action', async ({ app }) => {
      // Setup plugin
      const plugin = await app.importPluginFromPath(TEST_PLUGIN_PATH);
      await app.enablePlugin(plugin!.id);

      // Upload multiple clips
      const paths = await Promise.all([
        createTempFile(generateTestImage(100, 100, 'red'), 'png'),
        createTempFile(generateTestImage(100, 100, 'blue'), 'png'),
      ]);
      await app.uploadFiles(paths);
      await app.expectClipCount(2);

      const clips = await app.page.evaluate(async () => {
        // @ts-ignore
        return await window.go.main.App.GetClips(false, []);
      });

      // Execute bulk action
      const result = await app.executePluginActionViaAPI(
        plugin!.id,
        'test_bulk',
        clips.map((c: any) => c.id)
      );

      expect(result.success).toBe(true);

      // Verify clips created
      await app.page.reload();
      await app.waitForReady();
      await app.expectClipCount(4); // 2 original + 2 processed
    });
  });
});
```

**Step 4: Commit**

```bash
git add e2e/tests/plugins/ui-actions.spec.ts e2e/helpers/selectors.ts e2e/fixtures/test-fixtures.ts
git commit -m "$(cat <<'EOF'
test(e2e): add plugin UI extensions tests

Tests cover:
- GetPluginUIActions API
- Card menu dropdown with plugin actions
- Lightbox plugin buttons
- Action execution (simple, with options, bulk)

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 18: Run Tests and Fix Issues

**Step 1: Build the app**

```bash
cd /Users/egecan/Code/mahpastes/.worktrees/plugin-ui-extensions
~/go/bin/wails build
```

**Step 2: Run e2e tests**

```bash
cd e2e
npm test -- --grep "Plugin UI"
```

**Step 3: Fix any failing tests**

Iterate on fixes until all tests pass.

**Step 4: Commit fixes**

```bash
git add -A
git commit -m "$(cat <<'EOF'
fix: address test failures in plugin UI extensions

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 19: Create FAL.AI Plugin (Stub)

**Files:**
- Create: `plugins/fal-ai.lua`

**Note:** Full fal.ai plugin implementation is deferred. This creates the structure for future completion.

**Step 1: Create the plugin stub**

```lua
-- FAL.AI Image Processing Plugin
-- AI-powered image colorization, upscaling, restoration, and editing

Plugin = {
  name = "FAL.AI Image Processing",
  version = "1.0.0",
  description = "AI-powered image colorization, upscaling, restoration, and editing",
  author = "mahpastes",

  settings = {
    {key = "api_key", type = "password", label = "FAL.AI API Key"},
  },

  ui = {
    lightbox_buttons = {
      {id = "colorize", label = "Colorize", icon = "wand"},
      {id = "upscale_esrgan", label = "Upscale (ESRGAN)", icon = "arrows-expand"},
      {id = "upscale_clarity", label = "Upscale (Clarity)", icon = "arrows-expand"},
      {id = "restore_codeformer", label = "Restore (CodeFormer)", icon = "sparkles",
        options = {
          {id = "fix_colors", type = "checkbox", label = "Fix faded colors", default = true},
          {id = "remove_scratches", type = "checkbox", label = "Remove scratches", default = true},
        }
      },
      {id = "edit_flux2", label = "AI Edit (Flux 2)", icon = "pencil",
        options = {
          {id = "prompt", type = "text", label = "Describe the edit", required = true},
          {id = "strength", type = "range", label = "Edit strength", min = 0.1, max = 1, step = 0.1, default = 0.8},
        }
      },
      {id = "vectorize", label = "Vectorize (SVG)", icon = "vector"},
    },
    card_actions = {
      {id = "colorize", label = "Colorize", icon = "wand"},
      {id = "upscale_esrgan", label = "Upscale (ESRGAN)", icon = "arrows-expand"},
    },
  },

  network = {
    ["fal.ai"] = {"POST"},
    ["fal.media"] = {"GET"},
  },
}

-- FAL.AI endpoints
local FAL_ENDPOINTS = {
  colorize = "fal-ai/ddcolor",
  upscale_esrgan = "fal-ai/esrgan",
  upscale_clarity = "fal-ai/clarity-upscaler",
  restore_codeformer = "fal-ai/codeformer",
  edit_flux2 = "fal-ai/flux-pro/v1.1-ultra/edit",
  vectorize = "fal-ai/imageutils/vectorize",
}

function get_api_key()
  local settings_json = storage.get("settings") or "{}"
  local settings = json.decode(settings_json) or {}
  return settings.api_key
end

function on_ui_action(action_id, clip_ids, options)
  local api_key = get_api_key()
  if not api_key or api_key == "" then
    toast.show("Please configure your FAL.AI API key in Settings → Plugins", "error")
    return {}
  end

  -- TODO: Implement actual fal.ai API calls
  -- For now, show a message that this is not yet implemented
  toast.show("FAL.AI plugin coming soon - " .. action_id, "info")
  return {}
end
```

**Step 2: Commit**

```bash
git add plugins/fal-ai.lua
git commit -m "$(cat <<'EOF'
feat(plugin): add fal.ai plugin stub

Plugin structure with all UI elements defined. Actual API implementation
deferred - currently shows placeholder message.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 20: Final Integration Test

**Step 1: Run full test suite**

```bash
cd /Users/egecan/Code/mahpastes/.worktrees/plugin-ui-extensions/e2e
npm test
```

**Step 2: Fix any regressions**

**Step 3: Final commit**

```bash
git add -A
git commit -m "$(cat <<'EOF'
chore: finalize plugin UI extensions implementation

All tests passing. Ready for review.

Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>
EOF
)"
```

---

## Summary

This plan implements plugin UI extensions in 20 tasks:

1. **Backend (Tasks 1-9):** Manifest parsing, Lua APIs, PluginService methods
2. **Frontend (Tasks 10-16):** Icons, card menu, lightbox buttons, options dialog
3. **Testing (Tasks 17-18):** E2E tests and fixes
4. **Plugins (Tasks 11, 19):** Test plugin and fal.ai stub
5. **Integration (Task 20):** Final verification

Each task is atomic and builds on previous work. The test plugin enables e2e testing without external dependencies.
