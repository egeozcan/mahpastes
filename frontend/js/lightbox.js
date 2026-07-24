(() => {
    function createLightboxController(deps) {
        const MAX_SCALE = 8;
        const ZOOM_FACTOR = 1.2;
        const SLIDER_STEPS = 100;
        const WHEEL_THRESHOLD = 56;
        const WHEEL_QUIET_MS = 180;
        const EPSILON = 0.0001;

        const state = {
            phase: 'closed',
            clips: [],
            currentId: null,
            opener: null,
            openerIndex: -1,
            focusTrapCleanup: null,
            requestGeneration: 0,
            naturalWidth: 0,
            naturalHeight: 0,
            viewportWidth: 0,
            viewportHeight: 0,
            fitScale: 1,
            scale: 1,
            panX: 0,
            panY: 0,
            pointerId: null,
            dragStartX: 0,
            dragStartY: 0,
            dragStartPanX: 0,
            dragStartPanY: 0,
            touchMode: null,
            pinchLastDistance: 0,
            lastTouchX: 0,
            lastTouchY: 0,
            swipeStartX: 0,
            swipeStartY: 0,
            swipeStartedAt: 0,
            wheelX: 0,
            wheelY: 0,
            wheelDirection: 0,
            wheelLocked: false,
            wheelQuietTimer: null,
        };

        const backgroundInert = new Map();

        function idsEqual(first, second) {
            return String(first) === String(second);
        }

        function currentClip() {
            return state.clips.find(clip => idsEqual(clip.id, state.currentId)) || null;
        }

        function currentIndex() {
            return state.clips.findIndex(clip => idsEqual(clip.id, state.currentId));
        }

        function renderBoundaryState() {
            const index = currentIndex();
            const hasPrevious = index > 0;
            const hasNext = index >= 0 && index < state.clips.length - 1;
            deps.elements.previous.hidden = !hasPrevious;
            deps.elements.previous.disabled = !hasPrevious;
            deps.elements.next.hidden = !hasNext;
            deps.elements.next.disabled = !hasNext;
        }

        function renderImageInfo(clip, image = deps.elements.image) {
            if (!deps.elements.imageInfo) return;
            if (!clip) {
                deps.elements.imageInfo.textContent = '';
                return;
            }
            const filename = clip.filename || 'Pasted Image';
            const position = `${currentIndex() + 1}/${state.clips.length}`;
            const resolution = image?.naturalWidth && image?.naturalHeight
                ? `${image.naturalWidth}×${image.naturalHeight}`
                : '';
            deps.elements.imageInfo.textContent = resolution
                ? `${position} · ${filename} · ${resolution}`
                : `${position} · ${filename}`;
        }

        function setBackgroundInert(enabled) {
            for (const element of deps.backgroundRoots || []) {
                if (enabled) {
                    if (!backgroundInert.has(element)) {
                        backgroundInert.set(element, element.hasAttribute('inert'));
                    }
                    element.setAttribute('inert', '');
                } else if (!backgroundInert.get(element)) {
                    element.removeAttribute('inert');
                }
            }
            if (!enabled) backgroundInert.clear();
        }

        function computeFitScale() {
            if (!state.naturalWidth || !state.naturalHeight || !state.viewportWidth || !state.viewportHeight) {
                return 1;
            }
            const widthScale = state.viewportWidth / state.naturalWidth;
            const heightScale = state.viewportHeight / state.naturalHeight;
            return Math.min(1, widthScale, heightScale);
        }

        function sliderPositionToScale(position) {
            const fitScale = Math.max(Number.EPSILON, state.fitScale);
            if (fitScale >= MAX_SCALE) return MAX_SCALE;
            return fitScale * Math.pow(MAX_SCALE / fitScale, Number(position) / SLIDER_STEPS);
        }

        function scaleToSliderPosition(scale) {
            const fitScale = Math.max(Number.EPSILON, state.fitScale);
            if (fitScale >= MAX_SCALE) return SLIDER_STEPS;
            const position = SLIDER_STEPS * Math.log(scale / fitScale) / Math.log(MAX_SCALE / fitScale);
            return Math.max(0, Math.min(SLIDER_STEPS, position));
        }

        function isPannable() {
            if (state.scale <= state.fitScale + EPSILON) return false;
            return state.naturalWidth * state.scale > state.viewportWidth + 0.5 ||
                state.naturalHeight * state.scale > state.viewportHeight + 0.5;
        }

        function constrainPan() {
            const maxX = Math.max(0, (state.naturalWidth * state.scale - state.viewportWidth) / 2);
            const maxY = Math.max(0, (state.naturalHeight * state.scale - state.viewportHeight) / 2);
            state.panX = Math.max(-maxX, Math.min(maxX, state.panX));
            state.panY = Math.max(-maxY, Math.min(maxY, state.panY));
        }

        function applyViewport() {
            deps.elements.panLayer.style.transform = `translate(-50%, -50%) translate(${state.panX}px, ${state.panY}px)`;
            deps.elements.image.style.transform = `scale(${state.scale})`;
            deps.elements.viewport.dataset.pannable = String(isPannable());

            const actualPercent = Math.round(state.scale * 100);
            const fitPercent = Math.round(state.fitScale * 100);
            deps.elements.zoomInfo.textContent = `${actualPercent}%`;
            deps.elements.slider.value = String(Math.round(scaleToSliderPosition(state.scale)));
            deps.elements.slider.setAttribute('aria-valuetext', `${actualPercent}% actual; Fit is ${fitPercent}%`);
            deps.elements.fit.setAttribute('aria-pressed', String(Math.abs(state.scale - state.fitScale) < EPSILON));
            deps.elements.actual.setAttribute('aria-pressed', String(Math.abs(state.scale - 1) < EPSILON));
        }

        function resetViewport() {
            state.naturalWidth = 0;
            state.naturalHeight = 0;
            state.fitScale = 1;
            state.scale = 1;
            state.panX = 0;
            state.panY = 0;
            const rect = deps.elements.viewport.getBoundingClientRect();
            state.viewportWidth = rect.width;
            state.viewportHeight = rect.height;
            applyViewport();
        }

        function resetWheelNavigation() {
            state.wheelX = 0;
            state.wheelY = 0;
            state.wheelDirection = 0;
            state.wheelLocked = false;
            clearTimeout(state.wheelQuietTimer);
            state.wheelQuietTimer = null;
        }

        function scheduleWheelReset() {
            clearTimeout(state.wheelQuietTimer);
            state.wheelQuietTimer = setTimeout(resetWheelNavigation, WHEEL_QUIET_MS);
        }

        function zoomAt(nextScale, focalX, focalY) {
            if (!state.naturalWidth || !state.naturalHeight) return;
            const clamped = Math.max(state.fitScale, Math.min(MAX_SCALE, nextScale));
            const centerX = state.viewportWidth / 2;
            const centerY = state.viewportHeight / 2;
            const imageX = (focalX - centerX - state.panX) / state.scale;
            const imageY = (focalY - centerY - state.panY) / state.scale;
            state.panX = focalX - centerX - imageX * clamped;
            state.panY = focalY - centerY - imageY * clamped;
            state.scale = clamped;
            constrainPan();
            applyViewport();
            if (state.scale > state.fitScale + EPSILON) resetWheelNavigation();
        }

        function initializeViewport(image) {
            state.naturalWidth = image.naturalWidth;
            state.naturalHeight = image.naturalHeight;
            const rect = deps.elements.viewport.getBoundingClientRect();
            state.viewportWidth = rect.width;
            state.viewportHeight = rect.height;
            state.fitScale = computeFitScale();
            state.scale = state.fitScale;
            state.panX = 0;
            state.panY = 0;
            applyViewport();
        }

        function showLoadError(error, clip, generation) {
            if (generation !== state.requestGeneration || state.phase === 'closed' || !idsEqual(state.currentId, clip.id)) return;
            state.phase = 'error';
            deps.elements.viewport.setAttribute('aria-busy', 'false');
            deps.elements.loading.hidden = true;
            deps.elements.error.hidden = false;
            deps.elements.image.hidden = true;
            deps.elements.status.textContent = `Failed to load ${clip.filename || 'image'}`;
            deps.reportError(error, clip);
        }

        async function renderCurrent() {
            const clip = currentClip();
            if (!clip || state.phase === 'closed') return;

            const generation = ++state.requestGeneration;
            state.phase = 'loading';
            deps.closeMenus();
            resetViewport();
            deps.elements.viewport.setAttribute('aria-busy', 'true');
            deps.elements.loading.hidden = false;
            deps.elements.error.hidden = true;
            deps.elements.image.hidden = true;
            deps.elements.caption.textContent = clip.filename || 'Pasted Image';
            deps.elements.image.alt = clip.filename || 'Image preview';
            renderBoundaryState();
            renderImageInfo(clip, null);
            deps.renderPluginActions(clip);
            deps.renderFileActions(clip);

            try {
                const dataURL = await deps.loadImage(clip);
                if (generation !== state.requestGeneration || state.phase === 'closed' || !idsEqual(state.currentId, clip.id)) return;

                let committed = false;
                const commitReady = () => {
                    if (committed || generation !== state.requestGeneration || state.phase === 'closed' || !idsEqual(state.currentId, clip.id)) return;
                    committed = true;
                    state.phase = 'ready';
                    deps.elements.viewport.setAttribute('aria-busy', 'false');
                    deps.elements.loading.hidden = true;
                    deps.elements.error.hidden = true;
                    deps.elements.image.hidden = false;
                    initializeViewport(deps.elements.image);
                    renderImageInfo(clip);
                    deps.elements.status.textContent = `${clip.filename || 'Image'}, ${currentIndex() + 1} of ${state.clips.length}, ${Math.round(state.scale * 100)}%`;
                };
                const commitError = () => showLoadError(new Error('Image decode failed'), clip, generation);

                deps.elements.image.addEventListener('load', commitReady, { once: true });
                deps.elements.image.addEventListener('error', commitError, { once: true });
                deps.elements.image.src = dataURL;
                if (deps.elements.image.complete && deps.elements.image.naturalWidth > 0) {
                    queueMicrotask(commitReady);
                }
            } catch (error) {
                showLoadError(error, clip, generation);
            }
        }

        async function open({ clips, currentId, opener }) {
            if (!Array.isArray(clips) || clips.length === 0) return;
            const wasClosed = state.phase === 'closed';
            state.clips = [...clips];
            state.currentId = currentId;
            if (!currentClip()) state.currentId = state.clips[0].id;

            if (wasClosed) {
                state.opener = opener || document.activeElement;
                state.openerIndex = deps.getOpenerIndex ? deps.getOpenerIndex(state.opener) : -1;
                setBackgroundInert(true);
            }

            resetWheelNavigation();
            state.phase = 'loading';
            deps.elements.root.removeAttribute('inert');
            deps.elements.root.classList.add('active');
            if (wasClosed) {
                if (state.focusTrapCleanup) state.focusTrapCleanup();
                state.focusTrapCleanup = deps.trapFocus(deps.elements.root);
                deps.elements.close.focus();
            }
            await renderCurrent();
        }

        function openSingle({ clip, opener }) {
            return open({ clips: [clip], currentId: clip.id, opener });
        }

        function setClips(clips) {
            const previousIndex = currentIndex();
            state.clips = [...clips];
            if (state.phase === 'closed') return;
            if (state.clips.length === 0) {
                close();
                return;
            }
            if (!currentClip()) {
                const nextIndex = Math.max(0, Math.min(previousIndex, state.clips.length - 1));
                state.currentId = state.clips[nextIndex].id;
                void renderCurrent();
            } else {
                renderBoundaryState();
                renderImageInfo(currentClip());
            }
        }

        function navigate(delta) {
            const index = currentIndex();
            const target = state.clips[index + delta];
            if (!target) return;
            state.currentId = target.id;
            void renderCurrent();
        }

        function command(name, detail = {}) {
            if (name === 'close') {
                close();
                return;
            }
            if (name === 'next') {
                navigate(1);
                return;
            }
            if (name === 'previous') {
                navigate(-1);
                return;
            }
            if (name === 'retry') {
                if (state.phase === 'error') void renderCurrent();
                return;
            }

            const clip = currentClip();
            if (name === 'edit') {
                if (clip) deps.editClip(clip);
                return;
            }
            if (name === 'copy') {
                if (clip) deps.copyClip(clip);
                return;
            }
            if (state.phase !== 'ready') return;

            const centerX = state.viewportWidth / 2;
            const centerY = state.viewportHeight / 2;
            if (name === 'zoom-in') zoomAt(state.scale * ZOOM_FACTOR, centerX, centerY);
            else if (name === 'zoom-out') zoomAt(state.scale / ZOOM_FACTOR, centerX, centerY);
            else if (name === 'fit') zoomAt(state.fitScale, centerX, centerY);
            else if (name === 'actual-size') zoomAt(1, centerX, centerY);
            else if (name === 'slider') zoomAt(sliderPositionToScale(Number(detail.position)), centerX, centerY);
            else if (name === 'pan') {
                state.panX += Number(detail.dx) || 0;
                state.panY += Number(detail.dy) || 0;
                constrainPan();
                applyViewport();
            }
        }

        function close() {
            if (state.phase === 'closed') return;
            state.phase = 'closed';
            state.requestGeneration++;
            resetWheelNavigation();
            if (state.focusTrapCleanup) state.focusTrapCleanup();
            state.focusTrapCleanup = null;
            deps.closeMenus();
            deps.elements.root.classList.remove('active');
            deps.elements.root.setAttribute('inert', '');
            deps.elements.viewport.setAttribute('aria-busy', 'false');
            deps.elements.loading.hidden = true;
            deps.elements.error.hidden = true;
            resetViewport();
            setBackgroundInert(false);

            const opener = state.opener;
            const openerIndex = state.openerIndex;
            state.opener = null;
            state.openerIndex = -1;
            deps.restoreFocus(opener, openerIndex);
        }

        function handleWheelNavigation(event) {
            event.preventDefault();
            scheduleWheelReset();
            if (state.wheelLocked) return;

            const eventDominant = Math.abs(event.deltaX) >= Math.abs(event.deltaY) ? event.deltaX : event.deltaY;
            const direction = Math.sign(eventDominant);
            if (state.wheelDirection && direction && direction !== state.wheelDirection) {
                state.wheelX = 0;
                state.wheelY = 0;
            }
            if (direction) state.wheelDirection = direction;
            state.wheelX += event.deltaX;
            state.wheelY += event.deltaY;
            const dominant = Math.abs(state.wheelX) >= Math.abs(state.wheelY) ? state.wheelX : state.wheelY;
            if (Math.abs(dominant) < WHEEL_THRESHOLD) return;
            command(dominant > 0 ? 'next' : 'previous');
            state.wheelLocked = true;
        }

        function onWheel(event) {
            if (state.phase !== 'ready') {
                if (state.wheelLocked && !event.ctrlKey && !event.metaKey) {
                    event.preventDefault();
                    scheduleWheelReset();
                }
                return;
            }
            const rect = deps.elements.viewport.getBoundingClientRect();
            if (event.ctrlKey || event.metaKey) {
                event.preventDefault();
                const factor = Math.exp(-event.deltaY * 0.002);
                zoomAt(state.scale * factor, event.clientX - rect.left, event.clientY - rect.top);
                return;
            }
            if (state.scale > state.fitScale + EPSILON) {
                event.preventDefault();
                state.panX -= event.deltaX;
                state.panY -= event.deltaY;
                constrainPan();
                applyViewport();
                return;
            }
            handleWheelNavigation(event);
        }

        function onPointerDown(event) {
            if (event.pointerType === 'touch' || event.button !== 0 || !isPannable()) return;
            if (event.target.closest('button')) return;
            state.pointerId = event.pointerId;
            state.dragStartX = event.clientX;
            state.dragStartY = event.clientY;
            state.dragStartPanX = state.panX;
            state.dragStartPanY = state.panY;
            deps.elements.viewport.setPointerCapture(event.pointerId);
            deps.elements.viewport.classList.add('is-panning');
            event.preventDefault();
        }

        function onPointerMove(event) {
            if (event.pointerId !== state.pointerId) return;
            state.panX = state.dragStartPanX + event.clientX - state.dragStartX;
            state.panY = state.dragStartPanY + event.clientY - state.dragStartY;
            constrainPan();
            applyViewport();
        }

        function endPointerPan(event) {
            if (event.pointerId !== state.pointerId) return;
            if (deps.elements.viewport.hasPointerCapture(event.pointerId)) {
                deps.elements.viewport.releasePointerCapture(event.pointerId);
            }
            state.pointerId = null;
            deps.elements.viewport.classList.remove('is-panning');
        }

        function onDoubleClick(event) {
            if (state.phase !== 'ready' || event.target.closest('button')) return;
            const rect = deps.elements.viewport.getBoundingClientRect();
            const focalX = event.clientX - rect.left;
            const focalY = event.clientY - rect.top;
            const target = Math.abs(state.scale - state.fitScale) < EPSILON ? 1 : state.fitScale;
            zoomAt(target, focalX, focalY);
            event.preventDefault();
        }

        function touchGeometry(touches) {
            const first = touches[0];
            const second = touches[1];
            const dx = second.clientX - first.clientX;
            const dy = second.clientY - first.clientY;
            return {
                distance: Math.hypot(dx, dy),
                clientX: (first.clientX + second.clientX) / 2,
                clientY: (first.clientY + second.clientY) / 2,
            };
        }

        function onTouchStart(event) {
            if (state.phase !== 'ready' || event.target.closest('button')) return;
            if (event.touches.length === 2) {
                const geometry = touchGeometry(event.touches);
                state.touchMode = 'pinch';
                state.pinchLastDistance = geometry.distance;
                event.preventDefault();
                return;
            }
            if (event.touches.length === 1) {
                const touch = event.touches[0];
                state.touchMode = isPannable()
                    ? 'pan'
                    : (Math.abs(state.scale - state.fitScale) < EPSILON ? 'swipe' : null);
                state.lastTouchX = touch.clientX;
                state.lastTouchY = touch.clientY;
                state.swipeStartX = touch.clientX;
                state.swipeStartY = touch.clientY;
                state.swipeStartedAt = performance.now();
            }
        }

        function onTouchMove(event) {
            if (event.touches.length === 2 && state.touchMode === 'pinch') {
                const geometry = touchGeometry(event.touches);
                const rect = deps.elements.viewport.getBoundingClientRect();
                if (state.pinchLastDistance > 0) {
                    zoomAt(
                        state.scale * geometry.distance / state.pinchLastDistance,
                        geometry.clientX - rect.left,
                        geometry.clientY - rect.top
                    );
                }
                state.pinchLastDistance = geometry.distance;
                event.preventDefault();
                return;
            }
            if (event.touches.length !== 1) return;
            const touch = event.touches[0];
            if (state.touchMode === 'pan') {
                state.panX += touch.clientX - state.lastTouchX;
                state.panY += touch.clientY - state.lastTouchY;
                constrainPan();
                applyViewport();
                event.preventDefault();
            }
            state.lastTouchX = touch.clientX;
            state.lastTouchY = touch.clientY;
        }

        function onTouchEnd(event) {
            if (event.touches.length > 0) return;
            if (state.touchMode === 'swipe') {
                const deltaX = state.lastTouchX - state.swipeStartX;
                const deltaY = state.lastTouchY - state.swipeStartY;
                const elapsed = performance.now() - state.swipeStartedAt;
                if (elapsed < 350 && Math.abs(deltaX) > 50 && Math.abs(deltaX) > Math.abs(deltaY) * 2) {
                    command(deltaX < 0 ? 'next' : 'previous');
                }
            }
            state.touchMode = null;
            state.pinchLastDistance = 0;
        }

        deps.elements.zoomOut.addEventListener('click', () => command('zoom-out'));
        deps.elements.zoomIn.addEventListener('click', () => command('zoom-in'));
        deps.elements.fit.addEventListener('click', () => command('fit'));
        deps.elements.actual.addEventListener('click', () => command('actual-size'));
        deps.elements.retry.addEventListener('click', () => command('retry'));
        deps.elements.slider.addEventListener('input', event => command('slider', { position: event.target.value }));
        deps.elements.slider.addEventListener('keydown', event => {
            if (!['Home', 'End', 'PageUp', 'PageDown'].includes(event.key)) return;
            event.preventDefault();
            let position = Number(deps.elements.slider.value);
            if (event.key === 'Home') position = 0;
            else if (event.key === 'End') position = SLIDER_STEPS;
            else if (event.key === 'PageUp') position = Math.min(SLIDER_STEPS, position + 10);
            else position = Math.max(0, position - 10);
            deps.elements.slider.value = String(position);
            command('slider', { position });
        });

        deps.elements.viewport.addEventListener('wheel', onWheel, { passive: false });
        deps.elements.viewport.addEventListener('pointerdown', onPointerDown);
        deps.elements.viewport.addEventListener('pointermove', onPointerMove);
        deps.elements.viewport.addEventListener('pointerup', endPointerPan);
        deps.elements.viewport.addEventListener('pointercancel', endPointerPan);
        deps.elements.viewport.addEventListener('dblclick', onDoubleClick);
        deps.elements.viewport.addEventListener('touchstart', onTouchStart, { passive: false });
        deps.elements.viewport.addEventListener('touchmove', onTouchMove, { passive: false });
        deps.elements.viewport.addEventListener('touchend', onTouchEnd);
        deps.elements.viewport.addEventListener('touchcancel', onTouchEnd);

        const resizeObserver = new ResizeObserver(() => {
            if (!state.naturalWidth || !state.naturalHeight) return;
            const wasFit = Math.abs(state.scale - state.fitScale) < EPSILON;
            const rect = deps.elements.viewport.getBoundingClientRect();
            state.viewportWidth = rect.width;
            state.viewportHeight = rect.height;
            state.fitScale = computeFitScale();
            if (wasFit) {
                state.scale = state.fitScale;
                state.panX = 0;
                state.panY = 0;
            } else {
                state.scale = Math.max(state.fitScale, state.scale);
                constrainPan();
            }
            applyViewport();
        });
        resizeObserver.observe(deps.elements.viewport);

        resetViewport();

        return Object.freeze({ open, openSingle, setClips, command, close });
    }

    window.createLightboxController = createLightboxController;
})();
