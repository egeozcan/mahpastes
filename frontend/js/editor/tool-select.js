// --- Selection Tool ---
// Rectangular pixel selection with move, resize, copy/paste, and marching ants.

const SelectTool = (() => {
    // --- State ---
    let selectionRect = null;       // {x, y, w, h} in canvas space
    let selectionImageData = null;  // Captured pixel data (ImageData)
    let activeHandle = null;        // null, 'nw', 'n', 'ne', 'e', 'se', 's', 'sw', 'w', 'move'
    let dragStartX = 0;
    let dragStartY = 0;
    let originalRect = null;        // Snapshot for drag calculation
    let clipboard = null;           // Internal ImageData copy
    let marchingAntsOffset = 0;
    let animFrameId = null;
    let committed = true;           // Whether current selection has been committed back
    let creating = false;           // Whether we are dragging to create a new selection
    let originalPosition = null;    // {x, y} where selection was first captured (for cancel)
    let originalCanvasData = null;  // Full canvas snapshot at time of capture (for cancel)
    let selectionSurface = null;    // Cached raster used by overlay rendering
    let lastAnimationTime = 0;

    const HANDLE_SIZE = 6;
    const HANDLE_HIT = 8;
    const MIN_SEL = 5;             // Minimum selection dimension

    // --- Handle positions ---

    function getHandles() {
        if (!selectionRect) return {};
        const { x, y, w, h } = selectionRect;
        return {
            nw: { x: x,         y: y },
            n:  { x: x + w / 2, y: y },
            ne: { x: x + w,     y: y },
            e:  { x: x + w,     y: y + h / 2 },
            se: { x: x + w,     y: y + h },
            s:  { x: x + w / 2, y: y + h },
            sw: { x: x,         y: y + h },
            w:  { x: x,         y: y + h / 2 },
        };
    }

    function hitTestHandles(cx, cy) {
        const handles = getHandles();
        const canvas = EditorCore.canvas;
        if (!canvas) return null;
        const rect = canvas.getBoundingClientRect();
        const scale = canvas.width / rect.width;
        const tol = HANDLE_HIT * scale;

        for (const [name, pos] of Object.entries(handles)) {
            if (Math.abs(cx - pos.x) <= tol && Math.abs(cy - pos.y) <= tol) {
                return name;
            }
        }
        return null;
    }

    function insideSelectionRect(cx, cy) {
        if (!selectionRect) return false;
        return cx >= selectionRect.x && cx <= selectionRect.x + selectionRect.w &&
               cy >= selectionRect.y && cy <= selectionRect.y + selectionRect.h;
    }

    // --- Cursor mapping ---

    const handleCursors = {
        nw: 'nwse-resize', ne: 'nesw-resize',
        sw: 'nesw-resize', se: 'nwse-resize',
        n: 'ns-resize', s: 'ns-resize',
        e: 'ew-resize', w: 'ew-resize',
        move: 'move',
    };

    // --- Overlay rendering ---

    function renderOverlay() {
        const oc = EditorCore.overlayCtx;
        const overlayCanvas = EditorCore.overlayCanvas;
        if (!oc || !overlayCanvas) return;

        const cw = overlayCanvas.width;
        const ch = overlayCanvas.height;
        oc.clearRect(0, 0, cw, ch);

        if (!selectionRect) return;

        const { x, y, w, h } = selectionRect;

        // 1. Draw selection image data on overlay at current position (scaled if resized)
        if (selectionSurface) {
            oc.drawImage(selectionSurface, x, y, w, h);
        }

        // 2. Marching ants border (two dashed lines for contrast)
        oc.lineWidth = 1;
        oc.setLineDash([6, 6]);

        // White dashes
        oc.strokeStyle = '#ffffff';
        oc.lineDashOffset = -marchingAntsOffset;
        oc.strokeRect(x, y, w, h);

        // Black dashes offset by half dash length
        oc.strokeStyle = '#292524';
        oc.lineDashOffset = -(marchingAntsOffset + 6);
        oc.strokeRect(x, y, w, h);

        oc.setLineDash([]);
        oc.lineDashOffset = 0;

        // 3. Draw 8 handles
        const handles = getHandles();
        const half = HANDLE_SIZE / 2;
        for (const pos of Object.values(handles)) {
            oc.fillStyle = '#ffffff';
            oc.fillRect(pos.x - half, pos.y - half, HANDLE_SIZE, HANDLE_SIZE);
            oc.strokeStyle = '#292524';
            oc.lineWidth = 1;
            oc.strokeRect(pos.x - half, pos.y - half, HANDLE_SIZE, HANDLE_SIZE);
        }
    }

    // --- Marching ants animation ---

    function animateMarchingAnts(timestamp) {
        if (timestamp - lastAnimationTime >= 66) {
            marchingAntsOffset = (marchingAntsOffset + 1) % 12;
            renderOverlay();
            lastAnimationTime = timestamp;
        }
        animFrameId = requestAnimationFrame(animateMarchingAnts);
    }

    function startAnimation() {
        if (window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) {
            renderOverlay();
            return;
        }
        if (animFrameId != null) return;
        lastAnimationTime = 0;
        animFrameId = requestAnimationFrame(animateMarchingAnts);
    }

    function stopAnimation() {
        if (animFrameId != null) {
            cancelAnimationFrame(animFrameId);
            animFrameId = null;
        }
    }

    // --- Selection commit ---

    function commitSelection() {
        if (!selectionRect || !selectionImageData || committed) return;

        const canvas = EditorCore.canvas;
        const ctx = EditorCore.ctx;
        if (!canvas || !ctx) return;

        const { x, y, w, h } = selectionRect;

        // Draw selectionImageData onto main canvas at current position (using offscreen canvas for scaling)
        const offscreen = document.createElement('canvas');
        offscreen.width = selectionImageData.width;
        offscreen.height = selectionImageData.height;
        const offCtx = offscreen.getContext('2d');
        offCtx.putImageData(selectionImageData, 0, 0);

        ctx.drawImage(offscreen, x, y, w, h);

        EditorCore.saveUndoState();
        committed = true;

        clearSelection();
    }

    // --- Selection cancel ---

    function cancelSelection() {
        if (!selectionRect || !selectionImageData) {
            clearSelection();
            return;
        }

        const canvas = EditorCore.canvas;
        const ctx = EditorCore.ctx;
        if (!canvas || !ctx) return;

        // Restore the full canvas to what it was when the selection was captured
        if (originalCanvasData) {
            ctx.putImageData(originalCanvasData, 0, 0);
        }

        clearSelection();
    }

    // --- Clear selection state ---

    function clearSelection() {
        selectionRect = null;
        selectionImageData = null;
        activeHandle = null;
        originalRect = null;
        creating = false;
        committed = true;
        originalPosition = null;
        originalCanvasData = null;
        selectionSurface = null;

        // Clear overlay
        const oc = EditorCore.overlayCtx;
        const overlayCanvas = EditorCore.overlayCanvas;
        if (oc && overlayCanvas) {
            oc.clearRect(0, 0, overlayCanvas.width, overlayCanvas.height);
        }
        if (typeof updateEditorSaveState === 'function') updateEditorSaveState();
    }

    // --- Capture pixels from canvas into selection ---

    function captureSelection(x, y, w, h) {
        const canvas = EditorCore.canvas;
        const ctx = EditorCore.ctx;
        if (!canvas || !ctx) return;

        // Clamp to canvas bounds
        const cx = Math.max(0, Math.round(x));
        const cy = Math.max(0, Math.round(y));
        const cw = Math.min(Math.round(w), canvas.width - cx);
        const ch = Math.min(Math.round(h), canvas.height - cy);
        if (cw <= 0 || ch <= 0) return;

        // Save full canvas state for cancel
        originalCanvasData = EditorCore.getCurrentSnapshot() || ctx.getImageData(0, 0, canvas.width, canvas.height);

        // Capture pixel data from the region
        selectionImageData = ctx.getImageData(cx, cy, cw, ch);
        selectionSurface = document.createElement('canvas');
        selectionSurface.width = cw;
        selectionSurface.height = ch;
        selectionSurface.getContext('2d').putImageData(selectionImageData, 0, 0);

        // Lift pixels without destroying transparency.
        ctx.clearRect(cx, cy, cw, ch);

        // Update the selection rect to clamped values
        selectionRect = { x: cx, y: cy, w: cw, h: ch };
        originalPosition = { x: cx, y: cy };
        committed = false;
    }

    // --- Tool interface ---

    function create() {
        return {
            activate() {
                // Start marching ants if there's an existing selection
                if (selectionRect) {
                    startAnimation();
                }
            },

            deactivate() {
                // Commit any uncommitted selection before switching tools
                if (!committed && selectionRect && selectionImageData) {
                    commitSelection();
                }
                stopAnimation();
                clearSelection();
            },

            onMouseDown(coords, e) {
                const cx = coords.x;
                const cy = coords.y;

                if (selectionRect) {
                    // Hit-test handles first
                    const handle = hitTestHandles(cx, cy);
                    if (handle) {
                        activeHandle = handle;
                        dragStartX = cx;
                        dragStartY = cy;
                        originalRect = { ...selectionRect };
                        creating = false;
                        return;
                    }

                    // Inside selection rect -> move
                    if (insideSelectionRect(cx, cy)) {
                        // Alt+drag = duplicate: put original back, move a copy
                        if (e.altKey && !committed && selectionImageData) {
                            // Put back the image data at its current position on the main canvas
                            const mainCtx = EditorCore.ctx;
                            if (mainCtx) {
                                const offscreen = document.createElement('canvas');
                                offscreen.width = selectionImageData.width;
                                offscreen.height = selectionImageData.height;
                                const offCtx = offscreen.getContext('2d');
                                offCtx.putImageData(selectionImageData, 0, 0);
                                mainCtx.drawImage(offscreen, selectionRect.x, selectionRect.y, selectionRect.w, selectionRect.h);
                            }
                            // Keep the original baked into the canvas while the same
                            // pixels remain floating as the duplicate. Cancelling the
                            // duplicate restores this state.
                            originalCanvasData = EditorCore.ctx.getImageData(0, 0, EditorCore.canvas.width, EditorCore.canvas.height);
                        }

                        activeHandle = 'move';
                        dragStartX = cx;
                        dragStartY = cy;
                        originalRect = { ...selectionRect };
                        creating = false;
                        return;
                    }

                    // Outside selection -> commit current and start new
                    commitSelection();
                }

                // Start creating a new selection
                creating = true;
                activeHandle = null;
                dragStartX = cx;
                dragStartY = cy;
                selectionRect = { x: cx, y: cy, w: 0, h: 0 };
            },

            onMouseMove(coords, e) {
                const cx = coords.x;
                const cy = coords.y;

                if (creating) {
                    // Expand rect from start point
                    let x = Math.min(dragStartX, cx);
                    let y = Math.min(dragStartY, cy);
                    let w = Math.abs(cx - dragStartX);
                    let h = Math.abs(cy - dragStartY);

                    // Shift = constrain to square
                    if (e.shiftKey) {
                        const side = Math.max(w, h);
                        w = side;
                        h = side;
                        // Adjust origin to maintain drag direction
                        if (cx < dragStartX) x = dragStartX - side;
                        if (cy < dragStartY) y = dragStartY - side;
                    }

                    selectionRect = { x, y, w, h };
                    renderOverlay();
                    return;
                }

                if (!activeHandle || !originalRect) return;

                const dx = cx - dragStartX;
                const dy = cy - dragStartY;

                if (activeHandle === 'move') {
                    // Move the selection
                    selectionRect = {
                        x: originalRect.x + dx,
                        y: originalRect.y + dy,
                        w: originalRect.w,
                        h: originalRect.h,
                    };
                } else {
                    // Resize via handle
                    let r = { ...originalRect };

                    if (activeHandle.includes('w')) {
                        r.x = originalRect.x + dx;
                        r.w = originalRect.w - dx;
                    }
                    if (activeHandle.includes('e')) {
                        r.w = originalRect.w + dx;
                    }
                    if (activeHandle.includes('n')) {
                        r.y = originalRect.y + dy;
                        r.h = originalRect.h - dy;
                    }
                    if (activeHandle.includes('s')) {
                        r.h = originalRect.h + dy;
                    }

                    // Prevent negative dimensions by flipping
                    if (r.w < 0) {
                        r.x = r.x + r.w;
                        r.w = Math.abs(r.w);
                    }
                    if (r.h < 0) {
                        r.y = r.y + r.h;
                        r.h = Math.abs(r.h);
                    }

                    if (e.shiftKey) {
                        const ratio = originalRect.w / originalRect.h || 1;
                        if (activeHandle === 'e' || activeHandle === 'w') {
                            const centerY = originalRect.y + originalRect.h / 2;
                            r.h = r.w / ratio;
                            r.y = centerY - r.h / 2;
                        } else if (activeHandle === 'n' || activeHandle === 's') {
                            const centerX = originalRect.x + originalRect.w / 2;
                            r.w = r.h * ratio;
                            r.x = centerX - r.w / 2;
                        } else {
                            const heightFromWidth = r.w / ratio;
                            if (activeHandle.includes('n')) r.y += r.h - heightFromWidth;
                            r.h = heightFromWidth;
                        }
                    }

                    // Enforce minimum size
                    r.w = Math.max(MIN_SEL, r.w);
                    r.h = Math.max(MIN_SEL, r.h);

                    selectionRect = r;
                }

                // Overlay is redrawn by the animation loop (or manually if animation hasn't started)
                if (animFrameId == null) renderOverlay();
            },

            onMouseUp(coords, e) {
                if (creating) {
                    creating = false;

                    // Check if selection is big enough
                    if (!selectionRect || selectionRect.w < MIN_SEL || selectionRect.h < MIN_SEL) {
                        clearSelection();
                        return;
                    }

                    // Capture pixels from the main canvas
                    captureSelection(selectionRect.x, selectionRect.y, selectionRect.w, selectionRect.h);

                    // Start marching ants
                    startAnimation();
                    return;
                }

                // Finalize move/resize
                activeHandle = null;
                originalRect = null;
            },

            getCursor(coords) {
                if (creating || !coords) return 'crosshair';
                const handle = hitTestHandles(coords.x, coords.y);
                if (handle) return handleCursors[handle];
                if (insideSelectionRect(coords.x, coords.y)) return 'move';
                return 'crosshair';
            },

            prepareForAction(intent) {
                if (!selectionRect) return 'proceed';
                if (intent === 'undo' || intent === 'redo') {
                    cancelSelection();
                    return 'consumed';
                }
                if (intent === 'save' || intent === 'transform') commitSelection();
                return 'proceed';
            },
        };
    }

    // --- Public methods ---

    /**
     * Select all: create a selection covering the entire canvas.
     */
    function selectAll() {
        if (EditorCore.activeToolName !== 'select') return;

        const canvas = EditorCore.canvas;
        if (!canvas) return;

        // Commit any existing selection first
        if (!committed && selectionRect && selectionImageData) {
            commitSelection();
        }

        // Create selection covering entire canvas
        selectionRect = { x: 0, y: 0, w: canvas.width, h: canvas.height };
        captureSelection(0, 0, canvas.width, canvas.height);
        startAnimation();
    }

    /**
     * Copy the current selection's image data to the internal clipboard.
     */
    function copySelection() {
        if (EditorCore.activeToolName !== 'select') return;
        if (!selectionImageData) return;

        // Deep copy the ImageData
        clipboard = new ImageData(
            new Uint8ClampedArray(selectionImageData.data),
            selectionImageData.width,
            selectionImageData.height
        );
    }

    /**
     * Paste from the internal clipboard as a new floating selection.
     */
    function pasteSelection() {
        if (EditorCore.activeToolName !== 'select') return;
        if (!clipboard) return;

        const canvas = EditorCore.canvas;
        if (!canvas) return;

        // Commit any existing selection first
        if (!committed && selectionRect && selectionImageData) {
            commitSelection();
        }

        // Create a new floating selection at center of canvas
        const pw = clipboard.width;
        const ph = clipboard.height;
        const cx = Math.round((canvas.width - pw) / 2);
        const cy = Math.round((canvas.height - ph) / 2);

        // Save canvas state for cancel
        const ctx = EditorCore.ctx;
        originalCanvasData = EditorCore.getCurrentSnapshot() || ctx.getImageData(0, 0, canvas.width, canvas.height);

        // Deep copy clipboard data
        selectionImageData = new ImageData(
            new Uint8ClampedArray(clipboard.data),
            clipboard.width,
            clipboard.height
        );
        selectionSurface = document.createElement('canvas');
        selectionSurface.width = pw;
        selectionSurface.height = ph;
        selectionSurface.getContext('2d').putImageData(selectionImageData, 0, 0);

        selectionRect = { x: cx, y: cy, w: pw, h: ph };
        originalPosition = { x: cx, y: cy };
        committed = false;

        startAnimation();
    }

    /**
     * Delete the selected region (fill with white and commit).
     */
    function deleteSelection() {
        if (EditorCore.activeToolName !== 'select') return;
        if (!selectionRect) return;

        const canvas = EditorCore.canvas;
        const ctx = EditorCore.ctx;
        if (!canvas || !ctx) return;

        // If uncommitted, the region is already cleared on the main canvas
        // Just discard the image data and commit
        if (!committed) {
            // Region is already white (cleared during capture), just save state
            selectionImageData = null;
            committed = true;
            EditorCore.saveUndoState();
            clearSelection();
        } else {
            // Committed selection: delete to transparency.
            ctx.clearRect(
                Math.round(selectionRect.x),
                Math.round(selectionRect.y),
                Math.round(selectionRect.w),
                Math.round(selectionRect.h)
            );
            EditorCore.saveUndoState();
            clearSelection();
        }
    }

    /**
     * Confirm the current selection (commit it, triggered by Enter key).
     */
    function confirmSelection() {
        if (EditorCore.activeToolName !== 'select') return;
        if (!selectionRect || !selectionImageData || committed) return;
        commitSelection();
    }

    /**
     * Cancel the current selection (restore original, triggered by Escape key).
     */
    function cancelSelectionKey() {
        if (EditorCore.activeToolName !== 'select') return;
        if (!selectionRect) return;
        cancelSelection();
    }

    /**
     * Check if selection tool has an active selection (for shortcut routing).
     */
    function hasSelection() {
        return selectionRect != null;
    }

    return {
        create,
        selectAll,
        copySelection,
        pasteSelection,
        deleteSelection,
        confirmSelection,
        cancelSelectionKey,
        hasSelection,
    };
})();
