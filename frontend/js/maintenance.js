// --- Maintenance Module ---

const maintenanceModal = document.getElementById('maintenance-modal');
const openMaintenanceBtn = document.getElementById('open-maintenance-btn');
const maintenanceCloseBtn = document.getElementById('maintenance-close');
const maintenanceDeduplicateBtn = document.getElementById('maintenance-deduplicate-btn');
const maintenanceRemoveEmptyTagsBtn = document.getElementById('maintenance-remove-empty-tags-btn');
const maintenanceCompactDbBtn = document.getElementById('maintenance-compact-db-btn');
const maintenanceStaleFilesBtn = document.getElementById('maintenance-stale-files-btn');
const maintenanceOrphanRowsBtn = document.getElementById('maintenance-orphan-rows-btn');
const maintenanceMarkdownCacheSection = document.getElementById('maintenance-markdown-cache-section');
const maintenanceMarkdownCacheSize = document.getElementById('maintenance-markdown-cache-size');
const maintenanceClearMarkdownCacheBtn = document.getElementById('maintenance-clear-markdown-cache-btn');

let lastFocusedElementBeforeMaintenance = null;
let maintenanceFocusTrapCleanup = null;

async function refreshMarkdownCacheStats() {
    const api = window.go?.main?.MarkdownService;
    if (!api) {
        maintenanceMarkdownCacheSection?.classList.add('hidden');
        return;
    }
    maintenanceMarkdownCacheSection?.classList.remove('hidden');
    try {
        const stats = await api.GetImageCacheStats();
        maintenanceMarkdownCacheSize.textContent = `${formatFileSize(stats.bytes || 0)} · ${stats.entries || 0} image${stats.entries === 1 ? '' : 's'}`;
    } catch (error) {
        maintenanceMarkdownCacheSize.textContent = 'Unavailable';
    }
}

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
    refreshMarkdownCacheStats();
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
        }, null, { confirmLabel: 'Deduplicate' });
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
    }, null, { confirmLabel: 'Remove' });
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
                const { before = 0, after = 0 } = await window.go.main.App.CompactDatabase() || {};
                const reclaimed = before - after;
                showToast(`Reclaimed ${formatBytes(reclaimed)} (was ${formatBytes(before)}, now ${formatBytes(after)})`, 'success');
            } catch (err) {
                showToast('Compact failed', 'error');
            }
        },
        null,
        { confirmLabel: 'Compact', variant: 'primary' }
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
                const { count = 0, bytes = 0 } = await window.go.main.App.CleanStaleFiles() || {};
                showToast(`Removed ${count} file${count !== 1 ? 's' : ''} (${formatFileSize(bytes)})`, 'success');
            } catch (err) {
                showToast('Sweep failed', 'error');
            }
        },
        null,
        { confirmLabel: 'Sweep' }
    );
}

async function runOrphanRowsSweep() {
    let report;
    try {
        report = await window.go.main.App.GetOrphanDBRows();
    } catch (err) {
        showToast('Failed to scan orphans', 'error');
        return;
    }
    const total = (report.plugin_storage||0) + (report.plugin_permissions||0)
        + (report.stale_follows||0) + (report.stale_auto_tags||0)
        + (report.stale_hidden_tag_ids||0);
    if (total === 0) {
        showToast('No orphan rows to clean');
        return;
    }
    const listHTML = [
        ['Plugin storage rows', report.plugin_storage],
        ['Plugin permissions', report.plugin_permissions],
        ['Stale follows', report.stale_follows],
        ['Stale auto-tag folders (will be NULL-ed)', report.stale_auto_tags],
        ['Stale hidden-tag IDs', report.stale_hidden_tag_ids],
    ].filter(([_, n]) => n > 0)
     .map(([label, n]) => `<span class="block text-left">&middot; ${escapeHTML(label)}: ${n}</span>`)
     .join('');
    closeMaintenance();
    showConfirmDialog('Clean orphan rows',
        `<span class="block mb-2">Will clean:</span>` +
        `<span class="block text-[10px] text-stone-400 mb-2">${listHTML}</span>`,
        async () => {
            try {
                const cleaned = await window.go.main.App.CleanOrphanDBRows();
                const cleanedTotal = (cleaned.plugin_storage||0) + (cleaned.plugin_permissions||0)
                    + (cleaned.stale_follows||0) + (cleaned.stale_auto_tags||0)
                    + (cleaned.stale_hidden_tag_ids||0);
                showToast(`Cleaned ${cleanedTotal} orphan row${cleanedTotal !== 1 ? 's' : ''}`, 'success');
            } catch (err) {
                showToast('Orphan cleanup failed', 'error');
            }
        },
        null,
        { confirmLabel: 'Clean' }
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
maintenanceOrphanRowsBtn?.addEventListener('click', runOrphanRowsSweep);
maintenanceClearMarkdownCacheBtn?.addEventListener('click', () => {
    closeMaintenance();
    showConfirmDialog(
        'Clear Markdown Image Cache?',
        'Cached remote Markdown images will be removed. Clips are not affected.',
        async () => {
            try {
                await window.go.main.MarkdownService.ClearImageCache();
                showToast('Markdown image cache cleared', 'success');
            } catch (error) {
                showToast('Failed to clear Markdown image cache', 'error');
            }
        },
        null,
        { variant: 'danger', confirmLabel: 'Clear Cache' }
    );
});
