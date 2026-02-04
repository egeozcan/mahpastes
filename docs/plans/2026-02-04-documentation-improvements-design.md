# Documentation Improvements Design

Add comprehensive documentation for the Tags feature and Plugin system, plus fill in missing details in existing docs.

## Overview

- **Audience:** Both end users and plugin developers
- **Scope:** New Plugins section, new Tags feature page, updates to existing docs
- **Structure:** Plugins as new top-level sidebar category

## New Sidebar Structure

```
mahpastes Docs
├── Welcome (intro)
├── Getting Started (existing)
│   ├── Installation
│   ├── Quick Start
│   └── Keyboard Shortcuts
├── Features (existing + new)
│   ├── Clipboard Management
│   ├── Image Editor
│   ├── Image Comparison
│   ├── Text Editor
│   ├── Tags              ← NEW
│   ├── Auto-Delete
│   ├── Archive
│   ├── Watch Folders
│   └── Bulk Actions
├── Plugins               ← NEW TOP-LEVEL
│   ├── Overview
│   ├── Installing Plugins
│   ├── Writing Plugins
│   │   ├── Getting Started
│   │   ├── Plugin Manifest
│   │   ├── Event Handling
│   │   └── Settings & Storage
│   ├── API Reference
│   └── Example Plugins
├── Tutorials (existing)
├── Developers (existing, updated)
│   └── Database Schema   ← Updated with new tables
└── Reference (existing)
```

## New Files

### 1. Tags Feature Page

**File:** `docs/features/tags.md`

**Content:**
- What are Tags - Color-coded labels to organize clips
- Creating Tags - From tag manager or inline when assigning
- Assigning Tags to Clips - Click tag icon on clip card
- Filtering by Tags - Click tag in filter bar
- Managing Tags - Rename, change color, delete
- Bulk Tagging - Multi-select clips, apply/remove tags
- Tag Colors - 8 auto-assigned colors (stone, red, amber, green, blue, violet, pink, cyan)

### 2. Plugins Overview

**File:** `docs/plugins/overview.md`

**Content:**
- What plugins can do:
  - Automate workflows (auto-tag imports, sync to cloud)
  - Integrate with external services (Dropbox, webhooks)
  - Transform content (compress images, format text)
  - React to events (clip created, file detected)
  - Run scheduled tasks (periodic cleanup, sync checks)
- Plugin architecture (single Lua file, sandboxed execution)
- Security model (declared permissions, user approval)

### 3. Installing Plugins

**File:** `docs/plugins/installing-plugins.md`

**Content:**
- Opening Settings → Plugins panel
- Importing a `.lua` file
- Reviewing permissions (network domains, filesystem access)
- Enabling/disabling plugins
- Viewing plugin logs
- Configuring plugin settings
- Revoking permissions
- Removing plugins

### 4. Writing Plugins - Getting Started

**File:** `docs/plugins/writing-plugins/getting-started.md`

**Content:**
- Plugin file structure (single `.lua` file)
- The `Plugin = {}` manifest table
- Required fields (name) vs optional (version, description, author)
- "Hello World" example that logs on startup
- Testing your plugin (import, check logs)

### 5. Writing Plugins - Plugin Manifest

**File:** `docs/plugins/writing-plugins/plugin-manifest.md`

**Content:**
- All manifest fields with types and examples
- `events` array - subscribing to app events
- `network` table - declaring HTTP permissions by domain/method
- `filesystem` table - read/write permission flags
- `schedules` array - periodic task definitions
- `settings` array - declaring configurable settings (text, password, checkbox, select)

### 6. Writing Plugins - Event Handling

**File:** `docs/plugins/writing-plugins/event-handling.md`

**Content:**
- Handler naming convention (`clip:created` → `on_clip_created`)
- All available events with payloads:
  - `app:startup`, `app:shutdown`
  - `clip:created`, `clip:deleted`, `clip:archived`, `clip:unarchived`
  - `watch:file_detected`, `watch:import_complete`
  - `tag:created`, `tag:updated`, `tag:deleted`, `tag:added_to_clip`, `tag:removed_from_clip`
- 30-second timeout per handler
- Error handling with pcall

### 7. Writing Plugins - Settings & Storage

**File:** `docs/plugins/writing-plugins/settings-storage.md`

**Content:**
- Declaring settings in manifest
- Setting types: text, password, checkbox, select
- Reading settings with `storage.get("setting:key")`
- Plugin-scoped key-value storage API
- Persisting state between restarts
- Storage limits (10MB per plugin)

### 8. Plugin API Reference

**File:** `docs/plugins/api-reference.md`

**Content by module:**

**clips module:**
- `clips.list(filter?)` - Query clips
- `clips.get(id)` - Get full clip with data
- `clips.create({data, content_type, filename?})` - Create new clip
- `clips.update(id, {is_archived?})` - Update metadata
- `clips.delete(id)` - Delete single clip
- `clips.delete_many({id1, id2, ...})` - Bulk delete
- `clips.archive(id)` / `clips.unarchive(id)`

**tags module:**
- `tags.list()` - All tags with usage counts
- `tags.get(id)` - Single tag by ID
- `tags.create(name)` - Create tag (auto-assigns color)
- `tags.update(id, {name?, color?})` - Update tag
- `tags.delete(id)` - Delete tag
- `tags.add_to_clip(tag_id, clip_id)` - Assign tag
- `tags.remove_from_clip(tag_id, clip_id)` - Remove tag
- `tags.get_for_clip(clip_id)` - Get clip's tags

**storage module:**
- `storage.get(key)` - Read value
- `storage.set(key, value)` - Write value
- `storage.delete(key)` - Remove key
- `storage.list()` - All keys

**http module:**
- `http.get(url, {headers?})`
- `http.post(url, {headers?, body?})`
- `http.put(url, {headers?, body?})`
- `http.patch(url, {headers?, body?})`
- `http.delete(url, {headers?})`
- Response format: `{status, headers, body}`
- Domain/method enforcement

**fs module:**
- `fs.read(path)` - Read file (triggers permission prompt)
- `fs.write(path, content)` - Write file
- `fs.list(path)` - List directory
- `fs.exists(path)` - Check existence (no prompt)

**toast module:**
- `toast.show(message, type?)` - Show notification

**Utility functions:**
- `log(message)` - Write to plugin log
- `json.encode(table)` / `json.decode(string)`
- `base64.encode(data)` / `base64.decode(string)`
- `utils.time()` - Current Unix timestamp

**Resource limits:**
| Resource | Limit | On Violation |
|----------|-------|--------------|
| Execution time | 30s per handler | Terminate, notify user |
| Memory | 50MB per plugin | Terminate, notify user |
| HTTP requests | 100/minute | Throttle, log warning |
| File operations | 50/minute | Throttle, log warning |
| Storage | 10MB per plugin | Reject writes, notify |

### 9. Example Plugins

**File:** `docs/plugins/example-plugins.md`

**Example 1: Auto-Tagger** (beginner)
- Tags image clips with "screenshot" if filename contains "screenshot"
- Tags clips from watch folders with "imported"
- Demonstrates: `clip:created` event, `tags.create()`, `tags.add_to_clip()`

**Example 2: Webhook Notifier** (intermediate)
- Sends POST to configured webhook URL when clips are created
- User configures webhook URL via plugin settings
- Demonstrates: `settings` manifest, `http.post()`, `storage.get("setting:*")`

**Example 3: Periodic Cleanup** (intermediate)
- Deletes unarchived clips older than N days (configurable)
- Runs every hour via scheduled task
- Demonstrates: `schedules` manifest, `clips.list()`, `clips.delete_many()`

Each example includes:
- Full plugin code (copy-pasteable)
- Line-by-line explanation
- Required permissions

## Updates to Existing Files

### 1. intro.md

Add to feature table:
| **Tags** | Color-coded labels to organize clips |
| **Plugins** | Extend functionality with Lua scripts |

### 2. developers/database-schema.md

Add tables:

```sql
CREATE TABLE tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE NOT NULL,
    color TEXT NOT NULL
);

CREATE TABLE clip_tags (
    clip_id INTEGER,
    tag_id INTEGER,
    PRIMARY KEY (clip_id, tag_id),
    FOREIGN KEY (clip_id) REFERENCES clips(id) ON DELETE CASCADE,
    FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
);

CREATE TABLE plugins (
    id INTEGER PRIMARY KEY,
    filename TEXT UNIQUE,
    name TEXT,
    version TEXT,
    enabled INTEGER DEFAULT 1,
    status TEXT DEFAULT 'enabled',
    error_count INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE plugin_permissions (
    plugin_id INTEGER,
    permission_type TEXT,
    path TEXT,
    granted_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (plugin_id) REFERENCES plugins(id)
);

CREATE TABLE plugin_storage (
    plugin_id INTEGER,
    key TEXT,
    value BLOB,
    PRIMARY KEY (plugin_id, key),
    FOREIGN KEY (plugin_id) REFERENCES plugins(id)
);
```

### 3. developers/api-reference.md

Add Tag Operations section:
- `CreateTag(name string) (*Tag, error)`
- `DeleteTag(id int64) error`
- `GetTags() ([]Tag, error)`
- `UpdateTag(id int64, name string, color string) error`
- `AddTagToClip(clipID, tagID int64) error`
- `RemoveTagFromClip(clipID, tagID int64) error`
- `GetClipTags(clipID int64) ([]Tag, error)`
- `BulkAddTag(clipIDs []int64, tagID int64) error`
- `BulkRemoveTag(clipIDs []int64, tagID int64) error`

### 4. sidebars.js

Add Tags to Features:
```javascript
{
  type: 'category',
  label: 'Features',
  items: [
    // ... existing items
    'features/tags',  // NEW
    // ...
  ],
},
```

Add Plugins category:
```javascript
{
  type: 'category',
  label: 'Plugins',
  collapsed: false,
  items: [
    'plugins/overview',
    'plugins/installing-plugins',
    {
      type: 'category',
      label: 'Writing Plugins',
      items: [
        'plugins/writing-plugins/getting-started',
        'plugins/writing-plugins/plugin-manifest',
        'plugins/writing-plugins/event-handling',
        'plugins/writing-plugins/settings-storage',
      ],
    },
    'plugins/api-reference',
    'plugins/example-plugins',
  ],
},
```

## File Summary

**New files (9):**
1. `docs/features/tags.md`
2. `docs/plugins/overview.md`
3. `docs/plugins/installing-plugins.md`
4. `docs/plugins/writing-plugins/getting-started.md`
5. `docs/plugins/writing-plugins/plugin-manifest.md`
6. `docs/plugins/writing-plugins/event-handling.md`
7. `docs/plugins/writing-plugins/settings-storage.md`
8. `docs/plugins/api-reference.md`
9. `docs/plugins/example-plugins.md`

**Updated files (4):**
1. `docs/intro.md`
2. `docs/developers/database-schema.md`
3. `docs/developers/api-reference.md`
4. `sidebars.js`
