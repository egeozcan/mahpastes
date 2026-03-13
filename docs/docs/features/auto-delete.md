---
sidebar_position: 6
---

# Auto-Delete

Set clips to automatically delete after a specified time. Keep your clipboard clean without manual maintenance.

## How It Works

1. A clip is stored with an expiration timestamp (if one is set)
2. A background job checks for expired clips every minute
3. Expired clips are permanently deleted

## Setting Expiration at Upload

The upload area includes an **expiration dropdown** next to the upload button. Choose how long the clip should live:

- **No Expiry** (default)
- **15 minutes**
- **1 hour**
- **6 hours**
- **24 hours**
- **7 days**

The selected expiration applies to every clip uploaded until you change it.

## Setting Expiration on Existing Clips

Right-click a clip card to open the context menu and select **Set Expiration**. A popover appears with the same presets (15m, 1h, 6h, 24h, 7d). Choosing a preset sets the expiration relative to the current time.

## Canceling Expiration

Right-click a clip that has an active expiration and select **Cancel Expiration** from the context menu. The clip reverts to no expiry and the Temp badge is removed.

:::warning
Archiving a clip does **not** cancel its expiration. The cleanup job deletes expired clips regardless of archive status.
:::

## Bulk Operations

When multiple clips are selected, the bulk toolbar provides two expiration actions:

- **Set Expiration** -- opens the preset popover and applies the chosen duration to all selected clips
- **Clear Expiry** -- removes expiration from all selected clips

Press <span className="keyboard-key">X</span> as a keyboard shortcut to open the Set Expiration popover for the current selection.

## Visual Indicator

Clips with an active expiration display an enhanced **Temp** badge in the top-left corner of the card. The badge includes the remaining time:

- **Temp &middot; 23m** -- minutes remaining
- **Temp &middot; 2h** -- hours remaining
- **Temp &middot; 3d** -- days remaining

The remaining time refreshes automatically when the app window regains focus, so the badge stays accurate even after switching away.

## Automatic Cleanup

### Background Job

mahpastes runs a cleanup job that:
- Runs every 60 seconds
- Checks for expired clips
- Deletes them from the database

### View Filtering

Expired clips are also filtered out of the gallery view in real time. All clip queries exclude rows where the expiration timestamp has passed, so a clip may vanish from the UI before the background job deletes it from the database. The cleanup job then permanently removes the database record up to 60 seconds later.

### What Gets Deleted

- The clip content (image, text, etc.)
- The database record

:::note
The cleanup job only runs a SQL DELETE. It does not clean up temporary files or emit plugin events for expired clips. This differs from manual deletion, which cleans up temp files, orphaned tags, and emits `clip:deleted` plugin events.
:::

### What Doesn't Get Deleted

- Clips with no expiry (the default)
- Clips where expiration was canceled via the context menu

## Use Cases

- **Sensitive content** -- passwords, tokens, or private data that auto-deletes after 15 minutes
- **Quick transfers** -- temporary clips that clean up on their own
- **Work sessions** -- content relevant only during a session, set to expire after a few hours

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
- Use no expiry for anything you might need later
- Archive important clips (but note this does not cancel expiration)
- Create backups regularly

## REST API & CLI

You can manage expiration programmatically through the [REST API](./rest-api.md) or the `mp` CLI:

```bash
# Set expiration via CLI
mp clip expire <id> --minutes 60

# Cancel expiration via CLI
mp clip expire <id> --clear
```

See the [REST API reference](./rest-api.md#set--cancel-expiration) for the `PUT` and `DELETE` endpoints on `/api/v1/clips/{id}/expiration`.
