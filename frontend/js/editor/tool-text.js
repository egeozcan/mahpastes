// --- Text Tool ---
// Places a text input at the clicked position on the canvas, then commits
// the text as a drawn annotation on the main canvas.

const TextTool = (() => {
    // Internal text input state
    let textInputActive = false;
    let textInputX = 0;
    let textInputY = 0;

    /**
     * Show the floating text input at the given canvas coordinates.
     */
    function showTextInput(x, y) {
        textInputActive = true;
        textInputX = x;
        textInputY = y;

        const input = document.getElementById('canvas-text-input');
        const canvas = EditorCore.canvas;
        if (!input || !canvas) return;

        const rect = canvas.getBoundingClientRect();
        const scaleX = rect.width / canvas.width;
        const scaleY = rect.height / canvas.height;

        const fontSize = EditorCore.fontSize;
        // Adjust font size for the screen based on canvas scaling
        const screenFontSize = fontSize * scaleX;

        input.style.left = (rect.left + x * scaleX - 2) + 'px';
        input.style.top = (rect.top + y * scaleY - 2) + 'px';

        // Match styles
        input.style.fontSize = `${screenFontSize}px`;
        input.style.color = EditorCore.currentColor;
        input.style.fontFamily = 'Arial, sans-serif';
        input.style.lineHeight = '1';
        input.style.padding = '0';
        input.style.margin = '0';
        input.style.display = 'block';

        input.value = '';
        input.focus();
    }

    /**
     * Commit the current text input content to the canvas.
     */
    function commitTextInput() {
        const input = document.getElementById('canvas-text-input');
        if (!input) return;

        const text = input.value.trim();

        if (text) {
            const ctx = EditorCore.ctx;
            if (ctx) {
                ctx.save();
                ctx.globalAlpha = EditorCore.currentOpacity;
                ctx.fillStyle = EditorCore.currentColor;
                ctx.font = `${EditorCore.fontSize}px Arial, sans-serif`;
                ctx.textBaseline = 'top';
                ctx.fillText(text, textInputX, textInputY);
                ctx.restore();
                EditorCore.saveUndoState();
            }
        }

        input.style.display = 'none';
        input.value = '';
        textInputActive = false;
    }

    function cancelTextInput() {
        const input = document.getElementById('canvas-text-input');
        if (input) {
            input.style.display = 'none';
            input.value = '';
        }
        textInputActive = false;
        if (typeof updateEditorSaveState === 'function') updateEditorSaveState();
    }

    /**
     * Create a text tool instance.
     */
    function create() {
        return {
            activate() {
                // No setup needed
            },

            deactivate() {
                // Commit any active text input when switching away
                if (textInputActive) {
                    commitTextInput();
                }
            },

            onMouseDown(coords, e) {
                e.preventDefault(); // Prevent canvas from stealing focus

                // Commit existing text first if active
                if (textInputActive) {
                    commitTextInput();
                }

                showTextInput(coords.x, coords.y);
            },

            onMouseMove(coords, e) {
                // Text tool doesn't track mouse movement
            },

            onMouseUp(coords, e) {
                // Text tool doesn't need mouse up handling
            },

            getCursor() {
                return 'text';
            },

            prepareForAction(intent) {
                if (!textInputActive) return 'proceed';
                if (intent === 'undo' || intent === 'redo') {
                    cancelTextInput();
                    return 'consumed';
                }
                if (intent === 'save' || intent === 'transform') commitTextInput();
                return 'proceed';
            }
        };
    }

    return {
        create,
        commitTextInput,
        cancelTextInput,
        get isActive() {
            return textInputActive;
        }
    };
})();
