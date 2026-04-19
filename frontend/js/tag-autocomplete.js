// Tag autocomplete — a small self-contained combobox for tag-name inputs.
//
// Usage:
//   const ac = TagAutocomplete.attach(inputEl, {
//       getTags,                                  // async () => [{id,name,color}, ...]
//       onSelect: (value, meta) => { ... },       // meta.kind: 'existing' | 'new'; meta.tag when existing
//   });
//   // later: ac.destroy() or ac.refresh()
//
// Behavior:
//   - Filter tags by case-insensitive substring of the typed text.
//   - Rank: exact match, then prefix match, then substring; alphabetical within each bucket.
//   - If the typed text is non-empty and no tag matches exactly, append a synthetic
//     "Create new: <typed>" row so the user can always commit a new tag with Enter.
//   - Keyboard: ArrowDown / ArrowUp move highlight, Enter commits, Esc closes, Tab closes.
//   - Outside click closes. Closing does not clear the input — the user can keep typing.

const TagAutocomplete = (() => {

    function normalize(s) {
        return String(s || '').toLowerCase().trim();
    }

    function rankTags(tags, query) {
        const q = normalize(query);
        if (!q) {
            return tags.slice().sort((a, b) => a.name.localeCompare(b.name));
        }
        const exact = [];
        const prefix = [];
        const substr = [];
        for (const t of tags) {
            const n = t.name.toLowerCase();
            if (n === q) exact.push(t);
            else if (n.startsWith(q)) prefix.push(t);
            else if (n.includes(q)) substr.push(t);
        }
        const byName = (a, b) => a.name.localeCompare(b.name);
        exact.sort(byName); prefix.sort(byName); substr.sort(byName);
        return exact.concat(prefix, substr);
    }

    function hasExactMatch(tags, query) {
        const q = normalize(query);
        if (!q) return false;
        return tags.some(t => t.name.toLowerCase() === q);
    }

    // Mirrors the Go-side validateTagName rules so we don't suggest a
    // "Create new" row that the backend would reject. Keeps the empty-segment
    // check and the reserved "_api" segment guard. Length cap is loose (the
    // backend has the authoritative one).
    function isCreatableName(query) {
        const q = String(query || '').trim();
        if (!q) return false;
        const segs = q.split('/');
        for (const s of segs) {
            const t = s.trim();
            if (t === '') return false;
            if (t === '_api') return false;
        }
        return true;
    }

    function attach(input, opts) {
        const getTags = opts.getTags || (async () => {
            if (window.go && window.go.main && window.go.main.App && window.go.main.App.GetTags) {
                return window.go.main.App.GetTags();
            }
            return [];
        });
        const onSelect = opts.onSelect || (() => {});

        // Dropdown element, injected as sibling of the input so we can position it
        // absolutely. Its parent is expected to be `position: relative`.
        const dropdown = document.createElement('div');
        dropdown.className = 'hidden absolute left-0 right-0 mt-1 bg-white border border-stone-200 rounded-md shadow-lg max-h-56 overflow-y-auto z-[70]';
        dropdown.setAttribute('role', 'listbox');
        // Inserted after the input so it appears below it within the same stacking context.
        input.insertAdjacentElement('afterend', dropdown);

        let cachedTags = null;
        let items = [];       // {kind, name, tag?}[]
        let activeIndex = -1;
        let open = false;
        // openToken guards against the async ensureTags() in openDropdown racing
        // with a concurrent closeDropdown (Escape, blur, outside click). Any
        // openDropdown call that finishes after the token has advanced becomes a
        // no-op.
        let openToken = 0;

        async function ensureTags() {
            if (cachedTags) return cachedTags;
            try {
                const t = await getTags();
                cachedTags = Array.isArray(t) ? t : [];
            } catch {
                cachedTags = [];
            }
            return cachedTags;
        }

        function buildItems(query) {
            const ranked = rankTags(cachedTags || [], query);
            const out = ranked.map(t => ({ kind: 'existing', name: t.name, tag: t }));
            // Only offer "Create new" when the typed text is a name the backend
            // would actually accept — otherwise we'd let users mint rows like
            // "incoming/" with empty leaf segments.
            if (query && !hasExactMatch(cachedTags || [], query) && isCreatableName(query)) {
                out.push({ kind: 'new', name: query });
            }
            return out;
        }

        function renderDropdown() {
            dropdown.innerHTML = '';
            items.forEach((it, i) => {
                const btn = document.createElement('button');
                btn.type = 'button';
                btn.setAttribute('role', 'option');
                btn.dataset.index = String(i);
                btn.dataset.kind = it.kind;
                btn.dataset.name = it.name;
                btn.className = 'tag-ac-item w-full text-left px-2.5 py-1.5 text-xs flex items-center justify-between hover:bg-stone-100 focus:outline-none';
                if (i === activeIndex) btn.classList.add('bg-stone-100');

                const label = document.createElement('span');
                label.className = 'truncate text-stone-800 flex items-center gap-1.5';
                const icon = document.createElement('span');
                icon.className = 'opacity-60';
                icon.textContent = it.kind === 'new' ? '✨' : '📁';
                label.appendChild(icon);
                const text = document.createElement('span');
                text.className = 'truncate';
                if (it.kind === 'new') {
                    text.textContent = 'Create new: ' + it.name;
                } else {
                    text.textContent = it.name;
                }
                label.appendChild(text);
                btn.appendChild(label);

                const badge = document.createElement('span');
                badge.className = 'text-[10px] uppercase tracking-wide shrink-0 ml-2 ' +
                    (it.kind === 'new' ? 'text-emerald-600' : 'text-stone-400');
                badge.textContent = it.kind === 'new' ? 'new' : 'existing';
                btn.appendChild(badge);

                btn.addEventListener('mousedown', (e) => {
                    // mousedown beats the input's blur — prevents dropdown tearing down
                    // before the click registers.
                    e.preventDefault();
                });
                btn.addEventListener('click', () => {
                    commit(i);
                });
                dropdown.appendChild(btn);
            });
        }

        async function openDropdown() {
            const my = ++openToken;
            await ensureTags();
            if (my !== openToken) return;   // another open/close superseded us
            items = buildItems(input.value);
            if (items.length === 0) {
                closeDropdown();
                return;
            }
            activeIndex = items.length > 0 ? 0 : -1;
            renderDropdown();
            dropdown.classList.remove('hidden');
            open = true;
        }

        function closeDropdown() {
            openToken++;   // invalidate any in-flight openDropdown
            dropdown.classList.add('hidden');
            open = false;
            activeIndex = -1;
        }

        function commit(index) {
            if (index < 0 || index >= items.length) {
                // No highlight — treat typed text as "new" if non-empty.
                const q = input.value.trim();
                if (!q) return;
                closeDropdown();
                onSelect(q, { kind: 'new' });
                return;
            }
            const it = items[index];
            input.value = it.name;
            closeDropdown();
            onSelect(it.name, it.kind === 'existing' ? { kind: 'existing', tag: it.tag } : { kind: 'new' });
        }

        function updateHighlight() {
            Array.from(dropdown.querySelectorAll('.tag-ac-item')).forEach((btn, i) => {
                btn.classList.toggle('bg-stone-100', i === activeIndex);
            });
            const activeEl = dropdown.querySelector(`.tag-ac-item[data-index="${activeIndex}"]`);
            if (activeEl && activeEl.scrollIntoView) {
                activeEl.scrollIntoView({ block: 'nearest' });
            }
        }

        // --- Event wiring ---

        const onInput = async () => {
            await openDropdown();
        };

        const onFocus = async () => {
            await openDropdown();
        };

        const onKeydown = (e) => {
            if (!open && (e.key === 'ArrowDown' || e.key === 'ArrowUp')) {
                e.preventDefault();
                openDropdown();
                return;
            }
            if (!open) return;

            if (e.key === 'ArrowDown') {
                e.preventDefault();
                activeIndex = Math.min(items.length - 1, activeIndex + 1);
                updateHighlight();
            } else if (e.key === 'ArrowUp') {
                e.preventDefault();
                activeIndex = Math.max(0, activeIndex - 1);
                updateHighlight();
            } else if (e.key === 'Enter') {
                // Only consume Enter if we actually commit something from the dropdown.
                // This keeps the input's default form-submit behavior available when
                // the user has closed the dropdown.
                e.preventDefault();
                commit(activeIndex);
            } else if (e.key === 'Escape') {
                // Stop the modal close handler from picking this up.
                e.stopPropagation();
                e.preventDefault();
                closeDropdown();
            } else if (e.key === 'Tab') {
                closeDropdown();
            }
        };

        const onDocumentClick = (e) => {
            if (!open) return;
            if (e.target === input || dropdown.contains(e.target)) return;
            closeDropdown();
        };

        // Blur handler runs after the click event on a dropdown item (the item's
        // mousedown preventDefault keeps focus on the input long enough for the
        // click to commit). Delay via rAF so a dropdown-item click has time to
        // fire first before the blur closes the list.
        const onBlur = () => {
            requestAnimationFrame(() => closeDropdown());
        };

        input.addEventListener('input', onInput);
        input.addEventListener('focus', onFocus);
        input.addEventListener('keydown', onKeydown);
        input.addEventListener('blur', onBlur);
        document.addEventListener('click', onDocumentClick);

        return {
            destroy() {
                input.removeEventListener('input', onInput);
                input.removeEventListener('focus', onFocus);
                input.removeEventListener('keydown', onKeydown);
                input.removeEventListener('blur', onBlur);
                document.removeEventListener('click', onDocumentClick);
                dropdown.remove();
            },
            refresh() {
                cachedTags = null;
            },
            close() { closeDropdown(); },
        };
    }

    return { attach, isCreatableName, _rankTags: rankTags, _hasExactMatch: hasExactMatch };
})();

if (typeof window !== 'undefined') {
    window.TagAutocomplete = TagAutocomplete;
}
