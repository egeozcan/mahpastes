// --- Text Clip Editor ---
// Owns text-specific editing behavior: drafts, JSON assistance, search/replace,
// wrapping, and editor status. Persistence remains in the shared editor controller.

const TextClipEditor = (() => {
    const DRAFT_PREFIX = 'mahpastes:text-editor-draft:v1:';
    const WRAP_PREFERENCE_KEY = 'mahpastes:text-editor-wrap';
    const DRAFT_DELAY_MS = 250;

    let active = false;
    let clipID = null;
    let filename = '';
    let contentType = '';
    let originalText = '';
    let draftTimer = null;
    let initialized = false;
    let wrapEnabled = true;

    function element(id) {
        return document.getElementById(id);
    }

    function textarea() {
        return element('text-editor-textarea');
    }

    function storageGet(key) {
        try {
            return localStorage.getItem(key);
        } catch (_) {
            return null;
        }
    }

    function storageSet(key, value) {
        try {
            localStorage.setItem(key, value);
            return true;
        } catch (_) {
            return false;
        }
    }

    function storageRemove(key) {
        try {
            localStorage.removeItem(key);
        } catch (_) {
            // Draft recovery is best-effort and must never block editing.
        }
    }

    function draftKey() {
        return clipID === null ? null : `${DRAFT_PREFIX}${clipID}`;
    }

    function getValue() {
        return textarea()?.value || '';
    }

    function isDirty() {
        return active && getValue() !== originalText;
    }

    function clearDraft() {
        const key = draftKey();
        if (key) storageRemove(key);
        if (draftTimer) {
            clearTimeout(draftTimer);
            draftTimer = null;
        }
    }

    function persistDraft() {
        if (!active) return;
        const status = element('text-editor-draft-status');
        const key = draftKey();
        if (!key || !isDirty()) {
            clearDraft();
            if (status) status.textContent = '';
            return;
        }

        const saved = storageSet(key, JSON.stringify({
            filename,
            contentType,
            originalText,
            text: getValue(),
            updatedAt: Date.now(),
        }));
        if (status) status.textContent = saved ? 'Draft saved' : 'Draft unavailable';
    }

    function scheduleDraft() {
        if (draftTimer) clearTimeout(draftTimer);
        draftTimer = setTimeout(() => {
            draftTimer = null;
            persistDraft();
        }, DRAFT_DELAY_MS);
    }

    function restoreDraft() {
        const key = draftKey();
        if (!key) return false;

        const raw = storageGet(key);
        if (!raw) return false;

        try {
            const draft = JSON.parse(raw);
            const matchesClip = draft.filename === filename &&
                draft.contentType === contentType &&
                draft.originalText === originalText;
            if (!matchesClip || typeof draft.text !== 'string' || draft.text === originalText) {
                storageRemove(key);
                return false;
            }
            textarea().value = draft.text;
            element('text-editor-draft-status').textContent = 'Recovered draft';
            return true;
        } catch (_) {
            storageRemove(key);
            return false;
        }
    }

    function isJSON() {
        return contentType === 'application/json';
    }

    function getJSONValidation() {
        if (!isJSON()) return { valid: true, message: '' };

        const value = getValue();
        try {
            JSON.parse(value);
            return { valid: true, message: 'Valid JSON' };
        } catch (error) {
            let location = '';
            const match = String(error?.message || '').match(/position\s+(\d+)/i);
            if (match) {
                const position = Math.min(Number(match[1]), value.length);
                const before = value.slice(0, position);
                const line = before.split('\n').length;
                const lastNewline = before.lastIndexOf('\n');
                const column = position - lastNewline;
                location = ` · Ln ${line}, Col ${column}`;
            }
            return { valid: false, message: `Invalid JSON${location}` };
        }
    }

    function updateValidation() {
        const status = element('text-editor-validation-status');
        if (!status) return;
        status.classList.remove('text-red-400', 'text-emerald-400');

        if (!isJSON()) {
            status.textContent = '';
            return;
        }

        const validation = getJSONValidation();
        status.textContent = validation.message;
        status.classList.add(validation.valid ? 'text-emerald-400' : 'text-red-400');
    }

    function updateCursorStatus() {
        const input = textarea();
        if (!input) return;
        const position = input.selectionStart || 0;
        const before = input.value.slice(0, position);
        const line = before.split('\n').length;
        const lastNewline = before.lastIndexOf('\n');
        const column = position - lastNewline;
        element('text-editor-cursor-status').textContent = `Ln ${line}, Col ${column}`;
        const count = Array.from(input.value).length;
        element('text-editor-character-status').textContent = `${count} ${count === 1 ? 'character' : 'characters'}`;
    }

    function updateSaveState() {
        if (!active) return;
        const saveButton = element('editor-save-in-place');
        if (saveButton) saveButton.disabled = !isDirty();
    }

    function matchStarts(query) {
        if (!query) return [];
        const haystack = getValue().toLocaleLowerCase();
        const needle = query.toLocaleLowerCase();
        const matches = [];
        let start = 0;
        while (start <= haystack.length - needle.length) {
            const found = haystack.indexOf(needle, start);
            if (found === -1) break;
            matches.push(found);
            start = found + Math.max(needle.length, 1);
        }
        return matches;
    }

    function setSearchStatus(currentIndex, total) {
        const status = element('text-editor-search-status');
        if (!status) return;
        if (!element('text-editor-find').value) {
            status.textContent = '';
        } else if (total === 0) {
            status.textContent = 'No matches';
        } else {
            status.textContent = `${currentIndex + 1} of ${total}`;
        }
    }

    function selectMatch(matchIndex) {
        const query = element('text-editor-find').value;
        const matches = matchStarts(query);
        if (matches.length === 0) {
            setSearchStatus(0, 0);
            renderSearchHighlights();
            return false;
        }

        const normalizedIndex = (matchIndex + matches.length) % matches.length;
        const start = matches[normalizedIndex];
        const input = textarea();
        // Keep keyboard focus in the search controls. The mirrored highlight
        // layer makes matches visible without relying on native selection paint.
        input.setSelectionRange(start, start + query.length);
        setSearchStatus(normalizedIndex, matches.length);
        updateCursorStatus();
        renderSearchHighlights();
        return true;
    }

    function currentMatchIndex(matches, query) {
        const input = textarea();
        const selected = input.value.slice(input.selectionStart, input.selectionEnd);
        if (selected.toLocaleLowerCase() === query.toLocaleLowerCase()) {
            return matches.indexOf(input.selectionStart);
        }
        return -1;
    }

    function syncHighlightLayer() {
        const input = textarea();
        const layer = element('text-editor-highlight-layer');
        if (!input || !layer) return;

        // clientWidth/clientHeight exclude scrollbars, keeping mirrored wrapping
        // aligned with the textarea's actual text viewport.
        layer.style.width = `${input.clientWidth}px`;
        layer.style.height = `${input.clientHeight}px`;
        layer.scrollTop = input.scrollTop;
        layer.scrollLeft = input.scrollLeft;
    }

    function revealActiveSearchMatch() {
        const input = textarea();
        const layer = element('text-editor-highlight-layer');
        const match = layer.querySelector('[data-search-active="true"]');
        if (!match) return;

        const layerRect = layer.getBoundingClientRect();
        const matchRect = match.getBoundingClientRect();
        const padding = 16;
        const top = matchRect.top - layerRect.top + input.scrollTop;
        const bottom = matchRect.bottom - layerRect.top + input.scrollTop;

        if (top < input.scrollTop + padding) {
            input.scrollTop = Math.max(0, top - padding);
        } else if (bottom > input.scrollTop + input.clientHeight - padding) {
            input.scrollTop = bottom - input.clientHeight + padding;
        }

        if (!wrapEnabled) {
            const left = matchRect.left - layerRect.left + input.scrollLeft;
            const right = matchRect.right - layerRect.left + input.scrollLeft;
            if (left < input.scrollLeft + padding) {
                input.scrollLeft = Math.max(0, left - padding);
            } else if (right > input.scrollLeft + input.clientWidth - padding) {
                input.scrollLeft = right - input.clientWidth + padding;
            }
        }
    }

    function renderSearchHighlights() {
        const layer = element('text-editor-highlight-layer');
        if (!layer) return;
        layer.replaceChildren();

        const panelOpen = !element('text-editor-find-panel').classList.contains('hidden');
        const query = element('text-editor-find').value;
        const matches = panelOpen ? matchStarts(query) : [];
        if (!query || matches.length === 0) {
            syncHighlightLayer();
            return;
        }

        const value = getValue();
        const activeIndex = currentMatchIndex(matches, query);
        let position = 0;
        const fragment = document.createDocumentFragment();

        matches.forEach((start, index) => {
            if (start > position) {
                fragment.appendChild(document.createTextNode(value.slice(position, start)));
            }

            const match = document.createElement('span');
            match.dataset.searchMatch = '';
            match.dataset.searchActive = index === activeIndex ? 'true' : 'false';
            match.textContent = value.slice(start, start + query.length);
            fragment.appendChild(match);
            position = start + query.length;
        });

        if (position < value.length) {
            fragment.appendChild(document.createTextNode(value.slice(position)));
        }
        layer.appendChild(fragment);
        syncHighlightLayer();
        revealActiveSearchMatch();
        syncHighlightLayer();
    }

    function find(direction) {
        const query = element('text-editor-find').value;
        const matches = matchStarts(query);
        if (matches.length === 0) {
            setSearchStatus(0, 0);
            renderSearchHighlights();
            return;
        }

        const current = currentMatchIndex(matches, query);
        if (current !== -1) {
            selectMatch(current + direction);
            return;
        }

        const input = textarea();
        if (direction > 0) {
            const next = matches.findIndex(start => start >= input.selectionEnd);
            selectMatch(next === -1 ? 0 : next);
        } else {
            let previous = matches.length - 1;
            for (let i = matches.length - 1; i >= 0; i--) {
                if (matches[i] < input.selectionStart) {
                    previous = i;
                    break;
                }
            }
            selectMatch(previous);
        }
    }

    function refreshSearch(selectFirst) {
        const query = element('text-editor-find').value;
        const matches = matchStarts(query);
        if (!query || matches.length === 0) {
            setSearchStatus(0, matches.length);
            renderSearchHighlights();
            return;
        }

        const current = currentMatchIndex(matches, query);
        if (selectFirst || current === -1) {
            selectMatch(0);
        } else {
            setSearchStatus(current, matches.length);
            renderSearchHighlights();
        }
    }

    function dispatchInput() {
        textarea().dispatchEvent(new Event('input', { bubbles: true }));
    }

    function replaceCurrent() {
        const query = element('text-editor-find').value;
        if (!query) return;

        const input = textarea();
        const selected = input.value.slice(input.selectionStart, input.selectionEnd);
        if (selected.toLocaleLowerCase() !== query.toLocaleLowerCase()) {
            find(1);
            return;
        }

        const replacement = element('text-editor-replace').value;
        const replacementStart = input.selectionStart;
        input.setRangeText(replacement, input.selectionStart, input.selectionEnd, 'end');
        dispatchInput();

        const matches = matchStarts(query);
        const nextIndex = matches.findIndex(start => start >= replacementStart + replacement.length);
        if (matches.length > 0) selectMatch(nextIndex === -1 ? 0 : nextIndex);
    }

    function escapeRegExp(value) {
        return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    }

    function replaceAll() {
        const query = element('text-editor-find').value;
        if (!query) return;
        const replacement = element('text-editor-replace').value;
        textarea().value = getValue().replace(new RegExp(escapeRegExp(query), 'giu'), () => replacement);
        dispatchInput();
    }

    function setFindPanelOpen(open) {
        const panel = element('text-editor-find-panel');
        const toggle = element('text-editor-find-toggle');
        panel.classList.toggle('hidden', !open);
        panel.classList.toggle('flex', open);
        element('text-editor-highlight-layer').classList.toggle('hidden', !open);
        toggle.setAttribute('aria-expanded', open ? 'true' : 'false');
        if (open) {
            element('text-editor-find').focus();
            element('text-editor-find').select();
            refreshSearch(true);
        } else {
            renderSearchHighlights();
            if (active) textarea().focus();
        }
    }

    function applyWrap() {
        const input = textarea();
        const toggle = element('text-editor-wrap-toggle');
        input.setAttribute('wrap', wrapEnabled ? 'soft' : 'off');
        input.classList.toggle('text-editor-nowrap', !wrapEnabled);
        element('text-editor-highlight-layer').classList.toggle('text-editor-nowrap', !wrapEnabled);
        toggle.setAttribute('aria-pressed', wrapEnabled ? 'true' : 'false');
        toggle.classList.toggle('bg-stone-700', wrapEnabled);
        toggle.classList.toggle('border-stone-500', wrapEnabled);
        toggle.classList.toggle('text-white', wrapEnabled);
        requestAnimationFrame(syncHighlightLayer);
    }

    function toggleWrap() {
        wrapEnabled = !wrapEnabled;
        applyWrap();
        storageSet(WRAP_PREFERENCE_KEY, wrapEnabled ? 'true' : 'false');
    }

    function formatJSON() {
        const validation = getJSONValidation();
        if (!validation.valid) {
            showToast('Fix invalid JSON before formatting.');
            return;
        }
        textarea().value = JSON.stringify(JSON.parse(getValue()), null, 2);
        dispatchInput();
    }

    function handleInput() {
        updateValidation();
        updateCursorStatus();
        updateSaveState();
        scheduleDraft();
        if (!element('text-editor-find-panel').classList.contains('hidden')) {
            refreshSearch(false);
        }
    }

    function open(options) {
        active = true;
        clipID = options.clipID;
        filename = options.filename;
        contentType = options.contentType;
        originalText = options.text;

        textarea().value = originalText;
        element('text-editor-find').value = '';
        element('text-editor-replace').value = '';
        element('text-editor-search-status').textContent = '';
        element('text-editor-draft-status').textContent = '';
        setFindPanelOpen(false);

        const storedWrap = storageGet(WRAP_PREFERENCE_KEY);
        wrapEnabled = storedWrap === null ? true : storedWrap !== 'false';
        applyWrap();

        element('text-editor-format-json').classList.toggle('hidden', !isJSON());
        restoreDraft();
        updateValidation();
        updateCursorStatus();
        updateSaveState();
        requestAnimationFrame(() => textarea().focus());
    }

    function close(options) {
        if (options?.discardDraft) clearDraft();
        if (draftTimer) {
            clearTimeout(draftTimer);
            draftTimer = null;
        }
        active = false;
        clipID = null;
        filename = '';
        contentType = '';
        originalText = '';
        setFindPanelOpen(false);
    }

    function confirmSave(callback) {
        const validation = getJSONValidation();
        if (!isJSON() || validation.valid) {
            callback();
            return;
        }

        showConfirmDialog(
            'Save invalid JSON?',
            'This content is not valid JSON. Save it anyway while keeping the application/json content type?',
            callback,
            null,
            { variant: 'primary', confirmLabel: 'Save Anyway' }
        );
    }

    function setup() {
        if (initialized) return;
        initialized = true;

        const input = textarea();
        input.addEventListener('input', handleInput);
        input.addEventListener('scroll', syncHighlightLayer);
        ['click', 'keyup', 'select'].forEach(eventName => input.addEventListener(eventName, updateCursorStatus));
        input.addEventListener('keydown', event => {
            if ((event.metaKey || event.ctrlKey) && event.key.toLocaleLowerCase() === 'f') {
                event.preventDefault();
                event.stopImmediatePropagation();
                setFindPanelOpen(true);
            }
        });

        element('text-editor-find-toggle').addEventListener('click', () => {
            setFindPanelOpen(element('text-editor-find-panel').classList.contains('hidden'));
        });
        element('text-editor-find-close').addEventListener('click', () => setFindPanelOpen(false));
        element('text-editor-find-next').addEventListener('click', () => find(1));
        element('text-editor-find-previous').addEventListener('click', () => find(-1));
        element('text-editor-replace-current').addEventListener('click', replaceCurrent);
        element('text-editor-replace-all').addEventListener('click', replaceAll);
        element('text-editor-find').addEventListener('input', () => refreshSearch(true));
        element('text-editor-wrap-toggle').addEventListener('click', toggleWrap);
        element('text-editor-format-json').addEventListener('click', formatJSON);

        [element('text-editor-find'), element('text-editor-replace')].forEach(searchInput => {
            searchInput.addEventListener('keydown', event => {
                if (event.key === 'Escape') {
                    event.preventDefault();
                    event.stopImmediatePropagation();
                    setFindPanelOpen(false);
                } else if (event.key === 'Enter') {
                    event.preventDefault();
                    find(event.shiftKey ? -1 : 1);
                }
            });
        });

        if (typeof ResizeObserver !== 'undefined') {
            new ResizeObserver(syncHighlightLayer).observe(input);
        } else {
            window.addEventListener('resize', syncHighlightLayer);
        }

        window.addEventListener('beforeunload', () => {
            if (active && isDirty()) persistDraft();
        });
    }

    return {
        setup,
        open,
        close,
        getValue,
        isDirty,
        clearDraft,
        confirmSave,
    };
})();
