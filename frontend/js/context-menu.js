// --- Shared Context Menu Module ---

const ContextMenu = (() => {
    let activeMenu = null;
    let activeSubmenu = null;
    let activeSubmenuTrigger = null;
    let submenuOpenTimer = null;
    let submenuCloseTimer = null;
    let outsideClickHandler = null;
    let onActionCallback = null;
    let currentClipId = null;
    let lastFocusedBeforeOpen = null;
    let activePortalRoot = document.body;

    // Maps submenu trigger elements to their children data
    const submenuChildrenMap = new WeakMap();

    // --- DOM Creation ---

    function createMenuItem(item) {
        if (item.type === 'divider') {
            const hr = document.createElement('hr');
            hr.className = 'card-menu-divider';
            return hr;
        }

        if (item.type === 'submenu') {
            return createSubmenuTrigger(item);
        }

        // Regular menu item
        const btn = document.createElement('button');
        btn.className = 'card-menu-item' + (item.danger ? ' card-menu-item-danger' : '');
        btn.setAttribute('role', 'menuitem');
        btn.setAttribute('tabindex', '-1');
        if (item.id) btn.dataset.action = item.id;
        if (item.tooltip) btn.title = item.tooltip;

        // Copy through any extra data attributes
        if (item.pluginId) btn.dataset.pluginId = item.pluginId;
        if (item.actionId) btn.dataset.actionId = item.actionId;
        if (item.hasOptions !== undefined) btn.dataset.hasOptions = item.hasOptions;

        const iconPart = item.iconHtml || '';
        const labelPart = `<span>${item.label}</span>`;
        btn.innerHTML = iconPart + labelPart;

        return btn;
    }

    function createSubmenuTrigger(item) {
        const btn = document.createElement('button');
        btn.className = 'card-menu-item card-menu-submenu-trigger';
        btn.setAttribute('role', 'menuitem');
        btn.setAttribute('tabindex', '-1');
        btn.setAttribute('aria-haspopup', 'true');
        btn.setAttribute('aria-expanded', 'false');
        if (item.submenuId) btn.dataset.submenu = item.submenuId;

        const iconPart = item.iconHtml || '';
        const labelPart = `<span>${item.label}</span>`;
        const arrowPart = '<span class="card-menu-arrow">\u25B8</span>';
        btn.innerHTML = iconPart + labelPart + arrowPart;

        submenuChildrenMap.set(btn, item.children || []);

        return btn;
    }

    function createSubmenuPanel(children) {
        const panel = document.createElement('div');
        panel.className = 'card-menu-submenu';
        panel.setAttribute('role', 'menu');

        children.forEach(child => {
            const el = createMenuItem(child);
            panel.appendChild(el);
        });

        return panel;
    }

    // --- Positioning ---

    function positionMainMenu(menu, anchor) {
        // Accept either an Element (use getBoundingClientRect) or a plain
        // rect { top, left, right, bottom, width, height } for pointer anchoring.
        const buttonRect = (typeof anchor.getBoundingClientRect === 'function')
            ? anchor.getBoundingClientRect()
            : anchor;
        const menuRect = menu.getBoundingClientRect();
        const pad = 8;
        const gap = 4;
        const vw = window.innerWidth;
        const vh = window.innerHeight;

        // Horizontal: align right edge to button, clamp within viewport
        let left = buttonRect.right - menuRect.width;
        if (left < pad) left = pad;
        if (left + menuRect.width > vw - pad) left = vw - menuRect.width - pad;

        // Vertical: prefer below, then above, then constrain with scroll
        const spaceBelow = vh - buttonRect.bottom - gap - pad;
        const spaceAbove = buttonRect.top - gap - pad;
        let top;
        let maxHeight = null;

        if (spaceBelow >= menuRect.height) {
            top = buttonRect.bottom + gap;
        } else if (spaceAbove >= menuRect.height) {
            top = buttonRect.top - menuRect.height - gap;
        } else if (spaceBelow >= spaceAbove) {
            top = buttonRect.bottom + gap;
            maxHeight = spaceBelow;
        } else {
            maxHeight = spaceAbove;
            top = buttonRect.top - gap - maxHeight;
        }

        // Final clamp
        if (top + menuRect.height > vh - pad) {
            top = vh - menuRect.height - pad;
        }
        if (top < pad) {
            top = pad;
            maxHeight = vh - 2 * pad;
        }

        menu.style.top = `${top}px`;
        menu.style.left = `${left}px`;

        if (maxHeight !== null) {
            menu.style.maxHeight = `${maxHeight}px`;
            menu.style.overflowY = 'auto';
        }
    }

    function positionSubmenu(panel, triggerEl) {
        const triggerRect = triggerEl.getBoundingClientRect();
        const pad = 8;
        const vw = window.innerWidth;
        const vh = window.innerHeight;

        // Temporarily add to DOM to measure
        panel.style.visibility = 'hidden';
        activePortalRoot.appendChild(panel);
        const panelRect = panel.getBoundingClientRect();

        // Prefer right of trigger, flip left if overflow
        let left;
        const spaceRight = vw - triggerRect.right - pad;
        const spaceLeft = triggerRect.left - pad;

        if (spaceRight >= panelRect.width) {
            left = triggerRect.right;
        } else if (spaceLeft >= panelRect.width) {
            left = triggerRect.left - panelRect.width;
        } else {
            // Not enough room either side, pick the bigger side
            left = spaceRight >= spaceLeft
                ? vw - panelRect.width - pad
                : pad;
        }

        // Top-align to trigger, clamp vertically
        let top = triggerRect.top;
        if (top + panelRect.height > vh - pad) {
            top = vh - panelRect.height - pad;
        }
        if (top < pad) {
            top = pad;
        }

        panel.style.top = `${top}px`;
        panel.style.left = `${left}px`;
        panel.style.visibility = '';
    }

    // --- Submenu Management ---

    function openSubmenu(triggerEl) {
        if (activeSubmenuTrigger === triggerEl) return;

        closeSubmenu();

        const children = submenuChildrenMap.get(triggerEl);
        if (!children || children.length === 0) return;

        const panel = createSubmenuPanel(children);
        activeSubmenu = panel;
        activeSubmenuTrigger = triggerEl;

        triggerEl.classList.add('card-menu-submenu-open');
        triggerEl.setAttribute('aria-expanded', 'true');

        positionSubmenu(panel, triggerEl);

        // Submenu hover handlers — cancel close timer when cursor enters
        panel.addEventListener('mouseenter', () => {
            clearTimeout(submenuCloseTimer);
            submenuCloseTimer = null;
        });

        panel.addEventListener('mouseleave', () => {
            scheduleSubmenuClose();
        });

        // Click delegation for submenu items
        panel.addEventListener('click', (e) => {
            const item = e.target.closest('[role="menuitem"]');
            if (!item) return;
            e.stopPropagation();
            if (onActionCallback) {
                onActionCallback(item.dataset.action, currentClipId, item);
            }
            close();
        });

        // Keyboard handler for submenu (panel is on body, not inside menu, so events don't bubble to menu)
        panel.addEventListener('keydown', (e) => {
            handleSubmenuKeyboard(e);
        });
    }

    function closeSubmenu() {
        clearTimeout(submenuOpenTimer);
        submenuOpenTimer = null;
        clearTimeout(submenuCloseTimer);
        submenuCloseTimer = null;

        if (activeSubmenu) {
            activeSubmenu.remove();
            activeSubmenu = null;
        }
        if (activeSubmenuTrigger) {
            activeSubmenuTrigger.classList.remove('card-menu-submenu-open');
            activeSubmenuTrigger.setAttribute('aria-expanded', 'false');
            activeSubmenuTrigger = null;
        }
    }

    function scheduleSubmenuClose() {
        clearTimeout(submenuCloseTimer);
        submenuCloseTimer = setTimeout(() => {
            closeSubmenu();
        }, 200);
    }

    // --- Hover Handlers for Submenu Triggers ---

    function setupSubmenuHover(menu) {
        const triggers = menu.querySelectorAll('.card-menu-submenu-trigger');

        triggers.forEach(trigger => {
            trigger.addEventListener('mouseenter', () => {
                // Cancel any pending close
                clearTimeout(submenuCloseTimer);
                submenuCloseTimer = null;

                // If a different submenu trigger is active, close it first
                if (activeSubmenuTrigger && activeSubmenuTrigger !== trigger) {
                    closeSubmenu();
                }

                // Open after delay to prevent flicker
                clearTimeout(submenuOpenTimer);
                submenuOpenTimer = setTimeout(() => {
                    openSubmenu(trigger);
                }, 150);
            });

            trigger.addEventListener('mouseleave', () => {
                // Cancel pending open if cursor left before delay
                clearTimeout(submenuOpenTimer);
                submenuOpenTimer = null;

                // Schedule close with delay for diagonal movement
                if (activeSubmenuTrigger === trigger) {
                    scheduleSubmenuClose();
                }
            });
        });

        // When hovering a non-trigger item, close submenu
        const allItems = menu.querySelectorAll(':scope > [role="menuitem"]:not(.card-menu-submenu-trigger)');
        allItems.forEach(item => {
            item.addEventListener('mouseenter', () => {
                clearTimeout(submenuOpenTimer);
                submenuOpenTimer = null;
                if (activeSubmenu) {
                    scheduleSubmenuClose();
                }
            });
        });
    }

    // --- Keyboard Navigation ---

    function getMenuItems(container) {
        return Array.from(container.querySelectorAll(':scope > [role="menuitem"]'));
    }

    function findFocusedIndex(items) {
        const focused = document.activeElement;
        return items.indexOf(focused);
    }

    function focusNextItem(items, currentIndex, direction) {
        if (items.length === 0) return -1;
        const idx = (currentIndex + direction + items.length) % items.length;
        items[idx].focus();
        return idx;
    }

    function setupKeyboard(menu) {
        menu.addEventListener('keydown', (e) => {
            // If a submenu is open, handle its keyboard first
            if (activeSubmenu && activeSubmenu.contains(document.activeElement)) {
                handleSubmenuKeyboard(e);
                return;
            }

            handleMainMenuKeyboard(e, menu);
        });
    }

    function handleMainMenuKeyboard(e, menu) {
        const items = getMenuItems(menu);
        const idx = findFocusedIndex(items);

        switch (e.key) {
            case 'ArrowDown':
                e.preventDefault();
                focusNextItem(items, idx, 1);
                break;

            case 'ArrowUp':
                e.preventDefault();
                focusNextItem(items, idx, -1);
                break;

            case 'ArrowRight': {
                const focused = items[idx];
                if (focused && focused.classList.contains('card-menu-submenu-trigger')) {
                    e.preventDefault();
                    openSubmenu(focused);
                    // Focus first submenu item
                    if (activeSubmenu) {
                        const subItems = getMenuItems(activeSubmenu);
                        if (subItems.length > 0) subItems[0].focus();
                    }
                }
                break;
            }

            case 'Enter':
            case ' ':
                e.preventDefault();
                if (idx >= 0) {
                    const focused = items[idx];
                    if (focused.classList.contains('card-menu-submenu-trigger')) {
                        openSubmenu(focused);
                        if (activeSubmenu) {
                            const subItems = getMenuItems(activeSubmenu);
                            if (subItems.length > 0) subItems[0].focus();
                        }
                    } else {
                        focused.click();
                    }
                }
                break;

            case 'Escape':
                e.preventDefault();
                e.stopPropagation();
                close();
                break;

            case 'Tab':
                e.preventDefault();
                close();
                break;
        }
    }

    function handleSubmenuKeyboard(e) {
        const items = getMenuItems(activeSubmenu);
        const idx = findFocusedIndex(items);

        switch (e.key) {
            case 'ArrowDown':
                e.preventDefault();
                focusNextItem(items, idx, 1);
                break;

            case 'ArrowUp':
                e.preventDefault();
                focusNextItem(items, idx, -1);
                break;

            case 'ArrowLeft':
                e.preventDefault();
                // Close submenu, return focus to trigger
                if (activeSubmenuTrigger) {
                    const trigger = activeSubmenuTrigger;
                    closeSubmenu();
                    trigger.focus();
                }
                break;

            case 'Enter':
            case ' ':
                e.preventDefault();
                if (idx >= 0) {
                    items[idx].click();
                }
                break;

            case 'Escape':
                e.preventDefault();
                e.stopPropagation();
                if (activeSubmenuTrigger) {
                    const trigger = activeSubmenuTrigger;
                    closeSubmenu();
                    trigger.focus();
                } else {
                    close();
                }
                break;

            case 'Tab':
                e.preventDefault();
                close();
                break;
        }
    }

    // --- Outside Click ---

    function addOutsideClickHandler() {
        // Use requestAnimationFrame to avoid catching the click that opened the menu
        requestAnimationFrame(() => {
            outsideClickHandler = (e) => {
                if (activeMenu && !activeMenu.contains(e.target) &&
                    (!activeSubmenu || !activeSubmenu.contains(e.target))) {
                    close();
                }
            };
            document.addEventListener('mousedown', outsideClickHandler);
        });
    }

    function removeOutsideClickHandler() {
        if (outsideClickHandler) {
            document.removeEventListener('mousedown', outsideClickHandler);
            outsideClickHandler = null;
        }
    }

    // --- Public API ---

    function open(items, clipId, anchor, onAction, options = {}) {
        // Close any existing menu
        close();
        activePortalRoot = options.portalRoot || document.body;

        // Dismiss tag popover if visible (it has higher z-index)
        if (typeof closeTagPopover === 'function') closeTagPopover();

        currentClipId = clipId;
        onActionCallback = onAction;
        lastFocusedBeforeOpen = document.activeElement;

        // Create menu container
        const menu = document.createElement('div');
        menu.className = 'card-menu-dropdown fixed';
        menu.setAttribute('role', 'menu');
        menu.setAttribute('aria-label', options.ariaLabel || 'Clip actions');
        if (options.menuId) menu.id = options.menuId;
        if (clipId != null) menu.dataset.clipId = clipId;

        // Populate items
        items.forEach(item => {
            const el = createMenuItem(item);
            menu.appendChild(el);
        });

        // Click delegation for main menu items (not submenu triggers)
        menu.addEventListener('click', (e) => {
            const item = e.target.closest('[role="menuitem"]');
            if (!item) return;
            // Don't handle clicks on submenu triggers — they open submenus
            if (item.classList.contains('card-menu-submenu-trigger')) return;
            e.stopPropagation();
            if (onActionCallback) {
                onActionCallback(item.dataset.action, currentClipId, item);
            }
            close();
        });

        // Add to DOM and position
        activePortalRoot.appendChild(menu);
        positionMainMenu(menu, anchor);
        if (anchor?.setAttribute) anchor.setAttribute('aria-expanded', 'true');

        // Setup interactions
        setupSubmenuHover(menu);
        setupKeyboard(menu);
        addOutsideClickHandler();

        activeMenu = menu;

        // Focus first item
        const firstItem = menu.querySelector('[role="menuitem"]');
        if (firstItem) firstItem.focus();

        return menu;
    }

    function close() {
        closeSubmenu();
        removeOutsideClickHandler();

        if (activeMenu) {
            // Reset trigger button aria state
            const clipId = activeMenu.dataset.clipId;
            if (clipId) {
                const triggerBtn = document.querySelector(`[data-action="menu"][data-id="${clipId}"]`);
                if (triggerBtn) {
                    triggerBtn.setAttribute('aria-expanded', 'false');
                }
            }
            activeMenu.remove();
            activeMenu = null;
        }

        onActionCallback = null;
        currentClipId = null;
        activePortalRoot = document.body;

        // Restore focus to the element that was focused before the menu opened
        if (lastFocusedBeforeOpen) {
            lastFocusedBeforeOpen.setAttribute?.('aria-expanded', 'false');
            if (lastFocusedBeforeOpen.isConnected) lastFocusedBeforeOpen.focus();
            lastFocusedBeforeOpen = null;
        }
    }

    function isOpen() {
        return activeMenu !== null;
    }

    return { open, close, isOpen };
})();
