// Image cache for base64 data
const imageCache = new Map();

async function createClipCard(clip) {
    const card = document.createElement('li');
    card.className = 'bg-white rounded-2xl shadow-sm border border-gray-100 overflow-hidden flex flex-col transition-all duration-300 hover:shadow-xl hover:-translate-y-1 relative group';
    card.dataset.id = clip.id;
    card.dataset.filename = (clip.filename || '').toLowerCase();
    card.dataset.type = (clip.content_type || '').toLowerCase();
    card.setAttribute('aria-label', `Clip: ${clip.filename || 'Pasted Content'}`);

    const checkboxHTML = `
        <div class="absolute top-3 right-3 z-30 opacity-0 group-hover:opacity-100 focus-within:opacity-100 group-[.has-checked]:opacity-100 transition-opacity duration-200">
            <div class="relative">
                <input type="checkbox" data-id="${clip.id}"
                    aria-label="Select clip ${clip.filename || 'Pasted Content'}"
                    class="clip-checkbox appearance-none w-6 h-6 rounded-lg border-2 border-white bg-black/10 backdrop-blur-sm checked:bg-blue-500 checked:border-blue-500 transition-all cursor-pointer shadow-md peer" ${selectedIds.has(clip.id) ? 'checked' : ''}>
                <svg class="absolute inset-0 w-6 h-6 text-white pointer-events-none opacity-0 peer-checked:opacity-100 transition-opacity p-1" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M5 13l4 4L19 7" />
                </svg>
            </div>
        </div>
    `;

    let previewHTML = '';

    if (clip.content_type.startsWith('image/')) {
        // For images, show loading placeholder initially
        previewHTML = `<div class="preview-container overflow-hidden h-56 w-full bg-gray-100 flex items-center justify-center">
            <img data-clip-id="${clip.id}" alt="${escapeHTML(clip.filename) || 'Uploaded image'}" class="h-full w-full object-cover group-hover:scale-105 transition-transform duration-500 hidden">
            <div class="loading-spinner text-gray-400">
                <svg class="animate-spin h-8 w-8" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                    <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                    <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                </svg>
            </div>
        </div>`;
    } else if (clip.content_type === 'text/html') {
        // For HTML, show text preview (no iframe in Wails)
        const htmlPreview = escapeHTML(clip.preview || '').substring(0, 200);
        previewHTML = `<div class="preview-container h-56 w-full relative bg-gray-100">
            <div class="p-4 text-xs text-gray-500 font-mono overflow-hidden h-full">${htmlPreview}...</div>
            <div class="absolute inset-0 bg-transparent" title="HTML Preview"></div>
        </div>`;
    } else if (clip.content_type.startsWith('text/') || clip.content_type === 'application/json') {
        previewHTML = `<div class="preview-container h-56 w-full overflow-hidden bg-gray-900"><pre class="p-4 text-[10px] leading-relaxed overflow-auto h-full text-gray-300"><code>${escapeHTML(clip.preview)}</code></pre></div>`;
    } else {
        previewHTML = `
        <div class="preview-container h-56 w-full flex flex-col items-center justify-center bg-[#f1f5f9] text-[#94a3b8]">
            <svg class="w-16 h-16" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1" d="M7 21h10a2 2 0 002-2V9.414a1 1 0 00-.293-.707l-5.414-5.414A1 1 0 0012.586 3H7a2 2 0 00-2 2v14a2 2 0 002 2z"></path></svg>
            <span class="mt-4 text-xs font-bold uppercase tracking-widest">${clip.content_type.split('/')[1] || 'FILE'}</span>
        </div>`;
    }

    let expirationBadge = '';
    if (clip.expires_at) {
        expirationBadge = `<div class="absolute top-3 left-3 bg-amber-500 text-white text-[10px] font-black px-2.5 py-1 rounded-lg shadow-lg z-20 flex items-center border border-amber-400">
            TEMP
        </div>`;
    }

    card.innerHTML = `
        ${checkboxHTML}
        <div class="relative cursor-pointer" data-action="open-lightbox">
            ${expirationBadge}
            ${previewHTML}
            <div class="absolute inset-0 bg-black/0 group-hover:bg-black/5 transition-colors"></div>
        </div>

        <!-- Action Toolbar (Icons + Labels) -->
        <div class="px-2 py-3 border-b border-gray-100 flex items-center justify-between bg-[#f8fafc]">
            <button class="flex flex-col items-center gap-1 group/btn p-1.5 flex-1" data-action="copy-path" title="Copy Path">
                <svg class="w-4 h-4 text-gray-400 group-hover/btn:text-blue-500 transition-colors" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M8 5H6a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2v-1M8 5a2 2 0 002 2h2a2 2 0 002-2M8 5a2 2 0 012-2h2a2 2 0 012 2m0 0h2a2 2 0 012 2v3m2 4H10m0 0l3-3m-3 3l3 3"></path></svg>
                <span class="text-[9px] font-bold text-gray-500 group-hover/btn:text-blue-600 uppercase tracking-tighter transition-colors">Copy Path</span>
            </button>
            <button class="flex flex-col items-center gap-1 group/btn p-1.5 flex-1" data-action="save-file" title="Save File">
                <svg class="w-4 h-4 text-gray-400 group-hover/btn:text-blue-500 transition-colors" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M4 16v1a2 2 0 002 2h12a2 2 0 002-2v-1m-4-4l-4 4m0 0l-4-4m4 4V4"></path></svg>
                <span class="text-[9px] font-bold text-gray-500 group-hover/btn:text-blue-600 uppercase tracking-tighter transition-colors">Save</span>
            </button>
            ${isEditableType(clip.content_type) ? `
            <button class="flex flex-col items-center gap-1 group/btn p-1.5 flex-1" data-action="edit" title="Edit">
                <svg class="w-4 h-4 text-gray-400 group-hover/btn:text-green-500 transition-colors" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"></path></svg>
                <span class="text-[9px] font-bold text-gray-500 group-hover/btn:text-green-600 uppercase tracking-tighter transition-colors">Edit</span>
            </button>
            ` : ''}
            <button class="flex flex-col items-center gap-1 group/btn p-1.5 flex-1" data-action="archive" title="${isViewingArchive ? 'Restore' : 'Archive'}">
                <svg class="w-4 h-4 text-gray-400 group-hover/btn:text-orange-500 transition-colors" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24">
                    ${isViewingArchive
            ? '<path stroke-linecap="round" stroke-linejoin="round" d="M3 10h10a8 8 0 018 8v2M3 10l6 6m-6-6l6-6"></path>'
            : '<path stroke-linecap="round" stroke-linejoin="round" d="M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4"></path>'
        }
                </svg>
                <span class="text-[9px] font-bold text-gray-500 group-hover/btn:text-orange-600 uppercase tracking-tighter transition-colors">${isViewingArchive ? 'Restore' : 'Archive'}</span>
            </button>
            <button class="flex flex-col items-center gap-1 group/btn p-1.5 flex-1" data-action="delete" title="Delete">
                <svg class="w-4 h-4 text-gray-400 group-hover/btn:text-red-500 transition-colors" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"></path></svg>
                <span class="text-[9px] font-bold text-gray-500 group-hover/btn:text-red-600 uppercase tracking-tighter transition-colors">Delete</span>
            </button>
        </div>

        <div class="p-4 flex flex-col justify-between flex-1">
            <p class="text-[15px] font-bold text-[#1e293b] truncate mb-1" title="${escapeHTML(clip.filename) || 'Pasted Content'}">
                ${escapeHTML(clip.filename) || '<em class="text-gray-400 not-italic font-normal">Pasted Content</em>'}
            </p>
            <div class="flex justify-between items-center text-[11px] font-bold uppercase tracking-widest">
                <span class="text-[#94a3b8]">${clip.content_type.split('/')[1]?.toUpperCase() || 'FILE'}</span>
                <span class="text-[#cbd5e1]">${new Date(clip.created_at).toLocaleDateString([], { month: 'short', day: 'numeric' })}</span>
            </div>
        </div>
    `;

    // Action button listeners
    card.querySelector('[data-action="copy-path"]').addEventListener('click', (e) => { e.stopPropagation(); saveTempFile(clip.id); });
    card.querySelector('[data-action="save-file"]').addEventListener('click', (e) => { e.stopPropagation(); saveClipToFile(clip.id); });
    const editBtn = card.querySelector('[data-action="edit"]');
    if (editBtn) {
        editBtn.addEventListener('click', (e) => { e.stopPropagation(); openEditor(clip.id); });
    }
    card.querySelector('[data-action="archive"]').addEventListener('click', (e) => { e.stopPropagation(); toggleArchiveClip(clip.id); });
    card.querySelector('[data-action="delete"]').addEventListener('click', (e) => { e.stopPropagation(); deleteClip(clip.id); });

    // Checkbox logic
    const checkbox = card.querySelector('.clip-checkbox');
    checkbox.addEventListener('change', (e) => {
        const id = Number(clip.id);
        if (e.target.checked) {
            selectedIds.add(id);
            card.classList.add('has-checked');
        } else {
            selectedIds.delete(id);
            card.classList.remove('has-checked');
        }

        // Sync Select All checkbox
        const allCheckboxes = Array.from(gallery.querySelectorAll('.clip-checkbox'));
        selectAllCheckbox.checked = allCheckboxes.length > 0 && allCheckboxes.every(cb => cb.checked);

        updateBulkToolbar();
    });

    // Prevent lightbox trigger if clicking checkbox
    checkbox.addEventListener('click', (e) => e.stopPropagation());

    // Lightbox trigger logic
    if (clip.content_type.startsWith('image/')) {
        const imageIndex = imageClips.length;
        imageClips.push(clip);
        card.querySelector('[data-action="open-lightbox"]').addEventListener('click', () => openLightbox(imageIndex));

        // Load image asynchronously
        loadImageForCard(clip.id, card);
    } else {
        // For non-images, clicking opens the editor or shows content
        card.querySelector('[data-action="open-lightbox"]').addEventListener('click', () => {
            if (isEditableType(clip.content_type)) {
                openEditor(clip.id);
            }
        });
    }

    gallery.appendChild(card);
}

// Load image data for a card
async function loadImageForCard(clipId, card) {
    try {
        const clipData = await getClipData(clipId);
        const dataUrl = `data:${clipData.content_type};base64,${clipData.data}`;

        // Cache the data URL
        imageCache.set(clipId, dataUrl);

        const img = card.querySelector(`img[data-clip-id="${clipId}"]`);
        const spinner = card.querySelector('.loading-spinner');

        if (img) {
            img.src = dataUrl;
            img.classList.remove('hidden');
        }
        if (spinner) {
            spinner.remove();
        }
    } catch (error) {
        console.error(`Failed to load image for clip ${clipId}:`, error);
        const spinner = card.querySelector('.loading-spinner');
        if (spinner) {
            spinner.innerHTML = '<span class="text-red-400 text-xs">Failed to load</span>';
        }
    }
}

// Get cached or load image data URL
async function getImageDataUrl(clipId) {
    if (imageCache.has(clipId)) {
        return imageCache.get(clipId);
    }

    const clipData = await getClipData(clipId);
    const dataUrl = `data:${clipData.content_type};base64,${clipData.data}`;
    imageCache.set(clipId, dataUrl);
    return dataUrl;
}

function updateBulkToolbar() {
    const count = selectedIds.size;
    if (count > 0) {
        bulkToolbar.classList.remove('hidden', 'translate-y-full', 'opacity-0', 'pointer-events-none');
        bulkToolbar.classList.add('translate-y-0', 'opacity-100', 'pointer-events-auto');
        selectedCountEl.textContent = `${count} item${count > 1 ? 's' : ''} selected`;
        bulkArchiveText.textContent = isViewingArchive ? 'Restore' : 'Archive';

        // Comparison Logic: Show compare button if 2 items are selected and BOTH are images
        if (count === 2) {
            const selectedImages = Array.from(selectedIds).filter(id => {
                const card = gallery.querySelector(`li[data-id="${id}"]`);
                return card && card.dataset.type.startsWith('image/');
            });

            if (selectedImages.length === 2) {
                bulkCompareBtn.classList.remove('hidden');
            } else {
                bulkCompareBtn.classList.add('hidden');
            }
        } else {
            bulkCompareBtn.classList.add('hidden');
        }
    } else {
        bulkToolbar.classList.remove('translate-y-0', 'opacity-100', 'pointer-events-auto');
        bulkToolbar.classList.add('translate-y-full', 'opacity-0', 'pointer-events-none');
        selectAllCheckbox.checked = false;
        bulkCompareBtn.classList.add('hidden');
    }
}

function toggleSelectAll() {
    const checkboxes = gallery.querySelectorAll('.clip-checkbox');
    const shouldSelectAll = selectAllCheckbox.checked;

    checkboxes.forEach(cb => {
        const id = Number(cb.dataset.id);
        cb.checked = shouldSelectAll;
        const card = cb.closest('li');
        if (shouldSelectAll) {
            selectedIds.add(id);
            if (card) card.classList.add('has-checked');
        } else {
            selectedIds.delete(id);
            if (card) card.classList.remove('has-checked');
        }
    });
    updateBulkToolbar();
}

function cancelSelection() {
    selectedIds.clear();
    const checkboxes = gallery.querySelectorAll('.clip-checkbox');
    checkboxes.forEach(cb => {
        cb.checked = false;
        const card = cb.closest('li');
        if (card) card.classList.remove('has-checked');
    });
    selectAllCheckbox.checked = false;
    updateBulkToolbar();
}

function toggleViewMode() {
    isViewingArchive = !isViewingArchive;
    toggleArchiveViewBtn.setAttribute('aria-pressed', isViewingArchive);
    if (isViewingArchive) {
        archiveBtnText.textContent = "View Active";
        toggleArchiveViewBtn.classList.replace('bg-[#e2e8f0]', 'bg-blue-600');
        toggleArchiveViewBtn.classList.replace('hover:bg-[#cbd5e1]', 'hover:bg-blue-700');
        toggleArchiveViewBtn.classList.replace('text-[#334155]', 'text-white');
        uploadSection.classList.add('opacity-50', 'pointer-events-none'); // Disable upload in archive view
        uploadSection.setAttribute('aria-hidden', 'true');
    } else {
        archiveBtnText.textContent = "View Archive";
        toggleArchiveViewBtn.classList.replace('bg-blue-600', 'bg-[#e2e8f0]');
        toggleArchiveViewBtn.classList.replace('hover:bg-blue-700', 'hover:bg-[#cbd5e1]');
        toggleArchiveViewBtn.classList.replace('text-white', 'text-[#334155]');
        uploadSection.classList.remove('opacity-50', 'pointer-events-none');
        uploadSection.removeAttribute('aria-hidden');
    }
    // Clear image cache when switching views
    imageCache.clear();
    loadClips();
}

// Search Logic
const searchInput = document.getElementById('search-input');
searchInput.addEventListener('input', (e) => {
    const query = e.target.value.toLowerCase();
    const cards = gallery.querySelectorAll('li');
    cards.forEach(card => {
        const filename = card.dataset.filename || '';
        const type = card.dataset.type || '';
        if (filename.includes(query) || type.includes(query)) {
            card.style.display = '';
        } else {
            card.style.display = 'none';
        }
    });
});
