# Open With & Context Menu Restructure — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add "Open" / "Open With..." to the context menu, restructure into submenus (Copy ▸, Plugins ▸), and add keyboard activation + arrow-key navigation.

**Architecture:** Platform-specific Go files (`open_darwin.go`, `open_windows.go`, `open_other.go`) handle OS open commands. Frontend extracts a shared `context-menu.js` module used by both card menu and lightbox menu. Submenus use JS-managed hover delays for native feel.

**Tech Stack:** Go + `exec.Command` (backend), Vanilla JS + CSS (frontend), Playwright (tests)

---

### Task 1: Backend — Platform-specific open functions

**Files:**
- Create: `open_darwin.go`
- Create: `open_windows.go`
- Create: `open_other.go`

**Step 1: Create `open_darwin.go`**

```go
//go:build darwin

package main

import (
	"fmt"
	"os/exec"
)

func openFileWithDefaultApp(path string) error {
	cmd := exec.Command("open", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("open failed: %w: %s", err, string(out))
	}
	return nil
}

func openFileWithApp(filePath, appPath string) error {
	cmd := exec.Command("open", "-a", appPath, filePath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("open -a failed: %w: %s", err, string(out))
	}
	return nil
}
```

**Step 2: Create `open_windows.go`**

```go
//go:build windows

package main

import (
	"fmt"
	"os/exec"
)

func openFileWithDefaultApp(path string) error {
	cmd := exec.Command("cmd", "/c", "start", "", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("start failed: %w: %s", err, string(out))
	}
	return nil
}

func openFileWithApp(filePath, appPath string) error {
	cmd := exec.Command("cmd", "/c", "start", "", appPath, filePath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("start failed: %w: %s", err, string(out))
	}
	return nil
}
```

**Step 3: Create `open_other.go`**

```go
//go:build !darwin && !windows

package main

import (
	"fmt"
	"runtime"
)

func openFileWithDefaultApp(_ string) error {
	return fmt.Errorf("open is not supported on %s", runtime.GOOS)
}

func openFileWithApp(_, _ string) error {
	return fmt.Errorf("open-with is not supported on %s", runtime.GOOS)
}
```

**Step 4: Verify it compiles**

Run: `cd /Users/egecan/Code/mahpastes && go build ./...`
Expected: No errors

**Step 5: Commit**

```bash
git add open_darwin.go open_windows.go open_other.go
git commit -m "feat: add platform-specific open file functions"
```

---

### Task 2: Backend — App methods for Open, Open With, ChooseApplication

**Files:**
- Modify: `app.go` (add 3 new methods near `CreateTempFile` around line 1443)

**Step 1: Add the three methods to `app.go`**

Add after the `CreateTempFile` method (around line 1449):

```go
// OpenClipWithDefaultApp materializes a temp file and opens it with the OS default application.
func (a *App) OpenClipWithDefaultApp(id int64) error {
	item, err := a.prepareClipTransferItem(id, "open_default")
	if err != nil {
		return err
	}
	return openFileWithDefaultApp(item.AbsPath)
}

// OpenClipWithApp materializes a temp file and opens it with a specific application.
func (a *App) OpenClipWithApp(id int64, appPath string) error {
	item, err := a.prepareClipTransferItem(id, "open_with")
	if err != nil {
		return err
	}
	return openFileWithApp(item.AbsPath, appPath)
}

// ChooseApplication shows a file picker dialog for selecting an application.
// Returns the selected path, or empty string if cancelled.
func (a *App) ChooseApplication() (string, error) {
	return chooseApplicationDialog(a.ctx)
}
```

**Step 2: Add `chooseApplicationDialog` to each platform file**

Append to `open_darwin.go`:

```go
import (
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func chooseApplicationDialog(ctx context.Context) (string, error) {
	return runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{
		Title:            "Choose Application",
		DefaultDirectory: "/Applications",
		Filters: []runtime.FileFilter{
			{DisplayName: "Applications", Pattern: "*.app"},
		},
	})
}
```

Note: Merge the `import` block with the existing imports — the file needs `context`, `fmt`, `os/exec`, and the wails runtime import.

Append to `open_windows.go`:

```go
func chooseApplicationDialog(ctx context.Context) (string, error) {
	return runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{
		Title:            "Choose Application",
		DefaultDirectory: `C:\Program Files`,
		Filters: []runtime.FileFilter{
			{DisplayName: "Executables", Pattern: "*.exe"},
		},
	})
}
```

Note: Same import merging needed.

Append to `open_other.go`:

```go
func chooseApplicationDialog(_ context.Context) (string, error) {
	return "", fmt.Errorf("choose-application is not supported on %s", runtime.GOOS)
}
```

Note: Add `context` to the import block. `runtime` is already imported (Go's `runtime`, not Wails).

**Step 3: Verify it compiles**

Run: `cd /Users/egecan/Code/mahpastes && go build ./...`
Expected: No errors

**Step 4: Regenerate frontend bindings**

Run: `cd /Users/egecan/Code/mahpastes && ~/go/bin/wails generate module`
Expected: New methods appear in `frontend/wailsjs/go/main/App.js` and `App.d.ts`

**Step 5: Commit**

```bash
git add app.go open_darwin.go open_windows.go open_other.go frontend/wailsjs/
git commit -m "feat: add OpenClipWithDefaultApp, OpenClipWithApp, ChooseApplication"
```

---

### Task 3: Frontend — Extract shared context menu module

This task creates `frontend/js/context-menu.js` with submenu rendering, positioning, hover delays, and keyboard navigation. Both `ui.js` (card menu) and `modals.js` (lightbox menu) will use it.

**Files:**
- Create: `frontend/js/context-menu.js`
- Modify: `frontend/index.html` (add `<script>` tag before `ui.js`)

**Step 1: Create `frontend/js/context-menu.js`**

This module provides:
- `buildContextMenu(items, options)` — creates the DOM, returns the menu element
- `positionMenu(menu, anchor)` — positions relative to anchor element
- `closeContextMenu()` — removes any open context menu
- Submenu hover logic with 150ms open delay, 200ms close delay
- Keyboard navigation (↑/↓/→/←/Enter/Space/Escape)

```javascript
// --- Context Menu Module ---
// Shared context menu rendering with submenu support, hover delays, and keyboard navigation.

const ContextMenu = (() => {
    let activeMenu = null;
    let activeSubmenu = null;
    let openTimer = null;
    let closeTimer = null;
    let outsideClickHandler = null;

    const OPEN_DELAY = 150;
    const CLOSE_DELAY = 200;

    // Build a menu item element
    function createMenuItem(item, clipId, onAction) {
        if (item.type === 'divider') {
            const div = document.createElement('hr');
            div.className = 'card-menu-divider';
            return div;
        }

        if (item.type === 'submenu') {
            return createSubmenuTrigger(item, clipId, onAction);
        }

        const btn = document.createElement('button');
        btn.className = `card-menu-item${item.danger ? ' card-menu-item-danger' : ''}`;
        btn.setAttribute('role', 'menuitem');
        btn.setAttribute('tabindex', '-1');
        btn.dataset.action = item.id;
        btn.dataset.clipId = clipId;

        // Plugin action data attributes
        if (item.pluginId) {
            btn.dataset.pluginId = item.pluginId;
            btn.dataset.actionId = item.actionId;
            btn.dataset.hasOptions = item.hasOptions ? 'true' : 'false';
        }

        const iconHtml = item.iconHtml || '';
        btn.innerHTML = `${iconHtml}<span>${item.label}</span>`;
        if (item.tooltip) btn.title = item.tooltip;

        btn.addEventListener('click', (e) => {
            e.stopPropagation();
            close();
            if (onAction) onAction(item.id, clipId, item);
        });

        return btn;
    }

    // Build a submenu trigger with ▸ arrow
    function createSubmenuTrigger(item, clipId, onAction) {
        const trigger = document.createElement('button');
        trigger.className = 'card-menu-item card-menu-submenu-trigger';
        trigger.setAttribute('role', 'menuitem');
        trigger.setAttribute('tabindex', '-1');
        trigger.setAttribute('aria-haspopup', 'true');
        trigger.setAttribute('aria-expanded', 'false');

        const iconHtml = item.iconHtml || '';
        trigger.innerHTML = `${iconHtml}<span>${item.label}</span><span class="card-menu-arrow">▸</span>`;
        if (item.tooltip) trigger.title = item.tooltip;

        // Pre-build submenu (hidden until hover/keyboard)
        const submenu = document.createElement('div');
        submenu.className = 'card-menu-submenu';
        submenu.setAttribute('role', 'menu');

        item.children.forEach(child => {
            submenu.appendChild(createMenuItem(child, clipId, onAction));
        });

        trigger._submenu = submenu;
        trigger._submenuItems = item.children;

        // Hover open with delay
        trigger.addEventListener('mouseenter', () => {
            clearTimeout(closeTimer);
            clearTimeout(openTimer);
            openTimer = setTimeout(() => openSubmenu(trigger, submenu), OPEN_DELAY);
        });

        trigger.addEventListener('mouseleave', (e) => {
            clearTimeout(openTimer);
            // Check if moving into the submenu
            const related = e.relatedTarget;
            if (submenu.contains(related)) return;
            closeTimer = setTimeout(() => closeSubmenu(trigger, submenu), CLOSE_DELAY);
        });

        submenu.addEventListener('mouseenter', () => {
            clearTimeout(closeTimer);
        });

        submenu.addEventListener('mouseleave', (e) => {
            const related = e.relatedTarget;
            if (trigger === related || trigger.contains(related)) return;
            closeTimer = setTimeout(() => closeSubmenu(trigger, submenu), CLOSE_DELAY);
        });

        return trigger;
    }

    function openSubmenu(trigger, submenu) {
        // Close any other open submenu first
        if (activeSubmenu && activeSubmenu !== submenu) {
            const prevTrigger = activeSubmenu._trigger;
            if (prevTrigger) closeSubmenu(prevTrigger, activeSubmenu);
        }

        trigger.setAttribute('aria-expanded', 'true');
        trigger.classList.add('card-menu-submenu-open');
        document.body.appendChild(submenu);
        positionSubmenu(submenu, trigger);
        submenu.style.display = '';
        activeSubmenu = submenu;
        submenu._trigger = trigger;
    }

    function closeSubmenu(trigger, submenu) {
        trigger.setAttribute('aria-expanded', 'false');
        trigger.classList.remove('card-menu-submenu-open');
        if (submenu.parentNode) submenu.parentNode.removeChild(submenu);
        if (activeSubmenu === submenu) activeSubmenu = null;
    }

    function positionSubmenu(submenu, trigger) {
        const triggerRect = trigger.getBoundingClientRect();
        const pad = 4;
        const vw = window.innerWidth;
        const vh = window.innerHeight;

        // Temporarily show to measure
        submenu.style.visibility = 'hidden';
        submenu.style.position = 'fixed';
        submenu.style.display = '';
        const subRect = submenu.getBoundingClientRect();
        submenu.style.visibility = '';

        // Prefer right side; flip left if overflows
        let left = triggerRect.right + pad;
        if (left + subRect.width > vw - pad) {
            left = triggerRect.left - subRect.width - pad;
        }

        // Align top with trigger, clamp to viewport
        let top = triggerRect.top;
        if (top + subRect.height > vh - pad) {
            top = vh - subRect.height - pad;
        }
        if (top < pad) top = pad;

        submenu.style.top = `${top}px`;
        submenu.style.left = `${left}px`;
    }

    // Position the main menu relative to an anchor element
    function positionMenu(menu, anchor) {
        const anchorRect = anchor.getBoundingClientRect();
        const menuRect = menu.getBoundingClientRect();
        const pad = 8;
        const gap = 4;
        const vw = window.innerWidth;
        const vh = window.innerHeight;

        // Horizontal: align right edge to anchor, clamp within viewport
        let left = anchorRect.right - menuRect.width;
        if (left < pad) left = pad;
        if (left + menuRect.width > vw - pad) left = vw - menuRect.width - pad;

        // Vertical: prefer below, then above, then constrain with scroll
        const spaceBelow = vh - anchorRect.bottom - gap - pad;
        const spaceAbove = anchorRect.top - gap - pad;
        let top;
        let maxHeight = null;

        if (spaceBelow >= menuRect.height) {
            top = anchorRect.bottom + gap;
        } else if (spaceAbove >= menuRect.height) {
            top = anchorRect.top - menuRect.height - gap;
        } else if (spaceBelow >= spaceAbove) {
            top = anchorRect.bottom + gap;
            maxHeight = spaceBelow;
        } else {
            maxHeight = spaceAbove;
            top = anchorRect.top - gap - maxHeight;
        }

        if (top + menuRect.height > vh - pad) {
            top = vh - menuRect.height - pad;
        }
        if (top < pad) {
            top = pad;
            maxHeight = vh - 2 * pad;
        }

        menu.style.top = `${top}px`;
        menu.style.left = `${left}px`;

        if (maxHeight !== null) {
            menu.style.maxHeight = `${maxHeight}px`;
            menu.style.overflowY = 'auto';
        }
    }

    // Keyboard navigation
    function setupKeyboard(menu) {
        menu.addEventListener('keydown', (e) => {
            // If a submenu is active, delegate to submenu keyboard handler
            if (activeSubmenu && activeSubmenu.contains(document.activeElement)) {
                handleSubmenuKeyboard(e);
                return;
            }
            handleMenuKeyboard(e, menu);
        });
    }

    function getMenuItems(container) {
        return Array.from(container.querySelectorAll(':scope > [role="menuitem"]'));
    }

    function handleMenuKeyboard(e, menu) {
        const items = getMenuItems(menu);
        const focused = document.activeElement;
        const idx = items.indexOf(focused);

        switch (e.key) {
            case 'ArrowDown':
                e.preventDefault();
                focusItem(items, idx, 1);
                break;
            case 'ArrowUp':
                e.preventDefault();
                focusItem(items, idx, -1);
                break;
            case 'ArrowRight':
                e.preventDefault();
                if (focused && focused._submenu) {
                    openSubmenu(focused, focused._submenu);
                    const subItems = getMenuItems(focused._submenu);
                    if (subItems.length) subItems[0].focus();
                }
                break;
            case 'Enter':
            case ' ':
                e.preventDefault();
                if (focused && focused._submenu) {
                    openSubmenu(focused, focused._submenu);
                    const subItems = getMenuItems(focused._submenu);
                    if (subItems.length) subItems[0].focus();
                } else if (focused) {
                    focused.click();
                }
                break;
            case 'Escape':
                e.preventDefault();
                close();
                break;
            case 'Tab':
                e.preventDefault();
                close();
                break;
        }
    }

    function handleSubmenuKeyboard(e) {
        const submenu = activeSubmenu;
        if (!submenu) return;

        const items = getMenuItems(submenu);
        const focused = document.activeElement;
        const idx = items.indexOf(focused);

        switch (e.key) {
            case 'ArrowDown':
                e.preventDefault();
                focusItem(items, idx, 1);
                break;
            case 'ArrowUp':
                e.preventDefault();
                focusItem(items, idx, -1);
                break;
            case 'ArrowLeft':
                e.preventDefault();
                // Return to parent trigger
                const trigger = submenu._trigger;
                if (trigger) {
                    closeSubmenu(trigger, submenu);
                    trigger.focus();
                }
                break;
            case 'Enter':
            case ' ':
                e.preventDefault();
                if (focused) focused.click();
                break;
            case 'Escape':
                e.preventDefault();
                const trig = submenu._trigger;
                if (trig) {
                    closeSubmenu(trig, submenu);
                    trig.focus();
                }
                break;
            case 'Tab':
                e.preventDefault();
                close();
                break;
        }
    }

    function focusItem(items, currentIdx, direction) {
        if (!items.length) return;
        let next = currentIdx + direction;
        if (next >= items.length) next = 0;
        if (next < 0) next = items.length - 1;
        items[next].focus();
    }

    // Open a context menu
    function open(items, clipId, anchor, onAction) {
        close(); // close any existing

        const menu = document.createElement('div');
        menu.className = 'card-menu-dropdown fixed';
        menu.setAttribute('role', 'menu');
        menu.setAttribute('aria-label', 'Clip actions');
        menu.dataset.clipId = clipId;

        items.forEach(item => {
            menu.appendChild(createMenuItem(item, clipId, onAction));
        });

        document.body.appendChild(menu);
        positionMenu(menu, anchor);
        setupKeyboard(menu);

        // Focus first item
        const firstItem = menu.querySelector('[role="menuitem"]');
        if (firstItem) firstItem.focus();

        activeMenu = menu;

        // Outside click handler
        outsideClickHandler = (e) => {
            if (!menu.contains(e.target) && !(activeSubmenu && activeSubmenu.contains(e.target))) {
                close();
            }
        };
        // Defer to avoid catching the click that opened the menu
        requestAnimationFrame(() => {
            document.addEventListener('mousedown', outsideClickHandler, true);
        });

        // Update trigger button state
        anchor.setAttribute('aria-expanded', 'true');
        menu._anchor = anchor;

        return menu;
    }

    // Close the active context menu and any submenus
    function close() {
        clearTimeout(openTimer);
        clearTimeout(closeTimer);

        if (activeSubmenu) {
            const trigger = activeSubmenu._trigger;
            if (trigger) closeSubmenu(trigger, activeSubmenu);
        }

        if (activeMenu) {
            const anchor = activeMenu._anchor;
            if (anchor) anchor.setAttribute('aria-expanded', 'false');
            activeMenu.remove();
            activeMenu = null;
        }

        if (outsideClickHandler) {
            document.removeEventListener('mousedown', outsideClickHandler, true);
            outsideClickHandler = null;
        }
    }

    function isOpen() {
        return activeMenu !== null;
    }

    return { open, close, isOpen, positionMenu };
})();
```

**Step 2: Add `<script>` tag in `frontend/index.html`**

Find the `<script>` tags section. Add `context-menu.js` before `ui.js`:

```html
<script src="js/context-menu.js"></script>
```

**Step 3: Verify the file is loadable**

Run: `cd /Users/egecan/Code/mahpastes && make dev` (visually confirm no JS errors)
Or verify syntax: the file should parse without errors.

**Step 4: Commit**

```bash
git add frontend/js/context-menu.js frontend/index.html
git commit -m "feat: add shared context menu module with submenu support"
```

---

### Task 4: Frontend — CSS for submenus

**Files:**
- Modify: `frontend/css/main.css` (add after `.card-menu-divider` block, around line 207)

**Step 1: Add submenu CSS classes**

Add after the `.card-menu-divider` rule (line 207):

```css
/* Submenu trigger */
.card-menu-submenu-trigger {
    justify-content: flex-start;
}

.card-menu-arrow {
    margin-left: auto;
    font-size: 0.625rem;
    color: #a8a29e;
    line-height: 1;
}

.card-menu-submenu-trigger:hover .card-menu-arrow,
.card-menu-submenu-trigger:focus .card-menu-arrow,
.card-menu-submenu-open .card-menu-arrow {
    color: #57534e;
}

.card-menu-submenu-open {
    background-color: #f5f5f4;
}

/* Submenu panel (same style as main dropdown) */
.card-menu-submenu {
    position: fixed;
    background-color: white;
    border-radius: 0.375rem;
    box-shadow: 0 10px 15px -3px rgba(0, 0, 0, 0.1), 0 4px 6px -2px rgba(0, 0, 0, 0.05);
    border: 1px solid #e7e5e4;
    padding: 0.25rem 0;
    z-index: 51;
    min-width: 120px;
}
```

**Step 2: Verify styles**

Run: `make dev`, open card menu, hover a submenu trigger — visually confirm styling.

**Step 3: Commit**

```bash
git add frontend/css/main.css
git commit -m "feat: add CSS for context menu submenus"
```

---

### Task 5: Frontend — Refactor card menu to use ContextMenu module

**Files:**
- Modify: `frontend/js/ui.js` (lines 78-316 area — `getMenuIcon`, `cardMenuTooltips`, `renderCardMenu`, `positionCardMenu`, `setupMenuKeyboard`, `closeCardMenu`)

**Step 1: Add icon and tooltip maps for new items to `ui.js`**

In the `menuIcons` object (around line 78), add:

```javascript
'open': '<path stroke-linecap="round" stroke-linejoin="round" d="M13.5 6H5.25A2.25 2.25 0 003 8.25v10.5A2.25 2.25 0 005.25 21h10.5A2.25 2.25 0 0018 18.75V10.5m-10.5 6L21 3m0 0h-5.25M21 3v5.25"/>',
'open-with': '<path stroke-linecap="round" stroke-linejoin="round" d="M3.75 9.776c.112-.017.227-.026.344-.026h15.812c.117 0 .232.009.344.026m-16.5 0a2.25 2.25 0 00-1.883 2.542l.857 6a2.25 2.25 0 002.227 1.932H19.05a2.25 2.25 0 002.227-1.932l.857-6a2.25 2.25 0 00-1.883-2.542m-16.5 0V6A2.25 2.25 0 016 3.75h3.879a1.5 1.5 0 011.06.44l2.122 2.12a1.5 1.5 0 001.06.44H18A2.25 2.25 0 0120.25 9v.776"/>',
'copy': '<path stroke-linecap="round" stroke-linejoin="round" d="M15.75 17.25v3.375c0 .621-.504 1.125-1.125 1.125h-9.75a1.125 1.125 0 01-1.125-1.125V7.875c0-.621.504-1.125 1.125-1.125H6.75a9.06 9.06 0 011.5.124m7.5 10.376h3.375c.621 0 1.125-.504 1.125-1.125V11.25c0-4.46-3.243-8.161-7.5-8.876a9.06 9.06 0 00-1.5-.124H9.375c-.621 0-1.125.504-1.125 1.125v3.5m7.5 10.375H9.375a1.125 1.125 0 01-1.125-1.125v-9.25m12 6.625v-1.875a3.375 3.375 0 00-3.375-3.375h-1.5a1.125 1.125 0 01-1.125-1.125v-1.5a3.375 3.375 0 00-3.375-3.375H9.75"/>',
'plugins': '<path stroke-linecap="round" stroke-linejoin="round" d="M14.25 6.087c0-.355.186-.676.401-.959.221-.29.349-.634.349-1.003 0-1.036-1.007-1.875-2.25-1.875s-2.25.84-2.25 1.875c0 .369.128.713.349 1.003.215.283.401.604.401.959v0a.64.64 0 01-.657.643 48.39 48.39 0 01-4.163-.3c.186 1.613.293 3.25.315 4.907a.656.656 0 01-.658.663v0c-.355 0-.676-.186-.959-.401a1.647 1.647 0 00-1.003-.349c-1.036 0-1.875 1.007-1.875 2.25s.84 2.25 1.875 2.25c.369 0 .713-.128 1.003-.349.283-.215.604-.401.959-.401v0c.31 0 .555.26.532.57a48.039 48.039 0 01-.642 5.056c1.518.19 3.058.309 4.616.354a.64.64 0 00.657-.643v0c0-.355-.186-.676-.401-.959a1.647 1.647 0 01-.349-1.003c0-1.035 1.008-1.875 2.25-1.875 1.243 0 2.25.84 2.25 1.875 0 .369-.128.713-.349 1.003-.215.283-.401.604-.401.959v0c0 .333.277.599.61.58a48.1 48.1 0 005.427-.63 48.05 48.05 0 00.582-4.717.532.532 0 00-.533-.57v0c-.355 0-.676.186-.959.401-.29.221-.634.349-1.003.349-1.035 0-1.875-1.007-1.875-2.25s.84-2.25 1.875-2.25c.37 0 .713.128 1.003.349.283.215.604.401.959.401v0a.656.656 0 00.658-.663 48.422 48.422 0 00-.37-5.36c-1.886.342-3.81.574-5.766.689a.578.578 0 01-.61-.58v0z"/>',
```

In `cardMenuTooltips` (around line 99), add:

```javascript
'open': 'Open with your default application',
'open-with': 'Choose an application to open this clip',
```

**Step 2: Replace `renderCardMenu` with a function that builds the item list and calls `ContextMenu.open`**

Replace the existing `renderCardMenu` function (lines 115-221) with:

```javascript
function buildMenuItemList(clip) {
    const ct = clip.content_type || '';
    const items = [];

    // Open actions
    items.push({ id: 'open', label: 'Open', icon: 'open', tooltip: cardMenuTooltips['open'] });
    items.push({ id: 'open-with', label: 'Open With\u2026', icon: 'open-with', tooltip: cardMenuTooltips['open-with'] });

    items.push({ type: 'divider' });

    // Copy submenu
    const copyChildren = [
        { id: 'copy-path', label: 'Path', icon: 'copy-path', tooltip: cardMenuTooltips['copy-path'] },
        { id: 'copy-file', label: 'File', icon: 'copy-file', tooltip: cardMenuTooltips['copy-file'] },
    ];
    if (ct.startsWith('text/') || ct === 'application/json' || ct.startsWith('image/')) {
        copyChildren.push({ id: 'copy-contents', label: 'Contents', icon: 'copy-contents', tooltip: cardMenuTooltips['copy-contents'] });
    }
    items.push({ type: 'submenu', label: 'Copy', icon: 'copy', children: copyChildren });

    items.push({ id: 'save-file', label: 'Save', icon: 'save', tooltip: cardMenuTooltips['save-file'] });

    if (isEditableType(ct)) {
        items.push({ id: 'edit', label: 'Edit', icon: 'edit', tooltip: cardMenuTooltips['edit'] });
    }

    items.push({ id: 'tags', label: 'Tags', icon: 'tags', tooltip: cardMenuTooltips['tags'] });
    items.push({ id: 'metadata', label: 'Metadata', icon: 'metadata', tooltip: cardMenuTooltips['metadata'] });

    if (clip.expires_at) {
        items.push({ id: 'cancel-expiration', label: 'Cancel Expiration', icon: 'cancel-expiration', tooltip: cardMenuTooltips['cancel-expiration'] });
    } else {
        items.push({ id: 'set-expiration', label: 'Set Expiration', icon: 'set-expiration', tooltip: cardMenuTooltips['set-expiration'] });
    }

    items.push({
        id: 'archive',
        label: isViewingArchive ? 'Restore' : 'Archive',
        icon: isViewingArchive ? 'restore' : 'archive',
        tooltip: isViewingArchive ? 'Move back from archive' : cardMenuTooltips['archive'],
    });

    if (clip.duplicate_count > 0) {
        items.push({ id: 'merge-duplicates', label: 'Merge Duplicates', icon: 'merge', tooltip: cardMenuTooltips['merge-duplicates'] });
    }

    items.push({ id: 'delete', label: 'Delete', icon: 'delete', danger: true, tooltip: cardMenuTooltips['delete'] });

    // Plugin actions submenu
    if (pluginUIActions && pluginUIActions.card_actions && pluginUIActions.card_actions.length > 0) {
        const applicableActions = pluginUIActions.card_actions.filter(action =>
            shouldShowPluginAction(action, clip)
        );
        if (applicableActions.length > 0) {
            items.push({ type: 'divider' });
            const pluginChildren = applicableActions.map(action => ({
                id: 'plugin',
                label: escapeHTML(action.label),
                pluginId: action.plugin_id,
                actionId: action.id,
                hasOptions: action.options && action.options.length > 0,
                iconHtml: typeof getPluginIcon === 'function'
                    ? (getPluginIcon(action.icon) || getPluginIcon('bolt') || '')
                    : '',
            }));
            items.push({ type: 'submenu', label: 'Plugins', icon: 'plugins', children: pluginChildren });
        }
    }

    // Resolve icon HTML for non-plugin items
    items.forEach(item => {
        if (item.type === 'divider') return;
        if (!item.iconHtml && item.icon) {
            item.iconHtml = getMenuIcon(item.icon);
        }
        if (item.children) {
            item.children.forEach(child => {
                if (!child.iconHtml && child.icon) {
                    child.iconHtml = getMenuIcon(child.icon);
                }
            });
        }
    });

    return items;
}

function renderCardMenu(clipId, button, clip) {
    const items = buildMenuItemList(clip);
    return ContextMenu.open(items, clipId, button, (action, id, item) => {
        if (action === 'plugin') {
            handlePluginAction(item, id);
        } else {
            handleCardAction(action, id, button);
        }
    });
}
```

**Step 3: Remove the old `positionCardMenu`, `setupMenuKeyboard`, `closeCardMenu` functions**

Replace `closeCardMenu` (lines 304-316) with a delegating wrapper:

```javascript
function closeCardMenu() {
    ContextMenu.close();
}
```

Remove `positionCardMenu` (lines 223-273) and `setupMenuKeyboard` (lines 276-302) — they are now in `context-menu.js`.

**Step 4: Add new action handlers in `handleCardAction`**

In `handleCardAction` (the switch statement around line 319), add cases for `open` and `open-with`:

```javascript
case 'open':
    try {
        await window.go.main.App.OpenClipWithDefaultApp(id);
    } catch (err) {
        showToast('Failed to open clip.');
    }
    break;
case 'open-with':
    try {
        const appPath = await window.go.main.App.ChooseApplication();
        if (appPath) {
            await window.go.main.App.OpenClipWithApp(id, appPath);
        }
    } catch (err) {
        showToast('Failed to open clip.');
    }
    break;
```

**Step 5: Extract `handlePluginAction` helper if not already separate**

The plugin action dispatching that was inline in the menu click handler needs to be callable from `renderCardMenu`'s `onAction` callback. Check if there's an existing function — if not, extract the plugin action dispatch logic into a function:

```javascript
function handlePluginAction(item, clipId) {
    // item has .pluginId, .actionId, .hasOptions
    // Dispatch to existing plugin action handling (same as before)
    const triggerButton = null; // no specific button reference needed
    handleCardAction('plugin', clipId, triggerButton, item);
}
```

The existing `handleCardAction` already handles `action === 'plugin'` if wired through event delegation. Verify the current click-delegation approach in `app.js` still works, or wire it through the onAction callback.

**Step 6: Verify by running the app**

Run: `make dev`
- Right-click a card → menu should show with Open, Open With, Copy ▸, Plugins ▸
- Hover Copy ▸ → submenu with Path, File, Contents
- Click actions → same behavior as before
- Escape closes menu

**Step 7: Commit**

```bash
git add frontend/js/ui.js
git commit -m "feat: refactor card menu to use ContextMenu with submenus"
```

---

### Task 6: Frontend — Refactor lightbox menu to use ContextMenu module

**Files:**
- Modify: `frontend/js/modals.js` (lines 750-923 area — `lightboxFileMenuTooltips`, `openLightboxFileMenu`, `closeLightboxFileMenu`, `handleLightboxFileAction`)

**Step 1: Replace `openLightboxFileMenu` to use `buildMenuItemList` and `ContextMenu.open`**

The lightbox menu should use the same `buildMenuItemList` from `ui.js` but with a lightbox-specific action handler. Replace the function (lines 761-859):

```javascript
function openLightboxFileMenu(trigger) {
    const clip = imageClips[currentLightboxIndex];
    if (!clip) return;

    const items = buildMenuItemList(clip);
    ContextMenu.open(items, clip.id, trigger, (action, clipId, item) => {
        if (action === 'plugin') {
            handlePluginAction(item, clipId);
        } else {
            handleLightboxFileAction(action);
        }
    });
}
```

**Step 2: Replace `closeLightboxFileMenu` to delegate to `ContextMenu.close`**

```javascript
function closeLightboxFileMenu(skipFocus) {
    ContextMenu.close();
}
```

Remove the old lightbox-specific menu rendering, positioning, and keyboard code that's now handled by `ContextMenu`.

**Step 3: Add `open` and `open-with` cases to `handleLightboxFileAction`**

In the switch statement (line 892), add:

```javascript
case 'open':
    try {
        await window.go.main.App.OpenClipWithDefaultApp(clip.id);
    } catch (err) {
        showToast('Failed to open clip.');
    }
    break;
case 'open-with':
    try {
        const appPath = await window.go.main.App.ChooseApplication();
        if (appPath) {
            await window.go.main.App.OpenClipWithApp(clip.id, appPath);
        }
    } catch (err) {
        showToast('Failed to open clip.');
    }
    break;
```

Make `handleLightboxFileAction` async (add `async` keyword).

**Step 4: Verify lightbox menu works**

Run: `make dev`
- Open lightbox → click file menu trigger → same menu structure
- Hover Copy ▸ → submenu opens
- Click Open → opens file with default app

**Step 5: Commit**

```bash
git add frontend/js/modals.js
git commit -m "feat: refactor lightbox menu to use shared ContextMenu module"
```

---

### Task 7: Frontend — Keyboard activation (Ctrl+Enter / ContextMenu key)

**Files:**
- Modify: `frontend/js/app.js` (shortcut registration section, around line 484+)

**Step 1: Register the context menu shortcut**

Add a new shortcut registration in the `registerShortcuts` section of `app.js`:

```javascript
ShortcutManager.register({
    id: 'clip-context-menu', label: 'Open Context Menu', category: 'clip',
    defaultKey: 'ctrl+Enter',
    context: 'clip',
    callback: () => {
        const focused = document.querySelector('.clip-focused');
        if (!focused) return;
        const clipId = focused.dataset.id;
        const menuBtn = focused.querySelector('[data-action="menu"]');
        if (menuBtn && clipId) {
            menuBtn.click();
        }
    }
});
```

Note: `ctrl+Enter` maps to Ctrl on all platforms (not Cmd on Mac, since `mod+Enter` would map to Cmd). The `ContextMenu` key is automatically handled by the browser's `contextmenu` event, which is already captured on cards (line 842 in `ui.js`). No separate shortcut needed for the Windows ContextMenu key — the existing `contextmenu` listener handles it.

**Step 2: Verify**

Run: `make dev`
- Focus a card with arrow keys → press Ctrl+Enter → context menu opens
- Press ↓/↑ → navigate items
- Press → on Copy ▸ → submenu opens
- Press ← → returns to parent
- Press Escape → closes

**Step 3: Commit**

```bash
git add frontend/js/app.js
git commit -m "feat: add Ctrl+Enter shortcut to open clip context menu"
```

---

### Task 8: Update selectors and test fixtures

**Files:**
- Modify: `e2e/helpers/selectors.ts` (update `cardMenu` section)
- Modify: `e2e/fixtures/test-fixtures.ts` (update `openCardMenu` and add submenu helpers)

**Step 1: Update selectors**

In `e2e/helpers/selectors.ts`, update the `cardMenu` section (lines 69-86):

```typescript
cardMenu: {
    dropdown: '.card-menu-dropdown',
    // Top-level items
    open: '.card-menu-dropdown [data-action="open"]',
    openWith: '.card-menu-dropdown [data-action="open-with"]',
    // Submenu triggers
    copyTrigger: '.card-menu-dropdown .card-menu-submenu-trigger:has(span:text("Copy"))',
    pluginsTrigger: '.card-menu-dropdown .card-menu-submenu-trigger:has(span:text("Plugins"))',
    // Copy submenu items (inside .card-menu-submenu)
    copyPath: '.card-menu-submenu [data-action="copy-path"]',
    copyFile: '.card-menu-submenu [data-action="copy-file"]',
    copyContents: '.card-menu-submenu [data-action="copy-contents"]',
    // Other top-level items
    save: '.card-menu-dropdown [data-action="save-file"]',
    edit: '.card-menu-dropdown [data-action="edit"]',
    tags: '.card-menu-dropdown [data-action="tags"]',
    archive: '.card-menu-dropdown [data-action="archive"]',
    setExpiration: '.card-menu-dropdown [data-action="set-expiration"]',
    cancelExpiration: '.card-menu-dropdown [data-action="cancel-expiration"]',
    delete: '.card-menu-dropdown [data-action="delete"]',
    mergeDuplicates: '.card-menu-dropdown [data-action="merge-duplicates"]',
    metadata: '.card-menu-dropdown [data-action="metadata"]',
    // Plugin items (inside plugins submenu)
    pluginAction: '.card-menu-submenu [data-action="plugin"]',
    divider: '.card-menu-dropdown .card-menu-divider',
    // Submenu
    submenu: '.card-menu-submenu',
    submenuItem: '.card-menu-submenu [role="menuitem"]',
},
```

Note: The `:has(span:text("Copy"))` selector may need adjustment for Playwright — Playwright supports `:has-text()` pseudo-selector. Use the approach that works: `'.card-menu-submenu-trigger >> text=Copy'` or a data attribute if cleaner. Consider adding `data-submenu="copy"` and `data-submenu="plugins"` to the trigger buttons in `context-menu.js` for testability.

**Step 2: Add submenu data attributes in `context-menu.js`**

In the `createSubmenuTrigger` function, add a data attribute:

```javascript
trigger.dataset.submenu = item.submenuId || item.label.toLowerCase();
```

And in `buildMenuItemList` in `ui.js`, add `submenuId: 'copy'` and `submenuId: 'plugins'` to the submenu items.

Then update selectors to use:

```typescript
copyTrigger: '.card-menu-dropdown [data-submenu="copy"]',
pluginsTrigger: '.card-menu-dropdown [data-submenu="plugins"]',
```

**Step 3: Add helper methods to `AppHelper` in `test-fixtures.ts`**

After the existing `openCardMenu` method (line 1913):

```typescript
async hoverCopySubmenu(filename: string): Promise<void> {
    await this.openCardMenu(filename);
    const trigger = this.page.locator(selectors.cardMenu.copyTrigger);
    await trigger.hover();
    await this.page.locator(selectors.cardMenu.submenu).waitFor({ state: 'visible', timeout: 3000 });
}

async hoverPluginsSubmenu(filename: string): Promise<void> {
    await this.openCardMenu(filename);
    const trigger = this.page.locator(selectors.cardMenu.pluginsTrigger);
    await trigger.hover();
    await this.page.locator(selectors.cardMenu.submenu).waitFor({ state: 'visible', timeout: 3000 });
}
```

**Step 4: Commit**

```bash
git add e2e/helpers/selectors.ts e2e/fixtures/test-fixtures.ts frontend/js/context-menu.js frontend/js/ui.js
git commit -m "feat: update selectors and test fixtures for submenu structure"
```

---

### Task 9: Update existing e2e tests for new menu structure

**Files:**
- Modify: `e2e/tests/clips/copy-actions.spec.ts`
- Modify: `e2e/tests/plugins/ui-actions.spec.ts`
- Modify: `e2e/tests/clips/tooltips.spec.ts`

**Step 1: Update `copy-actions.spec.ts`**

Tests that use `selectors.cardMenu.copyFile`, `selectors.cardMenu.copyContents`, etc. now need to hover the Copy submenu first. Example update:

```typescript
// Before:
await app.openCardMenu(filename);
const copyFileBtn = app.page.locator(selectors.cardMenu.copyFile);
await expect(copyFileBtn).toBeVisible();

// After:
await app.hoverCopySubmenu(filename);
const copyFileBtn = app.page.locator(selectors.cardMenu.copyFile);
await expect(copyFileBtn).toBeVisible();
```

Apply this pattern to all tests in `copy-actions.spec.ts` that access copy items. The lightbox tests similarly need to open the lightbox file menu and hover the Copy trigger.

**Step 2: Update `ui-actions.spec.ts`**

Tests that check for plugin actions in the card menu need to hover the Plugins submenu:

```typescript
// Before:
await app.openCardMenu(path.basename(imagePath));
const pluginItems = menu.locator('[data-action="plugin"]');

// After:
await app.hoverPluginsSubmenu(path.basename(imagePath));
const pluginItems = app.page.locator(selectors.cardMenu.pluginAction);
```

The divider test needs updating — the divider before plugins is now before the Plugins submenu trigger, still in the main menu.

**Step 3: Update `tooltips.spec.ts`**

If tooltip tests reference copy actions directly, they need submenu hover first.

**Step 4: Run all tests**

Run: `cd e2e && npm test`
Expected: All tests pass

**Step 5: Commit**

```bash
git add e2e/tests/
git commit -m "test: update existing tests for submenu menu structure"
```

---

### Task 10: New e2e tests — Menu structure and Open actions

**Files:**
- Create: `e2e/tests/clips/context-menu.spec.ts`

**Step 1: Write menu structure tests**

```typescript
import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage, generateTestText } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

test.describe('Context Menu Structure', () => {
    test('should show Open and Open With at the top of the menu', async ({ app }) => {
        const imagePath = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(imagePath);

        await app.openCardMenu(path.basename(imagePath));
        const menu = app.page.locator(selectors.cardMenu.dropdown);
        const items = menu.locator(':scope > [role="menuitem"]');

        // First two items should be Open and Open With
        await expect(items.nth(0)).toHaveAttribute('data-action', 'open');
        await expect(items.nth(1)).toHaveAttribute('data-action', 'open-with');
    });

    test('should show Copy submenu trigger', async ({ app }) => {
        const imagePath = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(imagePath);

        await app.openCardMenu(path.basename(imagePath));
        const copyTrigger = app.page.locator(selectors.cardMenu.copyTrigger);
        await expect(copyTrigger).toBeVisible();
        await expect(copyTrigger).toHaveText(/Copy.*▸/);
    });

    test('should expand Copy submenu on hover with Path and File items', async ({ app }) => {
        const imagePath = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(imagePath);

        await app.hoverCopySubmenu(path.basename(imagePath));
        await expect(app.page.locator(selectors.cardMenu.copyPath)).toBeVisible();
        await expect(app.page.locator(selectors.cardMenu.copyFile)).toBeVisible();
    });

    test('should show Contents in Copy submenu for text clips', async ({ app }) => {
        const textPath = await createTempFile(generateTestText('ctx-test'), 'txt');
        await app.uploadFile(textPath);

        await app.hoverCopySubmenu(path.basename(textPath));
        await expect(app.page.locator(selectors.cardMenu.copyContents)).toBeVisible();
    });

    test('should NOT show Plugins submenu when no plugins installed', async ({ app }) => {
        const imagePath = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(imagePath);

        await app.openCardMenu(path.basename(imagePath));
        const pluginsTrigger = app.page.locator(selectors.cardMenu.pluginsTrigger);
        await expect(pluginsTrigger).not.toBeVisible();
    });

    test('should close submenu when hovering away', async ({ app }) => {
        const imagePath = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(imagePath);

        await app.hoverCopySubmenu(path.basename(imagePath));
        const submenu = app.page.locator(selectors.cardMenu.submenu);
        await expect(submenu).toBeVisible();

        // Hover away to a different menu item
        await app.page.locator(selectors.cardMenu.delete).hover();
        await submenu.waitFor({ state: 'hidden', timeout: 3000 });
    });

    test('should show same menu structure in lightbox', async ({ app }) => {
        const imagePath = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(imagePath);

        await app.openLightbox(path.basename(imagePath));
        const trigger = app.page.locator('#lightbox-file-menu-trigger');
        await trigger.click();

        const menu = app.page.locator(selectors.cardMenu.dropdown);
        await expect(menu).toBeVisible();

        const openItem = menu.locator('[data-action="open"]');
        await expect(openItem).toBeVisible();
    });
});

test.describe('Context Menu Keyboard Navigation', () => {
    test('should open menu with Ctrl+Enter on focused card', async ({ app }) => {
        const imagePath = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(imagePath);

        // Focus the card using keyboard navigation
        await app.page.keyboard.press('ArrowDown');
        await app.page.keyboard.press('Control+Enter');

        const menu = app.page.locator(selectors.cardMenu.dropdown);
        await expect(menu).toBeVisible();
    });

    test('should navigate menu items with arrow keys', async ({ app }) => {
        const imagePath = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(imagePath);

        await app.openCardMenu(path.basename(imagePath));

        // First item should be focused (Open)
        const openItem = app.page.locator(selectors.cardMenu.open);
        await expect(openItem).toBeFocused();

        // Arrow down to Open With
        await app.page.keyboard.press('ArrowDown');
        const openWithItem = app.page.locator(selectors.cardMenu.openWith);
        await expect(openWithItem).toBeFocused();
    });

    test('should open Copy submenu with ArrowRight', async ({ app }) => {
        const imagePath = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(imagePath);

        await app.openCardMenu(path.basename(imagePath));

        // Navigate to Copy trigger (skip Open, Open With, divider)
        // The exact number of ArrowDown presses depends on item count
        // Navigate to the Copy submenu trigger
        const copyTrigger = app.page.locator(selectors.cardMenu.copyTrigger);
        await copyTrigger.focus();
        await app.page.keyboard.press('ArrowRight');

        const submenu = app.page.locator(selectors.cardMenu.submenu);
        await expect(submenu).toBeVisible();
    });

    test('should close submenu with ArrowLeft', async ({ app }) => {
        const imagePath = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(imagePath);

        await app.openCardMenu(path.basename(imagePath));
        const copyTrigger = app.page.locator(selectors.cardMenu.copyTrigger);
        await copyTrigger.focus();
        await app.page.keyboard.press('ArrowRight');

        const submenu = app.page.locator(selectors.cardMenu.submenu);
        await expect(submenu).toBeVisible();

        await app.page.keyboard.press('ArrowLeft');
        await submenu.waitFor({ state: 'hidden', timeout: 3000 });
        await expect(copyTrigger).toBeFocused();
    });

    test('should close menu with Escape', async ({ app }) => {
        const imagePath = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(imagePath);

        await app.openCardMenu(path.basename(imagePath));
        const menu = app.page.locator(selectors.cardMenu.dropdown);
        await expect(menu).toBeVisible();

        await app.page.keyboard.press('Escape');
        await menu.waitFor({ state: 'hidden', timeout: 3000 });
    });
});

test.describe('Open Actions', () => {
    test('should show Open item in card menu', async ({ app }) => {
        const imagePath = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(imagePath);

        await app.openCardMenu(path.basename(imagePath));
        const openItem = app.page.locator(selectors.cardMenu.open);
        await expect(openItem).toBeVisible();
        await expect(openItem).toHaveText(/Open/);
    });

    test('should show Open With item in card menu', async ({ app }) => {
        const imagePath = await createTempFile(generateTestImage(), 'png');
        await app.uploadFile(imagePath);

        await app.openCardMenu(path.basename(imagePath));
        const openWithItem = app.page.locator(selectors.cardMenu.openWith);
        await expect(openWithItem).toBeVisible();
    });
});
```

**Step 2: Run the new tests**

Run: `cd e2e && npx playwright test tests/clips/context-menu.spec.ts`
Expected: All pass

**Step 3: Run full test suite**

Run: `cd e2e && npm test`
Expected: All pass

**Step 4: Commit**

```bash
git add e2e/tests/clips/context-menu.spec.ts
git commit -m "test: add context menu structure, keyboard, and open action tests"
```

---

### Task 11: Final verification and cleanup

**Step 1: Run full e2e test suite**

Run: `cd e2e && npm test`
Expected: All tests pass

**Step 2: Manual verification checklist**

- [ ] Right-click card → menu opens with Open, Open With at top
- [ ] Hover Copy ▸ → submenu appears after short delay with Path, File, Contents
- [ ] Hover away → submenu closes after short delay
- [ ] Diagonal mouse movement from trigger to submenu doesn't accidentally close it
- [ ] Click Copy > Path → copies temp file path (toast appears)
- [ ] Click Open → file opens in default app
- [ ] Click Open With → file picker dialog appears
- [ ] Plugins ▸ submenu shows when plugin with card actions is installed
- [ ] Plugins ▸ is hidden when no plugins installed
- [ ] Lightbox file menu has identical structure
- [ ] Ctrl+Enter opens menu on focused card
- [ ] Arrow key navigation works in menu and submenus
- [ ] Escape closes menu/submenu appropriately
- [ ] Menu dismisses on outside click

**Step 3: Commit any final fixes**

If any issues found during manual testing, fix and commit.
