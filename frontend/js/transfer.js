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
    preparedDragItems.clear();
    pendingDragPrep.clear();
    pendingDragLookup.clear();
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

    const lookupPromise = (async () => {
        const service = window.go?.main?.TransferService;
        if (!service || typeof service.GetExistingPreparedClipForTransfer !== 'function') {
            return null;
        }

        const prepared = await service.GetExistingPreparedClipForTransfer({
            clip_id: id,
            channel: 'drag_out'
        });

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
        pendingDragLookup.delete(id);
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

    const prepPromise = (async () => {
        const service = window.go?.main?.TransferService;
        if (!service || typeof service.PrepareClipForTransfer !== 'function') {
            throw new Error('Transfer service binding is unavailable');
        }

        const prepared = await service.PrepareClipForTransfer({
            clip_id: id,
            channel: 'drag_out'
        });

        preparedDragItems.set(id, prepared);
        return prepared;
    })();

    pendingDragPrep.set(id, prepPromise);

    try {
        return await prepPromise;
    } finally {
        pendingDragPrep.delete(id);
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
    getTransferCapabilitiesForTest: () => JSON.parse(JSON.stringify(getEffectiveTransferCapabilities())),
    setTransferCapabilitiesForTest: (caps) => {
        transferCapabilitiesOverride = caps;
    },
    clearTransferCapabilitiesForTest: () => {
        transferCapabilitiesOverride = null;
    }
});
