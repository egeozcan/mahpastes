# Keyboard Shortcuts System Design

## Summary

Add a comprehensive keyboard shortcuts system to mahpastes with a centralized shortcut manager, arrow-key grid navigation, a rebinding UI in Settings, and a quick-reference cheat sheet overlay.

## Decisions

- **Scope**: App-scoped only (no global/system-wide hotkeys)
- **Architecture**: Centralized shortcut manager (Approach A) — single `shortcuts.js` module with registry, one `keydown` handler, Map-based dispatch
- **Customization**: Fully rebindable via click-to-record in Settings
- **Cheat sheet**: Read-only overlay triggered by `?` key
- **Clip targeting**: Arrow-key grid navigation with visual focus indicator
- **Settings UI**: New section inside existing settings modal
- **Persistence**: JSON blob in SQLite `settings` table (overrides only)

## Shortcut Registry & Engine

### Action registration

Each action registered with the manager:

```
{
  id: "toggle-archive",
  label: "Toggle Archive View",
  category: "navigation",
  defaultKey: "a",
  context: "gallery",
  callback: () => toggleViewMode()
}
```

### Contexts

Contexts control when shortcuts are active:

- `global` — always active (unless typing in an input)
- `gallery` — main clip grid visible, no modal open
- `lightbox` — lightbox is open
- `bulk` — clips are selected (bulk toolbar visible)
- `clip` — a clip in the grid has keyboard focus
- `watch` — watch view is active

### Key combo format

`"mod+shift+k"` where `mod` = Cmd on macOS, Ctrl on Windows/Linux. Single keys: `"a"`, `"?"`, `"Escape"`.

### Conflict detection

When rebinding, the manager checks if the new combo is already used in an overlapping context and warns the user.

### Input guard

All shortcuts suppressed when focus is in `<input>`, `<textarea>`, or `[contenteditable]` (except Escape).

## Default Key Map

### Navigation

| Action | Key | Context |
|--------|-----|---------|
| Focus search | `/` | gallery |
| Toggle archive view | `a` | gallery |
| Open watch view | `w` | gallery |
| Open settings | `,` | global |
| Open plugins | `p` | gallery |
| Open nav drawer | `m` | global |

### Gallery / Clip Grid

| Action | Key | Context |
|--------|-----|---------|
| Upload/add clip | `n` | gallery |
| Select all | `Mod+a` | gallery |
| Clear selection | `Escape` | bulk |
| Clear temp files | `Mod+Shift+Delete` | gallery |
| Navigate grid | Arrow keys | gallery |
| Select/deselect focused clip | `Space` | clip |

### Clip Actions (focused clip)

| Action | Key | Context |
|--------|-----|---------|
| Open in lightbox | `Enter` | clip |
| Copy clip | `c` | clip |
| Delete clip | `d` | clip |
| Archive/unarchive | `e` | clip |
| Download clip | `Mod+d` | clip |
| Tag clip | `t` | clip |

### Lightbox

| Action | Key | Context |
|--------|-----|---------|
| Close | `Escape` | lightbox |
| Next image | `ArrowRight` | lightbox |
| Previous image | `ArrowLeft` | lightbox |
| Zoom in | `+` | lightbox |
| Zoom out | `-` | lightbox |
| Open editor | `e` | lightbox |
| Compare images | `Mod+k` | lightbox |

### Bulk Actions (clips selected)

| Action | Key | Context |
|--------|-----|---------|
| Bulk copy | `c` | bulk |
| Bulk delete | `d` | bulk |
| Bulk archive | `e` | bulk |
| Bulk download | `Mod+d` | bulk |
| Bulk tag | `t` | bulk |

### System

| Action | Key | Context |
|--------|-----|---------|
| Show cheat sheet | `?` | global |
| Close any modal | `Escape` | global |

## Arrow-Key Grid Navigation

- Visual focus indicator: `ring-2 ring-stone-400 ring-offset-2` outline + `scale-[1.02]` lift
- Navigation reads computed grid layout (`getComputedStyle` → `grid-template-columns`) to determine column count
- `ArrowRight` → next clip, `ArrowLeft` → previous, `ArrowDown` → same column next row, `ArrowUp` → same column previous row
- Wraps at edges (right from last column → first column next row)
- Scrolls focused clip into view smoothly
- Focus lost when modal opens, search is focused, or Escape pressed
- Any arrow key when no clip focused → focus first visible clip
- State: single `focusedClipIndex` integer, visual class (not DOM tabindex focus)

## Settings UI (Rebinding)

- New "KEYBOARD SHORTCUTS" section inside existing settings modal
- Shortcuts listed grouped by category
- Each row: action label (left), clickable `<kbd>` badge (right)
- Badge styled: `bg-stone-100 border border-stone-200 rounded px-2 py-0.5 text-xs font-mono`
- Click badge → recording mode (badge pulses, next keypress = new binding)
- Conflict warning inline: "Already used by [action] — override?"
- "Reset All to Defaults" button at bottom
- Persistence: only overrides stored as JSON in `settings` table with key `"keyboard_shortcuts"`

## Cheat Sheet Overlay

- Triggered by `?` key
- Full-window overlay: `bg-stone-900/80 backdrop-blur-sm`
- Centered panel with shortcuts in multi-column grid grouped by category
- Same `<kbd>` pill style as settings
- Dismisses on Escape, click outside, or `?` again
- Read-only — shows current bindings (including customizations)
- Footer: "Edit shortcuts in Settings" as clickable link → closes overlay, opens Settings to shortcuts section
- Purely frontend, reads from in-memory registry

## Migration of Existing Handlers

| Current handler | Migration |
|----------------|-----------|
| Escape to close drawer (app.js) | `close-modal` action, `global` context |
| Ctrl+C/Cmd+C smart copy (app.js) | `copy-clip` in `clip` context, `bulk-copy` in `bulk` context |
| Lightbox arrows/Escape (modals.js) | `lightbox-next`, `lightbox-prev`, `lightbox-close` in `lightbox` context |
| Confirm dialog Escape + Tab (app.js) | **Stays as-is** (transient, not rebindable) |

Old `addEventListener('keydown', ...)` calls removed from app.js and modals.js, replaced by registrations into the shortcut manager.

## File Structure

### New files

- `frontend/js/shortcuts.js` — shortcut manager module
- `e2e/tests/shortcuts/shortcuts.spec.ts` — e2e tests

### Modified files

- `frontend/index.html` — cheat sheet overlay markup, shortcuts settings section, script tag
- `frontend/js/app.js` — remove old keydown handlers, init shortcut manager, register actions
- `frontend/js/modals.js` — remove old lightbox keydown, register lightbox actions
- `frontend/js/settings.js` — shortcuts settings section rendering and rebinding UI
- `e2e/helpers/selectors.ts` — new selectors for shortcuts UI elements

### No backend changes needed

`App.GetSetting`/`App.SetSetting` already support the `settings` table key-value store.

## Testing

E2e tests covering:
- Default shortcuts fire correctly (navigation, clip actions)
- Grid navigation with arrow keys
- Cheat sheet opens/closes with `?`
- Shortcuts suppressed in input fields
- Rebinding via settings UI
- Conflict detection
- Reset to defaults
- Context awareness (gallery vs lightbox vs bulk)
