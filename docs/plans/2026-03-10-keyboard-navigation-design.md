# Keyboard Navigation Design

Full keyboard navigability for mahpastes using roving tabindex as a standard pattern, consistent focus management, and TDD-driven e2e test coverage.

## Decisions

- **Roving tabindex everywhere**: All list-like structures are a single Tab stop. Arrow keys navigate within. Tab moves between sections.
- **Shared module**: `frontend/js/roving-tabindex.js` — reusable class adopted by all list/grid components.
- **Focus-on-open**: Modal containers focused for dialogs with `aria-labelledby`; primary action focused for confirm/action dialogs; first input for forms.
- **Focus trapping**: Shared `trapFocus()` utility applied to all modals (not just lightbox).
- **Focus restoration**: Every modal saves and restores `document.activeElement`.
- **Focus indicators**: Global `*:focus-visible` outline in stone-400, replacing the `clip-focused` CSS class with real DOM focus.
- **Tests inline**: Keyboard tests live in existing feature test files, not a separate directory.
- **TDD**: Red-green-refactor for every component.

## RovingTabindex Module

`frontend/js/roving-tabindex.js` — constructor options:

| Option | Type | Description |
|--------|------|-------------|
| `container` | Element | DOM element containing the items |
| `itemSelector` | string | CSS selector for focusable items |
| `orientation` | `'horizontal'` \| `'vertical'` \| `'grid'` | Arrow key behavior |
| `columnsOrGetColumns` | number \| function | Column count for grid orientation |
| `wrap` | boolean | Wrap at edges |
| `onFocus(item)` | function | Callback when item receives focus |
| `onActivate(item)` | function | Callback on Enter/Space |

Behavior:
- `tabindex="0"` on active item, `tabindex="-1"` on all others
- Arrow keys move focus based on orientation
- Home/End jump to first/last
- Tab leaves the container (next/previous Tab stop)
- `update()` re-indexes when items change
- Restores focus to last-focused item when container regains focus

### Components adopting it

| Component | Orientation | Items |
|-----------|------------|-------|
| Gallery grid | grid | `.clip-card` |
| View tabs | horizontal | `[role="tab"]` |
| Tag filter checkboxes | vertical | checkbox labels |
| Watch folder cards | vertical | folder cards |
| Bulk action toolbar | horizontal | toolbar buttons |
| Lightbox action buttons | horizontal | action buttons |
| Plugin list | vertical | plugin cards |
| Settings shortcut list | vertical | shortcut rows |

## Focus Management for Modals

### Focus-on-open

- Dialogs with `aria-labelledby`: focus the modal container
- Action dialogs (confirm, delete): focus primary action button
- Form dialogs (tag create, expiration): focus first input

### Focus trapping

Shared `trapFocus(container)` utility traps Tab/Shift+Tab within all focusable elements (buttons, inputs, checkboxes, links). Applied to all modals: lightbox, comparison, settings, plugins, confirm, editor.

### Focus restoration

Every modal saves `document.activeElement` before opening, restores on close.

## Visible Focus Indicators

Global CSS:
```css
*:focus-visible {
  outline: 2px solid #a8a29e; /* stone-400 */
  outline-offset: 2px;
  border-radius: 4px;
}
```

Overrides:
- Gallery clips: existing clip border style via `:focus-visible`
- Buttons on dark backgrounds: lighter ring (stone-300)
- Inputs: keep existing `focus:border-stone-400` Tailwind pattern

Remove `clip-focused` CSS class — replaced by `:focus-visible` on clip cards with real DOM focus.

## ShortcutManager Changes

- Remove `focusedClipIndex` — gallery `RovingTabindex` owns focus state via DOM
- `clip` context derived from `document.activeElement` matching `.clip-card`
- Shortcut actions get target clip from `document.activeElement`
- `clearClipFocus()` becomes `gallery.rovingTabindex.reset()`

## E2E Test Additions

Tests added inline in existing feature test files:

| Test File | Keyboard Tests |
|-----------|---------------|
| `clips/*.spec.ts` | Tab into gallery, arrow navigate, Enter to open, keyboard delete/archive |
| `tags/*.spec.ts` | Arrow navigate tag filter, Space to toggle, Escape to close |
| `shortcuts/shortcuts.spec.ts` | Roving tabindex (Tab in/out, Home/End, wrap) |
| `images/lightbox.spec.ts` | Full focus trap, focus-on-open, focus restore |
| `watch/*.spec.ts` | Tab into folder list, arrow navigate, activate via keyboard |
| `bulk/*.spec.ts` | Keyboard multi-select, toolbar roving tabindex |
| `search/*.spec.ts` | / to search, Tab to gallery, arrow to result, Enter |

AppHelper additions:
- `expectFocusOn(selector)` — asserts active element matches selector
- `tabTo(selector)` — presses Tab until selector focused (with max iterations)

## Implementation Order

### Phase 1 — Infrastructure
1. `RovingTabindex` module
2. `trapFocus()` utility
3. Global `focus-visible` CSS
4. AppHelper keyboard test utilities

### Phase 2 — Gallery grid
5. Failing tests: Tab into grid, arrow nav, Enter to open, Tab out
6. Adopt `RovingTabindex` on gallery
7. Rewire ShortcutManager to use DOM focus
8. Replace `clip-focused` with `:focus-visible`
9. Verify existing shortcut tests pass

### Phase 3 — Modals
10. Failing tests: focus-on-open, Tab trap, focus restore
11. Apply focus trap to all modals
12. Apply focus-on-open and focus-restore
13. Standardize across all modals

### Phase 4 — Remaining list components
14. View tabs
15. Tag filter dropdown
16. Watch folder list
17. Bulk action toolbar
18. Plugin list
19. Each with failing test first

### Phase 5 — Full workflow tests
20. End-to-end keyboard-only workflows without mouse
