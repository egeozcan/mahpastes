// Focus trap cleanup for the comparison viewer.
let comparisonFocusTrapCleanup = null;

// --- Plugin Menu in Lightbox ---

// Render single trigger button for plugin actions in lightbox
async function renderLightboxPluginButtons(clip) {
    const container = document.getElementById('lightbox-plugin-actions');
    if (!container) return;

    container.innerHTML = '';

    if (!pluginUIActions || !pluginUIActions.lightbox_buttons || pluginUIActions.lightbox_buttons.length === 0) {
        container.classList.add('hidden');
        return;
    }

    const actions = clip
        ? pluginUIActions.lightbox_buttons.filter(action => shouldShowPluginAction(action, clip))
        : pluginUIActions.lightbox_buttons;

    if (actions.length === 0) {
        container.classList.add('hidden');
        return;
    }

    // Determine trigger label: plugin name if single plugin, "Plugins" if multiple
    const pluginNames = new Set(actions.map(a => a.plugin_name).filter(Boolean));
    const triggerLabel = pluginNames.size === 1 ? [...pluginNames][0] : 'Plugins';

    const btn = document.createElement('button');
    btn.className = 'lightbox-plugin-trigger';
    btn.id = 'lightbox-plugin-menu-trigger';
    btn.setAttribute('aria-expanded', 'false');
    btn.setAttribute('aria-haspopup', 'true');
    btn.setAttribute('aria-controls', 'lightbox-plugin-menu');

    const chevronSvg = '<svg fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M4.5 15.75l7.5-7.5 7.5 7.5"/></svg>';
    btn.innerHTML = `<span>${escapeHTML(triggerLabel)}</span>${chevronSvg}`;
    btn.setAttribute('title', 'Run plugin actions on this clip');

    btn.addEventListener('click', (e) => {
        e.stopPropagation();
        const menu = document.getElementById('lightbox-plugin-menu');
        if (menu) {
            closeLightboxPluginMenu();
        } else {
            openLightboxPluginMenu(btn, actions, clip);
        }
    });

    container.appendChild(btn);
    container.classList.remove('hidden');
}

function openLightboxPluginMenu(trigger, actions, clip) {
    // Remove any existing menu
    closeLightboxPluginMenu(true);

    const menu = document.createElement('div');
    menu.id = 'lightbox-plugin-menu';
    menu.className = 'lightbox-plugin-menu';
    menu.setAttribute('role', 'menu');
    menu.setAttribute('aria-label', 'Plugin actions');

    // Group actions by plugin_name
    const grouped = new Map();
    for (const action of actions) {
        const name = action.plugin_name || 'Plugin';
        if (!grouped.has(name)) grouped.set(name, []);
        grouped.get(name).push(action);
    }

    const showHeaders = grouped.size > 1;
    let isFirst = true;

    for (const [pluginName, pluginActions] of grouped) {
        if (showHeaders) {
            if (!isFirst) {
                const divider = document.createElement('div');
                divider.className = 'lightbox-plugin-menu-divider';
                menu.appendChild(divider);
            }
            const header = document.createElement('div');
            header.className = 'lightbox-plugin-menu-header';
            header.textContent = pluginName;
            menu.appendChild(header);
        }

        for (const action of pluginActions) {
            const item = document.createElement('button');
            item.className = 'lightbox-plugin-menu-item';
            item.setAttribute('role', 'menuitem');
            item.dataset.pluginId = action.plugin_id;
            item.dataset.actionId = action.id;

            const icon = action.icon ? (getPluginIcon(action.icon) || '') : '';
            item.innerHTML = `${icon}<span>${escapeHTML(action.label)}</span>`;

            item.addEventListener('click', (e) => {
                e.stopPropagation();
                closeLightboxPluginMenu(true);
                handleLightboxPluginAction(action, clip);
            });

            menu.appendChild(item);
        }

        isFirst = false;
    }

    lightbox.appendChild(menu);
    positionLightboxPopupMenu(menu, trigger);
    setupLightboxPluginMenuKeyboard(menu);

    // Animate in
    requestAnimationFrame(() => {
        menu.classList.add('active');
    });

    trigger.setAttribute('aria-expanded', 'true');
}

function positionLightboxPopupMenu(menu, trigger) {
    const triggerRect = trigger.getBoundingClientRect();
    const menuRect = menu.getBoundingClientRect();
    const gap = 8;

    let top = triggerRect.top - menuRect.height - gap;
    let left = triggerRect.left;

    // Fall back to below if not enough space above
    if (top < 8) {
        top = triggerRect.bottom + gap;
    }

    // Clamp horizontal to viewport
    if (left + menuRect.width > window.innerWidth - 8) {
        left = window.innerWidth - menuRect.width - 8;
    }
    if (left < 8) {
        left = 8;
    }

    // Clamp vertical to viewport
    if (top + menuRect.height > window.innerHeight - 8) {
        top = window.innerHeight - menuRect.height - 8;
    }
    if (top < 8) {
        top = 8;
    }

    menu.style.top = `${top}px`;
    menu.style.left = `${left}px`;
}

function setupLightboxPluginMenuKeyboard(menu) {
    const items = menu.querySelectorAll('.lightbox-plugin-menu-item');
    if (items.length === 0) return;

    menu.addEventListener('keydown', (e) => {
        const focused = document.activeElement;
        const itemArray = Array.from(items);
        const idx = itemArray.indexOf(focused);

        if (e.key === 'ArrowDown') {
            e.preventDefault();
            const next = idx < itemArray.length - 1 ? idx + 1 : 0;
            itemArray[next].focus();
        } else if (e.key === 'ArrowUp') {
            e.preventDefault();
            const prev = idx > 0 ? idx - 1 : itemArray.length - 1;
            itemArray[prev].focus();
        } else if (e.key === 'Escape') {
            e.preventDefault();
            e.stopPropagation();
            closeLightboxPluginMenu(true);
            document.getElementById('lightbox-plugin-menu-trigger')?.focus();
        } else if (e.key === 'Tab') {
            e.preventDefault();
            closeLightboxPluginMenu(true);
            document.getElementById('lightbox-plugin-menu-trigger')?.focus();
        }
    });

    // Focus first item
    items[0].focus();
}

function closeLightboxPluginMenu() {
    const menu = document.getElementById('lightbox-plugin-menu');
    if (!menu) return;

    document.getElementById('lightbox-plugin-menu-trigger')?.setAttribute('aria-expanded', 'false');
    menu.remove();
}

async function handleLightboxPluginAction(action, clip) {
    if (!clip) return;
    if (action.options?.length > 0) {
        openPluginOptionsDialog(action, [clip.id]);
    } else {
        await executePluginAction(action.plugin_id, action.id, [clip.id], {}, action.async);
    }
}

// --- File Actions Menu in Lightbox ---

function renderLightboxFileActions(clip) {
    const container = document.getElementById('lightbox-file-actions');
    if (!container) return;

    container.innerHTML = '';
    if (!clip) return;

    const btn = document.createElement('button');
    btn.className = 'lightbox-file-trigger';
    btn.id = 'lightbox-file-menu-trigger';
    btn.setAttribute('aria-expanded', 'false');
    btn.setAttribute('aria-haspopup', 'true');
    btn.setAttribute('aria-controls', 'lightbox-file-menu');

    // Three-dot icon + "Actions" label
    const dotsSvg = '<svg fill="currentColor" viewBox="0 0 24 24"><circle cx="12" cy="5" r="1.5"/><circle cx="12" cy="12" r="1.5"/><circle cx="12" cy="19" r="1.5"/></svg>';
    btn.innerHTML = `${dotsSvg}<span>Actions</span>`;
    btn.setAttribute('title', 'File operations and management');

    btn.addEventListener('click', (e) => {
        e.stopPropagation();
        if (ContextMenu.isOpen()) {
            closeLightboxFileMenu();
        } else {
            openLightboxFileMenu(btn, clip);
        }
    });

    container.appendChild(btn);
}

function openLightboxFileMenu(trigger, clip) {
    if (!clip) return;

    // buildMenuItemList is shared with the card menu — the lightbox intentionally
    // shows the full set of actions (metadata, expiration, merge, etc.) for parity.
    const items = buildMenuItemList(clip);
    ContextMenu.open(items, clip.id, trigger, (action, clipId, item) => {
        if (action === 'plugin') {
            handlePluginCardAction({
                action: 'plugin',
                pluginId: item.dataset.pluginId,
                actionId: item.dataset.actionId,
                clipId: clipId,
                hasOptions: item.dataset.hasOptions,
            });
        } else {
            handleLightboxFileAction(action, clip);
        }
    }, {
        portalRoot: lightbox,
        menuId: 'lightbox-file-menu',
        ariaLabel: 'File actions',
    });
}

function closeLightboxFileMenu() {
    ContextMenu.close();
}

async function handleLightboxFileAction(action, clip) {
    if (!clip) return;

    switch (action) {
        case 'open':
            try {
                await window.go.main.App.OpenClipWithDefaultApp(clip.id);
            } catch (err) {
                showToast('Failed to open clip.');
            }
            break;
        case 'open-with':
            try {
                const appPath = await window.go.main.App.ChooseApplication();
                if (appPath) {
                    await window.go.main.App.OpenClipWithApp(clip.id, appPath);
                }
            } catch (err) {
                showToast('Failed to open clip.');
            }
            break;
        case 'copy-path':
            saveTempFile(clip.id);
            break;
        case 'copy-public-link':
            createAndCopyPublicLink(clip.id);
            break;
        case 'copy-file':
            copyFileToClipboard(clip.id);
            break;
        case 'copy-contents':
            copyClipContents(clip.id);
            break;
        case 'save-file':
            saveClipToFile(clip.id);
            break;
        case 'edit':
            window.LightboxController.close();
            openEditor(clip.id);
            break;
        case 'tags': {
            const trigger = document.getElementById('lightbox-file-menu-trigger');
            openTagPopover(clip.id, trigger);
            break;
        }
        case 'metadata':
            openMetadataModal(clip.id, {
                filename: clip.filename || '',
                content_type: clip.content_type || '',
                size: clip.size || 0,
                created_at: clip.created_at || '',
            });
            break;
        case 'set-expiration': {
            const trigger = document.getElementById('lightbox-file-menu-trigger');
            openExpirationPopover(clip.id, trigger);
            break;
        }
        case 'cancel-expiration':
            cancelExpiration(clip.id);
            break;
        case 'archive':
            window.LightboxController.close();
            toggleArchiveClip(clip.id);
            break;
        case 'merge-duplicates':
            try {
                await window.go.main.App.MergeDuplicates(clip.id);
                showToast('Merged duplicates', 'success');
                loadClips();
            } catch (err) {
                showToast('Failed to merge duplicates', 'error');
            }
            break;
        case 'rename':
            renameClip(clip.id);
            break;
        case 'delete':
            window.LightboxController.close();
            deleteClip(clip.id);
            break;
    }
}

// --- Comparison Functions ---

async function openComparisonModal() {
    const selectedArray = Array.from(selectedIds);
    if (selectedArray.length !== 2) return;

    lastFocusedElementBeforeComparison = document.activeElement;
    comparisonClipIds = [...selectedArray];
    diffCache.clear();

    // Load both images as base64
    try {
        const [dataUrl1, dataUrl2] = await Promise.all([
            getImageDataUrl(selectedArray[0]),
            getImageDataUrl(selectedArray[1])
        ]);
        comparisonImgBottom.src = dataUrl1;
        comparisonImgTop.src = dataUrl2;
        comparisonImgDiff.src = '';
    } catch (error) {
        console.error('Failed to load images for comparison:', error);
        return;
    }

    // Reset state
    comparisonMode = 'fade';
    zoomLevel = 1;
    isStretched = false;
    comparisonRange.value = 50;

    // Show image info after images load
    updateComparisonImageInfo();

    // Ensure top image wrapper matches bottom image rendered size
    if (!window._comparisonResizeObserver) {
        window._comparisonResizeObserver = new ResizeObserver(() => {
            if (comparisonMode !== 'diff') {
                comparisonImgTopWrapper.style.width = comparisonImgBottom.offsetWidth + 'px';
                comparisonImgTopWrapper.style.height = comparisonImgBottom.offsetHeight + 'px';
            }
        });
    }
    window._comparisonResizeObserver.observe(comparisonImgBottom);

    updateComparisonView();
    comparisonModal.removeAttribute('inert');
    comparisonModal.classList.add('active');
    if (comparisonFocusTrapCleanup) comparisonFocusTrapCleanup();
    comparisonFocusTrapCleanup = trapFocus(comparisonModal);
    comparisonModal.focus();
}

function closeComparisonModal() {
    if (comparisonFocusTrapCleanup) {
        comparisonFocusTrapCleanup();
        comparisonFocusTrapCleanup = null;
    }
    comparisonModal.classList.remove('active');
    comparisonModal.setAttribute('inert', '');
    comparisonSimilarity.classList.add('hidden');
    comparisonImageInfo.classList.add('hidden');
    if (window._comparisonResizeObserver) {
        window._comparisonResizeObserver.disconnect();
    }
    setTimeout(() => {
        comparisonImgBottom.src = '';
        comparisonImgTop.src = '';
        comparisonImgDiff.src = '';
        diffCache.clear();
        comparisonClipIds = [];
        if (lastFocusedElementBeforeComparison) {
            lastFocusedElementBeforeComparison.focus();
        }
    }, 300);
}

function updateComparisonView() {
    const value = comparisonRange.value;

    // Mode button states
    const modes = [
        { btn: modeFadeBtn, mode: 'fade' },
        { btn: modeSliderBtn, mode: 'slider' },
        { btn: modeDiffBtn, mode: 'diff' },
    ];
    for (const { btn, mode } of modes) {
        if (comparisonMode === mode) {
            btn.classList.add('bg-white', 'shadow-sm', 'text-stone-800');
            btn.classList.remove('text-stone-500');
        } else {
            btn.classList.remove('bg-white', 'shadow-sm', 'text-stone-800');
            btn.classList.add('text-stone-500');
        }
    }

    // Show/hide images based on mode
    if (comparisonMode === 'diff') {
        comparisonImgBottom.classList.add('hidden');
        comparisonImgTopWrapper.classList.add('hidden');
        comparisonImgDiff.classList.remove('hidden');
        comparisonSliderLine.classList.add('hidden');
        comparisonRangeLabel.textContent = 'Threshold';
        comparisonLabelA.classList.add('hidden');
        comparisonLabelB.classList.add('hidden');

        loadDiffImage();
    } else {
        comparisonImgBottom.classList.remove('hidden');
        comparisonImgTopWrapper.classList.remove('hidden');
        comparisonImgDiff.classList.add('hidden');
        comparisonLabelA.classList.remove('hidden');
        comparisonLabelB.classList.remove('hidden');
        comparisonSimilarity.classList.add('hidden');

        if (comparisonMode === 'fade') {
            comparisonImgTopWrapper.style.clipPath = 'none';
            comparisonImgTop.style.opacity = value / 100;
            comparisonSliderLine.classList.add('hidden');
            comparisonRangeLabel.textContent = 'Opacity';
        } else {
            comparisonImgTopWrapper.style.clipPath = `inset(0 ${100 - value}% 0 0)`;
            comparisonImgTop.style.opacity = 1;
            comparisonSliderLine.classList.remove('hidden');
            comparisonSliderLine.style.left = `${value}%`;
            comparisonRangeLabel.textContent = 'Position';
        }
    }

    // Alignment & Stretch
    comparisonViewport.style.justifyContent = alignHSelect.value;
    comparisonViewport.style.alignItems = alignVSelect.value;

    if (isStretched) {
        comparisonContainer.style.width = '100%';
        comparisonContainer.style.height = '100%';
        comparisonImgBottom.style.width = '100%';
        comparisonImgBottom.style.height = '100%';
        comparisonImgBottom.style.objectFit = 'fill';
        comparisonImgTop.style.objectFit = 'fill';
        comparisonImgDiff.style.width = '100%';
        comparisonImgDiff.style.height = '100%';
        comparisonImgDiff.style.objectFit = 'fill';
        toggleStretchBtn.classList.add('bg-stone-800', 'text-white');
        toggleStretchBtn.classList.remove('bg-stone-100');
    } else {
        comparisonContainer.style.width = '';
        comparisonContainer.style.height = '';
        comparisonImgBottom.style.width = '';
        comparisonImgBottom.style.height = '';
        comparisonImgBottom.style.objectFit = 'contain';
        comparisonImgTop.style.objectFit = 'contain';
        comparisonImgDiff.style.width = '';
        comparisonImgDiff.style.height = '';
        comparisonImgDiff.style.objectFit = 'contain';
        toggleStretchBtn.classList.remove('bg-stone-800', 'text-white');
        toggleStretchBtn.classList.add('bg-stone-100');
    }

    // Zoom
    comparisonContainer.style.transform = `scale(${zoomLevel})`;
    zoomLevelEl.textContent = `${Math.round(zoomLevel * 100)}%`;
}

function updateComparisonImageInfo() {
    const imgA = comparisonImgBottom;
    const imgB = comparisonImgTop;

    function formatSize(bytes) {
        if (bytes < 1024) return bytes + 'B';
        if (bytes < 1048576) return (bytes / 1024).toFixed(1) + 'KB';
        return (bytes / 1048576).toFixed(1) + 'MB';
    }

    function waitForLoad(img) {
        return new Promise(resolve => {
            if (img.naturalWidth > 0) return resolve();
            img.addEventListener('load', resolve, { once: true });
        });
    }

    Promise.all([waitForLoad(imgA), waitForLoad(imgB)]).then(() => {
        const cards = comparisonClipIds.map(id =>
            document.querySelector(`#gallery li[data-id="${id}"]`)
        );
        const typeA = cards[0]?.dataset.type?.split('/')[1]?.toUpperCase() || 'IMG';
        const typeB = cards[1]?.dataset.type?.split('/')[1]?.toUpperCase() || 'IMG';

        const sizeA = cards[0]?.dataset.size ? formatSize(parseInt(cards[0].dataset.size)) : '';
        const sizeB = cards[1]?.dataset.size ? formatSize(parseInt(cards[1].dataset.size)) : '';

        const infoA = `A: ${imgA.naturalWidth}\u00d7${imgA.naturalHeight} ${typeA}${sizeA ? ' ' + sizeA : ''}`;
        const infoB = `B: ${imgB.naturalWidth}\u00d7${imgB.naturalHeight} ${typeB}${sizeB ? ' ' + sizeB : ''}`;

        comparisonImageInfo.textContent = `${infoA}  \u2502  ${infoB}`;
        comparisonImageInfo.classList.remove('hidden');

        // Show warning icon if dimensions differ
        const dimsDiffer = imgA.naturalWidth !== imgB.naturalWidth || imgA.naturalHeight !== imgB.naturalHeight;
        if (dimsDiffer) {
            comparisonImageInfo.textContent += ' \u26a0';
        }
    });
}

function swapComparisonImages() {
    const tmpSrc = comparisonImgBottom.src;
    comparisonImgBottom.src = comparisonImgTop.src;
    comparisonImgTop.src = tmpSrc;

    comparisonClipIds.reverse();

    diffCache.clear();
    if (comparisonMode === 'diff') {
        loadDiffImage();
    }

    updateComparisonImageInfo();
}

async function loadDiffImage() {
    const threshold = parseInt(comparisonRange.value);
    const cacheKey = threshold;

    if (diffCache.has(cacheKey)) {
        const cached = diffCache.get(cacheKey);
        comparisonImgDiff.src = cached.dataUrl;
        comparisonSimilarity.textContent = `${(cached.similarity * 100).toFixed(1)}% similar`;
        comparisonSimilarity.classList.remove('hidden');
        return;
    }

    if (comparisonClipIds.length !== 2) return;

    try {
        const result = await window.go.main.App.GetImageDiff(
            comparisonClipIds[0],
            comparisonClipIds[1],
            threshold
        );
        diffCache.set(cacheKey, {
            dataUrl: result.diff_data_url,
            similarity: result.similarity,
        });
        if (comparisonMode === 'diff') {
            comparisonImgDiff.src = result.diff_data_url;
            comparisonSimilarity.textContent = `${(result.similarity * 100).toFixed(1)}% similar`;
            comparisonSimilarity.classList.remove('hidden');
        }
    } catch (error) {
        console.error('Failed to load diff image:', error);
        comparisonSimilarity.textContent = 'Diff failed';
        comparisonSimilarity.classList.remove('hidden');
    }
}

function zoomFit() {
    const viewport = document.querySelector('.comparison-viewport');
    const vw = viewport.clientWidth - 80;
    const vh = viewport.clientHeight - 80;

    // Wait for images to load to get dimensions
    const imgW = Math.max(comparisonImgBottom.naturalWidth || 800, comparisonImgTop.naturalWidth || 800);
    const imgH = Math.max(comparisonImgBottom.naturalHeight || 600, comparisonImgTop.naturalHeight || 600);

    const scale = Math.min(vw / imgW, vh / imgH);
    zoomLevel = Math.min(scale, 1); // Don't upscale past original if it fits
    updateComparisonView();
}

// --- Slider Dragging Logic ---
let isDraggingSlider = false;

function startDragging(e) {
    if (comparisonMode !== 'slider') return;
    isDraggingSlider = true;
    document.addEventListener('mousemove', drag);
    document.addEventListener('mouseup', stopDragging);
    document.addEventListener('touchmove', drag, { passive: false });
    document.addEventListener('touchend', stopDragging);
    drag(e);
}

function drag(e) {
    if (!isDraggingSlider) return;

    const rect = comparisonContainer.getBoundingClientRect();
    const clientX = e.type.includes('touch') ? e.touches[0].clientX : e.clientX;

    let x = (clientX - rect.left) / rect.width;
    x = Math.max(0, Math.min(1, x));

    comparisonRange.value = x * 100;
    updateComparisonView();

    if (e.cancelable) e.preventDefault();
}

function stopDragging() {
    isDraggingSlider = false;
    document.removeEventListener('mousemove', drag);
    document.removeEventListener('mouseup', stopDragging);
    document.removeEventListener('touchmove', drag);
    document.removeEventListener('touchend', stopDragging);
}
