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

// Text eligibility comes from the one classification registry, so callers never
// repeat an extension or MIME check. Note what this does NOT do: it does not
// return true for images. The helper it replaced did, and two call sites depended
// on that, so a caller gating the editor *as a whole* must use isEditableClip.
function isTextCandidate(filename, contentType) {
    return MahpastesTextEditor.TextFileTypes.isTextCandidate({ filename, contentType });
}

function isImageType(contentType) {
    return contentType.startsWith('image/');
}

// For call sites that gate the editor as a whole rather than the text path
// specifically — the card context menu's Edit item, for one, which must keep
// offering Edit for image clips.
function isEditableClip(filename, contentType) {
    return isTextCandidate(filename, contentType) || isImageType(contentType);
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

// Text save bytes come from TextCodec, which re-encodes using the fidelity
// profile recorded when the clip was opened — the BOM's presence, the document's
// newline style — so an ordinary save writes back the bytes it read. The old
// helper here encoded the raw editor string, which silently converted a CRLF
// document to LF and dropped its BOM.
//
// A refusal is a refusal, not a fallback: the one case that cannot be preserved
// is an unpaired surrogate, which TextEncoder would write as EF BF BD.
function reportTextEncodingRefusal(result) {
    if (result.reason === 'unpaired-surrogate') {
        const at = result.position ? ` at line ${result.position.line}, column ${result.position.column}` : '';
        showToast(`Cannot save: unpaired ${result.codeUnit}${at}. Remove it and try again.`);
        return;
    }
    showToast('Cannot save: the text could not be encoded.');
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

/**
 * Opens the editor for a clip.
 *
 * `options.initialMode` is `'preview'`, `'edit'`, or `'default'`. Callers express
 * the mode here and never simulate a tab click after opening: `default` (or an
 * omitted option) lets the descriptor decide, while the explicit Edit entry points
 * pass `'edit'` so Markdown and CSV land in the editor rather than in Preview.
 */
async function openEditor(clipId, options = {}) {
    const editorModal = document.getElementById('editor-modal');
    if (!editorModal.classList.contains('active')) lastFocusedBeforeEditor = document.activeElement;
    try {
        // One atomic read for both branches: filename, content type, bytes, and
        // UTF-8 validity. Composing metadata and bytes from two requests would
        // let a concurrent update pair one clip's metadata with another revision's
        // bytes — config.json metadata over PNG bytes.
        const clipData = await getClipText(clipId);
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
            // Text editor. The views are swapped only after the decode result is
            // known, because the 16 MiB cap declines to open the modal at all.
            //
            // The payload may be a plain string (valid text content types) or
            // base64 (invalid UTF-8, and extension-classified application types
            // even when valid), so the encoding is read from the payload rather
            // than inferred from the content type.
            //
            // decodeClipPayload is the policy-bearing entry point: it refuses
            // invalid UTF-8 and oversized documents outright rather than handing
            // back replacement-decoded text a save could write back. The visible
            // `value` excludes the BOM and uses LF; `profile` is what puts the
            // bytes back the way they were found.
            const decoded = MahpastesTextEditor.TextCodec.decodeClipPayload({
                data: clipData.data,
                dataEncoding: clipData.data_encoding,
                validUTF8: clipData.valid_utf8,
            });

            // The 16 MiB cap is a decline to open at all, not a read-only state:
            // the editor path would otherwise materialize one document as a
            // Blob, a base64 string, a decoded string, a CodeMirror document, and
            // possibly a preview simultaneously.
            if (!decoded.ok && decoded.reason === 'too-large') {
                const megabytes = Math.round(decoded.byteLength / (1024 * 1024));
                showToast(`Text clip is too large to edit (${megabytes} MB) — download it instead.`);
                editorClipId = null;
                editorContentType = '';
                editorFilename = '';
                isTextEditor = false;
                return;
            }

            isTextEditor = true;
            imageEditorView.classList.add('hidden');
            textEditorView.classList.remove('hidden');

            TextClipEditor.open({
                clipID: clipId,
                filename: editorFilename,
                contentType,
                text: decoded.ok ? decoded.value : '',
                // Non-null puts up the format-neutral byte-safety screen and keeps
                // Save and Save As unavailable. This deliberately replaces today's
                // lossy editing of Latin-1 and other invalid text/* clips.
                unavailable: decoded.ok ? null : decoded.reason,
                byteLength: decoded.byteLength,
                textProfile: decoded.ok ? decoded.profile : null,
                initialMode: options.initialMode || 'default',
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
        // Images were handled above, so the text check alone is right here. The
        // Markdown filename fallback the old check carried is now redundant:
        // classification recognizes .md/.markdown itself.
        if (isTextCandidate(filename, contentType)) {
            // A linked text reference is a generic open, so it uses the
            // descriptor's default mode.
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

async function performSaveEditorInPlace(snapshot) {
    setEditorSaving(true);
    // Captured once. `isTextEditor` is mutable module state describing whatever the
    // editor holds *now*; reading it after the awaited write would let a late text
    // save run the text completion path against a freshly opened image editor and
    // close it, discarding that editor's work.
    const isTextSave = isTextEditor;
    // Held across persistence, not just validation: formatting must not run at any
    // point during a save, and prepareSave's own lock ends when validation resolves.
    if (isTextSave) TextClipEditor.beginSave(snapshot);
    try {
        let base64Data;
        let contentType = editorContentType;
        // The write target comes from the snapshot for text, not from the mutable
        // module globals. Those describe whatever clip the editor is on *now*, which
        // after a pending validation need not be the clip the snapshot came from.
        let targetClipID = editorClipId;
        let targetFilename = editorFilename;
        if (isTextSave) {
            // Last line of defence: prepareSave already re-checked, but the encode
            // and this call are another await boundary away from the click.
            if (!TextClipEditor.snapshotStillTargets(snapshot)) {
                showToast('Save cancelled: the editor moved to a different clip.');
                setEditorSaving(false);
                return;
            }
            // From the snapshot, never from live editor state: typing during a
            // pending save must not change the bytes being written.
            const encoded = TextClipEditor.encodeSnapshot(snapshot);
            if (!encoded.ok) {
                reportTextEncodingRefusal(encoded);
                setEditorSaving(false);
                return;
            }
            base64Data = encoded.base64;
            targetClipID = snapshot.clipID;
            targetFilename = snapshot.filename;
            contentType = snapshot.contentType;
        } else {
            const exported = await EditorExport.exportCanvas({
                canvas: EditorCore.canvas,
                originalMime: EditorCore.originalContentType,
                filename: editorFilename,
            });
            base64Data = exported.data;
            contentType = exported.contentType;
            editorFilename = exported.filename;
            targetFilename = exported.filename;
        }
        await window.go.main.App.UpdateClipData(targetClipID, contentType, base64Data, targetFilename);
        if (isTextSave) {
            // The write is awaited, so the editor may have been closed and pointed at
            // another clip while it was in flight. Clearing a draft or closing the
            // modal here would act on a clip this save has nothing to do with — and
            // that clip may not even be a text clip.
            if (!TextClipEditor.snapshotStillTargets(snapshot)) {
                showToast('Saved.');
                loadClips();
                return;
            }
            // A successful save resets the open-time error baseline to what is now
            // on disk. If the user typed while the save was in flight, those edits
            // were deliberately not part of the snapshot, so closing here would
            // discard bytes they can still see: stay open and dirty instead.
            if (TextClipEditor.commitSavedBaseline(snapshot)) {
                showToast('Saved! Edits made while saving are still unsaved.');
                setEditorSaving(false);
                updateEditorSaveState();
                loadClips();
                return;
            }
            TextClipEditor.clearDraft();
        }
        showToast('Saved!');
        closeEditor({ force: true });
        loadClips();
    } catch (error) {
        console.error('Error saving in place:', error);
        showToast('Failed to save.');
        setEditorSaving(false);
    } finally {
        if (isTextSave) TextClipEditor.endSave(snapshot);
    }
}

// The byte-safety screen leaves Save and Save As unavailable, and a disabled
// button does not achieve that by itself: `mod+s` is bound to saveEditorContent,
// which would otherwise reveal the Save As field and then write an empty clip
// from an editor that was deliberately never given a value.
function textSaveUnavailable() {
    if (!isTextEditor || !TextClipEditor.isUnavailable()) return false;
    showToast('Saving is unavailable: this clip is not valid UTF-8 text.');
    return true;
}

async function saveEditorInPlace() {
    if (!editorClipId || editorSaving) {
        if (!editorClipId) showToast('No clip to overwrite.');
        return;
    }
    if (textSaveUnavailable()) return;

    if (isTextEditor) {
        // The disabled `Saving…` state is entered as soon as the user activates
        // Save, before validation resolves, so waiting on the worker never reads as
        // an unresponsive button.
        setEditorSaving(true);
        const snapshot = await TextClipEditor.prepareSave({ targetFilename: editorFilename });
        if (!snapshot) {
            setEditorSaving(false);
            updateEditorSaveState();
            return;
        }
        await performSaveEditorInPlace(snapshot);
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

async function performSaveEditorContent(snapshot) {
    const filenameInput = document.getElementById('editor-filename');
    // The snapshot resolved the target filename when the user activated the
    // action; reading the field again here would let an edit made during
    // validation redirect the write.
    const filename = snapshot ? snapshot.filename : filenameInput.value.trim();
    if (!filename) {
        showToast('Please enter a filename.');
        filenameInput.focus();
        return;
    }

    setEditorSaving(true);
    // Same reasoning as save-in-place: pin the branch before any await.
    const isTextSave = isTextEditor;
    if (isTextSave) TextClipEditor.beginSave(snapshot);
    try {
        let base64Data;
        let contentType = editorContentType;
        let outputFilename = filename;
        if (isTextSave) {
            if (!TextClipEditor.snapshotStillTargets(snapshot)) {
                showToast('Save cancelled: the editor moved to a different clip.');
                setEditorSaving(false);
                return;
            }
            const encoded = TextClipEditor.encodeSnapshot(snapshot);
            if (!encoded.ok) {
                reportTextEncodingRefusal(encoded);
                setEditorSaving(false);
                return;
            }
            base64Data = encoded.base64;
            contentType = snapshot.contentType;
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
        if (isTextSave) {
            // As above: the upload is awaited, so ownership is re-checked before
            // touching the editor or its draft.
            if (!TextClipEditor.snapshotStillTargets(snapshot)) {
                showToast('Saved as new clip!');
                loadClips();
                return;
            }
            // Save As writes a *copy*, so the open clip keeps whatever the user has
            // in front of them. If they typed while the upload was in flight, those
            // edits were deliberately not in the snapshot; closing here would discard
            // bytes they can still see. Same rule as save-in-place.
            if (TextClipEditor.snapshotMovedOn(snapshot)) {
                showToast('Saved as new clip! Edits made while saving are still unsaved.');
                setEditorSaving(false);
                updateEditorSaveState();
                loadClips();
                return;
            }
            TextClipEditor.clearDraft();
        }
        showToast('Saved as new clip!');
        closeEditor({ force: true });
        loadClips();
    } catch (error) {
        console.error('Error saving:', error);
        showToast('Failed to save.');
        setEditorSaving(false);
    } finally {
        if (isTextSave) TextClipEditor.endSave(snapshot);
    }
}

async function saveEditorContent() {
    if (editorSaving) return;
    if (textSaveUnavailable()) return;
    if (!saveAsMode) {
        setSaveAsMode(true);
        return;
    }

    if (isTextEditor) {
        const proposed = document.getElementById('editor-filename').value.trim();
        if (!proposed) {
            showToast('Please enter a filename.');
            document.getElementById('editor-filename').focus();
            return;
        }
        // Disabled `Saving…` first, then validation: Save As reclassifies against
        // the proposed target filename, so a copy named .json is validated as
        // strict JSON before upload even when the open clip is a .txt.
        setEditorSaving(true);
        const snapshot = await TextClipEditor.prepareSave({ targetFilename: proposed });
        if (!snapshot) {
            setEditorSaving(false);
            updateEditorSaveState();
            return;
        }
        await performSaveEditorContent(snapshot);
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
        // Enter places the annotation; Shift+Enter (or Alt+Enter) falls through
        // to the textarea so a caption can run to a second line.
        if (e.key === 'Enter' && !e.shiftKey && !e.altKey) {
            e.preventDefault();
            TextTool.commitTextInput();
        } else if (e.key === 'Escape') {
            e.preventDefault();
            e.stopImmediatePropagation();
            TextTool.cancelTextInput();
            document.querySelector('.editor-tool-btn[data-tool="text"]')?.focus();
        }
    });
    textInput.addEventListener('input', () => TextTool.autoSizeInput(textInput));
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
