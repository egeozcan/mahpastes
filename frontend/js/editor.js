// --- Editor Module ---
// Thin wrapper that wires up EditorCore, tool modules, and the UI.
// All drawing logic lives in tool-brush.js, tool-shapes.js, tool-text.js.
// Canvas, undo/redo, and event dispatch live in editor-core.js.

// Editor metadata (kept here, not in EditorCore)
let editorClipId = null;
let editorContentType = '';
let editorFilename = '';
let isTextEditor = false;

// --- Utility Functions ---

function isEditableType(contentType) {
    return contentType.startsWith('text/') ||
        contentType === 'application/json' ||
        contentType.startsWith('image/');
}

function isImageType(contentType) {
    return contentType.startsWith('image/');
}

function getNewFilename(original) {
    // Generate a "copy" filename
    const lastDot = original.lastIndexOf('.');
    if (lastDot === -1) {
        return original + '_edited';
    }
    const name = original.substring(0, lastDot);
    const ext = original.substring(lastDot);
    return name + '_edited' + ext;
}

// --- Open / Close ---

async function openEditor(clipId) {
    try {
        // Fetch the clip data via Wails binding
        const clipData = await getClipData(clipId);
        if (!clipData) throw new Error('Failed to load clip');

        const contentType = clipData.content_type || '';
        editorClipId = clipId;
        editorContentType = contentType;
        editorFilename = clipData.filename || `clip_${clipId}`;

        const editorModal = document.getElementById('editor-modal');
        const textEditorView = document.getElementById('text-editor-view');
        const imageEditorView = document.getElementById('image-editor-view');
        const editorFilenameInput = document.getElementById('editor-filename');

        // Set filename in input
        editorFilenameInput.value = getNewFilename(editorFilename);

        if (isImageType(contentType)) {
            // Image editor
            isTextEditor = false;
            textEditorView.classList.add('hidden');
            imageEditorView.classList.remove('hidden');

            // Convert base64 to blob
            const binaryData = atob(clipData.data);
            const bytes = new Uint8Array(binaryData.length);
            for (let i = 0; i < binaryData.length; i++) {
                bytes[i] = binaryData.charCodeAt(i);
            }
            const blob = new Blob([bytes], { type: contentType });

            // Initialize EditorCore canvas and load image
            await EditorCore.loadImage(blob, contentType);

            // Register tools
            EditorCore.registerTool('brush', BrushTool.createBrush());
            EditorCore.registerTool('eraser', BrushTool.createEraser());
            EditorCore.registerTool('line', ShapeTools.createLine());
            EditorCore.registerTool('rectangle', ShapeTools.createRectangle());
            EditorCore.registerTool('circle', ShapeTools.createCircle());
            EditorCore.registerTool('arrow', ArrowTool.create());
            EditorCore.registerTool('text', TextTool.create());
            EditorCore.registerTool('eyedropper', EyedropperTool.create());
            EditorCore.registerTool('crop', CropTool.create());

            // Attach mouse/touch/keyboard listeners
            EditorCore.attachListeners();

            // Update zoom display to reflect initial zoom level
            ZoomTool.updateZoomDisplay();

            // Select default tool
            EditorCore.selectTool('brush');
        } else {
            // Text editor
            isTextEditor = true;
            imageEditorView.classList.add('hidden');
            textEditorView.classList.remove('hidden');

            // For text, data is already a string
            document.getElementById('text-editor-textarea').value = clipData.data;
        }

        editorModal.removeAttribute('inert');
        editorModal.classList.add('active');
        resetToolState();

    } catch (error) {
        console.error('Error opening editor:', error);
        showToast('Failed to open editor.');
    }
}

function closeEditor() {
    const editorModal = document.getElementById('editor-modal');
    editorModal.classList.remove('active');
    editorModal.setAttribute('inert', '');

    // Reset EditorCore (detaches listeners, clears canvases, clears undo/redo)
    EditorCore.reset();

    // Clear editor metadata
    editorClipId = null;
    editorContentType = '';
    editorFilename = '';

    // Hide text input
    const textInput = document.getElementById('canvas-text-input');
    if (textInput) {
        textInput.style.display = 'none';
        textInput.value = '';
    }
}

// --- Tool State UI ---

function resetToolState() {
    // Reset drawing properties on EditorCore
    EditorCore.currentColor = '#44403c';
    EditorCore.currentOpacity = 1;
    EditorCore.brushSize = 8;
    EditorCore.fontSize = 16;

    // Update UI controls
    updateToolButtons();
    document.getElementById('editor-color').value = EditorCore.currentColor;
    document.getElementById('editor-opacity').value = EditorCore.currentOpacity * 100;
    document.getElementById('editor-opacity-value').textContent = '100%';
    document.getElementById('editor-brush-size').value = EditorCore.brushSize;
    document.getElementById('editor-brush-size-value').textContent = EditorCore.brushSize + 'px';
}

function updateToolButtons() {
    document.querySelectorAll('.editor-tool-btn').forEach(btn => {
        btn.classList.toggle('active', btn.dataset.tool === EditorCore.activeToolName);
    });
}

function updateContextToolbar() {
    const cropOpts = document.getElementById('editor-crop-options');
    const anonOpts = document.getElementById('editor-anonymize-options');
    const textOpts = document.getElementById('editor-text-options');
    const toolName = EditorCore.activeToolName;

    if (cropOpts) cropOpts.style.display = toolName === 'crop' ? 'flex' : 'none';
    if (anonOpts) anonOpts.style.display = toolName === 'anonymize' ? 'flex' : 'none';
    if (textOpts) textOpts.style.display = toolName === 'text' ? 'flex' : 'none';
}

function selectTool(tool) {
    // Commit any active text input when switching away from text
    if (TextTool.isActive && tool !== 'text') {
        TextTool.commitTextInput();
    }

    EditorCore.selectTool(tool);
    updateToolButtons();
    updateContextToolbar();
}

// --- Save ---

async function saveEditorContent() {
    const filename = document.getElementById('editor-filename').value.trim();
    if (!filename) {
        showToast('Please enter a filename.');
        return;
    }

    let base64Data;
    let contentType = editorContentType;

    if (isTextEditor) {
        const text = document.getElementById('text-editor-textarea').value;
        // Convert text to base64
        base64Data = btoa(unescape(encodeURIComponent(text)));
    } else {
        // Use EditorCore's canvas and preserve original format when possible
        const savedContentType = EditorCore.originalContentType;
        const exportType = (savedContentType === 'image/jpeg' || savedContentType === 'image/webp')
            ? savedContentType
            : 'image/png';
        const dataUrl = EditorCore.canvas.toDataURL(exportType);
        base64Data = dataUrl.split(',')[1];
        contentType = exportType;
    }

    // Create FileData for upload
    const fileData = {
        name: filename,
        content_type: contentType,
        data: base64Data
    };

    try {
        await upload([fileData]);

        showToast('Saved as new clip!');
        closeEditor();
        loadClips(); // Refresh gallery

    } catch (error) {
        console.error('Error saving:', error);
        showToast('Failed to save.');
    }
}

// --- Event Listener Setup (called from app.js) ---

function setupEditorListeners() {
    // Close button
    document.getElementById('editor-close').addEventListener('click', closeEditor);

    // Save button
    document.getElementById('editor-save').addEventListener('click', saveEditorContent);

    // Tool buttons — delegate to EditorCore via selectTool wrapper
    document.querySelectorAll('.editor-tool-btn').forEach(btn => {
        btn.addEventListener('click', () => selectTool(btn.dataset.tool));
    });

    // Color picker
    document.getElementById('editor-color').addEventListener('input', (e) => {
        EditorCore.currentColor = e.target.value;
    });

    // Opacity slider
    document.getElementById('editor-opacity').addEventListener('input', (e) => {
        EditorCore.currentOpacity = e.target.value / 100;
        document.getElementById('editor-opacity-value').textContent = e.target.value + '%';
    });

    // Brush size slider
    document.getElementById('editor-brush-size').addEventListener('input', (e) => {
        EditorCore.brushSize = parseInt(e.target.value);
        document.getElementById('editor-brush-size-value').textContent = EditorCore.brushSize + 'px';
    });

    // Undo/Redo — delegate to EditorCore
    document.getElementById('editor-undo').addEventListener('click', () => EditorCore.undo());
    document.getElementById('editor-redo').addEventListener('click', () => EditorCore.redo());

    // Zoom controls — delegate to ZoomTool
    document.getElementById('editor-zoom-fit')?.addEventListener('click', () => ZoomTool.zoomToFit());
    document.getElementById('editor-zoom-100')?.addEventListener('click', () => ZoomTool.zoomTo100());
    document.getElementById('editor-zoom-in')?.addEventListener('click', () => ZoomTool.zoomIn());
    document.getElementById('editor-zoom-out')?.addEventListener('click', () => ZoomTool.zoomOut());

    // Rotate/Flip buttons — delegate to TransformTool (no-ops until tool-transform.js exists)
    document.getElementById('editor-rotate-cw')?.addEventListener('click', () => typeof TransformTool !== 'undefined' && TransformTool.rotateCW());
    document.getElementById('editor-rotate-ccw')?.addEventListener('click', () => typeof TransformTool !== 'undefined' && TransformTool.rotateCCW());
    document.getElementById('editor-flip-h')?.addEventListener('click', () => typeof TransformTool !== 'undefined' && TransformTool.flipH());
    document.getElementById('editor-flip-v')?.addEventListener('click', () => typeof TransformTool !== 'undefined' && TransformTool.flipV());

    // Font size input
    document.getElementById('editor-font-size')?.addEventListener('input', (e) => {
        EditorCore.fontSize = parseInt(e.target.value) || 24;
    });

    // Text input commit on Enter or blur
    const textInput = document.getElementById('canvas-text-input');
    textInput.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') {
            e.preventDefault();
            TextTool.commitTextInput();
        } else if (e.key === 'Escape') {
            e.preventDefault();
            textInput.style.display = 'none';
            textInput.value = '';
        }
    });
    textInput.addEventListener('blur', () => {
        if (TextTool.isActive) {
            TextTool.commitTextInput();
        }
    });

    // Crop controls
    document.getElementById('editor-crop-ratio')?.addEventListener('change', (e) => {
        const val = e.target.value;
        if (val === 'free') CropTool.setAspectRatio(null);
        else {
            const [w, h] = val.split(':').map(Number);
            CropTool.setAspectRatio(w / h);
        }
    });

    document.getElementById('editor-crop-swap')?.addEventListener('click', () => {
        CropTool.swapAspectRatio();
    });

    document.getElementById('editor-crop-rotate')?.addEventListener('input', (e) => {
        CropTool.setRotation(parseFloat(e.target.value));
        document.getElementById('editor-crop-rotate-value').textContent = e.target.value + '°';
    });

    document.getElementById('editor-crop-confirm')?.addEventListener('click', () => {
        CropTool.confirm();
    });

    document.getElementById('editor-crop-cancel')?.addEventListener('click', () => {
        CropTool.cancel();
    });

    // Click outside to close
    document.getElementById('editor-modal').addEventListener('click', (e) => {
        if (e.target.id === 'editor-modal') {
            closeEditor();
        }
    });

}
