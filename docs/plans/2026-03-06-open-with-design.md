# Open With & Context Menu Restructure

**Date**: 2026-03-06
**Status**: Approved

## Summary

Add "Open" and "Open With..." actions to the clip context menu. Restructure the menu with submenus: Copy (Path, File, Contents) and Plugins (plugin actions). Add keyboard activation and full arrow-key navigation.

## Platforms

- macOS + Windows (Linux stubs)

## Backend

### New Go Methods on `App`

```go
func (a *App) OpenClipWithDefaultApp(id int64) error
func (a *App) OpenClipWithApp(id int64, appPath string) error
func (a *App) ChooseApplication() (string, error)
```

### Platform Implementation

**OpenClipWithDefaultApp**: Materializes temp file via `prepareClipTransferItem`, then:
- macOS: `exec.Command("open", path)`
- Windows: `exec.Command("cmd", "/c", "start", "", path)`

**OpenClipWithApp**: Same temp file, then:
- macOS: `exec.Command("open", "-a", appPath, path)`
- Windows: `exec.Command("cmd", "/c", "start", "", appPath, path)`

**ChooseApplication**: Shows file picker dialog:
- macOS: `runtime.OpenFileDialog` with `DefaultDirectory: "/Applications"`, filter `*.app`
- Windows: `runtime.OpenFileDialog` with `DefaultDirectory: "C:\\Program Files"`, filter `*.exe`

### File Organization

- `open_darwin.go` — macOS implementation
- `open_windows.go` — Windows implementation
- `open_other.go` — stub returning "not supported"

## Frontend — Context Menu Structure

### New Menu Layout

```
┌──────────────────────────────┐
│ Open                         │
│ Open With...                 │
├──────────────────────────────┤
│ Copy                       ▸ │
│   ┌────────────────────────┐ │
│   │ Path                   │ │
│   │ File                   │ │
│   │ Contents               │ │
│   └────────────────────────┘ │
│ Save                         │
│ Edit (if image)              │
│ Tags                         │
│ Metadata                     │
│ Set Expiration / Cancel      │
│ Archive / Restore            │
│ Merge Duplicates (if dupe)   │
│ Delete                       │
├──────────────────────────────┤
│ Plugins                    ▸ │
│   ┌────────────────────────┐ │
│   │ [plugin action 1]      │ │
│   │ [plugin action 2]      │ │
│   └────────────────────────┘ │
└──────────────────────────────┘
```

### Submenu Behavior

- Hover triggers submenu after ~150ms delay
- Submenu appears to the right, flipped left if near screen edge
- ~200ms close delay when leaving (diagonal cursor tolerance)
- Arrow indicator: `▸` right-aligned in parent item
- If no plugin actions exist, "Plugins ▸" and its divider are hidden

### Shared Rendering

- Extract `buildMenuItems(clip, context)` used by both card menu (`ui.js`) and lightbox menu (`modals.js`)
- Submenu open/close logic in a reusable helper (delay timers, position calculation, edge detection)

## Keyboard Support

### Menu Activation

- macOS: `Ctrl+Enter` on a focused/selected card opens context menu
- Windows: `ContextMenu` key on a focused/selected card opens context menu
- Menu positioned near the card's menu trigger button

### Navigation When Menu Is Open

- `↑` / `↓` — move focus between items (wraps)
- `→` on submenu trigger — opens submenu, focuses first item
- `←` inside submenu — closes submenu, returns focus to parent
- `Enter` / `Space` — activates focused item
- `Escape` — closes submenu if open, otherwise closes entire menu
- First item auto-focused when menu opens via keyboard

### Accessibility

- Menu items get `tabindex="-1"` (focusable but not in tab order)
- `role="menu"` / `role="menuitem"` attributes
- `aria-haspopup="true"` and `aria-expanded` on submenu triggers

## Action Handling

### Open

1. Click "Open" → `handleCardAction('open', clipId)`
2. Calls `App.OpenClipWithDefaultApp(clipId)`
3. Error → toast

### Open With...

1. Click "Open With..." → `handleCardAction('open-with', clipId)`
2. Calls `App.ChooseApplication()`
3. If non-empty, calls `App.OpenClipWithApp(clipId, appPath)`
4. Cancel or error → no-op / toast

### Copy Submenu

Same handlers as before (`copy-path`, `copy-file`, `copy-contents`), triggered from submenu.

### Plugins Submenu

Same handlers as before, rendered one level deeper. No logic changes.

### Menu Dismissal

- Clicking any leaf action closes entire menu
- Clicking outside closes everything
- Escape closes everything
- Opening another card's menu closes previous

## CSS

### New Classes

- `.card-menu-submenu-trigger` — parent item with `▸` arrow, `position: relative`
- `.card-menu-submenu` — absolutely positioned panel, same styling as `.card-menu-dropdown`
- Hover states managed via JS for delay control

## Testing

### New e2e Tests

- Open action: verify menu item exists and triggers backend call
- Open With...: verify triggers `ChooseApplication` dialog
- Copy submenu: hover "Copy ▸", verify submenu with Path/File/Contents
- Plugins submenu: install test plugin, verify actions under "Plugins ▸"
- Menu structure: verify item order, dividers, conditional items
- Keyboard: open via Ctrl+Enter, arrow navigation, Enter to activate, Escape to close
- Submenu positioning: verify no viewport overflow
- No plugins = no Plugins item
- Lightbox menu parity: same structure in lightbox

### Existing Test Updates

Tests referencing top-level "Copy Path", "Copy File", "Copy Contents" updated to navigate through Copy submenu.
