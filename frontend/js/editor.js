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
let lastFocusedBeforeEditor = null;
let editorFocusTrapCleanup = null;

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
    const pendingImageOperation = !isTextEditor && (
        EditorCore.activeToolName === 'crop' ||
        (EditorCore.activeToolName === 'select' && SelectTool.hasSelection()) ||
        TextTool.isActive
    );
    saveButton.disabled = saving || (isTextEditor ? !TextClipEditor.isDirty() : (!EditorCore.isDirty() && !pendingImageOperation));
    saveAsButton.disabled = saving;
}

function updateEditorSaveState() {
    if (!document.getElementById('editor-modal')?.classList.contains('active') || editorSaving) return;
    const saveButton = document.getElementById('editor-save-in-place');
    const pendingImageOperation = !isTextEditor && (
        EditorCore.activeToolName === 'crop' ||
        (EditorCore.activeToolName === 'select' && SelectTool.hasSelection()) ||
        TextTool.isActive
    );
    saveButton.disabled = isTextEditor ? !TextClipEditor.isDirty() : (!EditorCore.isDirty() && !pendingImageOperation);
}

// --- Open / Close ---

async function openEditor(clipId) {
    const editorModal = document.getElementById('editor-modal');
    if (!editorModal.classList.contains('active')) lastFocusedBeforeEditor = document.activeElement;
    try {
        // Fetch the clip data via Wails binding
        const clipData = await getClipData(clipId);
        if (!clipData) throw new Error('Failed to load clip');

        const contentType = clipData.content_type || '';
        editorClipId = clipId;
        editorContentType = contentType;
        editorFilename = clipData.filename || `clip_${clipId}`;

        const textEditorView = document.getElementById('text-editor-view');
        const imageEditorView = document.getElementById('image-editor-view');
        const editorFilenameInput = document.getElementById('editor-filename');

        // Show the original filename; request a new name only after Save As.
        document.getElementById('editor-mode-label').textContent = 'Edit';
        document.getElementById('editor-current-filename').textContent = editorFilename;
        const downscaleWarning = document.getElementById('editor-downscale-warning');
        downscaleWarning.classList.add('hidden');
        downscaleWarning.textContent = '';
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
            if (EditorCore.wasDownscaled) {
                downscaleWarning.textContent = `${EditorCore.originalWidth}×${EditorCore.originalHeight} resized to ${EditorCore.canvas.width}×${EditorCore.canvas.height} for editing`;
                downscaleWarning.classList.remove('hidden');
            }
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
                validUTF8: clipData.valid_utf8 !== false,
            });
        }

        editorModal.removeAttribute('inert');
        editorModal.classList.add('active');
        resetToolState();
        updateEditorSaveState();
        if (editorFocusTrapCleanup) editorFocusTrapCleanup();
        editorFocusTrapCleanup = trapFocus(editorModal);
        if (!isTextEditor) requestAnimationFrame(() => document.querySelector('.editor-tool-btn.active')?.focus());

    } catch (error) {
        console.error('Error opening editor:', error);
        if (editorFocusTrapCleanup) editorFocusTrapCleanup();
        editorFocusTrapCleanup = null;
        editorModal.classList.remove('active');
        editorModal.setAttribute('inert', '');
        showToast('Failed to open editor.');
    }
}

async function openMarkdownReferenceCandidate(candidate, fragment) {
    const openTarget = async () => {
        closeEditor({ force: true, discardDraft: true });
        const contentType = candidate.content_type || '';
        const filename = candidate.filename || '';
        if (contentType.startsWith('image/')) {
            await window.LightboxController.openSingle({
                clip: {
                    id: candidate.clip_id,
                    filename,
                    content_type: contentType,
                    is_archived: candidate.is_archived || false,
                },
                opener: document.activeElement,
            });
            return;
        }
        if (isEditableType(contentType) || /\.(?:md|markdown)$/i.test(filename)) {
            await openEditor(candidate.clip_id);
            if (fragment) {
                requestAnimationFrame(() => requestAnimationFrame(() => {
                    let headingID = fragment;
                    try { headingID = decodeURIComponent(fragment); } catch (_) { /* keep raw fragment */ }
                    document.querySelector(`#markdown-preview-content #${CSS.escape(headingID)}`)?.scrollIntoView({ block: 'start' });
                }));
            }
            return;
        }
        await window.go.main.App.OpenClipWithDefaultApp(candidate.clip_id);
    };

    if (hasUnsavedEditorChanges()) {
        showConfirmDialog(
            'Discard unsaved changes?',
            'Opening this linked clip will discard your unsaved changes.',
            openTarget,
            null,
            { variant: 'danger', confirmLabel: 'Open Clip' }
        );
        return;
    }
    await openTarget();
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
    if (editorFocusTrapCleanup) editorFocusTrapCleanup();
    editorFocusTrapCleanup = null;
    editorModal.classList.remove('active');
    editorModal.setAttribute('inert', '');

    if (isTextEditor) {
        TextClipEditor.close({ discardDraft: options?.discardDraft === true });
    }

    // Reset EditorCore (detaches listeners, clears canvases, clears undo/redo)
    EditorCore.reset();
    setSaveAsMode(false);

    const fallbackFocusTarget = document.querySelector(`li[data-id="${editorClipId}"] [data-action="menu"]`);

    // Clear editor metadata
    editorClipId = null;
    editorContentType = '';
    editorFilename = '';
    isTextEditor = false;
    editorSaving = false;

    TextTool.cancelTextInput();
    document.getElementById('editor-mode-label').textContent = 'Edit';
    document.getElementById('editor-downscale-warning').classList.add('hidden');

    const focusTarget = lastFocusedBeforeEditor;
    lastFocusedBeforeEditor = null;
    requestAnimationFrame(() => {
        const target = focusTarget?.isConnected && focusTarget.offsetParent !== null
            ? focusTarget
            : fallbackFocusTarget;
        if (target?.isConnected && typeof target.focus === 'function') target.focus();
    });
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
    updatePropertyControls();
}

function updatePropertyControls() {
    const tool = EditorCore.activeToolName;
    const supported = {
        color: ['brush', 'line', 'arrow', 'rectangle', 'circle', 'text'],
        opacity: ['brush', 'line', 'arrow', 'rectangle', 'circle', 'text'],
        size: ['brush', 'eraser', 'line', 'arrow', 'rectangle', 'circle', 'anonymize'],
    };
    const controls = {
        color: document.getElementById('editor-color'),
        opacity: document.getElementById('editor-opacity'),
        size: document.getElementById('editor-brush-size'),
    };
    Object.entries(controls).forEach(([property, control]) => {
        const enabled = supported[property].includes(tool);
        control.disabled = !enabled;
        const group = control.closest('[data-editor-property]');
        group?.classList.toggle('opacity-40', !enabled);
        group?.classList.toggle('cursor-not-allowed', !enabled);
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
    updateEditorSaveState();
}

// --- Save ---

async function performSaveEditorInPlace() {
    setEditorSaving(true);
    try {
        let base64Data;
        let contentType = editorContentType;
        if (isTextEditor) {
            base64Data = encodeTextToBase64(TextClipEditor.getValue());
        } else {
            const exported = await EditorExport.exportCanvas({
                canvas: EditorCore.canvas,
                originalMime: EditorCore.originalContentType,
                filename: editorFilename,
            });
            base64Data = exported.data;
            contentType = exported.contentType;
            editorFilename = exported.filename;
        }
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
        EditorCore.prepareForAction('save');
        if (!EditorCore.isDirty()) {
            showToast('No image changes to save.');
            updateEditorSaveState();
            return;
        }
        performSaveEditorInPlace();
    }
}

async function performSaveEditorContent() {
    const filenameInput = document.getElementById('editor-filename');
    const filename = filenameInput.value.trim();
    if (!filename) {
        showToast('Please enter a filename.');
        filenameInput.focus();
        return;
    }

    setEditorSaving(true);
    try {
        let base64Data;
        let contentType = editorContentType;
        let outputFilename = filename;
        if (isTextEditor) {
            base64Data = encodeTextToBase64(TextClipEditor.getValue());
        } else {
            EditorCore.prepareForAction('save');
            const exported = await EditorExport.exportCanvas({
                canvas: EditorCore.canvas,
                originalMime: EditorCore.originalContentType,
                filename,
            });
            base64Data = exported.data;
            contentType = exported.contentType;
            outputFilename = exported.filename;
            filenameInput.value = outputFilename;
        }

        await upload([{ name: outputFilename, content_type: contentType, data: base64Data }]);
        if (isTextEditor) TextClipEditor.clearDraft();
        showToast('Saved as new clip!');
        closeEditor({ force: true });
        loadClips();
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
            e.stopImmediatePropagation();
            TextTool.cancelTextInput();
            document.querySelector('.editor-tool-btn[data-tool="text"]')?.focus();
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
