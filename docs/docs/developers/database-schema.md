---
sidebar_position: 4
---

# Database Schema

mahpastes uses SQLite for local data storage. The database file is `clips.db` stored in the platform-specific data directory.

## Tables

### clips

Stores all clipboard content.

```sql
CREATE TABLE clips (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    content_type TEXT NOT NULL,
    data BLOB NOT NULL,
    filename TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    is_archived INTEGER DEFAULT 0,
    expires_at DATETIME,
    content_hash TEXT DEFAULT '',
    metadata TEXT DEFAULT '{}'
);
```

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER | Auto-incrementing primary key |
| `content_type` | TEXT | MIME type (e.g., "image/png", "text/plain") |
| `data` | BLOB | Raw binary content |
| `filename` | TEXT | Original filename (nullable) |
| `created_at` | DATETIME | Timestamp of creation |
| `is_archived` | INTEGER | 0 = active, 1 = archived |
| `expires_at` | DATETIME | Auto-delete timestamp (nullable) |
| `content_hash` | TEXT | SHA-256 hash for duplicate detection (empty string default) |
| `metadata` | TEXT | JSON object of user-defined key-value pairs |

**Indexes:**
- Primary key on `id`
- Index on `content_hash` for duplicate group queries

### watched_folders

Configuration for folders being watched.

```sql
CREATE TABLE watched_folders (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    path TEXT NOT NULL UNIQUE,
    filter_mode TEXT NOT NULL DEFAULT 'all',
    filter_presets TEXT,
    filter_regex TEXT,
    process_existing INTEGER DEFAULT 0,
    auto_archive INTEGER DEFAULT 0,
    is_paused INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER | Auto-incrementing primary key |
| `path` | TEXT | Absolute path to folder (unique) |
| `filter_mode` | TEXT | "all", "presets", or "custom" |
| `filter_presets` | TEXT | JSON array of preset names |
| `filter_regex` | TEXT | Regex pattern for custom filter |
| `process_existing` | INTEGER | Import existing files on add |
| `auto_archive` | INTEGER | Archive imports automatically |
| `is_paused` | INTEGER | Per-folder pause state |
| `created_at` | DATETIME | When folder was added |

**Constraints:**
- `path` must be unique

**Migrations:**
```sql
ALTER TABLE watched_folders ADD COLUMN auto_tag_id INTEGER
```

| Column | Type | Description |
|--------|------|-------------|
| `auto_tag_id` | INTEGER | Tag ID to auto-apply on import (nullable) |

### settings

Application settings as key-value pairs.

```sql
CREATE TABLE settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
```

| Column | Type | Description |
|--------|------|-------------|
| `key` | TEXT | Setting name (primary key) |
| `value` | TEXT | Setting value |

**Current settings:**

| Key | Values | Description |
|-----|--------|-------------|
| `global_watch_paused` | "true" / "false" | Global watching pause state |
| `hidden_tags` | JSON array of int64 | Tag IDs to hide from gallery by default |

### tags

Stores tag definitions for organizing clips.

```sql
CREATE TABLE tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    color TEXT NOT NULL
);
```

Note: Colors are auto-assigned from a palette when creating tags, not via a SQL default.

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER | Auto-incrementing primary key |
| `name` | TEXT | Tag name (unique) |
| `color` | TEXT | Hex color code for display |

**Tag hierarchy convention:** There is no `parent_id` column or separate hierarchy table. Tag hierarchy is derived entirely from the `/` separator in tag names. For example, the tag `work/client1/projectABC` is a child of `work/client1`, which is a child of `work`. Descendant queries use `LIKE 'prefix/%'` against the `name` column. This keeps the schema flat and avoids recursive joins.

### clip_tags

Junction table linking clips to tags (many-to-many).

```sql
CREATE TABLE clip_tags (
    clip_id INTEGER NOT NULL,
    tag_id INTEGER NOT NULL,
    PRIMARY KEY (clip_id, tag_id),
    FOREIGN KEY (clip_id) REFERENCES clips(id) ON DELETE CASCADE,
    FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
);
```

| Column | Type | Description |
|--------|------|-------------|
| `clip_id` | INTEGER | Foreign key to clips table |
| `tag_id` | INTEGER | Foreign key to tags table |

**Constraints:**
- Composite primary key on (clip_id, tag_id)
- Cascading deletes when clip or tag is removed

### plugins

Stores installed plugin metadata and state.

```sql
CREATE TABLE plugins (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    filename TEXT UNIQUE NOT NULL,
    name TEXT NOT NULL,
    version TEXT,
    enabled INTEGER DEFAULT 1,
    status TEXT DEFAULT 'enabled',
    error_count INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER | Auto-incrementing primary key |
| `filename` | TEXT | Plugin filename (unique) |
| `name` | TEXT | Human-readable plugin name |
| `version` | TEXT | Plugin version string (nullable) |
| `enabled` | INTEGER | 0 = disabled, 1 = enabled |
| `status` | TEXT | Current status (enabled, error, disabled) |
| `error_count` | INTEGER | Number of runtime errors |
| `created_at` | DATETIME | When plugin was installed |

**Migration:**
```sql
ALTER TABLE plugins ADD COLUMN source_url TEXT DEFAULT ''
```

| Column | Type | Description |
|--------|------|-------------|
| `source_url` | TEXT | URL the plugin was installed from (empty for local imports) |

### plugin_permissions

Stores granted permissions for plugins.

```sql
CREATE TABLE plugin_permissions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    plugin_id INTEGER NOT NULL,
    permission_type TEXT NOT NULL,
    path TEXT NOT NULL,
    granted_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (plugin_id) REFERENCES plugins(id) ON DELETE CASCADE
);
```

Migration adds `pending_reconfirm` column:
```sql
ALTER TABLE plugin_permissions ADD COLUMN pending_reconfirm INTEGER DEFAULT 0
```

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER | Auto-incrementing primary key |
| `plugin_id` | INTEGER | Foreign key to plugins table |
| `permission_type` | TEXT | Permission type (http, fs, etc.) |
| `path` | TEXT | Specific path/domain granted |
| `granted_at` | DATETIME | When permission was granted |
| `pending_reconfirm` | INTEGER | 1 if permission needs re-confirmation after restore |

### plugin_storage

Key-value storage scoped to individual plugins.

```sql
CREATE TABLE plugin_storage (
    plugin_id INTEGER NOT NULL,
    key TEXT NOT NULL,
    value BLOB,
    PRIMARY KEY (plugin_id, key),
    FOREIGN KEY (plugin_id) REFERENCES plugins(id) ON DELETE CASCADE
);
```

| Column | Type | Description |
|--------|------|-------------|
| `plugin_id` | INTEGER | Foreign key to plugins table |
| `key` | TEXT | Storage key |
| `value` | BLOB | Stored value (JSON-encoded) |

**Constraints:**
- Composite primary key on (plugin_id, key)
- Cascading delete when plugin is removed

### app_settings

Application settings used by internal services (e.g., plugin update interval).

```sql
CREATE TABLE IF NOT EXISTS app_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
```

| Column | Type | Description |
|--------|------|-------------|
| `key` | TEXT | Setting name (primary key) |
| `value` | TEXT | Setting value |

**Current settings:**

| Key | Values | Description |
|-----|--------|-------------|
| `plugin_update_interval` | "startup", "6h", "24h", "disabled" | How often to check for plugin updates |

## Schema Migrations

Migrations are handled inline in `initDB()`:

```go
// Initial table creation
db.Exec(createTableSQL)

// Migrations (idempotent - ALTER TABLE silently fails if column exists)
db.Exec("ALTER TABLE clips ADD COLUMN is_archived INTEGER DEFAULT 0")
db.Exec("ALTER TABLE clips ADD COLUMN expires_at DATETIME")
db.Exec("ALTER TABLE watched_folders ADD COLUMN auto_tag_id INTEGER")
db.Exec("ALTER TABLE plugin_permissions ADD COLUMN pending_reconfirm INTEGER DEFAULT 0")
db.Exec("ALTER TABLE plugins ADD COLUMN source_url TEXT DEFAULT ''")
db.Exec("ALTER TABLE clips ADD COLUMN content_hash TEXT DEFAULT ''")
db.Exec("ALTER TABLE clips ADD COLUMN metadata TEXT DEFAULT '{}'")
db.Exec("CREATE INDEX IF NOT EXISTS idx_clips_content_hash ON clips(content_hash)")
```

Migrations use `ALTER TABLE` which silently fails if column exists.

## Database Configuration

### DSN-Based Pragmas

All pragmas are set in the DSN string so they apply to every pooled connection:

```go
dsn := dbPath + "?_busy_timeout=5000&_journal_mode=wal&_foreign_keys=on"
db, err := sql.Open("sqlite", dsn)
```

| Pragma | Value | Purpose |
|--------|-------|---------|
| `busy_timeout` | 5000 | Wait up to 5 seconds when database is locked |
| `journal_mode` | WAL | Write-Ahead Logging for better read/write concurrency |
| `foreign_keys` | ON | Required for CASCADE deletes on `clip_tags`, `plugin_permissions`, `plugin_storage` |

Setting pragmas via DSN (not `db.Exec`) is important because `sql.Open` may use a connection pool. A `db.Exec("PRAGMA ...")` call only applies to one connection, while DSN-based pragmas apply to all connections the pool creates.

Closed on application shutdown:

```go
func (a *App) shutdown(ctx context.Context) {
    if a.db != nil {
        a.db.Close()
    }
}
```

## Common Queries

### Get clips for gallery

Basic query (no tag filters):
```sql
SELECT id, content_type, filename, created_at, expires_at,
       SUBSTR(data, 1, 500), is_archived
FROM clips
WHERE is_archived = ?
  AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)
ORDER BY created_at DESC
LIMIT 50
```

Note: Only first 500 bytes of data fetched for preview. When tag filters or hidden tags are active, the query uses JOINs with `clip_tags` for filtering.

### Get full clip data

```sql
SELECT content_type, data, filename
FROM clips
WHERE id = ?
```

### Insert new clip

```sql
INSERT INTO clips (content_type, data, filename, expires_at)
VALUES (?, ?, ?, ?)
```

### Toggle archive status

```sql
UPDATE clips
SET is_archived = NOT is_archived
WHERE id = ?
```

### Delete expired clips

```sql
DELETE FROM clips
WHERE expires_at IS NOT NULL
  AND expires_at <= CURRENT_TIMESTAMP
```

Runs every 60 seconds via cleanup job.

### Bulk operations

```sql
-- Bulk delete
DELETE FROM clips WHERE id IN (?, ?, ?)

-- Bulk archive toggle
UPDATE clips SET is_archived = NOT is_archived WHERE id IN (?, ?, ?)
```

## Data Storage Paths

| Platform | Path |
|----------|------|
| macOS | `~/Library/Application Support/mahpastes/clips.db` |
| Windows | `%APPDATA%\mahpastes\clips.db` |
| Linux | `~/.config/mahpastes/clips.db` |

## Backup

To backup your data:

```bash
# macOS
cp ~/Library/Application\ Support/mahpastes/clips.db backup.db

# Windows
copy %APPDATA%\mahpastes\clips.db backup.db

# Linux
cp ~/.config/mahpastes/clips.db backup.db
```

## Data Size Considerations

- Each clip stores full binary data
- Large images/files can grow database quickly
- No automatic cleanup except expiration
- Consider archiving + periodically clearing old clips

## Viewing the Database

Use any SQLite viewer:

```bash
# Command line
sqlite3 ~/Library/Application\ Support/mahpastes/clips.db

# Queries
.schema              -- Show all tables
SELECT * FROM clips LIMIT 5;
SELECT * FROM watched_folders;
SELECT * FROM settings;
```

Popular GUI tools:
- DB Browser for SQLite
- TablePlus
- DBeaver
