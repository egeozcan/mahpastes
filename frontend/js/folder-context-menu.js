// Folder context menu for folder view.
// Mirrors the clip card context menu (frontend/js/context-menu.js) pattern but builds
// folder-specific items and handles pointer/keyboard/touch trigger differences.
const FolderContextMenu = (() => {
    const LONG_PRESS_MS = 500;
    const LONG_PRESS_MOVE_PX = 20;

    function attach(cardEl, tag) {
        cardEl.addEventListener('contextmenu', (e) => {
            e.preventDefault();
            openFor(tag, pointerAnchor(e));
        });

        cardEl.addEventListener('keydown', (e) => {
            // Shift+F10 or ContextMenu key → open anchored at the card
            if ((e.shiftKey && e.key === 'F10') || e.key === 'ContextMenu') {
                e.preventDefault();
                openFor(tag, cardEl);
            }
        });

        // Touch long-press
        let touch = null;
        cardEl.addEventListener('touchstart', (e) => {
            if (e.touches.length !== 1) return;
            const t = e.touches[0];
            touch = { x: t.clientX, y: t.clientY, time: Date.now() };
        }, { passive: true });
        cardEl.addEventListener('touchmove', (e) => {
            if (!touch) return;
            const t = e.touches[0];
            const dx = t.clientX - touch.x, dy = t.clientY - touch.y;
            if (Math.hypot(dx, dy) > LONG_PRESS_MOVE_PX) touch = null;
        }, { passive: true });
        cardEl.addEventListener('touchend', (e) => {
            if (touch && Date.now() - touch.time >= LONG_PRESS_MS) {
                e.preventDefault();
                openFor(tag, { top: touch.y, left: touch.x, right: touch.x, bottom: touch.y, width: 0, height: 0 });
            }
            touch = null;
        });
    }

    function pointerAnchor(e) {
        const x = e.clientX, y = e.clientY;
        return { top: y, left: x, right: x, bottom: y, width: 0, height: 0 };
    }

    function openFor(tag, anchor) {
        const state = readLiveState(tag);
        const items = buildItems(tag, state);
        ContextMenu.open(items, tag.id, anchor, (action, id, item) => handleAction(action, tag, state));
        // Tag the menu so folder-specific selectors can distinguish it from the clip menu.
        const menuEl = document.querySelector('.card-menu-dropdown');
        if (menuEl) menuEl.setAttribute('data-source', 'folder');
    }

    function readLiveState(tag) {
        const s = (window.folderStatusMap && window.folderStatusMap.get(tag.id)) || {};
        const hidden = (typeof getHiddenTags === 'function') ? (getHiddenTags() || []) : [];
        return {
            served: !!s.served,
            servePaused: !!s.servePaused,
            shared: !!s.shared,
            sharePaused: !!s.sharePaused,
            hidden: hidden.includes(tag.id),
        };
    }

    function buildItems(tag, state) {
        const items = [];
        items.push({ id: 'open', label: 'Open', iconHtml: iconOpen() });
        items.push({ type: 'divider' });
        items.push({ id: 'move', label: 'Move…', iconHtml: iconMove() });
        items.push({ id: 'rename', label: 'Rename…', iconHtml: iconPencil() });
        items.push({ type: 'divider' });
        items.push(state.served
            ? { id: 'stop-serve', label: 'Stop serving', iconHtml: iconServe(), danger: true }
            : { id: 'serve', label: 'Serve…', iconHtml: iconServe() });
        items.push(state.shared
            ? { id: 'stop-share', label: 'Stop sharing', iconHtml: iconShare(), danger: true }
            : { id: 'share', label: 'Share…', iconHtml: iconShare() });
        items.push({ type: 'divider' });
        items.push(state.hidden
            ? { id: 'hide', label: 'Unhide', iconHtml: iconEye() }
            : { id: 'hide', label: 'Hide', iconHtml: iconEyeOff() });
        return items;
    }

    async function handleAction(action, tag, state) {
        try {
            switch (action) {
                case 'open':
                    if (typeof navigateToFolder === 'function') navigateToFolder(tag.id);
                    return;
                case 'move':
                    if (typeof FolderMoveModal !== 'undefined') FolderMoveModal.show(tag);
                    return;
                case 'rename':
                    if (typeof openFolderRenameDialog === 'function') openFolderRenameDialog(tag.id, tag.name);
                    return;
                case 'serve':
                    if (typeof openServeViewForTag === 'function') await openServeViewForTag(tag.id);
                    return;
                case 'stop-serve':
                    await doStopServing(tag.id);
                    return;
                case 'share':
                    if (typeof openShareFlowForTag === 'function') await openShareFlowForTag(tag.id);
                    return;
                case 'stop-share':
                    await doStopSharing(tag.id);
                    return;
                case 'hide':
                    await toggleHidden(tag.id, !state.hidden);
                    return;
            }
        } catch (e) {
            console.error('folder context menu action failed:', e);
            if (typeof showToast === 'function') showToast(e?.message || String(e), 'error');
        }
    }

    async function toggleHidden(tagID, shouldHide) {
        const existing = (typeof getHiddenTags === 'function') ? (getHiddenTags() || []).slice() : [];
        let next;
        if (shouldHide) {
            if (!existing.includes(tagID)) existing.push(tagID);
            next = existing;
        } else {
            next = existing.filter(id => id !== tagID);
        }
        // Optimistic update + persist
        if (typeof setHiddenTagsState === 'function') setHiddenTagsState(next);
        try {
            await window.go.main.App.SetHiddenTags(next);
            if (typeof showToast === 'function') showToast(shouldHide ? 'Folder hidden' : 'Folder unhidden', 'success');
            if (typeof window.renderFolderCards === 'function') await window.renderFolderCards();
        } catch (e) {
            if (typeof setHiddenTagsState === 'function') {
                const reverted = shouldHide ? next.filter(id => id !== tagID) : [...next, tagID];
                setHiddenTagsState(reverted);
            }
            if (typeof showToast === 'function') showToast('Failed to update hidden tags: ' + (e?.message || e), 'error');
        }
    }

    async function doStopServing(tagID) {
        try {
            await window.go.main.ServeService.StopServing(tagID);
            if (typeof showToast === 'function') showToast('Stopped serving', 'success');
        } catch (e) {
            const msg = (e && e.message) ? e.message : String(e);
            if (/no server running/i.test(msg)) {
                console.debug('stop-serve race: already stopped');
                return;
            }
            if (typeof showToast === 'function') showToast('Failed to stop serving: ' + msg, 'error');
        }
        if (window.folderStatusPoller) window.folderStatusPoller.evaluate();
    }

    async function doStopSharing(tagID) {
        try {
            await window.go.main.ShareService.StopShare(tagID);
            if (typeof showToast === 'function') showToast('Stopped sharing', 'success');
        } catch (e) {
            if (typeof showToast === 'function') showToast('Failed to stop sharing: ' + (e?.message || e), 'error');
        }
        if (window.folderStatusPoller) window.folderStatusPoller.evaluate();
    }

    function svg(pathD) {
        return `<svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">${pathD}</svg>`;
    }
    function iconOpen()    { return svg('<path stroke-linecap="round" stroke-linejoin="round" d="M13.5 6H5.25A2.25 2.25 0 0 0 3 8.25v10.5A2.25 2.25 0 0 0 5.25 21h10.5A2.25 2.25 0 0 0 18 18.75V10.5m-10.5 6L21 3m0 0h-5.25M21 3v5.25"/>'); }
    function iconMove()    { return svg('<path stroke-linecap="round" stroke-linejoin="round" d="M7.5 7.5h-.75A2.25 2.25 0 0 0 4.5 9.75v7.5a2.25 2.25 0 0 0 2.25 2.25h7.5a2.25 2.25 0 0 0 2.25-2.25v-7.5a2.25 2.25 0 0 0-2.25-2.25h-.75m-6 3.75 3-3 3 3M12 7.5v9"/>'); }
    function iconPencil()  { return svg('<path stroke-linecap="round" stroke-linejoin="round" d="m16.862 4.487 1.687-1.688a1.875 1.875 0 1 1 2.652 2.652L6.832 19.82a4.5 4.5 0 0 1-1.897 1.13l-2.685.8.8-2.685a4.5 4.5 0 0 1 1.13-1.897L16.863 4.487Z"/>'); }
    function iconServe()   { return svg('<circle cx="12" cy="12" r="9"/><path stroke-linecap="round" stroke-linejoin="round" d="M3 12h18M12 3a15 15 0 0 1 0 18M12 3a15 15 0 0 0 0 18"/>'); }
    function iconShare()   { return svg('<path stroke-linecap="round" stroke-linejoin="round" d="M10 13a5 5 0 0 0 7.07 0l3-3a5 5 0 0 0-7.07-7.07L12 4.5M14 11a5 5 0 0 0-7.07 0l-3 3a5 5 0 0 0 7.07 7.07L12 19.5"/>'); }
    function iconEye()     { return svg('<path stroke-linecap="round" stroke-linejoin="round" d="M2.036 12.322a1.012 1.012 0 0 1 0-.639C3.423 7.51 7.36 4.5 12 4.5c4.638 0 8.573 3.007 9.963 7.178.07.207.07.431 0 .639C20.577 16.49 16.64 19.5 12 19.5c-4.638 0-8.573-3.007-9.963-7.178Z"/><path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z"/>'); }
    function iconEyeOff()  { return svg('<path stroke-linecap="round" stroke-linejoin="round" d="M3.98 8.223A10.477 10.477 0 0 0 1.934 12C3.226 16.338 7.244 19.5 12 19.5c.993 0 1.953-.138 2.863-.395M6.228 6.228A10.45 10.45 0 0 1 12 4.5c4.756 0 8.773 3.162 10.065 7.498a10.523 10.523 0 0 1-4.293 5.774M6.228 6.228 3 3m3.228 3.228 3.65 3.65m7.894 7.894L21 21m-3.228-3.228-3.65-3.65m0 0a3 3 0 1 0-4.243-4.243m4.242 4.242L9.88 9.88"/>'); }

    return { attach, openFor, handleAction };
})();

window.FolderContextMenu = FolderContextMenu;
