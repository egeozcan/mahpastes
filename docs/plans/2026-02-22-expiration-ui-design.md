# Expiration UI Design

## Overview

Add frontend controls for the existing clip expiration backend. Users can set expiration at upload time or on existing clips, with bulk support.

## Backend Changes

- Add `SetExpiration(id int64, minutes int)` — sets `expires_at` on an existing clip
- Add `BulkSetExpiration(ids []int64, minutes int)` — bulk variant
- Add `BulkCancelExpiration(ids []int64)` — bulk cancel
- Existing `UploadFiles(files, expirationMinutes)` and `CancelExpiration(id)` unchanged

## Upload Flow

- Add a persistent expiration dropdown in the toolbar area near the upload trigger
- Options: **None** (default), **15m**, **1h**, **6h**, **24h**, **7d**
- Selected value passed to `UploadFiles` as `expirationMinutes`
- Selection persists for the session (resets to None on app restart)
- Styled as a compact `<select>` or button-group matching the stone design system

## Context Menu (Existing Clips)

- **No expiration set**: "Set Expiration" menu item opens a popover with 5 preset pill buttons (15m, 1h, 6h, 24h, 7d)
- **Expiration already set**: "Cancel Expiration" menu item clears expiration immediately (no popover)
- Positioned between "Tags" and "Archive" in the context menu
- Popover uses the same positioning logic as the card menu (anchored, viewport-aware)
- Styled as a compact row of pill buttons in stone palette

## Bulk Operations

- **"Set Expiration"** button in bulk toolbar opens the same preset popover, calls `BulkSetExpiration`
- **"Cancel Expiration"** button shown when any selected clip has expiration, calls `BulkCancelExpiration`
- Keyboard shortcut: `x` in bulk context
- If toolbar gets too crowded, overflow into a three-dot menu (only if needed)

## Temp Badge Enhancement

- Current "Temp" badge enhanced to show remaining time: `Temp · 23m`, `Temp · 2h`, `Temp · 3d`
- Rounding: minutes if < 1h, hours if < 24h, days if >= 24h
- No "left" suffix — kept compact
- Updates on gallery refresh, no live countdown
- Utility function `formatTimeRemaining(expiresAt)` handles formatting

## Auto-Refresh on Focus

- Call `loadClips()` when the app window regains focus
- Solves stale gallery (expired clips still showing) and stale badge times
- Uses Wails runtime window focus event

## Docs

- Docusaurus section covering expiration UX (upload, context menu, bulk, badge)
- CLAUDE.md updated if new patterns introduced

## Presets

| Label | Minutes |
|-------|---------|
| 15m   | 15      |
| 1h    | 60      |
| 6h    | 360     |
| 24h   | 1440    |
| 7d    | 10080   |
