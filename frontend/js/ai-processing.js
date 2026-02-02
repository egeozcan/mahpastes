// --- AI Processing Module ---

let currentAITask = null;
let aiModels = {};
let processingInProgress = false;
let currentAIClipIds = [];

// Elements
const aiProcessingModal = document.getElementById('ai-processing-modal');
const aiConfirmModal = document.getElementById('ai-confirm-modal');
const bulkAIBtn = document.getElementById('bulk-ai-btn');
const bulkAIDropdown = document.getElementById('bulk-ai-dropdown');
const aiModalTitle = document.getElementById('ai-modal-title');
const aiTaskDescription = document.getElementById('ai-task-description');
const aiImageCount = document.getElementById('ai-image-count');
const aiModelSelect = document.getElementById('ai-model-select');
const aiModalCancel = document.getElementById('ai-modal-cancel');
const aiModalProcess = document.getElementById('ai-modal-process');

// Task-specific option elements
const aiRestoreOptions = document.getElementById('ai-restore-options');
const aiEditOptions = document.getElementById('ai-edit-options');
const aiModelSection = document.getElementById('ai-model-section');

// Progress elements
const aiProgress = document.getElementById('ai-progress');
const aiProgressBar = document.getElementById('ai-progress-bar');
const aiProgressText = document.getElementById('ai-progress-text');

// Task configurations
const taskConfigs = {
    colorize: {
        title: 'Colorize Images',
        description: 'Add color to black & white images'
    },
    upscale: {
        title: 'Upscale Images',
        description: 'Increase image resolution'
    },
    restore: {
        title: 'Restore Images',
        description: 'Fix scratches, colors, and enhance quality'
    },
    edit: {
        title: 'AI Edit Images',
        description: 'Transform images using text prompts'
    },
    vectorize: {
        title: 'Vectorize Images',
        description: 'Convert images to SVG vector format'
    }
};

// Load available models
async function loadAIModels() {
    try {
        aiModels = await window.go.main.App.GetAvailableFalModels();
    } catch (error) {
        console.error('Failed to load AI models:', error);
    }
}

// Get selected image IDs
function getSelectedImageIds(excludeVector = false) {
    if (!selectedIds) return [];
    return Array.from(selectedIds).filter(id => {
        const card = gallery.querySelector(`li[data-id="${id}"]`);
        if (!card || !card.dataset.type) return false;
        const type = card.dataset.type;
        if (!type.startsWith('image/')) return false;
        // Exclude vector formats (SVG) if requested
        if (excludeVector && type === 'image/svg+xml') return false;
        return true;
    });
}

// Open AI processing modal
function openAIModal(task, clipIds = null) {
    currentAITask = task;
    const config = taskConfigs[task];

    // Get selected image IDs (exclude vectors for vectorize task)
    const excludeVector = task === 'vectorize';
    currentAIClipIds = clipIds || getSelectedImageIds(excludeVector);
    if (currentAIClipIds.length === 0) {
        if (excludeVector) {
            showToast('No raster images selected (SVGs cannot be vectorized)');
        } else {
            showToast('No images selected');
        }
        return;
    }

    // Update UI
    aiModalTitle.textContent = config.title;
    aiTaskDescription.textContent = config.description;
    aiImageCount.textContent = `${currentAIClipIds.length} image${currentAIClipIds.length > 1 ? 's' : ''} selected`;

    // Populate model dropdown
    aiModelSelect.innerHTML = '';
    const models = aiModels[task] || [];
    models.forEach((model, index) => {
        const option = document.createElement('option');
        option.value = model.id;
        option.textContent = `${model.name} - ${model.description}`;
        if (index === 0) option.selected = true;
        aiModelSelect.appendChild(option);
    });

    // Show/hide task-specific options
    aiRestoreOptions.classList.toggle('hidden', task !== 'restore');
    aiEditOptions.classList.toggle('hidden', task !== 'edit');
    aiModelSection.classList.toggle('hidden', models.length <= 1);

    // Reset progress
    aiProgress.classList.add('hidden');
    aiProgressBar.style.width = '0%';
    aiModalProcess.disabled = false;
    aiModalCancel.disabled = false;
    aiModalCancel.textContent = 'Cancel';

    // Show modal
    showAIModal(aiProcessingModal);
    closeDropdown();
}

function closeAIModal() {
    hideAIModal(aiProcessingModal);
    currentAITask = null;
    currentAIClipIds = [];
}

// Show confirmation dialog
function showAIConfirmation() {
    const modelOption = aiModelSelect.options[aiModelSelect.selectedIndex];
    const modelName = modelOption ? modelOption.textContent.split(' - ')[0] : currentAITask;

    document.getElementById('ai-confirm-message').innerHTML =
        `You are about to send <strong>${currentAIClipIds.length} image${currentAIClipIds.length > 1 ? 's' : ''}</strong> to fal.ai using <strong>${modelName}</strong>.`;

    showAIModal(aiConfirmModal);
}

function closeAIConfirmation() {
    hideAIModal(aiConfirmModal);
}

// Process images - starts background task and closes modal immediately
async function processImages() {
    closeAIConfirmation();

    if (currentAIClipIds.length === 0) return;

    // Build options
    const options = {
        task: currentAITask,
        model: aiModelSelect.value
    };

    // Add task-specific options
    if (currentAITask === 'restore') {
        options.fix_colors = document.getElementById('ai-restore-colors').checked;
        options.remove_scratches = document.getElementById('ai-restore-scratches').checked;
    } else if (currentAITask === 'edit') {
        options.prompt = document.getElementById('ai-edit-prompt').value;
        options.strength = parseInt(document.getElementById('ai-edit-strength').value) / 100;

        if (!options.prompt.trim()) {
            showToast('Please enter a prompt');
            return;
        }
    }

    // Build task name for display
    const taskName = `${taskConfigs[currentAITask].title} (${currentAIClipIds.length})`;

    try {
        // Start background task - returns immediately
        await startBackgroundTask(currentAIClipIds, options, taskName);

        // Close modal immediately
        closeAIModal();

        // Clear selection
        if (typeof cancelSelection === 'function') {
            cancelSelection();
        }

        showToast('Processing started');
    } catch (error) {
        console.error('AI processing error:', error);
        showToast('Failed to start processing: ' + error.message);
    }
}

// Dropdown toggle
function toggleDropdown() {
    bulkAIDropdown.classList.toggle('hidden');
}

function closeDropdown() {
    if (bulkAIDropdown) {
        bulkAIDropdown.classList.add('hidden');
    }
}

// Modal helpers
function showAIModal(modal) {
    modal.classList.remove('opacity-0', 'pointer-events-none');
    modal.classList.add('opacity-100');
    const inner = modal.querySelector(':scope > div');
    if (inner) {
        inner.classList.remove('scale-95');
        inner.classList.add('scale-100');
    }
}

function hideAIModal(modal) {
    modal.classList.add('opacity-0', 'pointer-events-none');
    modal.classList.remove('opacity-100');
    const inner = modal.querySelector(':scope > div');
    if (inner) {
        inner.classList.add('scale-95');
        inner.classList.remove('scale-100');
    }
}

// Event listeners
if (bulkAIBtn) {
    bulkAIBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        toggleDropdown();
    });
}

document.querySelectorAll('.ai-action-btn').forEach(btn => {
    btn.addEventListener('click', () => {
        openAIModal(btn.dataset.action);
    });
});

// Lightbox AI buttons
document.querySelectorAll('.lightbox-ai-btn').forEach(btn => {
    btn.addEventListener('click', (e) => {
        e.stopPropagation();
        const action = btn.dataset.action;
        if (typeof imageClips !== 'undefined' && typeof currentLightboxIndex !== 'undefined') {
            const clipId = imageClips[currentLightboxIndex]?.id;
            if (clipId) {
                closeLightbox();
                openAIModal(action, [clipId]);
            }
        }
    });
});

if (aiModalCancel) {
    aiModalCancel.addEventListener('click', closeAIModal);
}

if (aiModalProcess) {
    aiModalProcess.addEventListener('click', showAIConfirmation);
}

document.getElementById('ai-confirm-cancel')?.addEventListener('click', closeAIConfirmation);
document.getElementById('ai-confirm-proceed')?.addEventListener('click', processImages);

// Close dropdown when clicking outside
document.addEventListener('click', (e) => {
    if (bulkAIBtn && bulkAIDropdown && !bulkAIBtn.contains(e.target) && !bulkAIDropdown.contains(e.target)) {
        closeDropdown();
    }
});

// Edit strength slider update
document.getElementById('ai-edit-strength')?.addEventListener('input', (e) => {
    document.getElementById('ai-edit-strength-value').textContent = e.target.value + '%';
});

// Load models on init
window.addEventListener('load', loadAIModels);
