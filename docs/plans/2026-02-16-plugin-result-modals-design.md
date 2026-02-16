# Plugin Result Modals

## Problem

Plugins currently output results by creating new clips (via `clips.create`). This clutters the gallery with intermediate/informational content that users often just want to view — metadata reports, ASCII art previews, color palettes. Plugins need a way to display rich rendered content in a modal with explicit user actions to copy or save.

## Solution

A new plugin result modal system that renders markdown, images, and plain text. Plugins choose whether to show a modal or create a clip directly. The modal provides two fixed actions: "Copy to clipboard" and "Add as paste", with plugin-controlled payloads for each.

## Lua API

### Return value from `on_ui_action`

```lua
function on_ui_action(action_id, clip_ids, options)
    return {
        modal = {
            title = "Image Metadata",
            content = "## Camera\n\n| Field | Value |\n|---|---|\n| Make | Canon |",
            format = "markdown",
            copy_data = "Make: Canon\nModel: EOS R5",
            paste_data = "Make: Canon\nModel: EOS R5",
            paste_name = "metadata.txt",
            paste_content_type = "text/plain",
        }
    }
end
```

If `on_ui_action` returns a `modal` key, the frontend shows the modal instead of refreshing the gallery. The existing `result_clip_id` return key still works for plugins that create clips directly.

### Explicit `modal.show()` call

```lua
modal.show({
    title = "Results",
    content = "# Hello\n\nSome **bold** text.",
    format = "markdown",
})
```

Works from any context: event handlers, scheduled tasks, etc. Emits a `plugin:modal` Wails event.

### Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `title` | string | yes | Modal title, max 200 chars |
| `content` | string | yes | Display content, max 1MB |
| `format` | string | yes | `"markdown"`, `"image"`, or `"text"` |
| `copy_data` | string | no | What "Copy to clipboard" copies. Defaults to `content` |
| `paste_data` | string | no | What "Add as paste" saves. Defaults to `content`. Max 10MB |
| `paste_name` | string | no | Filename for the created clip |
| `paste_content_type` | string | no | MIME type for the created clip |

### Format types

- `"markdown"` — rendered with marked.js (GFM tables enabled), sanitized with DOMPurify
- `"image"` — displays a base64 data URI as `<img>`
- `"text"` — plain preformatted text in `<pre>` with monospace font

## Backend (Go)

### ModalData struct

```go
type ModalData struct {
    Title            string `json:"title"`
    Content          string `json:"content"`
    Format           string `json:"format"`
    CopyData         string `json:"copy_data,omitempty"`
    PasteData        string `json:"paste_data,omitempty"`
    PasteName        string `json:"paste_name,omitempty"`
    PasteContentType string `json:"paste_content_type,omitempty"`
}
```

### ActionResult extension

```go
type ActionResult struct {
    Success      bool       `json:"success"`
    Error        string     `json:"error,omitempty"`
    ResultClipID int64      `json:"result_clip_id,omitempty"`
    Modal        *ModalData `json:"modal,omitempty"`
}
```

### New file: `plugin/api_modal.go`

- Registers `modal` global table in Lua sandbox with `show(opts)` function
- Validates options: title, content, format, size limits
- Emits `plugin:modal` Wails event with `{plugin_id, title, content, format, copy_data, paste_data, paste_name, paste_content_type}`
- Rate-limited: 1 modal at a time (returns error if modal already open)

### Return value handling

In sandbox execution of `on_ui_action`, check return table for `modal` key. If present, extract and validate fields, populate `ActionResult.Modal`.

## Frontend

### Dependencies (vendored in `frontend/vendor/`)

- `marked.min.js` (~40KB) — markdown parsing, GFM tables enabled
- `purify.min.js` (~15KB) — HTML sanitization

Loaded via `<script>` tags in `index.html`, no CDN.

### New file: `frontend/js/modal-renderer.js`

Handles markdown rendering and plugin result modal logic.

### DOMPurify configuration

```js
const SANITIZE_CONFIG = {
    ALLOWED_TAGS: ['h1','h2','h3','h4','h5','h6','p','br','hr',
                   'strong','em','del','code','pre','blockquote',
                   'ul','ol','li','a','img','table','thead','tbody',
                   'tr','th','td','span'],
    ALLOWED_ATTR: ['href','src','alt','title','class'],
    ALLOW_DATA_ATTR: false,
    ADD_ATTR: ['target'],
};
```

Links get `target="_blank"` and `rel="noopener noreferrer"` via DOMPurify hook.

### Modal HTML (`#plugin-result-modal` in `index.html`)

```
#plugin-result-modal
  backdrop (click to close)
  modal container (max-w-2xl, max-h-[80vh])
    header: title + close button
    scrollable content area
      if markdown: rendered HTML in .plugin-md-content container
      if image: <img> centered
      if text: <pre> with monospace font
    footer:
      "Copy to clipboard" (secondary button style)
      "Add as paste" (primary button style)
```

### Markdown styles (`.plugin-md-content` in `frontend/css/modals.css`)

Scoped styles matching the stone-based design system: stone headings, `border-stone-200` table borders, `bg-stone-50` code blocks, IBM Plex Mono for code, proper spacing.

### Wiring

1. After `ExecutePluginAction` returns, check `result.modal`. If present, call `showPluginResultModal(result.modal)`.
2. Listen for `plugin:modal` Wails event and open the same modal.
3. "Copy to clipboard" copies `copy_data` (or `content` if absent).
4. "Add as paste" creates a clip from `paste_data`/`paste_name`/`paste_content_type` (or `content` with defaults), closes modal, refreshes gallery.

## Security

### Sanitization

- DOMPurify with strict tag/attribute allowlist — no `<script>`, `<style>`, `<iframe>`, `<form>`, event handlers, `javascript:` URIs
- Links open in system browser via `target="_blank" rel="noopener noreferrer"`
- `data:` URIs allowed only on `<img src>`
- External images: HTTPS only, no plain HTTP

### Content limits (validated in Go)

- `title`: max 200 chars
- `content`: max 1MB
- `copy_data`: max 1MB
- `paste_data`: max 10MB
- `format`: must be one of `"markdown"`, `"image"`, `"text"`

### Rate limiting

- `modal.show()`: 1 modal at a time, error if already open
- Return-value modals: no rate limit (1:1 with user actions)

## Plugin Modifications

### EXIF Viewer (`exif-viewer.lua`)

- Change `on_ui_action` to return `modal` with `format = "markdown"`
- Render metadata as a markdown table (camera, dimensions, GPS, etc.)
- `copy_data`: plain text report
- `paste_data`/`paste_name`: same text as `metadata.txt`
- Auto-tagging on `on_clip_created` unchanged

### ASCII Art (`ascii-art.lua`)

- Change `on_ui_action` to return `modal` with `format = "text"`
- Renders in `<pre>` with monospace font for proper visual preview
- `copy_data`: ASCII art string
- `paste_data`/`paste_name`: same as `ascii-art.txt`

### Palette Extractor (`palette-extractor.lua`)

- Change `on_ui_action` to return `modal` with `format = "markdown"`
- Show embedded SVG via data URI `<img>` plus markdown table of hex codes
- `copy_data`: newline-separated hex codes
- `paste_data`/`paste_content_type`: SVG bytes as `image/svg+xml`
- `paste_name`: `palette.svg`

### Unchanged plugins

FAL.AI, QR Code, Watermarker (produce images users want saved), Auto-Tagger, Expiring Clips (no visual output).
