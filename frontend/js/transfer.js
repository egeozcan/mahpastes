let transferCapabilities = {
    drag_out: {
        enabled: false,
        strategy: '',
        reason: 'Transfer service is unavailable',
        native_drag: false
    },
    clipboard_file: false
};

let transferCapabilitiesOverride = null;
const preparedDragItems = new Map();
const pendingDragPrep = new Map();
const pendingDragLookup = new Map();
// Bumped whenever cached drag state is thrown away (per clip, or all at
// once). A prepare or lookup already in flight captured the old value and
// must not write its now-stale result back into the cache.
const dragItemEpochs = new Map();
let dragGlobalEpoch = 0;

function dragEpochFor(id) {
    return `${dragGlobalEpoch}:${dragItemEpochs.get(id) || 0}`;
}

function parseLeaseExpiry(item) {
    if (!item || !item.lease_expires_at) {
        return NaN;
    }
    return Date.parse(item.lease_expires_at);
}

function pruneExpiredPreparedDragItems() {
    const now = Date.now();
    let changed = false;
    for (const [id, item] of preparedDragItems.entries()) {
        const expiresAt = parseLeaseExpiry(item);
        if (Number.isFinite(expiresAt) && expiresAt <= now) {
            preparedDragItems.delete(id);
            changed = true;
        }
    }
    if (changed && typeof resetDragHandleStates === 'function') {
        resetDragHandleStates();
    }
}

function getEffectiveTransferCapabilities() {
    return transferCapabilitiesOverride || transferCapabilities;
}

async function initTransferCapabilities() {
    try {
        const service = window.go?.main?.TransferService;
        if (!service || typeof service.GetTransferCapabilities !== 'function') {
            throw new Error('Transfer service binding is unavailable');
        }
        transferCapabilities = await service.GetTransferCapabilities();
    } catch (error) {
        console.error('Failed to load transfer capabilities:', error);
        transferCapabilities = {
            drag_out: {
                enabled: false,
                strategy: '',
                reason: 'Transfer capability lookup failed',
                native_drag: false
            },
            clipboard_file: false
        };
    }
    return getEffectiveTransferCapabilities();
}

function canDragOut() {
    return !!getEffectiveTransferCapabilities().drag_out?.enabled;
}

function canUseNativeDragOut() {
    const caps = getEffectiveTransferCapabilities();
    if (!caps.drag_out?.enabled || !caps.drag_out?.native_drag) {
        return false;
    }
    const service = window.go?.main?.TransferService;
    return !!service && typeof service.StartNativeDragOut === 'function';
}

function getDragStrategy() {
    return getEffectiveTransferCapabilities().drag_out?.strategy || '';
}

function getPreparedDragItem(clipId) {
    const id = Number(clipId);
    const item = preparedDragItems.get(id);
    if (!item) {
        return undefined;
    }
    const expiresAt = parseLeaseExpiry(item);
    if (Number.isFinite(expiresAt) && expiresAt <= Date.now()) {
        preparedDragItems.delete(id);
        if (typeof resetDragHandleStates === 'function') {
            resetDragHandleStates();
        }
        return undefined;
    }
    return item;
}

function clearPreparedDragState() {
    dragGlobalEpoch++;
    dragItemEpochs.clear();
    preparedDragItems.clear();
    pendingDragPrep.clear();
    pendingDragLookup.clear();
    if (typeof resetDragHandleStates === 'function') {
        resetDragHandleStates();
    }
}

// Forgets one clip's prepared drag item (its filename, transfer URL) after
// the clip changed in place — a rename patched into its card, say — without
// the gallery reload that would otherwise have cleared everything.
function invalidatePreparedDragItem(clipId) {
    const id = Number(clipId);
    if (!Number.isFinite(id) || id <= 0) {
        return;
    }
    dragItemEpochs.set(id, (dragItemEpochs.get(id) || 0) + 1);
    preparedDragItems.delete(id);
    pendingDragPrep.delete(id);
    pendingDragLookup.delete(id);
    if (typeof resetDragHandleStates === 'function') {
        resetDragHandleStates();
    }
}

async function startNativeDrag(clipId, preparedItem) {
    const id = Number(clipId);
    if (!Number.isFinite(id) || id <= 0) {
        throw new Error(`Invalid clip ID: ${clipId}`);
    }

    const service = window.go?.main?.TransferService;
    if (!service || typeof service.StartNativeDragOut !== 'function') {
        return false;
    }

    const prepared = preparedItem || getPreparedDragItem(id);
    try {
        return !!(await service.StartNativeDragOut({
            clip_id: id,
            abs_path: prepared?.abs_path || ''
        }));
    } catch (error) {
        console.error(`Failed to start native drag for clip ${id}:`, error);
        return false;
    }
}

async function lookupPreparedDrag(clipId) {
    const id = Number(clipId);
    if (!Number.isFinite(id) || id <= 0) {
        throw new Error(`Invalid clip ID: ${clipId}`);
    }

    const cached = getPreparedDragItem(id);
    if (cached) {
        return cached;
    }

    const pending = pendingDragLookup.get(id);
    if (pending) {
        return pending;
    }

    const epoch = dragEpochFor(id);
    const lookupPromise = (async () => {
        const service = window.go?.main?.TransferService;
        if (!service || typeof service.GetExistingPreparedClipForTransfer !== 'function') {
            return null;
        }

        const prepared = await service.GetExistingPreparedClipForTransfer({
            clip_id: id,
            channel: 'drag_out'
        });

        if (dragEpochFor(id) !== epoch) {
            // Invalidated while in flight: the answer describes the clip as
            // it was. Report nothing cached; a prepare fetches it afresh.
            return null;
        }
        if (prepared && prepared.abs_path) {
            preparedDragItems.set(id, prepared);
            return prepared;
        }

        preparedDragItems.delete(id);
        return null;
    })();

    pendingDragLookup.set(id, lookupPromise);
    try {
        return await lookupPromise;
    } finally {
        if (pendingDragLookup.get(id) === lookupPromise) {
            pendingDragLookup.delete(id);
        }
    }
}

async function prepareDrag(clipId) {
    const id = Number(clipId);
    if (!Number.isFinite(id) || id <= 0) {
        throw new Error(`Invalid clip ID: ${clipId}`);
    }

    const cached = preparedDragItems.get(id);
    if (cached) {
        return cached;
    }

    const pending = pendingDragPrep.get(id);
    if (pending) {
        return pending;
    }

    const epoch = dragEpochFor(id);
    const prepPromise = (async () => {
        const service = window.go?.main?.TransferService;
        if (!service || typeof service.PrepareClipForTransfer !== 'function') {
            throw new Error('Transfer service binding is unavailable');
        }

        const prepared = await service.PrepareClipForTransfer({
            clip_id: id,
            channel: 'drag_out'
        });

        if (dragEpochFor(id) !== epoch) {
            // Invalidated while in flight (the clip was renamed, say): this
            // result may carry the old filename. Prepare again rather than
            // hand a waiting drag, or the cache, a stale item.
            return prepareDrag(id);
        }
        preparedDragItems.set(id, prepared);
        return prepared;
    })();

    pendingDragPrep.set(id, prepPromise);

    try {
        return await prepPromise;
    } finally {
        if (pendingDragPrep.get(id) === prepPromise) {
            pendingDragPrep.delete(id);
        }
    }
}

function setDragData(dataTransfer, preparedItem, strategy) {
    const effectiveStrategy = strategy || getDragStrategy();
    return setDragDataForStrategy(dataTransfer, preparedItem, effectiveStrategy);
}

setInterval(pruneExpiredPreparedDragItems, 30000);

Object.assign(window.__testHelpers, {
    prepareDragForTest: (clipId) => prepareDrag(clipId),
    lookupPreparedDragForTest: (clipId) => lookupPreparedDrag(clipId),
    getPreparedDragItemForTest: (clipId) => getPreparedDragItem(clipId),
    startNativeDragForTest: (clipId, preparedItem) => startNativeDrag(clipId, preparedItem),
    clearPreparedDragStateForTest: () => clearPreparedDragState(),
    invalidatePreparedDragItemForTest: (clipId) => invalidatePreparedDragItem(clipId),
    getTransferCapabilitiesForTest: () => JSON.parse(JSON.stringify(getEffectiveTransferCapabilities())),
    setTransferCapabilitiesForTest: (caps) => {
        transferCapabilitiesOverride = caps;
    },
    clearTransferCapabilitiesForTest: () => {
        transferCapabilitiesOverride = null;
    }
});
