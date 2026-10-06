// Capability based: the headless adapter and ordinary platform builds do not
// expose Finder controls. No platform sniffing: the status names the location
// ("Finder" on macOS, "file manager" for the Linux mount).
(() => {
    const section = document.getElementById('file-provider-settings');
    const toggle = document.getElementById('file-provider-toggle');
    const reveal = document.getElementById('file-provider-reveal');
    const retry = document.getElementById('file-provider-retry');
    const message = document.getElementById('file-provider-status');
    const title = document.getElementById('file-provider-title');
    const intro = document.getElementById('file-provider-intro');
    const note = document.getElementById('file-provider-note');
    const hidden = document.getElementById('file-provider-hidden');
    const finderText = { title: title.textContent, intro: intro.textContent, note: note.textContent, hidden: hidden.textContent };
    let status = null;
    let busy = false;
    const service = () => window.go?.main?.FileProviderService;

    function render(value) {
        status = value;
        section.classList.toggle('hidden', !value?.supported);
        const location = value?.location || 'Finder';
        const folder = value?.path || 'a folder in your home directory';
        if (location === 'Finder') {
            title.textContent = finderText.title;
            intro.textContent = finderText.intro;
            note.textContent = finderText.note;
            hidden.textContent = finderText.hidden;
        } else {
            title.textContent = 'File manager access';
            intro.textContent = `Browse and copy pastes from Active, Archive and Tags in ${folder} with any file manager, file dialog or terminal. The folder is read-only and only available while Mahpastes is running. Edit pastes in Mahpastes.`;
            note.textContent = 'Disabling unmounts the folder. Files you copied out of it are kept.';
            hidden.textContent = 'Pastes with hidden tags are left out of Active and Archive but still appear in the folders of their other tags. Hidden tags appear under Tags as dot-folders, which most file managers hide. Copies saved elsewhere may remain after a paste is hidden or expires.';
        }
        const where = location === 'Finder' ? 'Finder' : 'file manager';
        toggle.textContent = value?.enabled ? `Disable ${where} access` : `Enable ${where} access`;
        reveal.textContent = `Open in ${where}`;
        reveal.classList.toggle('hidden', !value?.running);
        retry.classList.toggle('hidden', !value?.enabled || value?.running);
        const available = location === 'Finder' ? 'Available in Finder.' : `Available at ${folder}.`;
        const off = location === 'Finder' ? 'Finder access is off.' : 'File manager access is off.';
        message.textContent = value?.message || (value?.running ? available : off);
        if (value?.recoveryPath) message.textContent += ` Downloaded files were kept at ${value.recoveryPath}`;
    }

    window.loadFileProviderSettings = async () => {
        if (!service()?.Status) { render(null); return; }
        try { render(await service().Status()); }
        catch { render(null); }
    };

    async function run(action) {
        if (busy) return;
        busy = true; toggle.disabled = true; reveal.disabled = true; retry.disabled = true;
        try { await action(); await window.loadFileProviderSettings(); }
        catch (error) {
            await window.loadFileProviderSettings();
            message.textContent = String(error?.message || error);
        } finally { busy = false; toggle.disabled = false; reveal.disabled = false; retry.disabled = false; }
    }
    toggle.addEventListener('click', () => run(() => status?.enabled ? service().Disable() : service().Enable()));
    reveal.addEventListener('click', () => run(() => service().Reveal()));
    retry.addEventListener('click', () => run(() => service().Enable()));
})();
