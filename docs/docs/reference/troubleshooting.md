---
sidebar_position: 2
---

# Troubleshooting

Common issues and their solutions.

## Installation Issues

### macOS: "App is damaged" or Security Warning

macOS Gatekeeper may block unsigned apps.

**Solution:**
1. Right-click the app
2. Select "Open"
3. Click "Open" in the dialog
4. App will launch and be remembered

Or via Terminal:
```bash
xattr -cr /Applications/mahpastes.app
```

### macOS: App Won't Open (Apple Silicon)

If using Apple Silicon (M-series):
1. Ensure you downloaded the Universal build
2. Try the Intel build via Rosetta if issues persist

## Runtime Issues

### Paste Not Working

**Symptoms:** Pressing Cmd+V doesn't add clips.

**Solutions:**
1. Ensure mahpastes window is focused
2. Click outside any text field -- paste is disabled when a search box, form input, or text area is focused (the keystroke goes to the field instead)
3. Check if viewing Archive (can't paste to archive)
4. Verify clipboard has content
5. Try drag and drop instead

### Clips Not Appearing

**Symptoms:** Paste or drop works but clip doesn't show.

**Solutions:**
1. Refresh the gallery (switch tabs and back)
2. Check if accidentally archived
3. Check if expiration is set too short
4. Restart mahpastes

### Database Locked Error

**Symptoms:** Operations fail with database locked error.

**Solutions:**
1. Close any other apps accessing the database
2. Close SQLite tools if open
3. Restart mahpastes
4. If persists, delete WAL files (quit mahpastes first). macOS path shown -- substitute your platform's path from [Data Storage](data-storage.md#database):
   ```bash
   rm ~/Library/Application\ Support/mahpastes/clips.db-wal
   rm ~/Library/Application\ Support/mahpastes/clips.db-shm
   ```

### High Memory Usage

**Symptoms:** App uses excessive memory.

**Solutions:**
1. Delete large clips you don't need
2. Reduce number of clips in gallery
3. Archive old clips
4. Restart mahpastes periodically

## Watch Folder Issues

### Files Not Importing

**Symptoms:** New files in watched folder don't appear.

**Checklist:**
1. ✓ Watch folder is not paused
2. ✓ Global watching is not paused
3. ✓ Folder path still exists
4. ✓ File matches filter settings
5. ✓ File is not hidden (starts with .)

**Test filter:**
- Temporarily set filter to "All files"
- If imports work, adjust your filter

### Files Importing but Not Appearing

**Symptoms:** Import notification shows but clip isn't visible.

**Solutions:**
1. Check Archive view (if auto-archive enabled)
2. Refresh the gallery
3. Check for errors in notifications

### Original Files Not Deleted

**Symptoms:** Files import but remain in folder.

**Causes:**
- File was open in another app
- Permission issues
- Import failed silently

**Solutions:**
1. Close apps that might have the file open
2. Check folder permissions
3. Look for error notifications

### Watch Folder Shows Error

**Symptoms:** Folder shows an amber "Folder not found" badge.

**Causes:**
- Folder was deleted or moved
- Permission changed
- Network drive disconnected

**Solutions:**
1. Re-add the folder with correct path
2. Check folder exists and is accessible
3. Remove and re-add the watch

## Editor Issues

### Image Editor Tools Not Working

**Symptoms:** Can't draw on canvas.

**Solutions:**
1. Ensure a tool is selected (check toolbar highlight)
2. Select a tool by clicking its button in the toolbar
3. Check color isn't same as background
4. Increase stroke width if very thin

### Undo/Redo Not Working

**Symptoms:** Cmd+Z doesn't undo.

**Solutions:**
1. Ensure editor window is focused
2. Check if at start of history (nothing to undo)
3. Try clicking canvas first, then shortcut

### Text Not Visible

**Symptoms:** Text tool adds text but can't see it.

**Solutions:**
1. Change text color (may be same as background)
2. Increase font size
3. Click in a visible area of the image

## Performance Issues

### Slow Gallery Loading

**Symptoms:** Gallery takes long to load.

**Causes:**
- Many clips in database
- Large images
- Slow disk

**Solutions:**
1. Archive old clips
2. Delete unused clips
3. Set expiration for temporary clips

### Slow Image Editor

**Symptoms:** Editor is laggy when drawing.

**Causes:**
- Very large image
- Many history states
- Low system resources

**Solutions:**
1. Work with smaller images when possible
2. Save periodically (resets history)
3. Close other resource-heavy apps

## Data Issues

### Lost Clips

**Symptoms:** Clips disappeared unexpectedly.

**Possible causes:**
1. Expiration triggered -- expiration is set per-clip at upload time and runs on a 1-minute cleanup cycle
2. Accidentally deleted
3. Database corruption (rare)

**Prevention:**
- Use "Never" expiration for clips you want to keep
- Archive important clips for long-term storage (note: archiving does not cancel expiration)
- Regular backups

### Corrupted Database

**Symptoms:** App crashes, errors about database.

**Solutions:**
1. Restore from backup if available
2. Try database recovery (quit mahpastes first, macOS path shown -- substitute your platform's path from [Data Storage](data-storage.md#database)):
   ```bash
   cd ~/Library/Application\ Support/mahpastes/
   sqlite3 clips.db ".recover" | sqlite3 recovered.db
   mv recovered.db clips.db
   ```
3. Delete database and start fresh (last resort)

## Transfer Issues

### Drag-Out Not Working

**Symptoms:** Can't drag clips out of mahpastes into other apps.

**Solutions:**
1. Drag-out is supported on macOS and Windows. Linux support is planned.
2. Hover the grip handle on the clip card for at least 1 second to arm the drag.
3. Wait for the handle to finish the preparation animation before dragging.
4. If the handle shows a spinner, the temp file is still being prepared. Wait until it returns to the grip icon.

### Copy File Not Working

**Symptoms:** "Copy File" from the card menu doesn't paste a file in Finder.

**Solutions:**
1. Copy File is supported on macOS (NSPasteboard) and Windows (PowerShell). Not supported on Linux.
2. On Windows, ensure no policy blocks PowerShell clipboard access.
3. Try "Copy Contents" instead if you only need the raw data (text or image).

## Platform-Specific Issues

### macOS: Drag and Drop from Safari

**Issue:** Dragging images from Safari doesn't work.

**Solution:** Right-click and save image first, then drag file.

## REST API Issues

### Connection Refused

**Symptoms:** API calls fail with "connection refused."

**Solutions:**
1. Verify mahpastes is running
2. Open **Settings** > **API** and confirm the API is enabled
3. Check the port matches your `MP_API_URL` (default `http://localhost:44557`)

### 401 Unauthorized

**Symptoms:** API calls return 401.

**Solutions:**
1. Check your `MP_API_KEY` environment variable is set correctly
2. Verify the key has not been revoked in **Settings** > **API**
3. Confirm the key prefix is `mp_`

### 403 Forbidden

**Symptoms:** API calls return 403.

**Solutions:**
1. The key may lack the required role for the requested operation
2. Scoped keys can only access clips within their assigned tag -- verify the key's scope in **Settings** > **API**

### Port Already in Use

**Symptoms:** API fails to start, logs show "address already in use."

**Solutions:**
1. Change the API port in **Settings** > **API**
2. Find and stop the process using the port:
   ```bash
   lsof -i :44557
   ```

## CLI (`mp`) Issues

### "MP_API_KEY environment variable is required"

**Symptoms:** Any `mp` command fails immediately.

**Solution:** Export your API key before running commands:
```bash
export MP_API_KEY=mp_your_key_here
```

Add this line to your shell profile (`~/.zshrc` or `~/.bashrc`) for persistence.

### "Cannot connect to mahpastes"

**Symptoms:** Commands fail with the message "Cannot connect to mahpastes. Is the app running with the API enabled?"

**Solutions:**
1. Verify the mahpastes desktop app is running
2. Confirm the API is enabled in **Settings** > **API**
3. If using a custom URL, check `MP_API_URL`:
   ```bash
   export MP_API_URL=http://localhost:44557
   mp api status
   ```

### "Authentication failed. Check MP_API_KEY."

**Symptoms:** Commands fail with exit code 3.

**Solutions:**
1. Verify `MP_API_KEY` is set to a valid, non-revoked key
2. Regenerate the key in **Settings** > **API** if needed
3. Check for extra whitespace or quotes in the key value

## Plugin Issues

### Plugin Auto-Disabled

**Symptoms:** Plugin stops running; shows as disabled.

**Cause:** After 3 consecutive errors, plugins are automatically disabled.

**Solutions:**
1. Check the plugin logs for error details
2. Fix the underlying issue in the plugin script
3. Re-enable the plugin in the **Plugins** panel

### Permission Denied

**Symptoms:** Plugin fails with a permission error.

**Cause:** The plugin tried to access network or filesystem resources without declaring them in its manifest.

**Solution:** Add the required permission to the plugin's manifest:
- `network = { ["example.com"] = {"GET", "POST"} }` for HTTP requests (specify allowed domains and methods)
- `filesystem = { read = true }` or `filesystem = { read = true, write = true }` for file access
- `clipboard = true` for clipboard writes

### Sandbox Timeout

**Symptoms:** Plugin execution is killed mid-run.

**Cause:** Plugins have execution time limits:
- Event handlers: 30 seconds
- UI actions (synchronous): 5 minutes
- UI actions with `async = true`: 5 minutes, but run in a background goroutine so the UI is not blocked

**Solutions:**
1. Optimize long-running plugin logic
2. For actions that genuinely need time, set `async = true` in the action manifest so execution happens in the background and the UI stays responsive

## Tag Serve Issues

### 401 on `/_api` Requests

**Symptoms:** JSON API calls to a served tag return 401 Unauthorized.

**Cause:** The `/_api` endpoints use cookie authentication. The `_mp_serve_key` cookie is set when you visit the served page.

**Solution:** Load the served tag's page in a browser first -- the cookie is set automatically. Subsequent `/_api` requests from that page will authenticate via the cookie.

### File Upload Rejected

**Symptoms:** `POST /_api/_upload` returns 400 or 403.

**Checklist:**
1. File must be under 10 MB
2. Path in the `tag` form field must not contain `..` or `_api` segments
3. API access must be `readwrite` (not `read` or `none`)
4. Request must include a valid `_mp_serve_key` cookie

### JSON API Returns 404

**Symptoms:** `GET /_api/...` returns 404 for all paths.

**Cause:** API access is set to `"none"` (the default).

**Solution:** Stop and restart the tag server with API access enabled:
- **Read-only:** `--api-access read`
- **Full CRUD:** `--api-access readwrite`

In the UI, set the API access dropdown when starting the serve.

## Getting Help

If issues persist:

1. Check [GitHub Issues](https://github.com/egeozcan/mahpastes/issues) for known problems
2. Search closed issues for solutions
3. Open a new issue with:
   - OS and version
   - mahpastes version
   - Steps to reproduce
   - Error messages (if any)
   - Screenshots (if relevant)
