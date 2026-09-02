// Plugin search field — an async combobox over a plugin's on_search hook.
//
// Used for manifest fields with `type = "search"` in both the plugin settings
// panel and the action options dialog. Structure follows tag-autocomplete.js
// (dropdown injected as a sibling, mousedown preventDefault beats blur,
// openToken race guard) but the ranked local filter is replaced by a debounced
// SearchPluginOptions call, plus loading / no-results / busy / error rows.
// It is deliberately a separate module from TagAutocomplete: that one is a
// local ranked filter with create-new semantics; merging the two would put a
// regression into the import wizard and tag inputs.
//
// Usage:
//   const field = PluginSearchField.attach({
//       input,        // visible text input — shows the selected label
//       hiddenInput,  // hidden input — carries the selected value
//       pluginId, source,
//       onSelect: (value, label) => {},
//   });
//   // later: field.destroy() or field.clear()
//
// Any keystroke in the visible input clears the hidden value, so a form can
// only ever submit a value that came from an actual selection.

const PluginSearchField = (() => {

    const DEBOUNCE_MS = 250;
    let fieldSeq = 0; // unique ids for ARIA wiring

    function attach(opts) {
        const input = opts.input;
        const hiddenInput = opts.hiddenInput;
        const pluginId = opts.pluginId;
        const source = opts.source;
        const onSelect = opts.onSelect || (() => {});

        const uid = `plugin-search-${++fieldSeq}`;

        // Dropdown element, injected as sibling of the input. Its parent is
        // expected to be position: relative.
        const dropdown = document.createElement('div');
        dropdown.id = `${uid}-listbox`;
        dropdown.className = 'hidden absolute left-0 right-0 mt-1 bg-white border border-stone-200 rounded-md shadow-lg max-h-56 overflow-y-auto z-[70]';
        dropdown.setAttribute('role', 'listbox');
        input.insertAdjacentElement('afterend', dropdown);

        // Full combobox ARIA, which the tag autocomplete lacks.
        input.setAttribute('role', 'combobox');
        input.setAttribute('aria-expanded', 'false');
        input.setAttribute('aria-controls', dropdown.id);
        input.setAttribute('aria-autocomplete', 'list');
        input.setAttribute('autocomplete', 'off');

        let items = [];       // {value, label}[]
        let activeIndex = -1;
        let open = false;
        let debounceTimer = null;
        // Guards a debounced search from painting rows after a concurrent
        // close (Escape, blur, outside click) or a newer search.
        let openToken = 0;

        function isBusyError(message) {
            return String(message || '').toLowerCase().includes('busy');
        }

        function stateRow(text, extraClass) {
            const div = document.createElement('div');
            div.className = 'px-2.5 py-1.5 text-[11px] text-stone-400 ' + (extraClass || '');
            div.textContent = text;
            return div;
        }

        function renderState(text, extraClass) {
            dropdown.innerHTML = '';
            dropdown.appendChild(stateRow(text, extraClass));
            openDropdownPanel();
        }

        function renderDropdown() {
            dropdown.innerHTML = '';
            items.forEach((it, i) => {
                const btn = document.createElement('button');
                btn.type = 'button';
                btn.setAttribute('role', 'option');
                btn.id = `${dropdown.id}-opt-${i}`;
                btn.dataset.index = String(i);
                btn.className = 'psf-item w-full text-left px-2.5 py-1.5 text-xs flex items-center justify-between hover:bg-stone-100 focus:outline-none';
                if (i === activeIndex) btn.classList.add('bg-stone-100');

                const label = document.createElement('span');
                label.className = 'truncate text-stone-800';
                label.textContent = it.label;
                btn.appendChild(label);

                const value = document.createElement('span');
                value.className = 'text-[10px] text-stone-400 font-mono shrink-0 ml-2';
                value.textContent = it.value;
                btn.appendChild(value);

                btn.addEventListener('mousedown', (e) => {
                    // mousedown beats the input's blur — prevents dropdown
                    // tearing down before the click registers.
                    e.preventDefault();
                });
                btn.addEventListener('click', () => {
                    commit(i);
                });
                dropdown.appendChild(btn);
            });
            openDropdownPanel();
        }

        function openDropdownPanel() {
            dropdown.classList.remove('hidden');
            open = true;
            input.setAttribute('aria-expanded', 'true');
        }

        function closeDropdown() {
            openToken++; // invalidate any in-flight search
            if (debounceTimer) {
                clearTimeout(debounceTimer);
                debounceTimer = null;
            }
            dropdown.classList.add('hidden');
            dropdown.innerHTML = '';
            open = false;
            activeIndex = -1;
            input.removeAttribute('aria-activedescendant');
            input.setAttribute('aria-expanded', 'false');
        }

        function runSearch(query) {
            const my = ++openToken;
            if (debounceTimer) {
                clearTimeout(debounceTimer);
                debounceTimer = null;
            }

            const doSearch = async () => {
                debounceTimer = null;
                renderState('Searching…');
                let choices = null;
                let busy = false;
                try {
                    choices = await window.go.main.PluginService.SearchPluginOptions(pluginId, source, query);
                } catch (e) {
                    // A superseded request that rejects must not overwrite a
                    // newer search's results with stale busy/error state.
                    if (my !== openToken) return;
                    const raw = e && e.message ? e.message : String(e);
                    busy = isBusyError(raw);
                    if (busy) {
                        // The plugin is running something else; the next
                        // keystroke retries. Not an error to surface loudly.
                        renderState('Plugin is busy — try again', 'text-amber-700');
                    } else if (raw && !raw.includes('plugin search failed')) {
                        // A plugin-supplied failure passes through unwrapped
                        // (plugin.SearchError), so a denied host reads as a
                        // permission problem rather than "No results". Show
                        // it, truncated to keep the dropdown usable.
                        renderState(raw.length > 120 ? raw.slice(0, 117) + '…' : raw, 'text-red-500');
                    } else {
                        renderState('Search failed', 'text-red-500');
                    }
                    return;
                }
                if (my !== openToken) return; // superseded by a newer search/close
                items = Array.isArray(choices) ? choices : [];
                activeIndex = items.length > 0 ? 0 : -1;
                if (items.length === 0) {
                    renderState('No results');
                    return;
                }
                renderDropdown();
                updateHighlight();
            };

            debounceTimer = setTimeout(doSearch, DEBOUNCE_MS);
        }

        function commit(index) {
            if (index < 0 || index >= items.length) return;
            const it = items[index];
            input.value = it.label;
            hiddenInput.value = it.value;
            closeDropdown();
            onSelect(it.value, it.label);
        }

        function updateHighlight() {
            Array.from(dropdown.querySelectorAll('.psf-item')).forEach((btn, i) => {
                btn.classList.toggle('bg-stone-100', i === activeIndex);
            });
            const activeEl = dropdown.querySelector(`.psf-item[data-index="${activeIndex}"]`);
            if (activeEl) {
                input.setAttribute('aria-activedescendant', activeEl.id);
                if (activeEl.scrollIntoView) activeEl.scrollIntoView({ block: 'nearest' });
            }
        }

        // --- Event wiring ---

        const onInput = () => {
            // Any keystroke invalidates a previously chosen value: the form
            // must not submit a stale id under an edited label.
            if (hiddenInput.value !== '') {
                hiddenInput.value = '';
                if (opts.onDeselect) opts.onDeselect();
            }
            runSearch(input.value);
        };

        const onFocus = () => {
            runSearch(input.value);
        };

        const onKeydown = (e) => {
            if (!open && (e.key === 'ArrowDown' || e.key === 'ArrowUp')) {
                e.preventDefault();
                runSearch(input.value);
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
                if (activeIndex >= 0) {
                    e.preventDefault();
                    commit(activeIndex);
                }
                // No highlight — let the form's default submit stand.
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

        // As in tag-autocomplete: delayed via rAF so a dropdown-item click has
        // time to fire before the blur closes the list.
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
                closeDropdown();
                input.removeEventListener('input', onInput);
                input.removeEventListener('focus', onFocus);
                input.removeEventListener('keydown', onKeydown);
                input.removeEventListener('blur', onBlur);
                document.removeEventListener('click', onDocumentClick);
                input.removeAttribute('role');
                input.removeAttribute('aria-expanded');
                input.removeAttribute('aria-controls');
                input.removeAttribute('aria-autocomplete');
                input.removeAttribute('aria-activedescendant');
                dropdown.remove();
            },
            close() { closeDropdown(); },
            clear() {
                input.value = '';
                hiddenInput.value = '';
                closeDropdown();
            },
        };
    }

    return { attach };
})();

if (typeof window !== 'undefined') {
    window.PluginSearchField = PluginSearchField;
}
