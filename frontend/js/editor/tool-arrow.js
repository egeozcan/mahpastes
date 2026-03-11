// --- Arrow Tool ---
// Draws a line with an arrowhead at the endpoint. Uses Shift+snap for 45-degree
// angles via ShapeTools.snapTo45(). Previews on the overlay canvas during drag.

const ArrowTool = (() => {
    /**
     * Draw an arrowhead at the endpoint of a line.
     * @param {CanvasRenderingContext2D} ctx - Canvas context
     * @param {number} x1 - Start X
     * @param {number} y1 - Start Y
     * @param {number} x2 - End X (arrowhead tip)
     * @param {number} y2 - End Y (arrowhead tip)
     * @param {number} size - Brush size (arrowhead scales with this)
     */
    function drawArrowhead(ctx, x1, y1, x2, y2, size) {
        const angle = Math.atan2(y2 - y1, x2 - x1);
        const headLen = Math.max(size * 2, 10);
        ctx.beginPath();
        ctx.moveTo(x2, y2);
        ctx.lineTo(x2 - headLen * Math.cos(angle - Math.PI / 6), y2 - headLen * Math.sin(angle - Math.PI / 6));
        ctx.moveTo(x2, y2);
        ctx.lineTo(x2 - headLen * Math.cos(angle + Math.PI / 6), y2 - headLen * Math.sin(angle + Math.PI / 6));
        ctx.stroke();
    }

    /**
     * Create an arrow tool instance.
     * @returns {object} Tool implementing the EditorCore tool interface
     */
    function create() {
        return {
            activate() {
                // No setup needed
            },

            deactivate() {
                // Clear any lingering preview
                const oc = EditorCore.overlayCtx;
                const overlayCanvas = EditorCore.overlayCanvas;
                if (oc && overlayCanvas) {
                    oc.clearRect(0, 0, overlayCanvas.width, overlayCanvas.height);
                }
            },

            onMouseDown(coords, e) {
                // Start point is already set by EditorCore.handleMouseDown
            },

            onMouseMove(coords, e) {
                const oc = EditorCore.overlayCtx;
                const overlayCanvas = EditorCore.overlayCanvas;
                if (!oc || !overlayCanvas) return;

                const sx = EditorCore.startX;
                const sy = EditorCore.startY;

                let finalX = coords.x, finalY = coords.y;
                if (e.shiftKey) {
                    const snapped = ShapeTools.snapTo45(sx, sy, coords.x, coords.y);
                    finalX = snapped.x;
                    finalY = snapped.y;
                }

                oc.clearRect(0, 0, overlayCanvas.width, overlayCanvas.height);
                oc.save();
                oc.globalAlpha = EditorCore.currentOpacity * 0.5;
                oc.strokeStyle = EditorCore.currentColor;
                oc.lineWidth = EditorCore.brushSize;
                oc.lineCap = 'round';
                oc.setLineDash([5, 5]);

                // Draw dashed line preview
                oc.beginPath();
                oc.moveTo(sx, sy);
                oc.lineTo(finalX, finalY);
                oc.stroke();

                // Draw arrowhead preview
                drawArrowhead(oc, sx, sy, finalX, finalY, EditorCore.brushSize);

                oc.setLineDash([]);
                oc.restore();
            },

            onMouseUp(coords, e) {
                // Clear overlay preview
                const oc = EditorCore.overlayCtx;
                const overlayCanvas = EditorCore.overlayCanvas;
                if (oc && overlayCanvas) {
                    oc.clearRect(0, 0, overlayCanvas.width, overlayCanvas.height);
                }

                const ctx = EditorCore.ctx;
                if (!ctx) return;

                const sx = EditorCore.startX;
                const sy = EditorCore.startY;

                let finalX = coords.x, finalY = coords.y;
                if (e.shiftKey) {
                    const snapped = ShapeTools.snapTo45(sx, sy, coords.x, coords.y);
                    finalX = snapped.x;
                    finalY = snapped.y;
                }

                // Draw final arrow on main canvas
                ctx.save();
                ctx.globalAlpha = EditorCore.currentOpacity;
                ctx.strokeStyle = EditorCore.currentColor;
                ctx.lineWidth = EditorCore.brushSize;
                ctx.lineCap = 'round';

                // Line
                ctx.beginPath();
                ctx.moveTo(sx, sy);
                ctx.lineTo(finalX, finalY);
                ctx.stroke();

                // Arrowhead
                drawArrowhead(ctx, sx, sy, finalX, finalY, EditorCore.brushSize);

                ctx.restore();
                EditorCore.saveUndoState();
            },

            getCursor() {
                return 'crosshair';
            }
        };
    }

    return {
        create
    };
})();
