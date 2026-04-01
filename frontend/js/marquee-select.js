/**
 * Marquee (rubber-band) selection for the clip gallery.
 *
 * Allows users to click and drag on empty space in the gallery grid
 * to draw a selection rectangle that selects all intersecting clips.
 *
 * Usage: initMarqueeSelect({ gallery, selectedIds, updateBulkToolbar })
 */

// eslint-disable-next-line no-unused-vars
function initMarqueeSelect({ gallery, selectedIds, updateBulkToolbar }) {
    const DRAG_THRESHOLD = 5;
    const SCROLL_ZONE = 40;
    const SCROLL_SPEED = 8;

    let isDragging = false;
    let startPageX = 0;
    let startPageY = 0;
    let overlay = null;
    let preSnapshot = new Set();
    let shiftHeld = false;
    let rafId = null;

    // Listen on <main> so the empty area below the gallery also triggers marquee
    const mainEl = gallery.closest('main');
    const sectionEl = gallery.parentElement;
    mainEl.addEventListener('mousedown', onMouseDown);

    function isEmptySpace(target) {
        return target === gallery || target === sectionEl ||
            (target === mainEl && gallery.offsetHeight > 0);
    }

    function onMouseDown(e) {
        // Only trigger on empty space (gallery gaps, section, or main below cards)
        if (!isEmptySpace(e.target)) return;
        // Left click only
        if (e.button !== 0) return;
        // Guard against folder drag in progress
        if (window.__internalDragActive) return;

        e.preventDefault();

        shiftHeld = e.shiftKey;
        preSnapshot = new Set(selectedIds);

        // Store start position in page coordinates (scroll-invariant)
        startPageX = e.pageX;
        startPageY = e.pageY;

        isDragging = false;

        document.addEventListener('mousemove', onMouseMove);
        document.addEventListener('mouseup', onMouseUp);
    }

    function onMouseMove(e) {
        const dx = e.pageX - startPageX;
        const dy = e.pageY - startPageY;

        if (!isDragging) {
            if (Math.abs(dx) < DRAG_THRESHOLD && Math.abs(dy) < DRAG_THRESHOLD) return;
            isDragging = true;
            createOverlay();

            // Clear existing selection if Shift not held
            if (!shiftHeld) {
                clearAllSelections();
            }
        }

        if (rafId) cancelAnimationFrame(rafId);
        rafId = requestAnimationFrame(() => {
            updateOverlayPosition(e.pageX, e.pageY);
            updateCardSelections();
            autoScroll(e.clientY);
        });
    }

    function onMouseUp() {
        document.removeEventListener('mousemove', onMouseMove);
        document.removeEventListener('mouseup', onMouseUp);

        if (rafId) {
            cancelAnimationFrame(rafId);
            rafId = null;
        }

        if (!isDragging) {
            // Click on empty space without dragging — deselect all (unless Shift held)
            if (!shiftHeld) {
                clearAllSelections();
                updateBulkToolbar();
            }
        } else {
            updateBulkToolbar();
        }

        removeOverlay();
        isDragging = false;
    }

    function createOverlay() {
        overlay = document.createElement('div');
        overlay.className = 'marquee-overlay';
        overlay.setAttribute('aria-hidden', 'true');
        overlay.style.cssText =
            'position:absolute;' +
            'border:2px solid rgba(28,25,23,0.6);' +
            'background:rgba(28,25,23,0.07);' +
            'border-radius:2px;' +
            'pointer-events:none;' +
            'z-index:20;';
        gallery.appendChild(overlay);
    }

    function removeOverlay() {
        if (overlay) {
            overlay.remove();
            overlay = null;
        }
    }

    function updateOverlayPosition(currentPageX, currentPageY) {
        if (!overlay) return;

        // Convert page coordinates to gallery-relative coordinates
        const galleryRect = gallery.getBoundingClientRect();
        const galleryPageLeft = galleryRect.left + window.scrollX;
        const galleryPageTop = galleryRect.top + window.scrollY;

        const x1 = startPageX - galleryPageLeft;
        const y1 = startPageY - galleryPageTop;
        const x2 = currentPageX - galleryPageLeft;
        const y2 = currentPageY - galleryPageTop;

        overlay.style.left = Math.min(x1, x2) + 'px';
        overlay.style.top = Math.min(y1, y2) + 'px';
        overlay.style.width = Math.abs(x2 - x1) + 'px';
        overlay.style.height = Math.abs(y2 - y1) + 'px';
    }

    function updateCardSelections() {
        if (!overlay) return;

        const overlayRect = overlay.getBoundingClientRect();
        const cards = gallery.querySelectorAll('li[data-id]');

        // Start from snapshot if Shift held, else empty
        const next = shiftHeld ? new Set(preSnapshot) : new Set();

        for (const card of cards) {
            if (card.style.display === 'none') continue;
            if (rectsIntersect(overlayRect, card.getBoundingClientRect())) {
                next.add(Number(card.dataset.id));
            }
        }

        syncSelection(next);
    }

    function rectsIntersect(a, b) {
        return !(a.right <= b.left || a.left >= b.right || a.bottom <= b.top || a.top >= b.bottom);
    }

    function syncSelection(next) {
        selectedIds.clear();
        for (const id of next) selectedIds.add(id);

        const cards = gallery.querySelectorAll('li[data-id]');
        for (const card of cards) {
            const id = Number(card.dataset.id);
            const cb = card.querySelector('.clip-checkbox');
            const selected = next.has(id);
            card.classList.toggle('has-checked', selected);
            if (cb) cb.checked = selected;
        }

        // Sync the Select All checkbox
        const selectAllCb = document.getElementById('select-all-checkbox');
        if (selectAllCb) {
            const allCheckboxes = gallery.querySelectorAll('.clip-checkbox');
            selectAllCb.checked = allCheckboxes.length > 0 &&
                Array.from(allCheckboxes).every(cb => cb.checked);
        }
    }

    function clearAllSelections() {
        syncSelection(new Set());
    }

    function autoScroll(clientY) {
        if (clientY < SCROLL_ZONE) {
            window.scrollBy(0, -SCROLL_SPEED);
        } else if (clientY > window.innerHeight - SCROLL_ZONE) {
            window.scrollBy(0, SCROLL_SPEED);
        }
    }
}
