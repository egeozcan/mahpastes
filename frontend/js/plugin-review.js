// --- Plugin Review Module ---

// State
let reviewResolve = null; // Promise resolve for review result
let reviewSource = null;  // URL or file path being reviewed

// Elements
const reviewModal = document.getElementById('plugin-review-modal');
const reviewTitle = document.getElementById('plugin-review-title');
const reviewWarning = document.getElementById('plugin-review-warning');
const reviewName = document.getElementById('plugin-review-name');
const reviewVersion = document.getElementById('plugin-review-version');
const reviewAuthor = document.getElementById('plugin-review-author');
const reviewDescription = document.getElementById('plugin-review-description');
const reviewNetworkSection = document.getElementById('plugin-review-network-section');
const reviewNetwork = document.getElementById('plugin-review-network');
const reviewURLSettingsSection = document.getElementById('plugin-review-urlsettings-section');
const reviewURLSettings = document.getElementById('plugin-review-urlsettings');
const reviewFSSection = document.getElementById('plugin-review-fs-section');
const reviewFS = document.getElementById('plugin-review-fs');
const reviewClipboardSection = document.getElementById('plugin-review-clipboard-section');
const reviewEventsSection = document.getElementById('plugin-review-events-section');
const reviewEvents = document.getElementById('plugin-review-events');
const reviewCancelBtn = document.getElementById('plugin-review-cancel');
const reviewApproveBtn = document.getElementById('plugin-review-approve');

/**
 * Show the plugin review modal and return a promise that resolves to true (approved) or false (cancelled).
 * @param {object} preview - PluginPreview object from backend
 * @param {'install'|'update'} mode
 * @param {string} [currentVersion] - Current version (for update mode)
 */
function showPluginReview(preview, mode, currentVersion) {
    return new Promise((resolve) => {
        reviewResolve = resolve;
        reviewSource = preview.source;

        // Title
        reviewTitle.textContent = mode === 'update' ? 'Review Update' : 'Review Plugin';

        // Warning banner
        if (mode === 'update') {
            reviewWarning.classList.remove('hidden');
        } else {
            reviewWarning.classList.add('hidden');
        }

        // Plugin info
        reviewName.textContent = preview.name || 'Unknown';
        if (mode === 'update' && currentVersion) {
            reviewVersion.textContent = `v${currentVersion} → v${preview.version || '?'}`;
        } else {
            reviewVersion.textContent = `v${preview.version || '0.0.0'}`;
        }
        reviewAuthor.textContent = preview.author ? `by ${preview.author}` : '';
        reviewDescription.textContent = preview.description || '';

        // Network
        const network = preview.network || {};
        const domains = Object.keys(network);
        if (domains.length > 0) {
            reviewNetworkSection.classList.remove('hidden');
            reviewNetwork.innerHTML = domains.map(domain => {
                const methods = (network[domain] || []).join(', ');
                return `<div class="flex items-center gap-2 p-2 bg-white rounded border border-stone-200">
                    <span class="text-[11px] font-mono text-stone-600">${escapeHTML(domain)}</span>
                    <span class="text-[9px] text-stone-400">(${escapeHTML(methods)})</span>
                </div>`;
            }).join('');
        } else {
            reviewNetworkSection.classList.add('hidden');
        }

        // Settings-based network access: url settings grant their methods to
        // whatever host the user types, so the review must disclose them.
        const urlSettings = preview.url_settings || [];
        if (urlSettings.length > 0) {
            reviewURLSettingsSection.classList.remove('hidden');
            reviewURLSettings.innerHTML = urlSettings.map(s => {
                const methods = (s.methods || []).join(', ');
                return `<div class="p-2 bg-white rounded border border-stone-200">
                    <span class="text-[11px] text-stone-600">${escapeHTML(s.label || s.key)}</span>
                    <span class="text-[10px] text-stone-400"> — network access to a server you specify (${escapeHTML(methods)})</span>
                </div>`;
            }).join('');
        } else {
            reviewURLSettingsSection.classList.add('hidden');
        }

        // Filesystem
        if (preview.filesystem && (preview.filesystem.Read || preview.filesystem.Write)) {
            reviewFSSection.classList.remove('hidden');
            let badges = '';
            if (preview.filesystem.Read) {
                badges += '<span class="inline-flex items-center px-1.5 py-0.5 rounded text-[9px] font-medium uppercase bg-blue-100 text-blue-700">read</span>';
            }
            if (preview.filesystem.Write) {
                badges += '<span class="inline-flex items-center px-1.5 py-0.5 rounded text-[9px] font-medium uppercase bg-amber-100 text-amber-700">write</span>';
            }
            reviewFS.innerHTML = badges;
        } else {
            reviewFSSection.classList.add('hidden');
        }

        // Clipboard
        if (preview.clipboard) {
            reviewClipboardSection.classList.remove('hidden');
        } else {
            reviewClipboardSection.classList.add('hidden');
        }

        // Events
        const events = preview.events || [];
        if (events.length > 0) {
            reviewEventsSection.classList.remove('hidden');
            reviewEvents.innerHTML = events.map(event =>
                `<span class="inline-flex items-center px-1.5 py-0.5 rounded text-[9px] font-medium bg-stone-200 text-stone-600">${escapeHTML(event)}</span>`
            ).join('');
        } else {
            reviewEventsSection.classList.add('hidden');
        }

        // Button text
        reviewApproveBtn.textContent = mode === 'update' ? 'Approve & Update' : 'Approve & Install';
        reviewApproveBtn.disabled = false;

        // Show modal
        reviewModal.removeAttribute('inert');
        reviewModal.classList.remove('opacity-0', 'pointer-events-none');
        reviewModal.classList.add('opacity-100');
        reviewModal.querySelector(':scope > div').classList.remove('scale-95');
        reviewModal.querySelector(':scope > div').classList.add('scale-100');
    });
}

function closePluginReview(approved) {
    reviewModal.classList.add('opacity-0', 'pointer-events-none');
    reviewModal.classList.remove('opacity-100');
    reviewModal.querySelector(':scope > div').classList.add('scale-95');
    reviewModal.querySelector(':scope > div').classList.remove('scale-100');
    reviewModal.setAttribute('inert', '');

    if (reviewResolve) {
        reviewResolve(approved);
        reviewResolve = null;
    }
    reviewSource = null;
}

// Event listeners
reviewCancelBtn.addEventListener('click', () => closePluginReview(false));
reviewApproveBtn.addEventListener('click', () => {
    reviewApproveBtn.disabled = true;
    reviewApproveBtn.textContent = 'Installing...';
    closePluginReview(true);
});
reviewModal.addEventListener('click', (e) => {
    if (e.target === reviewModal) closePluginReview(false);
});
