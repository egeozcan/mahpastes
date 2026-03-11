// --- Brush & Eraser Tools ---
// Freehand drawing tools that share stroke logic but differ in composite operation.

const BrushTool = (() => {
    /**
     * Create a brush or eraser tool.
     * @param {'source-over'|'destination-out'} compositeOp - Canvas composite operation
     * @param {string} cursorStyle - CSS cursor string
     * @returns {object} Tool implementing the EditorCore tool interface
     */
    function createStrokeTool(compositeOp, cursorStyle) {
        return {
            activate() {
                // No setup needed
            },

            deactivate() {
                // Restore composite operation in case drawing was interrupted
                const ctx = EditorCore.ctx;
                if (ctx) {
                    ctx.globalCompositeOperation = 'source-over';
                }
            },

            onMouseDown(coords, e) {
                const ctx = EditorCore.ctx;
                if (!ctx) return;

                ctx.globalCompositeOperation = compositeOp;
                ctx.beginPath();
                ctx.moveTo(coords.x, coords.y);
            },

            onMouseMove(coords, e) {
                const ctx = EditorCore.ctx;
                if (!ctx) return;

                ctx.save();
                ctx.globalCompositeOperation = compositeOp;
                ctx.globalAlpha = compositeOp === 'destination-out' ? 1 : EditorCore.currentOpacity;
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
                if (ctx) {
                    ctx.globalCompositeOperation = 'source-over';
                    ctx.globalAlpha = 1;
                }
                EditorCore.saveUndoState();
            },

            getCursor() {
                return cursorStyle;
            }
        };
    }

    /**
     * Create a brush tool (draws with current color).
     */
    function createBrush() {
        return createStrokeTool('source-over', 'crosshair');
    }

    /**
     * Create an eraser tool (removes pixels).
     */
    function createEraser() {
        return createStrokeTool('destination-out', 'crosshair');
    }

    return {
        createBrush,
        createEraser
    };
})();
