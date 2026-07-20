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
