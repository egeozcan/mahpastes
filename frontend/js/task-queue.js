// --- Task Queue Module ---
// Handles plugin task progress events and displays them in a queue bar + modal.

const taskQueue = {
    tasks: new Map(),
    isQueueBarVisible: false
};

// DOM Elements
const queueBar = document.getElementById('task-queue-bar');
const queueBarText = document.getElementById('queue-bar-text');
const queueBarSpinner = document.getElementById('queue-bar-spinner');
const queueBarError = document.getElementById('queue-bar-error');
const queueModal = document.getElementById('queue-modal');
const queueTaskList = document.getElementById('queue-task-list');

// Initialize task queue and event listeners
function initTaskQueue() {
    if (typeof window.runtime !== 'undefined') {
        window.runtime.EventsOn('plugin:task:started', handlePluginTaskStarted);
        window.runtime.EventsOn('plugin:task:progress', handlePluginTaskProgress);
        window.runtime.EventsOn('plugin:task:completed', handlePluginTaskCompleted);
        window.runtime.EventsOn('plugin:task:failed', handlePluginTaskFailed);
    }
}

// Plugin task event handlers
function handlePluginTaskStarted(data) {
    const task = {
        id: data.task_id,
        task_name: data.name,
        status: 'running',
        progress: 0,
        total: data.total,
        plugin_id: data.plugin_id,
        created_at: new Date().toISOString(),
    };
    taskQueue.tasks.set(task.id, task);
    showQueueBar();
    updateQueueBar();
    renderQueueModal();
}

function handlePluginTaskProgress(data) {
    const task = taskQueue.tasks.get(data.task_id);
    if (task) {
        task.progress = data.current;
        task.total = data.total;
        updateQueueBar();
        renderQueueModal();
    }
}

function handlePluginTaskCompleted(data) {
    const task = taskQueue.tasks.get(data.task_id);
    if (task) {
        task.status = 'completed';
        task.progress = task.total;
        updateQueueBar();
        renderQueueModal();

        if (typeof loadClips === 'function') {
            loadClips();
        }
        showToast(`Completed: ${task.task_name}`);
    }
}

function handlePluginTaskFailed(data) {
    const task = taskQueue.tasks.get(data.task_id);
    if (task) {
        task.status = 'failed';
        task.error = data.error;
        updateQueueBar();
        renderQueueModal();
        showToast(`Failed: ${task.task_name}`);
    }
}

// Queue bar functions
function showQueueBar() {
    if (queueBar && !taskQueue.isQueueBarVisible) {
        queueBar.classList.remove('translate-y-full');
        queueBar.classList.add('translate-y-0');
        taskQueue.isQueueBarVisible = true;
        // Add padding to main content so queue bar doesn't cover it
        const main = document.querySelector('main');
        if (main) main.style.paddingBottom = '3rem';
    }
}

function hideQueueBar() {
    if (queueBar && taskQueue.isQueueBarVisible) {
        queueBar.classList.add('translate-y-full');
        queueBar.classList.remove('translate-y-0');
        taskQueue.isQueueBarVisible = false;
        // Remove padding from main content
        const main = document.querySelector('main');
        if (main) main.style.paddingBottom = '';
    }
}

function updateQueueBar() {
    const visibleTasks = getVisibleTasks();
    const runningTasks = visibleTasks.filter(t => t.status === 'running');
    const failedTasks = visibleTasks.filter(t => t.status === 'failed');
    const pendingTasks = visibleTasks.filter(t => t.status === 'pending');

    if (visibleTasks.length === 0) {
        hideQueueBar();
        return;
    }

    showQueueBar();

    // Toggle spinner/error icon based on whether tasks are running
    const isRunning = runningTasks.length > 0 || pendingTasks.length > 0;
    if (queueBarSpinner && queueBarError) {
        if (isRunning) {
            queueBarSpinner.classList.remove('hidden');
            queueBarError.classList.add('hidden');
        } else {
            queueBarSpinner.classList.add('hidden');
            queueBarError.classList.remove('hidden');
        }
    }

    if (queueBarText) {
        if (runningTasks.length > 0) {
            const task = runningTasks[0];
            if (runningTasks.length === 1) {
                queueBarText.textContent = `${task.task_name}: ${task.progress}/${task.total}`;
            } else {
                queueBarText.textContent = `Processing ${runningTasks.length} tasks...`;
            }
        } else if (failedTasks.length > 0) {
            queueBarText.textContent = `${failedTasks.length} failed task${failedTasks.length > 1 ? 's' : ''}`;
        } else if (pendingTasks.length > 0) {
            queueBarText.textContent = `${pendingTasks.length} task${pendingTasks.length > 1 ? 's' : ''} in queue`;
        }
    }
}

function getActiveTasks() {
    return Array.from(taskQueue.tasks.values()).filter(
        t => t.status === 'pending' || t.status === 'running'
    );
}

function getVisibleTasks() {
    // Tasks that should keep the queue bar visible (not completed or cancelled)
    return Array.from(taskQueue.tasks.values()).filter(
        t => t.status === 'pending' || t.status === 'running' || t.status === 'failed'
    );
}

// Queue modal functions
function openQueueModal() {
    if (queueModal) {
        queueModal.classList.remove('opacity-0', 'pointer-events-none');
        queueModal.classList.add('opacity-100');
        const inner = queueModal.querySelector(':scope > div');
        if (inner) {
            inner.classList.remove('scale-95');
            inner.classList.add('scale-100');
        }
        renderQueueModal();
    }
}

function closeQueueModal() {
    if (queueModal) {
        queueModal.classList.add('opacity-0', 'pointer-events-none');
        queueModal.classList.remove('opacity-100');
        const inner = queueModal.querySelector(':scope > div');
        if (inner) {
            inner.classList.add('scale-95');
            inner.classList.remove('scale-100');
        }
    }
}

function renderQueueModal() {
    if (!queueTaskList) return;

    const tasks = Array.from(taskQueue.tasks.values()).sort((a, b) => {
        // Running tasks first, then by creation time (newest first)
        if (a.status === 'running' && b.status !== 'running') return -1;
        if (b.status === 'running' && a.status !== 'running') return 1;
        return new Date(b.created_at) - new Date(a.created_at);
    });

    if (tasks.length === 0) {
        queueTaskList.innerHTML = `
            <div class="text-center py-8 text-stone-400 text-sm">
                No tasks in queue
            </div>
        `;
        return;
    }

    queueTaskList.innerHTML = tasks.map(task => renderTaskCard(task)).join('');
}

function renderTaskCard(task) {
    const statusIcon = getStatusIcon(task.status);
    const progressPercent = task.total > 0 ? (task.progress / task.total) * 100 : 0;
    const showProgress = task.status === 'running';
    const timeAgo = getTimeAgo(task.created_at);

    // Build error details section
    let errorDetailsHtml = '';
    if (task.error) {
        const errorId = `error-${task.id}-fallback`;
        errorDetailsHtml = `
            <div class="mt-2 pt-2 border-t border-stone-200">
                <p id="${errorId}" class="text-xs text-red-500 line-clamp-2 cursor-pointer" onclick="toggleErrorExpand('${errorId}')">${escapeHTML(task.error)}</p>
                <p class="text-xs text-stone-400 mt-1">Click to expand</p>
            </div>
        `;
    }

    return `
        <div class="task-card bg-stone-50 rounded-lg p-3" data-task-id="${task.id}">
            <div class="flex items-start justify-between mb-2">
                <div class="flex items-center gap-2">
                    ${statusIcon}
                    <span class="text-sm font-medium text-stone-700">${escapeHTML(task.task_name)}</span>
                </div>
            </div>

            ${showProgress ? `
                <div class="mb-2">
                    <div class="w-full bg-stone-200 rounded-full h-1.5">
                        <div class="bg-stone-600 h-1.5 rounded-full transition-all" style="width: ${progressPercent}%"></div>
                    </div>
                    <span class="text-xs text-stone-500 mt-1">${task.progress}/${task.total} completed</span>
                </div>
            ` : ''}

            <div class="text-xs text-stone-500">
                <span class="task-status">${getStatusText(task.status)}</span>
                ${task.error ? `<span class="text-red-500"> - ${escapeHTML(task.error)}</span>` : ''}
                <span class="ml-2">${timeAgo}</span>
            </div>
            ${errorDetailsHtml}
        </div>
    `;
}

function getStatusIcon(status) {
    switch (status) {
        case 'running':
            return `<svg class="animate-spin h-4 w-4 text-stone-600" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
            </svg>`;
        case 'completed':
            return `<svg class="w-4 h-4 text-green-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"></path>
            </svg>`;
        case 'cancelled':
            return `<svg class="w-4 h-4 text-stone-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"></path>
            </svg>`;
        case 'failed':
            return `<svg class="w-4 h-4 text-red-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"></path>
            </svg>`;
        default:
            return `<svg class="w-4 h-4 text-stone-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <circle cx="12" cy="12" r="10" stroke-width="2"></circle>
            </svg>`;
    }
}

function getStatusText(status) {
    switch (status) {
        case 'running': return 'Running';
        case 'completed': return 'Completed';
        case 'cancelled': return 'Cancelled';
        case 'failed': return 'Failed';
        case 'pending': return 'Waiting';
        default: return status;
    }
}

function getTimeAgo(dateStr) {
    const date = new Date(dateStr);
    const now = new Date();
    const seconds = Math.floor((now - date) / 1000);

    if (seconds < 60) return 'Just now';
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
    if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
    return `${Math.floor(seconds / 86400)}d ago`;
}

// Toggle error text expansion
function toggleErrorExpand(errorId) {
    const el = document.getElementById(errorId);
    if (el) {
        if (el.classList.contains('line-clamp-2')) {
            el.classList.remove('line-clamp-2');
        } else {
            el.classList.add('line-clamp-2');
        }
    }
}

// Clear completed/failed tasks (local only)
function clearCompletedTasksAction() {
    for (const [id, task] of taskQueue.tasks) {
        if (task.status === 'completed' || task.status === 'cancelled' || task.status === 'failed') {
            taskQueue.tasks.delete(id);
        }
    }
    updateQueueBar();
    renderQueueModal();
}

// Event listeners
if (queueBar) {
    queueBar.addEventListener('click', openQueueModal);
}

document.getElementById('queue-modal-close')?.addEventListener('click', closeQueueModal);
document.getElementById('queue-modal-done')?.addEventListener('click', closeQueueModal);
document.getElementById('clear-completed-btn')?.addEventListener('click', clearCompletedTasksAction);

queueModal?.addEventListener('click', (e) => {
    if (e.target === queueModal) closeQueueModal();
});

// Initialize on load
window.addEventListener('load', initTaskQueue);
