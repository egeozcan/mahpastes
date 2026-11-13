// System notifications. The backend shows a native notification for
// background events (imports, received shared clips, plugin results) only
// while the window is unfocused, so this module reports focus changes. It
// also drives the "System Notifications" section in Settings.
(() => {
    const service = () => window.go?.main?.NotificationService;

    // --- Focus reporting ---
    let reportedFocus = null;
    // Each bound call runs on its own goroutine, so two reports can reach Go
    // swapped. The backend orders them by this counter within one page load.
    let focusSeq = 0;
    const focusSession = Math.random().toString(36).slice(2) + Date.now().toString(36);
    function reportFocus() {
        const focused = document.hasFocus() && !document.hidden;
        if (focused === reportedFocus) return;
        // A bound call, not EventsEmit: under `wails dev` Wails forwards a
        // browser page's event to the native window, and crashes if that
        // window does not exist yet.
        const report = service()?.ReportWindowFocus;
        // No backend (yet): it treats the window as focused. Leave
        // reportedFocus alone so the next focus event reports again.
        if (typeof report !== 'function') return;
        try {
            Promise.resolve(report(focused, ++focusSeq, focusSession)).catch(() => {});
            reportedFocus = focused;
        } catch (e) {
            // Same: retried on the next focus event.
        }
    }
    window.addEventListener('focus', reportFocus);
    window.addEventListener('blur', reportFocus);
    document.addEventListener('visibilitychange', reportFocus);
    // A focus check at load can be stale in some webviews, and no focus event
    // follows when the window already had it; any input proves focus.
    window.addEventListener('pointerdown', reportFocus, true);
    window.addEventListener('keydown', reportFocus, true);
    window.addEventListener('load', reportFocus);

    // --- Settings section ---
    const toggle = document.getElementById('os-notifications-toggle');
    const message = document.getElementById('os-notifications-status');
    const testButton = document.getElementById('os-notifications-test');
    const platformNote = document.getElementById('os-notifications-platform-note');
    if (!toggle || !message || !testButton || !platformNote) return;

    let status = null;
    let busy = false;
    // Bumped by every user action, so a status fetch that started earlier
    // cannot overwrite what the user just chose.
    let generation = 0;

    const platformNotes = {
        darwin: 'macOS asks for permission the first time. Notifications need the mahpastes app bundle; development runs (wails dev) cannot show them.',
        windows: 'Turning this on registers mahpastes for notifications in your Windows user registry (HKCU). Focus assist can hide them.',
        linux: 'Uses your desktop’s notification service. Do Not Disturb modes can hide them.',
    };

    function describe(value) {
        if (!value) return { text: '', error: false };
        if (busy) {
            return {
                text: value.platform === 'darwin'
                    ? 'Waiting for macOS… answer the permission prompt if one appeared.'
                    : 'Waiting for the system…',
                error: false,
            };
        }
        if (!value.supported) {
            return { text: value.error || 'System notifications are not available.', error: false };
        }
        if (!value.enabled) return { text: 'Off.', error: false };
        switch (value.state) {
            case 'failed':
                return { text: `Not available: ${value.error || 'the system refused.'}`, error: true };
            case 'pending':
                return { text: 'Waiting for the system…', error: false };
            case 'ready':
                return { text: 'On. Notifications appear while mahpastes is in the background.', error: false };
            default:
                return { text: 'On.', error: false };
        }
    }

    function render() {
        toggle.checked = !!status?.enabled;
        toggle.disabled = busy;
        const { text, error } = describe(status);
        message.textContent = text;
        message.classList.toggle('text-red-500', error);
        message.classList.toggle('text-stone-500', !error);
        const canTest = !!status?.supported && !!status?.enabled && status?.state !== 'failed';
        testButton.classList.toggle('hidden', !canTest);
        testButton.disabled = busy;
        platformNote.textContent = platformNotes[status?.platform] || '';
    }

    async function refresh() {
        const svc = service();
        if (!svc?.GetNotificationStatus) return;
        const gen = generation;
        try {
            const value = await svc.GetNotificationStatus();
            if (gen !== generation || busy) return;
            status = value;
            render();
        } catch (e) {
            console.error('Failed to load notification status:', e);
        }
    }

    // Settings opens with the value fetched at startup, then refreshes.
    window.loadOSNotificationSettings = () => {
        render();
        refresh();
    };

    toggle.addEventListener('change', async () => {
        const svc = service();
        if (!svc?.SetNotificationsEnabled || busy) return;
        const want = toggle.checked;
        generation++;
        busy = true;
        status = { ...(status || {}), enabled: want };
        render();
        try {
            status = await svc.SetNotificationsEnabled(want);
        } catch (e) {
            console.error('Failed to save notification setting:', e);
            showToast('Failed to save notification setting: ' + errText(e), 'error');
            status = { ...(status || {}), enabled: !want };
        } finally {
            busy = false;
            render();
        }
    });

    testButton.addEventListener('click', async () => {
        const svc = service();
        if (!svc?.SendTestNotification || busy) return;
        generation++;
        busy = true;
        render();
        try {
            status = await svc.SendTestNotification();
            showToast('Test notification sent');
        } catch (e) {
            showToast('Could not show a notification: ' + errText(e), 'error');
            busy = false;
            await refresh();
        } finally {
            busy = false;
            render();
        }
    });

    window.addEventListener('load', refresh);
})();
