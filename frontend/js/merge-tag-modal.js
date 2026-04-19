// --- Merge Tag Modal ---

const mergeTagModal = document.getElementById('merge-tag-modal');
const mergeTagCloseBtn = document.getElementById('merge-tag-close');
const mergeTagCancelBtn = document.getElementById('merge-tag-cancel');
const mergeTagConfirmBtn = document.getElementById('merge-tag-confirm');
const mergeTagSourceLabel = document.getElementById('merge-tag-source');
const mergeTagDestInput = document.getElementById('merge-tag-dest-input');
const mergeTagPreview = document.getElementById('merge-tag-preview');

let mergeTagSourceID = null;
let mergeTagAutocomplete = null;
let mergePreviewTimer = null;

async function openMergeTagModal(sourceID, sourceName) {
    mergeTagSourceID = sourceID;
    mergeTagSourceLabel.textContent = sourceName;
    mergeTagDestInput.value = '';
    mergeTagPreview.textContent = '';
    mergeTagConfirmBtn.disabled = true;
    delete mergeTagConfirmBtn.dataset.destId;

    // Refresh autocomplete cache so renamed/new tags are picked up.
    if (mergeTagAutocomplete) {
        mergeTagAutocomplete.refresh();
    }

    mergeTagModal.removeAttribute('inert');
    mergeTagModal.classList.remove('opacity-0', 'pointer-events-none');
    mergeTagModal.classList.add('opacity-100');
    mergeTagModal.querySelector(':scope > div').classList.remove('scale-95');
    mergeTagModal.querySelector(':scope > div').classList.add('scale-100');

    if (window.TagAutocomplete && !mergeTagAutocomplete) {
        // Merge requires an existing destination; suppress "Create new" suggestions.
        mergeTagAutocomplete = window.TagAutocomplete.attach(mergeTagDestInput, {
            allowCreate: false,
            onSelect: () => updateMergePreview(),
        });
    }
    mergeTagDestInput.focus();
}

function closeMergeTagModal() {
    mergeTagModal.classList.add('opacity-0', 'pointer-events-none');
    mergeTagModal.classList.remove('opacity-100');
    mergeTagModal.querySelector(':scope > div').classList.add('scale-95');
    mergeTagModal.querySelector(':scope > div').classList.remove('scale-100');
    mergeTagModal.setAttribute('inert', '');
    mergeTagSourceID = null;
}

async function updateMergePreview() {
    if (mergePreviewTimer) clearTimeout(mergePreviewTimer);
    mergePreviewTimer = setTimeout(async () => {
        const destName = mergeTagDestInput.value.trim();
        if (!destName || mergeTagSourceID == null) {
            mergeTagPreview.textContent = '';
            mergeTagConfirmBtn.disabled = true;
            return;
        }
        let tags;
        try {
            tags = await window.go.main.App.GetTags();
        } catch (err) {
            mergeTagPreview.textContent = `Error loading tags: ${err.message || err}`;
            mergeTagConfirmBtn.disabled = true;
            return;
        }
        const dest = tags.find(t => t.name === destName);
        if (!dest) {
            mergeTagPreview.textContent = `"${destName}" does not exist. Create it first.`;
            mergeTagConfirmBtn.disabled = true;
            return;
        }
        try {
            const preview = await window.go.main.App.PreviewMergeTag(mergeTagSourceID, dest.id);
            if (preview.blockers && preview.blockers.length > 0) {
                mergeTagPreview.innerHTML = preview.blockers.map(b =>
                    `<span class="block text-red-500">${escapeHTML(b)}</span>`
                ).join('');
                mergeTagConfirmBtn.disabled = true;
                return;
            }
            mergeTagPreview.innerHTML = `
                <span class="block">${preview.clip_count} clip${preview.clip_count !== 1 ? 's' : ''} will be reassigned.</span>
                <span class="block">${preview.descendant_count} descendant tag${preview.descendant_count !== 1 ? 's' : ''} will move under ${escapeHTML(preview.dest_name)}/.</span>`;
            mergeTagConfirmBtn.disabled = false;
            mergeTagConfirmBtn.dataset.destId = dest.id;
        } catch (err) {
            mergeTagPreview.textContent = `Error: ${err.message || err}`;
            mergeTagConfirmBtn.disabled = true;
        }
    }, 200);
}

mergeTagDestInput?.addEventListener('input', updateMergePreview);
mergeTagCloseBtn?.addEventListener('click', closeMergeTagModal);
mergeTagCancelBtn?.addEventListener('click', closeMergeTagModal);
mergeTagModal?.addEventListener('click', (e) => {
    if (e.target === mergeTagModal) closeMergeTagModal();
});
mergeTagModal?.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
        e.stopPropagation();
        closeMergeTagModal();
    }
});
mergeTagConfirmBtn?.addEventListener('click', async () => {
    const destID = parseInt(mergeTagConfirmBtn.dataset.destId, 10);
    if (!destID || mergeTagSourceID == null) return;
    try {
        await window.go.main.App.MergeTag(mergeTagSourceID, destID);
        if (typeof showToast === 'function') showToast('Tag merged', 'success');
        closeMergeTagModal();
        // Event handler (tag:merged) reloads state + re-navigates.
    } catch (err) {
        if (typeof showToast === 'function') showToast(`Merge failed: ${err.message || err}`, 'error');
    }
});

window.openMergeTagModal = openMergeTagModal;
