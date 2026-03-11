// --- Crop Tool ---
// Freeform crop with aspect ratio presets, rule-of-thirds grid,
// marching-ants selection, and rotation/straightening.

const CropTool = (() => {
    // --- State ---
    let cropRect = null;        // {x, y, w, h} in canvas coordinates
    let activeHandle = null;    // null | 'nw' | 'n' | 'ne' | 'e' | 'se' | 's' | 'sw' | 'w' | 'move'
    let dragStartX = 0;
    let dragStartY = 0;
    let originalRect = null;    // Snapshot for drag calculation
    let aspectRatio = null;     // null = free, or number like 4/3
    let rotationAngle = 0;      // Degrees
    let marchingAntsOffset = 0;
    let animFrameId = null;

    const HANDLE_SIZE = 6;
    const HANDLE_HIT = 8;       // Hit-test tolerance in canvas pixels
    const MIN_SIZE = 10;        // Minimum crop dimension

    // --- Handle positions ---

    /**
     * Get the 8 handle positions (4 corners + 4 edge midpoints) for the current crop rect.
     * Returns an object keyed by handle name.
     */
    function getHandles() {
        if (!cropRect) return {};
        const { x, y, w, h } = cropRect;
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

    /**
     * Hit-test the handles. Returns the handle name or null.
     */
    function hitTestHandles(cx, cy) {
        const handles = getHandles();
        // Scale tolerance by current zoom so handles remain easy to grab
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

    /**
     * Test whether a point is inside the crop rect.
     */
    function insideCropRect(cx, cy) {
        if (!cropRect) return false;
        return cx >= cropRect.x && cx <= cropRect.x + cropRect.w &&
               cy >= cropRect.y && cy <= cropRect.y + cropRect.h;
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
        if (!oc || !overlayCanvas || !cropRect) return;

        const cw = overlayCanvas.width;
        const ch = overlayCanvas.height;
        const { x, y, w, h } = cropRect;

        oc.clearRect(0, 0, cw, ch);

        // 1. Semi-transparent dark overlay
        oc.fillStyle = 'rgba(28, 25, 23, 0.6)';
        oc.fillRect(0, 0, cw, ch);

        // 2. Clear the crop region so the image shows through
        oc.clearRect(x, y, w, h);

        // 3. Rule-of-thirds grid
        oc.strokeStyle = 'rgba(168, 162, 158, 0.4)';
        oc.lineWidth = 1;
        oc.setLineDash([]);
        oc.beginPath();
        // Vertical thirds
        oc.moveTo(x + w / 3, y);
        oc.lineTo(x + w / 3, y + h);
        oc.moveTo(x + 2 * w / 3, y);
        oc.lineTo(x + 2 * w / 3, y + h);
        // Horizontal thirds
        oc.moveTo(x, y + h / 3);
        oc.lineTo(x + w, y + h / 3);
        oc.moveTo(x, y + 2 * h / 3);
        oc.lineTo(x + w, y + 2 * h / 3);
        oc.stroke();

        // 4. Marching ants border
        oc.strokeStyle = '#ffffff';
        oc.lineWidth = 1;
        oc.setLineDash([10, 10]);
        oc.lineDashOffset = -marchingAntsOffset;
        oc.strokeRect(x, y, w, h);

        oc.strokeStyle = '#292524';
        oc.lineDashOffset = -(marchingAntsOffset + 10);
        oc.strokeRect(x, y, w, h);

        oc.setLineDash([]);
        oc.lineDashOffset = 0;

        // 5. Draw 8 handles
        const handles = getHandles();
        const half = HANDLE_SIZE / 2;
        for (const pos of Object.values(handles)) {
            oc.fillStyle = '#ffffff';
            oc.fillRect(pos.x - half, pos.y - half, HANDLE_SIZE, HANDLE_SIZE);
            oc.strokeStyle = '#292524';
            oc.lineWidth = 1;
            oc.strokeRect(pos.x - half, pos.y - half, HANDLE_SIZE, HANDLE_SIZE);
        }

        // 6. Dimensions label
        const label = Math.round(w) + ' × ' + Math.round(h);
        oc.font = '10px "IBM Plex Mono", monospace';
        oc.textAlign = 'center';
        const labelY = y + h + 18;
        const labelX = x + w / 2;

        // Background pill for readability
        const metrics = oc.measureText(label);
        const padX = 6;
        const padY = 3;
        oc.fillStyle = 'rgba(28, 25, 23, 0.7)';
        const pillW = metrics.width + padX * 2;
        const pillH = 14 + padY;
        const pillX = labelX - pillW / 2;
        const pillY = labelY - 10 - padY / 2;
        oc.beginPath();
        oc.roundRect(pillX, pillY, pillW, pillH, 3);
        oc.fill();

        oc.fillStyle = '#ffffff';
        oc.fillText(label, labelX, labelY);
    }

    // --- Marching ants animation ---

    function animateMarchingAnts() {
        marchingAntsOffset = (marchingAntsOffset + 0.5) % 20;
        renderOverlay();
        animFrameId = requestAnimationFrame(animateMarchingAnts);
    }

    function startAnimation() {
        if (animFrameId != null) return;
        animateMarchingAnts();
    }

    function stopAnimation() {
        if (animFrameId != null) {
            cancelAnimationFrame(animFrameId);
            animFrameId = null;
        }
    }

    // --- Aspect ratio enforcement ---

    /**
     * Constrain a rect to the current aspect ratio, anchoring from the opposite corner/edge
     * of the active handle.
     */
    function constrainToAspect(rect, handle) {
        if (!aspectRatio) return rect;
        const r = { ...rect };

        if (handle === 'move') return r;

        // For corner handles, constrain based on diagonal
        if (['nw', 'ne', 'sw', 'se'].includes(handle)) {
            // Fit within current w/h maintaining aspect ratio
            const currentRatio = r.w / r.h;
            if (currentRatio > aspectRatio) {
                // Too wide, shrink width
                const newW = r.h * aspectRatio;
                if (handle === 'nw' || handle === 'sw') {
                    r.x = r.x + r.w - newW;
                }
                r.w = newW;
            } else {
                // Too tall, shrink height
                const newH = r.w / aspectRatio;
                if (handle === 'nw' || handle === 'ne') {
                    r.y = r.y + r.h - newH;
                }
                r.h = newH;
            }
        } else if (handle === 'n' || handle === 's') {
            // Adjust width to match new height
            const newW = r.h * aspectRatio;
            const deltaW = newW - r.w;
            r.x -= deltaW / 2;
            r.w = newW;
        } else if (handle === 'e' || handle === 'w') {
            // Adjust height to match new width
            const newH = r.w / aspectRatio;
            const deltaH = newH - r.h;
            r.y -= deltaH / 2;
            r.h = newH;
        }

        return r;
    }

    // --- Clamping ---

    function clampRect(r) {
        const canvas = EditorCore.canvas;
        if (!canvas) return r;
        const cw = canvas.width;
        const ch = canvas.height;

        r.x = Math.max(0, Math.min(r.x, cw - MIN_SIZE));
        r.y = Math.max(0, Math.min(r.y, ch - MIN_SIZE));
        r.w = Math.max(MIN_SIZE, Math.min(r.w, cw - r.x));
        r.h = Math.max(MIN_SIZE, Math.min(r.h, ch - r.y));

        return r;
    }

    // --- Tool interface ---

    function create() {
        return {
            activate() {
                const canvas = EditorCore.canvas;
                if (!canvas) return;

                // Initialize crop rect to center 80% of canvas
                const margin = 0.1;
                cropRect = {
                    x: canvas.width * margin,
                    y: canvas.height * margin,
                    w: canvas.width * (1 - 2 * margin),
                    h: canvas.height * (1 - 2 * margin),
                };

                // If aspect ratio is set, constrain initial rect
                if (aspectRatio) {
                    cropRect = constrainToAspect(cropRect, 'se');
                    // Re-center after constraining
                    cropRect.x = (canvas.width - cropRect.w) / 2;
                    cropRect.y = (canvas.height - cropRect.h) / 2;
                }

                rotationAngle = 0;
                activeHandle = null;

                // Reset UI controls
                const ratioSelect = document.getElementById('editor-crop-ratio');
                if (ratioSelect) ratioSelect.value = aspectRatio ? ratioToString(aspectRatio) : 'free';
                const rotateSlider = document.getElementById('editor-crop-rotate');
                if (rotateSlider) rotateSlider.value = '0';
                const rotateValue = document.getElementById('editor-crop-rotate-value');
                if (rotateValue) rotateValue.textContent = '0°';

                startAnimation();
            },

            deactivate() {
                stopAnimation();

                // Clear overlay
                const oc = EditorCore.overlayCtx;
                const overlayCanvas = EditorCore.overlayCanvas;
                if (oc && overlayCanvas) {
                    oc.clearRect(0, 0, overlayCanvas.width, overlayCanvas.height);
                }

                cropRect = null;
                activeHandle = null;
                originalRect = null;
                rotationAngle = 0;
                aspectRatio = null;
            },

            onMouseDown(coords, e) {
                if (!cropRect) return;

                dragStartX = coords.x;
                dragStartY = coords.y;

                // Hit-test handles first
                const handle = hitTestHandles(coords.x, coords.y);
                if (handle) {
                    activeHandle = handle;
                    originalRect = { ...cropRect };
                    return;
                }

                // Inside crop rect → move
                if (insideCropRect(coords.x, coords.y)) {
                    activeHandle = 'move';
                    originalRect = { ...cropRect };
                    return;
                }

                // Outside → start new crop rect
                activeHandle = 'se';
                cropRect = {
                    x: coords.x,
                    y: coords.y,
                    w: 1,
                    h: 1,
                };
                originalRect = { ...cropRect };
                dragStartX = coords.x;
                dragStartY = coords.y;
            },

            onMouseMove(coords, e) {
                if (!activeHandle || !originalRect) {
                    // Update cursor based on hover position
                    EditorCore.updateCursor();
                    return;
                }

                const dx = coords.x - dragStartX;
                const dy = coords.y - dragStartY;

                if (activeHandle === 'move') {
                    const canvas = EditorCore.canvas;
                    const maxX = canvas ? canvas.width - originalRect.w : Infinity;
                    const maxY = canvas ? canvas.height - originalRect.h : Infinity;
                    cropRect = {
                        x: Math.max(0, Math.min(originalRect.x + dx, maxX)),
                        y: Math.max(0, Math.min(originalRect.y + dy, maxY)),
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

                    // Enforce aspect ratio
                    r = constrainToAspect(r, activeHandle);

                    // Clamp to canvas bounds
                    cropRect = clampRect(r);
                }

                // Overlay is redrawn by the marching ants animation loop
            },

            onMouseUp(coords, e) {
                if (!cropRect) return;

                // Ensure minimum size
                if (cropRect.w < MIN_SIZE) cropRect.w = MIN_SIZE;
                if (cropRect.h < MIN_SIZE) cropRect.h = MIN_SIZE;

                // Re-enforce aspect ratio after clamping
                if (aspectRatio && activeHandle && activeHandle !== 'move') {
                    cropRect = constrainToAspect(cropRect, activeHandle);
                    cropRect = clampRect(cropRect);
                }

                activeHandle = null;
                originalRect = null;
            },

            getCursor() {
                // Dynamic cursor based on what's under the mouse
                // We can't easily get mouse position here, so return crosshair as default
                // The actual cursor changes happen in handleMouseMove via canvas style
                return 'crosshair';
            },
        };
    }

    /**
     * Convert a numeric aspect ratio back to a string for the select element.
     */
    function ratioToString(ratio) {
        const known = {
            1: '1:1',
            [4/3]: '4:3',
            [3/2]: '3:2',
            [16/9]: '16:9',
            [3/4]: '4:3',  // swapped
            [2/3]: '3:2',
            [9/16]: '16:9',
        };
        return known[ratio] || 'free';
    }

    // --- Crop execution ---

    function confirm() {
        if (!cropRect) return;

        const canvas = EditorCore.canvas;
        const ctx = EditorCore.ctx;
        const overlay = EditorCore.overlayCanvas;
        if (!canvas || !ctx) return;

        stopAnimation();

        const { x, y, w, h } = cropRect;
        const rx = Math.round(x);
        const ry = Math.round(y);
        const rw = Math.round(w);
        const rh = Math.round(h);

        let source = canvas;

        // If rotation is applied, create rotated intermediate canvas
        if (rotationAngle !== 0) {
            const radians = rotationAngle * Math.PI / 180;
            const tmpCanvas = document.createElement('canvas');
            tmpCanvas.width = canvas.width;
            tmpCanvas.height = canvas.height;
            const tmpCtx = tmpCanvas.getContext('2d');

            tmpCtx.translate(canvas.width / 2, canvas.height / 2);
            tmpCtx.rotate(radians);
            tmpCtx.translate(-canvas.width / 2, -canvas.height / 2);
            tmpCtx.drawImage(canvas, 0, 0);

            source = tmpCanvas;
        }

        // Create new canvas at crop rect dimensions
        const cropCanvas = document.createElement('canvas');
        cropCanvas.width = rw;
        cropCanvas.height = rh;
        const cropCtx = cropCanvas.getContext('2d');

        // Draw the relevant portion of the source onto the new canvas
        cropCtx.drawImage(source, rx, ry, rw, rh, 0, 0, rw, rh);

        // Resize EditorCore's canvases to new dimensions
        canvas.width = rw;
        canvas.height = rh;
        if (overlay) {
            overlay.width = rw;
            overlay.height = rh;
        }

        // Draw the cropped image onto the main canvas
        ctx.drawImage(cropCanvas, 0, 0);

        // Sync overlay and save undo state
        EditorCore.syncOverlay();
        EditorCore.saveUndoState();

        // Reset crop state
        cropRect = null;
        activeHandle = null;
        originalRect = null;
        rotationAngle = 0;

        // Clear overlay
        const oc = EditorCore.overlayCtx;
        if (oc && overlay) {
            oc.clearRect(0, 0, overlay.width, overlay.height);
        }

        // Switch back to brush tool
        if (typeof selectTool === 'function') {
            selectTool('brush');
            if (typeof updateToolButtons === 'function') updateToolButtons();
        }

        // Update zoom display
        if (typeof ZoomTool !== 'undefined') {
            ZoomTool.updateZoomDisplay();
        }
    }

    function cancel() {
        stopAnimation();

        // Clear overlay
        const oc = EditorCore.overlayCtx;
        const overlayCanvas = EditorCore.overlayCanvas;
        if (oc && overlayCanvas) {
            oc.clearRect(0, 0, overlayCanvas.width, overlayCanvas.height);
        }

        // Reset state
        cropRect = null;
        activeHandle = null;
        originalRect = null;
        rotationAngle = 0;

        // Switch back to brush tool
        if (typeof selectTool === 'function') {
            selectTool('brush');
            if (typeof updateToolButtons === 'function') updateToolButtons();
        }
    }

    function setAspectRatio(ratio) {
        aspectRatio = ratio;

        // Re-constrain existing crop rect if one exists
        if (cropRect && ratio) {
            cropRect = constrainToAspect(cropRect, 'se');
            cropRect = clampRect(cropRect);
        }
    }

    function swapAspectRatio() {
        if (!aspectRatio) return;
        aspectRatio = 1 / aspectRatio;

        // Re-constrain existing crop rect
        if (cropRect) {
            cropRect = constrainToAspect(cropRect, 'se');
            cropRect = clampRect(cropRect);
        }
    }

    function setRotation(degrees) {
        rotationAngle = degrees;
    }

    return {
        create,
        confirm,
        cancel,
        setAspectRatio,
        swapAspectRatio,
        setRotation,
    };
})();
