# CGo-Free Windows Compilation — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace `mattn/go-sqlite3` with `modernc.org/sqlite` so the project compiles without CGo on Windows (and all platforms).

**Architecture:** Swap the SQLite driver import and driver name string in the two files that reference it. Update `go.mod`. The `database/sql` interface is identical — all other code uses `*sql.DB` and is driver-agnostic.

**Tech Stack:** `modernc.org/sqlite` (pure-Go SQLite via C-to-Go transpilation)

---

### Task 1: Swap the SQLite driver

**Files:**
- Modify: `go.mod` (line 8: remove `mattn/go-sqlite3`, add `modernc.org/sqlite`)
- Modify: `database.go:12` (import) and `database.go:63` (driver name)
- Modify: `temp_clip_store_test.go:10` (import) and `temp_clip_store_test.go:16` (driver name)

**Step 1: Replace the import in `database.go`**

Change line 12 from:
```go
	_ "github.com/mattn/go-sqlite3"
```
to:
```go
	_ "modernc.org/sqlite"
```

**Step 2: Replace the driver name in `database.go`**

Change line 63 from:
```go
	db, err := sql.Open("sqlite3", dbPath)
```
to:
```go
	db, err := sql.Open("sqlite", dbPath)
```

**Step 3: Replace the import in `temp_clip_store_test.go`**

Change line 10 from:
```go
	_ "github.com/mattn/go-sqlite3"
```
to:
```go
	_ "modernc.org/sqlite"
```

**Step 4: Replace the driver name in `temp_clip_store_test.go`**

Change line 16 from:
```go
	db, err := sql.Open("sqlite3", dbPath)
```
to:
```go
	db, err := sql.Open("sqlite", dbPath)
```

**Step 5: Update Go modules**

Run:
```bash
go get modernc.org/sqlite
go mod tidy
```

Expected: `go.mod` gains `modernc.org/sqlite` and loses `github.com/mattn/go-sqlite3`. `go.sum` updates accordingly.

**Step 6: Run the Go unit test to verify the driver works**

Run:
```bash
go test ./... -v -count=1
```

Expected: All tests pass (specifically `temp_clip_store_test.go` tests exercise the new driver).

**Step 7: Commit**

```bash
git add database.go temp_clip_store_test.go go.mod go.sum
git commit -m "feat: replace mattn/go-sqlite3 with modernc.org/sqlite for CGo-free builds"
```

### Task 2: Update documentation references

**Files:**
- Modify: `docs/docs/developers/backend.md:137` (driver name in code example)
- Modify: `docs/docs/developers/database-schema.md:277` (driver name in code example)

**Step 1: Update `backend.md`**

Find the `sql.Open("sqlite3"` reference around line 137 and change to `sql.Open("sqlite"`. Also update the import if `mattn/go-sqlite3` appears nearby — change to `modernc.org/sqlite`.

**Step 2: Update `database-schema.md`**

Find the `sql.Open("sqlite3"` reference around line 277 and change to `sql.Open("sqlite"`. Same import fix if present.

**Step 3: Update any `go.mod` snippets in docs**

Check `backend.md` around line 384 for the `go.mod` listing that shows `mattn/go-sqlite3` — update to `modernc.org/sqlite`.

**Step 4: Commit**

```bash
git add docs/
git commit -m "docs: update SQLite driver references to modernc.org/sqlite"
```

### Task 3: Run e2e tests to verify nothing broke

**Step 1: Run e2e tests**

Run:
```bash
cd e2e && npm test
```

Expected: All tests pass. The driver swap is transparent to the application layer.

**Step 2: If tests pass, done. If tests fail, investigate.**

The most likely failure mode is a SQLite pragma or query syntax difference — but `modernc.org/sqlite` is a faithful SQLite translation, so this is unlikely.
