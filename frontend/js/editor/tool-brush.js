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
        let lastPoint = null;

        function restoreAt(coords) {
            const ctx = EditorCore.ctx;
            const canvas = EditorCore.canvas;
            if (!ctx || !canvas) return;

            const size = EditorCore.brushSize;
            const half = size / 2;
            const x = Math.round(coords.x - half);
            const y = Math.round(coords.y - half);
            const sx = Math.max(0, x);
            const sy = Math.max(0, y);
            const ex = Math.min(canvas.width, x + size);
            const ey = Math.min(canvas.height, y + size);
            const width = ex - sx;
            const height = ey - sy;
            if (width <= 0 || height <= 0) return;

            const baseline = EditorCore.getBaselineRegion(sx, sy, width, height);
            if (baseline) ctx.putImageData(baseline, sx, sy);
        }

        return {
            activate() { lastPoint = null; },
            deactivate() { lastPoint = null; },

            onMouseDown(coords) {
                // Preserve click-only gestures as no-ops, but retain the point
                // so the first delivered move can interpolate from it.
                lastPoint = { x: coords.x, y: coords.y };
            },

            onMouseMove(coords) {
                const previous = lastPoint || coords;
                const dx = coords.x - previous.x;
                const dy = coords.y - previous.y;
                const distance = Math.hypot(dx, dy);
                const spacing = Math.max(1, EditorCore.brushSize / 3);
                const steps = Math.max(1, Math.ceil(distance / spacing));
                for (let step = 1; step <= steps; step++) {
                    restoreAt({
                        x: previous.x + dx * step / steps,
                        y: previous.y + dy * step / steps,
                    });
                }
                lastPoint = { x: coords.x, y: coords.y };
            },

            onMouseUp() {
                lastPoint = null;
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
