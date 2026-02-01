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
    expires_at DATETIME
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

**Indexes:**
- Primary key on `id`
- (No additional indexes currently)

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

## Schema Migrations

Migrations are handled inline in `initDB()`:

```go
// Initial table creation
db.Exec(createTableSQL)

// Migrations (idempotent)
db.Exec("ALTER TABLE clips ADD COLUMN is_archived INTEGER DEFAULT 0")
db.Exec("ALTER TABLE clips ADD COLUMN expires_at DATETIME")
```

Migrations use `ALTER TABLE` which silently fails if column exists.

## Database Configuration

### WAL Mode

Write-Ahead Logging enabled for better concurrency:

```go
db.Exec("PRAGMA journal_mode=WAL")
```

Benefits:
- Better read/write concurrency
- Faster writes
- Crash recovery

### Connection

Single connection used throughout application lifetime:

```go
db, err := sql.Open("sqlite3", dbPath)
```

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

```sql
SELECT id, content_type, filename, created_at, expires_at,
       SUBSTR(data, 1, 500), is_archived
FROM clips
WHERE is_archived = ?
  AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)
ORDER BY created_at DESC
LIMIT 50
```

Note: Only first 500 bytes of data fetched for preview.

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
