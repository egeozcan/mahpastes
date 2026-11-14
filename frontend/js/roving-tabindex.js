// --- Roving Tabindex ---
// Reusable keyboard navigation for list-like structures.
// Usage: const rover = RovingTabindex.create({ container, itemSelector, ... });

const RovingTabindex = (() => {
    /**
     * @param {Object} opts
     * @param {HTMLElement} opts.container       - The parent element
     * @param {string}      opts.itemSelector    - CSS selector for navigable items
     * @param {'horizontal'|'vertical'|'grid'} [opts.orientation='vertical']
     * @param {number|Function} [opts.columns=1] - Column count (grid only)
     * @param {boolean}    [opts.wrap=false]      - Wrap at edges
     * @param {Function}   [opts.onFocus]         - Called with (item, index) on focus
     * @param {Function}   [opts.onActivate]      - Called with (item, index) on Enter/Space
     * @param {boolean}    [opts.keepActiveChildren=false] - Leave the active
     *        item's own controls in the tab order. By default an item is one
     *        tab stop and everything inside it is unreachable by Tab, which
     *        suits items whose controls have shortcuts (clip cards). For rows
     *        that have none (plugin cards, watch folders), arrows move between
     *        rows and Tab walks the controls of the row that has focus.
     */
    function create(opts) {
        const { container, itemSelector, onFocus, onActivate } = opts;
        const orientation = opts.orientation || 'vertical';
        const wrap = opts.wrap || false;
        const keepActiveChildren = opts.keepActiveChildren === true;
        const getColumns = typeof opts.columns === 'function'
            ? opts.columns
            : () => (opts.columns || 1);

        let activeIndex = 0;

        function getItems() {
            return Array.from(container.querySelectorAll(itemSelector))
                .filter(el => el.offsetParent !== null || el.offsetWidth > 0);
        }

        function setTabIndexes(items, focusIdx) {
            items.forEach((item, i) => {
                item.setAttribute('tabindex', i === focusIdx ? '0' : '-1');
                // Remove nested interactive elements from sequential tab order
                // so Tab skips the entire composite widget as a single stop.
                const children = item.querySelectorAll(
                    'button, [href], input, select, textarea, [tabindex]'
                );
                if (keepActiveChildren && i === focusIdx) {
                    // Give back what an earlier pass took, and nothing else:
                    // a control that set its own tabindex keeps it.
                    children.forEach(child => {
                        if (!child.hasAttribute('data-rover-stripped')) return;
                        const previous = child.getAttribute('data-rover-stripped');
                        if (previous === '') child.removeAttribute('tabindex');
                        else child.setAttribute('tabindex', previous);
                        child.removeAttribute('data-rover-stripped');
                    });
                    return;
                }
                children.forEach(child => {
                    if (keepActiveChildren && !child.hasAttribute('data-rover-stripped')) {
                        child.setAttribute('data-rover-stripped', child.getAttribute('tabindex') ?? '');
                    }
                    child.setAttribute('tabindex', '-1');
                });
            });
        }

        function focusItem(items, idx) {
            if (idx < 0 || idx >= items.length) return;
            activeIndex = idx;
            setTabIndexes(items, idx);
            items[idx].focus();
            items[idx].scrollIntoView({ behavior: 'smooth', block: 'nearest' });
            if (onFocus) onFocus(items[idx], idx);
        }

        function clampOrWrap(idx, total) {
            if (wrap) {
                return ((idx % total) + total) % total;
            }
            return Math.max(0, Math.min(idx, total - 1));
        }

        function handleKeydown(e) {
            const items = getItems();
            if (items.length === 0) return;

            const currentIdx = items.indexOf(e.target);
            if (currentIdx === -1) return;

            const cols = getColumns();
            let nextIdx = currentIdx;
            let handled = true;

            switch (e.key) {
                case 'ArrowRight':
                    if (orientation === 'vertical') { handled = false; break; }
                    nextIdx = clampOrWrap(currentIdx + 1, items.length);
                    break;
                case 'ArrowLeft':
                    if (orientation === 'vertical') { handled = false; break; }
                    nextIdx = clampOrWrap(currentIdx - 1, items.length);
                    break;
                case 'ArrowDown':
                    if (orientation === 'horizontal') { handled = false; break; }
                    if (orientation === 'grid') {
                        nextIdx = clampOrWrap(currentIdx + cols, items.length);
                    } else {
                        nextIdx = clampOrWrap(currentIdx + 1, items.length);
                    }
                    break;
                case 'ArrowUp':
                    if (orientation === 'horizontal') { handled = false; break; }
                    if (orientation === 'grid') {
                        nextIdx = clampOrWrap(currentIdx - cols, items.length);
                    } else {
                        nextIdx = clampOrWrap(currentIdx - 1, items.length);
                    }
                    break;
                case 'Home':
                    nextIdx = 0;
                    break;
                case 'End':
                    nextIdx = items.length - 1;
                    break;
                case 'Enter':
                case ' ':
                    if (onActivate) onActivate(items[currentIdx], currentIdx);
                    e.preventDefault();
                    return;
                default:
                    handled = false;
            }

            if (!handled) return;
            if (nextIdx !== currentIdx) {
                e.preventDefault();
                focusItem(items, nextIdx);
            } else {
                e.preventDefault();
            }
        }

        function handleFocusin(e) {
            const items = getItems();
            if (items.length === 0) return;
            if (keepActiveChildren) {
                // Focus landed in a row (a click on it or on one of its
                // controls): that row becomes the active one, so Tab continues
                // through its controls and not another row's.
                const rowIdx = items.findIndex(item => item.contains(e.target));
                if (rowIdx !== -1 && rowIdx !== activeIndex) {
                    activeIndex = rowIdx;
                    setTabIndexes(items, rowIdx);
                    if (onFocus && items[rowIdx] === e.target) onFocus(items[rowIdx], rowIdx);
                    return;
                }
            }
            if (container.contains(e.relatedTarget)) return;
            const targetIdx = items.indexOf(e.target);
            if (targetIdx !== -1) {
                activeIndex = targetIdx;
                if (onFocus) onFocus(items[targetIdx], targetIdx);
            }
        }

        function update() {
            const items = getItems();
            if (items.length === 0) return;
            if (activeIndex >= items.length) activeIndex = items.length - 1;
            if (activeIndex < 0) activeIndex = 0;
            setTabIndexes(items, activeIndex);
        }

        function reset() {
            activeIndex = 0;
            update();
        }

        function getActiveIndex() {
            return activeIndex;
        }

        function setActiveIndex(idx) {
            const items = getItems();
            if (idx >= 0 && idx < items.length) {
                activeIndex = idx;
                setTabIndexes(items, idx);
            }
        }

        /** Programmatically navigate in the given direction and focus the result. */
        function navigate(direction) {
            const items = getItems();
            if (items.length === 0) return;
            const cols = getColumns();
            let nextIdx = activeIndex;
            switch (direction) {
                case 'up':
                    nextIdx = orientation === 'grid'
                        ? clampOrWrap(activeIndex - cols, items.length)
                        : clampOrWrap(activeIndex - 1, items.length);
                    break;
                case 'down':
                    nextIdx = orientation === 'grid'
                        ? clampOrWrap(activeIndex + cols, items.length)
                        : clampOrWrap(activeIndex + 1, items.length);
                    break;
                case 'left':
                    nextIdx = clampOrWrap(activeIndex - 1, items.length);
                    break;
                case 'right':
                    nextIdx = clampOrWrap(activeIndex + 1, items.length);
                    break;
            }
            if (nextIdx !== activeIndex) focusItem(items, nextIdx);
        }

        function destroy() {
            container.removeEventListener('keydown', handleKeydown);
            container.removeEventListener('focusin', handleFocusin);
        }

        container.addEventListener('keydown', handleKeydown);
        container.addEventListener('focusin', handleFocusin);
        update();

        return { update, reset, destroy, getActiveIndex, setActiveIndex, getItems, navigate };
    }

    return { create };
})();
