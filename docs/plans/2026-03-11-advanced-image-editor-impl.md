# Advanced Image Editor Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Upgrade the image editor from a lightweight annotation tool to a capable general-purpose editor with crop, selection/move, anonymize, zoom/pan, arrow, eyedropper, rotate/flip, and full ShortcutManager integration.

**Architecture:** Refactor `editor.js` into a modular tool system under `frontend/js/editor/`. Each tool implements a common interface (activate, deactivate, mouse handlers, cursor, overlay rendering). A core module manages canvas, zoom/pan transforms, undo/redo, and tool switching. All shortcuts registered through the existing ShortcutManager with a new `'editor'` context.

**Tech Stack:** Vanilla JS (no framework/bundler), HTML5 Canvas API, CSS transforms for zoom, Playwright for e2e tests.

**Design doc:** `docs/plans/2026-03-11-advanced-image-editor-design.md`

---

### Task 1: Editor Core Architecture

Create the foundation module that manages canvas, state, tool registry, coordinate translation, and undo/redo. This replaces the global state in `editor.js`.

**Files:**
- Create: `frontend/js/editor/editor-core.js`
- Read: `frontend/js/editor.js` (current implementation for reference)

**Step 1: Create `editor-core.js` with EditorCore singleton**

```javascript
// frontend/js/editor/editor-core.js
// --- Editor Core ---
// Manages canvas, state, tool registry, coordinate translation, undo/redo

const EditorCore = (() => {
    // Canvas references
    let canvas = null;
    let ctx = null;
    let overlayCanvas = null;
    let overlayCtx = null;

    // Image state
    let originalImage = null;
    let originalContentType = '';  // Track for output format preservation

    // Tool registry
    const tools = new Map();  // name -> tool object
    let activeTool = null;
    let activeToolName = '';

    // Drawing properties (shared across tools)
    let currentColor = '#44403c';
    let currentOpacity = 1;
    let brushSize = 8;
    let fontSize = 24;  // Dedicated font size for text tool

    // Drawing state
    let isDrawing = false;
    let startX = 0;
    let startY = 0;
    let lastX = 0;
    let lastY = 0;

    // Zoom/pan state
    let zoomLevel = 1;      // 1 = 100%
    let panX = 0;
    let panY = 0;
    let isPanning = false;
    let spaceHeld = false;

    // Undo/redo
    let undoStack = [];  // ImageData objects
    let redoStack = [];
    const MAX_UNDO = 50;
    const MAX_UNDO_MEMORY = 100 * 1024 * 1024;  // 100MB

    // --- Canvas Setup ---

    function initCanvas(canvasEl, overlayEl) {
        canvas = canvasEl;
        ctx = canvas.getContext('2d');
        overlayCanvas = overlayEl;
        overlayCtx = overlayCanvas.getContext('2d');
    }

    async function loadImage(imageBlob, contentType) {
        originalContentType = contentType || 'image/png';
        originalImage = new Image();
        originalImage.src = URL.createObjectURL(imageBlob);

        await new Promise((resolve) => {
            originalImage.onload = resolve;
        });

        const maxSize = 4000;
        let width = originalImage.width;
        let height = originalImage.height;

        if (width > maxSize || height > maxSize) {
            const ratio = Math.min(maxSize / width, maxSize / height);
            width = Math.floor(width * ratio);
            height = Math.floor(height * ratio);
        }

        canvas.width = width;
        canvas.height = height;
        overlayCanvas.width = width;
        overlayCanvas.height = height;

        ctx.drawImage(originalImage, 0, 0, width, height);
        syncOverlay();
        saveUndoState();

        // Reset zoom
        zoomLevel = 1;
        panX = 0;
        panY = 0;
        applyTransform();
    }

    function syncOverlay() {
        if (!canvas || !overlayCanvas) return;
        const rect = canvas.getBoundingClientRect();
        const container = canvas.parentElement.getBoundingClientRect();
        overlayCanvas.style.left = (rect.left - container.left) + 'px';
        overlayCanvas.style.top = (rect.top - container.top) + 'px';
        overlayCanvas.style.width = rect.width + 'px';
        overlayCanvas.style.height = rect.height + 'px';
    }

    // --- Coordinate Translation ---

    function screenToCanvas(clientX, clientY) {
        const rect = canvas.getBoundingClientRect();
        const scaleX = canvas.width / rect.width;
        const scaleY = canvas.height / rect.height;
        return {
            x: (clientX - rect.left) * scaleX,
            y: (clientY - rect.top) * scaleY
        };
    }

    // --- Zoom/Pan Transform ---

    function applyTransform() {
        if (!canvas) return;
        const container = canvas.parentElement;
        if (!container) return;
        canvas.style.transform = `scale(${zoomLevel}) translate(${panX}px, ${panY}px)`;
        canvas.style.transformOrigin = 'center center';
        syncOverlay();
    }

    // --- Tool Registry ---

    function registerTool(name, tool) {
        tools.set(name, tool);
    }

    function selectTool(name) {
        if (activeTool && activeTool.deactivate) {
            activeTool.deactivate();
        }
        activeToolName = name;
        activeTool = tools.get(name) || null;
        if (activeTool && activeTool.activate) {
            activeTool.activate();
        }
        updateToolButtons();
        updateCursor();
    }

    function updateToolButtons() {
        document.querySelectorAll('.editor-tool-btn').forEach(btn => {
            const isActive = btn.dataset.tool === activeToolName;
            btn.classList.toggle('active', isActive);
        });
    }

    function updateCursor() {
        if (!canvas) return;
        if (activeTool && activeTool.getCursor) {
            canvas.style.cursor = activeTool.getCursor();
        } else {
            canvas.style.cursor = 'crosshair';
        }
    }

    // --- Undo/Redo (ImageData) ---

    function getUndoMemory() {
        let total = 0;
        for (const state of undoStack) {
            total += state.data.byteLength;
        }
        return total;
    }

    function saveUndoState() {
        redoStack = [];
        const imageData = ctx.getImageData(0, 0, canvas.width, canvas.height);
        undoStack.push(imageData);

        // Enforce memory ceiling — drop oldest (but keep original at index 0)
        while (undoStack.length > MAX_UNDO || (undoStack.length > 1 && getUndoMemory() > MAX_UNDO_MEMORY)) {
            if (undoStack.length <= 2) break; // Keep original + current
            undoStack.splice(1, 1); // Remove second-oldest (preserve original)
        }

        updateUndoRedoButtons();
    }

    function undo() {
        if (undoStack.length <= 1) return;
        redoStack.push(undoStack.pop());
        ctx.putImageData(undoStack[undoStack.length - 1], 0, 0);
        updateUndoRedoButtons();
    }

    function redo() {
        if (redoStack.length === 0) return;
        const state = redoStack.pop();
        undoStack.push(state);
        ctx.putImageData(state, 0, 0);
        updateUndoRedoButtons();
    }

    function updateUndoRedoButtons() {
        const undoBtn = document.getElementById('editor-undo');
        const redoBtn = document.getElementById('editor-redo');
        if (undoBtn) {
            undoBtn.disabled = undoStack.length <= 1;
            undoBtn.classList.toggle('opacity-50', undoStack.length <= 1);
        }
        if (redoBtn) {
            redoBtn.disabled = redoStack.length === 0;
            redoBtn.classList.toggle('opacity-50', redoStack.length === 0);
        }
    }

    // --- Mouse Event Dispatch ---

    function handleMouseDown(e) {
        // Pan with space held
        if (spaceHeld) {
            isPanning = true;
            lastX = e.clientX;
            lastY = e.clientY;
            return;
        }

        const coords = screenToCanvas(e.clientX, e.clientY);
        startX = coords.x;
        startY = coords.y;
        lastX = coords.x;
        lastY = coords.y;
        isDrawing = true;

        if (activeTool && activeTool.onMouseDown) {
            activeTool.onMouseDown(coords, e);
        }
    }

    function handleMouseMove(e) {
        if (isPanning) {
            const dx = e.clientX - lastX;
            const dy = e.clientY - lastY;
            panX += dx / zoomLevel;
            panY += dy / zoomLevel;
            lastX = e.clientX;
            lastY = e.clientY;
            applyTransform();
            return;
        }

        const coords = screenToCanvas(e.clientX, e.clientY);

        if (isDrawing && activeTool && activeTool.onMouseMove) {
            activeTool.onMouseMove(coords, e);
        }

        lastX = coords.x;
        lastY = coords.y;
    }

    function handleMouseUp(e) {
        if (isPanning) {
            isPanning = false;
            return;
        }

        if (!isDrawing) return;
        isDrawing = false;

        const coords = screenToCanvas(e.clientX, e.clientY);

        if (activeTool && activeTool.onMouseUp) {
            activeTool.onMouseUp(coords, e);
        }
    }

    // --- Touch Support ---

    function handleTouchStart(e) {
        e.preventDefault();
        const touch = e.touches[0];
        handleMouseDown({ clientX: touch.clientX, clientY: touch.clientY, shiftKey: false, altKey: false, preventDefault: () => {} });
    }

    function handleTouchMove(e) {
        e.preventDefault();
        const touch = e.touches[0];
        handleMouseMove({ clientX: touch.clientX, clientY: touch.clientY, shiftKey: false, altKey: false });
    }

    function handleTouchEnd(e) {
        handleMouseUp({ clientX: lastX, clientY: lastY, shiftKey: false, altKey: false });
    }

    // --- Lifecycle ---

    function attachListeners() {
        if (!canvas) return;
        canvas.addEventListener('mousedown', handleMouseDown);
        canvas.addEventListener('mousemove', handleMouseMove);
        canvas.addEventListener('mouseup', handleMouseUp);
        canvas.addEventListener('mouseleave', handleMouseUp);
        canvas.addEventListener('touchstart', handleTouchStart, { passive: false });
        canvas.addEventListener('touchmove', handleTouchMove, { passive: false });
        canvas.addEventListener('touchend', handleTouchEnd);
    }

    function detachListeners() {
        if (!canvas) return;
        canvas.removeEventListener('mousedown', handleMouseDown);
        canvas.removeEventListener('mousemove', handleMouseMove);
        canvas.removeEventListener('mouseup', handleMouseUp);
        canvas.removeEventListener('mouseleave', handleMouseUp);
        canvas.removeEventListener('touchstart', handleTouchStart);
        canvas.removeEventListener('touchmove', handleTouchMove);
        canvas.removeEventListener('touchend', handleTouchEnd);
    }

    function reset() {
        undoStack = [];
        redoStack = [];
        isDrawing = false;
        isPanning = false;
        spaceHeld = false;
        zoomLevel = 1;
        panX = 0;
        panY = 0;
        currentColor = '#44403c';
        currentOpacity = 1;
        brushSize = 8;
        fontSize = 24;
        activeToolName = '';
        if (activeTool && activeTool.deactivate) activeTool.deactivate();
        activeTool = null;
    }

    // --- Public API ---

    return {
        // Canvas
        initCanvas,
        loadImage,
        syncOverlay,
        screenToCanvas,
        get canvas() { return canvas; },
        get ctx() { return ctx; },
        get overlayCanvas() { return overlayCanvas; },
        get overlayCtx() { return overlayCtx; },

        // Tool registry
        registerTool,
        selectTool,
        get activeToolName() { return activeToolName; },
        get activeTool() { return activeTool; },

        // Drawing properties
        get currentColor() { return currentColor; },
        set currentColor(v) { currentColor = v; },
        get currentOpacity() { return currentOpacity; },
        set currentOpacity(v) { currentOpacity = v; },
        get brushSize() { return brushSize; },
        set brushSize(v) { brushSize = v; },
        get fontSize() { return fontSize; },
        set fontSize(v) { fontSize = v; },

        // Drawing state
        get isDrawing() { return isDrawing; },
        get startX() { return startX; },
        get startY() { return startY; },
        get lastX() { return lastX; },
        get lastY() { return lastY; },

        // Zoom/pan
        get zoomLevel() { return zoomLevel; },
        set zoomLevel(v) { zoomLevel = v; },
        get panX() { return panX; },
        set panX(v) { panX = v; },
        get panY() { return panY; },
        set panY(v) { panY = v; },
        get spaceHeld() { return spaceHeld; },
        set spaceHeld(v) { spaceHeld = v; },
        applyTransform,

        // Undo/redo
        saveUndoState,
        undo,
        redo,

        // Original format
        get originalContentType() { return originalContentType; },

        // Lifecycle
        attachListeners,
        detachListeners,
        reset,
    };
})();
```

**Step 2: Verify file created**

Run: `ls frontend/js/editor/editor-core.js`
Expected: File exists

**Step 3: Add `<script>` tag to `index.html`**

In `frontend/index.html`, add the editor module scripts BEFORE `editor.js` (line 1465). The new scripts must load before editor.js since editor.js will use them.

```html
    <script src="js/editor/editor-core.js"></script>
    <script src="js/editor.js"></script>
```

Note: Remove the old `<script src="js/editor.js"></script>` line and replace with the above block. Additional tool scripts will be added in subsequent tasks.

**Step 4: Run e2e tests to verify no regression**

Run: `cd e2e && npm test -- --grep "Image Editor" 2>&1 | tail -30`
Expected: All existing editor tests pass (the old editor.js still works alongside editor-core.js since we haven't migrated yet)

**Step 5: Commit**

```bash
git add frontend/js/editor/editor-core.js frontend/index.html
git commit -m "feat(editor): add EditorCore foundation module"
```

---

### Task 2: Refactor Existing Drawing Tools

Extract brush/eraser, shape tools, and text tool from `editor.js` into separate modules that use the EditorCore tool interface.

**Files:**
- Create: `frontend/js/editor/tool-brush.js`
- Create: `frontend/js/editor/tool-shapes.js`
- Create: `frontend/js/editor/tool-text.js`
- Modify: `frontend/js/editor.js` (gut it, make it a thin wrapper)
- Modify: `frontend/index.html` (add new script tags)

**Step 1: Create `tool-brush.js`**

```javascript
// frontend/js/editor/tool-brush.js
// Brush and Eraser tools — share drawing logic, differ in compositing mode

const BrushTool = (() => {
    function createBrushTool(isEraser) {
        return {
            activate() {},
            deactivate() {},
            getCursor() { return 'crosshair'; },

            onMouseDown(coords, e) {
                const ctx = EditorCore.ctx;
                ctx.beginPath();
                ctx.moveTo(coords.x, coords.y);
            },

            onMouseMove(coords, e) {
                const ctx = EditorCore.ctx;
                if (isEraser) {
                    ctx.globalCompositeOperation = 'destination-out';
                } else {
                    ctx.globalCompositeOperation = 'source-over';
                    ctx.globalAlpha = EditorCore.currentOpacity;
                    ctx.strokeStyle = EditorCore.currentColor;
                }
                ctx.lineWidth = EditorCore.brushSize;
                ctx.lineCap = 'round';
                ctx.lineJoin = 'round';

                ctx.lineTo(coords.x, coords.y);
                ctx.stroke();
                ctx.beginPath();
                ctx.moveTo(coords.x, coords.y);

                if (isEraser) {
                    ctx.globalCompositeOperation = 'source-over';
                }
            },

            onMouseUp(coords, e) {
                const ctx = EditorCore.ctx;
                ctx.globalCompositeOperation = 'source-over';
                ctx.globalAlpha = 1;
                EditorCore.saveUndoState();
            },
        };
    }

    return {
        createBrush() { return createBrushTool(false); },
        createEraser() { return createBrushTool(true); },
    };
})();
```

**Step 2: Create `tool-shapes.js`**

```javascript
// frontend/js/editor/tool-shapes.js
// Line, Rectangle, Circle tools — share shape preview pattern

const ShapeTools = (() => {
    function snapTo45(x1, y1, x2, y2) {
        const dx = x2 - x1;
        const dy = y2 - y1;
        const angle = Math.atan2(dy, dx);
        const distance = Math.sqrt(dx * dx + dy * dy);
        const snappedAngle = Math.round(angle / (Math.PI / 4)) * (Math.PI / 4);
        return {
            x: x1 + Math.cos(snappedAngle) * distance,
            y: y1 + Math.sin(snappedAngle) * distance
        };
    }

    function drawPreview(toolName, endX, endY, snap) {
        const oc = EditorCore.overlayCtx;
        const ov = EditorCore.overlayCanvas;
        oc.clearRect(0, 0, ov.width, ov.height);
        oc.globalAlpha = EditorCore.currentOpacity * 0.5;
        oc.strokeStyle = EditorCore.currentColor;
        oc.lineWidth = EditorCore.brushSize;
        oc.setLineDash([5, 5]);

        const sx = EditorCore.startX;
        const sy = EditorCore.startY;

        if (toolName === 'line') {
            let ex = endX, ey = endY;
            if (snap) {
                const s = snapTo45(sx, sy, endX, endY);
                ex = s.x; ey = s.y;
            }
            oc.beginPath();
            oc.moveTo(sx, sy);
            oc.lineTo(ex, ey);
            oc.stroke();
        } else if (toolName === 'rectangle') {
            oc.beginPath();
            oc.rect(Math.min(sx, endX), Math.min(sy, endY), Math.abs(endX - sx), Math.abs(endY - sy));
            oc.stroke();
        } else if (toolName === 'circle') {
            const radius = Math.sqrt(Math.pow(endX - sx, 2) + Math.pow(endY - sy, 2));
            oc.beginPath();
            oc.arc(sx, sy, radius, 0, Math.PI * 2);
            oc.stroke();
        }

        oc.setLineDash([]);
        oc.globalAlpha = 1;
    }

    function createShapeTool(toolName) {
        return {
            activate() {},
            deactivate() {},
            getCursor() { return 'crosshair'; },

            onMouseDown(coords, e) {},

            onMouseMove(coords, e) {
                drawPreview(toolName, coords.x, coords.y, e.shiftKey);
            },

            onMouseUp(coords, e) {
                const oc = EditorCore.overlayCtx;
                const ov = EditorCore.overlayCanvas;
                oc.clearRect(0, 0, ov.width, ov.height);

                const ctx = EditorCore.ctx;
                ctx.globalAlpha = EditorCore.currentOpacity;
                ctx.strokeStyle = EditorCore.currentColor;
                ctx.lineWidth = EditorCore.brushSize;

                const sx = EditorCore.startX;
                const sy = EditorCore.startY;

                if (toolName === 'line') {
                    let ex = coords.x, ey = coords.y;
                    if (e.shiftKey) {
                        const s = snapTo45(sx, sy, coords.x, coords.y);
                        ex = s.x; ey = s.y;
                    }
                    ctx.lineCap = 'round';
                    ctx.beginPath();
                    ctx.moveTo(sx, sy);
                    ctx.lineTo(ex, ey);
                    ctx.stroke();
                } else if (toolName === 'rectangle') {
                    ctx.beginPath();
                    ctx.rect(Math.min(sx, coords.x), Math.min(sy, coords.y),
                             Math.abs(coords.x - sx), Math.abs(coords.y - sy));
                    ctx.stroke();
                } else if (toolName === 'circle') {
                    const radius = Math.sqrt(Math.pow(coords.x - sx, 2) + Math.pow(coords.y - sy, 2));
                    ctx.beginPath();
                    ctx.arc(sx, sy, radius, 0, Math.PI * 2);
                    ctx.stroke();
                }

                ctx.globalAlpha = 1;
                EditorCore.saveUndoState();
            },
        };
    }

    return {
        createLine() { return createShapeTool('line'); },
        createRectangle() { return createShapeTool('rectangle'); },
        createCircle() { return createShapeTool('circle'); },
        snapTo45,
    };
})();
```

**Step 3: Create `tool-text.js`**

```javascript
// frontend/js/editor/tool-text.js
// Text annotation tool with dedicated font size

const TextTool = (() => {
    let textInputActive = false;
    let textInputX = 0;
    let textInputY = 0;

    function showTextInput(x, y) {
        textInputActive = true;
        textInputX = x;
        textInputY = y;

        const input = document.getElementById('canvas-text-input');
        const canvas = EditorCore.canvas;
        const rect = canvas.getBoundingClientRect();
        const scaleX = rect.width / canvas.width;

        const fs = EditorCore.fontSize;
        const screenFontSize = fs * scaleX;

        input.style.left = (rect.left + x * scaleX - 2) + 'px';
        input.style.top = (rect.top + y * (rect.height / canvas.height) - 2) + 'px';
        input.style.fontSize = `${screenFontSize}px`;
        input.style.color = EditorCore.currentColor;
        input.style.fontFamily = 'Arial, sans-serif';
        input.style.lineHeight = '1';
        input.style.padding = '0';
        input.style.margin = '0';
        input.style.display = 'block';
        input.value = '';
        input.focus();
    }

    function commitTextInput() {
        const input = document.getElementById('canvas-text-input');
        const text = input.value.trim();

        if (text) {
            const ctx = EditorCore.ctx;
            ctx.save();
            ctx.globalAlpha = EditorCore.currentOpacity;
            ctx.fillStyle = EditorCore.currentColor;
            ctx.font = `${EditorCore.fontSize}px Arial, sans-serif`;
            ctx.textBaseline = 'top';
            ctx.fillText(text, textInputX, textInputY);
            ctx.restore();
            EditorCore.saveUndoState();
        }

        input.style.display = 'none';
        input.value = '';
        textInputActive = false;
    }

    function create() {
        return {
            activate() {},
            deactivate() {
                if (textInputActive) commitTextInput();
            },
            getCursor() { return 'text'; },

            onMouseDown(coords, e) {
                e.preventDefault();
                if (textInputActive) {
                    commitTextInput();
                }
                showTextInput(coords.x, coords.y);
            },

            onMouseMove(coords, e) {},
            onMouseUp(coords, e) {},
        };
    }

    return {
        create,
        commitTextInput,
        get isActive() { return textInputActive; },
    };
})();
```

**Step 4: Rewrite `editor.js` as thin wrapper**

Replace the contents of `frontend/js/editor.js` to use EditorCore and the new tool modules. Keep the same public API (`openEditor`, `closeEditor`, `setupEditorListeners`, etc.) so `app.js` and `modals.js` don't need changes.

The key changes:
- `openEditor()` calls `EditorCore.initCanvas()`, `EditorCore.loadImage()`, `EditorCore.attachListeners()`, registers tools, selects default tool
- `closeEditor()` calls `EditorCore.detachListeners()`, `EditorCore.reset()`
- `setupEditorListeners()` wires DOM buttons to EditorCore methods
- `saveEditorContent()` uses `EditorCore.originalContentType` for format preservation
- Remove all the global drawing state variables (canvas, ctx, etc.)
- Remove all the drawing functions (drawBrush, drawLine, etc.)
- Remove the ad-hoc keydown listener (will be replaced by ShortcutManager in Task 3)

```javascript
// frontend/js/editor.js
// Thin wrapper — delegates to EditorCore and tool modules

// Editor metadata (not drawing state)
let editorClipId = null;
let editorContentType = '';
let editorFilename = '';
let isTextEditor = false;

function isEditableType(contentType) {
    return contentType.startsWith('text/') ||
        contentType === 'application/json' ||
        contentType.startsWith('image/');
}

function isImageType(contentType) {
    return contentType.startsWith('image/');
}

async function openEditor(clipId) {
    try {
        const clipData = await getClipData(clipId);
        if (!clipData) throw new Error('Failed to load clip');

        const contentType = clipData.content_type || '';
        editorClipId = clipId;
        editorContentType = contentType;
        editorFilename = clipData.filename || `clip_${clipId}`;

        const editorModal = document.getElementById('editor-modal');
        const textEditorView = document.getElementById('text-editor-view');
        const imageEditorView = document.getElementById('image-editor-view');
        const editorFilenameInput = document.getElementById('editor-filename');

        editorFilenameInput.value = getNewFilename(editorFilename);

        if (isImageType(contentType)) {
            isTextEditor = false;
            textEditorView.classList.add('hidden');
            imageEditorView.classList.remove('hidden');

            const binaryData = atob(clipData.data);
            const bytes = new Uint8Array(binaryData.length);
            for (let i = 0; i < binaryData.length; i++) {
                bytes[i] = binaryData.charCodeAt(i);
            }
            const blob = new Blob([bytes], { type: contentType });

            // Initialize canvas via EditorCore
            EditorCore.initCanvas(
                document.getElementById('editor-canvas'),
                document.getElementById('editor-overlay-canvas')
            );
            await EditorCore.loadImage(blob, contentType);

            // Register tools
            EditorCore.registerTool('brush', BrushTool.createBrush());
            EditorCore.registerTool('eraser', BrushTool.createEraser());
            EditorCore.registerTool('line', ShapeTools.createLine());
            EditorCore.registerTool('rectangle', ShapeTools.createRectangle());
            EditorCore.registerTool('circle', ShapeTools.createCircle());
            EditorCore.registerTool('text', TextTool.create());

            EditorCore.attachListeners();
            EditorCore.selectTool('brush');
        } else {
            isTextEditor = true;
            imageEditorView.classList.add('hidden');
            textEditorView.classList.remove('hidden');
            document.getElementById('text-editor-textarea').value = clipData.data;
        }

        editorModal.removeAttribute('inert');
        editorModal.classList.add('active');
        resetEditorUI();

    } catch (error) {
        console.error('Error opening editor:', error);
        showToast('Failed to open editor.');
    }
}

function getNewFilename(original) {
    const lastDot = original.lastIndexOf('.');
    if (lastDot === -1) return original + '_edited';
    const name = original.substring(0, lastDot);
    const ext = original.substring(lastDot);
    return name + '_edited' + ext;
}

function closeEditor() {
    const editorModal = document.getElementById('editor-modal');
    editorModal.classList.remove('active');
    editorModal.setAttribute('inert', '');

    editorClipId = null;
    editorContentType = '';
    editorFilename = '';

    // Hide text input
    const textInput = document.getElementById('canvas-text-input');
    if (textInput) {
        textInput.style.display = 'none';
        textInput.value = '';
    }

    EditorCore.detachListeners();
    EditorCore.reset();
}

function resetEditorUI() {
    EditorCore.currentColor = '#44403c';
    EditorCore.currentOpacity = 1;
    EditorCore.brushSize = 8;

    document.getElementById('editor-color').value = EditorCore.currentColor;
    document.getElementById('editor-opacity').value = EditorCore.currentOpacity * 100;
    document.getElementById('editor-opacity-value').textContent = '100%';
    document.getElementById('editor-brush-size').value = EditorCore.brushSize;
    document.getElementById('editor-brush-size-value').textContent = EditorCore.brushSize + 'px';
}

async function saveEditorContent() {
    const filename = document.getElementById('editor-filename').value.trim();
    if (!filename) {
        showToast('Please enter a filename.');
        return;
    }

    let base64Data;
    let contentType = editorContentType;

    if (isTextEditor) {
        const text = document.getElementById('text-editor-textarea').value;
        base64Data = btoa(unescape(encodeURIComponent(text)));
    } else {
        // Preserve original format
        const origType = EditorCore.originalContentType;
        if (origType === 'image/jpeg' || origType === 'image/jpg') {
            const dataUrl = EditorCore.canvas.toDataURL('image/jpeg', 0.92);
            base64Data = dataUrl.split(',')[1];
            contentType = 'image/jpeg';
        } else if (origType === 'image/webp') {
            // Try WebP, fall back to PNG
            const dataUrl = EditorCore.canvas.toDataURL('image/webp', 0.92);
            if (dataUrl.startsWith('data:image/webp')) {
                base64Data = dataUrl.split(',')[1];
                contentType = 'image/webp';
            } else {
                const pngUrl = EditorCore.canvas.toDataURL('image/png');
                base64Data = pngUrl.split(',')[1];
                contentType = 'image/png';
            }
        } else {
            const dataUrl = EditorCore.canvas.toDataURL('image/png');
            base64Data = dataUrl.split(',')[1];
            contentType = 'image/png';
        }
    }

    const fileData = {
        name: filename,
        content_type: contentType,
        data: base64Data
    };

    try {
        await upload([fileData], 0);
        showToast('Saved as new clip!');
        closeEditor();
        loadClips();
    } catch (error) {
        console.error('Error saving:', error);
        showToast('Failed to save.');
    }
}

function setupEditorListeners() {
    document.getElementById('editor-close').addEventListener('click', closeEditor);
    document.getElementById('editor-save').addEventListener('click', saveEditorContent);

    // Tool buttons
    document.querySelectorAll('.editor-tool-btn').forEach(btn => {
        btn.addEventListener('click', () => EditorCore.selectTool(btn.dataset.tool));
    });

    // Color picker
    document.getElementById('editor-color').addEventListener('input', (e) => {
        EditorCore.currentColor = e.target.value;
    });

    // Opacity slider
    document.getElementById('editor-opacity').addEventListener('input', (e) => {
        EditorCore.currentOpacity = e.target.value / 100;
        document.getElementById('editor-opacity-value').textContent = e.target.value + '%';
    });

    // Brush size slider
    document.getElementById('editor-brush-size').addEventListener('input', (e) => {
        EditorCore.brushSize = parseInt(e.target.value);
        document.getElementById('editor-brush-size-value').textContent = EditorCore.brushSize + 'px';
    });

    // Undo/Redo buttons
    document.getElementById('editor-undo').addEventListener('click', () => EditorCore.undo());
    document.getElementById('editor-redo').addEventListener('click', () => EditorCore.redo());

    // Text input commit
    const textInput = document.getElementById('canvas-text-input');
    textInput.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') {
            e.preventDefault();
            TextTool.commitTextInput();
        } else if (e.key === 'Escape') {
            e.preventDefault();
            textInput.style.display = 'none';
            textInput.value = '';
        }
    });
    textInput.addEventListener('blur', () => {
        if (TextTool.isActive) {
            TextTool.commitTextInput();
        }
    });

    // Click outside to close
    document.getElementById('editor-modal').addEventListener('click', (e) => {
        if (e.target.id === 'editor-modal') closeEditor();
    });
}
```

**Step 5: Update `index.html` script loading order**

Replace the editor script tag at line 1465 with the new modules. Order matters — core first, then tools, then wrapper:

```html
    <script src="js/editor/editor-core.js"></script>
    <script src="js/editor/tool-brush.js"></script>
    <script src="js/editor/tool-shapes.js"></script>
    <script src="js/editor/tool-text.js"></script>
    <script src="js/editor.js"></script>
```

**Step 6: Run e2e tests to verify no regression**

Run: `cd e2e && npm test -- --grep "Image Editor" 2>&1 | tail -30`
Expected: All existing editor tests pass

**Step 7: Commit**

```bash
git add frontend/js/editor/ frontend/js/editor.js frontend/index.html
git commit -m "refactor(editor): extract tools into modular architecture"
```

---

### Task 3: ShortcutManager Integration

Add `'editor'` context to ShortcutManager. Register all editor shortcuts. Remove the ad-hoc keydown listener.

**Files:**
- Modify: `frontend/js/shortcuts.js` (lines 18, 19-27, 42-58, 246, 355-362)
- Modify: `frontend/js/editor.js` (remove keydown listener, which was already removed in Task 2)
- Modify: `frontend/js/app.js` (add editor shortcut registrations)

**Step 1: Add `'editor'` context to ShortcutManager**

In `frontend/js/shortcuts.js`:

1. Add `'editor'` to `CATEGORY_ORDER` (line 18):
```javascript
const CATEGORY_ORDER = ['navigation', 'gallery', 'clip', 'lightbox', 'editor', 'comparison', 'bulk', 'system'];
```

2. Add label to `CATEGORY_LABELS` (line 19-27):
```javascript
editor: 'Image Editor',
```

3. In `getActiveContexts()` (around line 53-54), change the editor modal check from returning `[]` to returning `['editor']`:
```javascript
if (editorModal && editorModal.classList.contains('active')) {
    contexts.push('editor');
    return contexts;
}
```

4. Add `'editor'` to priority order (line 246):
```javascript
const priority = ['clip', 'bulk', 'lightbox', 'editor', 'comparison', 'watch', 'gallery', 'global'];
```

5. Add `'editor'` to context hierarchy in `contextsOverlap` (line 355-362):
```javascript
const hierarchy = {
    global: ['gallery', 'lightbox', 'editor', 'comparison', 'watch', 'bulk', 'clip'],
    gallery: ['clip', 'bulk'],
};
```

**Step 2: Register editor shortcuts in `app.js`**

Add a new registration block in the `registerShortcuts()` function (or equivalent initialization section) in `app.js`:

```javascript
// --- Editor shortcuts ---
ShortcutManager.register({ id: 'editor.brush',     label: 'Brush tool',         category: 'editor', context: 'editor', defaultKey: 'b', callback: () => EditorCore.selectTool('brush') });
ShortcutManager.register({ id: 'editor.eraser',    label: 'Eraser tool',        category: 'editor', context: 'editor', defaultKey: 'e', callback: () => EditorCore.selectTool('eraser') });
ShortcutManager.register({ id: 'editor.line',      label: 'Line tool',          category: 'editor', context: 'editor', defaultKey: 'l', callback: () => EditorCore.selectTool('line') });
ShortcutManager.register({ id: 'editor.rectangle', label: 'Rectangle tool',     category: 'editor', context: 'editor', defaultKey: 'u', callback: () => EditorCore.selectTool('rectangle') });
ShortcutManager.register({ id: 'editor.circle',    label: 'Circle tool',        category: 'editor', context: 'editor', defaultKey: 'o', callback: () => EditorCore.selectTool('circle') });
ShortcutManager.register({ id: 'editor.text',      label: 'Text tool',          category: 'editor', context: 'editor', defaultKey: 't', callback: () => EditorCore.selectTool('text') });
ShortcutManager.register({ id: 'editor.undo',      label: 'Undo',              category: 'editor', context: 'editor', defaultKey: 'mod+z', callback: () => EditorCore.undo() });
ShortcutManager.register({ id: 'editor.redo',      label: 'Redo',              category: 'editor', context: 'editor', defaultKey: 'mod+shift+z', callback: () => EditorCore.redo() });
ShortcutManager.register({ id: 'editor.save',      label: 'Save as new clip',  category: 'editor', context: 'editor', defaultKey: 'mod+s', callback: () => saveEditorContent() });
ShortcutManager.register({ id: 'editor.close',     label: 'Close editor',      category: 'editor', context: 'editor', defaultKey: 'Escape', callback: () => closeEditor() });
```

Note: Additional shortcuts for new tools (crop, select, anonymize, etc.) will be registered in their respective tasks.

**Step 3: Run e2e tests — keyboard shortcuts should still work**

Run: `cd e2e && npm test -- --grep "Keyboard Shortcuts" 2>&1 | tail -20`
Expected: Existing keyboard shortcut tests pass via ShortcutManager dispatch

**Step 4: Commit**

```bash
git add frontend/js/shortcuts.js frontend/js/app.js
git commit -m "feat(editor): integrate shortcuts with ShortcutManager"
```

---

### Task 4: Zoom & Pan

Implement CSS-transform-based zoom and space+drag pan.

**Files:**
- Create: `frontend/js/editor/tool-zoom.js`
- Modify: `frontend/js/editor/editor-core.js` (zoom wheel handler, space key handling)
- Modify: `frontend/css/modals.css` (canvas container overflow change)
- Modify: `frontend/index.html` (zoom toolbar controls, script tag)
- Modify: `frontend/js/editor.js` (wire zoom UI)
- Modify: `frontend/js/app.js` (register zoom shortcuts)

**Step 1: Update canvas container CSS**

In `frontend/css/modals.css`, change `.editor-canvas-container` (line 684-694):

```css
.editor-canvas-container {
    min-height: 0;
    min-width: 0;
    background: #292524;
    display: flex;
    align-items: center;
    justify-content: center;
    overflow: hidden;  /* Changed from auto to hidden for zoom */
    position: relative;
    padding: 1.5rem;
}
```

**Step 2: Create `tool-zoom.js`**

```javascript
// frontend/js/editor/tool-zoom.js
// Zoom/pan controls and wheel handler

const ZoomTool = (() => {
    const MIN_ZOOM = 0.1;   // 10%
    const MAX_ZOOM = 8.0;   // 800%
    const ZOOM_STEP = 0.1;

    function clampZoom(z) {
        return Math.max(MIN_ZOOM, Math.min(MAX_ZOOM, z));
    }

    function zoomIn() {
        EditorCore.zoomLevel = clampZoom(EditorCore.zoomLevel + ZOOM_STEP);
        EditorCore.applyTransform();
        updateZoomDisplay();
    }

    function zoomOut() {
        EditorCore.zoomLevel = clampZoom(EditorCore.zoomLevel - ZOOM_STEP);
        EditorCore.applyTransform();
        updateZoomDisplay();
    }

    function zoomToFit() {
        const canvas = EditorCore.canvas;
        if (!canvas) return;
        const container = canvas.parentElement;
        if (!container) return;

        const containerRect = container.getBoundingClientRect();
        const padding = 48; // 1.5rem * 2
        const availW = containerRect.width - padding;
        const availH = containerRect.height - padding;

        const fitZoom = Math.min(availW / canvas.width, availH / canvas.height, 1);
        EditorCore.zoomLevel = clampZoom(fitZoom);
        EditorCore.panX = 0;
        EditorCore.panY = 0;
        EditorCore.applyTransform();
        updateZoomDisplay();
    }

    function zoomTo100() {
        EditorCore.zoomLevel = 1;
        EditorCore.panX = 0;
        EditorCore.panY = 0;
        EditorCore.applyTransform();
        updateZoomDisplay();
    }

    function handleWheel(e) {
        e.preventDefault();
        const delta = -e.deltaY * 0.001;
        EditorCore.zoomLevel = clampZoom(EditorCore.zoomLevel + delta);
        EditorCore.applyTransform();
        updateZoomDisplay();
    }

    function updateZoomDisplay() {
        const display = document.getElementById('editor-zoom-display');
        if (display) {
            display.textContent = Math.round(EditorCore.zoomLevel * 100) + '%';
        }
    }

    function attachWheelListener() {
        const container = document.querySelector('.editor-canvas-container');
        if (container) {
            container.addEventListener('wheel', handleWheel, { passive: false });
        }
    }

    function detachWheelListener() {
        const container = document.querySelector('.editor-canvas-container');
        if (container) {
            container.removeEventListener('wheel', handleWheel);
        }
    }

    return {
        zoomIn,
        zoomOut,
        zoomToFit,
        zoomTo100,
        updateZoomDisplay,
        attachWheelListener,
        detachWheelListener,
    };
})();
```

**Step 3: Add space key handling for pan**

In `editor.js`, add space key listeners in `openEditor()` (after `EditorCore.attachListeners()`):

```javascript
// Space key for pan
document.addEventListener('keydown', editorSpaceDown);
document.addEventListener('keyup', editorSpaceUp);
```

Add these functions to `editor.js`:

```javascript
function editorSpaceDown(e) {
    if (e.code === 'Space' && !EditorCore.spaceHeld) {
        const editorModal = document.getElementById('editor-modal');
        if (!editorModal.classList.contains('active')) return;
        // Don't intercept space in text inputs
        if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA') return;
        e.preventDefault();
        EditorCore.spaceHeld = true;
        EditorCore.canvas.style.cursor = 'grab';
    }
}

function editorSpaceUp(e) {
    if (e.code === 'Space') {
        EditorCore.spaceHeld = false;
        if (EditorCore.canvas) {
            EditorCore.canvas.style.cursor = EditorCore.activeTool?.getCursor?.() || 'crosshair';
        }
    }
}
```

In `closeEditor()`, remove space listeners:

```javascript
document.removeEventListener('keydown', editorSpaceDown);
document.removeEventListener('keyup', editorSpaceUp);
```

**Step 4: Add zoom controls to toolbar HTML**

In `frontend/index.html`, add zoom controls after the undo/redo buttons (after line 835):

```html
<div class="h-5 w-px bg-stone-300"></div>

<div class="flex items-center gap-1">
    <button id="editor-zoom-fit" class="p-1.5 bg-stone-100 hover:bg-stone-200 rounded-md transition-colors text-[10px] font-medium text-stone-600"
        data-tooltip="Zoom to fit (Ctrl+0)">Fit</button>
    <button id="editor-zoom-100" class="p-1.5 bg-stone-100 hover:bg-stone-200 rounded-md transition-colors text-[10px] font-medium text-stone-600"
        data-tooltip="Zoom to 100% (Ctrl+1)">100%</button>
    <button id="editor-zoom-out" class="p-1.5 bg-stone-100 hover:bg-stone-200 rounded-md transition-colors"
        data-tooltip="Zoom out (Ctrl+-)">
        <svg class="w-3.5 h-3.5 text-stone-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M20 12H4"></path>
        </svg>
    </button>
    <span id="editor-zoom-display" class="text-[10px] font-medium text-stone-600 w-8 text-center">100%</span>
    <button id="editor-zoom-in" class="p-1.5 bg-stone-100 hover:bg-stone-200 rounded-md transition-colors"
        data-tooltip="Zoom in (Ctrl+=)">
        <svg class="w-3.5 h-3.5 text-stone-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M12 4v16m8-8H4"></path>
        </svg>
    </button>
</div>
```

**Step 5: Wire zoom buttons and listeners in `editor.js`**

In `setupEditorListeners()`, add:

```javascript
// Zoom controls
document.getElementById('editor-zoom-fit').addEventListener('click', () => ZoomTool.zoomToFit());
document.getElementById('editor-zoom-100').addEventListener('click', () => ZoomTool.zoomTo100());
document.getElementById('editor-zoom-in').addEventListener('click', () => ZoomTool.zoomIn());
document.getElementById('editor-zoom-out').addEventListener('click', () => ZoomTool.zoomOut());
```

In `openEditor()` (after `EditorCore.attachListeners()`), add:

```javascript
ZoomTool.attachWheelListener();
```

In `closeEditor()`, add:

```javascript
ZoomTool.detachWheelListener();
```

**Step 6: Add `tool-zoom.js` script tag to `index.html`**

Add before `editor.js`:
```html
<script src="js/editor/tool-zoom.js"></script>
```

**Step 7: Register zoom shortcuts in `app.js`**

```javascript
ShortcutManager.register({ id: 'editor.zoom_in',   label: 'Zoom in',           category: 'editor', context: 'editor', defaultKey: 'mod+=', callback: () => ZoomTool.zoomIn() });
ShortcutManager.register({ id: 'editor.zoom_out',  label: 'Zoom out',          category: 'editor', context: 'editor', defaultKey: 'mod+-', callback: () => ZoomTool.zoomOut() });
ShortcutManager.register({ id: 'editor.zoom_fit',  label: 'Zoom to fit',       category: 'editor', context: 'editor', defaultKey: 'mod+0', callback: () => ZoomTool.zoomToFit() });
ShortcutManager.register({ id: 'editor.zoom_100',  label: 'Zoom to 100%',      category: 'editor', context: 'editor', defaultKey: 'mod+1', callback: () => ZoomTool.zoomTo100() });
```

**Step 8: Run e2e tests**

Run: `cd e2e && npm test -- --grep "Image Editor" 2>&1 | tail -30`
Expected: All tests pass

**Step 9: Commit**

```bash
git add frontend/js/editor/ frontend/js/editor.js frontend/js/app.js frontend/css/modals.css frontend/index.html
git commit -m "feat(editor): add zoom/pan with scroll wheel and space+drag"
```

---

### Task 5: Toolbar UI Overhaul

Restructure the toolbar into grouped two-row layout with contextual sub-toolbars. Add new tool buttons (initially non-functional — tools implemented in subsequent tasks).

**Files:**
- Modify: `frontend/index.html` (editor toolbar section, lines 757-836)
- Modify: `frontend/css/modals.css` (toolbar styling)
- Modify: `frontend/js/editor.js` (wire new buttons)
- Modify: `frontend/js/editor/editor-core.js` (handle contextual toolbar visibility)

**Step 1: Replace toolbar HTML**

Replace the entire `<div class="editor-toolbar">` section (lines 757-836 in `index.html`) with the new two-row layout. This includes all existing tool buttons plus placeholders for new tools (crop, select, arrow, anonymize, eyedropper, rotate/flip). Use `data-tool` attributes matching the tool names from the design.

Key structure:
- Row 1: Tool buttons grouped with separators (select, crop | brush, eraser | line, arrow, rect, circle | text, anonymize | eyedropper)
- Row 2: Properties (color, opacity, size | undo, redo | rotate CW, rotate CCW, flip H, flip V | zoom controls)
- Contextual row: Hidden by default, shown per-tool (crop presets, anonymize mode, font size)

All new buttons use `data-tool` attribute and follow existing icon style (`w-4 h-4`, `stroke="currentColor"`, `stroke-width="1.5"`).

**Step 2: Add contextual toolbar CSS**

```css
.editor-context-toolbar {
    padding: 0.375rem 1rem;
    background: #f5f5f4;
    border-bottom: 1px solid #e7e5e4;
    display: none;
    flex-wrap: wrap;
    gap: 0.75rem;
    align-items: center;
}

.editor-context-toolbar.active {
    display: flex;
}
```

**Step 3: Wire toolbar buttons in `editor.js`**

New tool buttons dispatch to `EditorCore.selectTool()`. Rotate/flip buttons call functions directly (implemented in Task 8). Contextual toolbar visibility toggled in a new `updateContextToolbar()` function called from `EditorCore.selectTool()`.

**Step 4: Update `updateToolButtons()` in `editor-core.js`**

When a tool is selected, show/hide the relevant contextual toolbar section:
- Crop selected → show crop presets row
- Anonymize selected → show anonymize mode/effect toggles
- Text selected → show font size input (hide brush size)

**Step 5: Update e2e selectors**

In `e2e/helpers/selectors.ts`, add new tool selectors to the `editor.tools` object:

```typescript
tools: {
    select: '[data-tool="select"]',
    crop: '[data-tool="crop"]',
    brush: '[data-tool="brush"]',
    eraser: '[data-tool="eraser"]',
    line: '[data-tool="line"]',
    arrow: '[data-tool="arrow"]',
    rectangle: '[data-tool="rectangle"]',
    circle: '[data-tool="circle"]',
    text: '[data-tool="text"]',
    anonymize: '[data-tool="anonymize"]',
    eyedropper: '[data-tool="eyedropper"]',
},
```

Also add zoom selectors:

```typescript
zoomIn: '#editor-zoom-in',
zoomOut: '#editor-zoom-out',
zoomFit: '#editor-zoom-fit',
zoom100: '#editor-zoom-100',
zoomDisplay: '#editor-zoom-display',
```

**Step 6: Update `selectTool()` type in AppHelper**

In `e2e/fixtures/test-fixtures.ts`, update the `selectTool` method signature:

```typescript
async selectTool(tool: 'select' | 'crop' | 'brush' | 'eraser' | 'line' | 'arrow' | 'rectangle' | 'circle' | 'text' | 'anonymize' | 'eyedropper'): Promise<void> {
```

**Step 7: Run e2e tests**

Run: `cd e2e && npm test -- --grep "Image Editor" 2>&1 | tail -30`
Expected: All tests pass (new buttons exist but existing tool selection still works)

**Step 8: Commit**

```bash
git add frontend/index.html frontend/css/modals.css frontend/js/editor.js frontend/js/editor/editor-core.js e2e/helpers/selectors.ts e2e/fixtures/test-fixtures.ts
git commit -m "feat(editor): add grouped two-row toolbar with all tool buttons"
```

---

### Task 6: Arrow Tool

Extend shape tools with arrowhead drawing.

**Files:**
- Create: `frontend/js/editor/tool-arrow.js`
- Modify: `frontend/index.html` (add script tag)
- Modify: `frontend/js/editor.js` (register arrow tool)
- Modify: `frontend/js/app.js` (register shortcut)

**Step 1: Create `tool-arrow.js`**

```javascript
// frontend/js/editor/tool-arrow.js
// Arrow tool — line with arrowhead

const ArrowTool = (() => {
    function drawArrowhead(ctx, x1, y1, x2, y2, size) {
        const angle = Math.atan2(y2 - y1, x2 - x1);
        const headLen = Math.max(size * 2, 10);

        ctx.beginPath();
        ctx.moveTo(x2, y2);
        ctx.lineTo(
            x2 - headLen * Math.cos(angle - Math.PI / 6),
            y2 - headLen * Math.sin(angle - Math.PI / 6)
        );
        ctx.moveTo(x2, y2);
        ctx.lineTo(
            x2 - headLen * Math.cos(angle + Math.PI / 6),
            y2 - headLen * Math.sin(angle + Math.PI / 6)
        );
        ctx.stroke();
    }

    function create() {
        return {
            activate() {},
            deactivate() {},
            getCursor() { return 'crosshair'; },

            onMouseDown(coords, e) {},

            onMouseMove(coords, e) {
                const oc = EditorCore.overlayCtx;
                const ov = EditorCore.overlayCanvas;
                oc.clearRect(0, 0, ov.width, ov.height);
                oc.globalAlpha = EditorCore.currentOpacity * 0.5;
                oc.strokeStyle = EditorCore.currentColor;
                oc.lineWidth = EditorCore.brushSize;
                oc.lineCap = 'round';
                oc.setLineDash([5, 5]);

                let ex = coords.x, ey = coords.y;
                if (e.shiftKey) {
                    const s = ShapeTools.snapTo45(EditorCore.startX, EditorCore.startY, coords.x, coords.y);
                    ex = s.x; ey = s.y;
                }

                oc.beginPath();
                oc.moveTo(EditorCore.startX, EditorCore.startY);
                oc.lineTo(ex, ey);
                oc.stroke();
                oc.setLineDash([]);
                drawArrowhead(oc, EditorCore.startX, EditorCore.startY, ex, ey, EditorCore.brushSize);
                oc.globalAlpha = 1;
            },

            onMouseUp(coords, e) {
                const oc = EditorCore.overlayCtx;
                oc.clearRect(0, 0, EditorCore.overlayCanvas.width, EditorCore.overlayCanvas.height);

                const ctx = EditorCore.ctx;
                ctx.globalAlpha = EditorCore.currentOpacity;
                ctx.strokeStyle = EditorCore.currentColor;
                ctx.lineWidth = EditorCore.brushSize;
                ctx.lineCap = 'round';

                let ex = coords.x, ey = coords.y;
                if (e.shiftKey) {
                    const s = ShapeTools.snapTo45(EditorCore.startX, EditorCore.startY, coords.x, coords.y);
                    ex = s.x; ey = s.y;
                }

                ctx.beginPath();
                ctx.moveTo(EditorCore.startX, EditorCore.startY);
                ctx.lineTo(ex, ey);
                ctx.stroke();
                drawArrowhead(ctx, EditorCore.startX, EditorCore.startY, ex, ey, EditorCore.brushSize);
                ctx.globalAlpha = 1;
                EditorCore.saveUndoState();
            },
        };
    }

    return { create };
})();
```

**Step 2: Register in `editor.js` `openEditor()`**

```javascript
EditorCore.registerTool('arrow', ArrowTool.create());
```

**Step 3: Add script tag and shortcut**

Script tag in `index.html` before `editor.js`:
```html
<script src="js/editor/tool-arrow.js"></script>
```

Shortcut in `app.js`:
```javascript
ShortcutManager.register({ id: 'editor.arrow', label: 'Arrow tool', category: 'editor', context: 'editor', defaultKey: 'w', callback: () => EditorCore.selectTool('arrow') });
```

**Step 4: Run tests and commit**

Run: `cd e2e && npm test -- --grep "Image Editor" 2>&1 | tail -30`
Expected: Pass

```bash
git add frontend/js/editor/tool-arrow.js frontend/js/editor.js frontend/index.html frontend/js/app.js
git commit -m "feat(editor): add arrow drawing tool"
```

---

### Task 7: Eyedropper & Rotate/Flip

Add color picking and whole-image rotation/flip operations.

**Files:**
- Create: `frontend/js/editor/tool-eyedropper.js`
- Create: `frontend/js/editor/tool-transform.js`
- Modify: `frontend/index.html` (script tags)
- Modify: `frontend/js/editor.js` (register tools, wire rotate/flip buttons)
- Modify: `frontend/js/app.js` (register shortcuts)

**Step 1: Create `tool-eyedropper.js`**

```javascript
// frontend/js/editor/tool-eyedropper.js
// Color picker — samples pixel color, then switches back to previous tool

const EyedropperTool = (() => {
    let previousTool = 'brush';

    function create() {
        return {
            activate() {
                previousTool = EditorCore.activeToolName || 'brush';
            },
            deactivate() {},
            getCursor() { return 'crosshair'; },

            onMouseDown(coords, e) {
                const ctx = EditorCore.ctx;
                const pixel = ctx.getImageData(Math.round(coords.x), Math.round(coords.y), 1, 1).data;
                const hex = '#' + [pixel[0], pixel[1], pixel[2]].map(v => v.toString(16).padStart(2, '0')).join('');
                EditorCore.currentColor = hex;
                document.getElementById('editor-color').value = hex;

                // Switch back to previous tool
                EditorCore.selectTool(previousTool);
            },

            onMouseMove(coords, e) {},
            onMouseUp(coords, e) {},
        };
    }

    return { create };
})();
```

**Step 2: Create `tool-transform.js`**

```javascript
// frontend/js/editor/tool-transform.js
// Whole-image rotate and flip operations

const TransformTool = (() => {
    function rotateCW() {
        const canvas = EditorCore.canvas;
        const ctx = EditorCore.ctx;
        const imageData = ctx.getImageData(0, 0, canvas.width, canvas.height);

        // Create temp canvas with swapped dimensions
        const tmp = document.createElement('canvas');
        tmp.width = canvas.height;
        tmp.height = canvas.width;
        const tmpCtx = tmp.getContext('2d');

        tmpCtx.translate(tmp.width, 0);
        tmpCtx.rotate(Math.PI / 2);
        tmpCtx.drawImage(canvas, 0, 0);

        // Resize main canvas
        canvas.width = tmp.width;
        canvas.height = tmp.height;
        EditorCore.overlayCanvas.width = tmp.width;
        EditorCore.overlayCanvas.height = tmp.height;
        ctx.drawImage(tmp, 0, 0);

        EditorCore.syncOverlay();
        EditorCore.saveUndoState();
    }

    function rotateCCW() {
        const canvas = EditorCore.canvas;
        const ctx = EditorCore.ctx;

        const tmp = document.createElement('canvas');
        tmp.width = canvas.height;
        tmp.height = canvas.width;
        const tmpCtx = tmp.getContext('2d');

        tmpCtx.translate(0, tmp.height);
        tmpCtx.rotate(-Math.PI / 2);
        tmpCtx.drawImage(canvas, 0, 0);

        canvas.width = tmp.width;
        canvas.height = tmp.height;
        EditorCore.overlayCanvas.width = tmp.width;
        EditorCore.overlayCanvas.height = tmp.height;
        ctx.drawImage(tmp, 0, 0);

        EditorCore.syncOverlay();
        EditorCore.saveUndoState();
    }

    function flipH() {
        const canvas = EditorCore.canvas;
        const ctx = EditorCore.ctx;

        const tmp = document.createElement('canvas');
        tmp.width = canvas.width;
        tmp.height = canvas.height;
        const tmpCtx = tmp.getContext('2d');

        tmpCtx.translate(tmp.width, 0);
        tmpCtx.scale(-1, 1);
        tmpCtx.drawImage(canvas, 0, 0);

        ctx.clearRect(0, 0, canvas.width, canvas.height);
        ctx.drawImage(tmp, 0, 0);

        EditorCore.saveUndoState();
    }

    function flipV() {
        const canvas = EditorCore.canvas;
        const ctx = EditorCore.ctx;

        const tmp = document.createElement('canvas');
        tmp.width = canvas.width;
        tmp.height = canvas.height;
        const tmpCtx = tmp.getContext('2d');

        tmpCtx.translate(0, tmp.height);
        tmpCtx.scale(1, -1);
        tmpCtx.drawImage(canvas, 0, 0);

        ctx.clearRect(0, 0, canvas.width, canvas.height);
        ctx.drawImage(tmp, 0, 0);

        EditorCore.saveUndoState();
    }

    return { rotateCW, rotateCCW, flipH, flipV };
})();
```

**Step 3: Register eyedropper in `editor.js`, wire rotate/flip buttons**

In `openEditor()`:
```javascript
EditorCore.registerTool('eyedropper', EyedropperTool.create());
```

In `setupEditorListeners()`:
```javascript
document.getElementById('editor-rotate-cw')?.addEventListener('click', () => TransformTool.rotateCW());
document.getElementById('editor-rotate-ccw')?.addEventListener('click', () => TransformTool.rotateCCW());
document.getElementById('editor-flip-h')?.addEventListener('click', () => TransformTool.flipH());
document.getElementById('editor-flip-v')?.addEventListener('click', () => TransformTool.flipV());
```

**Step 4: Add script tags, shortcuts**

Script tags before `editor.js`:
```html
<script src="js/editor/tool-eyedropper.js"></script>
<script src="js/editor/tool-transform.js"></script>
```

Shortcuts in `app.js`:
```javascript
ShortcutManager.register({ id: 'editor.eyedropper',  label: 'Eyedropper',        category: 'editor', context: 'editor', defaultKey: 'i', callback: () => EditorCore.selectTool('eyedropper') });
ShortcutManager.register({ id: 'editor.rotate_cw',   label: 'Rotate 90° CW',     category: 'editor', context: 'editor', defaultKey: 'r', callback: () => TransformTool.rotateCW() });
ShortcutManager.register({ id: 'editor.rotate_ccw',  label: 'Rotate 90° CCW',     category: 'editor', context: 'editor', defaultKey: 'shift+r', callback: () => TransformTool.rotateCCW() });
```

**Step 5: Run tests and commit**

Run: `cd e2e && npm test -- --grep "Image Editor" 2>&1 | tail -30`

```bash
git add frontend/js/editor/tool-eyedropper.js frontend/js/editor/tool-transform.js frontend/js/editor.js frontend/index.html frontend/js/app.js
git commit -m "feat(editor): add eyedropper, rotate, and flip tools"
```

---

### Task 8: Crop Tool

Full crop tool with freeform selection, aspect ratio presets, and rotation/straightening.

**Files:**
- Create: `frontend/js/editor/tool-crop.js`
- Modify: `frontend/index.html` (script tag, contextual toolbar for presets)
- Modify: `frontend/js/editor.js` (register crop tool)
- Modify: `frontend/js/app.js` (register shortcuts)

**Step 1: Create `tool-crop.js`**

This is the most complex tool. Key components:

1. **Crop overlay** — dark mask with transparent crop region drawn on overlay canvas
2. **Handles** — 8 handles (4 corners + 4 edge midpoints) for resizing
3. **Drag interactions** — inside region = move, handles = resize, outside = new selection
4. **Aspect ratio** — constrain drag/resize when a preset is active
5. **Rotation** — slider that rotates the underlying image within the crop frame
6. **Confirm/cancel** — Enter to crop, Escape to cancel

The tool maintains its own state (crop rect, active handle, aspect ratio, rotation angle) and renders everything on the overlay canvas.

The crop execution: creates an offscreen canvas at crop dimensions, draws the rotated image onto it with the crop offset, then replaces the main canvas content and dimensions.

Key methods:
- `activate()`: Initialize crop rect to center 80% of canvas, render overlay
- `deactivate()`: Clear overlay, reset state
- `onMouseDown()`: Hit-test handles, interior, or start new selection
- `onMouseMove()`: Resize, move, or create new crop region
- `onMouseUp()`: Finalize drag
- `renderOverlay()`: Draw dark mask + crop region + handles + grid + dimensions
- `confirm()`: Execute the crop, update canvas dimensions, save undo state
- `cancel()`: Restore original state, deactivate

Implementation is ~300 lines. Write the full implementation, not a stub.

**Step 2: Add crop contextual toolbar to HTML**

Inside the contextual toolbar area (below the main toolbar):

```html
<div id="editor-crop-options" class="editor-context-toolbar">
    <div class="flex items-center gap-1.5">
        <label class="text-[10px] font-medium text-stone-500 uppercase tracking-wide">Ratio</label>
        <select id="editor-crop-ratio" class="text-[10px] font-medium text-stone-600 bg-stone-100 border border-stone-200 rounded px-1.5 py-1">
            <option value="free">Free</option>
            <option value="1:1">1:1</option>
            <option value="4:3">4:3</option>
            <option value="3:2">3:2</option>
            <option value="16:9">16:9</option>
        </select>
        <button id="editor-crop-swap" class="p-1 bg-stone-100 hover:bg-stone-200 rounded transition-colors" data-tooltip="Swap orientation">
            <svg class="w-3.5 h-3.5 text-stone-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M7 16V4m0 0L3 8m4-4l4 4m6 0v12m0 0l4-4m-4 4l-4-4"></path>
            </svg>
        </button>
    </div>
    <div class="h-5 w-px bg-stone-300"></div>
    <div class="flex items-center gap-1.5">
        <label class="text-[10px] font-medium text-stone-500 uppercase tracking-wide">Straighten</label>
        <input type="range" id="editor-crop-rotate" min="-45" max="45" value="0" step="0.5" class="w-24 accent-stone-600">
        <span id="editor-crop-rotate-value" class="text-[10px] font-medium text-stone-600 w-8">0°</span>
    </div>
    <div class="h-5 w-px bg-stone-300"></div>
    <div class="flex items-center gap-1">
        <button id="editor-crop-confirm" class="px-2.5 py-1 bg-stone-800 hover:bg-stone-700 text-white text-[10px] font-medium rounded-md transition-colors">Apply</button>
        <button id="editor-crop-cancel" class="px-2.5 py-1 bg-stone-100 hover:bg-stone-200 text-stone-600 text-[10px] font-medium rounded-md transition-colors">Cancel</button>
    </div>
</div>
```

**Step 3: Register, add script, wire contextual toolbar**

Register in `editor.js`, shortcut `c` in `app.js`. Show/hide `#editor-crop-options` when crop tool is active/inactive (in `updateContextToolbar()`).

**Step 4: Run tests and commit**

```bash
git add frontend/js/editor/tool-crop.js frontend/js/editor.js frontend/index.html frontend/js/app.js
git commit -m "feat(editor): add crop tool with presets and straighten"
```

---

### Task 9: Selection Tool

Rectangular pixel selection with move, resize, copy/paste.

**Files:**
- Create: `frontend/js/editor/tool-select.js`
- Modify: `frontend/index.html` (script tag)
- Modify: `frontend/js/editor.js` (register)
- Modify: `frontend/js/app.js` (register shortcuts)

**Step 1: Create `tool-select.js`**

Key state:
- `selectionRect` — {x, y, w, h} of the selection
- `selectionImageData` — captured pixels from `getImageData()`
- `activeHandle` — which handle is being dragged (null, 'nw', 'n', 'ne', 'e', 'se', 's', 'sw', 'w', 'move')
- `clipboard` — internal copy of ImageData for paste
- `marchingAntsOffset` — animation counter for dashed border

Key behaviors:
- **Creating selection**: Drag on canvas creates a rectangle. On mouseup, capture pixels with `getImageData()`, clear the region on main canvas (fill white), draw captured pixels on overlay at same position.
- **Moving**: Drag inside selection → update overlay position. Track offset from original.
- **Resizing**: Drag handles → scale the selection ImageData. Use an offscreen canvas to scale.
- **Copy/paste**: Ctrl+C stores ImageData in clipboard. Ctrl+V creates a new floating selection from clipboard at center.
- **Delete**: Fill selection with white, commit.
- **Commit**: When clicking outside selection or pressing Enter, draw overlay content onto main canvas at final position, save undo state.
- **Cancel**: Escape restores original pixels, clears selection.
- **Marching ants**: `requestAnimationFrame` loop that redraws the selection border with animated dash offset.

Handles: Same visual style as crop — white squares with stone-800 border.

Keyboard modifiers:
- Shift while creating: constrain to square
- Alt while moving: duplicate (put original back, move copy)
- Ctrl+A: select all

**Step 2: Register in `editor.js`, add shortcuts**

Shortcuts in `app.js`:
```javascript
ShortcutManager.register({ id: 'editor.select',      label: 'Select tool',      category: 'editor', context: 'editor', defaultKey: 'v', callback: () => EditorCore.selectTool('select') });
ShortcutManager.register({ id: 'editor.select_all',  label: 'Select all',       category: 'editor', context: 'editor', defaultKey: 'mod+a', callback: () => { /* select all via SelectTool */ } });
ShortcutManager.register({ id: 'editor.copy',        label: 'Copy selection',   category: 'editor', context: 'editor', defaultKey: 'mod+c', callback: () => { /* copy via SelectTool */ } });
ShortcutManager.register({ id: 'editor.paste',       label: 'Paste selection',  category: 'editor', context: 'editor', defaultKey: 'mod+v', callback: () => { /* paste via SelectTool */ } });
ShortcutManager.register({ id: 'editor.delete_sel',  label: 'Delete selection', category: 'editor', context: 'editor', defaultKey: 'Delete', callback: () => { /* delete via SelectTool */ } });
```

**Step 3: Run tests and commit**

```bash
git add frontend/js/editor/tool-select.js frontend/js/editor.js frontend/index.html frontend/js/app.js
git commit -m "feat(editor): add selection tool with move, resize, copy/paste"
```

---

### Task 10: Anonymize Tool

Pixelate and blur tool with brush and rectangle modes.

**Files:**
- Create: `frontend/js/editor/tool-anonymize.js`
- Modify: `frontend/index.html` (script tag, contextual toolbar)
- Modify: `frontend/js/editor.js` (register)
- Modify: `frontend/js/app.js` (register shortcuts)

**Step 1: Create `tool-anonymize.js`**

Two modes, two effects:
- **Modes**: brush (paint to anonymize) or rectangle (drag to anonymize region)
- **Effects**: pixelate or blur

Pixelate implementation:
```javascript
function pixelateRegion(ctx, x, y, w, h, blockSize) {
    const imageData = ctx.getImageData(x, y, w, h);
    const data = imageData.data;

    for (let by = 0; by < h; by += blockSize) {
        for (let bx = 0; bx < w; bx += blockSize) {
            // Average the block
            let r = 0, g = 0, b = 0, count = 0;
            for (let dy = 0; dy < blockSize && by + dy < h; dy++) {
                for (let dx = 0; dx < blockSize && bx + dx < w; dx++) {
                    const i = ((by + dy) * w + (bx + dx)) * 4;
                    r += data[i]; g += data[i+1]; b += data[i+2]; count++;
                }
            }
            r = Math.round(r / count); g = Math.round(g / count); b = Math.round(b / count);
            // Fill the block
            for (let dy = 0; dy < blockSize && by + dy < h; dy++) {
                for (let dx = 0; dx < blockSize && bx + dx < w; dx++) {
                    const i = ((by + dy) * w + (bx + dx)) * 4;
                    data[i] = r; data[i+1] = g; data[i+2] = b;
                }
            }
        }
    }
    ctx.putImageData(imageData, x, y);
}
```

Blur implementation:
```javascript
function blurRegion(ctx, x, y, w, h, radius) {
    // Use ctx.filter for efficient blur
    const tmp = document.createElement('canvas');
    tmp.width = w;
    tmp.height = h;
    const tmpCtx = tmp.getContext('2d');
    tmpCtx.drawImage(ctx.canvas, x, y, w, h, 0, 0, w, h);

    const blurred = document.createElement('canvas');
    blurred.width = w;
    blurred.height = h;
    const blurCtx = blurred.getContext('2d');
    blurCtx.filter = `blur(${radius}px)`;
    blurCtx.drawImage(tmp, 0, 0);

    ctx.drawImage(blurred, x, y);
}
```

Brush mode: On mousemove while drawing, apply effect to a square region around cursor (size = brushSize). Track previously affected coordinates to avoid re-processing.

Rectangle mode: On mouseup, apply effect to the entire dragged rectangle. Show preview on overlay during drag.

**Step 2: Add contextual toolbar**

```html
<div id="editor-anonymize-options" class="editor-context-toolbar">
    <div class="flex items-center gap-1.5">
        <label class="text-[10px] font-medium text-stone-500 uppercase tracking-wide">Mode</label>
        <button id="editor-anon-brush" class="px-2 py-1 bg-stone-800 text-white text-[10px] font-medium rounded-md transition-colors">Brush</button>
        <button id="editor-anon-rect" class="px-2 py-1 bg-stone-100 text-stone-600 text-[10px] font-medium rounded-md transition-colors">Rectangle</button>
    </div>
    <div class="h-5 w-px bg-stone-300"></div>
    <div class="flex items-center gap-1.5">
        <label class="text-[10px] font-medium text-stone-500 uppercase tracking-wide">Effect</label>
        <button id="editor-anon-pixelate" class="px-2 py-1 bg-stone-800 text-white text-[10px] font-medium rounded-md transition-colors">Pixelate</button>
        <button id="editor-anon-blur" class="px-2 py-1 bg-stone-100 text-stone-600 text-[10px] font-medium rounded-md transition-colors">Blur</button>
    </div>
</div>
```

**Step 3: Register, shortcuts, commit**

Shortcut: `x` for anonymize tool.

```bash
git add frontend/js/editor/tool-anonymize.js frontend/js/editor.js frontend/index.html frontend/js/app.js
git commit -m "feat(editor): add anonymize tool with pixelate and blur"
```

---

### Task 11: Text Tool Enhancement

Replace brush-size-based font sizing with dedicated font size control.

**Files:**
- Modify: `frontend/js/editor/tool-text.js` (use `EditorCore.fontSize`)
- Modify: `frontend/index.html` (contextual toolbar with font size)
- Modify: `frontend/js/editor.js` (wire font size input)
- Modify: `frontend/js/editor/editor-core.js` (show/hide brush size vs font size)

**Step 1: Add font size contextual toolbar**

```html
<div id="editor-text-options" class="editor-context-toolbar">
    <div class="flex items-center gap-1.5">
        <label class="text-[10px] font-medium text-stone-500 uppercase tracking-wide">Font Size</label>
        <input type="number" id="editor-font-size" min="8" max="200" value="24"
            class="w-14 text-[10px] font-medium text-stone-600 bg-stone-100 border border-stone-200 rounded px-1.5 py-1 text-center">
        <span class="text-[10px] text-stone-400">px</span>
    </div>
</div>
```

**Step 2: Wire in `editor.js`**

In `setupEditorListeners()`:
```javascript
document.getElementById('editor-font-size')?.addEventListener('input', (e) => {
    EditorCore.fontSize = parseInt(e.target.value) || 24;
});
```

When text tool is active, hide brush size controls, show font size. When other tools are active, show brush size, hide font size. Handle this in `updateContextToolbar()`.

**Step 3: Update `tool-text.js` to use `EditorCore.fontSize`**

Already done in Task 2 — `tool-text.js` uses `EditorCore.fontSize` for both the input display and canvas rendering.

**Step 4: Run tests and commit**

```bash
git add frontend/js/editor/tool-text.js frontend/js/editor.js frontend/index.html frontend/js/editor/editor-core.js
git commit -m "feat(editor): add dedicated font size control for text tool"
```

---

### Task 12: Brush Size & Opacity Keyboard Shortcuts

Add bracket key shortcuts for adjusting brush size and opacity.

**Files:**
- Modify: `frontend/js/app.js` (register shortcuts)
- Modify: `frontend/js/editor.js` (add adjustment functions)

**Step 1: Add adjustment functions to `editor.js`**

```javascript
function adjustBrushSize(delta) {
    const newSize = Math.max(1, Math.min(50, EditorCore.brushSize + delta));
    EditorCore.brushSize = newSize;
    document.getElementById('editor-brush-size').value = newSize;
    document.getElementById('editor-brush-size-value').textContent = newSize + 'px';
}

function adjustOpacity(delta) {
    const newOpacity = Math.max(0, Math.min(1, EditorCore.currentOpacity + delta));
    EditorCore.currentOpacity = newOpacity;
    const pct = Math.round(newOpacity * 100);
    document.getElementById('editor-opacity').value = pct;
    document.getElementById('editor-opacity-value').textContent = pct + '%';
}
```

**Step 2: Register shortcuts in `app.js`**

```javascript
ShortcutManager.register({ id: 'editor.size_up',     label: 'Increase brush size', category: 'editor', context: 'editor', defaultKey: ']', callback: () => adjustBrushSize(2) });
ShortcutManager.register({ id: 'editor.size_down',   label: 'Decrease brush size', category: 'editor', context: 'editor', defaultKey: '[', callback: () => adjustBrushSize(-2) });
ShortcutManager.register({ id: 'editor.opacity_up',  label: 'Increase opacity',    category: 'editor', context: 'editor', defaultKey: 'shift+]', callback: () => adjustOpacity(0.1) });
ShortcutManager.register({ id: 'editor.opacity_down',label: 'Decrease opacity',    category: 'editor', context: 'editor', defaultKey: 'shift+[', callback: () => adjustOpacity(-0.1) });
```

Note: `shift+[` produces `{` and `shift+]` produces `}` on US keyboards. The ShortcutManager's `SHIFT_KEY_MAP` handles this automatically — `shift+[` becomes `{` and `shift+]` becomes `}`. So register as `{` and `}` instead:

```javascript
ShortcutManager.register({ id: 'editor.opacity_up',  label: 'Increase opacity',    category: 'editor', context: 'editor', defaultKey: '}', callback: () => adjustOpacity(0.1) });
ShortcutManager.register({ id: 'editor.opacity_down',label: 'Decrease opacity',    category: 'editor', context: 'editor', defaultKey: '{', callback: () => adjustOpacity(-0.1) });
```

**Step 3: Run tests and commit**

```bash
git add frontend/js/editor.js frontend/js/app.js
git commit -m "feat(editor): add bracket key shortcuts for brush size and opacity"
```

---

### Task 13: E2E Tests for New Tools

Add comprehensive e2e tests for all new editor features.

**Files:**
- Modify: `e2e/helpers/selectors.ts` (final selector updates if any missing)
- Modify: `e2e/fixtures/test-fixtures.ts` (add new AppHelper methods)
- Create: `e2e/tests/images/editor-advanced.spec.ts`

**Step 1: Add AppHelper methods for new tools**

```typescript
// In test-fixtures.ts, add to AppHelper class:

async selectEditorTool(tool: string): Promise<void> {
    await this.page.locator(`[data-tool="${tool}"]`).click();
}

async setZoom(action: 'fit' | '100' | 'in' | 'out'): Promise<void> {
    await this.page.locator(`#editor-zoom-${action}`).click();
}

async getZoomLevel(): Promise<string> {
    return await this.page.locator('#editor-zoom-display').textContent() || '';
}

async cropImage(from: Point, to: Point): Promise<void> {
    await this.selectEditorTool('crop');
    await this.drawOnCanvas(from, to);
    await this.page.locator('#editor-crop-confirm').click();
}

async anonymizeRegion(from: Point, to: Point, mode: 'brush' | 'rect' = 'rect'): Promise<void> {
    await this.selectEditorTool('anonymize');
    if (mode === 'rect') {
        await this.page.locator('#editor-anon-rect').click();
    }
    await this.drawOnCanvas(from, to);
}
```

**Step 2: Write tests for new tools**

Create `e2e/tests/images/editor-advanced.spec.ts` with test groups:

1. **Zoom controls**: zoom in, zoom out, fit, 100%, scroll wheel zoom, zoom display updates
2. **Arrow tool**: select, draw, verify undo available
3. **Eyedropper**: pick color from canvas, verify color picker updates
4. **Rotate/Flip**: rotate CW, verify canvas dimensions swap; flip H, verify undo available
5. **Crop tool**: select, define region, confirm, verify canvas size changed
6. **Selection tool**: select region, move, verify undo; copy/paste
7. **Anonymize tool**: pixelate region, verify undo; switch modes
8. **Text font size**: change font size, verify input value
9. **Keyboard shortcuts**: test all new tool shortcuts (V, C, W, X, I, R)
10. **Bracket shortcuts**: brush size increase/decrease

Each test follows the existing pattern:
```typescript
test('should ...', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(200, 200), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);
    await app.openImageEditor(filename);
    // ... test specific action ...
});
```

**Step 3: Run all tests**

Run: `cd e2e && npm test 2>&1 | tail -50`
Expected: All tests pass including new ones

**Step 4: Commit**

```bash
git add e2e/tests/images/editor-advanced.spec.ts e2e/helpers/selectors.ts e2e/fixtures/test-fixtures.ts
git commit -m "test(editor): add e2e tests for all new editor tools"
```

---

### Task 14: Final Integration & Polish

Verify everything works together, fix any integration issues, and ensure design system compliance.

**Files:**
- All editor files (review and fix)
- `frontend/css/modals.css` (any missing styles)

**Step 1: Verify all tool buttons have correct `data-tooltip` with shortcut keys**

Review each button in `index.html` and ensure tooltip format matches existing pattern: `"Tool name (shortcut)"`.

**Step 2: Verify accessibility**

- All tool buttons have `aria-label`
- Active tool uses `aria-pressed="true"`
- Contextual toolbars have appropriate `role` attributes
- Focus management works within the editor

**Step 3: Review canvas state management**

- Ensure undo/redo correctly handles canvas dimension changes (crop, rotate)
- Verify undo across tool switches (e.g., crop → undo should revert crop)
- Test memory ceiling with large images

**Step 4: Run full e2e suite**

Run: `cd e2e && npm test 2>&1 | tail -50`
Expected: All tests pass

**Step 5: Final commit**

```bash
git add -A
git commit -m "feat(editor): polish and finalize advanced image editor"
```
