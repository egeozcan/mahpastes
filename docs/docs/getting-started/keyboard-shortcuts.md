---
sidebar_position: 3
---

# Keyboard Shortcuts

Complete reference for all keyboard shortcuts in mahpastes.

## Global Shortcuts

These work anywhere in the application.

| Shortcut | Action |
|----------|--------|
| <span className="keyboard-key">Cmd</span> + <span className="keyboard-key">V</span> | Paste from system clipboard |
| <span className="keyboard-key">Cmd</span> + <span className="keyboard-key">C</span> | Copy (context-dependent, see below) |
| <span className="keyboard-key">Esc</span> | Close current modal or dialog |

### Cmd+C Behavior

<span className="keyboard-key">Cmd</span> + <span className="keyboard-key">C</span> is context-dependent:

| Context | Action |
|---------|--------|
| Lightbox open | Copies clip contents (raw image or text) to clipboard |
| Clips selected in gallery | Copies selected clips as files to system clipboard |
| Text selected / input focused | Standard browser copy (not intercepted) |

## Image Editor Shortcuts

When the image editor is open:

| Shortcut | Action |
|----------|--------|
| <span className="keyboard-key">Cmd</span> + <span className="keyboard-key">Z</span> | Undo last action |
| <span className="keyboard-key">Cmd</span> + <span className="keyboard-key">Y</span> | Redo undone action |
| <span className="keyboard-key">Cmd</span> + <span className="keyboard-key">Shift</span> + <span className="keyboard-key">Z</span> | Redo (alternative) |
| <span className="keyboard-key">Cmd</span> + <span className="keyboard-key">S</span> | Save as new clip |
| <span className="keyboard-key">Esc</span> | Close editor |

:::tip Undo History
The image editor maintains up to 50 undo steps. You can undo and redo freely without losing your work.
:::

## Lightbox Shortcuts

When viewing an image in the lightbox:

| Shortcut | Action |
|----------|--------|
| <span className="keyboard-key">←</span> | Previous image |
| <span className="keyboard-key">→</span> | Next image |
| <span className="keyboard-key">Esc</span> | Close open menu first, then close lightbox |
| <span className="keyboard-key">Tab</span> / <span className="keyboard-key">Shift</span> + <span className="keyboard-key">Tab</span> | Cycle focus within lightbox buttons (focus trap) |

### Lightbox Menus (File/Plugin)

When a dropdown menu is open inside the lightbox:

| Shortcut | Action |
|----------|--------|
| <span className="keyboard-key">↓</span> / <span className="keyboard-key">↑</span> | Navigate menu items |
| <span className="keyboard-key">Esc</span> | Close menu |
| <span className="keyboard-key">Tab</span> | Close menu |

## Drop Zone

| Shortcut | Action |
|----------|--------|
| <span className="keyboard-key">Enter</span> or <span className="keyboard-key">Space</span> | Open file picker (when drop zone is focused) |

## Selection

| Shortcut | Action |
|----------|--------|
| Click checkbox | Add/remove clip from selection |
| Select All checkbox | Toggle selection of all visible clips |

## Plugin Modals

| Shortcut | Action |
|----------|--------|
| <span className="keyboard-key">Esc</span> | Close result modal first, then options modal, then plugins modal |

## Confirm Dialog

| Shortcut | Action |
|----------|--------|
| <span className="keyboard-key">Tab</span> / <span className="keyboard-key">Shift</span> + <span className="keyboard-key">Tab</span> | Cycle focus within dialog (focus trap) |
| <span className="keyboard-key">Esc</span> | Close dialog |

## Text Placement (Editor)

| Shortcut | Action |
|----------|--------|
| <span className="keyboard-key">Enter</span> | Confirm text placement |
| <span className="keyboard-key">Esc</span> | Cancel text placement |

## Tips for Efficient Use

### Quick Workflow

1. <span className="keyboard-key">Cmd</span> + <span className="keyboard-key">V</span> to paste
2. Click to view/edit
3. <span className="keyboard-key">Esc</span> to close and continue

### Image Annotation Flow

1. Open image editor
2. Select a tool from the toolbar (Brush, Line, Rectangle, Circle, Text, Eraser)
3. <span className="keyboard-key">Cmd</span> + <span className="keyboard-key">Z</span> if you make a mistake
4. <span className="keyboard-key">Cmd</span> + <span className="keyboard-key">S</span> to save

### Bulk Operations

1. Click checkboxes to select multiple clips
2. Use the action bar for bulk archive/delete/download

## Customization

Currently, keyboard shortcuts cannot be customized. This feature may be added in a future release.

If custom shortcuts are important to your workflow, please [open an issue](https://github.com/egeozcan/mahpastes/issues) on GitHub.
