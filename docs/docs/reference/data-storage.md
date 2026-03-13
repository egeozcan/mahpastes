---
sidebar_position: 1
---

# Data Storage

mahpastes stores all data locally on your machine. Nothing is sent to the cloud.

## Storage Locations

### Database

The SQLite database contains all clips and settings.

| Platform | Path |
|----------|------|
| **macOS** | `~/Library/Application Support/mahpastes/clips.db` |
| **Windows** | `%APPDATA%\mahpastes\clips.db` |
| **Linux** | `~/.config/mahpastes/clips.db` |

The data directory can be overridden with the `MAHPASTES_DATA_DIR` environment variable. This is used for testing.

The database uses WAL journal mode, a 5-second busy timeout, and foreign keys enabled. These pragmas are set via the DSN string for connection-pool safety.

### Temporary Files

Temp files are created for clipboard copy-as-file and drag-out transfers. Managed by `TempClipStore`.

| Platform | Path |
|----------|------|
| **macOS** | `~/Library/Application Support/mahpastes/clip_temp_files/` |
| **Windows** | `%APPDATA%\mahpastes\clip_temp_files\` |
| **Linux** | `~/.config/mahpastes/clip_temp_files/` |

Temp files have a 60-minute lease. A prune job runs every 10 minutes to remove stale and orphaned files. All temp files are also deleted on app exit.

## What's Stored

### In the Database

| Data | Storage |
|------|---------|
| Clip content | Full binary data as BLOB |
| Content type | MIME type string |
| Filename | Original name (if available) |
| Timestamps | Creation time, expiration time |
| Archive status | Boolean flag |
| Content hash | SHA-256 hash for [deduplication](../features/deduplication.md) |
| Metadata | JSON key-value pairs per clip |
| Tags | Name and color |
| Clip-tag associations | Many-to-many relationship |
| Watch folders | Path, filter settings, auto-archive, auto-tag |
| Plugins | Filename, name, version, status, source URL, permissions (with re-confirmation flag after restore) |
| Plugin storage | Plugin-scoped key-value data |
| API keys | Name, hashed key, role, optional tag scope, revocation status |
| Settings | Key-value pairs (e.g., hidden_tags, global_watch_paused) |
| App settings | Internal service settings (e.g., plugin_update_interval) |

### File Sizes

- Each clip stores the full content
- Images are stored as-is (PNG, JPG, etc.)
- Text is stored as UTF-8
- No compression applied

### Content Hashes

Every clip has a SHA-256 content hash computed at upload time. Existing clips without hashes are backfilled automatically on startup. These hashes power the [deduplication](../features/deduplication.md) feature.

## Backup

### Built-in Backup (Recommended)

mahpastes has a built-in backup feature that creates portable, version-independent backups:

1. Go to **Settings** > **Backup & Restore**
2. Click **Create Backup**
3. Choose a location for the ZIP file

This backs up clips, tags, plugins, watch folders, and settings. See [Backup & Restore](../features/backup-restore.md) for full details.

### Manual Backup (Advanced)

For direct database backup, copy the database file. The example below shows the macOS path -- substitute your platform's path from the table above.

```bash
# macOS
cp ~/Library/Application\ Support/mahpastes/clips.db ~/backup/clips.db
```

:::warning
Quit mahpastes before copying the database file. Copying while the app is running may produce a corrupt backup due to WAL journaling.
:::

### Restore

**From built-in backup**: Go to **Settings** > **Backup & Restore** > **Restore from Backup**.

**From manual backup**:

1. Quit mahpastes
2. Copy backup file to the data directory
3. Restart mahpastes

```bash
# macOS
cp ~/backup/clips.db ~/Library/Application\ Support/mahpastes/clips.db
```

## Data Management

### Database Size

The database grows with each clip added. To check size:

```bash
ls -lh ~/Library/Application\ Support/mahpastes/clips.db
```

### Reducing Size

To reduce database size:

1. Delete unwanted clips in mahpastes
2. Use expiration for temporary clips
3. Periodically review and clean up archive

After deleting clips, the database file doesn't automatically shrink. To reclaim space:

```bash
sqlite3 ~/Library/Application\ Support/mahpastes/clips.db "VACUUM"
```

### Complete Reset

To start fresh:

1. Quit mahpastes
2. Delete the data directory
3. Restart mahpastes

```bash
rm -rf ~/Library/Application\ Support/mahpastes/
```

## Migration

### Moving to New Computer

The recommended approach is to use the built-in backup:

1. Create a backup via **Settings** > **Backup & Restore** > **Create Backup**
2. Transfer the ZIP file to your new machine
3. Install mahpastes on the new machine
4. Restore via **Settings** > **Backup & Restore** > **Restore from Backup**

Alternatively, you can copy the database file directly:

1. Quit mahpastes on the old machine
2. Copy `clips.db` to the new machine's [data directory](#database)
3. Start mahpastes on the new machine

### Version Compatibility

- Database schema may change between versions
- Migrations run automatically on startup
- Backup before major version upgrades

## Security Considerations

### Local Storage

- All data stored unencrypted
- Protected by OS file permissions
- Accessible to any process running as your user

### Sensitive Data

For sensitive content:
- Use expiration (auto-delete)
- Don't rely on mahpastes for secrets
- Delete sensitive clips when done
- Consider disk encryption at OS level

### Clipboard Security

- Clipboard is shared with all apps
- Other apps can read clipboard content
- Be cautious with passwords and tokens

## WAL Files

SQLite uses Write-Ahead Logging (WAL) mode. You may see additional files:

```
clips.db         # Main database
clips.db-wal     # Write-ahead log
clips.db-shm     # Shared memory file
```

These are normal and managed automatically. Don't delete while mahpastes is running.

## Inspecting the Database

View database contents with SQLite tools:

```bash
# Command line
sqlite3 ~/Library/Application\ Support/mahpastes/clips.db

# Common commands
.schema           # Show table structure
.tables           # List tables
SELECT COUNT(*) FROM clips;
SELECT id, filename, content_type FROM clips LIMIT 10;
```

GUI tools:
- DB Browser for SQLite
- TablePlus
- DBeaver
