// Capability based: the headless adapter and ordinary platform builds do not
// expose Finder controls. No platform sniffing or native details in user flows.
(() => {
    const section = document.getElementById('file-provider-settings');
    const toggle = document.getElementById('file-provider-toggle');
    const reveal = document.getElementById('file-provider-reveal');
    const retry = document.getElementById('file-provider-retry');
    const message = document.getElementById('file-provider-status');
    let status = null;
    let busy = false;
    const service = () => window.go?.main?.FileProviderService;

    function render(value) {
        status = value;
        section.classList.toggle('hidden', !value?.supported);
        toggle.textContent = value?.enabled ? 'Disable Finder access' : 'Enable Finder access';
        reveal.classList.toggle('hidden', !value?.running);
        retry.classList.toggle('hidden', !value?.enabled || value?.running);
        message.textContent = value?.message || (value?.running ? 'Available in Finder.' : 'Finder access is off.');
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
