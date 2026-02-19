---
sidebar_position: 6
---

# Auto-Delete

Set clips to automatically delete after a specified time. Keep your clipboard clean without manual maintenance.

## How It Works

1. When adding a clip, select an expiration time
2. The clip is stored with an expiration timestamp
3. A background job checks for expired clips every minute
4. Expired clips are permanently deleted

## Setting Expiration

### When Pasting

Before pasting content:

1. Click the expiration dropdown (default: "Never")
2. Select a duration:
   - **5 minutes**
   - **10 minutes**
   - **30 minutes**
   - **2 hours**
3. Paste your content

The clip is created with the selected expiration.

### Visual Indicator

Clips with expiration show a **Temp** badge in the top-left corner of the clip card.

## Expiration Options

| Duration | Best For |
|----------|----------|
| **5 min** | Very temporary content, passwords, quick shares |
| **10 min** | Short-term storage, quick reference |
| **30 min** | Working session content |
| **2 hours** | Longer work sessions, meeting notes |
| **Never** | Permanent clips (default) |

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

### Sensitive Content

For passwords, tokens, or private data:

1. Paste with 5-minute expiration
2. Use the content as needed
3. Content auto-deletes, reducing exposure

### Quick Transfers

Moving content between apps temporarily:

1. Paste with short expiration
2. Copy back when needed
3. Don't worry about cleanup

### Work Sessions

For content relevant only during a session:

1. Set 2-hour expiration
2. Work freely with clips
3. Everything cleans up after you're done

### Decluttering

To prevent clip accumulation:

1. Default to using expiration for most clips
2. Only leave permanent what you truly need
3. Archive important items instead

## Interaction with Archive

Archiving does not affect expiration. Archived clips with an active timer will still be auto-deleted when their time expires.

## Tips

### Develop Good Habits

- Default to short expiration for transient content
- Use archive for anything important
- Let mahpastes clean up the rest

### Security Consideration

Expiration helps with security hygiene:
- Sensitive data doesn't linger
- Reduces exposure window
- Automatic cleanup is reliable

### Don't Rely on It for Secrets

While helpful, auto-delete is not a security feature:
- Content exists until deleted
- The database file may retain traces
- Use proper secret management for truly sensitive data

## Troubleshooting

### Clip Not Deleted

If a clip isn't deleted at expected time:
- The cleanup job runs every 60 seconds
- There may be up to 60 seconds delay
- Ensure mahpastes is running

### Want Longer Durations?

Currently fixed at 5/10/30 min and 2 hours. For longer temporary storage:
- Use 2 hours as maximum
- Manually delete when done
- Or archive if truly important

### Accidentally Deleted

Auto-deleted clips cannot be recovered. To avoid losing content:
- Use "Never" expiration for anything you might need later
- Archive important clips (but note this does not cancel expiration)
- Create backups regularly
