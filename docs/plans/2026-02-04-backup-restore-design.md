# Backup & Restore Feature Design

**Date:** 2026-02-04
**Status:** Approved

## Overview

Add a manual backup/restore feature that exports all app data to a single ZIP file. Backups are version-independent, allowing restoration across different app versions with graceful degradation for missing features.

## Requirements

- Manual backup triggered from Settings modal
- Single ZIP file containing all data
- Full replace on restore (no merge)
- Version-independent format with forward/backward compatibility
- Watch folders restored in paused state
- Plugin permissions restored but require re-confirmation
- Sensitive data (API keys) excluded from backups

## Backup File Structure

```
mahpastes-backup-YYYY-MM-DD.zip
├── manifest.json          # Version info + summary stats
├── database.sql           # SQL dump of all tables
└── plugins/               # Plugin source files
    ├── my-plugin.lua
    └── another-plugin.lua
```

### manifest.json

```json
{
  "format_version": 1,
  "app_version": "1.2.3",
  "created_at": "2026-02-04T15:30:00Z",
  "platform": "darwin",
  "summary": {
    "clips": 142,
    "tags": 8,
    "plugins": 2,
    "watch_folders": 3
  },
  "excluded": ["fal_api_key"]
}
```

### database.sql

SQL dump format chosen over raw SQLite file for:
- Version independence (schema can evolve)
- Human-readable and diffable
- Selective import capability in future versions

Tables exported:
- `clips` - Full data including BLOBs (base64-encoded in SQL)
- `tags` - All tag definitions
- `clip_tags` - Relationships
- `settings` - Excluding keys matching `*api_key*` pattern
- `watched_folders` - All configs
- `plugins` - Plugin metadata
- `plugin_storage` - Plugin key-value data
- `plugin_permissions` - With `pending_reconfirm` flag

## Backup Creation Flow

### Backend

New method `CreateBackup(filepath string) error`:

1. Create temporary directory for staging
2. Export database tables to SQL
3. Copy plugin `.lua` files from plugins directory
4. Generate `manifest.json` with version info and counts
5. Create ZIP archive at user-specified path
6. Clean up temp directory

### Frontend

1. User clicks "Create Backup" in Settings
2. Call Wails `SaveFileDialog` with suggested filename `mahpastes-backup-YYYY-MM-DD.zip`
3. If user confirms, call `CreateBackup(selectedPath)`
4. Show success toast with file path, or error toast if failed

## Restore Flow

### Confirmation UI

After selecting a backup file, show confirmation modal:

```
┌─────────────────────────────────────────────────┐
│  Restore from Backup                            │
├─────────────────────────────────────────────────┤
│  ⚠️  This will replace ALL current data         │
│                                                 │
│  Backup created: Feb 4, 2026 at 3:30 PM         │
│  App version: 1.2.3                             │
│                                                 │
│  This backup contains:                          │
│    • 142 clips                                  │
│    • 8 tags                                     │
│    • 2 plugins                                  │
│    • 3 watch folders (will be paused)           │
│                                                 │
│  Your current data will be permanently deleted. │
│  This action cannot be undone.                  │
│                                                 │
│  [Cancel]                    [Delete & Restore] │
└─────────────────────────────────────────────────┘
```

### Backend Restore Process

1. Validate ZIP structure and `manifest.json`
2. Check `format_version` - warn if newer than supported
3. Stop any active watchers
4. Begin database transaction
5. Drop all existing data (DELETE FROM all tables)
6. Execute SQL import statements
7. Mark all `plugin_permissions` as `pending_reconfirm = true`
8. Set all `watched_folders` to `is_paused = true`
9. Commit transaction
10. Copy plugin `.lua` files to plugins directory
11. Reload plugin manager
12. Refresh frontend state

## Version Compatibility

### Forward Compatibility (newer backup on older app)

When `format_version` is higher than supported:

1. Show warning: "This backup was created with a newer version of mahpastes. Some data may not be restored."
2. Allow user to proceed or cancel
3. Unknown tables/columns are skipped (SQL errors caught and logged)
4. Unknown plugin files still copied

### Backward Compatibility (older backup on newer app)

1. Older format versions handled with migration logic
2. Missing columns get default values
3. Missing tables are simply empty after restore

### Schema Evolution Strategy

- Increment `format_version` only for breaking changes
- New columns should have sensible defaults
- Export code always writes current format
- Import code maintains handlers for all previous format versions

## Settings UI Design

Add "Backup & Restore" section to Settings modal:

```html
<h3 class="text-sm font-semibold uppercase tracking-wide text-stone-800 mb-3">
  Backup & Restore
</h3>

<p class="text-xs text-stone-600 mb-4">
  Create a backup of all clips, tags, plugins, and settings.
  Restoring will replace all current data.
</p>

<div class="flex gap-3">
  <button id="create-backup-btn"
    class="bg-stone-800 hover:bg-stone-700 text-white text-xs font-medium py-2.5 px-5 rounded-md transition-colors">
    Create Backup
  </button>
  <button id="restore-backup-btn"
    class="border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-600 text-xs font-medium py-2.5 px-5 rounded-md transition-colors">
    Restore from Backup
  </button>
</div>
```

## Error Handling

### Backup Errors

| Scenario | Handling |
|----------|----------|
| Disk full | Error toast: "Not enough disk space to create backup" |
| Permission denied | Error toast: "Cannot write to selected location" |
| Database locked | Retry with short delay, then error if persistent |

### Restore Errors

| Scenario | Handling |
|----------|----------|
| Invalid ZIP | Error: "Invalid backup file" |
| Missing manifest | Error: "This doesn't appear to be a mahpastes backup" |
| Corrupted SQL | Rollback transaction, error: "Backup file is corrupted" |
| Missing plugin files | Warn but continue |

### Atomic Restore

All database operations wrapped in single transaction. If anything fails, transaction rolls back and original data remains intact. Plugin files copied only after database transaction succeeds.

### App State During Operations

- During backup: App remains usable (read-only snapshot)
- During restore: Disable UI, show progress indicator
- After restore: Full app reload to refresh state

## Implementation Plan

### New Go Code

| File | Changes |
|------|---------|
| `app.go` | Add `CreateBackup`, `RestoreBackup`, `ValidateBackup` methods |
| `backup.go` (new) | Backup/restore logic, SQL export/import, ZIP handling |

### New Frontend Code

| File | Changes |
|------|---------|
| `index.html` | Backup & Restore section in settings, restore confirmation modal |
| `settings.js` | Button handlers, Wails API calls |
| `modals.js` | `showRestoreConfirmModal(manifest)` function |

### Database Change

Add `pending_reconfirm` boolean column to `plugin_permissions` table (nullable, defaults to false). Migration runs on app startup if column missing.

### Testing

New e2e tests in `e2e/tests/backup/`:
- `backup-create.spec.ts` - Create backup, verify ZIP contents
- `backup-restore.spec.ts` - Restore backup, verify data replaced
- `backup-version.spec.ts` - Forward/backward compatibility warnings
