// --- Anonymize Tool ---
// Pixelate or blur regions with brush and rectangle modes.

const AnonymizeTool = (() => {
    // --- State ---
    let mode = 'brush';      // 'brush' or 'rect'
    let effect = 'pixelate'; // 'pixelate' or 'blur'

    // --- Pixelate function ---

    function pixelateRegion(ctx, x, y, w, h, blockSize) {
        x = Math.max(0, Math.round(x));
        y = Math.max(0, Math.round(y));
        w = Math.min(ctx.canvas.width - x, Math.round(w));
        h = Math.min(ctx.canvas.height - y, Math.round(h));
        if (w <= 0 || h <= 0) return;

        const imageData = ctx.getImageData(x, y, w, h);
        const data = imageData.data;

        for (let by = 0; by < h; by += blockSize) {
            for (let bx = 0; bx < w; bx += blockSize) {
                let r = 0, g = 0, b = 0, count = 0;
                for (let dy = 0; dy < blockSize && by + dy < h; dy++) {
                    for (let dx = 0; dx < blockSize && bx + dx < w; dx++) {
                        const i = ((by + dy) * w + (bx + dx)) * 4;
                        r += data[i]; g += data[i+1]; b += data[i+2]; count++;
                    }
                }
                r = Math.round(r / count); g = Math.round(g / count); b = Math.round(b / count);
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

    // --- Blur function ---

    function blurRegion(ctx, x, y, w, h, radius) {
        x = Math.max(0, Math.round(x));
        y = Math.max(0, Math.round(y));
        w = Math.min(ctx.canvas.width - x, Math.round(w));
        h = Math.min(ctx.canvas.height - y, Math.round(h));
        if (w <= 0 || h <= 0) return;

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

    // --- Apply effect at a region ---

    function applyEffect(ctx, x, y, w, h) {
        const size = EditorCore.brushSize;
        if (effect === 'pixelate') {
            pixelateRegion(ctx, x, y, w, h, Math.max(2, size));
        } else {
            blurRegion(ctx, x, y, w, h, Math.max(1, size));
        }
    }

    // --- Apply effect at brush position ---

    function applyAtCursor(ctx, cx, cy) {
        const size = EditorCore.brushSize;
        const halfSize = size * 2; // Brush area is larger than block size for visible effect
        const x = cx - halfSize;
        const y = cy - halfSize;
        applyEffect(ctx, x, y, halfSize * 2, halfSize * 2);
    }

    // --- Tool factory ---

    function create() {
        return {
            activate() {
                // No setup needed
            },

            deactivate() {
                // Clear any lingering overlay preview
                const oc = EditorCore.overlayCtx;
                const overlayCanvas = EditorCore.overlayCanvas;
                if (oc && overlayCanvas) {
                    oc.clearRect(0, 0, overlayCanvas.width, overlayCanvas.height);
                }
            },

            onMouseDown(coords, e) {
                const ctx = EditorCore.ctx;
                if (!ctx) return;

                if (mode === 'brush') {
                    applyAtCursor(ctx, coords.x, coords.y);
                }
                // For rect mode, start point is already recorded by EditorCore
            },

            onMouseMove(coords, e) {
                const ctx = EditorCore.ctx;
                if (!ctx) return;

                if (mode === 'brush') {
                    applyAtCursor(ctx, coords.x, coords.y);
                } else {
                    // Rectangle mode: draw preview on overlay
                    const oc = EditorCore.overlayCtx;
                    const overlayCanvas = EditorCore.overlayCanvas;
                    if (!oc || !overlayCanvas) return;

                    const sx = EditorCore.startX;
                    const sy = EditorCore.startY;

                    oc.clearRect(0, 0, overlayCanvas.width, overlayCanvas.height);

                    // Dashed outline preview
                    oc.strokeStyle = '#44403c';
                    oc.lineWidth = 1.5;
                    oc.setLineDash([6, 4]);
                    oc.beginPath();
                    oc.rect(
                        Math.min(sx, coords.x), Math.min(sy, coords.y),
                        Math.abs(coords.x - sx), Math.abs(coords.y - sy)
                    );
                    oc.stroke();
                    oc.setLineDash([]);
                }
            },

            onMouseUp(coords, e) {
                const ctx = EditorCore.ctx;
                if (!ctx) return;

                if (mode === 'rect') {
                    // Clear overlay
                    const oc = EditorCore.overlayCtx;
                    const overlayCanvas = EditorCore.overlayCanvas;
                    if (oc && overlayCanvas) {
                        oc.clearRect(0, 0, overlayCanvas.width, overlayCanvas.height);
                    }

                    // Apply effect to the dragged rectangle
                    const sx = EditorCore.startX;
                    const sy = EditorCore.startY;
                    const rx = Math.min(sx, coords.x);
                    const ry = Math.min(sy, coords.y);
                    const rw = Math.abs(coords.x - sx);
                    const rh = Math.abs(coords.y - sy);

                    if (rw > 1 && rh > 1) {
                        applyEffect(ctx, rx, ry, rw, rh);
                    }
                }

                EditorCore.saveUndoState();
            },

            getCursor() {
                return 'crosshair';
            }
        };
    }

    // --- Public API ---

    function setMode(m) {
        mode = m;
    }

    function setEffect(e) {
        effect = e;
    }

    return {
        create,
        setMode,
        setEffect
    };
})();
