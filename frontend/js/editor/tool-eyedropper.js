// --- Eyedropper Tool ---
// Samples a pixel color from the canvas and sets it as the current drawing color.

const EyedropperTool = (() => {
    /**
     * Create an eyedropper tool instance.
     * On mousedown: samples the pixel color at the click position,
     * sets EditorCore.currentColor and the color input, then switches
     * back to the previously active tool.
     */
    function create() {
        let previousTool = null;

        return {
            activate(context = {}) {
                previousTool = context.previousToolName || null;
            },

            deactivate() {
                previousTool = null;
            },

            onMouseDown(coords, e) {
                const ctx = EditorCore.ctx;
                if (!ctx) return;

                const x = Math.max(0, Math.min(ctx.canvas.width - 1, Math.floor(coords.x)));
                const y = Math.max(0, Math.min(ctx.canvas.height - 1, Math.floor(coords.y)));
                const pixel = ctx.getImageData(x, y, 1, 1).data;

                // Convert RGBA to hex (ignore alpha)
                const hex = '#' +
                    ((1 << 24) | (pixel[0] << 16) | (pixel[1] << 8) | pixel[2])
                        .toString(16)
                        .slice(1);

                // Update EditorCore color and the color picker input
                EditorCore.currentColor = hex;
                const colorInput = document.getElementById('editor-color');
                if (colorInput) colorInput.value = hex;
            },

            onMouseMove(coords, e) {
                // No-op — don't draw anything
            },

            onMouseUp(coords, e) {
                // Switch back to the previous tool AFTER the mouse event cycle completes,
                // so the new tool doesn't receive stray move/up events from this click.
                const restoreTo = previousTool && previousTool !== 'eyedropper'
                    ? previousTool
                    : 'brush';
                selectTool(restoreTo);
                updateToolButtons();
            },

            getCursor() {
                return 'crosshair';
            }
        };
    }

    return { create };
})();
