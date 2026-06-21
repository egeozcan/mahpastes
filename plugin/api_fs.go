package plugin

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"
)

const (
	FSOperationsPerMinute = 50
	// MaxReadFileSize limits file reads to prevent memory exhaustion (50MB)
	MaxReadFileSize = 50 * 1024 * 1024
)

// PermissionCallback is called when a plugin needs filesystem access
// Returns the approved path (may be different from requested) or empty string if denied
type PermissionCallback func(pluginName string, permType string, requestedPath string) string

// FilesystemAPI provides restricted filesystem access to plugins
type FilesystemAPI struct {
	db              *sql.DB
	pluginID        int64
	pluginName      string
	wantsRead       bool
	wantsWrite      bool
	permCallback    PermissionCallback
	confinementRoot string            // canonical root all access is confined to ("" = unconfined, desktop)
	approvedPaths   map[string]string // permType:path -> approved path
	operationCount  int
	lastReset       time.Time
	mu              sync.Mutex
}

// NewFilesystemAPI creates a new filesystem API. When confinementRoot is
// non-empty (headless server mode) every resolved path must live under it,
// regardless of what the shared plugin_permissions table grants — those rows
// may have been approved by the desktop build for directories outside this
// server's data dir.
func NewFilesystemAPI(db *sql.DB, pluginID int64, pluginName string, perms FilesystemPerms, callback PermissionCallback, confinementRoot string) *FilesystemAPI {
	api := &FilesystemAPI{
		db:            db,
		pluginID:      pluginID,
		pluginName:    pluginName,
		wantsRead:     perms.Read,
		wantsWrite:    perms.Write,
		permCallback:  callback,
		approvedPaths: make(map[string]string),
		lastReset:     time.Now(),
	}
	if confinementRoot != "" {
		if canonical, err := canonicalizePluginPath(confinementRoot); err == nil {
			api.confinementRoot = canonical
		} else {
			api.confinementRoot = filepath.Clean(confinementRoot)
		}
	}

	// Load existing permissions from DB
	api.loadPermissions()

	return api
}

func (f *FilesystemAPI) loadPermissions() {
	// Skip pending_reconfirm rows: a permission imported from a backup is marked
	// pending until the user re-approves it, so it must not be silently honored
	// at the fs layer (it would otherwise resurrect broad grants from a restored
	// backup without any callback).
	rows, err := f.db.Query(
		"SELECT permission_type, path FROM plugin_permissions WHERE plugin_id = ? AND COALESCE(pending_reconfirm, 0) = 0",
		f.pluginID,
	)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var permType, path string
		if err := rows.Scan(&permType, &path); err != nil {
			continue
		}
		// In confined (headless) mode, drop any cached grant whose target is not
		// under the confinement root so a desktop-approved directory cannot be
		// reused to escape the data dir.
		if f.confinementRoot != "" {
			if canonical, cerr := canonicalizePluginPath(path); cerr != nil || !withinRoot(f.confinementRoot, canonical) {
				continue
			}
		}
		f.approvedPaths[permType+":"+path] = path
	}
}

func (f *FilesystemAPI) checkRateLimit() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	now := time.Now()
	if now.Sub(f.lastReset) >= time.Minute {
		f.operationCount = 0
		f.lastReset = now
	}

	if f.operationCount >= FSOperationsPerMinute {
		return fmt.Errorf("rate limit exceeded: %d operations per minute", FSOperationsPerMinute)
	}

	f.operationCount++
	return nil
}

func (f *FilesystemAPI) checkPermission(permType, path string) (string, error) {
	// Normalize path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}

	// Check if plugin declared this permission type
	if permType == "fs_read" && !f.wantsRead {
		return "", fmt.Errorf("plugin did not declare filesystem.read permission")
	}
	if permType == "fs_write" && !f.wantsWrite {
		return "", fmt.Errorf("plugin did not declare filesystem.write permission")
	}

	// In confined (headless) mode, resolve symlinks and require the real target
	// to be under the confinement root before honoring ANY grant (including a
	// cached/ancestor hit). opPath is the canonicalized path the os.* call then
	// operates on. NOTE: this narrows but does not fully eliminate a TOCTOU race
	// — the kernel still re-resolves opPath component-by-component at syscall
	// time, so a directory component swapped to an out-of-root symlink between
	// this check and the os.* call could still escape. That residual race is low
	// risk because the Lua fs API exposes no symlink/rename/link primitive, so a
	// plugin cannot create the racing swap through this API itself.
	opPath := absPath
	if f.confinementRoot != "" {
		canonical, cerr := canonicalizePluginPath(absPath)
		if cerr != nil || !withinRoot(f.confinementRoot, canonical) {
			return "", fmt.Errorf("permission denied: %s is outside the confinement root", absPath)
		}
		opPath = canonical
	}

	// Check if path is already approved
	key := permType + ":" + absPath
	if _, ok := f.approvedPaths[key]; ok {
		return opPath, nil
	}

	// Check if any parent directory is approved
	dir := absPath
	for {
		parentKey := permType + ":" + dir
		if _, ok := f.approvedPaths[parentKey]; ok {
			// Path is under an approved directory
			return opPath, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// Need to request permission
	if f.permCallback == nil {
		return "", fmt.Errorf("filesystem access not available")
	}

	approved := f.permCallback(f.pluginName, permType, absPath)
	if approved == "" {
		return "", fmt.Errorf("permission denied for %s", absPath)
	}

	// Save to DB and cache
	_, err = f.db.Exec(
		"INSERT INTO plugin_permissions (plugin_id, permission_type, path) VALUES (?, ?, ?)",
		f.pluginID, permType, approved,
	)
	if err == nil {
		f.approvedPaths[permType+":"+approved] = approved
	}

	// Check if the requested path is under the approved path
	if !isSubPath(approved, absPath) {
		return "", fmt.Errorf("approved path %s does not cover requested path %s", approved, absPath)
	}

	return opPath, nil
}

// withinRoot reports whether the canonical target is the root itself or a
// descendant of it.
func withinRoot(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// canonicalizePluginPath resolves the path to an absolute, symlink-free form. It
// resolves the nearest existing ancestor (so a not-yet-created file under an
// approved dir still canonicalizes) and re-appends the missing suffix.
func canonicalizePluginPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	cur := filepath.Clean(abs)
	var suffix []string
	for {
		if _, err := os.Lstat(cur); err == nil {
			resolved, rerr := filepath.EvalSymlinks(cur)
			if rerr != nil {
				return "", rerr
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return filepath.Clean(resolved), nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", fmt.Errorf("no existing ancestor for %q", p)
		}
		suffix = append(suffix, filepath.Base(cur))
		cur = parent
	}
}

func isSubPath(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return !filepath.IsAbs(rel) && rel != ".." && !startsWithParent(rel)
}

func startsWithParent(path string) bool {
	return len(path) >= 2 && path[:2] == ".."
}

// Register adds the fs module to the Lua state
func (f *FilesystemAPI) Register(L *lua.LState) {
	fsMod := L.NewTable()

	fsMod.RawSetString("read", L.NewFunction(f.read))
	fsMod.RawSetString("write", L.NewFunction(f.write))
	fsMod.RawSetString("list", L.NewFunction(f.list))
	fsMod.RawSetString("exists", L.NewFunction(f.exists))

	L.SetGlobal("fs", fsMod)
}

func (f *FilesystemAPI) read(L *lua.LState) int {
	path := L.CheckString(1)

	if err := f.checkRateLimit(); err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	approvedPath, err := f.checkPermission("fs_read", path)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	// Check file size before reading to prevent memory exhaustion
	info, err := os.Stat(approvedPath)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	if info.Size() > MaxReadFileSize {
		L.Push(lua.LNil)
		L.Push(lua.LString(fmt.Sprintf("file too large: %d bytes (max %d bytes)", info.Size(), MaxReadFileSize)))
		return 2
	}

	data, err := os.ReadFile(approvedPath)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LString(string(data)))
	return 1
}

func (f *FilesystemAPI) write(L *lua.LState) int {
	path := L.CheckString(1)
	content := L.CheckString(2)

	if err := f.checkRateLimit(); err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	approvedPath, err := f.checkPermission("fs_write", path)
	if err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	// Ensure parent directory exists
	dir := filepath.Dir(approvedPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	if err := os.WriteFile(approvedPath, []byte(content), 0644); err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LTrue)
	return 1
}

func (f *FilesystemAPI) list(L *lua.LState) int {
	path := L.CheckString(1)

	if err := f.checkRateLimit(); err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	approvedPath, err := f.checkPermission("fs_read", path)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	entries, err := os.ReadDir(approvedPath)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	result := L.NewTable()
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		item := L.NewTable()
		item.RawSetString("name", lua.LString(entry.Name()))
		item.RawSetString("is_dir", lua.LBool(entry.IsDir()))
		item.RawSetString("size", lua.LNumber(info.Size()))
		item.RawSetString("modified", lua.LNumber(info.ModTime().Unix()))

		result.Append(item)
	}

	L.Push(result)
	return 1
}

func (f *FilesystemAPI) exists(L *lua.LState) int {
	path := L.CheckString(1)

	// Normalize path
	absPath, err := filepath.Abs(path)
	if err != nil {
		L.Push(lua.LFalse)
		return 1
	}

	// Check if plugin declared read permission
	if !f.wantsRead {
		L.Push(lua.LFalse)
		return 1
	}

	// In confined mode, never reveal existence of paths outside the root.
	if f.confinementRoot != "" {
		if canonical, cerr := canonicalizePluginPath(absPath); cerr != nil || !withinRoot(f.confinementRoot, canonical) {
			L.Push(lua.LFalse)
			return 1
		}
	}

	// Check if path is under an already-approved directory
	const fsReadPrefix = "fs_read:"
	approved := false
	for key := range f.approvedPaths {
		if strings.HasPrefix(key, fsReadPrefix) {
			approvedPath := strings.TrimPrefix(key, fsReadPrefix)
			if isSubPath(approvedPath, absPath) {
				approved = true
				break
			}
		}
	}

	// If not under approved path, return false (don't leak existence info)
	if !approved {
		L.Push(lua.LFalse)
		return 1
	}

	_, err = os.Stat(absPath)
	L.Push(lua.LBool(err == nil))
	return 1
}
