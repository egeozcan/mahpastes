// --- Lightbox Functions ---

// Lightbox image element - managed here, accessed via getLightboxImg()
let lightboxImg = null;

function getLightboxImg() {
    if (!lightboxImg) {
        lightboxImg = document.getElementById('lightbox-img');
    }
    return lightboxImg;
}

// Lightbox zoom/pan state
let lightboxZoom = 1;
let lightboxPanX = 0;
let lightboxPanY = 0;
const lightboxMinZoom = 1;
const lightboxMaxZoom = 4;

// Gesture thresholds and timing constants
const ZOOM_STEP = 0.08;
const ZOOM_SNAP_THRESHOLD = 1.05;
const SWIPE_TIME_THRESHOLD_MS = 300;
const SWIPE_DISTANCE_THRESHOLD_PX = 50;
const NAV_THROTTLE_MS = 70;

// Touch tracking state
let touchStartDistance = 0;
let touchStartZoom = 1;
let touchStartX = 0;
let touchStartY = 0;
let touchStartPanX = 0;
let touchStartPanY = 0;
let lastTouchX = 0;
let lastTouchY = 0;
let isPinching = false;
let swipeStartTime = 0;

// Mouse drag state for panning
let isMouseDragging = false;
let mouseStartX = 0;
let mouseStartY = 0;
let mouseDragStartPanX = 0;
let mouseDragStartPanY = 0;

// Get distance between two touch points
function getTouchDistance(touches) {
    const dx = touches[0].clientX - touches[1].clientX;
    const dy = touches[0].clientY - touches[1].clientY;
    return Math.sqrt(dx * dx + dy * dy);
}

// Apply CSS transform to lightbox image
function updateLightboxTransform() {
    if (!lightboxImg) return;
    lightboxImg.style.transform = `scale(${lightboxZoom}) translate(${lightboxPanX / lightboxZoom}px, ${lightboxPanY / lightboxZoom}px)`;
    // Update cursor based on zoom level (grab when zoomed, default when not)
    if (!isMouseDragging) {
        lightboxImg.style.cursor = lightboxZoom > 1 ? 'grab' : 'default';
    }
}

// Update zoom percentage display and slider
function updateZoomDisplay() {
    const zoomInfo = document.getElementById('lightbox-zoom-info');
    const zoomSlider = document.getElementById('lightbox-zoom-slider');
    if (!zoomInfo || !lightboxImg) return;

    // For SVG or before image loads, show pinch zoom only
    if (!lightboxImg.naturalWidth) {
        const zoomPercent = Math.round(lightboxZoom * 100);
        zoomInfo.textContent = `${zoomPercent}%`;
        if (zoomSlider) zoomSlider.value = zoomPercent;
        return;
    }

    // Calculate effective zoom relative to native dimensions
    const displayedWidth = lightboxImg.offsetWidth;
    const nativeWidth = lightboxImg.naturalWidth;
    const baseScale = displayedWidth / nativeWidth;
    const effectiveZoom = baseScale * lightboxZoom;
    const effectivePercent = Math.round(effectiveZoom * 100);

    zoomInfo.textContent = `${effectivePercent}%`;

    // Update slider to reflect pinch zoom level (1x-4x maps to 100-400)
    if (zoomSlider) {
        zoomSlider.value = Math.round(lightboxZoom * 100);
    }
}

// Handle zoom slider input
function handleLightboxZoomSlider(e) {
    if (!lightbox.classList.contains('active')) return;

    const sliderValue = parseInt(e.target.value, 10);
    let newZoom = sliderValue / 100;

    // Clamp to valid range
    newZoom = Math.max(lightboxMinZoom, Math.min(lightboxMaxZoom, newZoom));
    lightboxZoom = newZoom;

    // Reset pan when zooming back to 1x
    if (lightboxZoom <= ZOOM_SNAP_THRESHOLD) {
        lightboxZoom = 1;
        lightboxPanX = 0;
        lightboxPanY = 0;
    }

    updateLightboxTransform();
    updateZoomDisplay();
}

// Update image info display in bottom bar
function updateLightboxImageInfo() {
    const imageInfo = document.getElementById('lightbox-image-info');
    if (!imageInfo) return;

    const clip = imageClips[currentLightboxIndex];
    if (!clip) {
        imageInfo.textContent = '';
        return;
    }

    const filename = clip.filename || 'Pasted Image';
    const position = `${currentLightboxIndex + 1}/${imageClips.length}`;
    imageInfo.textContent = `${position} · ${filename}`;
}

// Reset zoom state
function resetLightboxZoom() {
    lightboxZoom = 1;
    lightboxPanX = 0;
    lightboxPanY = 0;
    isMouseDragging = false;
    updateLightboxTransform();
    updateZoomDisplay();
    if (lightboxImg) {
        lightboxImg.style.cursor = 'default';
    }
}

// Constrain pan to image bounds
function constrainPan() {
    if (!lightboxImg) return;

    const container = document.querySelector('.lightbox-content');
    if (!container) return;

    const containerRect = container.getBoundingClientRect();
    const scaledWidth = lightboxImg.offsetWidth * lightboxZoom;
    const scaledHeight = lightboxImg.offsetHeight * lightboxZoom;

    const maxPanX = Math.max(0, (scaledWidth - containerRect.width) / 2);
    const maxPanY = Math.max(0, (scaledHeight - containerRect.height) / 2);

    lightboxPanX = Math.max(-maxPanX, Math.min(maxPanX, lightboxPanX));
    lightboxPanY = Math.max(-maxPanY, Math.min(maxPanY, lightboxPanY));
}

// Check if touch target is valid for gestures (not a button or nav)
function isValidTouchTarget(target) {
    if (!target) return false;
    // Reject touches on buttons, nav elements, or the bottom bar
    if (target.tagName === 'BUTTON') return false;
    if (target.closest('button')) return false;
    if (target.closest('.lightbox-nav')) return false;
    if (target.closest('.lightbox-bar')) return false;
    if (target.closest('.lightbox-close')) return false;
    // Allow everything else within the lightbox
    return true;
}

// Touch event handlers
function handleLightboxTouchStart(e) {
    // Only handle if lightbox is active and touch is on valid target
    if (!lightbox.classList.contains('active')) return;
    if (!isValidTouchTarget(e.target)) return;

    if (e.touches.length === 2) {
        // Pinch gesture starting
        isPinching = true;
        touchStartDistance = getTouchDistance(e.touches);
        touchStartZoom = lightboxZoom;
        e.preventDefault();
    } else if (e.touches.length === 1) {
        // Single touch - could be swipe or pan
        isPinching = false;
        const touch = e.touches[0];
        touchStartX = touch.clientX;
        touchStartY = touch.clientY;
        touchStartPanX = lightboxPanX;
        touchStartPanY = lightboxPanY;
        lastTouchX = touch.clientX;
        lastTouchY = touch.clientY;
        swipeStartTime = Date.now();
    }
}

function handleLightboxTouchMove(e) {
    if (!lightbox.classList.contains('active')) return;

    if (e.touches.length === 2 && isPinching) {
        // Pinch to zoom
        e.preventDefault();
        const currentDistance = getTouchDistance(e.touches);
        const scale = currentDistance / touchStartDistance;
        let newZoom = touchStartZoom * scale;

        // Clamp zoom level
        newZoom = Math.max(lightboxMinZoom, Math.min(lightboxMaxZoom, newZoom));
        lightboxZoom = newZoom;

        // Reset pan if zooming back to 1x
        if (lightboxZoom <= ZOOM_SNAP_THRESHOLD) {
            lightboxZoom = 1;
            lightboxPanX = 0;
            lightboxPanY = 0;
        }

        updateLightboxTransform();
        updateZoomDisplay();
    } else if (e.touches.length === 1 && !isPinching) {
        const touch = e.touches[0];
        const deltaX = touch.clientX - lastTouchX;
        const deltaY = touch.clientY - lastTouchY;

        if (lightboxZoom > 1) {
            // Panning zoomed image
            e.preventDefault();
            lightboxPanX += deltaX;
            lightboxPanY += deltaY;
            constrainPan();
            updateLightboxTransform();
        }

        lastTouchX = touch.clientX;
        lastTouchY = touch.clientY;
    }
}

function handleLightboxTouchEnd(e) {
    if (!lightbox.classList.contains('active')) return;

    if (isPinching && e.touches.length < 2) {
        isPinching = false;
        // Snap to 1x if close
        if (lightboxZoom < 1.1) {
            lightboxZoom = 1;
            lightboxPanX = 0;
            lightboxPanY = 0;
            updateLightboxTransform();
            updateZoomDisplay();
        }
        return;
    }

    if (e.touches.length === 0 && !isPinching) {
        const touchEndX = lastTouchX;
        const deltaX = touchEndX - touchStartX;
        const deltaY = lastTouchY - touchStartY;
        const elapsed = Date.now() - swipeStartTime;

        // Detect swipe (only when not zoomed)
        if (lightboxZoom <= ZOOM_SNAP_THRESHOLD && elapsed < SWIPE_TIME_THRESHOLD_MS && Math.abs(deltaX) > SWIPE_DISTANCE_THRESHOLD_PX && Math.abs(deltaX) > Math.abs(deltaY) * 2) {
            if (deltaX > 0) {
                showPrevImage();
            } else {
                showNextImage();
            }
        }
    }
}

function handleLightboxMouseDown(e) {
    if (!lightbox.classList.contains('active')) return;
    if (!isValidTouchTarget(e.target)) return;
    if (lightboxZoom <= 1) return; // Only drag when zoomed

    isMouseDragging = true;
    mouseStartX = e.clientX;
    mouseStartY = e.clientY;
    mouseDragStartPanX = lightboxPanX;
    mouseDragStartPanY = lightboxPanY;
    lightboxImg.style.cursor = 'grabbing';
    e.preventDefault();
}

function handleLightboxMouseMove(e) {
    if (!isMouseDragging) return;

    const deltaX = e.clientX - mouseStartX;
    const deltaY = e.clientY - mouseStartY;

    lightboxPanX = mouseDragStartPanX + deltaX;
    lightboxPanY = mouseDragStartPanY + deltaY;
    constrainPan();
    updateLightboxTransform();
}

function handleLightboxMouseUp() {
    if (!isMouseDragging) return;
    isMouseDragging = false;
    if (lightboxImg) {
        lightboxImg.style.cursor = lightboxZoom > 1 ? 'grab' : 'default';
    }
}

function handleLightboxDoubleClick(e) {
    if (!lightbox.classList.contains('active')) return;
    if (!isValidTouchTarget(e.target)) return;

    e.preventDefault();
    resetLightboxZoom();
}

// Trackpad gesture handler - continuous swipe with momentum detection
let deltaHistory = []; // Track recent deltas to detect momentum decay
let lastNavTime = 0;
let lastSwipeDirection = 0;

function handleLightboxWheel(e) {
    if (!lightbox.classList.contains('active')) return;
    if (e.target.closest('.lightbox-bar') || e.target.closest('button')) return;

    e.preventDefault();

    // Pinch zoom (trackpad pinch sets ctrlKey)
    if (e.ctrlKey) {
        const delta = e.deltaY > 0 ? -ZOOM_STEP : ZOOM_STEP;
        let newZoom = lightboxZoom + delta;
        newZoom = Math.max(lightboxMinZoom, Math.min(lightboxMaxZoom, newZoom));
        lightboxZoom = newZoom;

        if (lightboxZoom <= ZOOM_SNAP_THRESHOLD) {
            lightboxZoom = 1;
            lightboxPanX = 0;
            lightboxPanY = 0;
        }

        updateLightboxTransform();
        updateZoomDisplay();
        return;
    }

    // When zoomed: two-finger scroll pans the image
    if (lightboxZoom > 1) {
        lightboxPanX -= e.deltaX;
        lightboxPanY -= e.deltaY;
        constrainPan();
        updateLightboxTransform();
        return;
    }

    const absX = Math.abs(e.deltaX);
    const absY = Math.abs(e.deltaY);
    const direction = e.deltaX > 0 ? 1 : -1;
    const now = Date.now();

    // Not a horizontal swipe
    if (absX < 3 || absY > absX) {
        deltaHistory = [];
        return;
    }

    // Direction change resets momentum detection
    if (direction !== lastSwipeDirection) {
        deltaHistory = [];
        lastSwipeDirection = direction;
    }

    // Add to history (keep last 4 deltas)
    deltaHistory.push(absX);
    if (deltaHistory.length > 4) {
        deltaHistory.shift();
    }

    // Detect momentum: consistent decay pattern (each delta smaller than previous)
    let isMomentum = false;
    if (deltaHistory.length >= 3) {
        let decayCount = 0;
        for (let i = 1; i < deltaHistory.length; i++) {
            if (deltaHistory[i] <= deltaHistory[i - 1]) {
                decayCount++;
            }
        }
        // If all recent deltas are decaying, it's momentum
        isMomentum = decayCount === deltaHistory.length - 1;
    }

    // Navigate if not momentum and enough time passed
    if (!isMomentum && absX > 2 && now - lastNavTime > NAV_THROTTLE_MS) {
        if (direction > 0) {
            showNextImage();
        } else {
            showPrevImage();
        }
        lastNavTime = now;
    }
}

async function openLightbox(index) {
    if (index < 0 || index >= imageClips.length) return;
    currentLightboxIndex = index;
    const clip = imageClips[index];

    lastFocusedElementBeforeLightbox = document.activeElement;

    // Get or create image element (it gets removed on close)
    const lightboxContent = document.querySelector('.lightbox-content');
    let existingImg = document.getElementById('lightbox-img');

    if (!existingImg) {
        existingImg = document.createElement('img');
        existingImg.id = 'lightbox-img';
        existingImg.className = 'lightbox-image';
        existingImg.draggable = false;
        lightboxContent.insertBefore(existingImg, lightboxContent.firstChild);
    }

    // Update reference (event delegation handles mousedown/dblclick on lightbox-content)
    lightboxImg = existingImg;

    // Reset zoom state
    resetLightboxZoom();

    // Load image data as base64
    try {
        const dataUrl = await getImageDataUrl(clip.id);
        lightboxImg.src = dataUrl;
        // Update zoom display after image loads (use addEventListener for safe composition)
        lightboxImg.addEventListener('load', updateZoomDisplay, { once: true });
    } catch (error) {
        console.error('Failed to load image for lightbox:', error);
        lightboxImg.src = '';
    }

    lightboxImg.alt = escapeHTML(clip.filename) || 'Image preview';
    lightboxCaption.textContent = clip.filename || 'Pasted Image';

    lightbox.classList.add('active');
    updateLightboxNav();
    lightbox.focus();

    // Show AI actions if API key configured
    const lightboxAIActions = document.getElementById('lightbox-ai-actions');
    if (lightboxAIActions) {
        if (typeof falApiKeyConfigured !== 'undefined' && falApiKeyConfigured) {
            lightboxAIActions.classList.remove('hidden');
            // Hide vectorize button for SVGs (can't vectorize vectors)
            const vectorizeBtn = document.getElementById('lightbox-vectorize-btn');
            if (vectorizeBtn) {
                if (clip.content_type === 'image/svg+xml') {
                    vectorizeBtn.classList.add('hidden');
                } else {
                    vectorizeBtn.classList.remove('hidden');
                }
            }
        } else {
            lightboxAIActions.classList.add('hidden');
        }
    }

    // Update image info in bottom bar
    updateLightboxImageInfo();

    // Render plugin buttons
    renderLightboxPluginButtons();
}

function closeLightbox() {
    lightbox.classList.remove('active');
    resetLightboxZoom();
    setTimeout(() => {
        // Remove the image element completely to avoid any residue
        lightboxImg?.parentNode?.removeChild(lightboxImg);
        if (lastFocusedElementBeforeLightbox) {
            lastFocusedElementBeforeLightbox.focus();
        }
    }, 300);
}

function showNextImage() {
    if (currentLightboxIndex < imageClips.length - 1) {
        openLightbox(currentLightboxIndex + 1);
    }
}

function showPrevImage() {
    if (currentLightboxIndex > 0) {
        openLightbox(currentLightboxIndex - 1);
    }
}

function updateLightboxNav() {
    lightboxPrev.style.visibility = currentLightboxIndex > 0 ? 'visible' : 'hidden';
    lightboxNext.style.visibility = currentLightboxIndex < imageClips.length - 1 ? 'visible' : 'hidden';
}

function handleLightboxKeydown(e) {
    if (!lightbox.classList.contains('active')) return;

    if (e.key === 'Escape') {
        closeLightbox();
    } else if (e.key === 'ArrowRight') {
        showNextImage();
    } else if (e.key === 'ArrowLeft') {
        showPrevImage();
    } else if (e.key === 'Tab') {
        // Focus trap logic
        const focusableElements = lightbox.querySelectorAll('button');
        const first = focusableElements[0];
        const last = focusableElements[focusableElements.length - 1];

        if (e.shiftKey && document.activeElement === first) {
            last.focus();
            e.preventDefault();
        } else if (!e.shiftKey && document.activeElement === last) {
            first.focus();
            e.preventDefault();
        }
    }
}

// --- Plugin Buttons in Lightbox ---

// Render plugin buttons in lightbox
async function renderLightboxPluginButtons() {
    const container = document.getElementById('lightbox-plugin-actions');
    if (!container) return;

    container.innerHTML = '';

    if (!pluginUIActions || pluginUIActions.lightbox_buttons.length === 0) {
        container.classList.add('hidden');
        return;
    }

    pluginUIActions.lightbox_buttons.forEach(action => {
        const btn = document.createElement('button');
        btn.className = 'lightbox-plugin-btn';
        btn.dataset.pluginId = action.plugin_id;
        btn.dataset.actionId = action.id;
        btn.dataset.hasOptions = action.options && action.options.length > 0 ? 'true' : 'false';

        const icon = action.icon ? getPluginIcon(action.icon) : '';
        btn.innerHTML = `${icon}<span>${escapeHTML(action.label)}</span>`;

        btn.addEventListener('click', () => handleLightboxPluginAction(action));
        container.appendChild(btn);
    });

    container.classList.remove('hidden');
}

async function handleLightboxPluginAction(action) {
    const clip = imageClips[currentLightboxIndex];
    if (!clip) return;

    if (action.options && action.options.length > 0) {
        // Show options dialog
        openPluginOptionsDialog(action, [clip.id]);
    } else {
        // Execute directly
        await executePluginAction(action.plugin_id, action.id, [clip.id]);
    }
}

// Initialize lightbox gesture listeners (called from app.js after DOM ready)
function initLightboxGestures() {
    const lightboxContent = document.querySelector('.lightbox-content');
    const lightboxZoomSlider = document.getElementById('lightbox-zoom-slider');

    // Set slider min/max from JS constants to avoid drift between HTML and JS
    if (lightboxZoomSlider) {
        lightboxZoomSlider.min = lightboxMinZoom * 100;
        lightboxZoomSlider.max = lightboxMaxZoom * 100;
        lightboxZoomSlider.addEventListener('input', handleLightboxZoomSlider);
    }

    // Touch gestures - attach to backdrop for wider touch area
    lightbox.addEventListener('touchstart', handleLightboxTouchStart, { passive: false });
    lightbox.addEventListener('touchmove', handleLightboxTouchMove, { passive: false });
    lightbox.addEventListener('touchend', handleLightboxTouchEnd);

    // Mouse wheel zoom
    lightbox.addEventListener('wheel', handleLightboxWheel, { passive: false });

    // Event delegation for mousedown/dblclick on lightbox content
    // This avoids attaching/removing listeners when the image is recreated
    if (lightboxContent) {
        lightboxContent.addEventListener('mousedown', (e) => {
            if (e.target.id === 'lightbox-img') {
                handleLightboxMouseDown(e);
            }
        });
        lightboxContent.addEventListener('dblclick', (e) => {
            if (e.target.id === 'lightbox-img') {
                handleLightboxDoubleClick(e);
            }
        });
    }

    // Mouse drag for panning when zoomed (document-level for drag outside image)
    document.addEventListener('mousemove', handleLightboxMouseMove);
    document.addEventListener('mouseup', handleLightboxMouseUp);
}

// --- Comparison Functions ---

async function openComparisonModal() {
    const selectedArray = Array.from(selectedIds);
    if (selectedArray.length !== 2) return;

    lastFocusedElementBeforeComparison = document.activeElement;

    // Load both images as base64
    try {
        const [dataUrl1, dataUrl2] = await Promise.all([
            getImageDataUrl(selectedArray[0]),
            getImageDataUrl(selectedArray[1])
        ]);
        comparisonImgBottom.src = dataUrl1;
        comparisonImgTop.src = dataUrl2;
    } catch (error) {
        console.error('Failed to load images for comparison:', error);
        return;
    }

    // Reset state
    comparisonMode = 'fade';
    zoomLevel = 1;
    isStretched = false;
    comparisonRange.value = 50;

    updateComparisonView();
    comparisonModal.classList.add('active');
    comparisonModal.focus();
}

function closeComparisonModal() {
    comparisonModal.classList.remove('active');
    setTimeout(() => {
        comparisonImgBottom.src = '';
        comparisonImgTop.src = '';
        if (lastFocusedElementBeforeComparison) {
            lastFocusedElementBeforeComparison.focus();
        }
    }, 300);
}

function updateComparisonView() {
    const value = comparisonRange.value;

    // Mode updates
    if (comparisonMode === 'fade') {
        modeFadeBtn.classList.add('bg-white', 'shadow-sm', 'text-blue-600');
        modeFadeBtn.classList.remove('text-gray-500');
        modeSliderBtn.classList.remove('bg-white', 'shadow-sm', 'text-blue-600');
        modeSliderBtn.classList.add('text-gray-500');

        comparisonImgTopWrapper.style.clipPath = 'none';
        comparisonImgTop.style.opacity = value / 100;
        comparisonSliderLine.classList.add('hidden');
        comparisonRangeLabel.textContent = 'Opacity';
    } else {
        modeSliderBtn.classList.add('bg-white', 'shadow-sm', 'text-blue-600');
        modeSliderBtn.classList.remove('text-gray-500');
        modeFadeBtn.classList.remove('bg-white', 'shadow-sm', 'text-blue-600');
        modeFadeBtn.classList.add('text-gray-500');

        comparisonImgTopWrapper.style.clipPath = `inset(0 ${100 - value}% 0 0)`;
        comparisonImgTop.style.opacity = 1;
        comparisonSliderLine.classList.remove('hidden');
        comparisonSliderLine.style.left = `${value}%`;
        comparisonRangeLabel.textContent = 'Position';
    }

    // Alignment & Stretch
    comparisonContainer.style.justifyContent = alignHSelect.value;
    comparisonContainer.style.alignItems = alignVSelect.value;

    if (isStretched) {
        comparisonImgBottom.style.width = '1000px'; // Arbitrary large size
        comparisonImgBottom.style.height = '1000px';
        comparisonImgBottom.style.objectFit = 'fill';
        comparisonImgTop.style.objectFit = 'fill';
        toggleStretchBtn.classList.add('bg-blue-600', 'text-white');
        toggleStretchBtn.classList.remove('bg-gray-100');
    } else {
        comparisonImgBottom.style.width = 'auto';
        comparisonImgBottom.style.height = 'auto';
        comparisonImgBottom.style.objectFit = 'contain';
        comparisonImgTop.style.objectFit = 'contain';
        toggleStretchBtn.classList.remove('bg-blue-600', 'text-white');
        toggleStretchBtn.classList.add('bg-gray-100');
    }

    // Zoom
    comparisonContainer.style.transform = `scale(${zoomLevel})`;
    zoomLevelEl.textContent = `${Math.round(zoomLevel * 100)}%`;
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
