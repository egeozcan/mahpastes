// --- Lightbox Functions ---

async function openLightbox(index) {
    if (index < 0 || index >= imageClips.length) return;
    currentLightboxIndex = index;
    const clip = imageClips[index];

    lastFocusedElementBeforeLightbox = document.activeElement;

    // Load image data as base64
    try {
        const dataUrl = await getImageDataUrl(clip.id);
        lightboxImg.src = dataUrl;
    } catch (error) {
        console.error('Failed to load image for lightbox:', error);
        lightboxImg.src = '';
    }

    lightboxImg.alt = escapeHTML(clip.filename) || 'Image preview';
    lightboxCaption.textContent = clip.filename || 'Pasted Image';

    lightbox.classList.add('active');
    updateLightboxNav();
    lightbox.focus();

    // Screen reader announcement
    const announcement = `Image ${index + 1} of ${imageClips.length}: ${clip.filename || 'Pasted Image'} `;
    showToast(announcement);
}

function closeLightbox() {
    lightbox.classList.remove('active');
    setTimeout(() => {
        lightboxImg.src = '';
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
