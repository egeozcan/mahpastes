// --- Sort Module ---

const sortBtn = document.getElementById('sort-btn');

// `first` is the direction a field starts in when it is chosen: newest and
// largest first, but names and types from A (descending there meant two trips
// through the popover to get an alphabetical list). `asc` / `desc` are what
// each direction means for that field.
const sortFields = [
    { id: 'date', label: 'Date added', first: 'desc', asc: 'Oldest first', desc: 'Newest first' },
    { id: 'name', label: 'Filename', first: 'asc', asc: 'A to Z', desc: 'Z to A' },
    { id: 'size', label: 'File size', first: 'desc', asc: 'Smallest first', desc: 'Largest first' },
    { id: 'type', label: 'Content type', first: 'asc', asc: 'A to Z', desc: 'Z to A' },
];

// The button says what the sort is: it is persisted, and a leftover "by size"
// from last week otherwise shows nowhere but inside the popover. It takes the
// active style whenever the sort is not the default (newest first).
function updateSortButton() {
    const field = sortFields.find(f => f.id === currentSortField) || sortFields[0];
    const dir = currentSortDir === 'asc' ? 'asc' : 'desc';
    const text = `Sort: ${field.label}, ${field[dir].toLowerCase()}`;
    sortBtn.setAttribute('data-tooltip', text);
    sortBtn.setAttribute('aria-label', text);
    const custom = !(field.id === 'date' && dir === 'desc');
    sortBtn.dataset.sortActive = String(custom);
    for (const cls of ['bg-stone-800', 'text-white', 'border-stone-800', 'hover:bg-stone-700']) {
        sortBtn.classList.toggle(cls, custom);
    }
    for (const cls of ['border-stone-200', 'hover:border-stone-300', 'hover:bg-stone-100', 'text-stone-500']) {
        sortBtn.classList.toggle(cls, !custom);
    }
}

function openSortPopover() {
    closeSortPopover();
    // Don't let the sort popover coexist with the other header menus.
    if (typeof closeTagFilterDropdown === 'function') closeTagFilterDropdown(false);
    if (typeof closeSearchOptionsPopover === 'function') closeSearchOptionsPopover();

    const popover = document.createElement('div');
    popover.className = 'sort-popover fixed bg-white rounded-lg shadow-xl border border-stone-200 p-2 z-[60]';
    popover.setAttribute('role', 'menu');
    popover.setAttribute('aria-label', 'Sort options');
    popover.dataset.testid = 'sort-popover';

    // Sort field options
    sortFields.forEach((field) => {
        const { id, label } = field;
        const btn = document.createElement('button');
        const isActive = currentSortField === id;
        btn.className = `w-full text-left px-3 py-1.5 text-xs font-medium rounded transition-colors flex items-center justify-between gap-4 ${isActive ? 'bg-stone-100 text-stone-800' : 'text-stone-500 hover:bg-stone-50 hover:text-stone-700'}`;
        btn.setAttribute('role', 'menuitem');
        btn.dataset.testid = `sort-option-${id}`;
        btn.dataset.sort = id;

        const labelSpan = document.createElement('span');
        labelSpan.textContent = label;
        btn.appendChild(labelSpan);

        if (isActive) {
            const dirIcon = document.createElement('span');
            dirIcon.className = 'text-[10px] text-stone-400';
            dirIcon.textContent = currentSortDir === 'asc' ? `\u2191 ${field.asc}` : `\u2193 ${field.desc}`;
            btn.appendChild(dirIcon);
        }

        btn.addEventListener('click', async () => {
            if (isActive) {
                // Toggle direction
                await setSort(id, currentSortDir === 'asc' ? 'desc' : 'asc');
            } else {
                // Switch field, in the direction that field starts in
                await setSort(id, field.first);
            }
            closeSortPopover();
        });

        popover.appendChild(btn);
    });

    // Reversing is a second click on the active row; say so.
    const hint = document.createElement('p');
    hint.className = 'px-3 pt-1.5 pb-0.5 text-[10px] text-stone-400';
    hint.dataset.testid = 'sort-hint';
    // Not a menu item: kept out of the menu's own content for assistive tech.
    hint.setAttribute('role', 'presentation');
    hint.textContent = 'Click the active row to reverse';
    popover.appendChild(hint);

    document.body.appendChild(popover);
    positionSortPopover(popover);
    sortBtn.setAttribute('aria-expanded', 'true');
}

function positionSortPopover(popover) {
    const btnRect = sortBtn.getBoundingClientRect();
    const pad = 8;
    popover.style.top = `${btnRect.bottom + pad}px`;
    popover.style.left = `${btnRect.right - popover.offsetWidth}px`;

    // Constrain to viewport
    const rect = popover.getBoundingClientRect();
    if (rect.left < pad) popover.style.left = `${pad}px`;
    if (rect.bottom > window.innerHeight - pad) {
        popover.style.top = `${btnRect.top - rect.height - pad}px`;
    }
}

function closeSortPopover() {
    const existing = document.querySelector('.sort-popover');
    if (existing) existing.remove();
    sortBtn.setAttribute('aria-expanded', 'false');
}

sortBtn.addEventListener('click', (e) => {
    e.stopPropagation();
    const existing = document.querySelector('.sort-popover');
    if (existing) {
        closeSortPopover();
    } else {
        openSortPopover();
    }
});

document.addEventListener('click', (e) => {
    if (!e.target.closest('.sort-popover') && !e.target.closest('#sort-btn')) {
        closeSortPopover();
    }
});

