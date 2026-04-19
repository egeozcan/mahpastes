// --- Maintenance Module ---

const maintenanceModal = document.getElementById('maintenance-modal');
const openMaintenanceBtn = document.getElementById('open-maintenance-btn');
const maintenanceCloseBtn = document.getElementById('maintenance-close');
const maintenanceDeduplicateBtn = document.getElementById('maintenance-deduplicate-btn');
const maintenanceRemoveEmptyTagsBtn = document.getElementById('maintenance-remove-empty-tags-btn');
const maintenanceCompactDbBtn = document.getElementById('maintenance-compact-db-btn');
const maintenanceStaleFilesBtn = document.getElementById('maintenance-stale-files-btn');

let lastFocusedElementBeforeMaintenance = null;
let maintenanceFocusTrapCleanup = null;

function openMaintenance() {
    lastFocusedElementBeforeMaintenance = document.activeElement;
    maintenanceModal.removeAttribute('inert');
    maintenanceModal.classList.remove('opacity-0', 'pointer-events-none');
    maintenanceModal.classList.add('opacity-100');
    maintenanceModal.querySelector(':scope > div').classList.remove('scale-95');
    maintenanceModal.querySelector(':scope > div').classList.add('scale-100');
    if (maintenanceFocusTrapCleanup) maintenanceFocusTrapCleanup();
    maintenanceFocusTrapCleanup = trapFocus(maintenanceModal);
    maintenanceModal.focus();
}

function closeMaintenance() {
    if (maintenanceFocusTrapCleanup) {
        maintenanceFocusTrapCleanup();
        maintenanceFocusTrapCleanup = null;
    }
    maintenanceModal.classList.add('opacity-0', 'pointer-events-none');
    maintenanceModal.classList.remove('opacity-100');
    maintenanceModal.querySelector(':scope > div').classList.add('scale-95');
    maintenanceModal.querySelector(':scope > div').classList.remove('scale-100');
    maintenanceModal.setAttribute('inert', '');
    if (lastFocusedElementBeforeMaintenance && lastFocusedElementBeforeMaintenance !== document.body
        && !lastFocusedElementBeforeMaintenance.closest('[inert]')) {
        lastFocusedElementBeforeMaintenance.focus();
    } else {
        document.getElementById('drawer-toggle-btn')?.focus();
    }
    lastFocusedElementBeforeMaintenance = null;
}

async function runDeduplicate() {
    try {
        const groups = await window.go.main.App.GetDuplicateGroups();
        if (!groups || groups.length === 0) {
            showToast('No duplicates found');
            return;
        }

        const totalRemoved = groups.reduce((sum, g) => sum + (g.count - 1), 0);
        const listHTML = groups.map(g =>
            `<span class="block text-left">&middot; ${escapeHTML(g.filename || 'Untitled')} — ${g.count} copies (oldest kept, ${g.count - 1} removed)</span>`
        ).join('');

        const message = `<span class="block mb-2">${groups.length} duplicate group${groups.length > 1 ? 's' : ''} found:</span>` +
            `<span class="block text-[10px] text-stone-400 mb-2 max-h-40 overflow-y-auto">${listHTML}</span>` +
            `<span class="block">Tags will be merged. ${totalRemoved} clip${totalRemoved > 1 ? 's' : ''} will be removed.</span>`;

        // Close the maintenance modal so the confirm dialog stacks over the gallery rather than over another modal.
        closeMaintenance();

        showConfirmDialog('Deduplicate All', message, async () => {
            try {
                const removed = await window.go.main.App.DeduplicateAll();
                showToast(`Deduplicated: removed ${removed} clip${removed !== 1 ? 's' : ''}`, 'success');
                loadClips();
            } catch (err) {
                showToast('Failed to deduplicate', 'error');
            }
        });
    } catch (err) {
        showToast('Failed to check duplicates', 'error');
    }
}

async function runRemoveEmptyTags() {
    let candidates;
    try {
        // Dry-run preview: backend iterates the cascade inside a rolled-back
        // transaction, so this list is exactly what a commit will remove.
        candidates = await window.go.main.App.GetRemovableEmptyTags();
    } catch (err) {
        showToast('Failed to scan for empty tags', 'error');
        return;
    }

    if (!candidates || candidates.length === 0) {
        showToast('No empty tags to remove');
        return;
    }

    closeMaintenance();

    const listHTML = candidates.map(t => {
        const swatch = t.color
            ? `<span class="inline-block w-2 h-2 rounded-full mr-1.5 align-middle" style="background-color: ${escapeHTML(t.color)}"></span>`
            : '';
        return `<span class="block text-left">${swatch}${escapeHTML(t.name)}</span>`;
    }).join('');

    const message = `<span class="block mb-2">${candidates.length} empty tag${candidates.length > 1 ? 's' : ''} will be deleted:</span>` +
        `<span class="block text-[10px] text-stone-500 mb-2 max-h-40 overflow-y-auto">${listHTML}</span>` +
        `<span class="block">This cannot be undone.</span>`;

    showConfirmDialog('Remove Empty Tags', message, async () => {
        try {
            const removed = await window.go.main.App.RemoveEmptyTags();
            showToast(`Removed ${removed} empty tag${removed !== 1 ? 's' : ''}`, 'success');
            if (typeof loadTags === 'function') {
                await loadTags();
            }
            loadClips();
        } catch (err) {
            showToast('Failed to remove empty tags', 'error');
        }
    });
}

async function runCompactDatabase() {
    let sizeBefore;
    try {
        sizeBefore = await window.go.main.App.GetDatabaseSize();
    } catch (err) {
        showToast('Failed to read DB size', 'error');
        return;
    }
    const humanBefore = formatBytes(sizeBefore);
    closeMaintenance();

    showConfirmDialog(
        'Compact Database',
        `<span class="block mb-2">Current size: ${humanBefore}</span>` +
        `<span class="block">Running VACUUM + ANALYZE may take a few seconds and briefly locks the database.</span>`,
        async () => {
            try {
                const result = await window.go.main.App.CompactDatabase();
                // Wails v2 returns [before, after] for multi-return functions
                const before = Array.isArray(result) ? result[0] : result;
                const after = Array.isArray(result) ? result[1] : 0;
                const reclaimed = before - after;
                showToast(`Reclaimed ${formatBytes(reclaimed)} (was ${formatBytes(before)}, now ${formatBytes(after)})`, 'success');
            } catch (err) {
                showToast('Compact failed', 'error');
            }
        }
    );
}

async function runStaleFileSweep() {
    let files;
    try {
        files = await window.go.main.App.GetStaleFiles();
    } catch (err) {
        showToast('Failed to scan stale files', 'error');
        return;
    }
    if (!files || files.length === 0) {
        showToast('No stale files to clean');
        return;
    }
    const totalBytes = files.reduce((sum, f) => sum + (f.size || 0), 0);
    const grouped = files.reduce((acc, f) => {
        (acc[f.source] = acc[f.source] || []).push(f);
        return acc;
    }, {});
    const listHTML = Object.entries(grouped).map(([src, arr]) =>
        `<span class="block text-left font-semibold text-stone-600 mt-1">${escapeHTML(src)}</span>` +
        arr.map(f => `<span class="block text-left">&middot; ${escapeHTML(f.name)} (${escapeHTML(formatFileSize(f.size))}, ${f.age_hours.toFixed(1)}h)</span>`).join('')
    ).join('');

    closeMaintenance();
    showConfirmDialog(
        'Sweep stale files',
        `<span class="block mb-2">${files.length} file${files.length !== 1 ? 's' : ''} (${escapeHTML(formatFileSize(totalBytes))}):</span>` +
        `<span class="block text-[10px] text-stone-400 mb-2 max-h-40 overflow-y-auto">${listHTML}</span>`,
        async () => {
            try {
                const result = await window.go.main.App.CleanStaleFiles();
                // Wails v2 returns [count, bytes] for multi-return functions
                const count = Array.isArray(result) ? result[0] : result;
                const bytes = Array.isArray(result) ? result[1] : 0;
                showToast(`Removed ${count} file${count !== 1 ? 's' : ''} (${formatFileSize(bytes)})`, 'success');
            } catch (err) {
                showToast('Sweep failed', 'error');
            }
        }
    );
}

// Event listeners
openMaintenanceBtn.addEventListener('click', openMaintenance);
maintenanceCloseBtn.addEventListener('click', closeMaintenance);
maintenanceModal.addEventListener('click', (e) => {
    if (e.target === maintenanceModal) closeMaintenance();
});
maintenanceDeduplicateBtn.addEventListener('click', runDeduplicate);
maintenanceRemoveEmptyTagsBtn.addEventListener('click', runRemoveEmptyTags);
maintenanceCompactDbBtn.addEventListener('click', runCompactDatabase);
maintenanceStaleFilesBtn?.addEventListener('click', runStaleFileSweep);
