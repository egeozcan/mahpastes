---
sidebar_position: 1
---

# Screenshot Workflow

Learn how to capture, annotate, and share screenshots efficiently using mahpastes.

## The Complete Workflow

```
Capture → Paste → Annotate → Share
```

This tutorial covers each step in detail.

## Step 1: Capture a Screenshot

Use your system's screenshot tools:

### macOS

| Shortcut | Action |
|----------|--------|
| <span className="keyboard-key">Cmd</span> + <span className="keyboard-key">Shift</span> + <span className="keyboard-key">3</span> | Full screen |
| <span className="keyboard-key">Cmd</span> + <span className="keyboard-key">Shift</span> + <span className="keyboard-key">4</span> | Selection |
| <span className="keyboard-key">Cmd</span> + <span className="keyboard-key">Shift</span> + <span className="keyboard-key">5</span> | Screenshot toolbar |
| <span className="keyboard-key">Cmd</span> + <span className="keyboard-key">Shift</span> + <span className="keyboard-key">4</span> + <span className="keyboard-key">Space</span> | Window |

**Tip:** Hold <span className="keyboard-key">Ctrl</span> while taking the screenshot to copy to clipboard instead of saving to file.

### Windows

| Shortcut | Action |
|----------|--------|
| <span className="keyboard-key">Win</span> + <span className="keyboard-key">Shift</span> + <span className="keyboard-key">S</span> | Snipping Tool |
| <span className="keyboard-key">PrtSc</span> | Full screen to clipboard |
| <span className="keyboard-key">Alt</span> + <span className="keyboard-key">PrtSc</span> | Active window |

### Linux

Varies by desktop environment. Common options:
- `gnome-screenshot`
- `flameshot`
- <span className="keyboard-key">PrtSc</span> key

## Step 2: Import to mahpastes

### From Clipboard

1. Take screenshot to clipboard
2. Focus mahpastes window
3. Press <span className="keyboard-key">Cmd</span>/<span className="keyboard-key">Ctrl</span> + <span className="keyboard-key">V</span>

The screenshot appears in your gallery.

### From File

If your screenshot was saved to disk:

1. Drag the file from Finder/Explorer
2. Drop onto mahpastes window

Or set up a [Watch Folder](/features/watch-folders) for automatic import.

## Step 3: Annotate the Screenshot

Make your screenshot more useful with annotations.

### Open the Editor

1. Find your screenshot in the gallery
2. Click the edit button
3. The image editor opens

### Add Highlights

Use rectangles to draw attention to specific areas:

1. Press <span className="keyboard-key">R</span> for Rectangle tool
2. Choose a visible color (red works well)
3. Draw a rectangle around the area of interest

### Add Labels

Explain what you're highlighting:

1. Press <span className="keyboard-key">T</span> for Text tool
2. Click near your highlight
3. Type your label
4. Use contrasting colors for visibility

### Draw Arrows

Point to specific elements (using Line tool):

1. Press <span className="keyboard-key">L</span> for Line tool
2. Draw from label to target
3. Add an arrowhead by drawing short angled lines

### Example: Bug Report Screenshot

1. Capture the bug state
2. **Red rectangle** around the error
3. **Text label** explaining the issue
4. **Arrow** pointing to the specific element
5. Save

## Step 4: Share the Screenshot

### Copy to Clipboard

1. Click the copy button on the clip
2. Paste into your destination (Slack, email, etc.)

### Copy Path (for Terminal/CLI)

1. Click the path icon
2. mahpastes creates a temp file
3. Path is copied to clipboard
4. Use in terminal commands:

```bash
# Attach to GitHub issue
gh issue create --body "See screenshot" --attach $(pbpaste)

# Upload somewhere
curl -F "file=@$(pbpaste)" https://upload.example.com

# Open in Preview (macOS)
open $(pbpaste)
```

### Save to Disk

1. Click the download button
2. Choose save location
3. Share the file as needed

## Automated Screenshot Workflow

Set up automatic import for zero-friction capture.

### Configure Watch Folder

1. Open Watch Folders settings
2. Add your screenshots folder:
   - macOS: `~/Desktop` or custom location
   - Windows: Often `Pictures/Screenshots`
   - Linux: Varies by tool
3. Filter: Images preset
4. Auto-archive: Off (keep in main gallery)
5. Save

### Streamlined Workflow

Now:
1. Take screenshot (saves to disk)
2. mahpastes auto-imports it
3. Open mahpastes and annotate
4. Share

No manual paste required.

## Tips for Better Screenshots

### Before Capturing

- Close unnecessary windows
- Hide sensitive information
- Use appropriate zoom level
- Consider dark/light mode

### Annotation Best Practices

- Use bright, contrasting colors
- Keep text brief
- Don't over-annotate
- Use consistent styling

### For Bug Reports

Include:
1. The error/issue clearly visible
2. Relevant context (browser, app state)
3. Steps or state leading to the issue
4. Any error messages

### For Documentation

Include:
1. Clean, uncluttered UI
2. Clear labels for referenced elements
3. Appropriate window size
4. Consistent styling across screenshots

## Example Workflows

### Customer Support

1. Customer describes issue
2. You reproduce and screenshot
3. Annotate with circles and arrows
4. Add text explaining the solution
5. Copy and paste into support response

### Code Review

1. Screenshot the code/UI in question
2. Circle the problematic area
3. Add comments explaining concerns
4. Share in review comments

### Design Feedback

1. Screenshot the design
2. Mark areas needing changes
3. Add specific suggestions
4. Attach to design review
