---
sidebar_position: 6
---

# Auto-Delete

Set clips to automatically delete after a specified time. Keep your clipboard clean without manual maintenance.

## How It Works

1. A clip is stored with an expiration timestamp (if one is set)
2. A background job checks for expired clips every minute
3. Expired clips are permanently deleted

## Setting Expiration

:::warning Not Yet Implemented in UI
The backend supports per-clip expiration with presets (5, 10, 30, and 120 minutes), but the frontend does not expose this setting. All clips are currently created with no expiration. The backend API accepts an `expirationMinutes` parameter, so this feature may be surfaced in a future release.
:::

### Visual Indicator

Clips with expiration show a **Temp** badge in the top-left corner of the clip card.

## Canceling Expiration

There is no UI button to cancel expiration. The `CancelExpiration` backend API exists but is not exposed in the interface.

:::warning
Archiving a clip does **not** cancel its expiration. The cleanup job deletes expired clips regardless of archive status.
:::

## Automatic Cleanup

### Background Job

mahpastes runs a cleanup job that:
- Runs every 60 seconds
- Checks for expired clips
- Deletes them from the database
- Frees up storage space

### What Gets Deleted

- The clip content (image, text, etc.)
- The database record

:::note
The cleanup job only runs a SQL DELETE. It does not clean up temporary files or emit plugin events for expired clips. This differs from manual deletion, which cleans up temp files, orphaned tags, and emits `clip:deleted` plugin events.
:::

### What Doesn't Get Deleted

- Clips with "Never" expiration (the default)
- Clips where expiration was canceled via the API

## Use Cases

Once the expiration UI is implemented, typical use cases will include:

- **Sensitive content** -- passwords, tokens, or private data that auto-deletes after a few minutes
- **Quick transfers** -- temporary clips that clean up on their own
- **Work sessions** -- content relevant only during a session, set to expire after 2 hours

## Interaction with Archive

Archiving does not affect expiration. Archived clips with an active timer will still be auto-deleted when their time expires.

## Tips

### Security Consideration

Auto-delete is not a security feature:
- Content exists in the database until the cleanup job runs
- The database file may retain traces after deletion
- Use proper secret management for truly sensitive data

## Troubleshooting

### Clip Not Deleted

If a clip isn't deleted at expected time:
- The cleanup job runs every 60 seconds
- There may be up to 60 seconds delay
- Ensure mahpastes is running

### Accidentally Deleted

Auto-deleted clips cannot be recovered. To avoid losing content:
- Use "Never" expiration for anything you might need later
- Archive important clips (but note this does not cancel expiration)
- Create backups regularly
