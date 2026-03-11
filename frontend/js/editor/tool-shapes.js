// --- Shape Tools (Line, Rectangle, Circle) ---
// Shape tools draw previews on the overlay canvas during drag, then commit
// the final shape to the main canvas on mouse up.

const ShapeTools = (() => {
    /**
     * Snap endpoint to the nearest 45-degree increment from the start point.
     * @param {number} x1 - Start X
     * @param {number} y1 - Start Y
     * @param {number} x2 - End X
     * @param {number} y2 - End Y
     * @returns {{x: number, y: number}} Snapped endpoint
     */
    function snapTo45(x1, y1, x2, y2) {
        const dx = x2 - x1;
        const dy = y2 - y1;
        const angle = Math.atan2(dy, dx);
        const distance = Math.sqrt(dx * dx + dy * dy);

        // Snap to nearest 45 degree increment (PI/4)
        const snappedAngle = Math.round(angle / (Math.PI / 4)) * (Math.PI / 4);

        return {
            x: x1 + Math.cos(snappedAngle) * distance,
            y: y1 + Math.sin(snappedAngle) * distance
        };
    }

    /**
     * Draw a dashed preview shape on the overlay canvas.
     * @param {string} shapeType - 'line', 'rectangle', or 'circle'
     * @param {number} endX - Current mouse X in canvas space
     * @param {number} endY - Current mouse Y in canvas space
     * @param {boolean} snap - Whether shift is held (snap line to 45 degrees)
     */
    function drawPreview(shapeType, endX, endY, snap) {
        const oc = EditorCore.overlayCtx;
        const overlayCanvas = EditorCore.overlayCanvas;
        if (!oc || !overlayCanvas) return;

        const sx = EditorCore.startX;
        const sy = EditorCore.startY;

        oc.clearRect(0, 0, overlayCanvas.width, overlayCanvas.height);
        oc.globalAlpha = EditorCore.currentOpacity * 0.5;
        oc.strokeStyle = EditorCore.currentColor;
        oc.lineWidth = EditorCore.brushSize;
        oc.setLineDash([5, 5]);

        if (shapeType === 'line') {
            let finalX = endX, finalY = endY;
            if (snap) {
                const snapped = snapTo45(sx, sy, endX, endY);
                finalX = snapped.x;
                finalY = snapped.y;
            }
            oc.beginPath();
            oc.moveTo(sx, sy);
            oc.lineTo(finalX, finalY);
            oc.stroke();
        } else if (shapeType === 'rectangle') {
            oc.beginPath();
            oc.rect(
                Math.min(sx, endX), Math.min(sy, endY),
                Math.abs(endX - sx), Math.abs(endY - sy)
            );
            oc.stroke();
        } else if (shapeType === 'circle') {
            const radius = Math.sqrt(
                Math.pow(endX - sx, 2) + Math.pow(endY - sy, 2)
            );
            oc.beginPath();
            oc.arc(sx, sy, radius, 0, Math.PI * 2);
            oc.stroke();
        }

        oc.setLineDash([]);
        oc.globalAlpha = 1;
    }

    /**
     * Draw the final shape on the main canvas.
     * @param {string} shapeType - 'line', 'rectangle', or 'circle'
     * @param {number} endX - End X in canvas space
     * @param {number} endY - End Y in canvas space
     * @param {boolean} snap - Whether shift is held
     */
    function drawFinal(shapeType, endX, endY, snap) {
        const ctx = EditorCore.ctx;
        if (!ctx) return;

        const sx = EditorCore.startX;
        const sy = EditorCore.startY;

        ctx.save();
        ctx.globalAlpha = EditorCore.currentOpacity;
        ctx.strokeStyle = EditorCore.currentColor;
        ctx.lineWidth = EditorCore.brushSize;

        if (shapeType === 'line') {
            let finalX = endX, finalY = endY;
            if (snap) {
                const snapped = snapTo45(sx, sy, endX, endY);
                finalX = snapped.x;
                finalY = snapped.y;
            }
            ctx.lineCap = 'round';
            ctx.beginPath();
            ctx.moveTo(sx, sy);
            ctx.lineTo(finalX, finalY);
            ctx.stroke();
        } else if (shapeType === 'rectangle') {
            ctx.beginPath();
            ctx.rect(
                Math.min(sx, endX), Math.min(sy, endY),
                Math.abs(endX - sx), Math.abs(endY - sy)
            );
            ctx.stroke();
        } else if (shapeType === 'circle') {
            const radius = Math.sqrt(
                Math.pow(endX - sx, 2) + Math.pow(endY - sy, 2)
            );
            ctx.beginPath();
            ctx.arc(sx, sy, radius, 0, Math.PI * 2);
            ctx.stroke();
        }
        ctx.restore();
    }

    /**
     * Create a shape tool for the given shape type.
     * @param {string} shapeType - 'line', 'rectangle', or 'circle'
     * @returns {object} Tool implementing the EditorCore tool interface
     */
    function createShapeTool(shapeType) {
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
                drawPreview(shapeType, coords.x, coords.y, e.shiftKey);
            },

            onMouseUp(coords, e) {
                // Clear overlay preview
                const oc = EditorCore.overlayCtx;
                const overlayCanvas = EditorCore.overlayCanvas;
                if (oc && overlayCanvas) {
                    oc.clearRect(0, 0, overlayCanvas.width, overlayCanvas.height);
                }

                // Draw final shape on main canvas
                drawFinal(shapeType, coords.x, coords.y, e.shiftKey);
                EditorCore.saveUndoState();
            },

            getCursor() {
                return 'crosshair';
            }
        };
    }

    function createLine() {
        return createShapeTool('line');
    }

    function createRectangle() {
        return createShapeTool('rectangle');
    }

    function createCircle() {
        return createShapeTool('circle');
    }

    return {
        createLine,
        createRectangle,
        createCircle,
        snapTo45
    };
})();
