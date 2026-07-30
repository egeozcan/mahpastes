// Markdown clip preview policies: safe links, clip-relative references, and images.
const MarkdownPreview = (() => {
    const MAX_IMAGE_BYTES = 15 * 1024 * 1024;
    const MAX_PREVIEW_IMAGE_BYTES = 100 * 1024 * 1024;
    const MAX_PREVIEW_IMAGES = 256;
    const MAX_LOCAL_REFERENCES = 128;
    let sourceClipID = null;
    let generation = 0;
    let loadedImageBytes = 0;
    let loadedImageDecodedBytes = 0;
    let reservedImageBytes = 0;
    const activeDownloads = new Map();

    function service() {
        return window.go?.main?.MarkdownService || null;
    }

    function open(clipID) {
        sourceClipID = clipID;
        generation++;
        loadedImageBytes = 0;
        loadedImageDecodedBytes = 0;
        reservedImageBytes = 0;
    }

    function beginRender() {
        generation++;
        loadedImageBytes = 0;
        loadedImageDecodedBytes = 0;
        reservedImageBytes = 0;
        for (const requestID of activeDownloads.keys()) {
            service()?.CancelRemoteImage(requestID).catch(() => {});
        }
        activeDownloads.clear();
    }

    function close() {
        generation++;
        sourceClipID = null;
        for (const requestID of activeDownloads.keys()) {
            service()?.CancelRemoteImage(requestID).catch(() => {});
        }
        activeDownloads.clear();
        loadedImageBytes = 0;
        loadedImageDecodedBytes = 0;
        reservedImageBytes = 0;
    }

    function openExternal(rawURL) {
        if (window.runtime?.BrowserOpenURL) {
            window.runtime.BrowserOpenURL(rawURL);
        }
    }

    function externalLink(rawURL, text) {
        const link = document.createElement('a');
        link.href = rawURL;
        link.textContent = text || rawURL;
        link.addEventListener('click', event => {
            event.preventDefault();
            openExternal(rawURL);
        });
        return link;
    }

    function isExternalScheme(href) {
        return /^[a-z][a-z0-9+.-]*:/i.test(href);
    }

    function decorateStaticLink(link) {
        const href = link.getAttribute('href') || '';
        if (href.startsWith('#')) {
            link.addEventListener('click', event => {
                event.preventDefault();
                let headingID = href.slice(1);
                try { headingID = decodeURIComponent(headingID); } catch (_) { /* keep raw fragment */ }
                const target = link.closest('.markdown-content')?.querySelector(`#${CSS.escape(headingID)}`);
                target?.scrollIntoView({ block: 'start' });
            });
            return 'handled';
        }
        if (!isExternalScheme(href)) return 'relative';

        let parsed;
        try {
            parsed = new URL(href);
        } catch (_) {
            parsed = null;
        }
        if (parsed && ['https:', 'http:', 'mailto:'].includes(parsed.protocol)) {
            link.addEventListener('click', event => {
                event.preventDefault();
                openExternal(href);
            });
        } else {
            link.removeAttribute('href');
            link.setAttribute('aria-disabled', 'true');
            link.title = 'Blocked unsafe link';
        }
        return 'handled';
    }

    function makeReferenceAction(link, action) {
        link.setAttribute('role', 'button');
        link.tabIndex = 0;
        link.removeAttribute('aria-disabled');
        link.addEventListener('click', event => {
            event.preventDefault();
            action();
        });
        link.addEventListener('keydown', event => {
            if (event.key !== 'Enter' && event.key !== ' ') return;
            event.preventDefault();
            action();
        });
    }

    function applyReferenceResult(link, result) {
        link.dataset.markdownReferenceStatus = result.status;
        if (result.status === 'unique') {
            const candidate = result.candidates[0];
            makeReferenceAction(link, () => {
                if (typeof openMarkdownReferenceCandidate === 'function') {
                    openMarkdownReferenceCandidate(candidate, result.fragment || '');
                }
            });
            return;
        }
        if (result.status === 'ambiguous') {
            makeReferenceAction(link, () => {
                showCandidateChooser(link, result.candidates, result.fragment || '', false);
            });
            return;
        }
        link.removeAttribute('href');
        link.setAttribute('aria-disabled', 'true');
        link.title = result.status === 'invalid' ? result.error : 'Local clip unavailable';
    }

    function showCandidateChooser(anchor, candidates, fragment, imageMode) {
        const existing = anchor.parentElement?.querySelector(':scope > .markdown-reference-chooser');
        if (existing) {
            existing.remove();
            return;
        }
        const chooser = document.createElement('span');
        chooser.className = 'markdown-reference-chooser';
        chooser.setAttribute('role', 'group');
        chooser.setAttribute('aria-label', 'Choose matching clip');
        candidates.forEach(candidate => {
            const button = document.createElement('button');
            button.type = 'button';
            const paths = (candidate.matched_tag_paths || []).map(path => path || 'Root').join(', ');
            button.textContent = `${candidate.filename} · ${paths}`;
            button.addEventListener('click', async () => {
                if (imageMode) {
                    await loadLocalImage(anchor, candidate.clip_id);
                } else if (typeof openMarkdownReferenceCandidate === 'function') {
                    openMarkdownReferenceCandidate(candidate, fragment);
                }
                chooser.remove();
            });
            chooser.appendChild(button);
        });
        anchor.insertAdjacentElement('afterend', chooser);
    }

    function resetImagePlaceholder(descriptor) {
        const placeholder = descriptor.placeholder;
        placeholder.replaceChildren();
        const label = document.createElement('span');
        label.className = 'markdown-image-label';
        label.textContent = descriptor.alt || 'Markdown image';
        placeholder.appendChild(label);
        return placeholder;
    }

    function addURLControls(descriptor, secure) {
        const placeholder = resetImagePlaceholder(descriptor);
        placeholder.appendChild(externalLink(descriptor.source, descriptor.source));
        if (secure) {
            const button = document.createElement('button');
            button.type = 'button';
            button.textContent = 'Load Image';
            button.addEventListener('click', () => loadRemoteImage(descriptor));
            placeholder.appendChild(button);
        } else {
            const note = document.createElement('span');
            note.className = 'markdown-image-note';
            note.textContent = 'Insecure image cannot be loaded inline';
            placeholder.appendChild(note);
        }
    }

    function reserveImageBudget(placeholder, gen) {
        if (gen !== generation || loadedImageBytes + reservedImageBytes + MAX_IMAGE_BYTES > MAX_PREVIEW_IMAGE_BYTES) {
            const note = document.createElement('span');
            note.className = 'markdown-image-note';
            note.textContent = 'Preview image budget exceeded';
            placeholder.appendChild(note);
            return false;
        }
        reservedImageBytes += MAX_IMAGE_BYTES;
        return true;
    }

    function releaseImageBudget(gen) {
        if (gen === generation) {
            reservedImageBytes = Math.max(0, reservedImageBytes - MAX_IMAGE_BYTES);
        }
    }

    function displayImage(placeholder, result, alt, title) {
        const size = Number(result.size || 0);
        const decodedSize = Number(result.decoded_size || 0) ||
            (Number(result.width || 0) * Number(result.height || 0) * 4);
        if (size <= 0 || size > MAX_IMAGE_BYTES || loadedImageBytes + size > MAX_PREVIEW_IMAGE_BYTES ||
            decodedSize <= 0 || loadedImageDecodedBytes + decodedSize > MAX_PREVIEW_IMAGE_BYTES) {
            const note = document.createElement('span');
            note.className = 'markdown-image-note';
            note.textContent = 'Preview image budget exceeded';
            placeholder.appendChild(note);
            return false;
        }
        loadedImageBytes += size;
        loadedImageDecodedBytes += decodedSize;
        const img = document.createElement('img');
        img.src = `data:${result.content_type};base64,${result.data}`;
        img.alt = alt || 'Markdown image';
        if (title) img.title = title;
        placeholder.replaceWith(img);
        return true;
    }

    function releaseDownloadReservation(active) {
        if (!active?.reserved) return;
        releaseImageBudget(active.generation);
        active.reserved = false;
    }

    async function loadRemoteImage(descriptor) {
        const api = service();
        if (!api) return;
        const gen = descriptor.generation ?? generation;
        const placeholder = resetImagePlaceholder(descriptor);
        if (!reserveImageBudget(placeholder, gen)) return;
        const requestID = crypto.randomUUID();
        placeholder.appendChild(externalLink(descriptor.source, descriptor.source));
        const progress = document.createElement('progress');
        progress.className = 'markdown-image-progress';
        progress.max = 100;
        progress.removeAttribute('value');
        progress.setAttribute('aria-label', `Loading ${descriptor.alt || 'image'}`);
        placeholder.appendChild(progress);
        const status = document.createElement('span');
        status.className = 'markdown-image-progress-status';
        status.textContent = 'Queued';
        placeholder.appendChild(status);
        const cancel = document.createElement('button');
        cancel.type = 'button';
        cancel.textContent = 'Cancel';
        cancel.addEventListener('click', () => api.CancelRemoteImage(requestID));
        placeholder.appendChild(cancel);
        activeDownloads.set(requestID, { descriptor, progress, status, cancel, generation: gen, reserved: true });
        try {
            const result = await api.LoadRemoteImage(requestID, descriptor.source);
            const active = activeDownloads.get(requestID);
            releaseDownloadReservation(active);
            if (!active || gen !== generation) return;
            displayImage(placeholder, result, descriptor.alt, descriptor.title);
        } catch (error) {
            const active = activeDownloads.get(requestID);
            releaseDownloadReservation(active);
            if (!active || gen !== generation) return;
            addURLControls(descriptor, true);
            const note = document.createElement('span');
            note.className = 'markdown-image-note markdown-image-error';
            note.textContent = String(error?.message || error || 'Image load failed');
            descriptor.placeholder.appendChild(note);
        } finally {
            activeDownloads.delete(requestID);
        }
    }

    async function probeRemoteImage(descriptor, gen) {
        addURLControls(descriptor, true);
        const api = service();
        if (!api || !reserveImageBudget(descriptor.placeholder, gen)) return;
        try {
            const result = await api.GetCachedRemoteImage(descriptor.source);
            releaseImageBudget(gen);
            if (gen !== generation || !result?.hit) return;
            displayImage(descriptor.placeholder, result, descriptor.alt, descriptor.title);
        } catch (_) {
            releaseImageBudget(gen);
            // A cache miss/failure leaves the explicit Load control intact.
        }
    }

    async function loadLocalImage(placeholder, clipID, descriptor, gen = generation) {
        if (!reserveImageBudget(placeholder, gen)) return;
        try {
            const result = await service().GetLocalImage(clipID);
            releaseImageBudget(gen);
            if (gen !== generation) return;
            displayImage(placeholder, result, descriptor?.alt, descriptor?.title);
        } catch (error) {
            releaseImageBudget(gen);
            if (gen !== generation) return;
            const note = document.createElement('span');
            note.className = 'markdown-image-note markdown-image-error';
            note.textContent = String(error?.message || error || 'Image unavailable');
            placeholder.appendChild(note);
        }
    }

    async function validateEmbeddedImage(descriptor, gen) {
        const match = descriptor.source.match(/^data:(image\/(?:png|jpeg|gif|webp));base64,([a-z0-9+/=\s]+)$/i);
        if (!match) {
            resetImagePlaceholder(descriptor).append('Unsupported embedded image');
            return;
        }
        if (!reserveImageBudget(descriptor.placeholder, gen)) return;
        try {
            const result = await service().ValidateEmbeddedImage(match[2].replace(/\s/g, ''), match[1].toLowerCase());
            releaseImageBudget(gen);
            if (gen !== generation) return;
            displayImage(descriptor.placeholder, result, descriptor.alt, descriptor.title);
        } catch (error) {
            releaseImageBudget(gen);
            if (gen !== generation) return;
            resetImagePlaceholder(descriptor).append(String(error?.message || error || 'Embedded image unavailable'));
        }
    }

    async function enhance(container) {
        const gen = generation;
        const api = service();
        if (!api || sourceClipID === null) return;

        const relativeLinks = [];
        container.querySelectorAll('a[href]').forEach(link => {
            if (decorateStaticLink(link) !== 'relative') return;
            const reference = link.getAttribute('href') || '';
            link.removeAttribute('href');
            link.setAttribute('aria-disabled', 'true');
            link.dataset.markdownReferenceStatus = 'resolving';
            relativeLinks.push({ link, reference });
        });

        const descriptors = (container.markdownImages || []).slice(0, MAX_PREVIEW_IMAGES);
        (container.markdownImages || []).slice(MAX_PREVIEW_IMAGES).forEach(descriptor => {
            resetImagePlaceholder(descriptor).append('Preview image limit exceeded');
        });
        descriptors.forEach(descriptor => { descriptor.generation = gen; });
        const relativeImages = descriptors.filter(descriptor =>
            !isExternalScheme(descriptor.source) && !descriptor.source.startsWith('/')
        );

        const localEntries = [
            ...relativeLinks.map(item => ({ reference: item.reference, target: item })),
            ...relativeImages.map(descriptor => ({ reference: descriptor.source, target: descriptor })),
        ];
        localEntries.slice(MAX_LOCAL_REFERENCES).forEach(entry => {
            if (entry.target.link) {
                entry.target.link.dataset.markdownReferenceStatus = 'invalid';
                entry.target.link.title = 'Local reference limit exceeded';
            } else {
                resetImagePlaceholder(entry.target).append('Local reference limit exceeded');
            }
        });

        const uniqueReferences = [...new Set(localEntries.slice(0, MAX_LOCAL_REFERENCES).map(entry => entry.reference))];
        const resultByReference = new Map();
        if (uniqueReferences.length > 0) {
            try {
                const results = await api.ResolveReferences(sourceClipID, uniqueReferences);
                results.forEach((result, index) => resultByReference.set(uniqueReferences[index], result));
            } catch (error) {
                console.error('Failed to resolve Markdown references:', error);
                relativeLinks.forEach(({ link }) => {
                    link.dataset.markdownReferenceStatus = 'error';
                    link.title = 'Local reference could not be resolved';
                });
            }
        }
        if (gen !== generation) return;

        relativeLinks.slice(0, MAX_LOCAL_REFERENCES).forEach(({ link, reference }) => {
            const result = resultByReference.get(reference);
            if (result) applyReferenceResult(link, result);
        });

        for (const descriptor of descriptors) {
            if (gen !== generation) return;
            if (/^https:\/\//i.test(descriptor.source)) {
                await probeRemoteImage(descriptor, gen);
                continue;
            }
            if (/^http:\/\//i.test(descriptor.source)) {
                addURLControls(descriptor, false);
                continue;
            }
            if (/^data:/i.test(descriptor.source)) {
                await validateEmbeddedImage(descriptor, gen);
                continue;
            }
            if (isExternalScheme(descriptor.source) || descriptor.source.startsWith('/')) {
                resetImagePlaceholder(descriptor).append('Image unavailable');
                continue;
            }

            const result = resultByReference.get(descriptor.source);
            const placeholder = resetImagePlaceholder(descriptor);
            if (!result) {
                placeholder.append('Local reference could not be resolved');
                continue;
            }
            placeholder.dataset.markdownReferenceStatus = result.status;
            if (result.status === 'unique') {
                await loadLocalImage(placeholder, result.candidates[0].clip_id, descriptor, gen);
            } else if (result.status === 'ambiguous') {
                const button = document.createElement('button');
                button.type = 'button';
                button.textContent = 'Choose Image';
                button.addEventListener('click', () => showCandidateChooser(placeholder, result.candidates, '', true));
                placeholder.appendChild(button);
            } else {
                const note = document.createElement('span');
                note.className = 'markdown-image-note';
                note.textContent = result.status === 'invalid' ? 'Relative image unavailable' : 'Image unavailable';
                placeholder.appendChild(note);
            }
        }
    }

    if (window.runtime?.EventsOn) {
        window.runtime.EventsOn('markdown:image-cache-cleared', () => {
            if (typeof TextClipEditor !== 'undefined') TextClipEditor.refreshPreview();
        });
        window.runtime.EventsOn('markdown:image-progress', progress => {
            const active = activeDownloads.get(progress.request_id);
            if (!active) return;
            if (progress.total > 0) {
                active.progress.value = Math.min(100, progress.percent || 0);
                active.status.textContent = `${Math.round(progress.percent || 0)}% · ${formatFileSize(progress.bytes || 0)}`;
            } else {
                active.progress.removeAttribute('value');
                active.status.textContent = `${progress.state === 'queued' ? 'Queued' : 'Loading'} · ${formatFileSize(progress.bytes || 0)}`;
            }
            if (progress.state === 'cancelled') {
                releaseDownloadReservation(active);
                if (active.generation === generation) addURLControls(active.descriptor, true);
                activeDownloads.delete(progress.request_id);
            }
        });
    }

    return { open, beginRender, close, enhance };
})();
