---
sidebar_position: 4
---

# Text Editor

Edit text and code clips directly within mahpastes. Modify content, fix typos, or update code snippets without leaving the app.

## Opening the Editor

1. Click the edit button on any text-based clip
2. The editor opens with the full content loaded
3. Make your changes
4. Click **Save** to update the clip

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
| <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">Y</span> | Redo |

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

### Save

Click **Save** to apply your changes. The clip is updated with the new content.

:::note Content Type
The content type (text/plain, application/json, etc.) is preserved when saving. Only the content itself is modified.
:::

### Cancel

Click **Cancel** or press <span className="keyboard-key">Esc</span> to discard changes and close the editor.

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

- No syntax highlighting
- No code completion
- No line numbers
- Single-file editing only

For complex code editing, use a dedicated code editor and paste the result into mahpastes.
