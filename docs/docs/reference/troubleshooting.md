---
sidebar_position: 2
---

# Troubleshooting

Common issues and their solutions.

## Installation Issues

### macOS: "App is damaged" or Security Warning

Release downloads are signed and notarized, so Gatekeeper opens them without a
warning. If macOS still reports the app as damaged, download
`mahpastes-macos-universal.zip` again from the
[Releases page](https://github.com/egeozcan/mahpastes/releases) and extract it
with Finder or `ditto -x -k`; tools that drop extended attributes or symlinks
while unpacking break the signature.

An unsigned app that was downloaded or copied from another Mac, such as an
unsigned build artifact, is blocked instead. Allow it once:
1. Try to open the app, then dismiss the warning
2. Open **System Settings > Privacy & Security**
3. Click **Open Anyway** next to the message about mahpastes and confirm
4. The app launches and macOS remembers the choice

On macOS 14 and earlier you can instead right-click the app, select "Open" and
click "Open" in the dialog. Or clear the download quarantine via Terminal:
```bash
xattr -cr /Applications/mahpastes.app
```

### macOS: App Won't Open (Apple Silicon)

The release app is a universal build that runs natively on Intel and Apple
Silicon and needs macOS 13 or later. Check **About This Mac** for the macOS
version, and make sure you copied the whole `mahpastes.app` out of the archive.

### Windows: SmartScreen Warning

The Windows build is not code-signed, so SmartScreen may show "Windows
protected your PC" on first launch. Click **More info**, then **Run anyway**.
On ARM PCs, use `mahpastes-windows-arm64-installer.exe` or
`mahpastes-windows-arm64.zip`. The x64 installer refuses to install on ARM
("can't be installed on the current Windows architecture"); the x64 zip runs,
but under emulation.

### Linux: App Won't Start

The release build needs GTK 3, WebKitGTK 4.1 and glibc 2.39 or newer (Ubuntu
24.04, Debian 13, Fedora 40 and later). An error such as `libwebkit2gtk-4.1.so.0:
cannot open shared object file` means the library is missing: install
`libwebkit2gtk-4.1-0` (Debian/Ubuntu) or `webkit2gtk4.1` (Fedora). On older
distributions, such as Ubuntu 22.04, build from source instead; the build falls
back to WebKitGTK 4.0 there.

If the app does not appear in the application menu after `./install.sh`, log
out and back in, or check that `~/.local/share/applications/mahpastes.desktop`
exists.

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

**Symptoms:** App crashes, errors about database, or a "Could not open the database" dialog at startup.

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
1. Drag-out is supported on macOS, Windows and Linux. On Linux the drop target must accept a `text/uri-list` file drop (Dolphin, Nautilus and most GTK/Qt apps do); results can vary by compositor and receiving app. See [Drag and Drop](../features/drag-and-drop.md#linux).
2. Hover the grip handle on the clip card for at least 1 second to arm the drag.
3. Wait for the handle to finish the preparation animation before dragging.
4. If the handle shows a spinner, the temp file is still being prepared. Wait until it returns to the grip icon.

### Copy File Not Working

**Symptoms:** "Copy File" from the card menu doesn't paste a file in Finder (macOS), Explorer (Windows) or the Linux file manager.

**Solutions:**
1. Copy File is supported on macOS (NSPasteboard), Windows (PowerShell) and Linux. If the copy itself fails, for example in a Linux session where no clipboard is reachable, mahpastes shows "Failed to copy file." instead of a success message.
2. On Linux the file is offered as one clipboard type only, picked from `XDG_CURRENT_DESKTOP`: `x-special/gnome-copied-files` on Cinnamon, MATE, Xfce, Budgie and Pantheon (Nemo, Caja, Thunar and elementary Files read only that type), `text/uri-list` everywhere else (Dolphin, Nautilus 43 and later, PCManFM, browsers and chat apps). Pasting into an app that wants the other type, or into a terminal, pastes nothing. Paste before quitting mahpastes: the clipboard offer ends when the app exits.
3. On Windows, ensure no policy blocks PowerShell clipboard access.
4. Try "Copy Contents" instead if you only need the raw data (text or image).

## Platform-Specific Issues

### Window Opens in the Wrong Place or at the Wrong Size

**Issue:** After changing monitors or display scaling, the window opens somewhere awkward.

**Solution:** Quit mahpastes, delete `window.json` from the [data directory](./data-storage#window-state), and start it again. The window opens centred at its default size, and the new placement is remembered from then on.

### System Notifications Do Not Appear

**Issue:** **System Notifications** is on, but nothing shows when a watch folder imports in the background.

**Solution:**
1. Open **Settings → System Notifications**. If it says "Not available", the reason follows: on macOS allow mahpastes under **System Settings → Notifications** (the bare binary that `wails dev` runs cannot notify at all; a built `.app` can); on Linux make sure a notification daemon is running.
2. Click **Send Test Notification**. If nothing appears, Focus / Do Not Disturb / Focus assist is probably hiding it.
3. Notifications only appear while the window is unfocused, and similar events are combined, so a burst of imports shows as one notification after a couple of seconds.

See [System Notifications](../features/notifications).

### macOS: Drag and Drop from Safari

**Issue:** Dragging images from Safari doesn't work.

**Solution:** Right-click and save image first, then drag file.

### Import Folder: Files Marked for Deletion Are Not Deleted

**Issue:** The Import Folder summary reports files it could not delete, and they are still in the folder.

The wizard moves deleted originals to the Trash (macOS, Linux) or the Recycle Bin (Windows). When a file cannot be moved there, the wizard leaves it in place and reports it, instead of deleting it permanently.

**Causes:**
- **Linux:** the folder is on a mount without trash support, such as `/tmp` or another tmpfs or system-internal mount, or the mount does not allow a `.Trash-<uid>` folder.
- **Windows:** the folder is on a network share, a mapped network drive or a removable drive. These have no Recycle Bin. Paths longer than 259 characters are also refused.

**Solutions:**
1. Delete the remaining files yourself in your file manager.
2. Move the folder to a local disk before importing it.

On Linux, moving files to the Trash needs the `gio` tool (glib2; the `libglib2.0-bin` package on Debian and Ubuntu). Without it, the wizard warns that deletion is permanent before you choose any deletes.

On Windows, a file too large for the Recycle Bin is still deleted permanently.

## REST API Issues

### Connection Refused

**Symptoms:** API calls fail with "connection refused."

**Solutions:**
1. Verify mahpastes is running
2. Open **Settings** > **API** and confirm the API is enabled. The server does not start with the app unless **Start on launch** is on
3. Check the port matches your `MP_API_URL` (default `http://localhost:44557`)

### 401 Unauthorized

**Symptoms:** API calls return 401.

**Solutions:**
1. Check your `MP_API_KEY` environment variable (or the file `MP_API_KEY_FILE` names) holds the right key
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

### "MP_API_KEY environment variable (or MP_API_KEY_FILE) is required"

**Symptoms:** Any `mp` command fails immediately with exit code 3.

**Solution:** Export your API key before running commands:
```bash
export MP_API_KEY=mp_your_key_here
```

Or keep the key in a file and point `MP_API_KEY_FILE` at it:
```bash
export MP_API_KEY_FILE=~/.config/mahpastes-key
```

Add the export to your shell profile (`~/.zshrc` or `~/.bashrc`) for persistence.

### "MP_API_KEY_FILE ... is empty" or "must contain only the API key"

**Symptoms:** Any `mp` command fails immediately when `MP_API_KEY_FILE` is set.

**Solution:** The file must hold just the key, such as `mp_abc123...`, optionally followed by a newline. Remove blank or extra lines, spaces inside the key, and quotes.

### "Cannot connect to mahpastes"

**Symptoms:** Commands fail with the message "Cannot connect to mahpastes. Is the app running with the API enabled?"

If `MP_API_URL` points at a host that does not answer at all, this message appears after `MP_TIMEOUT` (default `30s`).

**Solutions:**
1. Verify the mahpastes desktop app is running
2. Confirm the API is enabled in **Settings** > **API**. Turn on **Start on launch** there so it starts with the app every time
3. If using a custom URL, check `MP_API_URL`:
   ```bash
   export MP_API_URL=http://localhost:44557
   mp api status
   ```

### "Timed out waiting for mahpastes"

**Symptoms:** A command fails with exit code 2 after about 30 seconds.

**Cause:** `mp` connected, but the server did not start responding within `MP_TIMEOUT` (default `30s`). This usually means the app is hung or something else is listening on that port.

**Solutions:**
1. Check that `MP_API_URL` points at mahpastes and that the app is responsive
2. On a slow network link, raise the limit, for example `export MP_TIMEOUT=2m`

### "Authentication failed. Check MP_API_KEY (or MP_API_KEY_FILE)."

**Symptoms:** Commands fail with exit code 3.

**Solutions:**
1. Verify `MP_API_KEY` (or the file `MP_API_KEY_FILE` names) holds a valid, non-revoked key
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
   - The log file: **Maintenance → Open Log Folder** opens the folder with `mahpastes.log` (see [Log Files](data-storage.md#log-files)); if the app does not start, find it under `logs/` in the data directory
   - Screenshots (if relevant)
