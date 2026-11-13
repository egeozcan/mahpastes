// App version, the opt-out "new release available" notice, and the macOS
// menu bar bridge. The desktop app emits "app:update_available" (see
// app_info_service.go) and "menu:action" (see app_menu.go).
(() => {
    const service = () => window.go?.main?.AppInfoService;
    const DISMISSED_KEY = 'mahpastes.dismissedUpdate';
    const DOCS_URL = 'https://egeozcan.github.io/mahpastes/';

    function errText(e) {
        return (e && (e.message || e.toString())) || 'unknown error';
    }

    function dismissedVersion() {
        try { return localStorage.getItem(DISMISSED_KEY) || ''; } catch (e) { return ''; }
    }
    function rememberDismissed(version) {
        try { localStorage.setItem(DISMISSED_KEY, version); } catch (e) { /* not persisted */ }
    }

    // Only http(s) links to GitHub are ever opened from the notice.
    function openExternal(url) {
        if (!/^https:\/\/(github\.com|egeozcan\.github\.io)\//.test(url)) return;
        if (window.runtime?.BrowserOpenURL) window.runtime.BrowserOpenURL(url);
        else window.open(url, '_blank', 'noopener');
    }

    function removeBanner() {
        document.getElementById('update-banner')?.remove();
    }

    function showBanner(update) {
        if (!update || !update.version || dismissedVersion() === update.version) return;
        removeBanner();
        // Sits above the bottom bar and the toast (bottom-16), so it covers
        // neither the clip count nor a toast.
        const banner = document.createElement('div');
        banner.id = 'update-banner';
        banner.className = 'fixed bottom-28 right-4 z-50 max-w-[calc(100vw-2rem)] flex items-center gap-3 ' +
            'bg-white border border-stone-200 rounded-md shadow-lg pl-4 pr-2 py-2.5 text-xs font-medium text-stone-600';
        banner.setAttribute('role', 'status');
        banner.setAttribute('data-testid', 'update-banner');

        const text = document.createElement('span');
        text.textContent = `mahpastes ${update.version} is available.`;

        const link = document.createElement('button');
        link.type = 'button';
        link.className = 'shrink-0 font-semibold text-stone-800 underline underline-offset-2 hover:text-stone-600 transition-colors';
        link.textContent = 'View release';
        link.setAttribute('data-testid', 'update-banner-link');
        link.addEventListener('click', () => openExternal(update.url));

        const close = document.createElement('button');
        close.type = 'button';
        close.className = 'shrink-0 p-1 text-stone-400 hover:text-stone-600 hover:bg-stone-100 rounded transition-colors';
        close.setAttribute('aria-label', 'Dismiss update notice');
        close.setAttribute('data-testid', 'update-banner-dismiss');
        close.innerHTML = '<svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">' +
            '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M6 18L18 6M6 6l12 12"/></svg>';
        close.addEventListener('click', () => {
            rememberDismissed(update.version);
            removeBanner();
        });

        banner.append(text, link, close);
        document.body.appendChild(banner);
    }

    // The release the check found, in Settings > About. The banner can be
    // dismissed for good; this line is where the update can still be found
    // afterwards. Text only (textContent), like the banner.
    let aboutUpdate = null;
    function renderAboutUpdate(update) {
        aboutUpdate = update && update.version ? update : null;
        const row = document.getElementById('about-update');
        const text = document.getElementById('about-update-text');
        if (!row || !text) return;
        row.hidden = !aboutUpdate;
        text.textContent = aboutUpdate ? `Version ${aboutUpdate.version} is available. ` : '';
    }
    document.getElementById('about-update-link')?.addEventListener('click', () => {
        if (aboutUpdate) openExternal(aboutUpdate.url);
    });

    function onUpdateAvailable(update) {
        renderAboutUpdate(update);
        showBanner(update);
    }

    async function init() {
        const svc = service();
        const versionEl = document.getElementById('about-version');
        const toggle = document.getElementById('update-check-toggle');
        if (!svc?.GetAppInfo) return;
        let info = null;
        try {
            info = await svc.GetAppInfo();
        } catch (e) {
            console.error('Failed to load app info:', e);
        }
        if (versionEl) {
            versionEl.textContent = info?.version ? `Version ${info.version}` : 'Version unknown';
        }
        if (info?.update) onUpdateAvailable(info.update);
        if (toggle) {
            toggle.checked = info ? !!info.update_check_enabled : true;
            toggle.addEventListener('change', async () => {
                const want = toggle.checked;
                try {
                    await svc.SetUpdateCheckEnabled(want);
                    if (!want) { removeBanner(); renderAboutUpdate(null); }
                } catch (e) {
                    toggle.checked = !want;
                    if (typeof showToast === 'function') showToast('Failed to save setting: ' + errText(e), 'error');
                }
            });
        }
    }

    // Menu item id -> ShortcutManager action id. Items run through
    // ShortcutManager.runAction, so a menu click never acts behind a dialog,
    // the lightbox, an editor, the comparison view or the import wizard. With
    // menuBar set, the gallery actions also run from the Watch, Serve and
    // Share views and with the nav drawer open (it is closed first): a menu
    // bar item that is enabled must not silently do nothing there.
    const MENU_ACTIONS = {
        'import-files': 'upload-clip',
        'import-folder': 'open-import',
        'settings': 'open-settings',
        'archive': 'toggle-archive',
        'folder-mode': 'toggle-folder',
        'watch': 'open-watch',
        'serve': 'open-serve',
        'plugins': 'open-plugins',
        'shortcuts': 'show-cheatsheet',
    };

    // What a gallery action needs done first when it is picked from another
    // main view; returning false skips the action itself. Watch Folders and
    // Serve switch views themselves and Plugins is a dialog that opens over
    // any view, so they need nothing.
    const showGallery = () => { if (typeof switchView === 'function') switchView('clips'); };
    const ENTER_GALLERY = {
        'import-files': showGallery,
        'import-folder': showGallery,
        // From another view these two go to the archive / folder view; a
        // plain toggle would leave it if it was already on. toggleViewMode
        // brings the gallery up itself.
        'archive': () => { if (!isViewingArchive) return true; showGallery(); return false; },
        'folder-mode': () => { showGallery(); return !folderMode; },
    };

    function onMenuAction(id) {
        if (id === 'documentation') {
            openExternal(DOCS_URL);
            return;
        }
        const actionId = MENU_ACTIONS[id];
        if (actionId && typeof ShortcutManager !== 'undefined') {
            ShortcutManager.runAction(actionId, { menuBar: true, enterGallery: ENTER_GALLERY[id] });
        }
    }
    window.__handleMenuAction = onMenuAction;

    // The same two Help entries in the nav drawer: Windows and Linux have no
    // menu bar, and on macOS nothing in the window pointed at either. They go
    // through the menu path, which closes the drawer before acting.
    document.getElementById('open-shortcuts-btn')?.addEventListener('click', () => onMenuAction('shortcuts'));
    document.getElementById('open-docs-btn')?.addEventListener('click', () => {
        if (typeof closeDrawer === 'function') closeDrawer();
        onMenuAction('documentation');
    });

    if (window.runtime?.EventsOn) {
        window.runtime.EventsOn('app:update_available', onUpdateAvailable);
        window.runtime.EventsOn('menu:action', onMenuAction);
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
})();
