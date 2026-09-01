// --- Search Options ---
//
// Two opt-in widenings of the search box, both of which need the database.
// The gallery holds at most a page of cards, each carrying only a 500-byte
// preview and none of the clips a hidden tag filtered out, so neither option
// can be answered from the DOM. Turning either one on routes the query through
// App.SearchClips; with both off, searching stays exactly what it always was —
// an instant, local filename/type filter over the cards already on screen.

// Persisted like the sort setting: content search is a preference.
let searchInContent = false;
// Session-only by design. Hidden tags exist to keep things out of sight, so
// revealing them is a deliberate act each session rather than an inherited default.
let searchIncludeHidden = false;

const searchOptionsBtn = document.getElementById('search-options-btn');
const searchOptionsInput = document.getElementById('search-input');

const searchOptionDefs = [
    {
        key: 'content',
        label: 'Search file contents',
        hint: 'Looks inside text and JSON clips, not just their names',
        get: () => searchInContent,
    },
    {
        key: 'hidden',
        label: 'Show hidden clips',
        hint: 'Includes clips carrying a hidden tag',
        get: () => searchIncludeHidden,
    },
];

function getSearchQuery() {
    return searchOptionsInput ? searchOptionsInput.value.trim() : '';
}

function getSearchOptions() {
    return { inContent: searchInContent, includeHidden: searchIncludeHidden };
}

// Folder mode is excluded on purpose: a folder shows one exact tag level, and
// "search everything" has no meaning there that would not quietly break out of
// the folder the user is standing in.
function searchOptionsApply() {
    const folderMode = typeof isFolderMode === 'function' && isFolderMode();
    return !folderMode && (searchInContent || searchIncludeHidden);
}

// True when the gallery should be filled from the backend instead of filtered
// in place. Callers use this to pick the loadClips branch.
function isDeepSearchActive() {
    return getSearchQuery() !== '' && searchOptionsApply();
}

function updateSearchOptionsButton() {
    if (!searchOptionsBtn) return;
    const active = searchOptionsApply();
    searchOptionsBtn.classList.toggle('bg-stone-800', active);
    searchOptionsBtn.classList.toggle('text-white', active);
    searchOptionsBtn.classList.toggle('hover:bg-stone-700', active);
    searchOptionsBtn.classList.toggle('text-stone-400', !active);
    searchOptionsBtn.classList.toggle('hover:text-stone-600', !active);
    searchOptionsBtn.classList.toggle('hover:bg-stone-100', !active);
    const on = searchOptionDefs.filter(o => o.get()).map(o => o.label.toLowerCase());
    searchOptionsBtn.setAttribute('aria-label', on.length ? `Search options: ${on.join(', ')}` : 'Search options');
}

async function setSearchOption(key, value) {
    if (key === 'content') {
        searchInContent = value;
        try {
            await window.go.main.App.SetSetting('search_in_content', value ? 'true' : 'false');
        } catch (error) {
            console.error('Error saving search option:', error);
        }
    } else {
        searchIncludeHidden = value;
    }
    updateSearchOptionsButton();
    // An option change only moves clips around while a query is being answered;
    // with an empty box the gallery is the plain listing either way.
    if (getSearchQuery() !== '' || (typeof galleryIsSearchResults === 'function' && galleryIsSearchResults())) {
        await loadClips();
    }
}

function closeSearchOptionsPopover() {
    const existing = document.querySelector('.search-options-popover');
    if (existing) existing.remove();
    searchOptionsBtn?.setAttribute('aria-expanded', 'false');
}

function openSearchOptionsPopover() {
    closeSearchOptionsPopover();
    if (typeof closeTagFilterDropdown === 'function') closeTagFilterDropdown(false);
    if (typeof closeSortPopover === 'function') closeSortPopover();

    const popover = document.createElement('div');
    popover.className = 'search-options-popover fixed bg-white rounded-lg shadow-xl border border-stone-200 p-2 w-64 z-[60]';
    popover.setAttribute('role', 'menu');
    popover.setAttribute('aria-label', 'Search options');
    popover.dataset.testid = 'search-options-popover';

    const folderMode = typeof isFolderMode === 'function' && isFolderMode();

    searchOptionDefs.forEach(({ key, label, hint, get }) => {
        const row = document.createElement('label');
        row.className = `flex items-start gap-2 px-2 py-1.5 rounded transition-colors ${folderMode ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer hover:bg-stone-50'}`;

        const checkbox = document.createElement('input');
        checkbox.type = 'checkbox';
        checkbox.checked = get();
        checkbox.disabled = folderMode;
        checkbox.className = 'mt-0.5 accent-stone-800';
        checkbox.dataset.testid = `search-option-${key}`;
        checkbox.addEventListener('change', () => setSearchOption(key, checkbox.checked));

        const text = document.createElement('span');
        text.className = 'flex flex-col gap-0.5';
        const labelSpan = document.createElement('span');
        labelSpan.className = 'text-xs font-medium text-stone-700';
        labelSpan.textContent = label;
        const hintSpan = document.createElement('span');
        hintSpan.className = 'text-[10px] text-stone-400 leading-snug';
        hintSpan.textContent = hint;
        text.append(labelSpan, hintSpan);

        row.append(checkbox, text);
        popover.appendChild(row);
    });

    if (folderMode) {
        const note = document.createElement('p');
        note.className = 'mt-1 px-2 pt-2 border-t border-stone-100 text-[10px] text-stone-400 leading-snug';
        note.dataset.testid = 'search-options-folder-note';
        note.textContent = 'Unavailable in folder view — a folder lists one exact tag level.';
        popover.appendChild(note);
    }

    document.body.appendChild(popover);
    positionSearchOptionsPopover(popover);
    searchOptionsBtn.setAttribute('aria-expanded', 'true');
    popover.querySelector('input:not([disabled])')?.focus();
}

function positionSearchOptionsPopover(popover) {
    const btnRect = searchOptionsBtn.getBoundingClientRect();
    const pad = 8;
    popover.style.top = `${btnRect.bottom + pad}px`;
    popover.style.left = `${btnRect.right - popover.offsetWidth}px`;

    const rect = popover.getBoundingClientRect();
    if (rect.left < pad) popover.style.left = `${pad}px`;
    if (rect.bottom > window.innerHeight - pad) {
        popover.style.top = `${btnRect.top - rect.height - pad}px`;
    }
}

searchOptionsBtn?.addEventListener('click', (e) => {
    e.stopPropagation();
    if (document.querySelector('.search-options-popover')) {
        closeSearchOptionsPopover();
    } else {
        openSearchOptionsPopover();
    }
});

document.addEventListener('click', (e) => {
    if (!e.target.closest('.search-options-popover') && !e.target.closest('#search-options-btn')) {
        closeSearchOptionsPopover();
    }
});

document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && document.querySelector('.search-options-popover')) {
        closeSearchOptionsPopover();
        searchOptionsBtn?.focus();
    }
});

async function initSearchOptions() {
    try {
        searchInContent = (await window.go.main.App.GetSetting('search_in_content')) === 'true';
    } catch (error) {
        console.error('Error loading search options:', error);
    }
    updateSearchOptionsButton();
}

Object.assign(window.__testHelpers || (window.__testHelpers = {}), {
    getSearchOptions,
    setSearchOption,
    isDeepSearchActive,
    galleryIsSearchResults: () => typeof galleryIsSearchResults === 'function' && galleryIsSearchResults(),
});
