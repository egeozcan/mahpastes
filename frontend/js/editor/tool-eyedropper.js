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
            activate() {
                // Remember the current tool so we can switch back after sampling
                previousTool = EditorCore.activeToolName;
            },

            deactivate() {
                previousTool = null;
            },

            onMouseDown(coords, e) {
                const ctx = EditorCore.ctx;
                if (!ctx) return;

                // Sample the pixel at the click position
                const pixel = ctx.getImageData(
                    Math.round(coords.x),
                    Math.round(coords.y),
                    1,
                    1
                ).data;

                // Convert RGBA to hex (ignore alpha)
                const hex = '#' +
                    ((1 << 24) | (pixel[0] << 16) | (pixel[1] << 8) | pixel[2])
                        .toString(16)
                        .slice(1);

                // Update EditorCore color and the color picker input
                EditorCore.currentColor = hex;
                const colorInput = document.getElementById('editor-color');
                if (colorInput) colorInput.value = hex;

                // Switch back to the previous tool
                const restoreTo = previousTool && previousTool !== 'eyedropper'
                    ? previousTool
                    : 'brush';
                selectTool(restoreTo);
                updateToolButtons();
            },

            onMouseMove(coords, e) {
                // No-op
            },

            onMouseUp(coords, e) {
                // No-op
            },

            getCursor() {
                return 'crosshair';
            }
        };
    }

    return { create };
})();
