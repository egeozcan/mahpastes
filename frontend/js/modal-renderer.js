// --- Plugin Result Modal Renderer ---

// State
let currentModalData = null;
let modalRenderGen = 0;

// Elements
const pluginResultModal = document.getElementById('plugin-result-modal');
const pluginResultTitle = document.getElementById('plugin-result-title');
const pluginResultBody = document.getElementById('plugin-result-body');
const pluginResultClose = document.getElementById('plugin-result-close');
const pluginResultCopy = document.getElementById('plugin-result-copy');
const pluginResultPaste = document.getElementById('plugin-result-paste');

async function renderPluginContent(content, format, pluginId, renderGen) {
    switch (format) {
        case 'markdown': {
            const container = MarkdownRenderer.render(content);
            container.querySelectorAll('a[href]').forEach((link) => {
                link.setAttribute('target', '_blank');
                link.setAttribute('rel', 'noopener noreferrer');
            });
            const markdownImages = container.markdownImages || [];
            markdownImages.slice(256).forEach(descriptor => {
                descriptor.placeholder.querySelector('.markdown-image-label').textContent = 'Plugin image limit exceeded';
            });
            for (const descriptor of markdownImages.slice(0, 256)) {
                const src = descriptor.source;
                let allowed = src.startsWith('data:');
                if (src.startsWith('https://')) {
                    allowed = pluginId
                        ? await window.go.main.PluginService.IsPluginURLAllowed(pluginId, src, 'GET')
                        : false;
                    if (renderGen !== modalRenderGen) return null;
                }
                if (!allowed) {
                    descriptor.placeholder.querySelector('.markdown-image-label').textContent =
                        'Image blocked — domain not in plugin allowlist';
                    continue;
                }
                if (renderGen !== modalRenderGen) return null;
                const img = document.createElement('img');
                img.src = src;
                img.alt = descriptor.alt || 'Plugin result';
                if (descriptor.title) img.title = descriptor.title;
                descriptor.placeholder.replaceWith(img);
            }
            return container;
        }
        case 'image': {
            if (content.startsWith('data:')) {
                // data: URIs always allowed
            } else if (content.startsWith('https://')) {
                // Fail-closed: block if plugin_id is missing
                const allowed = pluginId
                    ? await window.go.main.PluginService.IsPluginURLAllowed(pluginId, content, 'GET')
                    : false;
                if (renderGen !== modalRenderGen || !allowed) return null;
            } else {
                return null;
            }
            if (renderGen !== modalRenderGen) return null;
            const container = document.createElement('div');
            container.className = 'plugin-image-content';
            const img = document.createElement('img');
            img.src = content;
            img.alt = 'Plugin result';
            container.appendChild(img);
            return container;
        }
        case 'text': {
            const pre = document.createElement('pre');
            pre.className = 'plugin-text-content';
            pre.textContent = content;
            return pre;
        }
        default:
            return null;
    }
}

async function showPluginResultModal(data) {
    if (!data || !data.content || !data.format) return;

    const gen = ++modalRenderGen;
    currentModalData = data;

    // Acknowledge receipt so the Go-side guard ack-timeout knows the
    // frontend is alive. Emitted early (before async rendering) since
    // the ack confirms "request received", not "render complete".
    window.runtime.EventsEmit('plugin:modal:acked');

    pluginResultTitle.textContent = data.title || 'Result';

    pluginResultBody.innerHTML = '';
    const rendered = await renderPluginContent(data.content, data.format, data.plugin_id, gen);

    // Drop stale render if a newer modal request arrived during async rendering
    if (gen !== modalRenderGen) return;

    if (rendered) {
        pluginResultBody.appendChild(rendered);
    }

    pluginResultModal.removeAttribute('inert');
    pluginResultModal.classList.remove('opacity-0', 'pointer-events-none');
    pluginResultModal.classList.add('opacity-100');
}

function closePluginResultModal() {
    modalRenderGen++;
    pluginResultModal.classList.remove('opacity-100');
    pluginResultModal.classList.add('opacity-0', 'pointer-events-none');
    pluginResultModal.setAttribute('inert', '');
    currentModalData = null;
    // Notify Go side so modal.show() lock is released
    window.runtime.EventsEmit('plugin:modal:closed');
    // Refresh gallery — the action that produced this modal may have had
    // side effects (e.g. tag mutations) that the UI doesn't know about.
    if (typeof loadClips === 'function') {
        loadClips();
    }
}

async function pluginResultCopyToClipboard() {
    if (!currentModalData) return;

    const text = currentModalData.copy_data || currentModalData.content;
    try {
        await window.go.main.App.CopyToClipboard(text);
        showToast('Copied to clipboard');
    } catch (err) {
        console.error('Failed to copy to clipboard:', err);
        showToast('Failed to copy', 'error');
    }
}

async function pluginResultAddAsPaste() {
    if (!currentModalData) return;

    const data = currentModalData.paste_data || currentModalData.content;
    const format = currentModalData.format;

    // For image format without pre-encoded base64, only data: URIs and
    // https: URLs are valid sources. Block paste for anything else
    // (http://, bare strings, etc.) and for remote URLs when no explicit
    // paste_data is provided. Skip this guard when paste_data_base64 is
    // set — the data is already raw base64 bytes, not a URL.
    if (format === 'image' && !currentModalData.paste_data_base64) {
        if (!data.startsWith('https://') && !data.startsWith('data:')) {
            showToast('Cannot save image — unsupported source', 'error');
            return;
        }
        // URL paste_data can't be binary-encoded — reject regardless of
        // whether it came from content or an explicit paste_data field.
        if (data.startsWith('https://')) {
            showToast('Cannot save remote image — plugin should provide paste_data as base64', 'error');
            return;
        }
    }

    let name = currentModalData.paste_name;
    let contentType = currentModalData.paste_content_type;

    if (!name) {
        switch (format) {
            case 'markdown': name = 'plugin-result.md'; break;
            case 'image': name = 'plugin-result.png'; break;
            case 'text': name = 'plugin-result.txt'; break;
            default: name = 'plugin-result.txt';
        }
    }

    try {
        let encoded;
        if (currentModalData.paste_data_base64) {
            // Plugin already provided base64-encoded paste_data — use as-is
            encoded = data;
        } else {
            // Data URIs already contain base64 — extract payload and MIME directly.
            // Handle optional parameters (e.g. data:image/svg+xml;charset=utf-8;base64,...)
            const dataUriMatch = data.match(/^data:([^;,]+)(?:;[^;,]*)*;base64,(.+)$/);
            if (dataUriMatch) {
                if (!contentType) contentType = dataUriMatch[1];
                encoded = dataUriMatch[2];
            } else if (format === 'image' && data.startsWith('data:')) {
                // Non-base64 data URIs (e.g. data:image/svg+xml,<svg...>) can't
                // be reliably decoded here — reject rather than corrupt the clip.
                showToast('Cannot save image — non-base64 data URI not supported', 'error');
                return;
            } else {
                encoded = btoa(unescape(encodeURIComponent(data)));
            }
        }

        // Apply format-based defaults only after data URI extraction
        if (!contentType) {
            switch (format) {
                case 'markdown': contentType = 'text/markdown'; break;
                case 'image': contentType = 'image/png'; break;
                case 'text': contentType = 'text/plain'; break;
                default: contentType = 'text/plain';
            }
        }
        await window.go.main.App.UploadFileAndGetID({
            name: name,
            content_type: contentType,
            data: encoded,
        });
        showToast('Added as paste');
        closePluginResultModal();
    } catch (err) {
        console.error('Failed to add as paste:', err);
        showToast('Failed to add paste', 'error');
    }
}

// Event Listeners
pluginResultClose.addEventListener('click', closePluginResultModal);
pluginResultCopy.addEventListener('click', pluginResultCopyToClipboard);
pluginResultPaste.addEventListener('click', pluginResultAddAsPaste);

pluginResultModal.addEventListener('click', (e) => {
    if (e.target === pluginResultModal) closePluginResultModal();
});

// Listen for plugin:modal Wails event (from modal.show() API and async action results)
window.runtime.EventsOn('plugin:modal', (data) => {
    showPluginResultModal(data);
});
