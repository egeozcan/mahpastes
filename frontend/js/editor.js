// --- Editor Module ---

// Editor state
let editorClipId = null;
let editorContentType = '';
let editorFilename = '';
let isTextEditor = false;

// Canvas state
let canvas = null;
let ctx = null;
let originalImage = null;
let currentTool = 'brush';
let currentColor = '#3b82f6';
let currentOpacity = 1;
let brushSize = 8;
let isDrawing = false;
let startX = 0;
let startY = 0;
let lastX = 0;
let lastY = 0;
let undoStack = [];
let redoStack = [];
const MAX_UNDO = 50;

// Text tool state
let textInputActive = false;
let textInputX = 0;
let textInputY = 0;
let savedImageData = null;

// --- Editor Functions ---

function isEditableType(contentType) {
    return contentType.startsWith('text/') ||
        contentType === 'application/json' ||
        contentType.startsWith('image/');
}

function isImageType(contentType) {
    return contentType.startsWith('image/');
}

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
            await initCanvasEditor(blob);
        } else {
            // Text editor
            isTextEditor = true;
            imageEditorView.classList.add('hidden');
            textEditorView.classList.remove('hidden');

            // For text, data is already a string
            document.getElementById('text-editor-textarea').value = clipData.data;
        }

        editorModal.classList.add('active');
        resetToolState();

    } catch (error) {
        console.error('Error opening editor:', error);
        showToast('Failed to open editor.');
    }
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

function closeEditor() {
    const editorModal = document.getElementById('editor-modal');
    editorModal.classList.remove('active');

    // Clear state
    editorClipId = null;
    editorContentType = '';
    editorFilename = '';
    undoStack = [];
    redoStack = [];
    savedImageData = null;

    // Hide text input
    const textInput = document.getElementById('canvas-text-input');
    if (textInput) {
        textInput.style.display = 'none';
        textInput.value = '';
    }
    textInputActive = false;

    if (canvas) {
        canvas.removeEventListener('mousedown', handleCanvasMouseDown);
        canvas.removeEventListener('mousemove', handleCanvasMouseMove);
        canvas.removeEventListener('mouseup', handleCanvasMouseUp);
        canvas.removeEventListener('mouseleave', handleCanvasMouseUp);
    }
}

async function initCanvasEditor(imageBlob) {
    canvas = document.getElementById('editor-canvas');
    ctx = canvas.getContext('2d');

    originalImage = new Image();
    originalImage.src = URL.createObjectURL(imageBlob);

    await new Promise((resolve) => {
        originalImage.onload = resolve;
    });

    // Set canvas size to image size (with max limits for performance)
    const maxSize = 2000;
    let width = originalImage.width;
    let height = originalImage.height;

    if (width > maxSize || height > maxSize) {
        const ratio = Math.min(maxSize / width, maxSize / height);
        width = Math.floor(width * ratio);
        height = Math.floor(height * ratio);
    }

    canvas.width = width;
    canvas.height = height;

    // Draw the image
    ctx.drawImage(originalImage, 0, 0, width, height);

    // Save initial state
    saveUndoState();

    // Add event listeners
    canvas.addEventListener('mousedown', handleCanvasMouseDown);
    canvas.addEventListener('mousemove', handleCanvasMouseMove);
    canvas.addEventListener('mouseup', handleCanvasMouseUp);
    canvas.addEventListener('mouseleave', handleCanvasMouseUp);

    // Touch support
    canvas.addEventListener('touchstart', handleTouchStart, { passive: false });
    canvas.addEventListener('touchmove', handleTouchMove, { passive: false });
    canvas.addEventListener('touchend', handleTouchEnd);
}

function resetToolState() {
    currentTool = 'brush';
    currentColor = '#3b82f6';
    currentOpacity = 1;
    brushSize = 8;
    isDrawing = false;
    textInputActive = false;

    // Update UI
    updateToolButtons();
    document.getElementById('editor-color').value = currentColor;
    document.getElementById('editor-opacity').value = currentOpacity * 100;
    document.getElementById('editor-opacity-value').textContent = '100%';
    document.getElementById('editor-brush-size').value = brushSize;
    document.getElementById('editor-brush-size-value').textContent = brushSize + 'px';
}

function updateToolButtons() {
    document.querySelectorAll('.editor-tool-btn').forEach(btn => {
        btn.classList.remove('active', 'bg-blue-600', 'text-white');
        btn.classList.add('bg-gray-100', 'text-gray-700');
    });

    const activeBtn = document.querySelector(`.editor-tool-btn[data-tool="${currentTool}"]`);
    if (activeBtn) {
        activeBtn.classList.add('active', 'bg-blue-600', 'text-white');
        activeBtn.classList.remove('bg-gray-100', 'text-gray-700');
    }
}

function selectTool(tool) {
    currentTool = tool;
    updateToolButtons();

    // Close any active text input
    if (textInputActive && tool !== 'text') {
        commitTextInput();
    }
}

// --- Canvas Drawing ---

function getCanvasCoordinates(e) {
    const rect = canvas.getBoundingClientRect();
    const scaleX = canvas.width / rect.width;
    const scaleY = canvas.height / rect.height;

    return {
        x: (e.clientX - rect.left) * scaleX,
        y: (e.clientY - rect.top) * scaleY
    };
}

function handleCanvasMouseDown(e) {
    const coords = getCanvasCoordinates(e);
    startX = coords.x;
    startY = coords.y;
    lastX = coords.x;
    lastY = coords.y;

    if (currentTool === 'text') {
        e.preventDefault(); // Prevent canvas from stealing focus
        if (textInputActive) {
            commitTextInput();
        }
        showTextInput(coords.x, coords.y);
        return;
    }

    isDrawing = true;

    if (currentTool === 'brush' || currentTool === 'eraser') {
        ctx.beginPath();
        ctx.moveTo(coords.x, coords.y);
    } else if (currentTool === 'line' || currentTool === 'rectangle' || currentTool === 'circle') {
        savedImageData = ctx.getImageData(0, 0, canvas.width, canvas.height);
    }
}

function handleCanvasMouseMove(e) {
    if (!isDrawing) return;

    const coords = getCanvasCoordinates(e);

    if (currentTool === 'brush') {
        drawBrush(coords.x, coords.y);
    } else if (currentTool === 'eraser') {
        erase(coords.x, coords.y);
    } else if (currentTool === 'line' || currentTool === 'rectangle' || currentTool === 'circle') {
        // Preview - restore and redraw
        if (savedImageData) {
            ctx.putImageData(savedImageData, 0, 0);
        }
        drawShapePreview(coords.x, coords.y, e.shiftKey);
    }

    lastX = coords.x;
    lastY = coords.y;
}

function handleCanvasMouseUp(e) {
    if (!isDrawing) return;
    isDrawing = false;

    const coords = getCanvasCoordinates(e);

    // Restore clean state before final draw
    if (savedImageData) {
        ctx.putImageData(savedImageData, 0, 0);
        savedImageData = null;
    }

    if (currentTool === 'line') {
        drawLine(startX, startY, coords.x, coords.y, e.shiftKey);
        saveUndoState();
    } else if (currentTool === 'rectangle') {
        drawRectangle(startX, startY, coords.x, coords.y);
        saveUndoState();
    } else if (currentTool === 'circle') {
        drawCircle(startX, startY, coords.x, coords.y);
        saveUndoState();
    } else if (currentTool === 'brush' || currentTool === 'eraser') {
        saveUndoState();
    }
}

// Touch handlers
function handleTouchStart(e) {
    e.preventDefault();
    const touch = e.touches[0];
    const mouseEvent = new MouseEvent('mousedown', {
        clientX: touch.clientX,
        clientY: touch.clientY
    });
    handleCanvasMouseDown(mouseEvent);
}

function handleTouchMove(e) {
    e.preventDefault();
    const touch = e.touches[0];
    const mouseEvent = new MouseEvent('mousemove', {
        clientX: touch.clientX,
        clientY: touch.clientY,
        shiftKey: false
    });
    handleCanvasMouseMove(mouseEvent);
}

function handleTouchEnd(e) {
    const mouseEvent = new MouseEvent('mouseup', {
        clientX: lastX,
        clientY: lastY
    });
    handleCanvasMouseUp(mouseEvent);
}

// --- Drawing Functions ---

function drawBrush(x, y) {
    ctx.globalAlpha = currentOpacity;
    ctx.strokeStyle = currentColor;
    ctx.lineWidth = brushSize;
    ctx.lineCap = 'round';
    ctx.lineJoin = 'round';

    ctx.lineTo(x, y);
    ctx.stroke();
    ctx.beginPath();
    ctx.moveTo(x, y);
}

function erase(x, y) {
    ctx.globalCompositeOperation = 'destination-out';
    ctx.lineWidth = brushSize;
    ctx.lineCap = 'round';
    ctx.lineJoin = 'round';

    ctx.lineTo(x, y);
    ctx.stroke();
    ctx.beginPath();
    ctx.moveTo(x, y);

    ctx.globalCompositeOperation = 'source-over';
}

function drawLine(x1, y1, x2, y2, snap) {
    if (snap) {
        const snapped = snapTo45(x1, y1, x2, y2);
        x2 = snapped.x;
        y2 = snapped.y;
    }

    ctx.globalAlpha = currentOpacity;
    ctx.strokeStyle = currentColor;
    ctx.lineWidth = brushSize;
    ctx.lineCap = 'round';

    ctx.beginPath();
    ctx.moveTo(x1, y1);
    ctx.lineTo(x2, y2);
    ctx.stroke();
}

function drawRectangle(x1, y1, x2, y2) {
    ctx.globalAlpha = currentOpacity;
    ctx.strokeStyle = currentColor;
    ctx.lineWidth = brushSize;

    ctx.beginPath();
    ctx.rect(Math.min(x1, x2), Math.min(y1, y2), Math.abs(x2 - x1), Math.abs(y2 - y1));
    ctx.stroke();
}

function drawCircle(x1, y1, x2, y2) {
    const radius = Math.sqrt(Math.pow(x2 - x1, 2) + Math.pow(y2 - y1, 2));

    ctx.globalAlpha = currentOpacity;
    ctx.strokeStyle = currentColor;
    ctx.lineWidth = brushSize;

    ctx.beginPath();
    ctx.arc(x1, y1, radius, 0, Math.PI * 2);
    ctx.stroke();
}

function drawShapePreview(x, y, snap) {
    ctx.globalAlpha = currentOpacity * 0.5; // Preview is semi-transparent
    ctx.strokeStyle = currentColor;
    ctx.lineWidth = brushSize;
    ctx.setLineDash([5, 5]);

    if (currentTool === 'line') {
        let endX = x, endY = y;
        if (snap) {
            const snapped = snapTo45(startX, startY, x, y);
            endX = snapped.x;
            endY = snapped.y;
        }
        ctx.beginPath();
        ctx.moveTo(startX, startY);
        ctx.lineTo(endX, endY);
        ctx.stroke();
    } else if (currentTool === 'rectangle') {
        ctx.beginPath();
        ctx.rect(Math.min(startX, x), Math.min(startY, y), Math.abs(x - startX), Math.abs(y - startY));
        ctx.stroke();
    } else if (currentTool === 'circle') {
        const radius = Math.sqrt(Math.pow(x - startX, 2) + Math.pow(y - startY, 2));
        ctx.beginPath();
        ctx.arc(startX, startY, radius, 0, Math.PI * 2);
        ctx.stroke();
    }

    ctx.setLineDash([]);
    ctx.globalAlpha = 1;
}

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

// --- Text Tool ---

function showTextInput(x, y) {
    textInputActive = true;
    textInputX = x;
    textInputY = y;

    const input = document.getElementById('canvas-text-input');
    const rect = canvas.getBoundingClientRect();
    const scaleX = rect.width / canvas.width;
    const scaleY = rect.height / canvas.height;

    const fontSize = brushSize * 2;
    // Adjust font size for the screen based on canvas scaling
    const screenFontSize = fontSize * scaleX;

    input.style.left = (rect.left + x * scaleX - 2) + 'px';
    input.style.top = (rect.top + y * scaleY - 2) + 'px';

    // Match styles
    input.style.fontSize = `${screenFontSize}px`;
    input.style.color = currentColor;
    input.style.fontFamily = 'Arial, sans-serif';
    input.style.lineHeight = '1';
    input.style.padding = '0';
    input.style.margin = '0';
    input.style.display = 'block';

    input.value = '';
    input.focus();
}

function commitTextInput() {
    const input = document.getElementById('canvas-text-input');
    const text = input.value.trim();

    if (text) {
        ctx.save();
        ctx.globalAlpha = currentOpacity;
        ctx.fillStyle = currentColor;
        ctx.font = `${brushSize * 2}px Arial, sans-serif`;
        ctx.textBaseline = 'top';
        ctx.fillText(text, textInputX, textInputY);
        ctx.restore();
        saveUndoState();
    }

    input.style.display = 'none';
    input.value = '';
    textInputActive = false;
}

// --- Undo/Redo ---

function saveUndoState() {
    // Clear redo stack when new action is taken
    redoStack = [];

    // Save current canvas state
    undoStack.push(canvas.toDataURL());

    // Limit stack size
    if (undoStack.length > MAX_UNDO) {
        undoStack.shift();
    }

    updateUndoRedoButtons();
}

function undo() {
    if (undoStack.length <= 1) return; // Keep at least the original

    // Save current state to redo
    redoStack.push(undoStack.pop());

    // Restore previous state
    const img = new Image();
    img.onload = () => {
        ctx.clearRect(0, 0, canvas.width, canvas.height);
        ctx.drawImage(img, 0, 0);
    };
    img.src = undoStack[undoStack.length - 1];

    updateUndoRedoButtons();
}

function redo() {
    if (redoStack.length === 0) return;

    const state = redoStack.pop();
    undoStack.push(state);

    const img = new Image();
    img.onload = () => {
        ctx.clearRect(0, 0, canvas.width, canvas.height);
        ctx.drawImage(img, 0, 0);
    };
    img.src = state;

    updateUndoRedoButtons();
}

function redrawCanvas() {
    if (undoStack.length === 0) return;

    const img = new Image();
    img.onload = () => {
        ctx.clearRect(0, 0, canvas.width, canvas.height);
        ctx.drawImage(img, 0, 0);
    };
    img.src = undoStack[undoStack.length - 1];
}

function updateUndoRedoButtons() {
    const undoBtn = document.getElementById('editor-undo');
    const redoBtn = document.getElementById('editor-redo');

    if (undoBtn) {
        undoBtn.disabled = undoStack.length <= 1;
        undoBtn.classList.toggle('opacity-50', undoStack.length <= 1);
    }
    if (redoBtn) {
        redoBtn.disabled = redoStack.length === 0;
        redoBtn.classList.toggle('opacity-50', redoStack.length === 0);
    }
}

// --- Save As ---

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
        // Get canvas as base64
        const dataUrl = canvas.toDataURL('image/png');
        base64Data = dataUrl.split(',')[1];
        contentType = 'image/png';
    }

    // Create FileData for upload
    const fileData = {
        name: filename,
        content_type: contentType,
        data: base64Data
    };

    try {
        await upload([fileData], 0); // Never expire

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

    // Tool buttons
    document.querySelectorAll('.editor-tool-btn').forEach(btn => {
        btn.addEventListener('click', () => selectTool(btn.dataset.tool));
    });

    // Color picker
    document.getElementById('editor-color').addEventListener('input', (e) => {
        currentColor = e.target.value;
    });

    // Opacity slider
    document.getElementById('editor-opacity').addEventListener('input', (e) => {
        currentOpacity = e.target.value / 100;
        document.getElementById('editor-opacity-value').textContent = e.target.value + '%';
    });

    // Brush size slider
    document.getElementById('editor-brush-size').addEventListener('input', (e) => {
        brushSize = parseInt(e.target.value);
        document.getElementById('editor-brush-size-value').textContent = brushSize + 'px';
    });

    // Undo/Redo
    document.getElementById('editor-undo').addEventListener('click', undo);
    document.getElementById('editor-redo').addEventListener('click', redo);

    // Text input commit on Enter or blur
    const textInput = document.getElementById('canvas-text-input');
    textInput.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') {
            e.preventDefault();
            commitTextInput();
        } else if (e.key === 'Escape') {
            textInput.style.display = 'none';
            textInput.value = '';
            textInputActive = false;
        }
    });
    textInput.addEventListener('blur', () => {
        if (textInputActive) {
            commitTextInput();
        }
    });

    // Click outside to close
    document.getElementById('editor-modal').addEventListener('click', (e) => {
        if (e.target.id === 'editor-modal') {
            closeEditor();
        }
    });

    // Keyboard shortcuts
    document.addEventListener('keydown', (e) => {
        const editorModal = document.getElementById('editor-modal');
        if (!editorModal.classList.contains('active')) return;

        if (e.key === 'Escape') {
            closeEditor();
        } else if (e.ctrlKey || e.metaKey) {
            if (e.key === 'z' && !e.shiftKey) {
                e.preventDefault();
                undo();
            } else if ((e.key === 'z' && e.shiftKey) || e.key === 'y') {
                e.preventDefault();
                redo();
            } else if (e.key === 's') {
                e.preventDefault();
                saveEditorContent();
            }
        }
    });
}
