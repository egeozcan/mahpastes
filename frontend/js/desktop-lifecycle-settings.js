// Background & Startup settings, and the drawer's Quit entry. Capability
// based like file-provider-settings.js: the headless adapter has none of these
// bindings, so the section and the Quit entry stay hidden in server mode.
(() => {
    const section = document.getElementById('desktop-lifecycle-settings');
    const keepToggle = document.getElementById('keep-running-toggle');
    const keepNote = document.getElementById('keep-running-note');
    const keepRestart = document.getElementById('keep-running-restart');
    const loginBlock = document.getElementById('launch-at-login-block');
    const loginToggle = document.getElementById('launch-at-login-toggle');
    const hiddenLabel = document.getElementById('launch-hidden-label');
    const hiddenToggle = document.getElementById('launch-hidden-toggle');
    const hiddenNote = document.getElementById('launch-hidden-note');
    const loginStatus = document.getElementById('launch-at-login-status');
    const drawerQuit = document.getElementById('drawer-quit');
    const quitBtn = document.getElementById('quit-app-btn');

    const app = () => window.go?.main?.App;
    const supported = () => typeof app()?.GetDesktopLifecycle === 'function';
    const canQuit = () => typeof app()?.QuitApp === 'function';

    let state = null;
    let busy = false;

    // The Quit shortcut as currently bound ('' when unbound or not registered).
    const quitKey = () => {
        if (typeof ShortcutManager === 'undefined' || !ShortcutManager.actions.has('quit-app')) return '';
        const combo = ShortcutManager.getEffectiveCombo('quit-app');
        return combo ? ShortcutManager.comboToDisplay(combo) : '';
    };

    function keepRunningText(platform) {
        if (platform === 'darwin') {
            return 'Closing the window hides mahpastes. Click its Dock icon to show it again. Quit with ⌘Q, the Dock menu or Quit in the menu.';
        }
        const key = quitKey();
        const quitHow = key ? `${key} or Quit in the menu` : 'Quit in the menu';
        return `Closing the window hides it and mahpastes keeps running, without a tray icon. Open mahpastes again from your app launcher to show the window. Quit with ${quitHow}.`;
    }

    function hiddenText(platform) {
        return platform === 'darwin'
            ? 'At login mahpastes starts hidden. Click its Dock icon to show it.'
            : 'At login mahpastes starts without a window. Open it from your app launcher to show the window.';
    }

    function statusText(login, error) {
        if (error) return `Could not read the login entry: ${error}`;
        if (!login) return '';
        if (login.disabled_by_system) {
            return 'An entry exists but is switched off in your system’s startup settings. Turning it on here enables it again.';
        }
        if (!login.enabled) return '';
        if (login.target_missing) {
            return login.target
                ? `The login entry starts ${login.target}, which no longer exists. Turn the setting off and on again to use this copy.`
                : 'The login entry cannot be read. Turn the setting off and on again to rewrite it.';
        }
        if (login.other_copy) {
            return `The login entry starts another copy of mahpastes: ${login.target}. Turn the setting off and on again to use this copy.`;
        }
        return '';
    }

    function render(value) {
        state = value;
        section.classList.toggle('hidden', !value);
        if (!value) return;
        keepToggle.checked = !!value.keep_running_when_closed;
        keepNote.textContent = keepRunningText(value.platform);
        keepRestart.classList.toggle('hidden', !!value.keep_running_when_closed === !!value.keep_running_active);

        const login = value.launch_at_login || {};
        loginBlock.classList.toggle('hidden', !login.supported && !value.launch_at_login_error);
        loginToggle.checked = !!login.enabled;
        // The hidden choice is read back from the entry; with no entry the
        // toggle keeps whatever the user last chose here (on by default).
        if (login.enabled) hiddenToggle.checked = !!login.hidden;
        hiddenToggle.disabled = !login.enabled;
        hiddenLabel.classList.toggle('opacity-50', !login.enabled);
        hiddenNote.textContent = hiddenText(value.platform);
        hiddenNote.classList.toggle('hidden', !login.enabled || !hiddenToggle.checked);
        const status = statusText(login, value.launch_at_login_error);
        loginStatus.textContent = status;
        loginStatus.classList.toggle('hidden', !status);
    }

    window.loadDesktopLifecycleSettings = async () => {
        if (!supported()) { render(null); return; }
        try { render(await app().GetDesktopLifecycle()); }
        catch (error) {
            console.error('Failed to load background and startup settings:', error);
            render(null);
        }
    };

    async function run(action, failureMessage) {
        if (busy) return;
        busy = true;
        keepToggle.disabled = true; loginToggle.disabled = true; hiddenToggle.disabled = true;
        try {
            await action();
        } catch (error) {
            showToast(`${failureMessage}: ${errText(error)}`, 'error');
        } finally {
            busy = false;
            keepToggle.disabled = false; loginToggle.disabled = false;
            await window.loadDesktopLifecycleSettings();
        }
    }

    keepToggle.addEventListener('change', () => {
        const keep = keepToggle.checked;
        run(() => app().SetKeepRunningWhenClosed(keep), 'Could not save the setting');
    });
    loginToggle.addEventListener('change', () => {
        const enabled = loginToggle.checked;
        run(() => app().SetLaunchAtLogin(enabled, hiddenToggle.checked), enabled
            ? 'Could not turn on launch at login'
            : 'Could not turn off launch at login');
    });
    hiddenToggle.addEventListener('change', () => {
        if (!state?.launch_at_login?.enabled) return;
        const hidden = hiddenToggle.checked;
        run(() => app().SetLaunchAtLogin(true, hidden), 'Could not update launch at login');
    });

    // Quit: the same path as the window's close button with keep running off.
    // Unsaved editor changes are asked about natively before anything closes.
    window.quitApp = () => {
        if (!canQuit()) return;
        Promise.resolve(app().QuitApp()).catch(error => {
            showToast(`Could not quit: ${errText(error)}`, 'error');
        });
    };
    // The bindings are in place by the time the app has started; check then.
    const showQuit = () => drawerQuit.classList.toggle('hidden', !canQuit());
    showQuit();
    document.addEventListener('DOMContentLoaded', showQuit);
    quitBtn.addEventListener('click', () => {
        if (typeof closeDrawer === 'function') closeDrawer();
        window.quitApp();
    });
})();
