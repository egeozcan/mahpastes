# Plugin Result Modals Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a modal system that lets plugins display rendered markdown, images, and text with "Copy to clipboard" and "Add as paste" actions.

**Architecture:** New `modal` Lua API emits Wails events; `on_ui_action` return values extended to include nested `modal` table. Frontend renders content using vendored marked.js + DOMPurify, displayed in a new `#plugin-result-modal`.

**Tech Stack:** Go (Wails, gopher-lua), vanilla JS, marked.js, DOMPurify, Tailwind CSS

---

### Task 1: Vendor marked.js and DOMPurify

**Files:**
- Create: `frontend/vendor/marked.min.js`
- Create: `frontend/vendor/purify.min.js`

**Step 1: Download libraries**

Run:
```bash
cd /Users/egecan/Code/mahpastes/frontend
mkdir -p vendor
curl -L "https://cdn.jsdelivr.net/npm/marked@15/marked.min.js" -o vendor/marked.min.js
curl -L "https://cdn.jsdelivr.net/npm/dompurify@3/dist/purify.min.js" -o vendor/purify.min.js
```

**Step 2: Add script tags to index.html**

Modify: `frontend/index.html:921-935` — Add vendor scripts before the app scripts:

```html
    <!-- Vendor libraries for plugin result rendering -->
    <script src="vendor/marked.min.js"></script>
    <script src="vendor/purify.min.js"></script>

    <!-- Scripts split for better organization -->
    <script src="js/utils.js"></script>
```

**Step 3: Verify scripts load**

Run: `cd /Users/egecan/Code/mahpastes && make dev`
Open the app, open browser console, verify `marked` and `DOMPurify` are defined globals. Stop dev server.

**Step 4: Commit**

```bash
git add frontend/vendor/marked.min.js frontend/vendor/purify.min.js frontend/index.html
git commit -m "vendor: add marked.js and DOMPurify for plugin modal rendering"
```

---

### Task 2: Add ModalData to Go structs and extend return value parsing

**Files:**
- Modify: `plugin/manager.go:18-23` — Add `ModalData` fields to `ActionResult`
- Modify: `plugin/sandbox.go:276-296` — Parse nested `modal` table from return value

**Step 1: Add ModalData struct and extend ActionResult**

In `plugin/manager.go`, add `ModalData` struct and extend `ActionResult`:

```go
// ModalData represents content to display in a plugin result modal
type ModalData struct {
	Title            string `json:"title"`
	Content          string `json:"content"`
	Format           string `json:"format"`
	CopyData         string `json:"copy_data,omitempty"`
	PasteData        string `json:"paste_data,omitempty"`
	PasteName        string `json:"paste_name,omitempty"`
	PasteContentType string `json:"paste_content_type,omitempty"`
}

// ActionResult represents the result of a plugin action execution
type ActionResult struct {
	Success      bool       `json:"success"`
	Error        string     `json:"error,omitempty"`
	ResultClipID int64      `json:"result_clip_id,omitempty"`
	Modal        *ModalData `json:"modal,omitempty"`
}
```

**Step 2: Extend CallUIAction to handle nested modal table**

In `plugin/sandbox.go`, the `CallUIAction` method at line 280-293 currently only handles flat string/number/bool values. Modify it to also parse a nested `modal` table:

After the existing `ForEach` block that populates `result`, add:

```go
// Check for nested modal table
if modalVal := tbl.RawGetString("modal"); modalVal != nil {
    if modalTbl, ok := modalVal.(*lua.LTable); ok {
        modalData := make(map[string]interface{})
        modalTbl.ForEach(func(k, v lua.LValue) {
            if key, ok := k.(lua.LString); ok {
                if str, ok := v.(lua.LString); ok {
                    modalData[string(key)] = string(str)
                }
            }
        })
        result["modal"] = modalData
    }
}
```

**Step 3: Extend luaResultToActionResult to populate Modal field**

In `plugin/manager.go` at `luaResultToActionResult` (line 499-521), add modal parsing after the existing field checks:

```go
if modalRaw, ok := luaResult["modal"]; ok {
    if modalMap, ok := modalRaw.(map[string]interface{}); ok {
        modal := &ModalData{}
        if v, ok := modalMap["title"].(string); ok {
            modal.Title = v
        }
        if v, ok := modalMap["content"].(string); ok {
            modal.Content = v
        }
        if v, ok := modalMap["format"].(string); ok {
            modal.Format = v
        }
        if v, ok := modalMap["copy_data"].(string); ok {
            modal.CopyData = v
        }
        if v, ok := modalMap["paste_data"].(string); ok {
            modal.PasteData = v
        }
        if v, ok := modalMap["paste_name"].(string); ok {
            modal.PasteName = v
        }
        if v, ok := modalMap["paste_content_type"].(string); ok {
            modal.PasteContentType = v
        }
        // Validate required fields and limits
        if modal.Title != "" && modal.Content != "" && modal.Format != "" {
            if len(modal.Title) > 200 {
                modal.Title = modal.Title[:200]
            }
            if len(modal.Content) > 1<<20 { // 1MB
                modal.Content = modal.Content[:1<<20]
            }
            if len(modal.CopyData) > 1<<20 {
                modal.CopyData = modal.CopyData[:1<<20]
            }
            if len(modal.PasteData) > 10<<20 { // 10MB
                modal.PasteData = modal.PasteData[:10<<20]
            }
            validFormats := map[string]bool{"markdown": true, "image": true, "text": true}
            if validFormats[modal.Format] {
                result.Modal = modal
            }
        }
    }
}
```

**Step 4: Handle modal in async actions too**

In `plugin/manager.go` at the `ExecuteUIAction` method (line 475-486), the async branch currently ignores the return value (`_ = luaResult`). For async actions that return a modal, emit a Wails event instead:

```go
go func() {
    luaResult, err := p.Sandbox.CallUIAction(actionID, clipIDs, options, MaxUIActionTime)
    if err != nil {
        log.Printf("Plugin %s async action %s failed: %v", p.Name, actionID, err)
        m.incrementErrorCount(pluginID)
        return
    }
    m.resetErrorCount(pluginID)
    // Check if async action returned modal data
    actionResult := luaResultToActionResult(luaResult)
    if actionResult.Modal != nil {
        runtime.EventsEmit(m.ctx, "plugin:modal", map[string]interface{}{
            "plugin_id":          p.ID,
            "title":              actionResult.Modal.Title,
            "content":            actionResult.Modal.Content,
            "format":             actionResult.Modal.Format,
            "copy_data":          actionResult.Modal.CopyData,
            "paste_data":         actionResult.Modal.PasteData,
            "paste_name":         actionResult.Modal.PasteName,
            "paste_content_type": actionResult.Modal.PasteContentType,
        })
    }
}()
```

This requires adding the `runtime` import to `manager.go`:
```go
import "github.com/wailsapp/wails/v2/pkg/runtime"
```

**Step 5: Regenerate bindings**

Run: `cd /Users/egecan/Code/mahpastes && make bindings`

**Step 6: Commit**

```bash
git add plugin/manager.go plugin/sandbox.go frontend/wailsjs/
git commit -m "feat(plugin): extend ActionResult with ModalData for plugin result modals"
```

---

### Task 3: Create modal.show() Lua API

**Files:**
- Create: `plugin/api_modal.go`
- Modify: `plugin/manager.go:119-148` — Register the API when loading plugins

**Step 1: Create api_modal.go**

Create `plugin/api_modal.go` following the pattern of `api_toast.go`:

```go
package plugin

import (
	"context"
	"sync"

	lua "github.com/yuin/gopher-lua"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	maxModalTitleLength   = 200
	maxModalContentLength = 1 << 20  // 1MB
	maxModalCopyLength    = 1 << 20  // 1MB
	maxModalPasteLength   = 10 << 20 // 10MB
)

// ModalAPI provides modal display functionality for plugins
type ModalAPI struct {
	ctx      context.Context
	pluginID int64
	mu       sync.Mutex
	showing  bool
}

// NewModalAPI creates a new modal API instance
func NewModalAPI(ctx context.Context, pluginID int64) *ModalAPI {
	return &ModalAPI{
		ctx:      ctx,
		pluginID: pluginID,
	}
}

// Register adds the modal module to the Lua state
func (m *ModalAPI) Register(L *lua.LState) {
	modalMod := L.NewTable()
	modalMod.RawSetString("show", L.NewFunction(m.show))
	L.SetGlobal("modal", modalMod)
}

// ClearShowing resets the showing state (called when modal is closed)
func (m *ModalAPI) ClearShowing() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.showing = false
}

func (m *ModalAPI) show(L *lua.LState) int {
	opts := L.CheckTable(1)

	// Extract fields
	title := ""
	content := ""
	format := ""
	copyData := ""
	pasteData := ""
	pasteName := ""
	pasteContentType := ""

	if v := opts.RawGetString("title"); v != lua.LNil {
		title = v.String()
	}
	if v := opts.RawGetString("content"); v != lua.LNil {
		content = v.String()
	}
	if v := opts.RawGetString("format"); v != lua.LNil {
		format = v.String()
	}
	if v := opts.RawGetString("copy_data"); v != lua.LNil {
		copyData = v.String()
	}
	if v := opts.RawGetString("paste_data"); v != lua.LNil {
		pasteData = v.String()
	}
	if v := opts.RawGetString("paste_name"); v != lua.LNil {
		pasteName = v.String()
	}
	if v := opts.RawGetString("paste_content_type"); v != lua.LNil {
		pasteContentType = v.String()
	}

	// Validate required fields
	if title == "" || content == "" || format == "" {
		L.Push(lua.LFalse)
		L.Push(lua.LString("title, content, and format are required"))
		return 2
	}

	// Validate format
	validFormats := map[string]bool{"markdown": true, "image": true, "text": true}
	if !validFormats[format] {
		L.Push(lua.LFalse)
		L.Push(lua.LString("format must be 'markdown', 'image', or 'text'"))
		return 2
	}

	// Enforce size limits
	if len(title) > maxModalTitleLength {
		title = title[:maxModalTitleLength]
	}
	if len(content) > maxModalContentLength {
		L.Push(lua.LFalse)
		L.Push(lua.LString("content exceeds 1MB limit"))
		return 2
	}
	if len(copyData) > maxModalCopyLength {
		L.Push(lua.LFalse)
		L.Push(lua.LString("copy_data exceeds 1MB limit"))
		return 2
	}
	if len(pasteData) > maxModalPasteLength {
		L.Push(lua.LFalse)
		L.Push(lua.LString("paste_data exceeds 10MB limit"))
		return 2
	}

	// Rate limit: 1 modal at a time
	m.mu.Lock()
	if m.showing {
		m.mu.Unlock()
		L.Push(lua.LFalse)
		L.Push(lua.LString("a modal is already showing"))
		return 2
	}
	m.showing = true
	m.mu.Unlock()

	// Emit Wails event
	runtime.EventsEmit(m.ctx, "plugin:modal", map[string]interface{}{
		"plugin_id":          m.pluginID,
		"title":              title,
		"content":            content,
		"format":             format,
		"copy_data":          copyData,
		"paste_data":         pasteData,
		"paste_name":         pasteName,
		"paste_content_type": pasteContentType,
	})

	L.Push(lua.LTrue)
	return 1
}
```

**Step 2: Register ModalAPI in manager.go loadPlugin**

In `plugin/manager.go` at `loadPlugin` (around line 140, after taskAPI registration):

```go
modalAPI := NewModalAPI(m.ctx, p.ID)
modalAPI.Register(sandbox.GetState())
```

**Step 3: Commit**

```bash
git add plugin/api_modal.go plugin/manager.go
git commit -m "feat(plugin): add modal.show() Lua API for displaying rich content"
```

---

### Task 4: Add plugin result modal HTML to index.html

**Files:**
- Modify: `frontend/index.html` — Add `#plugin-result-modal` HTML before the script tags

**Step 1: Add modal HTML**

In `frontend/index.html`, add the modal HTML after the `plugin-options-modal` div (around line 919) and before the vendor script tags:

```html
    <!-- Plugin Result Modal -->
    <div id="plugin-result-modal" class="modal-backdrop opacity-0 pointer-events-none" role="dialog" aria-modal="true" aria-labelledby="plugin-result-title">
        <div class="modal-content max-w-2xl">
            <div class="modal-header">
                <h3 id="plugin-result-title" class="text-sm font-semibold text-stone-800">Result</h3>
                <button id="plugin-result-close" class="p-1 hover:bg-stone-100 rounded transition-colors" aria-label="Close">
                    <svg class="w-4 h-4 text-stone-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M6 18L18 6M6 6l12 12"/>
                    </svg>
                </button>
            </div>
            <div id="plugin-result-body" class="modal-body overflow-y-auto" style="max-height: 60vh;">
                <!-- Content rendered by JS -->
            </div>
            <div class="modal-footer">
                <button id="plugin-result-copy" class="btn-secondary flex items-center gap-1.5">
                    <svg class="w-3.5 h-3.5 opacity-60" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
                            d="M8 7v8a2 2 0 002 2h6M8 7V5a2 2 0 012-2h4.586a1 1 0 01.707.293l4.414 4.414a1 1 0 01.293.707V15a2 2 0 01-2 2h-2M8 7H6a2 2 0 00-2 2v10a2 2 0 002 2h8a2 2 0 002-2v-2"/>
                    </svg>
                    Copy to Clipboard
                </button>
                <button id="plugin-result-paste" class="btn-primary flex items-center gap-1.5">
                    <svg class="w-3.5 h-3.5 opacity-70" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
                            d="M12 4v16m8-8H4"/>
                    </svg>
                    Add as Paste
                </button>
            </div>
        </div>
    </div>
```

**Step 2: Commit**

```bash
git add frontend/index.html
git commit -m "feat(ui): add plugin result modal HTML structure"
```

---

### Task 5: Add markdown rendering styles to modals.css

**Files:**
- Modify: `frontend/css/modals.css` — Add `.plugin-md-content` scoped styles at the end

**Step 1: Add markdown content styles**

Append to `frontend/css/modals.css`:

```css
/* Plugin Result Modal - Markdown Content Styles */
.plugin-md-content {
    font-size: 0.8125rem;
    line-height: 1.6;
    color: #44403c;
}

.plugin-md-content h1 {
    font-size: 1.125rem;
    font-weight: 600;
    color: #1c1917;
    margin-top: 1.5rem;
    margin-bottom: 0.75rem;
    padding-bottom: 0.5rem;
    border-bottom: 1px solid #e7e5e4;
}

.plugin-md-content h1:first-child {
    margin-top: 0;
}

.plugin-md-content h2 {
    font-size: 0.9375rem;
    font-weight: 600;
    color: #292524;
    margin-top: 1.25rem;
    margin-bottom: 0.5rem;
}

.plugin-md-content h3 {
    font-size: 0.8125rem;
    font-weight: 600;
    color: #292524;
    margin-top: 1rem;
    margin-bottom: 0.375rem;
}

.plugin-md-content h4,
.plugin-md-content h5,
.plugin-md-content h6 {
    font-size: 0.75rem;
    font-weight: 600;
    color: #44403c;
    margin-top: 0.75rem;
    margin-bottom: 0.25rem;
    text-transform: uppercase;
    letter-spacing: 0.025em;
}

.plugin-md-content p {
    margin-bottom: 0.75rem;
}

.plugin-md-content p:last-child {
    margin-bottom: 0;
}

.plugin-md-content strong {
    font-weight: 600;
    color: #292524;
}

.plugin-md-content em {
    font-style: italic;
}

.plugin-md-content del {
    text-decoration: line-through;
    color: #a8a29e;
}

.plugin-md-content a {
    color: #57534e;
    text-decoration: underline;
    text-underline-offset: 2px;
}

.plugin-md-content a:hover {
    color: #292524;
}

.plugin-md-content code {
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.75rem;
    background: #f5f5f4;
    border: 1px solid #e7e5e4;
    border-radius: 0.25rem;
    padding: 0.125rem 0.375rem;
}

.plugin-md-content pre {
    background: #fafaf9;
    border: 1px solid #e7e5e4;
    border-radius: 0.375rem;
    padding: 0.75rem 1rem;
    overflow-x: auto;
    margin-bottom: 0.75rem;
}

.plugin-md-content pre code {
    background: none;
    border: none;
    padding: 0;
    font-size: 0.6875rem;
    line-height: 1.5;
}

.plugin-md-content blockquote {
    border-left: 3px solid #d6d3d1;
    padding-left: 0.75rem;
    margin: 0.75rem 0;
    color: #78716c;
    font-style: italic;
}

.plugin-md-content ul,
.plugin-md-content ol {
    padding-left: 1.25rem;
    margin-bottom: 0.75rem;
}

.plugin-md-content li {
    margin-bottom: 0.25rem;
}

.plugin-md-content ul li {
    list-style-type: disc;
}

.plugin-md-content ol li {
    list-style-type: decimal;
}

.plugin-md-content hr {
    border: none;
    border-top: 1px solid #e7e5e4;
    margin: 1rem 0;
}

.plugin-md-content img {
    max-width: 100%;
    height: auto;
    border-radius: 0.375rem;
    margin: 0.5rem 0;
}

/* Table styles */
.plugin-md-content table {
    width: 100%;
    border-collapse: collapse;
    margin-bottom: 0.75rem;
    font-size: 0.75rem;
}

.plugin-md-content thead th {
    background: #fafaf9;
    font-weight: 600;
    text-align: left;
    padding: 0.5rem 0.75rem;
    border: 1px solid #e7e5e4;
    color: #292524;
    text-transform: uppercase;
    letter-spacing: 0.025em;
    font-size: 0.6875rem;
}

.plugin-md-content tbody td {
    padding: 0.375rem 0.75rem;
    border: 1px solid #e7e5e4;
    color: #44403c;
}

.plugin-md-content tbody tr:nth-child(even) {
    background: #fafaf9;
}

/* Plain text format in result modal */
.plugin-text-content {
    font-family: 'IBM Plex Mono', monospace;
    font-size: 0.6875rem;
    line-height: 1.4;
    color: #44403c;
    white-space: pre;
    overflow-x: auto;
    background: #fafaf9;
    border: 1px solid #e7e5e4;
    border-radius: 0.375rem;
    padding: 0.75rem 1rem;
}

/* Image format in result modal */
.plugin-image-content {
    display: flex;
    align-items: center;
    justify-content: center;
}

.plugin-image-content img {
    max-width: 100%;
    max-height: 50vh;
    object-fit: contain;
    border-radius: 0.375rem;
}
```

**Step 2: Rebuild Tailwind**

Run: `cd /Users/egecan/Code/mahpastes && npx tailwindcss -i frontend/css/main.css -o frontend/dist/output.css`
(Or whatever the Tailwind build command is — check the Makefile.)

**Step 3: Commit**

```bash
git add frontend/css/modals.css
git commit -m "feat(ui): add scoped markdown and content styles for plugin result modal"
```

---

### Task 6: Create frontend/js/modal-renderer.js

**Files:**
- Create: `frontend/js/modal-renderer.js`
- Modify: `frontend/index.html` — Add script tag

**Step 1: Create modal-renderer.js**

```js
// --- Plugin Result Modal Renderer ---

// DOMPurify config
const PURIFY_CONFIG = {
    ALLOWED_TAGS: ['h1','h2','h3','h4','h5','h6','p','br','hr',
                   'strong','em','del','code','pre','blockquote',
                   'ul','ol','li','a','img','table','thead','tbody',
                   'tr','th','td','span'],
    ALLOWED_ATTR: ['href','src','alt','title','class'],
    ALLOW_DATA_ATTR: false,
};

// Configure DOMPurify hooks for link safety
if (typeof DOMPurify !== 'undefined') {
    DOMPurify.addHook('afterSanitizeAttributes', function(node) {
        // Force links to open in system browser
        if (node.tagName === 'A') {
            node.setAttribute('target', '_blank');
            node.setAttribute('rel', 'noopener noreferrer');
        }
        // Only allow https: and data: on img src
        if (node.tagName === 'IMG') {
            const src = node.getAttribute('src') || '';
            if (!src.startsWith('https://') && !src.startsWith('data:')) {
                node.removeAttribute('src');
            }
        }
    });
}

// Configure marked for GFM with tables
if (typeof marked !== 'undefined') {
    marked.setOptions({
        gfm: true,
        breaks: false,
    });
}

// State
let currentModalData = null;

// Elements
const pluginResultModal = document.getElementById('plugin-result-modal');
const pluginResultTitle = document.getElementById('plugin-result-title');
const pluginResultBody = document.getElementById('plugin-result-body');
const pluginResultClose = document.getElementById('plugin-result-close');
const pluginResultCopy = document.getElementById('plugin-result-copy');
const pluginResultPaste = document.getElementById('plugin-result-paste');

function renderPluginContent(content, format) {
    switch (format) {
        case 'markdown': {
            const rawHTML = marked.parse(content);
            const cleanHTML = DOMPurify.sanitize(rawHTML, PURIFY_CONFIG);
            const container = document.createElement('div');
            container.className = 'plugin-md-content';
            container.innerHTML = cleanHTML;
            return container;
        }
        case 'image': {
            const container = document.createElement('div');
            container.className = 'plugin-image-content';
            const img = document.createElement('img');
            img.src = content;
            img.alt = 'Plugin result';
            container.appendChild(img);
            return container;
        }
        case 'text': {
            const pre = document.createElement('pre');
            pre.className = 'plugin-text-content';
            pre.textContent = content;
            return pre;
        }
        default:
            return null;
    }
}

function showPluginResultModal(data) {
    if (!data || !data.content || !data.format) return;

    currentModalData = data;

    // Set title
    pluginResultTitle.textContent = data.title || 'Result';

    // Render content
    pluginResultBody.innerHTML = '';
    const rendered = renderPluginContent(data.content, data.format);
    if (rendered) {
        pluginResultBody.appendChild(rendered);
    }

    // Show modal
    pluginResultModal.classList.remove('opacity-0', 'pointer-events-none');
    pluginResultModal.classList.add('opacity-100');
}

function closePluginResultModal() {
    pluginResultModal.classList.remove('opacity-100');
    pluginResultModal.classList.add('opacity-0', 'pointer-events-none');
    currentModalData = null;
}

// --- Copy to Clipboard ---
async function pluginResultCopyToClipboard() {
    if (!currentModalData) return;

    const text = currentModalData.copy_data || currentModalData.content;
    try {
        await window.go.main.App.CopyToClipboard(text);
        showToast('Copied to clipboard');
    } catch (err) {
        console.error('Failed to copy to clipboard:', err);
        showToast('Failed to copy', 'error');
    }
}

// --- Add as Paste ---
async function pluginResultAddAsPaste() {
    if (!currentModalData) return;

    const data = currentModalData.paste_data || currentModalData.content;
    const format = currentModalData.format;

    // Determine defaults based on format
    let name = currentModalData.paste_name;
    let contentType = currentModalData.paste_content_type;

    if (!name) {
        switch (format) {
            case 'markdown': name = 'plugin-result.md'; break;
            case 'image': name = 'plugin-result.png'; break;
            case 'text': name = 'plugin-result.txt'; break;
            default: name = 'plugin-result.txt';
        }
    }

    if (!contentType) {
        switch (format) {
            case 'markdown': contentType = 'text/markdown'; break;
            case 'image': contentType = 'image/png'; break;
            case 'text': contentType = 'text/plain'; break;
            default: contentType = 'text/plain';
        }
    }

    try {
        // Base64 encode the data for UploadFileAndGetID
        const encoded = btoa(unescape(encodeURIComponent(data)));
        await window.go.main.App.UploadFileAndGetID({
            Name: name,
            ContentType: contentType,
            Data: encoded,
        });
        showToast('Added as paste');
        closePluginResultModal();
        if (typeof loadClips === 'function') {
            loadClips();
        }
    } catch (err) {
        console.error('Failed to add as paste:', err);
        showToast('Failed to add paste', 'error');
    }
}

// --- Event Listeners ---
pluginResultClose.addEventListener('click', closePluginResultModal);
pluginResultCopy.addEventListener('click', pluginResultCopyToClipboard);
pluginResultPaste.addEventListener('click', pluginResultAddAsPaste);

// Close on backdrop click
pluginResultModal.addEventListener('click', (e) => {
    if (e.target === pluginResultModal) closePluginResultModal();
});

// Listen for plugin:modal Wails event (from modal.show() API)
if (typeof window.runtime !== 'undefined') {
    window.runtime.EventsOn('plugin:modal', (data) => {
        showPluginResultModal(data);
    });
}
```

**Step 2: Add script tag to index.html**

In `frontend/index.html`, add the script after `plugins.js` and before `task-queue.js`:

```html
    <script src="js/plugins.js"></script>
    <script src="js/modal-renderer.js"></script>
    <script src="js/task-queue.js"></script>
```

**Step 3: Commit**

```bash
git add frontend/js/modal-renderer.js frontend/index.html
git commit -m "feat(ui): add modal-renderer.js for plugin result display"
```

---

### Task 7: Wire executePluginAction to show modal

**Files:**
- Modify: `frontend/js/plugins.js:559-580` — Check for modal in result

**Step 1: Update executePluginAction**

In `frontend/js/plugins.js`, modify the `executePluginAction` function (line 559-580) to check for `result.modal`:

Replace the success handling block:
```js
async function executePluginAction(pluginId, actionId, clipIds, options, isAsync) {
    try {
        const result = await window.go.main.PluginService.ExecutePluginAction(pluginId, actionId, clipIds, options || {});
        if (result && result.success) {
            // Check if result includes modal data
            if (result.modal) {
                showPluginResultModal(result.modal);
            } else if (isAsync) {
                showToast('Processing started...');
            } else {
                showToast('Action completed');
                if (typeof loadClips === 'function') {
                    loadClips();
                }
            }
        } else if (result && result.error) {
            showToast(result.error, 'error');
        }
        return result;
    } catch (error) {
        console.error('Failed to execute plugin action:', error);
        showToast('Action failed: ' + (error.message || 'Unknown error'), 'error');
        return { success: false, error: error.message };
    }
}
```

**Step 2: Add Escape key handling for plugin result modal**

In `frontend/js/plugins.js`, update the Escape key handler (line 540-554) to also check for the plugin result modal:

```js
document.addEventListener('keydown', (e) => {
    if (e.key !== 'Escape') return;
    e.preventDefault();

    // Plugin result modal takes highest priority
    if (pluginResultModal && !pluginResultModal.classList.contains('opacity-0')) {
        closePluginResultModal();
        return;
    }

    // Options dialog takes priority over plugins modal
    const optionsModal = document.getElementById('plugin-options-modal');
    if (optionsModal && !optionsModal.classList.contains('opacity-0')) {
        closePluginOptionsDialog();
        return;
    }

    if (!pluginsModal.classList.contains('opacity-0')) {
        closePlugins();
    }
});
```

**Step 3: Commit**

```bash
git add frontend/js/plugins.js
git commit -m "feat(ui): wire executePluginAction to show modal on result.modal"
```

---

### Task 8: Update EXIF Viewer plugin to use modal

**Files:**
- Modify: `plugins/exif-viewer.lua`

**Step 1: Rewrite format_metadata to produce markdown**

Replace the `format_metadata` function with a markdown version, and rewrite `on_ui_action` to return a modal instead of creating a clip:

```lua
-- Format EXIF metadata into markdown
local function format_metadata_markdown(info, meta, clip_info)
    local lines = {}
    table.insert(lines, "## File Info")
    table.insert(lines, "")
    table.insert(lines, "| Property | Value |")
    table.insert(lines, "|---|---|")

    if clip_info and clip_info.filename then
        table.insert(lines, "| File | " .. clip_info.filename .. " |")
    end
    if info then
        if info.width and info.height then
            table.insert(lines, "| Dimensions | " .. info.width .. " x " .. info.height .. " |")
        end
        if info.format then
            table.insert(lines, "| Format | " .. info.format .. " |")
        end
        if info.size then
            local size_kb = math.floor(info.size / 1024)
            local size_str = size_kb > 1024
                and string.format("%.1f MB", size_kb / 1024)
                or (size_kb .. " KB")
            table.insert(lines, "| Size | " .. size_str .. " |")
        end
    end

    if meta then
        local has_camera = (meta.camera_make and meta.camera_make ~= "") or
                          (meta.camera_model and meta.camera_model ~= "")
        if has_camera then
            table.insert(lines, "")
            table.insert(lines, "## Camera")
            table.insert(lines, "")
            table.insert(lines, "| Property | Value |")
            table.insert(lines, "|---|---|")
            if meta.camera_make and meta.camera_make ~= "" then
                table.insert(lines, "| Make | " .. meta.camera_make .. " |")
            end
            if meta.camera_model and meta.camera_model ~= "" then
                table.insert(lines, "| Model | " .. meta.camera_model .. " |")
            end
            if meta.lens and meta.lens ~= "" then
                table.insert(lines, "| Lens | " .. meta.lens .. " |")
            end
        end

        local has_settings = meta.iso or meta.aperture or meta.shutter_speed or meta.focal_length
        if has_settings then
            table.insert(lines, "")
            table.insert(lines, "## Settings")
            table.insert(lines, "")
            table.insert(lines, "| Property | Value |")
            table.insert(lines, "|---|---|")
            if meta.iso then
                table.insert(lines, "| ISO | " .. tostring(meta.iso) .. " |")
            end
            if meta.aperture then
                table.insert(lines, "| Aperture | " .. tostring(meta.aperture) .. " |")
            end
            if meta.shutter_speed then
                table.insert(lines, "| Shutter | " .. tostring(meta.shutter_speed) .. " |")
            end
            if meta.focal_length then
                table.insert(lines, "| Focal Length | " .. tostring(meta.focal_length) .. " |")
            end
        end

        if meta.date and meta.date ~= "" then
            table.insert(lines, "")
            table.insert(lines, "## Date")
            table.insert(lines, "")
            table.insert(lines, "**Taken:** " .. meta.date)
        end

        if meta.gps and type(meta.gps) == "table" then
            table.insert(lines, "")
            table.insert(lines, "## Location")
            table.insert(lines, "")
            table.insert(lines, "| Coordinate | Value |")
            table.insert(lines, "|---|---|")
            table.insert(lines, "| Latitude | " .. tostring(meta.gps.latitude) .. " |")
            table.insert(lines, "| Longitude | " .. tostring(meta.gps.longitude) .. " |")
        end

        if not has_camera and not has_settings and not (meta.date and meta.date ~= "") then
            table.insert(lines, "")
            table.insert(lines, "*No EXIF data found.*")
        end
    else
        table.insert(lines, "")
        table.insert(lines, "*No EXIF data found.*")
    end

    return table.concat(lines, "\n")
end
```

Also keep the old `format_metadata` function (rename it to `format_metadata_plain`) for the `copy_data`/`paste_data` fields.

Update `on_ui_action` — for a single clip, return a modal. For multiple clips, keep the current batch behavior (create clips):

```lua
function on_ui_action(action_id, clip_ids, options)
    if action_id ~= "view_exif" then
        return {success = false, error = "Unknown action: " .. action_id}
    end

    -- Single clip: show in modal
    if #clip_ids == 1 then
        local clip_id = clip_ids[1]
        local clip_info = clips.get(clip_id)
        local info = image.info(clip_id)
        local meta = image.metadata(clip_id)

        local md = format_metadata_markdown(info, meta, clip_info)
        local plain = format_metadata_plain(info, meta, clip_info)

        local original_name = (clip_info and clip_info.filename) or ("clip_" .. clip_id)
        local name = original_name:match("^(.+)%.[^%.]+$") or original_name

        return {
            success = true,
            modal = {
                title = "Image Metadata",
                content = md,
                format = "markdown",
                copy_data = plain,
                paste_data = plain,
                paste_name = name .. "_exif.txt",
                paste_content_type = "text/plain",
            }
        }
    end

    -- Multiple clips: batch create (keep existing behavior)
    -- ... (keep existing batch loop code)
end
```

Note: The `async = true` on the action manifest should be removed for the single-clip case, or the whole action can become synchronous since showing a modal is fast. Change the manifest to `async = false` (remove `async = true`):

```lua
ui = {
    lightbox_buttons = {
        {id = "view_exif", label = "View EXIF", icon = "info", file_types = {"image/*"}},
    },
    card_actions = {
        {id = "view_exif", label = "View EXIF", icon = "info", file_types = {"image/*"}},
    },
},
```

**Step 2: Commit**

```bash
git add plugins/exif-viewer.lua
git commit -m "feat(plugin): update EXIF viewer to show results in modal"
```

---

### Task 9: Update ASCII Art plugin to use modal

**Files:**
- Modify: `plugins/ascii-art.lua`

**Step 1: Update on_ui_action to return modal for single clip**

For a single clip, return a modal with `format = "text"`. For batch, keep current behavior.

Change manifest to remove `async = true` (ASCII art from grayscale_pixels is fast):

```lua
ui = {
    lightbox_buttons = {
        {id = "to_ascii", label = "To ASCII", icon = "code", file_types = {"image/*"},
            options = {
                {id = "width", type = "range", label = "Width (chars)", default = 80, min = 40, max = 120, step = 1},
            }
        },
    },
    card_actions = {
        {id = "to_ascii", label = "To ASCII", icon = "code", file_types = {"image/*"},
            options = {
                {id = "width", type = "range", label = "Width (chars)", default = 80, min = 40, max = 120, step = 1},
            }
        },
    },
},
```

Update `on_ui_action`:

```lua
function on_ui_action(action_id, clip_ids, options)
    if action_id ~= "to_ascii" then
        return {success = false, error = "Unknown action: " .. action_id}
    end

    options = options or {}
    local width = options.width or 80
    local height = math.floor(width / 2)

    -- Single clip: show in modal
    if #clip_ids == 1 then
        local clip_id = clip_ids[1]
        local pixels = image.grayscale_pixels(clip_id, width, height)
        if not pixels then
            return {success = false, error = "Failed to get grayscale pixels"}
        end

        local lines = {}
        for y = 0, height - 1 do
            local row = ""
            for x = 0, width - 1 do
                local lum = pixels[y * width + x + 1] or 0
                local inverted = 255 - lum
                local idx = math.floor(inverted / 256 * #charset) + 1
                if idx > #charset then idx = #charset end
                if idx < 1 then idx = 1 end
                row = row .. charset:sub(idx, idx)
            end
            lines[#lines + 1] = row
        end
        local art = table.concat(lines, "\n")

        local clip_info = clips.get(clip_id)
        local name = "ascii_art.txt"
        if clip_info and clip_info.filename then
            local base = clip_info.filename:match("^(.+)%.[^%.]+$") or clip_info.filename
            name = "ascii_" .. base .. ".txt"
        end

        return {
            success = true,
            modal = {
                title = "ASCII Art (" .. width .. "x" .. height .. ")",
                content = art,
                format = "text",
                copy_data = art,
                paste_data = art,
                paste_name = name,
                paste_content_type = "text/plain",
            }
        }
    end

    -- Multiple clips: batch create (keep existing behavior)
    -- ... (keep existing batch loop code)
end
```

**Step 2: Commit**

```bash
git add plugins/ascii-art.lua
git commit -m "feat(plugin): update ASCII art to show results in modal"
```

---

### Task 10: Update Palette Extractor plugin to use modal

**Files:**
- Modify: `plugins/palette-extractor.lua`

**Step 1: Update on_ui_action to return modal for single clip**

For a single clip, show markdown with embedded SVG data URI plus hex color table:

```lua
-- Single clip: show in modal
if #clip_ids == 1 then
    local clip_id = clip_ids[1]
    local colors = image.dominant_colors(clip_id, count)
    if not colors or #colors == 0 then
        return {success = false, error = "Failed to extract colors"}
    end

    local svg = build_swatch_svg(colors)
    local svg_b64 = base64.encode(svg)

    -- Build markdown with embedded SVG and color table
    local lines = {}
    table.insert(lines, "![Palette](data:image/svg+xml;base64," .. svg_b64 .. ")")
    table.insert(lines, "")
    table.insert(lines, "| # | Color |")
    table.insert(lines, "|---|---|")
    for i, color in ipairs(colors) do
        table.insert(lines, "| " .. i .. " | `" .. color .. "` |")
    end
    local md = table.concat(lines, "\n")

    -- Hex codes for clipboard
    local hex_list = table.concat(colors, "\n")

    -- Get name for paste
    local clip_info = clips.get(clip_id)
    local original_name = (clip_info and clip_info.filename) or ("clip_" .. clip_id)
    local name = original_name:match("^(.+)%.[^%.]+$") or original_name

    -- Optionally tag
    if tag_colors then
        for _, color in ipairs(colors) do
            local tag_id = find_or_create_tag(color)
            if tag_id then
                tags.add_to_clip(tag_id, clip_id)
            end
        end
    end

    return {
        success = true,
        modal = {
            title = "Color Palette (" .. #colors .. " colors)",
            content = md,
            format = "markdown",
            copy_data = hex_list,
            paste_data = base64.encode(svg),
            paste_name = name .. "_palette.svg",
            paste_content_type = "image/svg+xml",
        }
    }
end
```

Note: `paste_data` for the SVG is base64-encoded because `UploadFileAndGetID` expects base64. The modal-renderer's "Add as paste" path should handle this. Actually — we need to reconsider: the paste_data is already base64 from the Lua side, but the frontend `btoa(encodeURIComponent(...))` will double-encode it. We need to add a flag or detect this case.

**Better approach**: Add an optional `paste_data_base64` boolean field to `ModalData`. If true, `paste_data` is already base64-encoded and should be passed directly to `UploadFileAndGetID` without re-encoding. Add this to the Go struct, Lua parsing, and frontend logic.

Actually, simpler: the `paste_data` field should always contain raw data. The frontend always base64-encodes it before sending to Go. For binary data (like SVG with the base64.encode), the plugin should pass the raw SVG string, not the base64-encoded version. The frontend will base64-encode it.

So `paste_data = svg` (raw SVG string), not `paste_data = base64.encode(svg)`.

Remove `async = true` from manifest (same as other plugins).

**Step 2: Commit**

```bash
git add plugins/palette-extractor.lua
git commit -m "feat(plugin): update palette extractor to show results in modal"
```

---

### Task 11: Run e2e tests and fix issues

**Files:**
- Various test and source files as needed

**Step 1: Run full test suite**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test`

**Step 2: Fix any failures**

Address test failures. Common issues to watch for:
- Existing plugin tests may expect clips to be created instead of modals being shown
- The `executePluginAction` return shape changed (now includes `modal`)
- Tests that check clip counts after plugin actions may need updating

**Step 3: Commit fixes**

```bash
git add -A
git commit -m "fix: update tests for plugin result modal changes"
```

---

### Task 12: Add e2e tests for plugin result modal

**Files:**
- Create: `e2e/tests/plugins/result-modal.spec.ts`

**Step 1: Write tests**

Test cases:
1. Plugin action with modal return shows modal (not clip)
2. Modal displays rendered markdown content
3. "Copy to clipboard" copies `copy_data`
4. "Add as paste" creates a clip with correct name/type
5. Modal closes on Escape, backdrop click, close button
6. `plugin:modal` Wails event opens modal

Create a test plugin `e2e/test-plugins/modal-test.lua` that returns modal data from `on_ui_action`.

**Step 2: Run tests**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test -- --grep "result-modal"`

**Step 3: Fix any failures and commit**

```bash
git add e2e/tests/plugins/result-modal.spec.ts e2e/test-plugins/modal-test.lua
git commit -m "test: add e2e tests for plugin result modal"
```

---

### Task 13: Final verification

**Step 1: Run full test suite**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test`

Expected: All tests pass.

**Step 2: Manual smoke test**

Run: `cd /Users/egecan/Code/mahpastes && make dev`
- Upload an image
- Open lightbox, click EXIF Viewer — should show modal with markdown table
- Click "Copy to clipboard" — should copy plain text metadata
- Click "Add as paste" — should create text clip
- Try ASCII Art — should show modal with preformatted text
- Try Palette Extractor — should show modal with SVG and color table

**Step 3: Commit any final fixes**

```bash
git add -A
git commit -m "fix: final polish for plugin result modals"
```
