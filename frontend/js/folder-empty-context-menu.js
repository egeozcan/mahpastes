// Folder empty-space context menu.
// Right-click on the empty area of the gallery in folder mode shows:
//   - "New Folder" (always)
//   - The containing folder's options (when inside a folder)

const FolderEmptyContextMenu = (() => {

    function attach(galleryEl) {
        galleryEl.addEventListener('contextmenu', (e) => {
            // Only in folder mode
            if (typeof isFolderMode !== 'function' || !isFolderMode()) return;

            // Ignore right-clicks on folder cards, clip cards, or the empty-state paragraph
            const closestCard = e.target.closest('[data-folder], [data-id]');
            if (closestCard) return;
            // Also ignore right-clicks on text elements like the empty-state message
            if (e.target.closest('p')) return;

            e.preventDefault();
            e.stopPropagation();
            open(e);
        });
    }

    function open(e) {
        const anchor = { top: e.clientY, left: e.clientX, right: e.clientX, bottom: e.clientY, width: 0, height: 0 };
        const items = buildItems();
        ContextMenu.open(items, null, anchor, (action) => handleAction(action));
    }

    function buildItems() {
        const items = [];

        // "New Folder" — always present
        items.push({ id: 'new-folder', label: 'New Folder', iconHtml: iconNewFolder() });

        // If inside a folder, add the containing folder's options
        if (typeof activeTagFilters !== 'undefined' && activeTagFilters.length > 0
            && typeof allTags !== 'undefined' && typeof FolderContextMenu !== 'undefined'
            && FolderContextMenu.buildItems && FolderContextMenu.readLiveState) {

            const currentTagId = activeTagFilters[activeTagFilters.length - 1];
            const tag = allTags.find(t => t.id === currentTagId);
            if (tag) {
                items.push({ type: 'divider' });
                const state = FolderContextMenu.readLiveState(tag);
                const folderItems = FolderContextMenu.buildItems(tag, state);
                items.push(...folderItems);
            }
        }

        return items;
    }

    function iconNewFolder() {
        return `<svg class="w-3 h-3 opacity-60" fill="none" stroke="currentColor" stroke-width="1.5" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" d="M12 10.5v6m3-3H9m3.75-7.808L15.374 3.55a1.724 1.724 0 0 1 1.278-.56H19.5A2.25 2.25 0 0 1 21.75 5.25v13.5A2.25 2.25 0 0 1 19.5 21H4.5A2.25 2.25 0 0 1 2.25 18.75V5.25A2.25 2.25 0 0 1 4.5 3h5.793c.48 0 .94.19 1.278.55Z"/>
        </svg>`;
    }

    async function handleAction(action) {
        if (action === 'new-folder') {
            handleNewFolder();
            return;
        }

        // Delegate to FolderContextMenu for containing-folder actions
        if (typeof FolderContextMenu !== 'undefined' && FolderContextMenu.handleAction
            && typeof activeTagFilters !== 'undefined' && activeTagFilters.length > 0
            && typeof allTags !== 'undefined') {

            const currentTagId = activeTagFilters[activeTagFilters.length - 1];
            const tag = allTags.find(t => t.id === currentTagId);
            if (tag) {
                const state = FolderContextMenu.readLiveState(tag);
                FolderContextMenu.handleAction(action, tag, state);
            }
        }
    }

    async function handleNewFolder() {
        let prefix = '';
        if (typeof activeTagFilters !== 'undefined' && activeTagFilters.length > 0
            && typeof allTags !== 'undefined') {
            const currentTagId = activeTagFilters[activeTagFilters.length - 1];
            const currentTag = allTags.find(t => t.id === currentTagId);
            if (currentTag) {
                prefix = currentTag.name + '/';
            }
        }

        if (typeof showPromptDialog !== 'function') return;
        showPromptDialog('New Folder', prefix, async (name) => {
            if (!name || !name.trim()) return;
            const tagName = name.trim();

            // Validate
            if (typeof validateTagName === 'function') {
                if (!validateTagName(tagName)) {
                    // validateTagName already shows a toast on failure
                    return;
                }
            } else if (!tagName) {
                if (typeof showToast === 'function') showToast('Folder name cannot be empty.');
                return;
            }

            if (typeof createTag !== 'function') return;
            const tag = await createTag(tagName);
            if (tag) {
                if (typeof loadTags === 'function') await loadTags();
                if (typeof renderTagFilterDropdown === 'function') renderTagFilterDropdown();
                if (typeof loadClips === 'function') loadClips();
            }
        });
    }

    return { attach };
})();

window.FolderEmptyContextMenu = FolderEmptyContextMenu;
