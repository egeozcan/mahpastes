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
| **Linux** | `~/.config/mahpastes/clips.db` or `$XDG_CONFIG_HOME/mahpastes/clips.db` (see below) |

On Linux the data directory follows `XDG_CONFIG_HOME` without ever moving an existing library. The first match wins:

1. `$XDG_CONFIG_HOME/mahpastes/`, if `XDG_CONFIG_HOME` is an absolute path and this directory already exists.
2. `~/.config/mahpastes/`, if it already exists (every library created before XDG support lives here).
3. `$XDG_CONFIG_HOME/mahpastes/`, if `XDG_CONFIG_HOME` is an absolute path (a fresh install).
4. `~/.config/mahpastes/`.

The desktop app, the `mahpastesd` server and `scripts/linux/install.sh` all use this rule. To move an existing library to `$XDG_CONFIG_HOME`, quit mahpastes and move the whole `~/.config/mahpastes` directory there.

The data directory can be overridden with the `MAHPASTES_DATA_DIR` environment variable. This is used for testing.

Only one process may use a data directory at a time. The desktop app and `mahpastesd` hold an advisory
lock on `instance.lock` in the data directory while they run. The desktop app also listens on
`instance.sock` there (a Unix domain socket readable only by your user; on a very long data directory path
it is created in your per-user runtime or temp directory instead), which is how a second launch hands its
files to the running window. Both files are safe to delete while mahpastes is not running.

`desktop-prefs.json` in the data directory holds the desktop settings mahpastes needs before its
database opens: **Keep running when the window is closed** (see
[Background & Startup](../features/background-startup.md)) and the [window state](#window-state). It is
not part of a backup; deleting it restores the platform default and the default window placement. The **Launch at login** entry lives where your system keeps login items,
not in the data directory.

The database uses WAL journal mode, a 5-second busy timeout, and foreign keys enabled. These pragmas are set via the DSN string for connection-pool safety.

### Temporary Files

Temp files are created for clipboard copy-as-file and drag-out transfers. Managed by `TempClipStore`.

| Platform | Path |
|----------|------|
| **macOS** | `~/Library/Application Support/mahpastes/clip_temp_files/` |
| **Windows** | `%APPDATA%\mahpastes\clip_temp_files\` |
| **Linux** | `clip_temp_files/` inside the [data directory](#database) (`~/.config/mahpastes/` or `$XDG_CONFIG_HOME/mahpastes/`) |

Temp files have a 60-minute lease. A prune job runs every 10 minutes to remove stale and orphaned files. All temp files are also deleted on app exit.

### Window State

The main window's size, position and maximised state are saved under the `window` key of `desktop-prefs.json` in the data directory (next to `clips.db`) when the app quits, and read back at the next launch. With **Keep running when the window is closed**, closing the window only hides it; the placement is saved then (and whenever the window loses focus), and again when the app quits. It is not part of backups. To reset the window to its default size and placement, quit mahpastes and remove the `window` entry (or delete `desktop-prefs.json`, which also resets the keep-running choice).

### Log Files

mahpastes writes diagnostics, including Go crash traces, to `logs/mahpastes.log` in the data directory. When the file reaches 5 MB it is moved to `mahpastes.log.1` (replacing any older copy), so logs use at most about 10 MB. If the database cannot be opened at startup (for example because it is corrupt), or startup fails for another reason after the window opens, a dialog shows the error and names this folder before the app quits. **Open Log Folder** in the Maintenance window opens it (desktop app only; in server mode the logs stay on the server). Logs can contain file paths, so the folder and files are readable only by your user account where the system supports file permissions, and they never leave your machine.

## What's Stored

### In the Database

| Data | Storage |
|------|---------|
| Clip content | Full binary data as BLOB |
| Content type | MIME type string |
| Content hash | SHA-256 hash for deduplication |
| Filename | Original name (if available) |
| Timestamps | Creation time, expiration time |
| Archive status | Boolean flag |
| Metadata | JSON key-value pairs per clip |
| Tags | Name and color |
| Clip-tag associations | Many-to-many relationship |
| Watch folders | Path, filter settings, auto-archive, auto-tag |
| Plugins | Filename, name, version, status, source URL |
| Plugin permissions | Permission type, path, pending reconfirm flag (re-confirmation required after restore) |
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
