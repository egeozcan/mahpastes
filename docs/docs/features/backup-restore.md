---
sidebar_position: 10
---

# Backup & Restore

Back up your mahpastes data and restore it on any machine. Backups include all clips, tags, plugins, and watch folder configurations.

## Overview

The backup system creates a portable ZIP file containing:
- All clips (images, text, files)
- Tags and clip-tag associations
- Installed plugins, their storage, and permissions
- Watch folder configurations
- The `settings` table (watch folder pause state), with sensitive rows filtered out

## Creating a Backup

1. Open the menu drawer and click **Settings**
2. Find the **Backup & Restore** section
3. Click **Create Backup**
4. Choose a location and filename
5. Wait for the backup to complete

The backup file is a standard ZIP with a `.zip` extension.

![Settings with backup options](/img/screenshots/settings.png)

## Restoring from Backup

:::warning
Restoring replaces ALL current data. This cannot be undone.
:::

1. Open the menu drawer and click **Settings**
2. Find the **Backup & Restore** section
3. Click **Restore from Backup**
4. Select your backup file
5. Review the backup summary (clips, tags, plugins)
6. Click **Delete & Restore** to confirm

After restore:
- All clips and tags are restored
- Plugins are restored but permissions require re-confirmation
- Watch folders are restored but paused (re-enable manually)
- App preferences (`app_settings`) and API keys are **not** part of the backup and remain unchanged on the target machine

## What's Included

### Included in Backup

| Data | Notes |
|------|-------|
| Clips | Full content (images, text, files) with metadata |
| Tags | Names, colors, clip-tag associations |
| Plugins | Lua source files, plugin storage, and permissions (permissions are marked for re-confirmation on restore) |
| Watch folders | Paths, filter configurations, auto-archive/auto-tag settings (all folders are paused on restore) |
| Settings | The `settings` table (stores watch folder pause state). Rows matching sensitive patterns are excluded -- see below. |

:::note
The `app_settings` table (app preferences like plugin update interval) and the `api_keys` table are **not** included in backups. These remain untouched on the target machine during restore.
:::

### Excluded from Backup

| Data | Reason |
|------|--------|
| `app_settings` table | App preferences are not part of the backup scope |
| `api_keys` table | API keys are not part of the backup scope (re-create after restore) |
| Sensitive `settings` rows | Any row in the `settings` table whose key contains `api_key`, `secret`, `password`, or `token` is filtered out |
| Temporary transfer files | Regenerated as needed |

## Backup File Format

The backup is a ZIP file containing:

```
backup.zip
├── manifest.json      # Backup metadata
├── database.sql       # SQL dump of all data
└── plugins/           # Plugin Lua files
    ├── my-plugin.lua
    └── another-plugin.lua
```

### Manifest

The `manifest.json` contains:
- Format version (for compatibility)
- App version that created the backup
- Creation timestamp
- Platform (the OS that created the backup)
- Summary (clip count, tag count, plugin count, watch folder count)
- List of excluded sensitive setting keys

## Version Compatibility

### Forward Compatibility

Backups can be restored to newer versions of mahpastes:
- New features won't have data (expected)
- Core data (clips, tags) always restores
- Plugin APIs may change between versions

### Backward Compatibility

Restoring to older versions:
- Works for basic data (clips, tags)
- New features' data is ignored
- May show warnings for unknown data

### Tips

- Keep backups from major versions
- Test restore on a fresh install if switching versions
- Check release notes for breaking changes

## Use Cases

### Moving to a New Computer

1. Create backup on old machine
2. Transfer ZIP file (USB, cloud, etc.)
3. Install mahpastes on new machine
4. Restore from backup

### Regular Backups

Set up a routine:
1. Weekly or monthly backups
2. Store in cloud storage or external drive
3. Keep multiple versions (e.g., last 3 backups)

### Before Major Changes

Create a backup before:
- Upgrading mahpastes
- Bulk operations
- Plugin experiments
- System updates

### Sharing Clip Collections

Share curated clips with others:
1. Create clips you want to share
2. Create backup
3. Share the ZIP file
4. Recipient restores to their mahpastes

:::note
This replaces all their data. For partial sharing, use bulk export instead.
:::

## Security Considerations

### Sensitive Data

The `api_keys` and `app_settings` tables are not part of the backup scope at all. Within the `settings` table, rows whose key contains any of the following patterns are filtered out:
- `api_key`
- `secret`
- `password`
- `token`

After restoring to a new machine, re-create any API keys through Settings.

### Backup File Security

- Backups are not encrypted
- Protect backup files like any sensitive data
- Don't share backups containing private clips
- Store securely (encrypted drive, secure cloud)

### Plugin Permissions

After restore, plugin permissions are marked for re-confirmation:
- Review each plugin's requested permissions
- Approve or deny as appropriate
- Plugins won't run until permissions are confirmed

## Troubleshooting

### Backup Fails

**Disk space**: Ensure enough space for the backup file (roughly size of your clips).

**Permissions**: Check write permissions for the destination folder.

**Large database**: Very large databases may take time. Be patient.

### Restore Fails

**Invalid file**: Ensure the file is a mahpastes backup (check for manifest.json inside).

**Corrupted ZIP**: Try re-downloading or re-copying the backup file.

**Version mismatch**: Check if backup is from a much newer version. Some data may not restore.

### Missing Data After Restore

**API keys**: The `api_keys` table is not backed up. Re-create keys in Settings.

**App preferences**: The `app_settings` table is not backed up. Re-configure preferences on the new machine.

**Watch folders paused**: All watch folders are paused on restore. Manually resume them in the Watch view.

**Plugin permissions**: All permissions are marked for re-confirmation. Review and approve them in the Plugins panel.

### Clips Not Showing

After restore, clips should appear immediately. If not:
1. Refresh the page (Cmd+R / Ctrl+R)
2. Check if viewing Archive vs Active clips
3. Check tag filters

## Technical Details

### Backup Process

1. Eight database tables are exported as SQL INSERT statements: `clips`, `tags`, `clip_tags`, `settings` (with sensitive rows filtered), `watched_folders`, `plugins`, `plugin_storage`, `plugin_permissions`
2. Binary data (images) is encoded as hex literals in the SQL output
3. Plugin `.lua` files are copied from the data directory
4. Manifest generated with metadata (version, platform, summary, excluded keys)
5. All files are zipped together

### Restore Process

1. Backup validated (manifest and `database.sql` checks)
2. Watch folders stopped
3. Existing data cleared from all eight tables (inside a transaction)
4. SQL INSERT statements executed from `database.sql`
5. All plugin permissions marked as `pending_reconfirm`
6. All watch folders marked as paused
7. Transaction committed -- if any step above fails, the transaction rolls back and no data changes
8. Transfer temp files cleared
9. Existing plugin files removed, then backup plugin `.lua` files extracted
10. Plugin manager reloaded
11. Watch folders restarted (but all are paused per step 6)

### Atomic Restore

The restore uses a database transaction:
- Either everything restores or nothing changes
- Interrupted restore won't corrupt data
- Original data only cleared if restore succeeds

## REST API & CLI

You can create and restore backups programmatically through the [REST API](./rest-api.md) or the `mp` CLI:

```bash
# Create a backup (downloads the ZIP)
mp backup create --output ~/backups/mahpastes-backup.zip

# Restore from a backup
mp backup restore ~/backups/mahpastes-backup.zip
```

See the [REST API reference](./rest-api.md#backup) for the `GET /api/v1/backup` and `POST /api/v1/backup/restore` endpoints.
