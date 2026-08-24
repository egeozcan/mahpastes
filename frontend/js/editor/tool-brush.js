// --- Brush & Eraser Tools ---
// Freehand drawing tool and an eraser that restores original image pixels.

const BrushTool = (() => {
    /**
     * Create a brush tool (draws with current color).
     *
     * A stroke is accumulated at full opacity on an offscreen buffer and
     * composited onto the canvas once, on mouse up. Stroking each segment
     * straight onto the canvas with globalAlpha would let a slow stroke paint
     * over itself and darken wherever consecutive segments overlap, so a
     * 40%-opacity stroke came out mottled rather than evenly translucent.
     * While the gesture is live the buffer is previewed on the overlay canvas
     * at the target opacity, so what the user sees is what gets committed.
     */
    function createBrush() {
        let strokeCanvas = null;
        let strokeCtx = null;
        let strokeActive = false;

        function beginStrokeBuffer() {
            const canvas = EditorCore.canvas;
            if (!canvas) return null;

            if (!strokeCanvas) {
                strokeCanvas = document.createElement('canvas');
                strokeCtx = strokeCanvas.getContext('2d');
            }
            if (strokeCanvas.width !== canvas.width || strokeCanvas.height !== canvas.height) {
                // Assigning either dimension also clears the buffer.
                strokeCanvas.width = canvas.width;
                strokeCanvas.height = canvas.height;
            } else {
                strokeCtx.clearRect(0, 0, strokeCanvas.width, strokeCanvas.height);
            }
            return strokeCtx;
        }

        function clearOverlay() {
            const overlayCtx = EditorCore.overlayCtx;
            const overlayCanvas = EditorCore.overlayCanvas;
            if (overlayCtx && overlayCanvas) {
                overlayCtx.clearRect(0, 0, overlayCanvas.width, overlayCanvas.height);
            }
        }

        function previewStroke() {
            const overlayCtx = EditorCore.overlayCtx;
            const overlayCanvas = EditorCore.overlayCanvas;
            if (!overlayCtx || !overlayCanvas || !strokeCanvas) return;
            overlayCtx.clearRect(0, 0, overlayCanvas.width, overlayCanvas.height);
            overlayCtx.save();
            overlayCtx.globalAlpha = EditorCore.currentOpacity;
            overlayCtx.drawImage(strokeCanvas, 0, 0);
            overlayCtx.restore();
        }

        function discardStroke() {
            strokeActive = false;
            if (strokeCtx && strokeCanvas) {
                strokeCtx.clearRect(0, 0, strokeCanvas.width, strokeCanvas.height);
            }
            clearOverlay();
        }

        return {
            activate() {},
            deactivate() {
                discardStroke();
                strokeCanvas = null;
                strokeCtx = null;
                const ctx = EditorCore.ctx;
                if (ctx) ctx.globalCompositeOperation = 'source-over';
            },

            onMouseDown(coords, e) {
                const buffer = beginStrokeBuffer();
                if (!buffer) return;
                strokeActive = true;
                buffer.globalCompositeOperation = 'source-over';
                buffer.beginPath();
                buffer.moveTo(coords.x, coords.y);
            },

            onMouseMove(coords, e) {
                if (!strokeActive || !strokeCtx) return;
                strokeCtx.save();
                strokeCtx.strokeStyle = EditorCore.currentColor;
                strokeCtx.lineWidth = EditorCore.brushSize;
                strokeCtx.lineCap = 'round';
                strokeCtx.lineJoin = 'round';
                strokeCtx.lineTo(coords.x, coords.y);
                strokeCtx.stroke();
                strokeCtx.beginPath();
                strokeCtx.moveTo(coords.x, coords.y);
                strokeCtx.restore();
                previewStroke();
            },

            onMouseUp(coords, e) {
                const ctx = EditorCore.ctx;
                if (strokeActive && ctx && strokeCanvas) {
                    ctx.save();
                    ctx.globalAlpha = EditorCore.currentOpacity;
                    ctx.globalCompositeOperation = 'source-over';
                    ctx.drawImage(strokeCanvas, 0, 0);
                    ctx.restore();
                }
                discardStroke();
                EditorCore.saveUndoState();
            },

            cancel() {
                discardStroke();
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
        let erased = false;

        function restoreAt(coords) {
            const size = EditorCore.brushSize;
            const half = size / 2;
            if (EditorCore.restoreBaselineRegion(coords.x - half, coords.y - half, size, size)) {
                erased = true;
            }
        }

        return {
            activate() { lastPoint = null; erased = false; },
            deactivate() { lastPoint = null; },

            onMouseDown(coords) {
                // Preserve click-only gestures as no-ops, but retain the point
                // so the first delivered move can interpolate from it.
                lastPoint = { x: coords.x, y: coords.y };
                erased = false;
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
                erased = false;
                EditorCore.saveUndoState();
            },

            // Erased pixels are already gone from the canvas, so an aborted
            // gesture commits what it removed rather than losing it from the
            // undo history.
            cancel() {
                lastPoint = null;
                if (erased) EditorCore.saveUndoState();
                erased = false;
            },

            getCursor() { return 'crosshair'; }
        };
    }

    return {
        createBrush,
        createEraser
    };
})();
