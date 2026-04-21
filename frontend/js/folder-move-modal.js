// Stub — full implementation in Phase 5.
const FolderMoveModal = (() => {
    function show(tag) {
        if (typeof showToast === 'function') {
            showToast('Move modal coming soon', 'info');
        }
        return Promise.resolve(null);
    }
    return { show };
})();
window.FolderMoveModal = FolderMoveModal;
