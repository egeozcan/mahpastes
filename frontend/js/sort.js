// --- Sort Module ---

const sortBtn = document.getElementById('sort-btn');

const sortFields = [
    { id: 'date', label: 'Date added' },
    { id: 'name', label: 'Filename' },
    { id: 'size', label: 'File size' },
    { id: 'type', label: 'Content type' },
];

function openSortPopover() {
    closeSortPopover();

    const popover = document.createElement('div');
    popover.className = 'sort-popover fixed bg-white rounded-lg shadow-xl border border-stone-200 p-2 z-[60]';
    popover.setAttribute('role', 'menu');
    popover.setAttribute('aria-label', 'Sort options');
    popover.dataset.testid = 'sort-popover';

    // Sort field options
    sortFields.forEach(({ id, label }) => {
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
            dirIcon.textContent = currentSortDir === 'asc' ? '\u2191 Asc' : '\u2193 Desc';
            btn.appendChild(dirIcon);
        }

        btn.addEventListener('click', () => {
            if (isActive) {
                // Toggle direction
                setSort(id, currentSortDir === 'asc' ? 'desc' : 'asc');
            } else {
                // Switch field, default desc
                setSort(id, 'desc');
            }
            closeSortPopover();
        });

        popover.appendChild(btn);
    });

    document.body.appendChild(popover);
    positionSortPopover(popover);
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

document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') closeSortPopover();
});
