# CGo-Free Windows Compilation

## Problem

`mattn/go-sqlite3` requires CGo, which means Windows users need MinGW/MSYS2 to compile. This is a poor developer experience.

## Solution

Replace `mattn/go-sqlite3` with `modernc.org/sqlite` on all platforms. This is a pure-Go SQLite implementation (C-to-Go transpilation) that registers as the `"sqlite"` driver for `database/sql`.

## Changes

1. **`go.mod`** — Swap dependency
2. **`database.go`** — Change import and driver name (`"sqlite3"` → `"sqlite"`)
3. **`temp_clip_store_test.go`** — Same import and driver name change

## Trade-offs

- Binary size increases ~10-15MB
- Marginal performance difference (negligible for this app's workload)
- Eliminates CGo requirement on all platforms, not just Windows
