// --- Editor Core Module ---
// Foundation for the refactored image editor. Manages canvas, state, tool registry,
// coordinate translation, zoom/pan, and undo/redo with ImageData-based history.

const EditorCore = (() => {
    // --- Canvas references ---
    let canvas = null;
    let ctx = null;
    let overlayCanvas = null;
    let overlayCtx = null;

    // --- Image state ---
    let originalImage = null;
    let originalContentType = '';
    let originalWidth = 0;
    let originalHeight = 0;
    let wasDownscaled = false;
    let baselineVersion = null;

    // --- Drawing properties ---
    let currentColor = '#44403c';
    let currentOpacity = 1;
    let brushSize = 8;
    let fontSize = 16;

    // --- Drawing state ---
    let isDrawing = false;
    let startX = 0;
    let startY = 0;
    let lastX = 0;
    let lastY = 0;

    // --- Zoom/pan state ---
    let zoomLevel = 1;
    let panX = 0;
    let panY = 0;
    let isPanning = false;
    let spaceHeld = false;
    let panStartX = 0;
    let panStartY = 0;
    let panStartPanX = 0;
    let panStartPanY = 0;

    // --- Tool registry ---
    const tools = new Map();
    let activeToolName = null;

    // --- Undo/redo ---
    const MAX_UNDO = 50;
    const MAX_MEMORY_BYTES = 100 * 1024 * 1024; // 100MB ceiling
    let undoStack = [];
    let redoStack = [];
    let cleanRevision = 0;
    let currentRevision = 0;
    let nextRevision = 1;

    // --- Listener tracking ---
    let listenersAttached = false;

    // Bound handler references for add/removeEventListener
    let boundMouseDown = null;
    let boundMouseMove = null;
    let boundMouseUp = null;
    let boundTouchStart = null;
    let boundTouchMove = null;
    let boundTouchEnd = null;
    let boundWheel = null;
    let boundKeyDown = null;
    let boundKeyUp = null;
    let boundContextMenu = null;

    // --- Image loading ---

    /**
     * Load an image blob into the canvas. Caps at 4000px on longest side.
     * Returns a promise that resolves when the image is drawn.
     */
    async function loadImage(imageBlob, contentType) {
        canvas = document.getElementById('editor-canvas');
        ctx = canvas.getContext('2d');
        overlayCanvas = document.getElementById('editor-overlay-canvas');
        overlayCtx = overlayCanvas.getContext('2d');

        originalContentType = contentType || 'image/png';
        undoStack = [];
        redoStack = [];
        cleanRevision = 0;
        currentRevision = 0;
        nextRevision = 1;

        originalImage = new Image();
        const objectURL = URL.createObjectURL(imageBlob);
        try {
            originalImage.src = objectURL;
            await new Promise((resolve, reject) => {
                originalImage.onload = resolve;
                originalImage.onerror = reject;
            });
        } finally {
            URL.revokeObjectURL(objectURL);
        }

        originalWidth = originalImage.width;
        originalHeight = originalImage.height;
        const maxSize = 4000;
        let width = originalWidth;
        let height = originalHeight;
        wasDownscaled = width > maxSize || height > maxSize;

        if (wasDownscaled) {
            const ratio = Math.min(maxSize / width, maxSize / height);
            width = Math.floor(width * ratio);
            height = Math.floor(height * ratio);
        }

        canvas.width = width;
        canvas.height = height;
        overlayCanvas.width = width;
        overlayCanvas.height = height;

        ctx.drawImage(originalImage, 0, 0, width, height);
        baselineVersion = captureBaselineFromCanvas(canvas);
        syncOverlay();
        saveUndoState({ force: true });
        cleanRevision = currentRevision;
        originalImage = null;
    }

    // --- Coordinate translation ---

    /**
     * Convert screen (client) coordinates to canvas-space coordinates,
     * accounting for zoom and pan transforms.
     */
    function screenToCanvas(clientX, clientY) {
        const rect = canvas.getBoundingClientRect();
        const scaleX = canvas.width / rect.width;
        const scaleY = canvas.height / rect.height;

        return {
            x: (clientX - rect.left) * scaleX,
            y: (clientY - rect.top) * scaleY
        };
    }

    // --- Zoom/pan ---

    /**
     * Apply CSS transform for zoom and pan to the canvas wrapper.
     */
    function applyTransform() {
        const transform = `translate(${panX}px, ${panY}px) scale(${zoomLevel})`;
        // Transform the wrapper div so both canvases move/scale together
        if (canvas && canvas.parentElement) {
            canvas.parentElement.style.transform = transform;
        }
    }

    /**
     * Set zoom level, clamped to [0.1, 10].
     */
    function setZoom(level, clientX, clientY) {
        const newZoom = Math.max(0.1, Math.min(8, level));
        if (newZoom === zoomLevel) return;

        let focalPoint = null;
        if (clientX !== undefined && clientY !== undefined && canvas) {
            focalPoint = screenToCanvas(clientX, clientY);
        }

        zoomLevel = newZoom;
        applyTransform();

        if (focalPoint && canvas) {
            // A transformed DOMRect can be rounded to device pixels. Apply one
            // bounded residual correction so that rounding cannot leave the
            // canvas coordinate beneath the cursor drifting between zooms.
            for (let correction = 0; correction < 2; correction++) {
                const rect = canvas.getBoundingClientRect();
                const renderedX = rect.left + focalPoint.x / canvas.width * rect.width;
                const renderedY = rect.top + focalPoint.y / canvas.height * rect.height;
                const deltaX = clientX - renderedX;
                const deltaY = clientY - renderedY;
                panX += deltaX;
                panY += deltaY;
                applyTransform();
                if (Math.abs(deltaX) < 0.01 && Math.abs(deltaY) < 0.01) break;
            }
        }
    }

    /**
     * Reset zoom and pan to defaults.
     */
    function resetZoom() {
        zoomLevel = 1;
        panX = 0;
        panY = 0;
        applyTransform();
    }

    // --- Overlay sync ---

    /**
     * Sync overlay canvas dimensions with the main canvas.
     * The overlay is positioned via CSS (absolute, top:0, left:0) inside
     * the same wrapper div, so no JS positioning is needed.
     */
    function syncOverlay() {
        // Nothing to do — overlay shares the wrapper with the main canvas
        // and CSS handles positioning. Pixel dimensions are synced when
        // the canvas is resized (loadImage, crop, rotate).
    }

    function captureBaselineFromCanvas(sourceCanvas) {
        const sourceCtx = sourceCanvas?.getContext('2d');
        if (!sourceCtx) return null;
        return {
            width: sourceCanvas.width,
            height: sourceCanvas.height,
            imageData: sourceCtx.getImageData(0, 0, sourceCanvas.width, sourceCanvas.height),
        };
    }

    function setBaselineFromCanvas(sourceCanvas) {
        baselineVersion = captureBaselineFromCanvas(sourceCanvas);
    }

    function getBaselineRegion(x, y, width, height) {
        if (!baselineVersion || width <= 0 || height <= 0) return null;
        const result = new ImageData(width, height);
        const source = baselineVersion.imageData.data;
        const destination = result.data;
        for (let row = 0; row < height; row++) {
            const sourceY = y + row;
            if (sourceY < 0 || sourceY >= baselineVersion.height) continue;
            for (let column = 0; column < width; column++) {
                const sourceX = x + column;
                if (sourceX < 0 || sourceX >= baselineVersion.width) continue;
                const sourceIndex = (sourceY * baselineVersion.width + sourceX) * 4;
                const destinationIndex = (row * width + column) * 4;
                destination[destinationIndex] = source[sourceIndex];
                destination[destinationIndex + 1] = source[sourceIndex + 1];
                destination[destinationIndex + 2] = source[sourceIndex + 2];
                destination[destinationIndex + 3] = source[sourceIndex + 3];
            }
        }
        return result;
    }

    // --- Tool registry ---

    /**
     * Register a tool. Tool objects should implement:
     *   activate()               - called when tool becomes active
     *   deactivate()             - called when tool becomes inactive
     *   onMouseDown(coords, e)   - canvas mousedown
     *   onMouseMove(coords, e)   - canvas mousemove
     *   onMouseUp(coords, e)     - canvas mouseup
     *   getCursor(coords)        - return CSS cursor string for optional canvas coordinates
     */
    function registerTool(name, tool) {
        tools.set(name, tool);
    }

    /**
     * Switch to a named tool.
     */
    function selectTool(name) {
        if (!tools.has(name)) {
            console.warn('EditorCore: unknown tool "' + name + '"');
            return;
        }

        const previousToolName = activeToolName;
        const current = getActiveTool();
        if (current && typeof current.deactivate === 'function') {
            current.deactivate();
        }

        activeToolName = name;

        const next = tools.get(name);
        if (next && typeof next.activate === 'function') {
            next.activate({ previousToolName });
        }

        // Update cursor
        updateCursor();
    }

    /**
     * Get the active tool object, or null.
     */
    function getActiveTool() {
        if (!activeToolName) return null;
        return tools.get(activeToolName) || null;
    }

    /**
     * Update canvas cursor based on active tool or pan state.
     */
    function updateCursor(coords = null) {
        if (!canvas) return;
        let cursor;
        if (spaceHeld || isPanning) {
            cursor = isPanning ? 'grabbing' : 'grab';
        } else {
            const tool = getActiveTool();
            cursor = tool && typeof tool.getCursor === 'function'
                ? tool.getCursor(coords)
                : 'crosshair';
        }
        canvas.style.cursor = cursor;
        if (overlayCanvas) overlayCanvas.style.cursor = cursor;
    }

    // --- Undo/redo with ImageData ---

    /**
     * Calculate unique raw-pixel memory retained by undo/redo history. The
     * active eraser baseline is a working buffer, but older baseline versions
     * retained solely for history count toward the same ceiling.
     */
    function totalMemoryUsage() {
        const buffers = new Set();
        const activeBaselineBuffer = undoStack[undoStack.length - 1]
            ?.baselineVersion?.imageData?.data?.buffer;

        for (const entry of [...undoStack, ...redoStack]) {
            const imageBuffer = entry.imageData?.data?.buffer;
            if (imageBuffer) buffers.add(imageBuffer);
            const baselineBuffer = entry.baselineVersion?.imageData?.data?.buffer;
            if (baselineBuffer && baselineBuffer !== activeBaselineBuffer) {
                buffers.add(baselineBuffer);
            }
        }

        let total = 0;
        for (const buffer of buffers) total += buffer.byteLength;
        return total;
    }

    /**
     * Keep history within the advertised count and raw RGBA memory ceilings.
     * The current state is retained even when a large document leaves no room
     * for an older undo state.
     */
    function trimHistory() {
        // One opening state plus up to MAX_UNDO reversible actions.
        while (undoStack.length + redoStack.length > MAX_UNDO + 1) {
            if (undoStack.length > 1) undoStack.shift();
            else redoStack.shift();
        }
        while (totalMemoryUsage() > MAX_MEMORY_BYTES) {
            if (undoStack.length > 1) undoStack.shift();
            else if (redoStack.length > 0) redoStack.shift();
            else break;
        }
    }

    function imageDataEquals(a, b) {
        if (!a || !b || a.width !== b.width || a.height !== b.height || a.data.length !== b.data.length) return false;
        const left = a.data;
        const right = b.data;
        for (let i = 0; i < left.length; i++) {
            if (left[i] !== right[i]) return false;
        }
        return true;
    }

    /**
     * Save current canvas state to the undo stack.
     * Each entry stores {imageData, width, height} so undo/redo can
     * restore canvas dimensions after crop or rotate operations.
     */
    function saveUndoState(options = {}) {
        if (!canvas || !ctx) return false;

        const imageData = ctx.getImageData(0, 0, canvas.width, canvas.height);
        const previous = undoStack[undoStack.length - 1];
        if (!options.force && previous && imageDataEquals(previous.imageData, imageData) &&
                previous.baselineVersion === baselineVersion) {
            updateUndoRedoButtons();
            return false;
        }

        redoStack = [];
        currentRevision = nextRevision++;
        undoStack.push({
            imageData,
            width: canvas.width,
            height: canvas.height,
            baselineVersion,
            revision: currentRevision,
        });

        trimHistory();
        updateUndoRedoButtons();
        return true;
    }

    /**
     * Restore a canvas state entry, resizing canvas if dimensions changed.
     */
    function restoreState(entry) {
        // Resize canvas and overlay if dimensions differ (crop, rotate)
        if (canvas.width !== entry.width || canvas.height !== entry.height) {
            canvas.width = entry.width;
            canvas.height = entry.height;
            if (overlayCanvas) {
                overlayCanvas.width = entry.width;
                overlayCanvas.height = entry.height;
            }
            // Re-fit zoom so canvas isn't awkwardly panned/zoomed after dimension change
            if (typeof ZoomTool !== 'undefined') ZoomTool.zoomToFit();
        }

        ctx.putImageData(entry.imageData, 0, 0);
        baselineVersion = entry.baselineVersion || baselineVersion;
        currentRevision = entry.revision;
        syncOverlay();
    }

    /**
     * Undo: pop last state to redo stack, restore previous.
     */
    function undo() {
        if (prepareForAction('undo') === 'consumed') return;
        if (undoStack.length <= 1) return;

        redoStack.push(undoStack.pop());
        const entry = undoStack[undoStack.length - 1];
        restoreState(entry);
        trimHistory();
        updateUndoRedoButtons();
    }

    /**
     * Redo: pop from redo stack, push to undo, restore.
     */
    function redo() {
        if (prepareForAction('redo') === 'consumed') return;
        if (redoStack.length === 0) return;

        const entry = redoStack.pop();
        undoStack.push(entry);
        restoreState(entry);
        trimHistory();
        updateUndoRedoButtons();
    }

    /**
     * Redraw canvas from current top of undo stack.
     */
    function redrawCanvas() {
        if (undoStack.length === 0 || !ctx) return;
        const entry = undoStack[undoStack.length - 1];
        restoreState(entry);
    }

    /**
     * Update undo/redo button disabled states.
     */
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
        if (typeof updateEditorSaveState === 'function') updateEditorSaveState();
    }

    /**
     * Whether the current canvas state differs from the state loaded into the editor.
     * Tracking the clean history entry means undoing all edits returns to a clean state.
     */
    function isDirty() {
        return currentRevision !== 0 && currentRevision !== cleanRevision;
    }

    function prepareForAction(intent) {
        const tool = getActiveTool();
        if (!tool || typeof tool.prepareForAction !== 'function') return 'proceed';
        return tool.prepareForAction(intent) || 'proceed';
    }

    function getCurrentSnapshot() {
        return undoStack[undoStack.length - 1]?.imageData || null;
    }

    function getHistoryStats() {
        return {
            undoDepth: Math.max(0, undoStack.length - 1),
            redoDepth: redoStack.length,
            bytes: totalMemoryUsage(),
            maxBytes: MAX_MEMORY_BYTES,
        };
    }

    // --- Event dispatch ---

    function handleMouseDown(e) {
        if (!canvas) return;

        const clientX = e.clientX;
        const clientY = e.clientY;

        // Space+drag = pan (regardless of active tool)
        if (spaceHeld) {
            isPanning = true;
            panStartX = clientX;
            panStartY = clientY;
            panStartPanX = panX;
            panStartPanY = panY;
            updateCursor();
            e.preventDefault();
            return;
        }

        const coords = screenToCanvas(clientX, clientY);
        startX = coords.x;
        startY = coords.y;
        lastX = coords.x;
        lastY = coords.y;
        isDrawing = true;

        const tool = getActiveTool();
        if (tool && typeof tool.onMouseDown === 'function') {
            tool.onMouseDown(coords, e);
        }
    }

    function handleMouseMove(e) {
        if (!canvas) return;

        // Handle pan
        if (isPanning) {
            panX = panStartPanX + (e.clientX - panStartX);
            panY = panStartPanY + (e.clientY - panStartY);
            applyTransform();
            return;
        }

        const coords = screenToCanvas(e.clientX, e.clientY);

        if (!isDrawing) {
            updateCursor(coords);
            return;
        }

        const tool = getActiveTool();
        if (tool && typeof tool.onMouseMove === 'function') {
            tool.onMouseMove(coords, e);
        }

        lastX = coords.x;
        lastY = coords.y;
    }

    function handleMouseUp(e) {
        if (!canvas) return;

        // End pan
        if (isPanning) {
            isPanning = false;
            updateCursor();
            return;
        }

        if (!isDrawing) return;
        isDrawing = false;

        const coords = screenToCanvas(e.clientX, e.clientY);

        const tool = getActiveTool();
        if (tool && typeof tool.onMouseUp === 'function') {
            tool.onMouseUp(coords, e);
        }

        updateCursor(coords);
        if (typeof updateEditorSaveState === 'function') updateEditorSaveState();
    }

    function handleMouseLeave(e) {
        // No-op: mousemove and mouseup are on document, so drawing
        // continues even when the cursor leaves the canvas and resumes
        // when it re-enters.
    }

    function handleTouchStart(e) {
        e.preventDefault();
        const touch = e.touches[0];
        const mouseEvent = new MouseEvent('mousedown', {
            clientX: touch.clientX,
            clientY: touch.clientY
        });
        handleMouseDown(mouseEvent);
    }

    function handleTouchMove(e) {
        e.preventDefault();
        const touch = e.touches[0];
        const mouseEvent = new MouseEvent('mousemove', {
            clientX: touch.clientX,
            clientY: touch.clientY,
            shiftKey: false
        });
        handleMouseMove(mouseEvent);
    }

    function handleTouchEnd(e) {
        const touch = e.changedTouches[0];
        const mouseEvent = new MouseEvent('mouseup', {
            clientX: touch.clientX,
            clientY: touch.clientY
        });
        handleMouseUp(mouseEvent);
    }

    function handleWheel(e) {
        if (!canvas) return;
        // Pinch-to-zoom or ctrl+scroll
        if (e.ctrlKey || e.metaKey) {
            e.preventDefault();
            const delta = e.deltaY > 0 ? 0.9 : 1.1;
            setZoom(zoomLevel * delta, e.clientX, e.clientY);
            if (typeof ZoomTool !== 'undefined') ZoomTool.updateZoomDisplay();
        }
    }

    function handleKeyDown(e) {
        if (e.code === 'Space' && !e.repeat) {
            // Only intercept space if the editor canvas is in a visible editor
            // and the target isn't an input element
            const tag = e.target.tagName;
            if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return;
            if (!canvas) return;

            spaceHeld = true;
            updateCursor();
            e.preventDefault();
        }
    }

    function handleKeyUp(e) {
        if (e.code === 'Space') {
            spaceHeld = false;
            if (isPanning) {
                isPanning = false;
            }
            updateCursor();
        }
    }

    function handleContextMenu(e) {
        // Prevent right-click context menu on the canvas
        e.preventDefault();
    }

    // --- Listener lifecycle ---

    /**
     * Attach all mouse/touch/keyboard listeners to the canvas and document.
     */
    function attachListeners() {
        if (listenersAttached || !canvas) return;

        boundMouseDown = handleMouseDown;
        boundMouseMove = handleMouseMove;
        boundMouseUp = handleMouseUp;
        boundTouchStart = handleTouchStart;
        boundTouchMove = handleTouchMove;
        boundTouchEnd = handleTouchEnd;
        boundWheel = handleWheel;
        boundKeyDown = handleKeyDown;
        boundKeyUp = handleKeyUp;
        boundContextMenu = handleContextMenu;

        canvas.addEventListener('mousedown', boundMouseDown);
        overlayCanvas.addEventListener('mousedown', boundMouseDown);
        document.addEventListener('mousemove', boundMouseMove);
        document.addEventListener('mouseup', boundMouseUp);
        canvas.addEventListener('touchstart', boundTouchStart, { passive: false });
        canvas.addEventListener('touchmove', boundTouchMove, { passive: false });
        canvas.addEventListener('touchend', boundTouchEnd);
        overlayCanvas.addEventListener('touchstart', boundTouchStart, { passive: false });
        overlayCanvas.addEventListener('touchmove', boundTouchMove, { passive: false });
        overlayCanvas.addEventListener('touchend', boundTouchEnd);
        canvas.addEventListener('wheel', boundWheel, { passive: false });
        overlayCanvas.addEventListener('wheel', boundWheel, { passive: false });
        canvas.addEventListener('contextmenu', boundContextMenu);
        overlayCanvas.addEventListener('contextmenu', boundContextMenu);

        document.addEventListener('keydown', boundKeyDown);
        document.addEventListener('keyup', boundKeyUp);

        listenersAttached = true;
    }

    /**
     * Detach all listeners. Call when closing the editor.
     */
    function detachListeners() {
        if (!listenersAttached) return;

        if (canvas) {
            canvas.removeEventListener('mousedown', boundMouseDown);
        }
        if (overlayCanvas) {
            overlayCanvas.removeEventListener('mousedown', boundMouseDown);
        }
        document.removeEventListener('mousemove', boundMouseMove);
        document.removeEventListener('mouseup', boundMouseUp);

        if (canvas) {
            canvas.removeEventListener('touchstart', boundTouchStart);
            canvas.removeEventListener('touchmove', boundTouchMove);
            canvas.removeEventListener('touchend', boundTouchEnd);
            canvas.removeEventListener('wheel', boundWheel);
            canvas.removeEventListener('contextmenu', boundContextMenu);
        }
        if (overlayCanvas) {
            overlayCanvas.removeEventListener('touchstart', boundTouchStart);
            overlayCanvas.removeEventListener('touchmove', boundTouchMove);
            overlayCanvas.removeEventListener('touchend', boundTouchEnd);
            overlayCanvas.removeEventListener('wheel', boundWheel);
            overlayCanvas.removeEventListener('contextmenu', boundContextMenu);
        }

        document.removeEventListener('keydown', boundKeyDown);
        document.removeEventListener('keyup', boundKeyUp);

        listenersAttached = false;
    }

    // --- Reset ---

    /**
     * Clear all state. Call when closing the editor or loading a new image.
     */
    function reset() {
        detachListeners();

        // Reset tool BEFORE nullifying canvas (tools may reference canvas during deactivation)
        const current = getActiveTool();
        if (current && typeof current.deactivate === 'function') {
            current.deactivate();
        }
        activeToolName = null;

        // Clear tool registry to prevent accumulation on repeated opens
        tools.clear();

        // Clear transform on wrapper before losing canvas ref
        if (canvas && canvas.parentElement) {
            canvas.parentElement.style.transform = '';
        }

        // Reset canvas refs
        canvas = null;
        ctx = null;
        overlayCanvas = null;
        overlayCtx = null;
        originalImage = null;
        originalContentType = '';
        originalWidth = 0;
        originalHeight = 0;
        wasDownscaled = false;
        baselineVersion = null;

        // Reset drawing properties
        currentColor = '#44403c';
        currentOpacity = 1;
        brushSize = 8;
        fontSize = 16;

        // Reset drawing state
        isDrawing = false;
        startX = 0;
        startY = 0;
        lastX = 0;
        lastY = 0;

        // Reset zoom/pan
        zoomLevel = 1;
        panX = 0;
        panY = 0;
        isPanning = false;
        spaceHeld = false;
        panStartX = 0;
        panStartY = 0;
        panStartPanX = 0;
        panStartPanY = 0;

        // Clear undo/redo stacks
        undoStack = [];
        redoStack = [];
        cleanRevision = 0;
        currentRevision = 0;
        nextRevision = 1;
    }

    // --- Public API ---

    return {
        // Image loading
        loadImage,

        // Canvas references (getters for read access)
        get canvas() { return canvas; },
        get ctx() { return ctx; },
        get overlayCanvas() { return overlayCanvas; },
        get overlayCtx() { return overlayCtx; },
        get originalImage() { return originalImage; },
        get originalContentType() { return originalContentType; },
        get originalWidth() { return originalWidth; },
        get originalHeight() { return originalHeight; },
        get wasDownscaled() { return wasDownscaled; },

        // Drawing properties (getters and setters)
        get currentColor() { return currentColor; },
        set currentColor(v) { currentColor = v; },
        get currentOpacity() { return currentOpacity; },
        set currentOpacity(v) { currentOpacity = v; },
        get brushSize() { return brushSize; },
        set brushSize(v) { brushSize = v; },
        get fontSize() { return fontSize; },
        set fontSize(v) { fontSize = v; },

        // Drawing state (getters, setters for coordinates)
        get isDrawing() { return isDrawing; },
        set isDrawing(v) { isDrawing = v; },
        get startX() { return startX; },
        set startX(v) { startX = v; },
        get startY() { return startY; },
        set startY(v) { startY = v; },
        get lastX() { return lastX; },
        set lastX(v) { lastX = v; },
        get lastY() { return lastY; },
        set lastY(v) { lastY = v; },

        // Zoom/pan state (getters/setters)
        get zoomLevel() { return zoomLevel; },
        set zoomLevel(v) { zoomLevel = v; },
        get panX() { return panX; },
        set panX(v) { panX = v; },
        get panY() { return panY; },
        set panY(v) { panY = v; },
        get isPanning() { return isPanning; },
        get spaceHeld() { return spaceHeld; },

        // Coordinate translation
        screenToCanvas,

        // Zoom/pan control
        setZoom,
        resetZoom,
        applyTransform,

        // Overlay
        syncOverlay,
        setBaselineFromCanvas,
        getBaselineRegion,

        // Tool registry
        registerTool,
        selectTool,
        getActiveTool,
        get activeToolName() { return activeToolName; },
        get tools() { return tools; },

        // Undo/redo
        saveUndoState,
        undo,
        redo,
        redrawCanvas,
        updateUndoRedoButtons,
        get undoStack() { return undoStack; },
        get redoStack() { return redoStack; },
        isDirty,
        prepareForAction,
        getHistoryStats,
        getCurrentSnapshot,

        // Listener lifecycle
        attachListeners,
        detachListeners,

        // Full reset
        reset,

        // Cursor
        updateCursor,
    };
})();
