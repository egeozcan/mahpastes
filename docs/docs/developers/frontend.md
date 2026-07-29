---
sidebar_position: 2
---

# Frontend Architecture

The frontend is a single-page Wails webview app built with vanilla JavaScript, Tailwind CSS, and HTML. JavaScript is loaded as classic browser scripts in a fixed order from `index.html`; the only build step is Tailwind CSS compilation.

## File Structure

```
frontend/
├── index.html           Main HTML structure
├── package.json         Frontend dependencies
├── tailwind.config.js   Tailwind configuration
├── src/
│   └── main.css         Tailwind input
├── dist/
│   └── output.css       Compiled Tailwind
├── css/
│   ├── main.css         Global styles, scrollbars, form styling
│   └── modals.css       Modal-specific styles (lightbox, editor, comparison)
├── vendor/
│   ├── marked.min.js    Markdown parser (for plugin result modals)
│   └── purify.min.js    DOMPurify HTML sanitizer
├── js/
│   ├── app.js           Core app logic, event handlers, view state
│   ├── ui.js            Card rendering, gallery management, view mode toggle
│   ├── editor.js        Image/text editor wiring (delegates to editor/ modules)
│   ├── editor/
│   │   ├── editor-core.js    Canvas management, undo/redo, event dispatch
│   │   ├── tool-brush.js     Brush and eraser tools
│   │   ├── tool-shapes.js    Line, rectangle, circle tools
│   │   ├── tool-arrow.js     Arrow tool
│   │   ├── tool-text.js      Text overlay tool
│   │   ├── tool-crop.js      Crop tool with aspect ratio
│   │   ├── tool-select.js    Selection tool
│   │   ├── tool-anonymize.js Pixelate/blur anonymization tool
│   │   ├── tool-eyedropper.js Color picker tool
│   │   ├── tool-zoom.js      Zoom controls
│   │   └── tool-transform.js Rotate/flip transforms
│   ├── modals.js        Lightbox viewer, comparison modal, gesture handling
│   ├── tags.js          Tag management UI
│   ├── settings.js      Settings modal
│   ├── plugins.js       Plugin management UI
│   ├── plugin-icons.js  Plugin icon rendering
│   ├── task-queue.js    Plugin task progress UI
│   ├── watch.js         Watch folders UI
│   ├── transfer.js      Drag-out transfer state management
│   ├── transfer-strategies.js  Platform-specific drag data adapters
│   ├── modal-renderer.js       Plugin result modal rendering
│   ├── metadata.js      Clip metadata UI
│   ├── sort.js          Gallery sorting controls
│   ├── shortcuts.js     Keyboard shortcut registration and context handling
│   ├── plugin-review.js Plugin permission review UI
│   ├── tooltips.js      Tooltip enable/disable and hover behavior
│   ├── utils.js         Shared utilities (confirm/prompt dialogs, toasts, escapeHTML)
│   ├── wails-api.js     Wails bindings wrapper (loadClips, upload, fileToFileData)
│   ├── serve.js         Tag HTTP server management UI, view switching
│   ├── api-settings.js  REST API settings modal
│   ├── context-menu.js  Context menu rendering and event handling
│   ├── folder-drag.js   Drag-and-drop for tag folder reordering
│   └── roving-tabindex.js  Keyboard grid/list navigation
└── wailsjs/
    ├── go/main/App.js               Generated App bindings
    ├── go/main/PluginService.js     Generated PluginService bindings
    ├── go/main/ClipboardService.js  Generated ClipboardService bindings
    ├── go/main/TransferService.js   Generated TransferService bindings
    ├── go/main/ServeService.js      Generated ServeService bindings
    ├── go/main/APIService.js        Generated APIService bindings
    ├── go/main/*.d.ts               Generated TypeScript definitions for bindings
    ├── go/models.ts                 Generated Go struct type definitions
    └── runtime/runtime.js           Wails runtime
```

## Module Overview

### app.js — Core Application

Main application logic and event handling.

**Key responsibilities:**
- DOM element references
- Global state management
- Event listener setup
- File handling (paste, drop)
- Gallery loading

**State variables:**
```javascript
let isViewingArchive = false;     // Active vs Archive view
let selectedIds = new Set();       // Multi-select
let imageClips = [];               // For lightbox
let currentLightboxIndex = -1;     // Lightbox position
```

**Key functions:**
```javascript
handleFiles(files)      // Process dropped/pasted files
handleText(text)        // Process pasted text
toggleFolderMode()      // Toggle folder-mode navigation
getUploadExpirationMinutes() // Read the upload expiry selector
loadTags()              // Fetch tags and initialize tag UI
```

### ui.js — UI Interactions

Handles gallery rendering, card behavior, folder cards, and selection UI.

**Key responsibilities:**
- Gallery rendering
- Clip card generation
- Checkbox and Shift-click selection
- Search filtering in the rendered gallery
- Folder-mode card rendering
- Bulk toolbar state

**Key functions:**
```javascript
createClipCard(clip)    // Build individual card HTML
toggleViewMode()        // Switch active/archive mode
renderFolderCards()     // Build folder cards from tag hierarchy
navigateToFolder(id)    // Enter a tag folder in folder mode
updateBulkToolbar()     // Show/hide bulk actions based on selection
```

### editor.js — Image/Text Editing

Canvas-based image editor and text editor.

**Editor features:**
- Text editing for `text/*` and `application/json`
- Image editing for `image/*`
- Tools: select, crop, brush, eraser, line, arrow, rectangle, circle, text, anonymize, eyedropper
- Undo/redo (50 steps)
- Rotate/flip transforms
- Save in place or save as a new clip

**Key functions:**
```javascript
openEditor(clipId)         // Open image/text editor for a clip
saveEditorInPlace()        // Overwrite the current clip
saveEditorContent()        // Save edits as a new clip
setupEditorListeners()     // Wire buttons, sliders, and tool controls
```

**Canvas handling:**
```javascript
const canvas = document.getElementById('editor-canvas');
const overlayCanvas = document.getElementById('editor-overlay-canvas');

const tools = new Map();
let activeToolName = null;
let undoStack = [];
let redoStack = [];
```

### lightbox.js — Image Viewer

`LightboxController` owns the complete image-viewer lifecycle and exposes:

```javascript
LightboxController.open({ clips, currentId, opener })
LightboxController.openSingle({ clip, opener })
LightboxController.setClips(clips)
LightboxController.command(name, detail)
LightboxController.close()
```

The controller uses stable clip IDs, generation-guarded image loading, actual-scale Fit/1:1 zoom, focal-point transforms, focus restoration, and keyboard/mouse/touchpad/touch adapters. Callers must not mutate lightbox DOM or state directly.

Backdrop clicks close the viewer when the `shouldCloseOnBackdrop` dependency allows it (backed by the `lightbox_close_on_backdrop` setting). Only the viewport and the pan layer count as backdrop — the pan layer is laid out at the image's natural size, so for a zoomed image its box can span the whole viewport. A `suppressBackdropClick` flag, set on `pointerdown`/`touchstart` and by drags past a slop threshold, keeps non-primary buttons, menu-dismissing clicks, and pans that end over the backdrop from closing it. `contextmenu` inside the viewport opens the clip actions menu at the pointer via the `openContextMenu` dependency.

`modals.js` retains image comparison and other modal behavior.

### watch.js — Watch Folders

Watch folder configuration UI.

**Key responsibilities:**
- Folder list display
- Add/edit folder modal
- Filter configuration (`all`, preset checkboxes, or regex)
- Auto-tag assignment
- Optional processing of existing files when adding a folder
- Pause controls
- Status indicators

**Key functions:**
```javascript
loadWatchFolders()           // Fetch and render folders
openAddFolderDialog()        // Pick a folder, then open the modal
openFolderModal(path)        // Configure a new watched folder
saveFolderConfig()           // Persist add/edit changes
toggleFolderPause(id)        // Pause/resume folder
toggleGlobalPause()          // Global pause toggle
```

### utils.js — Utilities

Helper functions used across modules.

**Key functions:**
```javascript
showConfirmDialog(...)    // Shared confirm modal
showPromptDialog(...)     // Shared prompt modal
showToast(message)        // Toast notifications
trapFocus(container)      // Focus trap helper
copyToClipboard(text)     // Clipboard helper for plain text
escapeHTML(str)           // XSS prevention
```

### wails-api.js — Backend Wrapper

Wraps `window.go.main.*` Wails bindings with higher-level UI helpers.

```javascript
const clips = await window.go.main.App.GetClips(false, [], [], 'date', 'desc');
const clipData = await window.go.main.App.GetClipData(42);
await window.go.main.App.BulkDownloadToFile([1, 2, 3]);
```

### transfer.js — Drag-Out Transfers

Manages the state machine for dragging clips out of mahpastes into other apps. Wraps Wails bindings for `TransferService` and handles the prepare-then-drag lifecycle.

**Key responsibilities:**
- Prepare temp files for drag-out via `TransferService.PrepareClipForTransfer`
- Initiate native OS drag via `TransferService.StartNativeDragOut`
- Track in-flight transfer state

### transfer-strategies.js — Platform Drag Adapters

Platform-specific adapters for drag data formatting. On macOS, uses the `file-uri-v1` strategy to provide file URIs for native drag operations.

**Key responsibilities:**
- Abstract platform differences for drag data
- Provide `file://` URIs on macOS for Finder/Mail/Slack compatibility

### modal-renderer.js — Plugin Result Modals

Renders plugin action results in modal dialogs. Supports markdown (via marked + DOMPurify), plain text, and image formats.

**Key responsibilities:**
- Render markdown content with sanitized HTML
- Display base64 images from plugin results
- Provide copy-to-clipboard and paste-as-clip actions from modal content

### serve.js — Tag Server Management

UI for starting and stopping per-tag HTTP servers. Lets users pick a port, toggle LAN binding, and set JSON API access level (`none`, `read`, `readwrite`). Communicates with `ServeService` Wails bindings.

### api-settings.js — REST API Settings

Settings modal panel for the REST API server. Provides controls to start/stop the API, choose a port, and create or revoke API keys. Communicates with `APIService` Wails bindings.

Revoking goes through the shared in-app `showConfirmDialog` rather than the native `confirm()`, which a Wails webview does not reliably render. Revoked keys stay in the list, greyed out, with the `revoked_at` stamp in the badge's `title` and a note that the row is removed 7 days after revoking.

### context-menu.js — Context Menus

Renders right-click context menus on clip cards and other elements. Handles positioning, keyboard navigation within the menu, and dismissal on outside click or `Escape`.

### folder-drag.js — Folder Drag-and-Drop

Drag-and-drop support for reordering and nesting tag folders. Handles drag handles, drop targets, and visual feedback during drag operations.

### roving-tabindex.js — Keyboard Grid Navigation

Implements the roving tabindex pattern for keyboard navigation across gallery grids and list elements. Arrow keys move focus between items without requiring Tab for each cell.

## Event Handling

### Paste Events

```javascript
document.addEventListener('paste', e => {
    if (e.clipboardData.files.length > 0) {
        handleFiles(e.clipboardData.files);
    } else {
        const text = e.clipboardData.getData('text/plain');
        if (text) handleText(text);
    }
});
```

### Drag and Drop

```javascript
dropZone.addEventListener('drop', e => {
    e.preventDefault();
    handleFiles(e.dataTransfer.files);
});
```

### Keyboard Shortcuts

```javascript
document.addEventListener('keydown', e => {
    // ShortcutManager resolves context first:
    // clip, bulk, lightbox, editor, comparison, gallery, global.
    // app.js registers the actions; shortcuts.js handles dispatch and overrides.
});
```

## Backend Communication

### Calling Go Functions

```javascript
// GetClips takes archived flag, tag filter IDs, hidden tag IDs, sort field, and sort direction
const clips = await window.go.main.App.GetClips(false, [], [], 'date', 'desc');

// UploadFiles accepts file data, expiration minutes, and optional auto-tag ID
await window.go.main.App.UploadFiles(fileDataArray, expirationMinutes, autoTagID);

// Service-specific bindings are exposed under window.go.main.*
const plugins = await window.go.main.PluginService.GetPlugins();
```

### Listening to Events

```javascript
window.runtime.EventsOn('watch:import', (filename) => {
    showToast(`Imported: ${filename}`);
    loadClips();
});

window.runtime.EventsOn('watch:error', (data) => {
    showToast(`Error importing ${data.file}: ${data.error}`, 'error');
});
```

## Styling

### Tailwind Usage

The app is built mostly around Tailwind's `stone` palette, with accent colors such as `emerald`, `red`, and `amber` for status and warnings:

```html
<div class="flex items-center gap-2 p-4 bg-stone-50 rounded-lg">
    <button class="px-4 py-2 bg-stone-800 text-white rounded hover:bg-stone-700">
        Click me
    </button>
</div>
```

### Custom CSS

For complex components, custom CSS in `css/main.css`:

```css
/* Card context menu dropdown */
.card-menu-dropdown {
    background-color: white;
    border-radius: 0.375rem;
    box-shadow: 0 10px 15px -3px rgba(0, 0, 0, 0.1),
                0 4px 6px -2px rgba(0, 0, 0, 0.05);
    border: 1px solid #e7e5e4;
    padding: 0.25rem 0;
    z-index: 110;
    min-width: 140px;
}

/* Gallery card focus state */
#gallery > li:focus-visible {
    outline: 2px solid #a8a29e;
    outline-offset: 2px;
    transform: scale(1.02);
    z-index: 1;
}
```

### Modal Styling

Modals in `css/modals.css`:

```css
.modal-backdrop {
    position: fixed;
    inset: 0;
    background-color: rgba(28, 25, 23, 0.4);
    backdrop-filter: blur(4px);
    z-index: 150;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 1rem;
    transition: opacity 0.2s ease;
}

.modal-content {
    background: white;
    border-radius: 0.5rem;
    box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.25);
    width: 100%;
    overflow: hidden;
    transform: scale(0.95);
    transition: transform 0.2s ease;
}
```

## Build Process

### Development

`wails dev` handles everything automatically -- it runs `npm install` and starts the Tailwind CSS watcher via `wails.json` configuration (`frontend:install` and `frontend:dev:watcher`):

```bash
# From the project root -- this is all you need
wails dev
```

If you need to run the frontend build steps manually (e.g., to debug a Tailwind issue):

```bash
cd frontend
npm install              # Install Tailwind
npm run dev              # Watch CSS changes
```

### Production

`wails build` runs `npm install` and `npm run build` automatically before bundling:

```bash
# From the project root
wails build
```

To run the frontend build step in isolation:

```bash
cd frontend
npm run build           # Build and minify Tailwind CSS
```

## Best Practices

### DOM Queries

Cache DOM references at module load:

```javascript
// Good - cached once
const gallery = document.getElementById('gallery');

// Bad - queried every call
function render() {
    document.getElementById('gallery').innerHTML = '';
}
```

### Event Delegation

For dynamic content, use delegation:

```javascript
// Good - single listener on parent
gallery.addEventListener('click', e => {
    if (e.target.matches('.delete-btn')) {
        deleteClip(e.target.dataset.id);
    }
});

// Bad - listener per element (recreated on render)
document.querySelectorAll('.delete-btn').forEach(btn => {
    btn.addEventListener('click', () => deleteClip(btn.dataset.id));
});
```

### Async/Await

Use async/await for clarity:

```javascript
// Good
async function loadAndRender() {
    const clips = await window.go.main.App.GetClips(false, [], [], 'date', 'desc');
    for (const clip of clips) {
        await createClipCard(clip);
    }
}

// Less clear
function loadAndRender() {
    window.go.main.App.GetClips(false, [], [], 'date', 'desc')
        .then(clips => Promise.all(clips.map(createClipCard)));
}
```
