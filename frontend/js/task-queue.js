// --- Task Queue Module ---

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
        window.runtime.EventsOn('task:started', handleTaskStarted);
        window.runtime.EventsOn('task:progress', handleTaskProgress);
        window.runtime.EventsOn('task:completed', handleTaskCompleted);
        window.runtime.EventsOn('task:cancelled', handleTaskCancelled);
        window.runtime.EventsOn('task:failed', handleTaskFailed);
    }

    // Load existing tasks on startup
    loadExistingTasks();
}

async function loadExistingTasks() {
    try {
        const tasks = await window.go.main.App.GetTasks();
        if (tasks && tasks.length > 0) {
            tasks.forEach(task => {
                taskQueue.tasks.set(task.id, task);
            });
            updateQueueBar();
        }
    } catch (error) {
        console.error('Failed to load existing tasks:', error);
    }
}

// Event handlers
function handleTaskStarted(task) {
    taskQueue.tasks.set(task.id, task);
    showQueueBar();
    updateQueueBar();
    renderQueueModal();
}

function handleTaskProgress(data) {
    const task = taskQueue.tasks.get(data.taskId);
    if (task) {
        task.progress = data.progress;
        task.total = data.total;
        updateQueueBar();
        renderQueueModal();
    }
}

function handleTaskCompleted(task) {
    taskQueue.tasks.set(task.id, task);
    updateQueueBar();
    renderQueueModal();

    // Refresh gallery when task completes
    if (typeof loadClips === 'function') {
        loadClips();
    }

    // Show toast with appropriate message
    const successCount = task.results ? task.results.filter(r => r.success).length : 0;
    const failedCount = task.total - successCount;
    if (failedCount === task.total) {
        showToast(`Failed: ${task.task_name}`);
    } else if (failedCount > 0) {
        showToast(`Partially completed: ${task.task_name} (${successCount}/${task.total} succeeded)`);
    } else {
        showToast(`Completed: ${task.task_name}`);
    }
}

function handleTaskCancelled(data) {
    const task = taskQueue.tasks.get(data.taskId);
    if (task) {
        task.status = 'cancelled';
        updateQueueBar();
        renderQueueModal();
    }
}

function handleTaskFailed(data) {
    const task = taskQueue.tasks.get(data.taskId);
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

    // Add cancel button listeners
    queueTaskList.querySelectorAll('.task-cancel-btn').forEach(btn => {
        btn.addEventListener('click', async (e) => {
            e.stopPropagation();
            const taskId = btn.dataset.taskId;
            await cancelTaskById(taskId);
        });
    });
}

function renderTaskCard(task) {
    const statusIcon = getStatusIcon(task.status);
    const progressPercent = task.total > 0 ? (task.progress / task.total) * 100 : 0;
    const showProgress = task.status === 'running';
    const showCancel = task.status === 'running' || task.status === 'pending';
    const timeAgo = getTimeAgo(task.created_at);

    // Build error details section - show errors whenever results contain failures
    let errorDetailsHtml = '';

    // Check for failed results regardless of overall task status
    if (task.results && task.results.length > 0) {
        const failedResults = task.results.filter(r => r.success === false);
        if (failedResults.length > 0) {
            // Group by error message and count
            const errorCounts = {};
            failedResults.forEach(r => {
                const errorMsg = r.error || 'Unknown error';
                errorCounts[errorMsg] = (errorCounts[errorMsg] || 0) + 1;
            });

            const errorEntries = Object.entries(errorCounts);
            const errorItems = errorEntries
                .slice(0, 3)
                .map(([error, count]) => {
                    const countLabel = count > 1 ? ` (${count}x)` : '';
                    const errorId = `error-${task.id}-${Math.random().toString(36).substr(2, 9)}`;
                    return `
                        <li class="cursor-pointer" onclick="toggleErrorExpand('${errorId}')">
                            <span id="${errorId}" class="line-clamp-2 block">${escapeHTML(error)}${countLabel}</span>
                        </li>`;
                })
                .join('');

            const moreCount = errorEntries.length - 3;
            const moreText = moreCount > 0 ? `<li class="text-stone-400">...and ${moreCount} more</li>` : '';

            errorDetailsHtml = `
                <div class="mt-2 pt-2 border-t border-stone-200">
                    <ul class="text-xs text-red-500 space-y-1 list-disc list-inside">
                        ${errorItems}
                        ${moreText}
                    </ul>
                    <p class="text-xs text-stone-400 mt-1">Click error to expand</p>
                </div>
            `;
        }
    }

    // Fallback: show task-level error if no result details but task has error
    if (!errorDetailsHtml && task.error) {
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
                ${showCancel ? `
                    <button class="task-cancel-btn text-stone-400 hover:text-red-500 p-1 transition-colors" data-task-id="${task.id}" title="Cancel">
                        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M6 18L18 6M6 6l12 12"></path>
                        </svg>
                    </button>
                ` : ''}
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

// Actions
async function startBackgroundTask(clipIds, options, taskName) {
    try {
        const taskId = await window.go.main.App.StartAITask(clipIds, options, taskName);
        return taskId;
    } catch (error) {
        console.error('Failed to start task:', error);
        showToast('Failed to start task: ' + error.message);
        throw error;
    }
}

async function cancelTaskById(taskId) {
    try {
        await window.go.main.App.CancelTask(taskId);
    } catch (error) {
        console.error('Failed to cancel task:', error);
        showToast('Failed to cancel task');
    }
}

async function clearCompletedTasksAction() {
    try {
        await window.go.main.App.ClearCompletedTasks();
        // Remove from local state
        for (const [id, task] of taskQueue.tasks) {
            if (task.status === 'completed' || task.status === 'cancelled' || task.status === 'failed') {
                taskQueue.tasks.delete(id);
            }
        }
        updateQueueBar();
        renderQueueModal();
    } catch (error) {
        console.error('Failed to clear tasks:', error);
    }
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
