// --- Metadata Module ---

const metadataModal = document.getElementById('metadata-modal');
const metadataCloseBtn = document.getElementById('metadata-close');
const metadataList = document.getElementById('metadata-list');
const metadataAddBtn = document.getElementById('metadata-add');
const metadataSaveBtn = document.getElementById('metadata-save');
const metadataSystemInfo = document.getElementById('metadata-system-info');

let currentMetadataClipId = null;

function openMetadataModal(clipId, clipData) {
    currentMetadataClipId = clipId;
    metadataModal.removeAttribute('inert');
    metadataModal.classList.remove('opacity-0', 'pointer-events-none');
    metadataModal.classList.add('opacity-100');
    metadataModal.querySelector(':scope > div').classList.remove('scale-95');
    metadataModal.querySelector(':scope > div').classList.add('scale-100');
    renderSystemInfo(clipData);
    loadMetadata(clipId);
}

function renderSystemInfo(clipData) {
    metadataSystemInfo.innerHTML = '';
    if (!clipData) {
        metadataSystemInfo.classList.add('hidden');
        return;
    }
    metadataSystemInfo.classList.remove('hidden');

    const fields = [
        { label: 'Date added', value: clipData.created_at ? new Date(clipData.created_at).toLocaleString() : '—' },
        { label: 'Filename', value: clipData.filename || '—' },
        { label: 'Type', value: clipData.content_type || '—' },
        { label: 'Size', value: formatFileSize(clipData.size) },
    ];

    fields.forEach(({ label, value }) => {
        const row = document.createElement('div');
        row.className = 'flex items-center gap-2 text-[11px]';
        row.dataset.testid = 'metadata-system-row';
        const labelSpan = document.createElement('span');
        labelSpan.className = 'text-stone-400 font-medium w-20 flex-shrink-0';
        labelSpan.textContent = label;
        const valueSpan = document.createElement('span');
        valueSpan.className = 'text-stone-600 truncate';
        valueSpan.textContent = value;
        row.appendChild(labelSpan);
        row.appendChild(valueSpan);
        metadataSystemInfo.appendChild(row);
    });
}

function closeMetadataModal() {
    metadataModal.classList.add('opacity-0', 'pointer-events-none');
    metadataModal.classList.remove('opacity-100');
    metadataModal.querySelector(':scope > div').classList.add('scale-95');
    metadataModal.querySelector(':scope > div').classList.remove('scale-100');
    currentMetadataClipId = null;
    metadataModal.setAttribute('inert', '');
}

async function loadMetadata(clipId) {
    metadataList.innerHTML = '';
    try {
        const meta = await window.go.main.App.GetClipMetadata(clipId);
        const entries = Object.entries(meta || {});
        if (entries.length === 0) {
            renderEmptyState();
        } else {
            entries.forEach(([key, value]) => renderMetadataRow(key, value));
        }
    } catch (err) {
        console.error('Failed to load metadata:', err);
        renderEmptyState();
    }
}

function renderEmptyState() {
    metadataList.textContent = '';
    const p = document.createElement('p');
    p.dataset.testid = 'metadata-empty';
    p.className = 'text-xs text-stone-400 text-center py-4';
    p.textContent = "No metadata. Click 'Add Field' to get started.";
    metadataList.appendChild(p);
}

function renderMetadataRow(key, value) {
    // Remove empty state if present
    const emptyState = metadataList.querySelector('[data-testid="metadata-empty"]');
    if (emptyState) emptyState.remove();

    const row = document.createElement('div');
    row.className = 'flex items-center gap-2';
    row.dataset.testid = 'metadata-row';

    const keyInput = document.createElement('input');
    keyInput.type = 'text';
    keyInput.value = key;
    keyInput.placeholder = 'Key';
    keyInput.dataset.testid = 'metadata-key';
    keyInput.maxLength = 256;
    keyInput.className = 'flex-[2] block border border-stone-200 rounded-md text-xs bg-white placeholder-stone-400 focus:outline-none focus:border-stone-400 focus:ring-1 focus:ring-stone-400/20 transition-colors py-1.5 px-2';

    const valueInput = document.createElement('input');
    valueInput.type = 'text';
    valueInput.value = value;
    valueInput.placeholder = 'Value';
    valueInput.dataset.testid = 'metadata-value';
    valueInput.maxLength = 4096;
    valueInput.className = 'flex-[3] block border border-stone-200 rounded-md text-xs bg-white placeholder-stone-400 focus:outline-none focus:border-stone-400 focus:ring-1 focus:ring-stone-400/20 transition-colors py-1.5 px-2';

    const deleteBtn = document.createElement('button');
    deleteBtn.dataset.testid = 'metadata-delete-row';
    deleteBtn.setAttribute('aria-label', 'Remove metadata field');
    deleteBtn.className = 'p-1 text-stone-400 hover:text-stone-600 hover:bg-stone-100 rounded transition-colors flex-shrink-0';
    deleteBtn.innerHTML = `<svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M6 18L18 6M6 6l12 12"></path>
    </svg>`;
    deleteBtn.addEventListener('click', () => {
        row.remove();
        if (metadataList.children.length === 0) renderEmptyState();
    });

    row.appendChild(keyInput);
    row.appendChild(valueInput);
    row.appendChild(deleteBtn);
    metadataList.appendChild(row);

    return keyInput;
}

function addMetadataRow() {
    const input = renderMetadataRow('', '');
    input.focus();
}

async function saveMetadata() {
    if (!currentMetadataClipId) return;

    const rows = metadataList.querySelectorAll('[data-testid="metadata-row"]');
    const meta = {};
    const seenKeys = new Set();
    let hasDuplicate = false;

    // Clear previous highlights
    rows.forEach(row => {
        row.querySelector('[data-testid="metadata-key"]').classList.remove('border-red-400', 'bg-red-50');
    });

    rows.forEach(row => {
        const keyInput = row.querySelector('[data-testid="metadata-key"]');
        const key = keyInput.value.trim();
        const value = row.querySelector('[data-testid="metadata-value"]').value;
        if (key) {
            if (seenKeys.has(key)) {
                hasDuplicate = true;
                keyInput.classList.add('border-red-400', 'bg-red-50');
                // Also highlight the first occurrence
                rows.forEach(r => {
                    const k = r.querySelector('[data-testid="metadata-key"]');
                    if (k.value.trim() === key) k.classList.add('border-red-400', 'bg-red-50');
                });
            }
            seenKeys.add(key);
            meta[key] = value;
        }
    });

    if (hasDuplicate) {
        showToast('Duplicate keys found — please use unique keys', 'error');
        return;
    }

    try {
        await window.go.main.App.SetClipMetadataBulk(currentMetadataClipId, meta);
        showToast('Metadata saved');
        closeMetadataModal();
    } catch (err) {
        console.error('Failed to save metadata:', err);
        showToast('Failed to save metadata', 'error');
    }
}

// Event listeners
metadataCloseBtn.addEventListener('click', closeMetadataModal);
metadataAddBtn.addEventListener('click', addMetadataRow);
metadataSaveBtn.addEventListener('click', saveMetadata);
metadataModal.addEventListener('click', (e) => {
    if (e.target === metadataModal) closeMetadataModal();
});
