---
sidebar_position: 10
---

# Backup & Restore

Create complete backups of your mahpastes data and restore them on any machine. Backups are version-independent and include all clips, tags, plugins, and settings.

## Overview

The backup system creates a portable ZIP file containing:
- All clips (images, text, files)
- Tags and clip-tag associations
- Installed plugins and their storage
- Watch folder configurations
- Application settings (excluding sensitive data)

## Creating a Backup

1. Click **Settings** (gear icon)
2. Find the **Backup & Restore** section
3. Click **Create Backup**
4. Choose a location and filename
5. Wait for the backup to complete

The backup file is a standard ZIP with a `.zip` extension.

## Restoring from Backup

:::warning
Restoring replaces ALL current data. This cannot be undone.
:::

1. Click **Settings** (gear icon)
2. Find the **Backup & Restore** section
3. Click **Restore from Backup**
4. Select your backup file
5. Review the backup summary (clips, tags, plugins)
6. Click **Delete & Restore** to confirm

After restore:
- All clips and tags are restored
- Plugins are restored but permissions require re-confirmation
- Watch folders are restored but paused (re-enable manually)
- Settings are restored (except API keys and secrets)

## What's Included

### Included in Backup

| Data | Notes |
|------|-------|
| Clips | Full content (images, text, files) |
| Tags | Names, colors, clip associations |
| Plugins | Lua files and plugin storage |
| Watch folders | Paths and configurations (paused on restore) |
| Settings | General preferences |

### Excluded from Backup

| Data | Reason |
|------|--------|
| API keys | Security (re-enter after restore) |
| Passwords/tokens | Security |
| Temporary files | Regenerated as needed |

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
- Summary (clip count, tag count, etc.)
- List of excluded sensitive settings

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

### Best Practice

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

Backups intentionally exclude:
- API keys (fal.ai, etc.)
- Any setting containing "password", "secret", "token", or "api_key"

After restore, re-enter these values in Settings.

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

**Sensitive settings**: API keys are excluded. Re-enter them.

**Watch folders paused**: Manually resume watching in Watch Folders settings.

**Plugin permissions**: Confirm permissions in the Plugins panel.

### Clips Not Showing

After restore, clips should appear immediately. If not:
1. Refresh the page (Cmd+R / Ctrl+R)
2. Check if viewing Archive vs Active clips
3. Check tag filters

## Technical Details

### Backup Process

1. Database is exported as SQL INSERT statements
2. Binary data (images) encoded as hex literals
3. Plugin files copied from data directory
4. Manifest generated with metadata
5. All files zipped together

### Restore Process

1. Backup validated (manifest check)
2. Watch folders paused
3. Existing data cleared (in transaction)
4. SQL statements executed
5. Plugin files extracted
6. Permissions marked for reconfirmation
7. Transaction committed

### Atomic Restore

The restore uses a database transaction:
- Either everything restores or nothing changes
- Interrupted restore won't corrupt data
- Original data only cleared if restore succeeds
