// Markdown clip preview policies: safe links, clip-relative references, and images.
const MarkdownPreview = (() => {
    const MAX_IMAGE_BYTES = 15 * 1024 * 1024;
    const MAX_PREVIEW_IMAGE_BYTES = 100 * 1024 * 1024;
    const MAX_PREVIEW_IMAGES = 256;
    const MAX_LOCAL_REFERENCES = 128;
    // Images fetched at once while enhancing a render. Each in-flight fetch
    // reserves MAX_IMAGE_BYTES of the preview budget, so this also bounds that.
    const IMAGE_LOAD_CONCURRENCY = 4;
    let sourceClipID = null;
    let generation = 0;
    let loadedImageBytes = 0;
    let loadedImageDecodedBytes = 0;
    let reservedImageBytes = 0;
    const activeDownloads = new Map();
    // Per editor session: image key -> Promise of a validated image held as an
    // object URL. Preview re-renders on every edit; without this each render
    // refetched (and re-validated, and re-shipped as base64) every image.
    // Keys: `local:<clipID>`, `remote:<url>`, or `data:<sha256 of the URL>`.
    // Only successes stay cached, and only while something displays them:
    // an entry is kept while the current render shows it, a load of the
    // current render is still waiting on it, or the last completed render
    // showed it. An entry no reference of the current render ended up
    // displaying (e.g. refused by the preview budget) is dropped and revoked
    // as soon as its last waiting load settles, so the cache never holds
    // more than the budget lets two renders show plus the loads in flight —
    // not every image a render fetched and refused. beginRender sweeps every
    // entry neither the render being replaced nor the last completed render
    // showed, except loads still in flight, which the next sweep handles.
    // `local:` entries are dropped whenever the library change counter has
    // moved (a referenced clip edited, deleted or restored over), and the
    // whole cache goes, URLs revoked, when the session ends.
    let imageCache = new Map();
    // Keys an <img> of the current render (renderKeys) or the last
    // completed one (completedKeys) displays.
    let renderKeys = new Set();
    let completedKeys = new Set();
    // Key -> loads of the current render that may still display that entry.
    let pendingUses = new Map();
    let localCacheVersion = null;

    function service() {
        return window.go?.main?.MarkdownService || null;
    }

    function revokeEntries(promises) {
        for (const promise of promises) {
            promise.then(entry => URL.revokeObjectURL(entry.url), () => {});
        }
    }

    function clearImageCache() {
        revokeEntries(imageCache.values());
        imageCache = new Map();
        renderKeys = new Set();
        completedKeys = new Set();
        pendingUses = new Map();
        localCacheVersion = null;
    }

    // A load of render `gen` is about to look the key up. Every useKey is
    // paired with exactly one finishUse once that load has displayed (or
    // given up on) the entry.
    function useKey(key, gen) {
        if (gen === generation) pendingUses.set(key, (pendingUses.get(key) || 0) + 1);
    }

    // A load of a superseded render leaves its entry to the next sweep: the
    // render that replaced it may be about to reuse it.
    function finishUse(key, gen, shown) {
        if (gen !== generation) return;
        const count = (pendingUses.get(key) || 0) - 1;
        if (count > 0) pendingUses.set(key, count);
        else pendingUses.delete(key);
        if (shown) renderKeys.add(key);
        if (renderKeys.has(key) || completedKeys.has(key) || pendingUses.has(key)) return;
        const promise = imageCache.get(key);
        if (!promise) return;
        imageCache.delete(key);
        revokeEntries([promise]);
    }

    // Runs when a new render replaces the DOM: anything the outgoing render
    // and the last completed one did not use is no longer on screen.
    function sweepImageCache() {
        const dropped = [];
        for (const [key, promise] of imageCache) {
            // A load the outgoing render still has in flight is kept for the
            // new render to reuse; the next sweep drops it unless shown.
            if (!renderKeys.has(key) && !completedKeys.has(key) && !pendingUses.has(key)) {
                dropped.push(promise);
                imageCache.delete(key);
            }
        }
        revokeEntries(dropped);
        renderKeys = new Set();
        pendingUses = new Map();
    }

    function dropLocalImages() {
        const dropped = [];
        for (const [key, promise] of imageCache) {
            if (key.startsWith('local:')) {
                dropped.push(promise);
                imageCache.delete(key);
            }
        }
        revokeEntries(dropped);
    }

    async function libraryVersion() {
        try {
            return await window.go.main.App.GetLibraryVersion();
        } catch (_) {
            return null;
        }
    }

    async function embeddedImageKey(source) {
        try {
            const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(source));
            return 'data:' + Array.from(new Uint8Array(digest), b => b.toString(16).padStart(2, '0')).join('');
        } catch (_) {
            return source;
        }
    }

    function dropRemoteImages() {
        const dropped = [];
        for (const [key, promise] of imageCache) {
            if (key.startsWith('remote:')) {
                dropped.push(promise);
                imageCache.delete(key);
            }
        }
        revokeEntries(dropped);
    }

    // Converts a backend MarkdownImageData (already validated in Go: sniffed
    // type, DecodeConfig dimensions, decode budget) into a cache entry.
    function toImageEntry(result) {
        const binary = atob(result.data || '');
        const bytes = new Uint8Array(binary.length);
        for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
        return {
            url: URL.createObjectURL(new Blob([bytes], { type: result.content_type })),
            content_type: result.content_type,
            size: result.size,
            decoded_size: result.decoded_size,
            width: result.width,
            height: result.height,
        };
    }

    // One fetch per key per session; concurrent asks share it. A failure is
    // not cached, so the next render retries.
    function cachedImage(key, fetchResult) {
        let promise = imageCache.get(key);
        if (promise) return promise;
        const cache = imageCache;
        promise = Promise.resolve().then(fetchResult).then(toImageEntry);
        cache.set(key, promise);
        promise.catch(() => {
            if (cache.get(key) === promise) cache.delete(key);
        });
        return promise;
    }


    // One image-load scheduler for the whole session, not one pool per render.
    // A superseded render cannot cancel a GetLocalImage/ValidateEmbeddedImage
    // call already in flight, so a per-render pool let every new render start
    // IMAGE_LOAD_CONCURRENCY more reads on top of the ones still running.
    // Here a slot is held until its task — and so the backend call it awaits —
    // settles, whatever generation it belongs to; queued tasks of a
    // superseded render are discarded without running.
    let imageLoadQueue = [];
    let imageLoadsRunning = 0;

    function scheduleImageLoad(task, gen) {
        return new Promise(resolve => {
            imageLoadQueue.push({ task, gen, resolve });
            pumpImageLoads();
        });
    }

    function pumpImageLoads() {
        while (imageLoadsRunning < IMAGE_LOAD_CONCURRENCY && imageLoadQueue.length > 0) {
            const item = imageLoadQueue.shift();
            if (item.gen !== generation) {
                item.resolve();
                continue;
            }
            imageLoadsRunning++;
            Promise.resolve()
                .then(item.task)
                .catch(error => console.error('Markdown image load failed:', error))
                .finally(() => {
                    imageLoadsRunning--;
                    item.resolve();
                    pumpImageLoads();
                });
        }
    }

    // Called after every generation bump: settles the waiting loads of the
    // renders it superseded. Running ones keep their slots until they settle.
    function discardStaleImageLoads() {
        const stale = imageLoadQueue.filter(item => item.gen !== generation);
        imageLoadQueue = imageLoadQueue.filter(item => item.gen === generation);
        stale.forEach(item => item.resolve());
    }

    function open(clipID) {
        clearImageCache();
        sourceClipID = clipID;
        generation++;
        discardStaleImageLoads();
        wakeBudgetWaiters();
        loadedImageBytes = 0;
        loadedImageDecodedBytes = 0;
        reservedImageBytes = 0;
    }

    function beginRender() {
        generation++;
        discardStaleImageLoads();
        sweepImageCache();
        wakeBudgetWaiters();
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
        discardStaleImageLoads();
        wakeBudgetWaiters();
        sourceClipID = null;
        clearImageCache();
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

    // In image mode, `descriptor` and `gen` are the image placeholder's and
    // the render that created it: the chosen image loads through the shared
    // scheduler under that render, so a click cannot add a backend read on
    // top of the IMAGE_LOAD_CONCURRENCY the render's own loads already hold,
    // and a click on a superseded render's placeholder starts no read at all.
    function showCandidateChooser(anchor, candidates, fragment, imageMode, descriptor = null, gen = generation) {
        const existing = anchor.parentElement?.querySelector(':scope > .markdown-reference-chooser');
        if (existing) {
            existing.remove();
            return;
        }
        const chooser = document.createElement('span');
        chooser.className = 'markdown-reference-chooser';
        chooser.setAttribute('role', 'group');
        chooser.setAttribute('aria-label', 'Choose matching clip');
        let picked = false;
        candidates.forEach(candidate => {
            const button = document.createElement('button');
            button.type = 'button';
            const paths = (candidate.matched_tag_paths || []).map(path => path || 'Root').join(', ');
            button.textContent = `${candidate.filename} · ${paths}`;
            button.addEventListener('click', async () => {
                if (imageMode) {
                    // One pick per chooser: a second click must not queue another read.
                    if (picked) return;
                    picked = true;
                    await scheduleImageLoad(() => loadLocalImage(anchor, candidate.clip_id, descriptor, gen), gen);
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

    // Fetches waiting for another in-flight fetch to release its reservation.
    let budgetWaiters = [];

    function wakeBudgetWaiters() {
        const waiters = budgetWaiters;
        budgetWaiters = [];
        waiters.forEach(resolve => resolve());
    }

    // Reserves the worst case (MAX_IMAGE_BYTES) for one fetch. With several
    // fetches in flight, a reservation that does not fit yet waits for one of
    // them to settle — they may well turn out smaller — and is refused only
    // when nothing else is outstanding, which is exactly when the old serial
    // loop refused it.
    async function reserveImageBudget(placeholder, gen) {
        while (gen === generation &&
            loadedImageBytes + reservedImageBytes + MAX_IMAGE_BYTES > MAX_PREVIEW_IMAGE_BYTES &&
            reservedImageBytes > 0) {
            await new Promise(resolve => budgetWaiters.push(resolve));
        }
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
        wakeBudgetWaiters();
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
        img.src = result.url;
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
        const key = `remote:${descriptor.source}`;
        useKey(key, gen);
        let shown = false;
        try {
            shown = await downloadRemoteImage(api, descriptor, gen, key);
        } finally {
            finishUse(key, gen, shown);
        }
    }

    // Returns whether the cache entry under `key` ended up displayed.
    async function downloadRemoteImage(api, descriptor, gen, key) {
        const placeholder = resetImagePlaceholder(descriptor);
        const hit = imageCache.get(key);
        if (hit) {
            // Another reference to the same URL already loaded it.
            try {
                const entry = await hit;
                return gen === generation && displayImage(placeholder, entry, descriptor.alt, descriptor.title);
            } catch (_) {
                // A failed shared load falls through to a download of its own.
            }
        }
        if (!(await reserveImageBudget(placeholder, gen))) return false;
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
            if (!active || gen !== generation) return false;
            // Keeps an entry another reference stored meanwhile (its URL may
            // already be on screen) rather than replacing and revoking it.
            const entry = await cachedImage(key, () => result);
            if (gen !== generation) return false;
            return displayImage(placeholder, entry, descriptor.alt, descriptor.title);
        } catch (error) {
            const active = activeDownloads.get(requestID);
            releaseDownloadReservation(active);
            if (!active || gen !== generation) return false;
            addURLControls(descriptor, true);
            const note = document.createElement('span');
            note.className = 'markdown-image-note markdown-image-error';
            note.textContent = String(error?.message || error || 'Image load failed');
            descriptor.placeholder.appendChild(note);
            return false;
        } finally {
            activeDownloads.delete(requestID);
        }
    }

    async function probeRemoteImage(descriptor, gen) {
        addURLControls(descriptor, true);
        const api = service();
        const key = `remote:${descriptor.source}`;
        useKey(key, gen);
        let shown = false;
        try {
            const cached = imageCache.has(key);
            if (!api || (!cached && !(await reserveImageBudget(descriptor.placeholder, gen)))) return;
            try {
                const entry = await cachedImage(key, async () => {
                    const result = await api.GetCachedRemoteImage(descriptor.source);
                    // A miss is not an image; it must not be cached as one.
                    if (!result?.hit) throw new Error('not cached');
                    return result;
                });
                if (!cached) releaseImageBudget(gen);
                if (gen !== generation) return;
                shown = displayImage(descriptor.placeholder, entry, descriptor.alt, descriptor.title);
            } catch (_) {
                if (!cached) releaseImageBudget(gen);
                // A cache miss/failure leaves the explicit Load control intact.
            }
        } finally {
            finishUse(key, gen, shown);
        }
    }

    async function loadLocalImage(placeholder, clipID, descriptor, gen = generation) {
        const key = `local:${clipID}`;
        useKey(key, gen);
        let shown = false;
        try {
            const cached = imageCache.has(key);
            if (!cached && !(await reserveImageBudget(placeholder, gen))) return;
            try {
                const entry = await cachedImage(key, () => service().GetLocalImage(clipID));
                if (!cached) releaseImageBudget(gen);
                if (gen !== generation) return;
                shown = displayImage(placeholder, entry, descriptor?.alt, descriptor?.title);
            } catch (error) {
                if (!cached) releaseImageBudget(gen);
                if (gen !== generation) return;
                const note = document.createElement('span');
                note.className = 'markdown-image-note markdown-image-error';
                note.textContent = String(error?.message || error || 'Image unavailable');
                placeholder.appendChild(note);
            }
        } finally {
            finishUse(key, gen, shown);
        }
    }

    async function validateEmbeddedImage(descriptor, gen) {
        const match = descriptor.source.match(/^data:(image\/(?:png|jpeg|gif|webp));base64,([a-z0-9+/=\s]+)$/i);
        if (!match) {
            resetImagePlaceholder(descriptor).append('Unsupported embedded image');
            return;
        }
        const key = await embeddedImageKey(descriptor.source);
        if (gen !== generation) return;
        useKey(key, gen);
        let shown = false;
        try {
            const cached = imageCache.has(key);
            if (!cached && !(await reserveImageBudget(descriptor.placeholder, gen))) return;
            try {
                const entry = await cachedImage(key, () =>
                    service().ValidateEmbeddedImage(match[2].replace(/\s/g, ''), match[1].toLowerCase()));
                if (!cached) releaseImageBudget(gen);
                if (gen !== generation) return;
                shown = displayImage(descriptor.placeholder, entry, descriptor.alt, descriptor.title);
            } catch (error) {
                if (!cached) releaseImageBudget(gen);
                if (gen !== generation) return;
                resetImagePlaceholder(descriptor).append(String(error?.message || error || 'Embedded image unavailable'));
            }
        } finally {
            finishUse(key, gen, shown);
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
        // Read before any image of this render is fetched, so an entry cached
        // under this version holds bytes at least this new.
        // Only a render with local images needs it: one without uses no
        // `local:` entry, and the next beginRender sweeps them anyway.
        const versionPromise = relativeImages.length > 0 ? libraryVersion() : Promise.resolve(localCacheVersion);
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
        const version = await versionPromise;
        if (gen !== generation) return;
        if (version === null || version !== localCacheVersion) {
            dropLocalImages();
            localCacheVersion = version;
        }

        relativeLinks.slice(0, MAX_LOCAL_REFERENCES).forEach(({ link, reference }) => {
            const result = resultByReference.get(reference);
            if (result) applyReferenceResult(link, result);
        });

        // A bounded pool rather than a serial await per image: each fetch is an IPC
        // round trip plus Go-side validation, and they are independent — every
        // descriptor owns its placeholder.
        const tasks = descriptors.map(descriptor => () => enhanceImage(descriptor, gen, resultByReference));
        await Promise.all(tasks.map(task => scheduleImageLoad(task, gen)));
        if (gen === generation) completedKeys = renderKeys;
    }

    async function enhanceImage(descriptor, gen, resultByReference) {
        if (gen !== generation) return;
        if (/^https:\/\//i.test(descriptor.source)) {
            await probeRemoteImage(descriptor, gen);
            return;
        }
        if (/^http:\/\//i.test(descriptor.source)) {
            addURLControls(descriptor, false);
            return;
        }
        if (/^data:/i.test(descriptor.source)) {
            await validateEmbeddedImage(descriptor, gen);
            return;
        }
        if (isExternalScheme(descriptor.source) || descriptor.source.startsWith('/')) {
            resetImagePlaceholder(descriptor).append('Image unavailable');
            return;
        }

        const result = resultByReference.get(descriptor.source);
        const placeholder = resetImagePlaceholder(descriptor);
        if (!result) {
            placeholder.append('Local reference could not be resolved');
            return;
        }
        placeholder.dataset.markdownReferenceStatus = result.status;
        if (result.status === 'unique') {
            await loadLocalImage(placeholder, result.candidates[0].clip_id, descriptor, gen);
        } else if (result.status === 'ambiguous') {
            const button = document.createElement('button');
            button.type = 'button';
            button.textContent = 'Choose Image';
            button.addEventListener('click', () => showCandidateChooser(placeholder, result.candidates, '', true, descriptor, gen));
            placeholder.appendChild(button);
        } else {
            const note = document.createElement('span');
            note.className = 'markdown-image-note';
            note.textContent = result.status === 'invalid' ? 'Relative image unavailable' : 'Image unavailable';
            placeholder.appendChild(note);
        }
    }

    if (window.runtime?.EventsOn) {
        window.runtime.EventsOn('markdown:image-cache-cleared', () => {
            // The session cache must not keep showing what the user just cleared.
            dropRemoteImages();
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
