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

If using Apple Silicon (M1/M2/M3):
1. Ensure you downloaded the Universal build
2. Try the Intel build via Rosetta if issues persist

### Windows: SmartScreen Warning

Windows may show a SmartScreen warning for unsigned apps.

**Solution:**
1. Click "More info"
2. Click "Run anyway"

### Linux: App Won't Start

Ensure GTK3 is installed:

```bash
# Ubuntu/Debian
sudo apt install libgtk-3-0 libwebkit2gtk-4.0-37

# Fedora
sudo dnf install gtk3 webkit2gtk3

# Arch
sudo pacman -S gtk3 webkit2gtk
```

## Runtime Issues

### Paste Not Working

**Symptoms:** Pressing Cmd/Ctrl+V doesn't add clips.

**Solutions:**
1. Ensure mahpastes window is focused
2. Check if viewing Archive (can't paste to archive)
3. Verify clipboard has content
4. Try drag and drop instead

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
4. If persists, delete WAL files (mahpastes closed first):
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
1. Check Archive tab (if auto-archive enabled)
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

**Symptoms:** Folder shows red/warning status.

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
2. Try pressing keyboard shortcut (B for brush)
3. Check color isn't same as background
4. Increase stroke width if very thin

### Undo/Redo Not Working

**Symptoms:** Cmd/Ctrl+Z doesn't undo.

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
1. Expiration triggered (check expiration settings)
2. Accidentally deleted
3. Database corruption (rare)

**Prevention:**
- Archive important clips (removes expiration)
- Use longer expiration times
- Regular backups

### Corrupted Database

**Symptoms:** App crashes, errors about database.

**Solutions:**
1. Restore from backup if available
2. Try database recovery:
   ```bash
   sqlite3 clips.db ".recover" | sqlite3 recovered.db
   mv recovered.db clips.db
   ```
3. Delete database and start fresh (last resort)

## Platform-Specific Issues

### macOS: Drag and Drop from Safari

**Issue:** Dragging images from Safari doesn't work.

**Solution:** Right-click and save image first, then drag file.

### Windows: Blurry UI

**Issue:** App appears blurry on high-DPI displays.

**Solution:**
1. Right-click app executable
2. Properties → Compatibility
3. Change high DPI settings
4. Override scaling behavior: Application

### Linux: Clipboard Issues

**Issue:** Can't paste images from certain apps.

**Cause:** Clipboard format compatibility varies between Linux apps.

**Solutions:**
1. Save image to file first
2. Use drag and drop
3. Try different source apps

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
