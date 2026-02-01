---
sidebar_position: 2
---

# Frontend Architecture

The frontend is built with vanilla JavaScript, Tailwind CSS, and HTML. No framework, no build step (except Tailwind compilation).

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
│   ├── main.css         Custom styles
│   └── modals.css       Modal-specific styles
├── js/
│   ├── app.js           Core app logic (246 lines)
│   ├── ui.js            UI interactions (308 lines)
│   ├── editor.js        Image/text editor (700 lines)
│   ├── modals.js        Modal management (226 lines)
│   ├── watch.js         Watch folders (432 lines)
│   ├── utils.js         Utilities (151 lines)
│   └── wails-api.js     Backend wrapper (183 lines)
└── wailsjs/
    ├── go/main/App.js   Generated bindings
    └── runtime/runtime.js Wails runtime
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
loadClips()             // Fetch and render gallery
toggleViewMode()        // Switch Active/Archive
```

### ui.js — UI Interactions

Handles user interface behaviors.

**Key responsibilities:**
- Gallery rendering
- Clip card generation
- Lightbox navigation
- Comparison modal
- Toast notifications
- Bulk actions

**Key functions:**
```javascript
renderClips(clips)      // Render gallery grid
createClipCard(clip)    // Build individual card HTML
openLightbox(index)     // Open image viewer
closeLightbox()         // Close image viewer
showToast(message)      // Show notification
```

### editor.js — Image/Text Editing

Canvas-based image editor and text editor.

**Image editor features:**
- Tools: brush, line, rectangle, circle, text, eraser
- Undo/redo (50 steps)
- Color picker
- Stroke width
- Keyboard shortcuts

**Key functions:**
```javascript
openImageEditor(clipId)    // Open image in editor
openTextEditor(clipId)     // Open text in editor
saveImageEdit()            // Save annotated image
setupEditorListeners()     // Initialize editor events
```

**Canvas handling:**
```javascript
const canvas = document.getElementById('editor-canvas');
const ctx = canvas.getContext('2d');

// Drawing state
let isDrawing = false;
let currentTool = 'brush';
let history = [];           // Undo stack
let historyIndex = -1;      // Current position
```

### modals.js — Modal Management

Generic modal handling and confirm dialogs.

**Key functions:**
```javascript
openModal(modalId)        // Show a modal
closeModal(modalId)       // Hide a modal
showConfirmDialog(opts)   // Confirmation prompt
closeConfirmDialog()      // Close confirmation
```

**Focus trapping:**
Modals trap keyboard focus for accessibility:
```javascript
modal.addEventListener('keydown', e => {
    if (e.key === 'Tab') {
        // Trap focus within modal
    }
    if (e.key === 'Escape') {
        closeModal(modalId);
    }
});
```

### watch.js — Watch Folders

Watch folder configuration UI.

**Key responsibilities:**
- Folder list display
- Add/edit folder modal
- Filter configuration
- Pause controls
- Status indicators

**Key functions:**
```javascript
loadWatchFolders()           // Fetch and render folders
openAddFolderModal()         // Show add dialog
saveWatchFolder()            // Save configuration
toggleFolderPause(id)        // Pause/resume folder
toggleGlobalPause()          // Global pause toggle
```

### utils.js — Utilities

Helper functions used across modules.

**Key functions:**
```javascript
fileToFileData(file)      // Convert File to API format
formatBytes(bytes)        // Human-readable size
formatDate(date)          // Format timestamp
debounce(fn, delay)       // Debounce function
escapeHtml(str)           // XSS prevention
```

### wails-api.js — Backend Wrapper

Wraps Wails bindings with error handling.

```javascript
import * as App from '../wailsjs/go/main/App';

export async function getClips(archived) {
    try {
        return await App.GetClips(archived);
    } catch (error) {
        console.error('Failed to get clips:', error);
        throw error;
    }
}
```

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
    // Global shortcuts
    if (e.key === 'Escape') {
        closeLightbox();
        closeModal();
    }

    // Editor shortcuts
    if (editorOpen) {
        if (e.key === 'b') setTool('brush');
        // ...
    }
});
```

## Backend Communication

### Calling Go Functions

```javascript
import { GetClips, UploadFiles } from '../wailsjs/go/main/App';

// Async function call
const clips = await GetClips(false);

// With parameters
await UploadFiles(fileDataArray, expirationMinutes);
```

### Listening to Events

```javascript
import { EventsOn } from '../wailsjs/runtime/runtime';

EventsOn('watch:import', (filename) => {
    showToast(`Imported: ${filename}`);
    loadClips();
});

EventsOn('watch:error', (data) => {
    showToast(`Error importing ${data.file}: ${data.error}`, 'error');
});
```

## Styling

### Tailwind Usage

```html
<div class="flex items-center gap-2 p-4 bg-slate-50 rounded-lg">
    <button class="px-4 py-2 bg-blue-500 text-white rounded hover:bg-blue-600">
        Click me
    </button>
</div>
```

### Custom CSS

For complex components, custom CSS in `css/main.css`:

```css
.clip-card {
    transition: transform 0.2s, box-shadow 0.2s;
}

.clip-card:hover {
    transform: translateY(-2px);
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.1);
}
```

### Modal Styling

Modals in `css/modals.css`:

```css
.modal {
    position: fixed;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(0, 0, 0, 0.5);
}

.modal-content {
    background: white;
    border-radius: 0.5rem;
    max-width: 90vw;
    max-height: 90vh;
}
```

## Build Process

### Development

```bash
cd frontend
npm install              # Install Tailwind
npm run watch            # Watch CSS changes

# In root directory
wails dev               # Start dev server
```

### Production

```bash
cd frontend
npm run build           # Build Tailwind CSS

# In root directory
wails build             # Build app
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
    const clips = await getClips(false);
    renderClips(clips);
}

// Less clear
function loadAndRender() {
    getClips(false).then(clips => renderClips(clips));
}
```
