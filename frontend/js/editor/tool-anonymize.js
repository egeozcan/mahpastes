// --- Anonymize Tool ---
// Pixelate or blur regions with brush and rectangle modes.

const AnonymizeTool = (() => {
    // --- State ---
    let mode = 'brush';      // 'brush' or 'rect'
    let effect = 'pixelate'; // 'pixelate' or 'blur'

    // --- Noise helper (adds per-pixel jitter to destroy reversibility) ---

    function noise(amplitude) {
        return Math.round((Math.random() - 0.5) * 2 * amplitude);
    }

    function clamp(v) {
        return v < 0 ? 0 : v > 255 ? 255 : v;
    }

    // --- Pixelate function ---

    function pixelateRegion(ctx, x, y, w, h, blockSize) {
        x = Math.max(0, Math.round(x));
        y = Math.max(0, Math.round(y));
        w = Math.min(ctx.canvas.width - x, Math.round(w));
        h = Math.min(ctx.canvas.height - y, Math.round(h));
        if (w <= 0 || h <= 0) return;

        const imageData = ctx.getImageData(x, y, w, h);
        const data = imageData.data;
        const noiseAmp = Math.max(8, blockSize * 2);

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
                        data[i]   = clamp(r + noise(noiseAmp));
                        data[i+1] = clamp(g + noise(noiseAmp));
                        data[i+2] = clamp(b + noise(noiseAmp));
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

        const imageData = ctx.getImageData(x, y, w, h);
        const src = imageData.data;
        const dst = new Uint8ClampedArray(src);
        const r = Math.max(1, Math.round(radius));

        // Horizontal pass
        for (let row = 0; row < h; row++) {
            for (let col = 0; col < w; col++) {
                let rr = 0, gg = 0, bb = 0, aa = 0, count = 0;
                for (let k = -r; k <= r; k++) {
                    const c = col + k;
                    if (c < 0 || c >= w) continue;
                    const i = (row * w + c) * 4;
                    rr += src[i]; gg += src[i+1]; bb += src[i+2]; aa += src[i+3]; count++;
                }
                const i = (row * w + col) * 4;
                dst[i] = rr / count; dst[i+1] = gg / count; dst[i+2] = bb / count; dst[i+3] = aa / count;
            }
        }

        // Vertical pass (read from dst, write back to dst via a copy)
        const src2 = new Uint8ClampedArray(dst);
        for (let col = 0; col < w; col++) {
            for (let row = 0; row < h; row++) {
                let rr = 0, gg = 0, bb = 0, aa = 0, count = 0;
                for (let k = -r; k <= r; k++) {
                    const ro = row + k;
                    if (ro < 0 || ro >= h) continue;
                    const i = (ro * w + col) * 4;
                    rr += src2[i]; gg += src2[i+1]; bb += src2[i+2]; aa += src2[i+3]; count++;
                }
                const i = (row * w + col) * 4;
                dst[i] = rr / count; dst[i+1] = gg / count; dst[i+2] = bb / count; dst[i+3] = aa / count;
            }
        }

        // Add per-pixel noise to destroy reversibility
        const noiseAmp = Math.max(6, r);
        for (let i = 0; i < dst.length; i += 4) {
            dst[i]   = clamp(dst[i]   + noise(noiseAmp));
            dst[i+1] = clamp(dst[i+1] + noise(noiseAmp));
            dst[i+2] = clamp(dst[i+2] + noise(noiseAmp));
        }

        imageData.data.set(dst);
        ctx.putImageData(imageData, x, y);
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
