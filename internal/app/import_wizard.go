package app

// Folder-import wizard: scan a user-picked folder, inspect its files one at a
// time, and apply a reviewed plan of imports and deletions in a single call.
//
// These methods are deliberately NOT exposed over the REST API in
// api_manager.go, and the next person's instinct will be to add routes — don't.
// The paths they take are meaningful only on the machine running the process:
// a remote browser has no idea what "photos/2024/rome.jpg" means on the
// server's disk. Worse, a REST route would hand every API-key holder a remote
// directory listing, an arbitrary-file read, and a move-to-trash primitive,
// which is a categorically different capability from "read and write clips".
// The containment in resolveSessionPath bounds the damage to one folder the
// user picked in a native dialog; it does not make the capability appropriate
// to expose over a network. Server mode gets throwing stubs in rest-glue.js.
//
// Known residual races, both deliberate:
//
//  1. O_NOFOLLOW protects the final path component. A local process that
//     replaces an intermediate *directory* with a symlink between the scan and
//     the read could still redirect access outside the chosen folder. Closing
//     this means resolving the path with openat per component (and it has no
//     Windows equivalent). Judged not worth it: the attacker is a same-user
//     local process, which already has the user's filesystem authority, and the
//     app holds no privilege it does not.
//  2. trashVerified compares the file's identity and state, then unlinks by
//     path — no portable API deletes "this inode". The gap is microseconds on a
//     file the user selected themselves.
//
// What IS defended, because the windows are wide enough to hit by accident
// rather than by attack: a file changing between review and Apply (see
// importSession.reviewed), growing past the size cap mid-read, or being
// replaced between the read and the delete.

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go-clipboard/internal/cliptype"
	"go-clipboard/internal/imagemeta"
)

const (
	// maxImportScanEntries caps one scan. Large enough for a real photo dump,
	// small enough that the summary pane and the relPath set stay cheap.
	maxImportScanEntries = 10000

	// maxImportPreviewBytes bounds the base64 preview pushed across the Wails
	// bridge. It does not bound the read: hashing needs every byte regardless.
	maxImportPreviewBytes = 8 << 20

	// maxImportFileBytes refuses absurd imports. UploadFileAndGetID holds the
	// decoded bytes plus a ~1.33x base64 copy in memory, and SQLite has its own
	// blob ceiling, so failing loudly here beats an OOM mid-apply.
	maxImportFileBytes = 512 << 20

	// maxImportEXIFBytes bounds the EXIF read. EXIF lives in the file header,
	// so a 1 MB prefix is generous and keeps a 200 MB TIFF cheap.
	maxImportEXIFBytes = 1 << 20

	// maxImportTextPreviewBytes is how much of a text file is worth showing.
	maxImportTextPreviewBytes = 4 << 10

	// maxImportDuplicates caps the duplicate list per file. Seeing ten copies
	// already answers "do I have this?"; the eleventh adds nothing.
	maxImportDuplicates = 10
)

// Import decision actions. Import and delete compose, so the wizard's four
// buttons are these four values.
const (
	ImportActionSkip         = "skip"
	ImportActionImport       = "import"
	ImportActionDelete       = "delete"
	ImportActionImportDelete = "import_delete"
)

// Per-file outcomes reported by ImportApply.
const (
	ImportStatusOK           = "ok"
	ImportStatusSkipped      = "skipped"
	ImportStatusMissing      = "missing"
	ImportStatusImportFailed = "import_failed"
	ImportStatusTagFailed    = "tag_failed"
	ImportStatusTrashFailed  = "trash_failed"
	ImportStatusInvalid      = "invalid"
	// ImportStatusChanged means the file on disk is no longer the one the user
	// looked at when they made the decision.
	ImportStatusChanged = "changed"
)

// Reasons a preview was withheld.
const (
	ImportPreviewTooLarge       = "too_large"
	ImportPreviewNotPreviewable = "not_previewable"
)

// ImportScanEntry is one importable file found by the scan.
type ImportScanEntry struct {
	// RelPath is slash-separated and relative to the scan root. It is the only
	// handle the frontend ever gets, and the only thing later calls accept.
	RelPath string `json:"rel_path"`
	Name    string `json:"name"`
	// Dir is the slash-separated parent directory relative to the root, ""
	// for a file sitting at the root. It doubles as the suggested tag.
	Dir         string    `json:"dir"`
	Size        int64     `json:"size"`
	ModTime     time.Time `json:"mod_time"`
	ContentType string    `json:"content_type"`
}

// ImportScanSkipped explains what the walk left out, so the setup pane can say
// "6 skipped (3 hidden, 2 symlinks, 1 unreadable)" instead of silently
// presenting a different folder than the one the user sees in Finder.
type ImportScanSkipped struct {
	Dotted     int `json:"dotted"`
	Symlinks   int `json:"symlinks"`
	NonRegular int `json:"non_regular"`
	AppTemp    int `json:"app_temp"`
	Unreadable int `json:"unreadable"`
}

// ImportScanResult is the answer to StartImportSession.
type ImportScanResult struct {
	Root      string            `json:"root"`
	Recursive bool              `json:"recursive"`
	Entries   []ImportScanEntry `json:"entries"`
	Truncated bool              `json:"truncated"`
	Skipped   ImportScanSkipped `json:"skipped"`
	// TrashRecoverable is false on platforms where deletion is permanent, so
	// the UI can warn before the user assigns a single delete.
	TrashRecoverable bool `json:"trash_recoverable"`
}

// ImportDuplicate is an existing clip with the same content hash.
type ImportDuplicate struct {
	ClipID   int64  `json:"clip_id"`
	Filename string `json:"filename"`
	// time.Time to match ClipPreview.CreatedAt: the sqlite driver converts the
	// column for us and the frontend gets an RFC3339 string it can parse,
	// rather than SQLite's "YYYY-MM-DD HH:MM:SS" which new Date() mishandles.
	CreatedAt  time.Time `json:"created_at"`
	IsArchived bool      `json:"is_archived"`
	Tags       []Tag     `json:"tags"`
}

// ImportInspection is everything the review pane shows for one file.
type ImportInspection struct {
	RelPath     string    `json:"rel_path"`
	Name        string    `json:"name"`
	Dir         string    `json:"dir"`
	Size        int64     `json:"size"`
	ModTime     time.Time `json:"mod_time"`
	ContentType string    `json:"content_type"`
	ContentHash string    `json:"content_hash"`
	// PreviewData is base64, empty when withheld; PreviewOmitted then says why.
	PreviewData    string            `json:"preview_data"`
	PreviewOmitted string            `json:"preview_omitted"`
	EXIF           *imagemeta.EXIF   `json:"exif"`
	Duplicates     []ImportDuplicate `json:"duplicates"`
	SuggestedTag   string            `json:"suggested_tag"`
	// Error describes a problem with this file (unreadable, vanished). The
	// other fields are best-effort when it is set. Session and containment
	// violations are returned as a Go error instead.
	Error string `json:"error"`
}

// ImportDecision is one row of the reviewed plan.
type ImportDecision struct {
	RelPath string `json:"rel_path"`
	Action  string `json:"action"`
	TagName string `json:"tag_name"`
}

// ImportApplyResult is what actually happened to one file.
type ImportApplyResult struct {
	RelPath  string `json:"rel_path"`
	Action   string `json:"action"`
	ClipID   int64  `json:"clip_id"`
	TagID    int64  `json:"tag_id"`
	Imported bool   `json:"imported"`
	Trashed  bool   `json:"trashed"`
	Status   string `json:"status"`
	Error    string `json:"error"`
}

// ImportApplySummary is the whole run's outcome.
type ImportApplySummary struct {
	Results  []ImportApplyResult `json:"results"`
	Imported int                 `json:"imported"`
	Trashed  int                 `json:"trashed"`
	Skipped  int                 `json:"skipped"`
	Failed   int                 `json:"failed"`
}

// importSession scopes every path-taking call to one folder for the lifetime
// of one wizard run.
type importSession struct {
	root         string
	resolvedRoot string
	recursive    bool
	// allowed holds exactly the relPaths the scan emitted. Membership in this
	// set — not string hygiene — is what makes the other methods safe.
	allowed map[string]struct{}
	dirs    map[string]string // relPath -> suggested tag, cached from the walk

	// reviewed records what each file looked like when the user inspected it.
	// See reviewMark.
	// The gap between reviewing a file and pressing Apply is user think time —
	// seconds to minutes — and a sync client or an editor can replace a file in
	// that window. Acting on rel_path alone would then import and delete
	// something nobody ever saw. Guarded by App.importMu.
	reviewed map[string]reviewMark
}

// reviewMark is what the user was actually shown for one file.
//
// The hash is the load-bearing field. Stat data alone (identity, size, mtime)
// misses an in-place same-length rewrite on a filesystem with coarse timestamp
// granularity — exFAT and FAT round mtime to two seconds — and that is exactly
// the case where an import+delete would store torn bytes and then destroy the
// good original. Comparing content costs nothing on the import path, where the
// bytes have to be read anyway.
type reviewMark struct {
	info os.FileInfo
	hash string
}

// trashFile removes one file, moving it to the platform Trash where that
// exists. MAHPASTES_TRASH_MODE=remove forces a permanent delete; the e2e suite
// sets it so test runs do not fill the developer's Trash with fixtures.
func trashFile(absPath string) error {
	if os.Getenv("MAHPASTES_TRASH_MODE") == "remove" {
		return os.Remove(absPath)
	}
	return moveToTrash(absPath)
}

// errNoImportSession is returned when a path call arrives with no live scan.
var errNoImportSession = errors.New("no active import session")

// ApproveImportRoot marks a folder as chosen by the user in the native picker.
//
// Deliberately NOT part of the desktopCore interface. That interface is
// embedded in the Wails-bound App, so every method on it is callable from
// JavaScript; this one is reached only through the unexported `core` field, by
// BeginImportSession, immediately after the OS dialog returns. That asymmetry
// is the whole point — it is what keeps the picker from being bypassable.
func (a *App) ApproveImportRoot(root string) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return
	}
	a.importMu.Lock()
	a.importApprovedRoot = filepath.Clean(abs)
	a.importMu.Unlock()
}

// importRootIsApproved reports whether this root may be scanned.
//
// StartImportSession stays bound because the wizard re-scans the same folder
// when the user toggles "include subfolders", and because Playwright cannot
// dismiss a native dialog and has to reach the scan some other way. Without
// this check that binding would be a scan-anything primitive — point it at "/"
// and enumerate the disk. So a bound call is accepted only for a root the user
// actually picked, or when the e2e launcher has explicitly opted out.
func (a *App) importRootIsApproved(abs string) bool {
	_, ok := a.approveAndCaptureGeneration(abs)
	return ok
}

// approveAndCaptureGeneration checks the approval and reads the generation
// under a single lock acquisition.
//
// Doing these as two separate acquisitions left a hole: EndImportSession could
// land between them, so the scan would validate against the old approval and
// then capture the *new* generation — and the staleness check before install
// would compare equal and let the revoked session through.
func (a *App) approveAndCaptureGeneration(abs string) (uint64, bool) {
	allowUnpicked := os.Getenv("MAHPASTES_ALLOW_UNPICKED_IMPORT") == "1"

	a.importMu.Lock()
	defer a.importMu.Unlock()

	if !allowUnpicked && (a.importApprovedRoot == "" || a.importApprovedRoot != abs) {
		return 0, false
	}
	return a.importGeneration, true
}

// resolveSessionPath turns a session-relative path into an absolute one, or
// refuses. Every path-taking method in this file goes through it.
func (a *App) resolveSessionPath(rel string) (string, *importSession, error) {
	a.importMu.Lock()
	session := a.importSession
	a.importMu.Unlock()
	return resolvePathInSession(session, rel)
}

// resolvePathInSession is resolveSessionPath against an explicit session.
//
// ImportApply snapshots the session once and resolves every decision against
// that snapshot. Re-reading a.importSession per decision would let a concurrent
// StartImportSession swap the root mid-run, after which any later decision
// whose relative path also exists in the new folder would silently act on a
// different file than the user reviewed.
func resolvePathInSession(session *importSession, rel string) (string, *importSession, error) {
	if session == nil {
		return "", nil, errNoImportSession
	}

	// Membership first, and it is the real guarantee: a fully compromised
	// frontend still cannot name a file the scan did not already surface.
	// The syntactic checks below are belt-and-braces — they also keep the
	// map honest if a future refactor ever populates it from elsewhere.
	if _, ok := session.allowed[rel]; !ok {
		return "", nil, fmt.Errorf("path is not part of the current import scan: %q", rel)
	}
	if rel == "" || filepath.IsAbs(rel) || strings.ContainsRune(rel, 0) {
		return "", nil, fmt.Errorf("invalid import path: %q", rel)
	}
	cleaned := path.Clean(rel)
	if cleaned != rel {
		return "", nil, fmt.Errorf("invalid import path: %q", rel)
	}
	for _, seg := range strings.Split(cleaned, "/") {
		if seg == ".." {
			return "", nil, fmt.Errorf("invalid import path: %q", rel)
		}
	}

	abs := filepath.Join(session.root, filepath.FromSlash(cleaned))
	if !isInsideDir(session.root, abs) {
		return "", nil, fmt.Errorf("import path escapes the scan root: %q", rel)
	}

	// Re-check the file itself, not just the string. Between the scan and now
	// the entry could have become a symlink pointing anywhere, or a FIFO that
	// would block a read forever — see the reasoning already recorded on
	// PathProbe.IsRegular in paste_paths.go.
	//
	// These checks are advisory, NOT a TOCTOU guarantee: they inspect a name,
	// and the file can still be swapped between here and the read. What
	// actually closes that window is openImportFile's O_NOFOLLOW plus the fstat
	// on the resulting handle — every read below goes through the fd, never by
	// re-opening this path. Keep it that way.
	info, err := os.Lstat(abs)
	if err != nil {
		return "", session, err
	}
	if !info.Mode().IsRegular() {
		return "", session, fmt.Errorf("not a regular file: %q", rel)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		if !isInsideDir(session.resolvedRoot, resolved) {
			return "", session, fmt.Errorf("import path escapes the scan root: %q", rel)
		}
	}

	// Return the unresolved path: the user picked this folder, so the trash
	// should take the file they can see there.
	return abs, session, nil
}

// StartImportSession scans root and begins a wizard session.
//
// This is bound separately from the folder picker on purpose. Playwright cannot
// dismiss a native NSOpenPanel, so the e2e suite drives the wizard by calling
// this with a temp directory — the same bypass AppHelper.addWatchFolder uses.
func (a *App) StartImportSession(root string, recursive bool) (*ImportScanResult, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("no folder selected")
	}

	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve folder: %w", err)
	}
	abs = filepath.Clean(abs)

	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("failed to open folder: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a folder: %s", abs)
	}
	startGeneration, approved := a.approveAndCaptureGeneration(abs)
	if !approved {
		return nil, fmt.Errorf("folder was not chosen through the import picker")
	}

	// startGeneration was captured together with the approval above. Scanning a
	// large tree takes real time, and if the user closes the wizard meanwhile
	// EndImportSession bumps it — publishing the finished scan anyway would
	// resurrect an authorized session after it was revoked, or clobber a
	// session the user has since opened on a different folder.
	resolvedRoot := abs
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		resolvedRoot = r
	}

	// The app's own temp directory holds leased files it materialized for
	// "Copy Path" and drag-out. Importing those would duplicate clips that
	// already exist under throwaway names — the same concern PathProbe.IsTemp
	// encodes. Compare against both forms because /var is a symlink to
	// /private/var on macOS.
	tempDir := a.TempDir()
	resolvedTempDir := tempDir
	if r, err := filepath.EvalSymlinks(tempDir); err == nil {
		resolvedTempDir = r
	}

	result := &ImportScanResult{
		Root:             abs,
		Recursive:        recursive,
		Entries:          []ImportScanEntry{},
		TrashRecoverable: trashIsRecoverable() && os.Getenv("MAHPASTES_TRASH_MODE") != "remove",
	}

	truncated := false
	walkErr := filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// A subdirectory we cannot read is a fact about the folder, not a
			// reason to abandon the scan.
			result.Skipped.Unreadable++
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		if len(result.Entries) >= maxImportScanEntries {
			truncated = true
			return fs.SkipAll
		}

		name := d.Name()

		if d.IsDir() {
			if p == abs {
				return nil
			}
			// One rule covers .git, .DS_Store's neighbours, .Trash and the
			// AppleDouble sidecars, matching ProcessExistingFiles.
			if strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			if !recursive {
				return fs.SkipDir
			}
			if tempDir != "" && (isInsideDir(tempDir, p) || isInsideDir(resolvedTempDir, p)) {
				result.Skipped.AppTemp++
				return fs.SkipDir
			}
			return nil
		}

		if strings.HasPrefix(name, ".") {
			result.Skipped.Dotted++
			return nil
		}
		// WalkDir hands back the lstat mode, so symlinks are visible here. It
		// also never follows directory symlinks, which is why a symlink loop
		// cannot be constructed and no visited-inode set is needed.
		if d.Type()&fs.ModeSymlink != 0 {
			result.Skipped.Symlinks++
			return nil
		}
		if !d.Type().IsRegular() {
			result.Skipped.NonRegular++
			return nil
		}
		if tempDir != "" && (isInsideDir(tempDir, p) || isInsideDir(resolvedTempDir, p)) {
			result.Skipped.AppTemp++
			return nil
		}

		fi, err := d.Info()
		if err != nil {
			result.Skipped.Unreadable++
			return nil
		}

		relPath, err := filepath.Rel(abs, p)
		if err != nil {
			result.Skipped.Unreadable++
			return nil
		}
		relSlash := filepath.ToSlash(relPath)
		dir := path.Dir(relSlash)
		if dir == "." {
			dir = ""
		}

		result.Entries = append(result.Entries, ImportScanEntry{
			RelPath:     relSlash,
			Name:        name,
			Dir:         dir,
			Size:        fi.Size(),
			ModTime:     fi.ModTime(),
			ContentType: guessImportContentType(name),
		})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("failed to scan folder: %w", walkErr)
	}
	result.Truncated = truncated

	// Deterministic, subfolder-grouped order: the user walks one directory at a
	// time, which is what makes the tag pre-fill feel coherent.
	sort.Slice(result.Entries, func(i, j int) bool {
		return result.Entries[i].RelPath < result.Entries[j].RelPath
	})

	allowed := make(map[string]struct{}, len(result.Entries))
	dirs := make(map[string]string, len(result.Entries))
	for _, e := range result.Entries {
		allowed[e.RelPath] = struct{}{}
		dirs[e.RelPath] = e.Dir
	}

	a.importMu.Lock()
	if a.importGeneration != startGeneration {
		a.importMu.Unlock()
		return nil, fmt.Errorf("import session was closed while the folder was being scanned")
	}
	a.importSession = &importSession{
		root:         abs,
		resolvedRoot: resolvedRoot,
		recursive:    recursive,
		allowed:      allowed,
		dirs:         dirs,
		reviewed:     map[string]reviewMark{},
	}
	a.importMu.Unlock()

	return result, nil
}

// EndImportSession drops the session. Called when the wizard closes, so the
// path-taking methods fail closed for the rest of the process's life.
func (a *App) EndImportSession() error {
	a.importMu.Lock()
	a.importSession = nil
	a.importGeneration++
	// Revoke the picker approval too, or the last folder stays a standing
	// read-and-delete capability: bound JS could re-scan it long after the
	// wizard closed and submit delete decisions the user never saw. Approval
	// lasts exactly as long as the wizard it was granted for.
	a.importApprovedRoot = ""
	a.importMu.Unlock()
	return nil
}

// guessImportContentType mirrors what the import will actually store: the same
// extension-based guess ReadFileFromPath makes, promoted the same way
// UploadFileAndGetID promotes it, so the preview's declared type cannot
// disagree with the resulting clip.
func guessImportContentType(name string) string {
	ct := mime.TypeByExtension(filepath.Ext(name))
	if ct == "" {
		ct = "application/octet-stream"
	}
	return cliptype.PromoteMarkdown(name, ct)
}

// ImportInspect gathers everything the review pane shows for one file.
//
// Problems with the file itself land in Inspection.Error and still return an
// inspection, so the UI can explain and offer Skip. Only session and
// containment violations return a Go error.
func (a *App) ImportInspect(relPath string) (*ImportInspection, error) {
	abs, session, err := a.resolveSessionPath(relPath)
	if err != nil {
		if session == nil {
			return nil, err
		}
		// The path is legitimate but the file is gone or no longer readable.
		return &ImportInspection{
			RelPath:      relPath,
			Name:         path.Base(relPath),
			Dir:          session.dirs[relPath],
			SuggestedTag: session.dirs[relPath],
			Error:        err.Error(),
		}, nil
	}

	out := &ImportInspection{
		RelPath:      relPath,
		Name:         path.Base(relPath),
		Dir:          session.dirs[relPath],
		SuggestedTag: session.dirs[relPath],
		ContentType:  guessImportContentType(path.Base(relPath)),
		Duplicates:   []ImportDuplicate{},
	}

	// Open first, then fstat the handle. Doing it in this order — rather than
	// stat-then-open — is what makes the checks below apply to the bytes we
	// actually read: the fd pins one inode, so nothing swapped in afterwards
	// can change what is hashed, previewed or imported.
	f, err := openImportFile(abs)
	if err != nil {
		out.Error = fmt.Sprintf("cannot read file: %v", err)
		return out, nil
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		out.Error = fmt.Sprintf("file is no longer available: %v", err)
		return out, nil
	}
	if !info.Mode().IsRegular() {
		out.Error = "not a regular file"
		return out, nil
	}
	out.Size = info.Size()
	out.ModTime = info.ModTime()

	// Remember what the user is about to be shown, so Apply can tell whether it
	// is still the same file.
	recordReview := func(hash string) {
		a.importMu.Lock()
		if a.importSession == session && session.reviewed != nil {
			session.reviewed[relPath] = reviewMark{info: info, hash: hash}
		}
		a.importMu.Unlock()
	}

	// Stream the hash: this is the one operation that must touch every byte,
	// and buffering a 2 GB file to do it would defeat the point.
	hash, err := computeContentHashReader(f)
	if err != nil {
		out.Error = fmt.Sprintf("cannot read file: %v", err)
		return out, nil
	}
	out.ContentHash = hash
	recordReview(hash)

	if dups, err := a.findImportDuplicates(hash); err == nil {
		out.Duplicates = dups
	}

	isImage := strings.HasPrefix(out.ContentType, "image/")
	isText := strings.HasPrefix(out.ContentType, "text/") ||
		out.ContentType == "application/json"

	if isImage {
		if _, err := f.Seek(0, io.SeekStart); err == nil {
			if meta, err := imagemeta.ExtractEXIF(io.LimitReader(f, maxImportEXIFBytes)); err == nil && !meta.IsEmpty() {
				out.EXIF = meta
			}
		}
	}

	switch {
	case !isImage && !isText:
		out.PreviewOmitted = ImportPreviewNotPreviewable
	case isImage && out.Size > maxImportPreviewBytes:
		// Honest placeholder beats a broken <img>: the UI shows the size and
		// says the preview was skipped.
		out.PreviewOmitted = ImportPreviewTooLarge
	default:
		limit := int64(maxImportPreviewBytes)
		if isText && limit > maxImportTextPreviewBytes {
			limit = maxImportTextPreviewBytes
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			out.PreviewOmitted = ImportPreviewNotPreviewable
			break
		}
		data, err := io.ReadAll(io.LimitReader(f, limit))
		if err != nil {
			out.PreviewOmitted = ImportPreviewNotPreviewable
			break
		}
		out.PreviewData = base64.StdEncoding.EncodeToString(data)
	}

	return out, nil
}

// findImportDuplicates lists existing clips with the same content.
//
// This is folded into the inspection deliberately: no bound method ever maps an
// arbitrary hash to clip IDs, which would otherwise be a library-membership
// oracle for anything that can reach the bindings.
func (a *App) findImportDuplicates(hash string) ([]ImportDuplicate, error) {
	out := []ImportDuplicate{}
	if hash == "" {
		return out, nil
	}

	rows, err := a.db.Query(
		`SELECT id, filename, created_at, is_archived FROM clips
		 WHERE content_hash = ? ORDER BY id LIMIT ?`, hash, maxImportDuplicates)
	if err != nil {
		return out, err
	}
	defer rows.Close()

	for rows.Next() {
		var d ImportDuplicate
		var filename sql.NullString
		var archived sql.NullBool
		if err := rows.Scan(&d.ClipID, &filename, &d.CreatedAt, &archived); err != nil {
			return out, err
		}
		d.Filename = filename.String
		d.IsArchived = archived.Bool
		if tags, err := a.GetClipTags(d.ClipID); err == nil {
			d.Tags = tags
		} else {
			d.Tags = []Tag{}
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ImportApply executes a reviewed plan.
//
// The whole plan arrives in one call on purpose: it is what makes "nothing
// happens until Apply" a structural property rather than a UI convention.
// There is no bound method that imports or deletes a single file, so no
// sequence of frontend calls can half-execute a plan, and the
// import-before-trash ordering below cannot be broken by a JS exception, a
// reload, or the user closing the modal mid-run.
func (a *App) ImportApply(decisions []ImportDecision) (*ImportApplySummary, error) {
	// Snapshot the session once. Every decision below resolves against this
	// exact scan, so a concurrent StartImportSession cannot repoint the run at
	// a different folder halfway through.
	a.importMu.Lock()
	session := a.importSession
	a.importMu.Unlock()
	if session == nil {
		return nil, errNoImportSession
	}

	summary := &ImportApplySummary{Results: make([]ImportApplyResult, 0, len(decisions))}
	// One resolution per distinct tag name, not per file: a 500-file import of
	// a single subfolder should not hit the tags table 500 times.
	tagCache := map[string]int64{}
	total := len(decisions)

	for i, d := range decisions {
		res := ImportApplyResult{RelPath: d.RelPath, Action: d.Action}

		switch d.Action {
		case ImportActionSkip, "":
			res.Status = ImportStatusSkipped
			summary.Skipped++

		case ImportActionDelete:
			abs, _, err := resolvePathInSession(session, d.RelPath)
			if err != nil {
				res.Status, res.Error = importResolveStatus(err), err.Error()
				break
			}
			// Open and fstat before deleting: a delete-only decision never
			// reads the file, so without this the only thing standing between
			// the user and a swapped-in FIFO or replacement file is a name.
			seen, err := statImportFile(abs)
			if err != nil {
				res.Status, res.Error = importResolveStatus(err), err.Error()
			} else if mark, ok := a.reviewedAs(session, d.RelPath); ok && !sameFileState(mark.info, seen) {
				res.Status = ImportStatusChanged
				res.Error = "file changed after it was reviewed; not deleting"
			} else if err := trashVerified(abs, seen, deleteHash(a, session, d.RelPath)); err != nil {
				res.Status, res.Error = ImportStatusTrashFailed, err.Error()
			} else {
				res.Status, res.Trashed = ImportStatusOK, true
				summary.Trashed++
			}

		case ImportActionImport, ImportActionImportDelete:
			a.applyImportDecision(session, d, &res, tagCache)
			if res.Imported {
				summary.Imported++
			}
			if res.Trashed {
				summary.Trashed++
			}

		default:
			res.Status = ImportStatusInvalid
			res.Error = fmt.Sprintf("unknown action %q", d.Action)
		}

		if res.Status != ImportStatusOK && res.Status != ImportStatusSkipped {
			summary.Failed++
		}
		summary.Results = append(summary.Results, res)

		a.emitEvent("import:progress", map[string]interface{}{
			"index":    i + 1,
			"total":    total,
			"rel_path": d.RelPath,
			"status":   res.Status,
		})
	}

	return summary, nil
}

// applyImportDecision handles the import (and optional trash) of one file.
func (a *App) applyImportDecision(session *importSession, d ImportDecision, res *ImportApplyResult, tagCache map[string]int64) {
	abs, _, err := resolvePathInSession(session, d.RelPath)
	if err != nil {
		res.Status, res.Error = importResolveStatus(err), err.Error()
		return
	}

	fileData, seen, err := readImportFile(abs)
	if err != nil {
		res.Status, res.Error = ImportStatusImportFailed, err.Error()
		return
	}

	// Refuse a file that is no longer the one the user reviewed. Importing it
	// anyway would put unreviewed content in the library under a decision that
	// was made about something else — and with import+delete, destroy the
	// replacement too.
	importedHash := ""
	if decoded, decErr := base64.StdEncoding.DecodeString(fileData.Data); decErr == nil {
		importedHash = computeContentHash(decoded)
	}

	if mark, ok := a.reviewedAs(session, d.RelPath); ok {
		// Hash first: it is decisive where stat data is not, and the bytes are
		// already in hand. Fall back to stat only if the review predates a
		// hash for some reason.
		changed := false
		if mark.hash != "" {
			changed = importedHash == "" || importedHash != mark.hash
		} else {
			changed = !sameFileState(mark.info, seen)
		}
		if changed {
			res.Status = ImportStatusChanged
			res.Error = "file changed after it was reviewed; not imported"
			return
		}
	}

	clipID, err := a.UploadFileAndGetID(*fileData)
	// A zero clip ID means the row never committed. Trashing on that would
	// destroy the only copy — the same hard stop ProcessExistingFiles enforces
	// before it removes a watched file.
	if err != nil || clipID == 0 {
		res.Status = ImportStatusImportFailed
		if err != nil {
			res.Error = err.Error()
		} else {
			res.Error = "import did not produce a clip"
		}
		return
	}
	res.ClipID = clipID
	res.Imported = true
	res.Status = ImportStatusOK

	if tagName := strings.TrimSpace(d.TagName); tagName != "" {
		tagID, err := a.resolveImportTag(tagName, tagCache)
		if err != nil {
			res.Status, res.Error = ImportStatusTagFailed, err.Error()
		} else if err := a.AddTagToClip(clipID, tagID); err != nil {
			// Tree exclusivity can legitimately reject a tag. The clip is
			// imported either way, so the trash below is still safe.
			res.Status, res.Error = ImportStatusTagFailed, err.Error()
		} else {
			res.TagID = tagID
		}
	}

	// Emitted after tagging, matching UploadFiles' ordering: a plugin that
	// reacts to clip:created by adding tags would otherwise race the
	// tree-exclusivity enforcement in AddTagToClip above.
	//
	// UploadFileAndGetID deliberately does not emit this — only UploadFiles and
	// the REST/serve upload paths do, and the watcher substitutes
	// watch:import_complete. Without it, plugins that subscribe to clip:created
	// (auto-tagger among them) would never fire for wizard imports.
	a.emitPluginEvent("clip:created", map[string]interface{}{
		"id":           clipID,
		"content_type": fileData.ContentType,
		"filename":     fileData.Name,
	})

	// Delete only when the decision was carried out in full. A tag failure
	// (tree exclusivity rejecting the tag, a reserved name) means the user did
	// not get what they asked for, so leaving the file lets them retry — and on
	// platforms where "delete" is permanent, a partial success is not worth
	// spending the only copy on. The clip is already imported either way, so
	// re-running the wizard shows the file as a duplicate rather than silently
	// importing it twice.
	if d.Action == ImportActionImportDelete && res.Status == ImportStatusOK {
		// `seen` and importedHash describe the bytes the clip was actually built
		// from, so this deletes the file that was imported or nothing at all.
		if err := trashVerified(abs, seen, importedHash); err != nil {
			res.Status, res.Error = ImportStatusTrashFailed, err.Error()
			return
		}
		res.Trashed = true
	}
}

// readImportFile reads one scanned file into a FileData.
//
// Deliberately not a.ReadFileFromPath: that takes a path and re-opens it, which
// would reintroduce the swap window resolveSessionPath cannot close on its own.
// Here the size cap and the regular-file check are both applied to the open
// handle, so a file that grows or changes type after the check cannot slip
// past either.
func readImportFile(abs string) (*FileData, os.FileInfo, error) {
	f, err := openImportFile(abs)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read file: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("not a regular file")
	}
	if info.Size() > maxImportFileBytes {
		return nil, nil, fmt.Errorf("file is larger than the %d MB import limit", maxImportFileBytes>>20)
	}

	// Read one byte past the cap so growth during the read is detected rather
	// than silently truncated. A plain LimitReader at exactly the cap would
	// return a short clip that looks successful — and if the decision was
	// import+delete, the complete original would then be destroyed.
	data, err := io.ReadAll(io.LimitReader(f, maxImportFileBytes+1))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read file: %w", err)
	}
	if int64(len(data)) > maxImportFileBytes {
		return nil, nil, fmt.Errorf("file grew past the %d MB import limit while being read", maxImportFileBytes>>20)
	}

	// Re-stat the same handle. The size and mtime above describe the file as it
	// was when the read started; a writer that appends or rewrites during a
	// multi-second read of a large file would otherwise leave a clip holding a
	// mix of old and new content that matches neither what the user reviewed
	// nor what is on disk.
	after, err := f.Stat()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read file: %w", err)
	}
	if !sameFileState(info, after) {
		return nil, nil, fmt.Errorf("file changed while it was being read")
	}

	name := filepath.Base(abs)
	return &FileData{
		Name:        name,
		ContentType: guessImportContentType(name),
		Data:        base64.StdEncoding.EncodeToString(data),
	}, info, nil
}

// statImportFile opens a scanned file without following symlinks and returns
// the FileInfo of the handle. Doing it through an open descriptor rather than
// os.Lstat means the regular-file check and the identity that later gets
// compared both describe the same inode.
func statImportFile(abs string) (os.FileInfo, error) {
	f, err := openImportFile(abs)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file")
	}
	return info, nil
}

// sameFileState reports whether `current` is the same file, unchanged, as
// `want`. Inode identity alone is not enough: a writer can append to the very
// same inode between the read and the delete, which would leave a truncated
// clip behind and destroy the complete original.
func sameFileState(want, current os.FileInfo) bool {
	if want == nil || current == nil {
		return false
	}
	return os.SameFile(want, current) &&
		want.Size() == current.Size() &&
		want.ModTime().Equal(current.ModTime())
}

// reviewMark returns what the user saw for relPath, or nil if they never
// inspected it. A file that was never reviewed has no baseline to contradict,
// so it is not checked — there is nothing the user could be surprised about.
func (a *App) reviewedAs(session *importSession, relPath string) (reviewMark, bool) {
	a.importMu.Lock()
	defer a.importMu.Unlock()
	if session == nil || session.reviewed == nil {
		return reviewMark{}, false
	}
	m, ok := session.reviewed[relPath]
	return m, ok
}

// trashVerified deletes abs only if it is still the file that was reviewed.
//
// Between reading a file and trashing it there is a gap — the DB insert, the
// tagging, the plugin event, and for a delete-only decision the whole of the
// user's think time — and deletion is by path, because no portable API deletes
// an open descriptor. If something replaced or rewrote the path in that window,
// deleting it would destroy content nobody ever looked at.
//
// wantHash, when set, makes that check decisive: stat data alone cannot see a
// same-length in-place rewrite that preserves mtime, which is exactly what
// timestamp-preserving sync tools produce, and what a coarse-granularity
// filesystem (FAT, exFAT) makes easy by accident. The cost is re-reading the
// file before deleting it — worth paying, because on Windows and Linux this
// deletion is permanent.
func trashVerified(abs string, want os.FileInfo, wantHash string) error {
	if want != nil || wantHash != "" {
		f, err := openImportFile(abs)
		if err != nil {
			return fmt.Errorf("file is no longer there to delete: %w", err)
		}
		current, err := f.Stat()
		if err != nil {
			f.Close()
			return fmt.Errorf("file is no longer there to delete: %w", err)
		}
		if want != nil && !sameFileState(want, current) {
			f.Close()
			return fmt.Errorf("file changed since it was read; not deleting")
		}
		if wantHash != "" {
			hash, err := computeContentHashReader(f)
			if err != nil {
				f.Close()
				return fmt.Errorf("could not re-read the file before deleting: %w", err)
			}
			if hash != wantHash {
				f.Close()
				return fmt.Errorf("file contents changed since they were read; not deleting")
			}
		}
		f.Close()
	}
	// A vanishingly small window remains between this check and the unlink:
	// no portable API deletes "this inode" rather than "this path". Narrowing
	// it further would need per-platform work for a race an attacker has
	// microseconds to win, on a file the user themselves selected.
	return trashFile(abs)
}

// deleteHash returns the content hash the user was shown for relPath, or "" if
// they never inspected it — in which case there is no baseline to check against
// and the stat comparison stands alone.
func deleteHash(a *App, session *importSession, relPath string) string {
	if mark, ok := a.reviewedAs(session, relPath); ok {
		return mark.hash
	}
	return ""
}

// resolveImportTag maps a tag name to an id, creating the tag on first use.
// Lookup before create: CreateTag is not idempotent on an existing name.
func (a *App) resolveImportTag(name string, cache map[string]int64) (int64, error) {
	if id, ok := cache[name]; ok {
		return id, nil
	}

	var id int64
	err := a.db.QueryRow("SELECT id FROM tags WHERE name = ?", name).Scan(&id)
	switch {
	case err == nil:
	case errors.Is(err, sql.ErrNoRows):
		tag, createErr := a.CreateTag(name)
		if createErr != nil {
			return 0, createErr
		}
		id = tag.ID
	default:
		return 0, err
	}

	cache[name] = id
	return id, nil
}

// importResolveStatus classifies a containment/lstat failure. A vanished file
// is an ordinary fact about a live folder, not a failure of the run.
func importResolveStatus(err error) string {
	if errors.Is(err, os.ErrNotExist) {
		return ImportStatusMissing
	}
	return ImportStatusInvalid
}
