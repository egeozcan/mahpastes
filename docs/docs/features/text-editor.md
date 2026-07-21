---
sidebar_position: 4
---

# Text Editor

Edit text and code clips directly within mahpastes. Modify content, fix typos, or update code snippets without leaving the app.

## Opening the Editor

1. Click the menu button (three dots) on any text-based clip and select **Edit**
2. The editor opens full-screen with a dark theme (dark background, light text)
3. Make your changes
4. Click **Save** to overwrite the original, or **Save As** to create a new clip with the changes

:::note
Clicking a text preview in the gallery also opens the editor directly.
:::

![Text editor](/img/screenshots/text-editor.png)

Text-based clips include:
- Plain text
- Code (any language)
- JSON
- HTML
- Markdown (`.md` and `.markdown`, case-insensitive)

## Markdown Preview

Markdown clips open in **Preview** by default. Use the **Preview** and **Edit** tabs—or <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">Shift</span> + <span className="keyboard-key">P</span>—to switch modes. Preview always renders the current editor text, including unsaved changes. A recovered unsaved draft opens in Edit so it cannot be mistaken for saved content.

Preview supports sanitized GitHub-Flavored Markdown: headings, lists, read-only task lists, tables, fenced code, strikethrough, and autolinks. Embedded HTML is restricted to safe semantic markup; scripts, forms, iframes, styles, event handlers, and author-supplied classes or IDs are removed. Mermaid, math, footnotes, and syntax highlighting are not included.

### Links and images

- Web and email links open with the operating system's default handler. Unsafe URL schemes are blocked.
- Relative links resolve to clips through the Markdown clip's exact tags and descendant tag paths. Parent (`..`) traversal is not supported. Ambiguous matches let you choose a clip.
- Valid PNG, JPEG, GIF, and WebP clips referenced through the same tag paths render inline. SVG is not rendered inline.
- HTTPS images show their full URL and a **Load Image** button before the first download. HTTP images remain external links only.
- Approved remote images are cached by exact URL for up to one hour. A valid cache hit may display automatically. Clear the cache from **Maintenance → Markdown Image Cache**.

Images are limited to 15 MB each, 100 MB per Preview, and 256 image references, with additional dimension, decoded-pixel, and GIF-frame limits. Markdown source above 2 MB remains editable but is not rendered. Invalid UTF-8 Markdown retains its original bytes but cannot be previewed or edited in-app.

:::note Raw content is unchanged
Markdown rendering is a desktop viewing feature. Downloads, REST responses, public links, tag servers, and `mp clip data` return the original Markdown bytes.
:::

## Editor Features

### Monospace Font

All text displays in IBM Plex Mono for:
- Consistent character width
- Easy code reading
- Proper alignment of structured data

### Full Content View

Unlike the gallery preview (limited to 500 characters), the editor shows the complete content.

### Preserved Formatting

- Whitespace is preserved exactly
- Indentation remains intact
- Line breaks are maintained

## Editing Text

### Basic Operations

Standard text editing operations work as expected:
- Type to insert text
- Backspace/Delete to remove
- Select text with mouse or keyboard
- Cut, copy, paste with standard shortcuts

### Keyboard Shortcuts

| Shortcut | Action |
|----------|--------|
| <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">A</span> | Select all |
| <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">C</span> | Copy |
| <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">V</span> | Paste |
| <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">X</span> | Cut |
| <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">Z</span> | Undo |
| <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">Shift</span> + <span className="keyboard-key">Z</span> | Redo |
| <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">Y</span> | Redo (alternative) |
| <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">S</span> | Save as new clip |
| <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">Shift</span> + <span className="keyboard-key">P</span> | Toggle Markdown Preview/Edit |
| <span className="keyboard-key">Esc</span> | Close editor |

## Working with Different Content Types

### Plain Text

Edit notes, messages, or any plain text content.

**Example uses:**
- Fix typos in saved notes
- Update copied messages
- Modify text before re-copying

### Code

Edit code snippets of any programming language.

**Tips:**
- Indentation is preserved
- Syntax is not highlighted (plain text view)
- Great for quick edits to snippets

### JSON

Edit JSON content with proper formatting.

**Tips:**
- Validate JSON after editing
- Keep proper bracket matching
- Maintain correct quote usage

```json
{
  "name": "example",
  "value": 42
}
```

### HTML

Edit HTML source code directly.

**Tips:**
- Tags display as source (not rendered)
- Good for fixing markup issues
- Check syntax before saving

## Saving Changes

### Save (Overwrite)

Click **Save** to overwrite the original clip with your edits. The filename remains the same and the content type is preserved.

### Save as New Clip

Click **Save As** (or press <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">S</span>) to create a new clip with your edits. The original clip is preserved unchanged, and the new clip is saved with `_edited` appended to the filename.

:::note Content Type
The content type (text/plain, application/json, etc.) is preserved when saving with either method.
:::

### Cancel

Click the close button (X) or press <span className="keyboard-key">Esc</span> to close the editor. If the content has changed since you opened it, mahpastes asks you to confirm before discarding the unsaved changes.

## Use Cases

### Fix Typos

1. Open clip in editor
2. Find and fix the typo
3. Save
4. Copy the corrected content

### Modify Code Snippets

1. Store frequently-used code as clips
2. Open and edit for different contexts
3. Copy the modified version
4. Original stays intact in archive (if archived)

### Update Configuration

1. Paste a config file (JSON, YAML, etc.)
2. Edit values as needed
3. Save the updated version
4. Copy path or content for use

### Combine Text

1. Open a text clip
2. Paste additional content from clipboard
3. Arrange as needed
4. Save the combined result

## Tips

### For Code

- Archive important snippets
- Use descriptive filenames when possible
- Edit copies, not originals (archive first)

### For Long Text

- The editor scrolls for long content
- Use search (in your browser) to find text if needed
- Consider splitting very long content

### For Structured Data

- Be careful with JSON/YAML syntax
- Preserve proper indentation
- Validate after editing if critical

## Limitations

- No syntax highlighting (plain textarea, not a code editor)
- No code completion
- No line numbers
- Single-file editing only
- Spellcheck is disabled

For complex code editing, use a dedicated code editor and paste the result into mahpastes.
