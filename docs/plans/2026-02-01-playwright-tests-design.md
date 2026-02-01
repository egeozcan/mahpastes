# Playwright Test Suite Design for mahpastes

## Overview

Add comprehensive Playwright end-to-end tests with full parallel isolation via per-worker Wails instances.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Playwright Test Runner                    │
├──────────────┬──────────────┬──────────────┬───────────────┤
│   Worker 0   │   Worker 1   │   Worker 2   │   Worker N    │
│   Port 34115 │   Port 34116 │   Port 34117 │   Port 3411X  │
├──────────────┼──────────────┼──────────────┼───────────────┤
│  Wails Dev   │  Wails Dev   │  Wails Dev   │  Wails Dev    │
│  Instance 0  │  Instance 1  │  Instance 2  │  Instance N   │
├──────────────┼──────────────┼──────────────┼───────────────┤
│  SQLite DB   │  SQLite DB   │  SQLite DB   │  SQLite DB    │
│  /tmp/test-0 │  /tmp/test-1 │  /tmp/test-2 │  /tmp/test-N  │
└──────────────┴──────────────┴──────────────┴───────────────┘
```

## Implementation Steps

### 1. Go Backend Modification
- Add `MAHPASTES_DATA_DIR` environment variable support in `database.go`
- Allows tests to use isolated database directories

### 2. Test Infrastructure
- `e2e/playwright.config.ts` - Playwright configuration
- `e2e/global-setup.ts` - Spawn Wails instances before tests
- `e2e/global-teardown.ts` - Kill instances after tests
- `e2e/fixtures/test-fixtures.ts` - Custom fixture with AppHelper

### 3. Helper Modules
- `e2e/helpers/wails-manager.ts` - Manages Wails process lifecycle
- `e2e/helpers/test-data.ts` - Test data generators
- `e2e/helpers/selectors.ts` - Centralized DOM selectors

### 4. Test Suites
- `e2e/tests/clips/` - Upload, view, delete, archive
- `e2e/tests/bulk/` - Selection, bulk operations
- `e2e/tests/images/` - Lightbox, editor, comparison
- `e2e/tests/watch/` - Folders, filters, auto-import
- `e2e/tests/search/` - Filtering
- `e2e/tests/edge-cases/` - Expiration, errors

## Key Design Decisions

1. **Per-worker isolation** - Each Playwright worker gets its own Wails instance
2. **Port allocation** - Base port 34115, each worker gets 34115 + workerIndex
3. **Data isolation** - Each instance uses `/tmp/mahpastes-test-{workerIndex}/`
4. **AppHelper class** - Provides clean API for test interactions
5. **Automatic cleanup** - Fixtures delete all clips after each test
