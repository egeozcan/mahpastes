// Folder move modal — tree picker, live preview, submit via App.UpdateTag.
const FolderMoveModal = (() => {
    let currentTag = null;
    let selectedDest = null; // { id: number|null, path: string, isRoot: boolean }
    let rootEl = null;

    async function show(tag) {
        currentTag = tag;
        selectedDest = null;
        await ensureMounted();
        await populate();
        openModal();
    }

    async function ensureMounted() {
        if (rootEl) return;
        rootEl = document.createElement('div');
        rootEl.setAttribute('data-testid', 'folder-move-modal');
        rootEl.className = 'fixed inset-0 z-50 hidden items-center justify-center bg-black/40';
        rootEl.innerHTML = `
            <div class="bg-white rounded-md shadow-lg w-[480px] max-w-[90vw] max-h-[80vh] flex flex-col">
                <div class="flex items-center justify-between p-4 border-b border-stone-200">
                    <h2 class="text-sm font-semibold uppercase tracking-wide text-stone-800">Move folder</h2>
                    <button data-testid="folder-move-close" class="text-stone-400 hover:text-stone-600 text-xl leading-none" aria-label="Close">&times;</button>
                </div>
                <div class="p-4 text-xs font-medium text-stone-600">
                    Moving: <span data-testid="folder-move-source" class="text-stone-800"></span>
                </div>
                <div class="flex-1 overflow-y-auto px-4 pb-2">
                    <div class="text-[10px] uppercase tracking-wider text-stone-400 mb-1">Destination</div>
                    <ul data-testid="folder-move-tree" class="border border-stone-200 rounded-md p-2 text-xs font-medium space-y-0.5"></ul>
                </div>
                <div class="px-4 py-2 text-xs font-medium text-stone-600">
                    New path: <code data-testid="folder-move-preview" class="text-stone-800"></code>
                </div>
                <div data-testid="folder-move-error" class="px-4 text-xs text-red-600 hidden"></div>
                <div class="flex justify-end gap-2 p-4 border-t border-stone-200">
                    <button data-testid="folder-move-cancel" class="border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-600 text-xs font-medium py-2 px-3 rounded-md transition-colors">Cancel</button>
                    <button data-testid="folder-move-confirm" class="bg-stone-800 hover:bg-stone-700 text-white text-xs font-medium py-2 px-5 rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed" disabled>Move</button>
                </div>
            </div>`;
        document.body.appendChild(rootEl);

        rootEl.querySelector('[data-testid="folder-move-cancel"]').addEventListener('click', closeModal);
        rootEl.querySelector('[data-testid="folder-move-close"]').addEventListener('click', closeModal);
        rootEl.querySelector('[data-testid="folder-move-confirm"]').addEventListener('click', onConfirm);
        rootEl.addEventListener('click', (e) => { if (e.target === rootEl) closeModal(); });
        document.addEventListener('keydown', onKeydown);
    }

    function onKeydown(e) {
        if (!rootEl || rootEl.classList.contains('hidden')) return;
        if (e.key === 'Escape') closeModal();
    }

    async function populate() {
        const allTags = await window.go.main.App.GetTags();
        const sortedNames = allTags.map(t => t.name).sort();
        const sourcePath = currentTag.name;
        rootEl.querySelector('[data-testid="folder-move-source"]').textContent = sourcePath;

        const descendants = new Set(allTags.filter(t => t.name === sourcePath || t.name.startsWith(sourcePath + '/')).map(t => t.name));
        const parentPath = sourcePath.includes('/') ? sourcePath.substring(0, sourcePath.lastIndexOf('/')) : '';
        const ul = rootEl.querySelector('[data-testid="folder-move-tree"]');
        ul.innerHTML = '';

        // Root option
        const rootLi = document.createElement('li');
        rootLi.className = 'cursor-pointer rounded px-2 py-1 hover:bg-stone-100 flex items-center gap-2';
        rootLi.setAttribute('data-dest-root', 'true');
        if (parentPath === '') {
            rootLi.setAttribute('data-disabled', 'true');
            rootLi.title = 'Already here';
            rootLi.classList.add('opacity-50', 'cursor-not-allowed');
        }
        rootLi.innerHTML = `<span class="text-stone-400">○</span><span>Root (top level)</span>`;
        rootLi.addEventListener('click', () => {
            if (rootLi.getAttribute('data-disabled') === 'true') return;
            selectedDest = { id: null, path: '', isRoot: true };
            refreshSelection();
        });
        ul.appendChild(rootLi);

        // Flat sorted render, indent by depth
        for (const name of sortedNames) {
            const tag = allTags.find(t => t.name === name);
            const depth = (name.match(/\//g) || []).length;
            const li = document.createElement('li');
            li.className = 'cursor-pointer rounded px-2 py-1 hover:bg-stone-100 flex items-center gap-2';
            li.style.paddingLeft = `${8 + depth * 16}px`;
            li.setAttribute('data-dest-name', name.split('/').pop() || '');
            li.setAttribute('data-dest-path', name);
            li.setAttribute('data-tag-id', String(tag.id));
            const shortName = name.split('/').pop();
            li.innerHTML = `<span class="text-stone-400">•</span><span>${escapeHTML(shortName)}</span>`;

            let disabledReason = null;
            if (name === sourcePath) disabledReason = 'Cannot move folder into itself';
            else if (descendants.has(name)) disabledReason = 'Cannot move into own subfolder';
            else if (name === parentPath) disabledReason = 'Already here';
            if (disabledReason) {
                li.setAttribute('data-disabled', 'true');
                li.title = disabledReason;
                li.classList.add('opacity-50', 'cursor-not-allowed');
            }

            li.addEventListener('click', () => {
                if (li.getAttribute('data-disabled') === 'true') return;
                selectedDest = { id: tag.id, path: name, isRoot: false };
                refreshSelection();
            });
            ul.appendChild(li);
        }
    }

    function refreshSelection() {
        rootEl.querySelectorAll('[data-testid="folder-move-tree"] li').forEach(li => li.classList.remove('bg-stone-100'));
        if (selectedDest) {
            let sel;
            if (selectedDest.isRoot) sel = rootEl.querySelector('[data-dest-root="true"]');
            else sel = rootEl.querySelector(`[data-dest-path="${CSS.escape(selectedDest.path)}"]`);
            sel?.classList.add('bg-stone-100');
        }
        const preview = rootEl.querySelector('[data-testid="folder-move-preview"]');
        const shortName = currentTag.name.split('/').pop();
        if (selectedDest) {
            preview.textContent = selectedDest.isRoot ? shortName : `${selectedDest.path}/${shortName}`;
        } else {
            preview.textContent = '';
        }
        rootEl.querySelector('[data-testid="folder-move-confirm"]').disabled = !selectedDest;
        hideError();
    }

    async function onConfirm() {
        if (!selectedDest) return;
        const shortName = currentTag.name.split('/').pop();
        const newPath = selectedDest.isRoot ? shortName : `${selectedDest.path}/${shortName}`;
        try {
            await window.go.main.App.UpdateTag(currentTag.id, newPath, currentTag.color);
            if (typeof showToast === 'function') showToast(`Moved to ${newPath}`, 'success');
            closeModal();
            if (typeof window.renderFolderCards === 'function') await window.renderFolderCards();
            if (typeof loadClips === 'function') await loadClips();
        } catch (e) {
            showError(`Cannot move: ${e?.message || e}`);
        }
    }

    function showError(msg) {
        const el = rootEl.querySelector('[data-testid="folder-move-error"]');
        el.textContent = msg;
        el.classList.remove('hidden');
    }
    function hideError() {
        const el = rootEl.querySelector('[data-testid="folder-move-error"]');
        el.textContent = '';
        el.classList.add('hidden');
    }

    function openModal() {
        rootEl.classList.remove('hidden');
        rootEl.classList.add('flex');
        rootEl.querySelector('[data-testid="folder-move-cancel"]').focus();
    }

    function closeModal() {
        rootEl?.classList.add('hidden');
        rootEl?.classList.remove('flex');
        currentTag = null;
        selectedDest = null;
    }

    function escapeHTML(s) {
        return String(s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
    }

    return { show };
})();
window.FolderMoveModal = FolderMoveModal;
