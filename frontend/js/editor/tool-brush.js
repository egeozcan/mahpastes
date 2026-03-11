// --- Brush & Eraser Tools ---
// Freehand drawing tool and an eraser that restores original image pixels.

const BrushTool = (() => {
    /**
     * Create a brush tool (draws with current color).
     */
    function createBrush() {
        return {
            activate() {},
            deactivate() {
                const ctx = EditorCore.ctx;
                if (ctx) ctx.globalCompositeOperation = 'source-over';
            },

            onMouseDown(coords, e) {
                const ctx = EditorCore.ctx;
                if (!ctx) return;
                ctx.globalCompositeOperation = 'source-over';
                ctx.beginPath();
                ctx.moveTo(coords.x, coords.y);
            },

            onMouseMove(coords, e) {
                const ctx = EditorCore.ctx;
                if (!ctx) return;
                ctx.save();
                ctx.globalAlpha = EditorCore.currentOpacity;
                ctx.strokeStyle = EditorCore.currentColor;
                ctx.lineWidth = EditorCore.brushSize;
                ctx.lineCap = 'round';
                ctx.lineJoin = 'round';
                ctx.lineTo(coords.x, coords.y);
                ctx.stroke();
                ctx.beginPath();
                ctx.moveTo(coords.x, coords.y);
                ctx.restore();
            },

            onMouseUp(coords, e) {
                const ctx = EditorCore.ctx;
                if (ctx) ctx.globalAlpha = 1;
                EditorCore.saveUndoState();
            },

            getCursor() { return 'crosshair'; }
        };
    }

    /**
     * Create an eraser tool that restores the original (first undo state)
     * image pixels, effectively removing only drawn annotations.
     */
    function createEraser() {
        let baseImageData = null;

        return {
            activate() {
                // Snapshot the base image (first undo entry = original loaded image)
                const stack = EditorCore.undoStack;
                if (stack.length > 0) {
                    baseImageData = stack[0].imageData;
                }
            },

            deactivate() {
                baseImageData = null;
            },

            onMouseDown(coords, e) {
                const ctx = EditorCore.ctx;
                if (!ctx) return;
                ctx.beginPath();
                ctx.moveTo(coords.x, coords.y);
            },

            onMouseMove(coords, e) {
                const ctx = EditorCore.ctx;
                if (!ctx || !baseImageData) return;

                const size = EditorCore.brushSize;
                const half = size / 2;
                const x = Math.round(coords.x - half);
                const y = Math.round(coords.y - half);

                // Clip to canvas bounds
                const sx = Math.max(0, x);
                const sy = Math.max(0, y);
                const ex = Math.min(EditorCore.canvas.width, x + size);
                const ey = Math.min(EditorCore.canvas.height, y + size);
                const w = ex - sx;
                const h = ey - sy;
                if (w <= 0 || h <= 0) return;

                // Copy the corresponding region from the base image
                const baseW = baseImageData.width;
                const current = ctx.getImageData(sx, sy, w, h);
                const cd = current.data;
                const bd = baseImageData.data;

                for (let row = 0; row < h; row++) {
                    for (let col = 0; col < w; col++) {
                        const ci = (row * w + col) * 4;
                        const bi = ((sy + row) * baseW + (sx + col)) * 4;
                        cd[ci]     = bd[bi];
                        cd[ci + 1] = bd[bi + 1];
                        cd[ci + 2] = bd[bi + 2];
                        cd[ci + 3] = bd[bi + 3];
                    }
                }

                ctx.putImageData(current, sx, sy);
            },

            onMouseUp(coords, e) {
                EditorCore.saveUndoState();
            },

            getCursor() { return 'crosshair'; }
        };
    }

    return {
        createBrush,
        createEraser
    };
})();
