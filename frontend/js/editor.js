// --- Editor Module ---
// Thin wrapper that wires up EditorCore, tool modules, and the UI.
// All drawing logic lives in tool-brush.js, tool-shapes.js, tool-text.js.
// Canvas, undo/redo, and event dispatch live in editor-core.js.

// Editor metadata (kept here, not in EditorCore)
let editorClipId = null;
let editorContentType = '';
let editorFilename = '';
let isTextEditor = false;
let saveAsMode = false;
let editorSaving = false;

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

function encodeTextToBase64(text) {
    const bytes = new TextEncoder().encode(text);
    let binary = '';
    const chunkSize = 0x8000;
    for (let offset = 0; offset < bytes.length; offset += chunkSize) {
        binary += String.fromCharCode(...bytes.subarray(offset, offset + chunkSize));
    }
    return btoa(binary);
}

function setSaveAsMode(enabled) {
    saveAsMode = enabled;
    const filenameInput = document.getElementById('editor-filename');
    const cancelButton = document.getElementById('editor-save-as-cancel');
    const saveInPlaceButton = document.getElementById('editor-save-in-place');
    const saveAsLabel = document.getElementById('editor-save-as-label');

    filenameInput.classList.toggle('hidden', !enabled);
    cancelButton.classList.toggle('hidden', !enabled);
    saveInPlaceButton.classList.toggle('hidden', enabled);
    saveAsLabel.textContent = enabled ? 'Create Copy' : 'Save As';

    if (enabled) {
        filenameInput.focus();
        filenameInput.select();
    }
}

function cancelSaveAs() {
    setSaveAsMode(false);
    document.getElementById('editor-save').focus();
}

function setEditorSaving(saving) {
    editorSaving = saving;
    const saveButton = document.getElementById('editor-save-in-place');
    const saveAsButton = document.getElementById('editor-save');
    document.getElementById('editor-save-label').textContent = saving ? 'Saving…' : 'Save';
    document.getElementById('editor-save-as-label').textContent = saving
        ? 'Saving…'
        : (saveAsMode ? 'Create Copy' : 'Save As');
    saveButton.disabled = saving || (isTextEditor && !TextClipEditor.isDirty());
    saveAsButton.disabled = saving;
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

        // Show the original filename; request a new name only after Save As.
        document.getElementById('editor-current-filename').textContent = editorFilename;
        editorFilenameInput.value = getNewFilename(editorFilename);
        setSaveAsMode(false);
        setEditorSaving(false);

        if (isImageType(contentType)) {
            // Image editor
            isTextEditor = false;
            textEditorView.classList.add('hidden');
            imageEditorView.classList.remove('hidden');
            document.getElementById('editor-save-in-place').disabled = false;

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
            EditorCore.registerTool('select', SelectTool.create());
            EditorCore.registerTool('anonymize', AnonymizeTool.create());

            // Attach mouse/touch/keyboard listeners
            EditorCore.attachListeners();

            // Fit image to container and show the correct zoom percentage
            ZoomTool.zoomToFit();

            // Select default tool
            EditorCore.selectTool('brush');
        } else {
            // Text editor
            isTextEditor = true;
            imageEditorView.classList.add('hidden');
            textEditorView.classList.remove('hidden');

            // For text, data is already a string.
            TextClipEditor.open({
                clipID: clipId,
                filename: editorFilename,
                contentType,
                text: clipData.data,
            });
        }

        editorModal.removeAttribute('inert');
        editorModal.classList.add('active');
        resetToolState();

    } catch (error) {
        console.error('Error opening editor:', error);
        showToast('Failed to open editor.');
    }
}

function hasUnsavedEditorChanges() {
    if (!editorClipId) return false;

    if (isTextEditor) {
        return TextClipEditor.isDirty();
    }

    return EditorCore.isDirty();
}

function closeEditor(options) {
    const force = options === true || options?.force === true;
    if (!force && hasUnsavedEditorChanges()) {
        showConfirmDialog(
            'Discard unsaved changes?',
            'Your changes have not been saved. Discard them and close the editor?',
            () => closeEditor({ force: true, discardDraft: true }),
            null,
            { variant: 'danger', confirmLabel: 'Discard' }
        );
        return;
    }

    const editorModal = document.getElementById('editor-modal');
    editorModal.classList.remove('active');
    editorModal.setAttribute('inert', '');

    if (isTextEditor) {
        TextClipEditor.close({ discardDraft: options?.discardDraft === true });
    }

    // Reset EditorCore (detaches listeners, clears canvases, clears undo/redo)
    EditorCore.reset();
    setSaveAsMode(false);

    // Clear editor metadata
    editorClipId = null;
    editorContentType = '';
    editorFilename = '';
    isTextEditor = false;
    editorSaving = false;

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
    const fontSizeInput = document.getElementById('editor-font-size');
    if (fontSizeInput) fontSizeInput.value = EditorCore.fontSize;
}

function adjustBrushSize(delta) {
    const newSize = Math.max(1, Math.min(50, EditorCore.brushSize + delta));
    EditorCore.brushSize = newSize;
    const sizeInput = document.getElementById('editor-brush-size');
    const sizeLabel = document.getElementById('editor-brush-size-value');
    if (sizeInput) sizeInput.value = newSize;
    if (sizeLabel) sizeLabel.textContent = newSize + 'px';
}

function adjustOpacity(delta) {
    const newOpacity = Math.max(0, Math.min(1, EditorCore.currentOpacity + delta));
    EditorCore.currentOpacity = newOpacity;
    const pct = Math.round(newOpacity * 100);
    const opacityInput = document.getElementById('editor-opacity');
    const opacityLabel = document.getElementById('editor-opacity-value');
    if (opacityInput) opacityInput.value = pct;
    if (opacityLabel) opacityLabel.textContent = pct + '%';
}

function updateToolButtons() {
    document.querySelectorAll('.editor-tool-btn').forEach(btn => {
        const isActive = btn.dataset.tool === EditorCore.activeToolName;
        btn.classList.toggle('active', isActive);
        btn.setAttribute('aria-pressed', isActive ? 'true' : 'false');
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

async function performSaveEditorInPlace() {
    let base64Data;
    let contentType = editorContentType;

    if (isTextEditor) {
        base64Data = encodeTextToBase64(TextClipEditor.getValue());
    } else {
        const savedContentType = EditorCore.originalContentType;
        const exportType = (savedContentType === 'image/jpeg' || savedContentType === 'image/webp')
            ? savedContentType
            : 'image/png';
        const dataUrl = EditorCore.canvas.toDataURL(exportType);
        base64Data = dataUrl.split(',')[1];
        contentType = exportType;
    }

    setEditorSaving(true);
    try {
        await window.go.main.App.UpdateClipData(editorClipId, contentType, base64Data, editorFilename);
        if (isTextEditor) TextClipEditor.clearDraft();
        showToast('Saved!');
        closeEditor({ force: true });
        loadClips();
    } catch (error) {
        console.error('Error saving in place:', error);
        showToast('Failed to save.');
        setEditorSaving(false);
    }
}

function saveEditorInPlace() {
    if (!editorClipId || editorSaving) {
        if (!editorClipId) showToast('No clip to overwrite.');
        return;
    }

    if (isTextEditor) {
        TextClipEditor.confirmSave(performSaveEditorInPlace);
    } else {
        performSaveEditorInPlace();
    }
}

async function performSaveEditorContent() {
    const filename = document.getElementById('editor-filename').value.trim();
    if (!filename) {
        showToast('Please enter a filename.');
        document.getElementById('editor-filename').focus();
        return;
    }

    let base64Data;
    let contentType = editorContentType;

    if (isTextEditor) {
        base64Data = encodeTextToBase64(TextClipEditor.getValue());
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

    const fileData = {
        name: filename,
        content_type: contentType,
        data: base64Data
    };

    setEditorSaving(true);
    try {
        await upload([fileData]);
        if (isTextEditor) TextClipEditor.clearDraft();
        showToast('Saved as new clip!');
        closeEditor({ force: true });
        loadClips(); // Refresh gallery
    } catch (error) {
        console.error('Error saving:', error);
        showToast('Failed to save.');
        setEditorSaving(false);
    }
}

function saveEditorContent() {
    if (editorSaving) return;
    if (!saveAsMode) {
        setSaveAsMode(true);
        return;
    }

    if (isTextEditor) {
        TextClipEditor.confirmSave(performSaveEditorContent);
    } else {
        performSaveEditorContent();
    }
}

// --- Event Listener Setup (called from app.js) ---

function setupEditorListeners() {
    TextClipEditor.setup();

    // Close button
    document.getElementById('editor-close').addEventListener('click', closeEditor);

    // Save buttons
    document.getElementById('editor-save').addEventListener('click', saveEditorContent);
    document.getElementById('editor-save-in-place').addEventListener('click', saveEditorInPlace);
    document.getElementById('editor-save-as-cancel').addEventListener('click', cancelSaveAs);
    document.getElementById('editor-filename').addEventListener('keydown', (event) => {
        if (event.key === 'Enter') {
            event.preventDefault();
            event.stopImmediatePropagation();
            saveEditorContent();
        } else if (event.key === 'Escape') {
            event.preventDefault();
            event.stopImmediatePropagation();
            cancelSaveAs();
        }
    });

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

    // Anonymize controls
    document.getElementById('editor-anon-brush')?.addEventListener('click', () => {
        AnonymizeTool.setMode('brush');
        document.getElementById('editor-anon-brush').classList.add('bg-stone-800', 'text-white');
        document.getElementById('editor-anon-brush').classList.remove('bg-stone-100', 'text-stone-600');
        document.getElementById('editor-anon-brush').setAttribute('aria-pressed', 'true');
        document.getElementById('editor-anon-rect').classList.remove('bg-stone-800', 'text-white');
        document.getElementById('editor-anon-rect').classList.add('bg-stone-100', 'text-stone-600');
        document.getElementById('editor-anon-rect').setAttribute('aria-pressed', 'false');
    });

    document.getElementById('editor-anon-rect')?.addEventListener('click', () => {
        AnonymizeTool.setMode('rect');
        document.getElementById('editor-anon-rect').classList.add('bg-stone-800', 'text-white');
        document.getElementById('editor-anon-rect').classList.remove('bg-stone-100', 'text-stone-600');
        document.getElementById('editor-anon-rect').setAttribute('aria-pressed', 'true');
        document.getElementById('editor-anon-brush').classList.remove('bg-stone-800', 'text-white');
        document.getElementById('editor-anon-brush').classList.add('bg-stone-100', 'text-stone-600');
        document.getElementById('editor-anon-brush').setAttribute('aria-pressed', 'false');
    });

    document.getElementById('editor-anon-pixelate')?.addEventListener('click', () => {
        AnonymizeTool.setEffect('pixelate');
        document.getElementById('editor-anon-pixelate').classList.add('bg-stone-800', 'text-white');
        document.getElementById('editor-anon-pixelate').classList.remove('bg-stone-100', 'text-stone-600');
        document.getElementById('editor-anon-pixelate').setAttribute('aria-pressed', 'true');
        document.getElementById('editor-anon-blur').classList.remove('bg-stone-800', 'text-white');
        document.getElementById('editor-anon-blur').classList.add('bg-stone-100', 'text-stone-600');
        document.getElementById('editor-anon-blur').setAttribute('aria-pressed', 'false');
    });

    document.getElementById('editor-anon-blur')?.addEventListener('click', () => {
        AnonymizeTool.setEffect('blur');
        document.getElementById('editor-anon-blur').classList.add('bg-stone-800', 'text-white');
        document.getElementById('editor-anon-blur').classList.remove('bg-stone-100', 'text-stone-600');
        document.getElementById('editor-anon-blur').setAttribute('aria-pressed', 'true');
        document.getElementById('editor-anon-pixelate').classList.remove('bg-stone-800', 'text-white');
        document.getElementById('editor-anon-pixelate').classList.add('bg-stone-100', 'text-stone-600');
        document.getElementById('editor-anon-pixelate').setAttribute('aria-pressed', 'false');
    });

    // Click outside to close
    document.getElementById('editor-modal').addEventListener('click', (e) => {
        if (e.target.id === 'editor-modal') {
            closeEditor();
        }
    });

}
