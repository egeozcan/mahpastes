// --- Transform Tool ---
// Whole-image rotation (90° CW/CCW) and flip (horizontal/vertical) operations.

const TransformTool = (() => {
    /**
     * Rotate the canvas 90° clockwise.
     * Creates a temp canvas with swapped dimensions, draws the rotated image,
     * then copies it back to the main canvas.
     */
    function rotateCW() {
        const canvas = EditorCore.canvas;
        const ctx = EditorCore.ctx;
        const overlay = EditorCore.overlayCanvas;
        if (!canvas || !ctx) return;

        const tmp = document.createElement('canvas');
        tmp.width = canvas.height;
        tmp.height = canvas.width;
        const tmpCtx = tmp.getContext('2d');

        tmpCtx.translate(tmp.width, 0);
        tmpCtx.rotate(Math.PI / 2);
        tmpCtx.drawImage(canvas, 0, 0);

        // Resize main canvas and overlay to new dimensions
        canvas.width = tmp.width;
        canvas.height = tmp.height;
        if (overlay) {
            overlay.width = tmp.width;
            overlay.height = tmp.height;
        }

        ctx.drawImage(tmp, 0, 0);

        EditorCore.syncOverlay();
        EditorCore.saveUndoState();
    }

    /**
     * Rotate the canvas 90° counter-clockwise.
     * Creates a temp canvas with swapped dimensions, draws the rotated image,
     * then copies it back to the main canvas.
     */
    function rotateCCW() {
        const canvas = EditorCore.canvas;
        const ctx = EditorCore.ctx;
        const overlay = EditorCore.overlayCanvas;
        if (!canvas || !ctx) return;

        const tmp = document.createElement('canvas');
        tmp.width = canvas.height;
        tmp.height = canvas.width;
        const tmpCtx = tmp.getContext('2d');

        tmpCtx.translate(0, tmp.height);
        tmpCtx.rotate(-Math.PI / 2);
        tmpCtx.drawImage(canvas, 0, 0);

        // Resize main canvas and overlay to new dimensions
        canvas.width = tmp.width;
        canvas.height = tmp.height;
        if (overlay) {
            overlay.width = tmp.width;
            overlay.height = tmp.height;
        }

        ctx.drawImage(tmp, 0, 0);

        EditorCore.syncOverlay();
        EditorCore.saveUndoState();
    }

    /**
     * Flip the canvas horizontally (mirror left-right).
     */
    function flipH() {
        const canvas = EditorCore.canvas;
        const ctx = EditorCore.ctx;
        if (!canvas || !ctx) return;

        const tmp = document.createElement('canvas');
        tmp.width = canvas.width;
        tmp.height = canvas.height;
        const tmpCtx = tmp.getContext('2d');

        tmpCtx.translate(tmp.width, 0);
        tmpCtx.scale(-1, 1);
        tmpCtx.drawImage(canvas, 0, 0);

        ctx.clearRect(0, 0, canvas.width, canvas.height);
        ctx.drawImage(tmp, 0, 0);

        EditorCore.saveUndoState();
    }

    /**
     * Flip the canvas vertically (mirror top-bottom).
     */
    function flipV() {
        const canvas = EditorCore.canvas;
        const ctx = EditorCore.ctx;
        if (!canvas || !ctx) return;

        const tmp = document.createElement('canvas');
        tmp.width = canvas.width;
        tmp.height = canvas.height;
        const tmpCtx = tmp.getContext('2d');

        tmpCtx.translate(0, tmp.height);
        tmpCtx.scale(1, -1);
        tmpCtx.drawImage(canvas, 0, 0);

        ctx.clearRect(0, 0, canvas.width, canvas.height);
        ctx.drawImage(tmp, 0, 0);

        EditorCore.saveUndoState();
    }

    return {
        rotateCW,
        rotateCCW,
        flipH,
        flipV
    };
})();
