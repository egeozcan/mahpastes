// --- Text Tool ---
// Places a text input at the clicked position on the canvas, then commits
// the text as a drawn annotation on the main canvas.

const TextTool = (() => {
    // Internal text input state
    let textInputActive = false;
    let textInputX = 0;
    let textInputY = 0;

    /**
     * Line spacing used for both the input and the committed annotation, so
     * wrapping a second line does not shift the text when it is drawn.
     */
    const LINE_HEIGHT = 1.2;

    function getInput() {
        return document.getElementById('canvas-text-input');
    }

    /**
     * Grow the input to fit its content. The element lives in canvas space, so
     * its box is measured in canvas pixels and the wrapper transform handles
     * the on-screen scaling.
     */
    function autoSizeInput(input) {
        const lines = input.value.split('\n');
        const fontSize = EditorCore.fontSize;
        const ctx = EditorCore.ctx;

        let widest = 0;
        if (ctx) {
            ctx.save();
            ctx.font = `${fontSize}px Arial, sans-serif`;
            for (const line of lines) widest = Math.max(widest, ctx.measureText(line).width);
            ctx.restore();
        }

        // A trailing caret needs room of its own, or typing at the end of the
        // longest line scrolls the text out of view inside the input.
        input.style.width = Math.max(60, Math.ceil(widest) + fontSize) + 'px';
        input.style.height = Math.ceil(lines.length * fontSize * LINE_HEIGHT) + 'px';
    }

    /**
     * Show the floating text input at the given canvas coordinates.
     */
    function showTextInput(x, y) {
        textInputActive = true;
        textInputX = x;
        textInputY = y;

        const input = getInput();
        const canvas = EditorCore.canvas;
        if (!input || !canvas) return;

        const fontSize = EditorCore.fontSize;

        // Offset by the 1px border so the input's first glyph sits where
        // fillText will put it.
        input.style.left = (x - 1) + 'px';
        input.style.top = (y - 1) + 'px';

        // Match styles
        input.style.fontSize = `${fontSize}px`;
        input.style.color = EditorCore.currentColor;
        input.style.fontFamily = 'Arial, sans-serif';
        input.style.lineHeight = String(LINE_HEIGHT);
        input.style.display = 'block';

        input.value = '';
        autoSizeInput(input);
        input.focus();
    }

    /**
     * Commit the current text input content to the canvas.
     */
    function commitTextInput() {
        const input = getInput();
        if (!input) return;

        // Trailing whitespace and blank lines would only add empty rows;
        // leading whitespace is the user's, so it is left alone.
        const text = input.value.replace(/\s+$/, '');

        if (text.trim()) {
            const ctx = EditorCore.ctx;
            if (ctx) {
                const fontSize = EditorCore.fontSize;
                ctx.save();
                ctx.globalAlpha = EditorCore.currentOpacity;
                ctx.fillStyle = EditorCore.currentColor;
                ctx.font = `${fontSize}px Arial, sans-serif`;
                ctx.textBaseline = 'top';
                text.split('\n').forEach((line, index) => {
                    ctx.fillText(line, textInputX, textInputY + index * fontSize * LINE_HEIGHT);
                });
                ctx.restore();
                EditorCore.saveUndoState();
            }
        }

        input.style.display = 'none';
        input.value = '';
        textInputActive = false;
    }

    function cancelTextInput() {
        const input = getInput();
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
        autoSizeInput,
        get isActive() {
            return textInputActive;
        }
    };
})();
