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
        const banner = document.createElement('div');
        banner.id = 'update-banner';
        banner.className = 'update-banner';
        banner.setAttribute('role', 'status');
        banner.setAttribute('data-testid', 'update-banner');

        const text = document.createElement('span');
        text.textContent = `mahpastes ${update.version} is available. `;
        const link = document.createElement('a');
        link.textContent = 'View release';
        link.setAttribute('data-testid', 'update-banner-link');
        link.addEventListener('click', () => openExternal(update.url));
        text.appendChild(link);

        const close = document.createElement('button');
        close.type = 'button';
        close.textContent = '×';
        close.setAttribute('aria-label', 'Dismiss');
        close.setAttribute('data-testid', 'update-banner-dismiss');
        close.addEventListener('click', () => {
            rememberDismissed(update.version);
            removeBanner();
        });

        banner.append(text, close);
        document.body.appendChild(banner);
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
        if (info?.update) showBanner(info.update);
        if (toggle) {
            toggle.checked = info ? !!info.update_check_enabled : true;
            toggle.addEventListener('change', async () => {
                const want = toggle.checked;
                try {
                    await svc.SetUpdateCheckEnabled(want);
                    if (!want) removeBanner();
                } catch (e) {
                    toggle.checked = !want;
                    if (typeof showToast === 'function') showToast('Failed to save setting: ' + errText(e), 'error');
                }
            });
        }
    }

    // Menu item id -> ShortcutManager action id. Items run through
    // ShortcutManager.runAction, which checks that the action's context is
    // live, so a menu click never acts behind a modal.
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

    function onMenuAction(id) {
        if (id === 'documentation') {
            openExternal(DOCS_URL);
            return;
        }
        const actionId = MENU_ACTIONS[id];
        if (actionId && typeof ShortcutManager !== 'undefined') ShortcutManager.runAction(actionId);
    }
    window.__handleMenuAction = onMenuAction;

    if (window.runtime?.EventsOn) {
        window.runtime.EventsOn('app:update_available', showBanner);
        window.runtime.EventsOn('menu:action', onMenuAction);
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
})();
