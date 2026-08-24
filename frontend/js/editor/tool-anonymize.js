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

    /**
     * Clip a requested region to the canvas. Clamping the origin alone would
     * slide the region inward instead of trimming it, so a brush dab near the
     * left or top edge would reach further right/down than the cursor.
     */
    function clipRegion(ctx, x, y, w, h) {
        const left = Math.round(x);
        const top = Math.round(y);
        const right = Math.min(ctx.canvas.width, left + Math.round(w));
        const bottom = Math.min(ctx.canvas.height, top + Math.round(h));
        const clippedX = Math.max(0, left);
        const clippedY = Math.max(0, top);
        return {
            x: clippedX,
            y: clippedY,
            w: right - clippedX,
            h: bottom - clippedY,
        };
    }

    // --- Pixelate function ---

    function pixelateRegion(ctx, x, y, w, h, blockSize) {
        ({ x, y, w, h } = clipRegion(ctx, x, y, w, h));
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
        ({ x, y, w, h } = clipRegion(ctx, x, y, w, h));
        if (w <= 0 || h <= 0) return;

        const imageData = ctx.getImageData(x, y, w, h);
        const src = imageData.data;
        const horizontal = new Uint8ClampedArray(src.length);
        const dst = new Uint8ClampedArray(src.length);
        const r = Math.max(1, Math.round(radius));

        // Sliding-window box blur: O(width * height), independent of radius.
        // RGB is averaged in premultiplied-alpha space to avoid dark or colored
        // fringes around transparent pixels.
        const addSourcePixel = (sums, index, direction) => {
            const alpha = src[index + 3];
            const alphaScale = alpha / 255;
            sums.r += direction * src[index] * alphaScale;
            sums.g += direction * src[index + 1] * alphaScale;
            sums.b += direction * src[index + 2] * alphaScale;
            sums.a += direction * alpha;
        };
        for (let row = 0; row < h; row++) {
            const sums = { r: 0, g: 0, b: 0, a: 0 };
            let count = 0;
            for (let col = 0; col <= Math.min(r, w - 1); col++) {
                addSourcePixel(sums, (row * w + col) * 4, 1);
                count++;
            }
            for (let col = 0; col < w; col++) {
                const i = (row * w + col) * 4;
                horizontal[i] = sums.r / count;
                horizontal[i + 1] = sums.g / count;
                horizontal[i + 2] = sums.b / count;
                horizontal[i + 3] = sums.a / count;
                const remove = col - r;
                const add = col + r + 1;
                if (remove >= 0) {
                    addSourcePixel(sums, (row * w + remove) * 4, -1);
                    count--;
                }
                if (add < w) {
                    addSourcePixel(sums, (row * w + add) * 4, 1);
                    count++;
                }
            }
        }

        for (let col = 0; col < w; col++) {
            let rr = 0, gg = 0, bb = 0, aa = 0;
            let count = 0;
            for (let row = 0; row <= Math.min(r, h - 1); row++) {
                const i = (row * w + col) * 4;
                rr += horizontal[i]; gg += horizontal[i + 1]; bb += horizontal[i + 2]; aa += horizontal[i + 3]; count++;
            }
            for (let row = 0; row < h; row++) {
                const i = (row * w + col) * 4;
                const alpha = aa / count;
                dst[i + 3] = alpha;
                if (alpha > 0) {
                    dst[i] = (rr / count) * 255 / alpha;
                    dst[i + 1] = (gg / count) * 255 / alpha;
                    dst[i + 2] = (bb / count) * 255 / alpha;
                } else {
                    dst[i] = 0; dst[i + 1] = 0; dst[i + 2] = 0;
                }
                const remove = row - r;
                const add = row + r + 1;
                if (remove >= 0) {
                    const j = (remove * w + col) * 4;
                    rr -= horizontal[j]; gg -= horizontal[j + 1]; bb -= horizontal[j + 2]; aa -= horizontal[j + 3]; count--;
                }
                if (add < h) {
                    const j = (add * w + col) * 4;
                    rr += horizontal[j]; gg += horizontal[j + 1]; bb += horizontal[j + 2]; aa += horizontal[j + 3]; count++;
                }
            }
        }

        const noiseAmp = Math.max(6, r);
        for (let i = 0; i < dst.length; i += 4) {
            if (dst[i + 3] === 0) continue;
            dst[i] = clamp(dst[i] + noise(noiseAmp));
            dst[i + 1] = clamp(dst[i + 1] + noise(noiseAmp));
            dst[i + 2] = clamp(dst[i + 2] + noise(noiseAmp));
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
        let lastBrushPoint = null;
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
                    lastBrushPoint = { x: coords.x, y: coords.y };
                }
                // For rect mode, start point is already recorded by EditorCore
            },

            onMouseMove(coords, e) {
                const ctx = EditorCore.ctx;
                if (!ctx) return;

                if (mode === 'brush') {
                    const previous = lastBrushPoint || coords;
                    const dx = coords.x - previous.x;
                    const dy = coords.y - previous.y;
                    const distance = Math.hypot(dx, dy);
                    const spacing = Math.max(2, EditorCore.brushSize);
                    const steps = Math.max(1, Math.ceil(distance / spacing));
                    for (let step = 1; step <= steps; step++) {
                        applyAtCursor(ctx, previous.x + dx * step / steps, previous.y + dy * step / steps);
                    }
                    lastBrushPoint = { x: coords.x, y: coords.y };
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

                lastBrushPoint = null;
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
