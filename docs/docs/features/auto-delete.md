---
sidebar_position: 5
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

Clips with expiration show:
- A clock icon
- Remaining time until deletion

## Expiration Options

| Duration | Best For |
|----------|----------|
| **5 min** | Very temporary content, passwords, quick shares |
| **10 min** | Short-term storage, quick reference |
| **30 min** | Working session content |
| **2 hours** | Longer work sessions, meeting notes |
| **Never** | Permanent clips (default) |

## Canceling Expiration

Changed your mind? Remove the expiration before it triggers:

1. Find the clip with the clock icon
2. Click the clock icon
3. Select **Cancel expiration**

The clip becomes permanent.

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
- Any temporary files associated with the clip

### What Doesn't Get Deleted

- Archived clips (archiving removes expiration)
- Clips with "Never" expiration
- Clips where expiration was canceled

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

Archiving a clip **removes its expiration**:

1. Clip has 5-minute expiration
2. You archive it before expiration
3. Expiration is canceled
4. Clip stays in archive permanently

This protects important content from accidental deletion.

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

Auto-deleted clips cannot be recovered. If you need content:
- Cancel expiration before it triggers
- Archive important clips immediately
- Use "Never" for anything you might need later
