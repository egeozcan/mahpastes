package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"go-clipboard/internal/bridgeiface"
	"go-clipboard/internal/cliptype"
	"go-clipboard/plugin"

	"golang.design/x/clipboard"
	_ "golang.org/x/image/webp"
)

const maxMetadataPairs = 50

// ClipboardCopier is the clipboard surface used by REST clipboard handlers.
// The desktop ClipboardService satisfies this interface; headless servers leave
// it unset and those handlers return 501.
type ClipboardCopier interface {
	CopyFileToClipboard(id int64) error
	BulkCopyFilesToClipboard(ids []int64) error
	CopyClipContents(id int64) error
}

// App struct holds the application state
type App struct {
	ctx              context.Context
	bridge           bridgeiface.Bridge
	db               *sql.DB
	tempDir          string
	tempStore        *TempClipStore
	transferHandler  *TransferFileHandler
	mu               sync.Mutex
	watcherManager   *WatcherManager
	serveManager     *ServeManager
	apiManager       *APIManager
	pluginManager    *plugin.Manager
	clipboardService ClipboardCopier
	shareManager     *ShareManager
	markdownCache    *markdownImageCache
	markdownLoader   *markdownRemoteImageLoader

	// importSession scopes the folder-import wizard's path-taking methods to
	// one user-picked folder. Guarded by importMu rather than mu because
	// ImportApply holds it across a long run of file I/O and must not block
	// unrelated clip operations. See import_wizard.go.
	importMu      sync.Mutex
	importSession *importSession
	// importApprovedRoot is the folder the user last chose in the native
	// picker. See ApproveImportRoot in import_wizard.go.
	importApprovedRoot string
	// importGeneration is bumped whenever a session is torn down, so a scan
	// that finishes afterwards can tell it is stale.
	importGeneration uint64

	// pluginsReady reports that Bootstrap has finished attempting to load
	// plugins. Until then an empty action set is ambiguous — it could mean
	// "no plugins installed" or "not loaded yet" — and the frontend has no way
	// to tell the two apart without polling. Set regardless of whether plugin
	// init succeeded, since a failed init is also a final answer.
	pluginsReady atomic.Bool

	// shareHookWG tracks the async share-publication goroutines spawned by
	// the tagging paths so callers (tests, shutdown) can wait for a tag
	// operation's fan-out to finish. shareHookMu guards shareHookClosed and
	// serializes the counter's Add against Shutdown's gate close, so a hook
	// can never be registered after the shutdown wait has started observing
	// zero — which is both a lost publication and invalid WaitGroup use.
	//
	// Invariant: every production Add goes through tryAddShareHook or
	// adoptShareHookAdmission. The second form skips the suspend check and is
	// only legal while the caller already holds an admission of its own — see
	// its comment for why that precondition makes the Add safe. A bare Add is
	// only legal where no concurrent from-zero Wait can exist, i.e. the
	// sequential tests that stage a hook before calling the waiter.
	//
	// The gate has two independent states. shareHookClosed is Shutdown's
	// one-way close: once set, no hook is ever admitted again. shareHookSuspends
	// is the reopenable form RestoreBackup uses; it counts nested suspensions
	// and never touches shareHookClosed, so resuming after a restore cannot
	// revive hooks on an app that is quitting.
	shareHookWG       sync.WaitGroup
	shareHookMu       sync.Mutex
	shareHookClosed   bool
	shareHookSuspends int

	// backupRestoreMu is the restore-exclusion lock. RestoreBackup is the only
	// writer; CreateBackup and the share-relevant tag mutations are readers, so
	// they run freely alongside each other and only ever stall for a restore.
	//
	// The write side serializes RestoreBackup against itself. A restore is a
	// sequence of global swaps — stop the share manager, null the pointer,
	// replace every row, install an identity file, rebuild — and two of them
	// interleaved corrupt each other: B walks past the teardown while A holds
	// shareManager nil, A's deferred rebuild installs manager A, then B replaces
	// the database and the identity underneath it and installs manager B without
	// ever stopping A. Manager A is then live against B's database with A's keys.
	//
	// CreateBackup reads two stores that have to agree — the database, and
	// share_identity.key beside it — and a restore landing between those reads
	// pairs database A's active publications with identity B. Nothing about that
	// ZIP looks wrong; it only fails much later, when a takeover restore installs
	// identity B under share strings that name A's peer id and every existing
	// share for that publication goes dead with no way back. Excluding restores
	// is all it needs, so it reads: two concurrent backups export into separate
	// temp dirs and cannot disturb each other, and making them writers would
	// mean every tag mutation below stalls for the length of a full SQL export.
	//
	// AddTagToClip and BulkAddTag read for the whole of their operation — from
	// tx.Begin through the decision to spawn the publication hook — so a tag
	// mutation either finishes entirely before a restore starts or begins
	// entirely after it ends. Without that, the REST API stays open across a
	// restore and a tag mutation can serialize at the SQLite level yet commit
	// just after the restore's own commit, writing clip_tags rows for a clip id
	// that now names an unrelated restored clip. Worse, if it lands in the
	// window where hooks are still suspended, its publication is suppressed and
	// nothing re-scans tag associations, so followers never see that clip at
	// all. Held across the commit and the hook decision, not just the tx,
	// because that whole span has to see one coherent database.
	//
	// The read side is deliberately released before the plugin event: Lua
	// handlers run synchronously and tags.add_to_clip re-enters AddTagToClip,
	// which under Go's RWMutex is a recursive RLock — a deadlock the instant a
	// restore's Lock() is queued between the two acquisitions.
	//
	// The desktop dialogs and the REST endpoints can both reach these
	// operations, so concurrent calls are reachable in production, not just in
	// tests. No reader calls the writer and the writer calls no reader, so the
	// lock cannot self-deadlock; see the audit in RestoreBackup.
	backupRestoreMu sync.RWMutex
}

// PluginsReady reports whether plugin loading has finished (successfully or
// not). An empty UI action set is only meaningful once this is true.
func (a *App) PluginsReady() bool { return a.pluginsReady.Load() }

// NewApp creates a new App instance
func NewApp() *App {
	return &App{bridge: bridgeiface.NoOp{}}
}

func (a *App) SetBridge(b bridgeiface.Bridge) {
	if b == nil {
		a.bridge = bridgeiface.NoOp{}
		return
	}
	a.bridge = b
}

// SetClipboardService wires the desktop clipboard implementation into the core.
func (a *App) SetClipboardService(s ClipboardCopier) { a.clipboardService = s }

// SetDB wires an already-open database into the app. Bootstrap normally does
// this; focused tests use it to exercise services without starting managers.
func (a *App) SetDB(db *sql.DB) { a.db = db }

// SetTransferHandler wires the desktop transfer HTTP handler into the core.
func (a *App) SetTransferHandler(h *TransferFileHandler) { a.transferHandler = h }

func (a *App) DB() *sql.DB                     { return a.db }
func (a *App) PluginManager() *plugin.Manager  { return a.pluginManager }
func (a *App) ShareManager() *ShareManager     { return a.shareManager }
func (a *App) ServeManager() *ServeManager     { return a.serveManager }
func (a *App) WatcherManager() *WatcherManager { return a.watcherManager }
func (a *App) APIManager() *APIManager         { return a.apiManager }
func (a *App) TempStore() *TempClipStore       { return a.tempStore }
func (a *App) TempDir() string                 { return a.tempDir }

func (a *App) PrepareClipTransferItem(id int64, source string) (*PreparedTransferItem, error) {
	return a.prepareClipTransferItem(id, source)
}

// PrepareClipMediaItem returns a short-lived, range-capable URL for playing a
// video clip in the gallery and lightbox.
//
// It is backed by a leased temp file rather than by the clip row. Serving
// ranges straight out of SQLite looks cheaper — no copy on disk — but SQLite
// cannot seek into a blob: every SUBSTR(data, ...) walks the whole overflow
// page chain from byte zero, costing ~0.17ms per MB of clip no matter how few
// bytes are asked for. Streaming a file that way is quadratic in its size (a
// 100MB clip took 1.7s to read through, a 500MB clip ~42s), and every seek pays
// it again. One materialization is O(n); after that http.ServeContent seeks the
// file for free. The frontend caches the URL for the lease window, so a clip is
// copied at most once per session.
func (a *App) PrepareClipMediaItem(id int64) (*PreparedTransferItem, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid clip ID: %d", id)
	}
	if a.db == nil || a.transferHandler == nil {
		return nil, fmt.Errorf("media streaming is not initialized")
	}
	metadata, err := loadClipStreamMetadata(a.db, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrClipNotFound
		}
		return nil, fmt.Errorf("load clip %d: %w", id, err)
	}
	if !strings.HasPrefix(metadata.contentType, "video/") {
		return nil, fmt.Errorf("clip %d is not a video", id)
	}

	// Reuse a temp file that is still under lease instead of recopying the clip.
	prepared, err := a.lookupPreparedClipTransferItem(id, mediaPreviewChannel)
	if err != nil || prepared == nil {
		prepared, err = a.prepareClipTransferItem(id, mediaPreviewChannel)
		if err != nil {
			return nil, err
		}
	}

	token, err := generateTransferToken()
	if err != nil {
		return nil, fmt.Errorf("generate media token: %w", err)
	}
	name := filepath.Base(prepared.AbsPath)
	a.transferHandler.RegisterMediaToken(token, prepared.AbsPath, name, metadata.contentType, prepared.LeaseExpiresAt)
	return &PreparedTransferItem{
		ClipID:         id,
		AbsPath:        prepared.AbsPath,
		TransferURL:    "/media/" + token + "/" + url.PathEscape(name),
		Filename:       metadata.filename.String,
		ContentType:    metadata.contentType,
		LeaseExpiresAt: prepared.LeaseExpiresAt,
	}, nil
}

func (a *App) LookupPreparedClipTransferItem(id int64, source string) (*PreparedTransferItem, error) {
	return a.lookupPreparedClipTransferItem(id, source)
}

func (a *App) InitTempStore(dataDir string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.tempDir = filepath.Join(dataDir, "clip_temp_files")
	if err := os.MkdirAll(a.tempDir, 0755); err != nil {
		return fmt.Errorf("create temp dir %q: %w", a.tempDir, err)
	}
	a.tempStore = NewTempClipStore(a.db, a.tempDir, defaultTempLeaseTTL, defaultTempPruneInterval)
	log.Printf("Temporary files will be stored in %s\n", a.tempDir)
	return nil
}

// computeContentHash returns the hex-encoded SHA-256 hash of data.
func computeContentHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// computeContentHashReader is computeContentHash over a stream, for callers
// that must not hold the whole file in memory. It produces byte-identical
// output, so hashes from the two are directly comparable against the
// clips.content_hash column.
func computeContentHashReader(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// isSQLiteNoSuchTable returns true when err is the SQLite "no such table"
// error. Used to make DeleteTag tolerant of minimal test schemas that omit
// optional tables (watched_folders, settings, follows).
func isSQLiteNoSuchTable(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no such table")
}

// emitEvent sends a frontend event. The bridge is nil-safe, so this is a
// no-op during tests that don't run the Wails lifecycle.
func (a *App) emitEvent(event string, data ...interface{}) {
	if a.bridge == nil {
		return
	}
	a.bridge.Emit(event, data...)
}

// emitPluginEvent dispatches a plugin event, guarded for nil pluginManager.
func (a *App) emitPluginEvent(name string, data map[string]interface{}) {
	if a.pluginManager != nil {
		a.pluginManager.EmitEvent(name, data)
	}
}

// emitWatchError sends an error event to the frontend
func (a *App) emitWatchError(filePath string, errMsg string) {
	if a.bridge == nil {
		return
	}
	a.bridge.Emit("watch:error", map[string]string{
		"file":  filepath.Base(filePath),
		"error": errMsg,
	})
}

// emitWatchImport sends an import event to the frontend with full clip data
func (a *App) emitWatchImport(clip ClipPreview) {
	if a.bridge == nil {
		return
	}
	a.bridge.Emit("watch:import", clip)
}

// getClipPreview fetches a single clip's preview data (private helper, not exported to frontend)
func (a *App) getClipPreview(id int64) (*ClipPreview, error) {
	var clip ClipPreview
	var filename sql.NullString
	var expiresAt sql.NullTime
	var previewData []byte
	var isArchivedInt int

	err := a.db.QueryRow(`
		SELECT id, content_type, filename, created_at, expires_at, SUBSTR(data, 1, 500), is_archived, LENGTH(data)
		FROM clips WHERE id = ?`, id).Scan(
		&clip.ID, &clip.ContentType, &filename, &clip.CreatedAt, &expiresAt, &previewData, &isArchivedInt, &clip.Size)
	if err != nil {
		return nil, err
	}

	clip.Filename = filename.String
	clip.IsArchived = isArchivedInt == 1
	if expiresAt.Valid {
		clip.ExpiresAt = &expiresAt.Time
	}
	if strings.HasPrefix(clip.ContentType, "text/") || clip.ContentType == "application/json" {
		clip.Preview = string(previewData)
	}

	clip.Tags, _ = a.GetClipTags(id)
	if clip.Tags == nil {
		clip.Tags = []Tag{}
	}
	return &clip, nil
}

// RefreshWatches reloads the watcher configuration
func (a *App) RefreshWatches() error {
	if a.watcherManager != nil {
		return a.watcherManager.refreshWatches()
	}
	return nil
}

type BootstrapOptions struct {
	DB                 *sql.DB
	DataDir            string
	Bridge             bridgeiface.Bridge
	InitClipboard      bool
	PermissionCallback func(pluginName, permType, requestedPath string) string
	// FSConfinementRoot, when set, confines all plugin filesystem access under
	// this directory regardless of what the shared plugin_permissions table
	// grants. The headless server sets it to DataDir; the desktop build leaves
	// it empty (user may approve arbitrary directories).
	FSConfinementRoot string
}

// Bootstrap initializes the shared runtime managers around an already-open DB.
func (a *App) Bootstrap(ctx context.Context, opts BootstrapOptions) error {
	if opts.DB == nil || opts.PermissionCallback == nil {
		return fmt.Errorf("Bootstrap: DB and PermissionCallback are required")
	}
	a.ctx = ctx
	a.SetBridge(opts.Bridge)
	a.db = opts.DB

	if err := a.InitMarkdownImages(opts.DataDir); err != nil {
		log.Printf("Warning: Failed to initialize Markdown images: %v", err)
	}

	StartCleanupJob(ctx, a.db)

	if err := a.InitTempStore(opts.DataDir); err != nil {
		log.Printf("Warning: Failed to initialize temp store: %v", err)
	} else if err := a.tempStore.Prune(true); err != nil {
		log.Printf("Warning: Failed to prune temp clip files on startup: %v", err)
	}

	if opts.InitClipboard {
		if err := clipboard.Init(); err != nil {
			log.Printf("Warning: Failed to initialize clipboard: %v", err)
		}
	}

	wm, err := NewWatcherManager(a)
	if err != nil {
		log.Printf("Warning: Failed to initialize watcher manager: %v", err)
	} else {
		a.watcherManager = wm
		if err := wm.Start(); err != nil {
			log.Printf("Warning: Failed to start watcher: %v", err)
		}
	}

	a.serveManager = NewServeManager(a)

	sm, smErr := NewShareManager(ctx, a.db, opts.DataDir)
	if smErr != nil {
		log.Printf("Warning: Failed to initialize share manager: %v", smErr)
	} else {
		a.shareManager = sm
		sm.SetEventFn(a.bridge.Emit)
		if err := sm.ResumeAll(); err != nil {
			log.Printf("Warning: ShareManager ResumeAll: %v", err)
		}
		sm.StartSweepers()
	}

	a.apiManager = NewAPIManager(a, opts.DataDir)

	pluginsDir := filepath.Join(opts.DataDir, "plugins")
	pm, err := plugin.NewManager(ctx, a.bridge, a.db, pluginsDir)
	if err != nil {
		log.Printf("Warning: Failed to initialize plugin manager: %v", err)
	} else {
		a.pluginManager = pm
		pm.SetMetadataFuncs(a.GetClipMetadata, a.UpdateClipMetadata)
		pm.SetTagCreateFunc(func(name string) (*plugin.TagCreateResult, error) {
			tag, err := a.CreateTag(name)
			if err != nil {
				return nil, err
			}
			return &plugin.TagCreateResult{
				ID:    tag.ID,
				Name:  tag.Name,
				Color: tag.Color,
			}, nil
		})
		pm.SetPermissionCallback(opts.PermissionCallback)
		pm.SetFSConfinementRoot(opts.FSConfinementRoot)

		if err := pm.LoadPlugins(); err != nil {
			log.Printf("Warning: Failed to load plugins: %v", err)
		}
		// Plugin loading happens late in Bootstrap and can outlast the frontend's
		// initial action fetch on a cold launch. Notify the UI when the complete,
		// atomic action set is actually ready.
		a.bridge.Emit("plugin:ready", nil)

		pm.EmitEvent("app:startup", nil)

		uc := plugin.NewUpdateChecker(a.ctx, a.bridge, a.db, pm)
		pm.SetUpdateChecker(uc)
		interval := a.getUpdateCheckInterval()
		if interval != "disabled" {
			uc.Start(ParseUpdateInterval(interval))
		}
	}
	// Set on both paths: if the plugin manager failed to initialise, an empty
	// action set is still the final answer, and the frontend should stop
	// waiting for plugins that are never going to arrive.
	a.pluginsReady.Store(true)
	return nil
}

// Shutdown is called when the app is closing
func (a *App) Shutdown(ctx context.Context) {
	if a.markdownLoader != nil {
		a.markdownLoader.CancelAll()
	}

	// Shutdown plugins first
	if a.pluginManager != nil {
		a.pluginManager.Shutdown()
	}

	// Stop API server
	if a.apiManager != nil {
		a.apiManager.Stop()
	}

	// Stop all serving
	if a.serveManager != nil {
		a.serveManager.StopAll()
	}

	// Stop watcher
	if a.watcherManager != nil {
		a.watcherManager.Stop()
	}

	// Share-publication hooks fired by AddTagToClip/BulkAddTag run in their
	// own goroutines. Give them a bounded window to finish before the share
	// manager and the DB go away: nothing re-scans tag associations on the
	// next startup, so an emission skipped here never happens at all.
	//
	// The gate has to close before the wait. Waiting without it is a race in
	// both directions: a tag operation that commits while Wait sees zero would
	// run its hook against a stopping manager and a closing DB, and its
	// registration would be a from-zero Add concurrent with a Wait, which
	// WaitGroup does not allow.
	a.closeShareHooks()
	if !a.waitForShareHooks(shareHookShutdownTimeout) {
		log.Printf("share: timed out after %s waiting for in-flight publication hooks; some newly tagged clips may not have reached followers", shareHookShutdownTimeout)
	}

	// Stop share manager
	if a.shareManager != nil {
		a.shareManager.Stop()
	}

	if a.db != nil {
		a.db.Close()
	}
	// Clean up temp files
	a.DeleteAllTempFiles()
}

// shareHookShutdownTimeout bounds how long Shutdown blocks on in-flight
// share-publication hooks. Long enough for a normal emission to land, short
// enough that a stuck one still lets the app quit.
const shareHookShutdownTimeout = 5 * time.Second

// tryAddShareHook registers one about-to-be-spawned share-publication
// goroutine and reports whether the caller may spawn it. It returns false once
// shutdown has closed the gate or while a restore has it suspended; the caller
// then completes normally without publishing. The matching Done belongs to the
// goroutine that gets spawned.
func (a *App) tryAddShareHook() bool {
	a.shareHookMu.Lock()
	defer a.shareHookMu.Unlock()
	if a.shareHookClosed || a.shareHookSuspends > 0 {
		return false
	}
	a.shareHookWG.Add(1)
	return true
}

// adoptShareHookAdmission registers one more in-flight share-publication
// goroutine on the strength of an admission the caller already holds, and
// reports whether the caller may spawn it. Unlike tryAddShareHook it does not
// consult the suspend counter.
//
// Only legal while the caller already holds an admission. Two things follow
// from that precondition. The WaitGroup counter is provably non-zero, so this
// can never be the from-zero Add that races a concurrent Wait. And any drain
// running right now is already pinned by the caller's admission, so it has not
// yet reached the point where the state these children publish against goes
// away: they observe pre-restore rows and publish through the pre-restore
// manager, which is still alive precisely because the restore has not passed
// its drain. The drain simply waits for the children too.
//
// A suspension must not refuse here, and that is the whole reason this exists.
// A restore suspends the gate and then blocks in its drain on the very
// admission the caller holds; if the caller's children re-entered the gate,
// every one of them would be refused while their rows were already committed.
// Nothing re-scans tag associations, so a restore that then failed and rolled
// back would leave those tags on disk with their publications never sent and
// no later pass to notice.
//
// Shutdown's close is respected, so children are suppressed at shutdown exactly
// as they are today. The transfer is what restore correctness needs; a quit is
// best-effort by construction — waitForShareHooks gives up after
// shareHookShutdownTimeout and the process exits regardless — so admitting more
// work there would only risk overrunning that bound to no end.
func (a *App) adoptShareHookAdmission() bool {
	a.shareHookMu.Lock()
	defer a.shareHookMu.Unlock()
	if a.shareHookClosed {
		return false
	}
	a.shareHookWG.Add(1)
	return true
}

// closeShareHooks permanently stops new hook registrations. Hooks already past
// the gate keep running and are drained by waitForShareHooks; publications
// refused after this point are lost the same way a hard quit loses them.
func (a *App) closeShareHooks() {
	a.shareHookMu.Lock()
	a.shareHookClosed = true
	a.shareHookMu.Unlock()
}

// suspendShareHooks refuses new hook registrations until the matching
// resumeShareHooks call. Suspensions nest, and none of them clears
// shareHookClosed, so a suspend/resume pair around a restore can never reopen a
// gate that Shutdown has closed for good.
func (a *App) suspendShareHooks() {
	a.shareHookMu.Lock()
	a.shareHookSuspends++
	a.shareHookMu.Unlock()
}

// resumeShareHooks releases one suspension. Hooks are admitted again once the
// last suspension is released, and only if shutdown has not closed the gate in
// the meantime.
func (a *App) resumeShareHooks() {
	a.shareHookMu.Lock()
	if a.shareHookSuspends > 0 {
		a.shareHookSuspends--
	}
	a.shareHookMu.Unlock()
}

// quiesceShareHooksForRestore suspends new share-publication hooks, drains the
// ones already in flight, and returns the resume the caller must defer. It is
// the restore-time counterpart of Shutdown's close: RestoreBackup replaces
// every row in the database and swaps the share manager, and a hook that spans
// that swap publishes pre-restore state — envelopes sealed with the old symkey
// — into the restored ring, corrupting catch-up for the restored followers.
//
// The drain reuses Shutdown's bound: the work being waited on is the same
// single emission, so the same "long enough to land, short enough not to hang"
// tradeoff applies.
//
// Timeout carries Shutdown's caveat: a straggler that outlives the drain keeps
// running against the stopped manager, and the resume readmits hooks while that
// straggler is still counted. Bounded and logged rather than waited on forever,
// because a stuck hook must not make restore unfinishable.
func (a *App) quiesceShareHooksForRestore() func() {
	a.suspendShareHooks()
	if !a.waitForShareHooks(shareHookShutdownTimeout) {
		log.Printf("share: timed out after %s draining in-flight publication hooks before restore; a straggler may still publish pre-restore state", shareHookShutdownTimeout)
	}
	return a.resumeShareHooks
}

// waitForShareHooks blocks until every in-flight share-publication goroutine
// has finished, or until timeout elapses. Reports whether they all finished.
// On the timeout path the waiter goroutine outlives this call; it holds only
// the WaitGroup and exits once the hooks drain.
func (a *App) waitForShareHooks(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		a.shareHookWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// shareHookAdmittedHook runs inside spawnAdmittedShareHook before the share
// manager pointer is captured — that is, with the admission already held. It is
// nil in production; tests set it to hold a call inside that exact window and
// prove that the admission — not the capture — is what a restore's drain waits
// on. Stored atomically so setting it can never race a concurrent tagging call.
var shareHookAdmittedHook atomic.Pointer[func()]

// spawnAdmittedShareHook publishes one newly tagged clip to followers on its
// own goroutine, consuming an admission the caller already holds. It is the
// half of the publication path that runs after the gate has said yes.
//
// The order here is the whole point: the admission is held before a.shareManager
// is read, and is released only when the publication is done. Reading the
// manager without holding an admission left a window where a caller could
// capture the pre-restore manager, stall, and be counted only after
// RestoreBackup had drained a WaitGroup this call had not yet joined, stopped
// that manager, replaced the database and rebuilt — at which point the hook
// sealed envelopes with the old symkey into the restored ring.
//
// Callers take the admission before the transaction that decides there is
// something to publish commits, so the window this closes now starts at the
// commit rather than here; see AddTagToClip.
//
// The admission is consumed on every path. A nil manager releases it here
// rather than leaving it in the WaitGroup, where it would stall every later
// drain.
func (a *App) spawnAdmittedShareHook(clipID, tagID int64, source string) {
	if fn := shareHookAdmittedHook.Load(); fn != nil {
		(*fn)()
	}
	sm := a.shareManager
	if sm == nil {
		a.shareHookWG.Done()
		return
	}
	go func() {
		defer a.shareHookWG.Done()
		if err := sm.OnClipCreated(clipID, []int64{tagID}); err != nil {
			log.Printf("share: OnClipCreated(%d) from %s: %v", clipID, source, err)
		}
	}()
}

// spawnShareHook takes an admission and publishes one newly tagged clip,
// reporting whether the gate admitted it. A false means shutdown closed the
// gate or a restore has it suspended, and the caller skips the publication.
//
// This is the convenience form for a caller whose publication decision is not
// tied to a transaction commit. Both tagging paths pin the drain across their
// own commit instead, so they take the admission themselves — with
// tryAddShareHook for the operation, adoptShareHookAdmission for each
// publication under it — and hand each one to spawnAdmittedShareHook. There
// must never be two admissions for one publication.
func (a *App) spawnShareHook(clipID, tagID int64, source string) bool {
	if !a.tryAddShareHook() {
		return false
	}
	a.spawnAdmittedShareHook(clipID, tagID, source)
	return true
}

// shutdown is kept for the Wails lifecycle until the desktop entry moves to
// the exported method.
func (a *App) shutdown(ctx context.Context) {
	a.Shutdown(ctx)
}

func (a *App) getUpdateCheckInterval() string {
	var value string
	err := a.db.QueryRow("SELECT value FROM app_settings WHERE key = 'plugin_update_interval'").Scan(&value)
	if err != nil {
		return "24h"
	}
	return value
}

func ParseUpdateInterval(interval string) time.Duration {
	switch interval {
	case "startup":
		return 0
	case "6h":
		return 6 * time.Hour
	case "24h":
		return 24 * time.Hour
	default:
		return 24 * time.Hour
	}
}

// ClipPreview is the struct for JSON responses in the gallery
type ClipPreview struct {
	ID             int64      `json:"id"`
	ContentType    string     `json:"content_type"`
	Filename       string     `json:"filename"`
	CreatedAt      time.Time  `json:"created_at"`
	ExpiresAt      *time.Time `json:"expires_at"`
	Preview        string     `json:"preview"`
	IsArchived     bool       `json:"is_archived"`
	Tags           []Tag      `json:"tags"`
	Size           int64      `json:"size"`
	DuplicateCount int        `json:"duplicate_count"`
}

// DuplicateGroup represents a set of clips sharing the same content hash
type DuplicateGroup struct {
	ContentHash string `json:"content_hash"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Count       int    `json:"count"`
	OldestID    int64  `json:"oldest_id"`
}

// ClipData for full clip retrieval
type ClipData struct {
	ID           int64  `json:"id"`
	ContentType  string `json:"content_type"`
	Data         string `json:"data"` // base64 encoded for binary, raw for valid text
	Filename     string `json:"filename"`
	ValidUTF8    bool   `json:"valid_utf8"`
	DataEncoding string `json:"data_encoding"` // "utf8" or "base64"
}

// DiffResult returned by GetImageDiff
type DiffResult struct {
	Similarity  float64 `json:"similarity"`
	DiffDataUrl string  `json:"diff_data_url"` // data:image/png;base64,...
}

// FileData for uploads - binary data as base64
type FileData struct {
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Data        string `json:"data"` // base64 encoded
}

// WatchedFolder represents a folder being watched for new files
type WatchedFolder struct {
	ID              int64     `json:"id"`
	Path            string    `json:"path"`
	FilterMode      string    `json:"filter_mode"`      // "all", "presets", "custom"
	FilterPresets   []string  `json:"filter_presets"`   // ["images", "videos", "documents"]
	FilterRegex     string    `json:"filter_regex"`     // regex pattern for custom mode
	ProcessExisting bool      `json:"process_existing"` // import existing files when added
	AutoArchive     bool      `json:"auto_archive"`     // archive imports immediately
	AutoTagID       *int64    `json:"auto_tag_id"`      // tag to auto-apply on import
	IsPaused        bool      `json:"is_paused"`        // per-folder pause
	CreatedAt       time.Time `json:"created_at"`
	Exists          bool      `json:"exists"` // whether folder path exists on disk
}

// WatchedFolderConfig for creating/updating watched folders
type WatchedFolderConfig struct {
	Path            string   `json:"path"`
	FilterMode      string   `json:"filter_mode"`
	FilterPresets   []string `json:"filter_presets"`
	FilterRegex     string   `json:"filter_regex"`
	ProcessExisting bool     `json:"process_existing"`
	AutoArchive     bool     `json:"auto_archive"`
	AutoTagID       *int64   `json:"auto_tag_id"`
}

// Tag represents a clip tag with color
type Tag struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
	Count int    `json:"count"` // Number of clips using this tag
}

// Constants for tag operations
const (
	maxTagNameLength = 50
	defaultClipLimit = 50
)

// tagColors is the palette of colors auto-assigned to new tags
var tagColors = []string{
	"#78716C", // stone
	"#EF4444", // red
	"#F59E0B", // amber
	"#22C55E", // green
	"#3B82F6", // blue
	"#8B5CF6", // violet
	"#EC4899", // pink
	"#06B6D4", // cyan
}

func sortColumn(field string) string {
	switch field {
	case "name":
		return "c.filename"
	case "size":
		return "LENGTH(c.data)"
	case "type":
		return "c.content_type"
	default:
		return "c.created_at"
	}
}

// GetClips retrieves a list of clips for the gallery, optionally filtered by tags
func (a *App) GetClips(archived bool, tagIDs []int64, hiddenTagIDs []int64, sortField string, sortDir string) ([]ClipPreview, error) {
	return a.getClipsInternal(archived, tagIDs, hiddenTagIDs, sortField, sortDir, true, nil)
}

func (a *App) GetClipsDirect(archived bool, tagIDs []int64, hiddenTagIDs []int64, sortField string, sortDir string) ([]ClipPreview, error) {
	return a.getClipsInternal(archived, tagIDs, hiddenTagIDs, sortField, sortDir, false, nil)
}

// clipSearchSpec is a free-text search layered on top of the tag, archive and
// expiry filters of a clip listing. Query matches the filename and the content
// type, mirroring the gallery's client-side filter so both paths agree on what
// a plain search means; InContent widens it to the stored bytes of text-like
// clips, which the gallery cannot see (cards only carry a 500-byte preview).
type clipSearchSpec struct {
	Query     string
	InContent bool
}

// textClipCondition is the SQL form of the "text-based clip" test used when
// building previews: only these types are worth scanning for a content match,
// and scanning image or archive bytes would produce nonsense hits.
const textClipCondition = "(c.content_type LIKE 'text/%' OR c.content_type = 'application/json')"

// buildClipSearchClause renders a search spec into a WHERE fragment plus its
// arguments. Returns ("", nil) when there is nothing to search for.
//
// SQLite LIKE is case-insensitive for ASCII only. Keep the query's non-ASCII
// code points intact: applying Go's Unicode-aware lowercase to only the
// parameter makes otherwise literal matches such as ÄPFEL → Äpfel impossible.
func buildClipSearchClause(search *clipSearchSpec) (string, []interface{}) {
	if search == nil {
		return "", nil
	}
	query := strings.TrimSpace(search.Query)
	if query == "" {
		return "", nil
	}
	// Wildcards typed by the user are literals, not patterns.
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
	pattern := "%" + escaped + "%"

	terms := []string{
		`COALESCE(c.filename, '') LIKE ? ESCAPE '\'`,
		`c.content_type LIKE ? ESCAPE '\'`,
	}
	args := []interface{}{pattern, pattern}
	if search.InContent {
		terms = append(terms, `(`+textClipCondition+` AND CAST(c.data AS TEXT) LIKE ? ESCAPE '\')`)
		args = append(args, pattern)
	}
	return "\n\t\t  AND (" + strings.Join(terms, " OR ") + ")", args
}

// SearchClips returns the clips matching a free-text query, under the same tag,
// archive and expiry rules as GetClips. Pass an empty hiddenTagIDs slice to let
// clips carrying a hidden tag surface in the results — hiding is a browsing
// convenience, and search is where a user goes when they know what they want.
func (a *App) SearchClips(archived bool, tagIDs []int64, hiddenTagIDs []int64, query string, searchContent bool, sortField string, sortDir string) ([]ClipPreview, error) {
	return a.getClipsInternal(archived, tagIDs, hiddenTagIDs, sortField, sortDir, true,
		&clipSearchSpec{Query: query, InContent: searchContent})
}

// GetFolderClips returns clips tagged with the given tag but NOT tagged with any
// descendant of that tag. Used by folder mode so clips appear only at their
// deepest folder level.
//
// Hidden tags are deliberately NOT applied here: in folder mode, hiding affects
// how folder cards are drawn (dimmed), not which clips a folder contains. A clip
// tagged both `contacts` and `web/contacts` must still appear inside `contacts`
// even when `web` is hidden — otherwise the folder looks empty while its card
// still counts the clip.
func (a *App) GetFolderClips(archived bool, tagID int64, sortField string, sortDir string) ([]ClipPreview, error) {
	descendantIDs, err := a.getDescendantTagIDs(tagID)
	if err != nil {
		return nil, err
	}
	// Descendant IDs ride the hidden-tag channel purely as exclusions, so clips
	// that live in a subfolder are not repeated at this level.
	return a.getClipsInternal(archived, []int64{tagID}, descendantIDs, sortField, sortDir, false, nil)
}

// GetUntaggedClips returns clips that have no tags at all.
func (a *App) GetUntaggedClips(archived bool, hiddenTagIDs []int64, sortField string, sortDir string) ([]ClipPreview, error) {
	return a.getClipsInternal(archived, nil, hiddenTagIDs, sortField, sortDir, false, nil, true)
}

// HiddenClipInfo reports clips that match the active tag filters but are kept
// out of the gallery because they also carry a hidden tag. Tags lists the hidden
// tag names responsible, so the UI can explain what is being withheld.
type HiddenClipInfo struct {
	Count int      `json:"count"`
	Tags  []string `json:"tags"`
}

// GetHiddenClipInfo counts the clips that GetClips would have returned for these
// filters if no tags were hidden. Same filter expansion as GetClips, with the
// hidden anti-join flipped into a requirement.
func (a *App) GetHiddenClipInfo(archived bool, tagIDs []int64, hiddenTagIDs []int64) (HiddenClipInfo, error) {
	info := HiddenClipInfo{Tags: []string{}}

	scope := a.buildClipFilterScope(tagIDs, hiddenTagIDs, true)
	if len(scope.effectiveHidden) == 0 {
		return info, nil
	}

	archivedInt := 0
	if archived {
		archivedInt = 1
	}

	var conditions []string
	var args []interface{}
	for _, group := range scope.filterGroups {
		placeholders := make([]string, len(group.ids))
		for i, id := range group.ids {
			placeholders[i] = "?"
			args = append(args, id)
		}
		conditions = append(conditions,
			fmt.Sprintf("EXISTS (SELECT 1 FROM clip_tags ct WHERE ct.clip_id = c.id AND ct.tag_id IN (%s))",
				strings.Join(placeholders, ",")))
	}

	hiddenPlaceholders := make([]string, len(scope.effectiveHidden))
	for i, id := range scope.effectiveHidden {
		hiddenPlaceholders[i] = "?"
		args = append(args, id)
	}
	hiddenIn := strings.Join(hiddenPlaceholders, ",")
	conditions = append(conditions,
		fmt.Sprintf("EXISTS (SELECT 1 FROM clip_tags ct WHERE ct.clip_id = c.id AND ct.tag_id IN (%s))", hiddenIn))
	conditions = append(conditions, "c.is_archived = ?", "(c.expires_at IS NULL OR c.expires_at > CURRENT_TIMESTAMP)")
	args = append(args, archivedInt)

	where := strings.Join(conditions, "\n\t\t  AND ")

	if err := a.db.QueryRow(
		fmt.Sprintf("SELECT COUNT(DISTINCT c.id) FROM clips c WHERE %s", where), args...,
	).Scan(&info.Count); err != nil {
		return info, fmt.Errorf("failed to count hidden clips: %w", err)
	}
	if info.Count == 0 {
		return info, nil
	}

	// Name the hidden tags actually responsible, not every hidden tag.
	nameArgs := append([]interface{}{}, args...)
	for _, id := range scope.effectiveHidden {
		nameArgs = append(nameArgs, id)
	}
	rows, err := a.db.Query(fmt.Sprintf(`
		SELECT DISTINCT t.name
		FROM clips c
		INNER JOIN clip_tags ct ON ct.clip_id = c.id
		INNER JOIN tags t ON t.id = ct.tag_id
		WHERE %s
		  AND t.id IN (%s)
		ORDER BY t.name`, where, hiddenIn), nameArgs...)
	if err != nil {
		return info, fmt.Errorf("failed to list hidden tags: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		info.Tags = append(info.Tags, name)
	}
	return info, rows.Err()
}

// tagFilterGroup is one active tag filter, expanded to the tag IDs that satisfy
// it. A clip must match every group (AND across groups, OR within a group).
type tagFilterGroup struct {
	ids []int64
}

// clipFilterScope is the resolved tag scope of a clip query: which tags satisfy
// each active filter, and which hidden tags still apply once the filters have
// revealed their own subtree and ancestors.
type clipFilterScope struct {
	filterGroups    []tagFilterGroup
	effectiveHidden []int64
}

// buildClipFilterScope resolves active filters and hidden tags into the ID sets
// the clip queries run on. Shared by the listing query and the hidden-clip
// counter so the "N hidden" note can never disagree with the list it sits under.
func (a *App) buildClipFilterScope(tagIDs []int64, hiddenTagIDs []int64, expandFilters bool) clipFilterScope {
	// Expand hidden tags to include descendants (always, regardless of expandFilters)
	expandedHidden := make([]int64, 0, len(hiddenTagIDs))
	for _, tagID := range hiddenTagIDs {
		expandedHidden = append(expandedHidden, tagID)
		descendants, err := a.getDescendantTagIDs(tagID)
		if err == nil {
			expandedHidden = append(expandedHidden, descendants...)
		}
	}

	// Build expanded filter groups (one per original tag filter)
	var filterGroups []tagFilterGroup
	// Collect all filter IDs (expanded) for effective hidden computation
	allFilterIDs := make(map[int64]bool)
	for _, tagID := range tagIDs {
		group := tagFilterGroup{ids: []int64{tagID}}
		allFilterIDs[tagID] = true
		if expandFilters {
			descendants, err := a.getDescendantTagIDs(tagID)
			if err == nil {
				group.ids = append(group.ids, descendants...)
				for _, d := range descendants {
					allFilterIDs[d] = true
				}
			}
		}
		filterGroups = append(filterGroups, group)
	}

	// Also mark ancestors of active filter tags so they are not hidden.
	// This ensures filtering by a subtag of a hidden parent reveals clips correctly.
	for _, tagID := range tagIDs {
		var tagName string
		if err := a.db.QueryRow("SELECT name FROM tags WHERE id = ?", tagID).Scan(&tagName); err != nil {
			continue
		}
		for _, ancestor := range getAncestorTagNames(tagName) {
			var ancestorID int64
			if err := a.db.QueryRow("SELECT id FROM tags WHERE name = ?", ancestor).Scan(&ancestorID); err == nil {
				allFilterIDs[ancestorID] = true
			}
		}
	}

	// Compute effective hidden tags: remove any that are in active filters, their descendants, or their ancestors
	effectiveHidden := make([]int64, 0)
	for _, id := range expandedHidden {
		if !allFilterIDs[id] {
			effectiveHidden = append(effectiveHidden, id)
		}
	}

	return clipFilterScope{filterGroups: filterGroups, effectiveHidden: effectiveHidden}
}

func (a *App) getClipsInternal(archived bool, tagIDs []int64, hiddenTagIDs []int64, sortField string, sortDir string, expandFilters bool, search *clipSearchSpec, untaggedOnly ...bool) ([]ClipPreview, error) {
	archivedInt := 0
	if archived {
		archivedInt = 1
	}

	col := sortColumn(sortField)
	dir := "DESC"
	if sortDir == "asc" {
		dir = "ASC"
	}
	orderClause := fmt.Sprintf("ORDER BY %s %s", col, dir)
	if col != "c.created_at" {
		orderClause += ", c.created_at DESC, c.id DESC"
	} else if dir == "DESC" {
		orderClause += ", c.id DESC"
	} else {
		orderClause += ", c.id ASC"
	}

	scope := a.buildClipFilterScope(tagIDs, hiddenTagIDs, expandFilters)
	filterGroups := scope.filterGroups
	effectiveHidden := scope.effectiveHidden

	var query string
	var args []interface{}

	wantUntagged := len(untaggedOnly) > 0 && untaggedOnly[0]
	untaggedClause := ""
	if wantUntagged {
		untaggedClause = "\n\t\t  AND NOT EXISTS (SELECT 1 FROM clip_tags ct2 WHERE ct2.clip_id = c.id)"
	}

	searchClause, searchArgs := buildClipSearchClause(search)

	selectCols := `c.id, c.content_type, c.filename, c.created_at, c.expires_at, SUBSTR(c.data, 1, 500), c.is_archived, LENGTH(c.data),
		       (SELECT COUNT(*) FROM clips c2 WHERE c2.content_hash = c.content_hash AND c2.content_hash != '' AND c2.id != c.id)`

	if len(filterGroups) > 0 {
		// Filter by tags using EXISTS per group (AND logic - clip must match ALL groups)

		// Build EXISTS clauses for each filter group
		var existsClauses []string
		for _, group := range filterGroups {
			placeholders := make([]string, len(group.ids))
			for i, id := range group.ids {
				placeholders[i] = "?"
				args = append(args, id)
			}
			existsClauses = append(existsClauses,
				fmt.Sprintf("EXISTS (SELECT 1 FROM clip_tags ct WHERE ct.clip_id = c.id AND ct.tag_id IN (%s))",
					strings.Join(placeholders, ",")))
		}

		// Hidden tags anti-join via NOT EXISTS
		hiddenClause := ""
		if len(effectiveHidden) > 0 {
			hiddenPlaceholders := make([]string, len(effectiveHidden))
			for i, id := range effectiveHidden {
				hiddenPlaceholders[i] = "?"
				args = append(args, id)
			}
			hiddenClause = fmt.Sprintf("\n\t\t  AND NOT EXISTS (SELECT 1 FROM clip_tags ct WHERE ct.clip_id = c.id AND ct.tag_id IN (%s))",
				strings.Join(hiddenPlaceholders, ","))
		}

		args = append(args, searchArgs...)
		args = append(args, archivedInt)

		query = fmt.Sprintf(`
		SELECT %s
		FROM clips c
		WHERE %s%s%s%s
		  AND c.is_archived = ?
		  AND (c.expires_at IS NULL OR c.expires_at > CURRENT_TIMESTAMP)
		%s
		LIMIT %d`, selectCols, strings.Join(existsClauses, "\n\t\t  AND "), hiddenClause, untaggedClause, searchClause, orderClause, defaultClipLimit)
	} else if len(effectiveHidden) > 0 {
		// No tag filters but has hidden tags - use NOT EXISTS anti-join
		hiddenPlaceholders := make([]string, len(effectiveHidden))
		for i, id := range effectiveHidden {
			hiddenPlaceholders[i] = "?"
			args = append(args, id)
		}
		args = append(args, searchArgs...)
		args = append(args, archivedInt)

		query = fmt.Sprintf(`
		SELECT %s
		FROM clips c
		WHERE NOT EXISTS (SELECT 1 FROM clip_tags ct WHERE ct.clip_id = c.id AND ct.tag_id IN (%s))%s%s
		  AND c.is_archived = ?
		  AND (c.expires_at IS NULL OR c.expires_at > CURRENT_TIMESTAMP)
		%s
		LIMIT %d`, selectCols, strings.Join(hiddenPlaceholders, ","), untaggedClause, searchClause, orderClause, defaultClipLimit)
	} else {
		// No filters, no hidden tags - original simple query
		args = append(args, archivedInt)
		args = append(args, searchArgs...)
		query = fmt.Sprintf(`
		SELECT %s
		FROM clips c
		WHERE c.is_archived = ?%s%s AND (c.expires_at IS NULL OR c.expires_at > CURRENT_TIMESTAMP)
		%s
		LIMIT %d`, selectCols, untaggedClause, searchClause, orderClause, defaultClipLimit)
	}

	rows, err := a.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query clips: %w", err)
	}
	defer rows.Close()

	var clips []ClipPreview
	var clipIDs []int64
	for rows.Next() {
		var clip ClipPreview
		var filename sql.NullString
		var expiresAt sql.NullTime
		var previewData []byte
		var isArchivedInt int

		if err := rows.Scan(&clip.ID, &clip.ContentType, &filename, &clip.CreatedAt, &expiresAt, &previewData, &isArchivedInt, &clip.Size, &clip.DuplicateCount); err != nil {
			log.Printf("Failed to scan clip row: %v\n", err)
			continue
		}

		clip.Filename = filename.String
		clip.IsArchived = isArchivedInt == 1
		if expiresAt.Valid {
			clip.ExpiresAt = &expiresAt.Time
		}

		// Only set string preview for text-based types
		if strings.HasPrefix(clip.ContentType, "text/") || clip.ContentType == "application/json" {
			clip.Preview = string(previewData)
		} else {
			clip.Preview = ""
		}

		clip.Tags = []Tag{} // Initialize empty, will be filled by batch query
		clips = append(clips, clip)
		clipIDs = append(clipIDs, clip.ID)
	}

	// Batch load tags for all clips (fixes N+1 query problem)
	if len(clipIDs) > 0 {
		tagsByClipID, err := a.getTagsForClips(clipIDs)
		if err != nil {
			log.Printf("Warning: failed to batch load clip tags: %v", err)
		} else {
			for i := range clips {
				if tags, ok := tagsByClipID[clips[i].ID]; ok {
					clips[i].Tags = tags
				}
			}
		}
	}

	if clips == nil {
		clips = []ClipPreview{}
	}
	return clips, nil
}

// getTagsForClips batch loads tags for multiple clips in a single query
func (a *App) getTagsForClips(clipIDs []int64) (map[int64][]Tag, error) {
	if len(clipIDs) == 0 {
		return map[int64][]Tag{}, nil
	}

	placeholders := make([]string, len(clipIDs))
	args := make([]interface{}, len(clipIDs))
	for i, id := range clipIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT ct.clip_id, t.id, t.name, t.color
		FROM clip_tags ct
		INNER JOIN tags t ON ct.tag_id = t.id
		WHERE ct.clip_id IN (%s)
		ORDER BY ct.clip_id, t.name
	`, strings.Join(placeholders, ","))

	rows, err := a.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to batch query clip tags: %w", err)
	}
	defer rows.Close()

	result := make(map[int64][]Tag)
	for rows.Next() {
		var clipID int64
		var tag Tag
		if err := rows.Scan(&clipID, &tag.ID, &tag.Name, &tag.Color); err != nil {
			log.Printf("Failed to scan batch clip tag: %v", err)
			continue
		}
		result[clipID] = append(result[clipID], tag)
	}

	return result, nil
}

// GetClipData retrieves full clip data by ID
func (a *App) GetClipData(id int64) (*ClipData, error) {
	var contentType string
	var data []byte
	var filename sql.NullString

	row := a.db.QueryRow("SELECT content_type, data, filename FROM clips WHERE id = ?", id)
	if err := row.Scan(&contentType, &data, &filename); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("clip not found")
		}
		return nil, fmt.Errorf("failed to get clip: %w", err)
	}

	clip := &ClipData{
		ID:           id,
		ContentType:  contentType,
		Filename:     filename.String,
		ValidUTF8:    utf8.Valid(data),
		DataEncoding: "base64",
	}

	// Invalid UTF-8 always crosses the bridge as base64, whatever the content
	// type. string(data) replacement-decodes, and once that has happened no
	// amount of frontend work can recover the original bytes — the frontend can
	// only forward what it was given. The guard used to be Markdown-specific,
	// which meant a text/plain clip containing byte FF arrived already destroyed.
	//
	// Valid text-ish content still crosses as a plain string, and everything
	// else as base64. An extension-classified application type therefore arrives
	// as base64 even when it is perfectly good UTF-8, which is why the frontend
	// text decoder handles both encodings after classification rather than
	// inferring text from the content type.
	if !clip.ValidUTF8 {
		clip.Data = base64.StdEncoding.EncodeToString(data)
	} else if strings.HasPrefix(contentType, "text/") || contentType == "application/json" {
		clip.Data = string(data)
		clip.DataEncoding = "utf8"
	} else {
		clip.Data = base64.StdEncoding.EncodeToString(data)
	}

	return clip, nil
}

// UploadFileAndGetID uploads a single file and returns the clip ID
func (a *App) UploadFileAndGetID(file FileData) (int64, error) {
	// Decode base64 data
	data, err := base64.StdEncoding.DecodeString(file.Data)
	if err != nil {
		return 0, fmt.Errorf("failed to decode base64 data: %w", err)
	}

	contentType := cliptype.PromoteMarkdown(file.Name, file.ContentType)

	// Special handling for text
	if contentType == "text/plain" || contentType == "" {
		textData := string(data)
		trimmedText := strings.TrimSpace(textData)

		if strings.HasPrefix(trimmedText, "<!DOCTYPE html") {
			contentType = "text/html"
		} else if isJSON(trimmedText) {
			contentType = "application/json"
		} else {
			contentType = "text/plain"
		}
	}

	contentHash := computeContentHash(data)
	result, err := a.db.Exec("INSERT INTO clips (content_type, data, filename, content_hash) VALUES (?, ?, ?, ?)",
		contentType, data, file.Name, contentHash)
	if err != nil {
		return 0, fmt.Errorf("failed to insert into db: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get inserted ID: %w", err)
	}

	var dupCount int
	a.db.QueryRow("SELECT COUNT(*) FROM clips WHERE content_hash = ? AND id != ?", contentHash, id).Scan(&dupCount)
	if dupCount > 0 {
		a.emitEvent("clip:duplicate", map[string]interface{}{
			"id":    id,
			"count": dupCount,
		})
	}

	// NOTE: share hook deliberately omitted here — callers tag the clip via
	// AddTagToClip, which fires OnClipCreated once with the canonical tag
	// set. See the matching note in UploadFiles.

	return id, nil
}

// UploadFiles handles file uploads
// expiryTimestamp converts a relative minutes-from-now expiry into canonical UTC
// text ("YYYY-MM-DD HH:MM:SS", the same form SQLite's CURRENT_TIMESTAMP uses) so
// expires_at comparisons are correct regardless of the server's local timezone.
// Binding a raw time.Time makes the driver serialize a local-zone Go String()
// value (with a monotonic-clock suffix) that does not compare correctly against
// CURRENT_TIMESTAMP. Returns nil when minutes <= 0 (no expiry).
func expiryTimestamp(minutes int) *string {
	if minutes <= 0 {
		return nil
	}
	s := time.Now().UTC().Add(time.Duration(minutes) * time.Minute).Format("2006-01-02 15:04:05")
	return &s
}

func (a *App) UploadFiles(files []FileData, expirationMinutes int, autoTagID int64) error {
	expiresAt := expiryTimestamp(expirationMinutes)

	for _, file := range files {
		// Decode base64 data
		data, err := base64.StdEncoding.DecodeString(file.Data)
		if err != nil {
			log.Printf("Failed to decode base64 data for file %s: %v", file.Name, err)
			continue
		}

		contentType := cliptype.PromoteMarkdown(file.Name, file.ContentType)

		// Special handling for text
		if contentType == "text/plain" || contentType == "" {
			textData := string(data)
			trimmedText := strings.TrimSpace(textData)

			if strings.HasPrefix(trimmedText, "<!DOCTYPE html") {
				contentType = "text/html"
			} else if isJSON(trimmedText) {
				contentType = "application/json"
			} else {
				contentType = "text/plain"
			}
		}

		contentHash := computeContentHash(data)
		result, err := a.db.Exec("INSERT INTO clips (content_type, data, filename, expires_at, content_hash) VALUES (?, ?, ?, ?, ?)",
			contentType, data, file.Name, expiresAt, contentHash)
		if err != nil {
			log.Printf("Failed to insert into db: %v\n", err)
			continue
		}

		clipID, _ := result.LastInsertId()

		// Auto-tag with folder tag if specified
		if autoTagID > 0 {
			if err := a.AddTagToClip(clipID, autoTagID); err != nil {
				log.Printf("Failed to auto-tag clip %d with tag %d: %v", clipID, autoTagID, err)
			}
		}

		// Emit plugin event
		if a.pluginManager != nil {
			a.pluginManager.EmitEvent("clip:created", map[string]interface{}{
				"id":           clipID,
				"content_type": contentType,
				"filename":     file.Name,
			})
		}

		var dupCount int
		a.db.QueryRow("SELECT COUNT(*) FROM clips WHERE content_hash = ? AND id != ?", contentHash, clipID).Scan(&dupCount)
		if dupCount > 0 {
			a.emitEvent("clip:duplicate", map[string]interface{}{
				"id":    clipID,
				"count": dupCount,
			})
		}

		// NOTE: the share hook intentionally does NOT fire here. Any upload
		// that needs to be published routes through AddTagToClip (including
		// the autoTagID branch above), which owns the OnClipCreated hook.
		// Firing it here too produced a double-publish — the follower
		// received the same clip twice with different envelope seqs.
	}

	return nil
}

// UpdateClipData replaces the content of an existing clip in place,
// recalculating the content hash.
func (a *App) UpdateClipData(id int64, contentType string, base64Data string, filename string) error {
	data, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return fmt.Errorf("failed to decode base64 data: %w", err)
	}

	contentType = cliptype.PromoteMarkdown(filename, contentType)
	contentHash := computeContentHash(data)
	result, err := a.db.Exec(
		"UPDATE clips SET data = ?, content_type = ?, filename = ?, content_hash = ? WHERE id = ?",
		data, contentType, filename, contentHash, id,
	)
	if err != nil {
		return fmt.Errorf("failed to update clip: %w", err)
	}

	// A zero-row UPDATE is not success. If the clip expired or was deleted through
	// the API, the CLI, or another window while the editor had it open, this call
	// would otherwise return nil — and the editor takes that as "saved", clears the
	// draft, and closes, destroying the only copy of the user's edit.
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to confirm clip update: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("clip not found")
	}

	// The temp file for this clip now holds the previous revision. Its name is
	// derived from the clip ID, so anything that reuses a leased file — media
	// playback looks one up before materializing — would serve the old bytes
	// for the rest of the lease. Drop it and let the next request rebuild it.
	if err := a.deleteTempFilesForClipIDs([]int64{id}); err != nil {
		log.Printf("Warning: failed to drop temp file for updated clip %d: %v", id, err)
	}

	return nil
}

// ClipMatch represents a clip matching a filename search.
type ClipMatch struct {
	ID          int64  `json:"id"`
	Filename    string `json:"filename"`
	ContentHash string `json:"content_hash"`
}

// FindClipsByFilenameAndTag returns clips matching any of the given filenames
// within a specific tag. When tagID is 0, matches untagged clips only.
func (a *App) FindClipsByFilenameAndTag(filenames []string, tagID int64) ([]ClipMatch, error) {
	if len(filenames) == 0 {
		return nil, nil
	}

	// Build placeholder string for IN clause
	placeholders := make([]string, len(filenames))
	args := make([]interface{}, len(filenames))
	for i, fn := range filenames {
		placeholders[i] = "?"
		args[i] = fn
	}
	inClause := strings.Join(placeholders, ", ")

	var query string
	if tagID > 0 {
		query = fmt.Sprintf(`
			SELECT c.id, c.filename, c.content_hash
			FROM clips c
			JOIN clip_tags ct ON c.id = ct.clip_id
			WHERE ct.tag_id = ? AND c.filename IN (%s)
			AND c.is_archived = 0
			ORDER BY c.id DESC`, inClause)
		args = append([]interface{}{tagID}, args...)
	} else {
		query = fmt.Sprintf(`
			SELECT c.id, c.filename, c.content_hash
			FROM clips c
			WHERE c.filename IN (%s)
			AND c.is_archived = 0
			AND c.id NOT IN (SELECT clip_id FROM clip_tags)
			ORDER BY c.id DESC`, inClause)
	}

	rows, err := a.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to find clips by filename: %w", err)
	}
	defer rows.Close()

	var matches []ClipMatch
	for rows.Next() {
		var m ClipMatch
		if err := rows.Scan(&m.ID, &m.Filename, &m.ContentHash); err != nil {
			return nil, fmt.Errorf("failed to scan clip match: %w", err)
		}
		matches = append(matches, m)
	}
	return matches, nil
}

// RenameClip changes only the filename of a clip.
func (a *App) RenameClip(id int64, newFilename string) error {
	newFilename = strings.TrimSpace(newFilename)
	if newFilename == "" {
		return fmt.Errorf("filename cannot be empty")
	}
	if strings.ContainsAny(newFilename, "/\\") {
		return fmt.Errorf("filename contains path separator")
	}
	if strings.Contains(newFilename, "..") {
		return fmt.Errorf("filename contains path traversal")
	}
	if strings.ContainsRune(newFilename, 0) {
		return fmt.Errorf("filename contains null byte")
	}

	result, err := a.db.Exec(`
		UPDATE clips
		SET filename = ?,
			content_type = CASE
				WHEN LOWER(?) LIKE '%.md' OR LOWER(?) LIKE '%.markdown' THEN ?
				ELSE content_type
			END
		WHERE id = ?`,
		newFilename, newFilename, newFilename, cliptype.MarkdownContentType, id)
	if err != nil {
		return fmt.Errorf("failed to rename clip: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("clip not found")
	}

	a.emitPluginEvent("clip:renamed", map[string]interface{}{
		"id":       id,
		"filename": newFilename,
	})
	return nil
}

// DeleteClip deletes a clip by ID
func (a *App) DeleteClip(id int64) error {
	// Get tag IDs before deleting (to clean up orphaned tags)
	rows, err := a.db.Query("SELECT tag_id FROM clip_tags WHERE clip_id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to query clip tags: %w", err)
	}
	var tagIDs []int64
	for rows.Next() {
		var tagID int64
		if err := rows.Scan(&tagID); err == nil {
			tagIDs = append(tagIDs, tagID)
		}
	}
	rows.Close()

	// Explicitly delete clip_tags (don't rely on CASCADE)
	_, err = a.db.Exec("DELETE FROM clip_tags WHERE clip_id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete clip tags: %w", err)
	}

	// Delete the clip
	_, err = a.db.Exec("DELETE FROM clips WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete clip: %w", err)
	}
	if err := a.deleteTempFilesForClipIDs([]int64{id}); err != nil {
		log.Printf("Warning: failed to clean temp file for clip %d: %v", id, err)
	}

	// Clean up orphaned tags
	for _, tagID := range tagIDs {
		a.deleteTagIfOrphaned(tagID)
	}

	// Emit plugin event
	if a.pluginManager != nil {
		a.pluginManager.EmitEvent("clip:deleted", id)
	}
	return nil
}

// MergeDuplicates keeps the oldest clip with the given content_hash,
// merges tags from all duplicates onto it, deletes the duplicates,
// and updates the survivor's created_at to now.
func (a *App) MergeDuplicates(clipID int64) error {
	// Get the content_hash for this clip
	var contentHash string
	err := a.db.QueryRow("SELECT content_hash FROM clips WHERE id = ?", clipID).Scan(&contentHash)
	if err != nil {
		return fmt.Errorf("failed to get clip hash: %w", err)
	}
	if contentHash == "" {
		return fmt.Errorf("clip has no content hash")
	}

	// Find all clips with same hash, ordered by id (oldest first)
	rows, err := a.db.Query("SELECT id FROM clips WHERE content_hash = ? ORDER BY id ASC", contentHash)
	if err != nil {
		return fmt.Errorf("failed to find duplicates: %w", err)
	}
	var allIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			allIDs = append(allIDs, id)
		}
	}
	rows.Close()

	if len(allIDs) < 2 {
		return nil // No duplicates
	}

	survivorID := allIDs[0] // Oldest
	duplicateIDs := allIDs[1:]

	tx, err := a.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Merge tags from all duplicates to survivor
	for _, dupID := range duplicateIDs {
		_, err := tx.Exec(`
			INSERT OR IGNORE INTO clip_tags (clip_id, tag_id)
			SELECT ?, tag_id FROM clip_tags WHERE clip_id = ?
		`, survivorID, dupID)
		if err != nil {
			return fmt.Errorf("failed to merge tags from clip %d: %w", dupID, err)
		}
	}

	// Enforce tree exclusivity on the survivor's merged tags.
	// The INSERT OR IGNORE above may have added tags from the same tree
	// that the survivor already had a tag in.
	{
		rows, err := tx.Query(`
			SELECT ct.tag_id, t.name FROM clip_tags ct
			INNER JOIN tags t ON ct.tag_id = t.id
			WHERE ct.clip_id = ?
			ORDER BY ct.rowid ASC`, survivorID)
		if err != nil {
			return fmt.Errorf("failed to query survivor tags: %w", err)
		}
		seenRoots := map[string]bool{}
		var removeTagIDs []int64
		for rows.Next() {
			var tagID int64
			var tagName string
			if err := rows.Scan(&tagID, &tagName); err != nil {
				rows.Close()
				return fmt.Errorf("failed to scan survivor tag: %w", err)
			}
			root := getRootTagName(tagName)
			if seenRoots[root] {
				removeTagIDs = append(removeTagIDs, tagID)
			} else {
				seenRoots[root] = true
			}
		}
		rows.Close()
		for _, tagID := range removeTagIDs {
			tx.Exec("DELETE FROM clip_tags WHERE clip_id = ? AND tag_id = ?", survivorID, tagID)
		}
	}

	// Delete clip_tags for duplicates
	for _, dupID := range duplicateIDs {
		tx.Exec("DELETE FROM clip_tags WHERE clip_id = ?", dupID)
	}

	// Delete duplicate clips
	for _, dupID := range duplicateIDs {
		tx.Exec("DELETE FROM clips WHERE id = ?", dupID)
	}

	// Update survivor's created_at to now (moves to top)
	tx.Exec("UPDATE clips SET created_at = CURRENT_TIMESTAMP WHERE id = ?", survivorID)

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit merge: %w", err)
	}

	// Clean temp files for deleted clips
	if err := a.deleteTempFilesForClipIDs(duplicateIDs); err != nil {
		log.Printf("Warning: failed to clean temp files for merged clips: %v", err)
	}

	// Emit plugin events
	if a.pluginManager != nil {
		for _, dupID := range duplicateIDs {
			a.pluginManager.EmitEvent("clip:deleted", dupID)
		}
	}

	return nil
}

// GetDuplicateGroups returns all groups of clips that share the same content hash
func (a *App) GetDuplicateGroups() ([]DuplicateGroup, error) {
	rows, err := a.db.Query(`
		SELECT content_hash, MIN(filename) as filename, MIN(content_type) as content_type, COUNT(*) as cnt, MIN(id) as oldest_id
		FROM clips
		WHERE content_hash != ''
		GROUP BY content_hash
		HAVING cnt > 1
		ORDER BY cnt DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query duplicate groups: %w", err)
	}
	defer rows.Close()

	var groups []DuplicateGroup
	for rows.Next() {
		var g DuplicateGroup
		var filename sql.NullString
		if err := rows.Scan(&g.ContentHash, &filename, &g.ContentType, &g.Count, &g.OldestID); err != nil {
			log.Printf("Warning: failed to scan duplicate group: %v", err)
			continue
		}
		g.Filename = filename.String
		groups = append(groups, g)
	}
	if groups == nil {
		groups = []DuplicateGroup{}
	}
	return groups, nil
}

// DeduplicateAll merges all duplicate groups, keeping the oldest clip in each group
func (a *App) DeduplicateAll() (int, error) {
	groups, err := a.GetDuplicateGroups()
	if err != nil {
		return 0, err
	}

	totalRemoved := 0
	for _, group := range groups {
		err := a.MergeDuplicates(group.OldestID)
		if err != nil {
			log.Printf("Warning: failed to merge group %s: %v", group.ContentHash, err)
			continue
		}
		totalRemoved += group.Count - 1
	}

	return totalRemoved, nil
}

// ToggleArchive toggles the archived status of a clip
func (a *App) ToggleArchive(id int64) error {
	_, err := a.db.Exec("UPDATE clips SET is_archived = NOT is_archived WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to toggle archive: %w", err)
	}

	// Emit plugin event
	if a.pluginManager != nil {
		// Get current archived state after toggle
		var isArchived int
		a.db.QueryRow("SELECT is_archived FROM clips WHERE id = ?", id).Scan(&isArchived)
		if isArchived == 1 {
			a.pluginManager.EmitEvent("clip:archived", map[string]interface{}{
				"id": id,
			})
		} else {
			a.pluginManager.EmitEvent("clip:unarchived", map[string]interface{}{
				"id": id,
			})
		}
	}

	return nil
}

// CancelExpiration removes the expiration for a clip
func (a *App) CancelExpiration(id int64) error {
	_, err := a.db.Exec("UPDATE clips SET expires_at = NULL WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to cancel expiration: %w", err)
	}
	return nil
}

// SetExpiration sets the expiration for a clip
func (a *App) SetExpiration(id int64, minutes int) error {
	if minutes <= 0 {
		return fmt.Errorf("expiration minutes must be positive")
	}
	expiresAt := expiryTimestamp(minutes)
	_, err := a.db.Exec("UPDATE clips SET expires_at = ? WHERE id = ?", expiresAt, id)
	if err != nil {
		return fmt.Errorf("failed to set expiration: %w", err)
	}
	return nil
}

// --- Tag Methods ---

// CreateTag creates a new tag with auto-assigned color
func (a *App) CreateTag(name string) (*Tag, error) {
	var err error
	name, err = validateTagName(name)
	if err != nil {
		return nil, err
	}

	// Use transaction to prevent race condition in color assignment
	tx, err := a.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Auto-create any missing ancestor tags (e.g. "work" and "work/client1"
	// for "work/client1/projectABC").
	ancestors := getAncestorTagNames(name)
	var createdAncestors []Tag
	for _, ancestor := range ancestors {
		// Check if ancestor already exists
		var exists bool
		if err := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM tags WHERE name = ?)", ancestor).Scan(&exists); err != nil {
			return nil, fmt.Errorf("failed to check ancestor tag %q: %w", ancestor, err)
		}
		if exists {
			continue
		}

		// Determine color: inherit from nearest existing ancestor, or use palette rotation
		color := a.pickColorForTag(tx, ancestor)

		result, err := tx.Exec("INSERT INTO tags (name, color) VALUES (?, ?)", ancestor, color)
		if err != nil {
			// Race: someone else created it between our check and insert
			if strings.Contains(err.Error(), "UNIQUE") {
				continue
			}
			return nil, fmt.Errorf("failed to create ancestor tag %q: %w", ancestor, err)
		}
		id, _ := result.LastInsertId()
		createdAncestors = append(createdAncestors, Tag{ID: id, Name: ancestor, Color: color, Count: 0})
	}

	// Now create the requested tag itself
	color := a.pickColorForTag(tx, name)

	result, err := tx.Exec("INSERT INTO tags (name, color) VALUES (?, ?)", name, color)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, fmt.Errorf("tag already exists: %s", name)
		}
		return nil, fmt.Errorf("failed to create tag: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get tag ID: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Emit events for auto-created ancestors first, then for the requested tag
	for _, anc := range createdAncestors {
		a.emitPluginEvent("tag:created", map[string]interface{}{
			"id":    anc.ID,
			"name":  anc.Name,
			"color": anc.Color,
		})
	}
	a.emitPluginEvent("tag:created", map[string]interface{}{
		"id":    id,
		"name":  name,
		"color": color,
	})

	return &Tag{
		ID:    id,
		Name:  name,
		Color: color,
		Count: 0,
	}, nil
}

// pickColorForTag determines the color for a new tag. It walks up the
// ancestor chain looking for an existing tag whose color can be inherited.
// If no ancestor exists, it falls back to palette rotation based on current
// tag count.  The lookup uses the provided transaction.
func (a *App) pickColorForTag(tx *sql.Tx, name string) string {
	// Walk ancestors from nearest to root, looking for an existing tag color
	parent := getParentTagName(name)
	for parent != "" {
		var color string
		err := tx.QueryRow("SELECT color FROM tags WHERE name = ?", parent).Scan(&color)
		if err == nil {
			return color
		}
		parent = getParentTagName(parent)
	}

	// No ancestor found — use palette rotation
	var count int
	if err := tx.QueryRow("SELECT COUNT(*) FROM tags").Scan(&count); err != nil {
		// Fallback to first color on error
		return tagColors[0]
	}
	return tagColors[count%len(tagColors)]
}

// UpdateTag updates a tag's name and/or color
func (a *App) UpdateTag(id int64, name, color string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("tag name cannot be empty")
	}
	if len(name) > maxTagNameLength {
		return fmt.Errorf("tag name too long (max %d characters)", maxTagNameLength)
	}

	// Reserve "_api" as a path segment — used by tag serve JSON API.
	// Also reject empty path segments (leading/trailing/consecutive slashes).
	for _, seg := range strings.Split(name, "/") {
		if strings.TrimSpace(seg) == "" {
			return fmt.Errorf("tag name contains empty path segment")
		}
		if seg == "_api" {
			return fmt.Errorf("tag name contains reserved segment '_api'")
		}
	}

	// Check if any tag in the subtree is currently served. ServeManager
	// caches tag names, so renames break the server's path resolution.
	var oldNameForCheck string
	if err := a.db.QueryRow(`SELECT name FROM tags WHERE id = ?`, id).Scan(&oldNameForCheck); err != nil {
		return fmt.Errorf("tag not found")
	}
	if oldNameForCheck != name {
		if served := a.tagIsServedInSubtree(oldNameForCheck); served != "" {
			return fmt.Errorf("cannot rename: tag %q in this subtree is currently served. Stop the server first.", served)
		}
	}

	tx, err := a.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Fetch the old name so we can cascade rename descendants
	var oldName string
	err = tx.QueryRow("SELECT name FROM tags WHERE id = ?", id).Scan(&oldName)
	if err != nil {
		return fmt.Errorf("tag not found")
	}

	_, err = tx.Exec("UPDATE tags SET name = ?, color = ? WHERE id = ?", name, color, id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return fmt.Errorf("tag name already exists: %s", name)
		}
		return fmt.Errorf("failed to update tag: %w", err)
	}

	// After updating the tag itself, cascade rename descendants
	if oldName != name {
		oldPrefix := oldName + "/"
		newPrefix := name + "/"
		_, err = tx.Exec(`UPDATE tags SET name = ? || SUBSTR(name, ?) WHERE name LIKE ?`,
			newPrefix, utf8.RuneCountInString(oldPrefix)+1, oldPrefix+"%")
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return fmt.Errorf("tag rename conflicts with an existing tag")
			}
			return fmt.Errorf("failed to rename descendant tags: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit tag update: %w", err)
	}

	// Emit plugin event
	a.emitPluginEvent("tag:updated", map[string]interface{}{
		"id":    id,
		"name":  name,
		"color": color,
	})

	// Emit Wails runtime event so the frontend can re-resolve folder-view state.
	a.emitEvent("tag:updated", map[string]any{
		"id":       id,
		"old_name": oldName,
		"new_name": name,
	})

	return nil
}

// DeleteTag deletes a tag using a three-phase flow:
//  1. Preconditions check (read-only) — refuse if blocked (e.g., active follow).
//  2. SQL transaction — null watched_folders auto_tag_id, remove from hidden
//     list, delete the row. FK cascades handle clip_tags, api_keys (SET NULL
//     + trigger auto-revoke), shares (CASCADE).
//  3. Post-commit runtime cleanup — stop in-memory share publication + serve
//     server. Failures here are logged; the orphan-rows tool is the backstop.
func (a *App) DeleteTag(id int64) error {
	// Fetch name up front for event payloads and error messages.
	var name string
	if err := a.db.QueryRow(`SELECT name FROM tags WHERE id = ?`, id).Scan(&name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("tag not found")
		}
		return fmt.Errorf("lookup tag: %w", err)
	}

	// Phase 1: preconditions
	blockers, err := a.checkTagReferencePreconditions(id)
	if err != nil {
		return fmt.Errorf("check preconditions: %w", err)
	}
	if len(blockers) > 0 {
		return fmt.Errorf("cannot delete tag: %s", blockers[0])
	}

	// Phase 2: SQL transaction
	tx, err := a.db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`UPDATE watched_folders SET auto_tag_id = NULL WHERE auto_tag_id = ?`, id); err != nil {
		if !isSQLiteNoSuchTable(err) {
			return fmt.Errorf("null auto_tag_id: %w", err)
		}
		// Missing table: no watch folders to update (tolerate minimal test schemas).
	}

	// Update hidden list inside the transaction (tx-aware read + write so
	// nothing escapes the snapshot).
	hiddenIDs, herr := getHiddenTagsTx(tx)
	if herr != nil {
		return fmt.Errorf("get hidden tags: %w", herr)
	}
	filtered := make([]int64, 0, len(hiddenIDs))
	for _, h := range hiddenIDs {
		if h != id {
			filtered = append(filtered, h)
		}
	}
	if len(filtered) != len(hiddenIDs) {
		if err := setHiddenTagsTx(tx, filtered); err != nil {
			return fmt.Errorf("update hidden_tags: %w", err)
		}
	}

	if _, err := tx.Exec(`DELETE FROM tags WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete tag row: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	// Phase 3: post-commit runtime cleanup (best-effort, guarded).
	//
	// StopShare is no-op-safe after Task 4's Step 3a fix: it only emits
	// the event when there was an actual publication to stop.
	// StopServing errors on a non-served tag, so guard with IsServing.
	if a.shareManager != nil {
		if err := a.shareManager.StopShare(id); err != nil {
			log.Printf("DeleteTag: StopShare(%d) failed (best-effort): %v", id, err)
		}
	}
	if a.serveManager != nil && a.serveManager.IsServing(id) {
		if err := a.serveManager.StopServing(id); err != nil {
			log.Printf("DeleteTag: StopServing(%d) failed (best-effort): %v", id, err)
		}
	}

	// Emit plugin event (unchanged name, existing handler)
	if a.pluginManager != nil {
		a.pluginManager.EmitEvent("tag:deleted", id)
	}
	// Emit Wails runtime event so the frontend can re-resolve folder view.
	a.emitEvent("tag:deleted", map[string]any{"id": id, "name": name})

	return nil
}

// MergeTagPreview summarizes the impact of a proposed merge.
type MergeTagPreview struct {
	ClipCount       int      `json:"clip_count"`
	DescendantCount int      `json:"descendant_count"`
	Blockers        []string `json:"blockers"`
	SourceName      string   `json:"source_name"`
	DestName        string   `json:"dest_name"`
}

// PreviewMergeTag returns counts + blockers without mutating anything.
func (a *App) PreviewMergeTag(sourceID, destID int64) (MergeTagPreview, error) {
	var out MergeTagPreview

	var srcName, dstName string
	if err := a.db.QueryRow(`SELECT name FROM tags WHERE id = ?`, sourceID).Scan(&srcName); err != nil {
		return out, fmt.Errorf("source tag not found: %w", err)
	}
	if err := a.db.QueryRow(`SELECT name FROM tags WHERE id = ?`, destID).Scan(&dstName); err != nil {
		return out, fmt.Errorf("destination tag not found: %w", err)
	}
	out.SourceName = srcName
	out.DestName = dstName

	out.Blockers = a.checkMergeTagPreconditions(sourceID, destID, srcName, dstName)

	if err := a.db.QueryRow(
		`SELECT COUNT(*) FROM clip_tags WHERE tag_id = ?`, sourceID,
	).Scan(&out.ClipCount); err != nil {
		return out, fmt.Errorf("count clips: %w", err)
	}

	// Descendants: tags whose name starts with "{srcName}/".
	if err := a.db.QueryRow(
		`SELECT COUNT(*) FROM tags WHERE name LIKE ? || '/%'`, srcName,
	).Scan(&out.DescendantCount); err != nil {
		return out, fmt.Errorf("count descendants: %w", err)
	}

	return out, nil
}

// MergeTag folds source into destination in a single transaction:
//  1. Preconditions — refuses if any blocker (see checkMergeTagPreconditions).
//  2. Reassigns all clips from source to destination with same-tree
//     exclusivity preserved (three-step SQL sequence below).
//  3. Renames every descendant of source with the prefix swap.
//  4. Migrates non-networked ID references (api_keys, watched_folders,
//     hidden list) via migrateTagReferences.
//  5. Deletes the source tag row.
//
// Emits tag:merged (runtime + plugin) on success.
func (a *App) MergeTag(sourceID, destID int64) error {
	var srcName, dstName string
	if err := a.db.QueryRow(`SELECT name FROM tags WHERE id = ?`, sourceID).Scan(&srcName); err != nil {
		return fmt.Errorf("source tag not found")
	}
	if err := a.db.QueryRow(`SELECT name FROM tags WHERE id = ?`, destID).Scan(&dstName); err != nil {
		return fmt.Errorf("destination tag not found")
	}

	blockers := a.checkMergeTagPreconditions(sourceID, destID, srcName, dstName)
	if len(blockers) > 0 {
		return fmt.Errorf("cannot merge: %s", blockers[0])
	}

	// Destination root = first path segment.
	destRoot := dstName
	if idx := strings.Index(dstName, "/"); idx >= 0 {
		destRoot = dstName[:idx]
	}

	tx, err := a.db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	// Reassignment enforces same-tree exclusivity (matches AddTagToClip /
	// removeSameTreeTags). Order matters: insert destination first, then
	// clean sibling tags in dest's root tree anchored on SOURCE (so
	// unrelated clips that happen to hold destination stay untouched),
	// then delete any leftover source rows.
	//
	// (2a) Insert destination for every clip that currently has source.
	if _, err := tx.Exec(`INSERT OR IGNORE INTO clip_tags(clip_id, tag_id)
		SELECT clip_id, ? FROM clip_tags WHERE tag_id = ?`, destID, sourceID); err != nil {
		return fmt.Errorf("reassign clip_tags: %w", err)
	}

	// (2b) Same-tree cleanup: for every clip THAT HAD SOURCE, remove any
	// other tag under destination's root tree (keep destination only).
	// Anchoring on source (not destination) ensures we don't accidentally
	// rewrite an unrelated clip that already happened to carry destination.
	if _, err := tx.Exec(`DELETE FROM clip_tags
		WHERE clip_id IN (SELECT clip_id FROM clip_tags WHERE tag_id = ?)
		  AND tag_id IN (
		    SELECT id FROM tags WHERE name = ? OR name LIKE ? || '/%'
		  )
		  AND tag_id != ?`, sourceID, destRoot, destRoot, destID); err != nil {
		return fmt.Errorf("same-tree cleanup: %w", err)
	}

	// (2c) Delete remaining source clip_tags rows (catches cross-root case;
	// no-op when same-root case already wiped them via 2b).
	if _, err := tx.Exec(`DELETE FROM clip_tags WHERE tag_id = ?`, sourceID); err != nil {
		return fmt.Errorf("delete old clip_tags: %w", err)
	}

	// (3) Rename descendants with prefix swap. Reuses the same SQL as
	// UpdateTag's cascade rename.
	oldPrefix := srcName + "/"
	newPrefix := dstName + "/"
	if _, err := tx.Exec(`UPDATE tags SET name = ? || SUBSTR(name, ?) WHERE name LIKE ?`,
		newPrefix, utf8.RuneCountInString(oldPrefix)+1, oldPrefix+"%"); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return fmt.Errorf("merge would create duplicate tag path")
		}
		return fmt.Errorf("rename descendants: %w", err)
	}

	// (4) Migrate non-networked references.
	if err := a.migrateTagReferences(tx, sourceID, destID); err != nil {
		return fmt.Errorf("migrate references: %w", err)
	}

	// (5) Delete the source row.
	if _, err := tx.Exec(`DELETE FROM tags WHERE id = ?`, sourceID); err != nil {
		return fmt.Errorf("delete source: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	// Emit events.
	if a.pluginManager != nil {
		a.pluginManager.EmitEvent("tag:merged", map[string]interface{}{
			"source_id": sourceID, "dest_id": destID,
			"source_name": srcName, "dest_name": dstName,
		})
	}
	a.emitEvent("tag:merged", map[string]any{
		"source_id": sourceID, "dest_id": destID,
		"source_name": srcName, "dest_name": dstName,
	})

	return nil
}

// candidateEmptyTagsQuery selects tag ids/names that currently have no clips
// AND no descendant tags (path-based hierarchy via "/" prefix match).
const candidateEmptyTagsQuery = `
	SELECT t.id, t.name, t.color
	FROM tags t
	LEFT JOIN clip_tags ct ON ct.tag_id = t.id
	WHERE NOT EXISTS (
		SELECT 1 FROM tags children
		WHERE children.name LIKE t.name || '/%'
	)
	GROUP BY t.id
	HAVING COUNT(ct.clip_id) = 0
	ORDER BY t.name
`

// removeEmptyTagsMaxPasses caps the iterative cascade — real depth is bounded
// by the tag tree height; the cap only exists to stop pathological loops.
const removeEmptyTagsMaxPasses = 1024

// GetRemovableEmptyTags returns the full list of tags that would be deleted
// by a call to RemoveEmptyTags, in the order they would be removed.
//
// The iterative cascade (a parent that becomes childless after its
// leaf-children are pruned) is simulated inside a transaction that is rolled
// back before returning, so this call is side-effect free and its result
// exactly matches what RemoveEmptyTags will delete.
func (a *App) GetRemovableEmptyTags() ([]Tag, error) {
	tx, err := a.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin dry-run transaction: %w", err)
	}
	defer tx.Rollback()

	var out []Tag
	for pass := 0; pass < removeEmptyTagsMaxPasses; pass++ {
		rows, err := tx.Query(candidateEmptyTagsQuery)
		if err != nil {
			return nil, fmt.Errorf("failed to query empty tags: %w", err)
		}
		var pending []Tag
		for rows.Next() {
			var t Tag
			if err := rows.Scan(&t.ID, &t.Name, &t.Color); err != nil {
				rows.Close()
				return nil, err
			}
			pending = append(pending, t)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
		if len(pending) == 0 {
			return out, nil
		}
		for _, t := range pending {
			if _, err := tx.Exec("DELETE FROM tags WHERE id = ?", t.ID); err != nil {
				return nil, fmt.Errorf("failed to simulate delete for tag %d: %w", t.ID, err)
			}
			out = append(out, t)
		}
	}
	return out, fmt.Errorf("remove empty tags exceeded %d passes", removeEmptyTagsMaxPasses)
}

// RemoveEmptyTags deletes tags that have no clips and no descendant tags.
// Iterates until stable, so parents that become childless after their
// leaf-children are pruned are also removed in the same pass.
//
// Returns the total number of tags deleted. Use GetRemovableEmptyTags for a
// side-effect-free preview of what this method will remove.
func (a *App) RemoveEmptyTags() (int, error) {
	total := 0
	for pass := 0; pass < removeEmptyTagsMaxPasses; pass++ {
		rows, err := a.db.Query(candidateEmptyTagsQuery)
		if err != nil {
			return total, fmt.Errorf("failed to query empty tags: %w", err)
		}
		var ids []int64
		for rows.Next() {
			var id int64
			var name, color string
			if err := rows.Scan(&id, &name, &color); err != nil {
				rows.Close()
				return total, err
			}
			ids = append(ids, id)
		}
		if err := rows.Close(); err != nil {
			return total, err
		}
		if len(ids) == 0 {
			return total, nil
		}
		// Call DeleteTag so share cleanup and plugin events fire.
		for _, id := range ids {
			if err := a.DeleteTag(id); err != nil {
				return total, fmt.Errorf("failed to delete tag %d: %w", id, err)
			}
			total++
		}
	}
	return total, fmt.Errorf("remove empty tags exceeded %d passes", removeEmptyTagsMaxPasses)
}

// GetTags retrieves all tags with usage counts
func (a *App) GetTags() ([]Tag, error) {
	rows, err := a.db.Query(`
		SELECT t.id, t.name, t.color, COUNT(ct.clip_id) as count
		FROM tags t
		LEFT JOIN clip_tags ct ON t.id = ct.tag_id
		GROUP BY t.id
		ORDER BY t.name
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query tags: %w", err)
	}
	defer rows.Close()

	var tags []Tag
	for rows.Next() {
		var tag Tag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Color, &tag.Count); err != nil {
			log.Printf("Failed to scan tag: %v", err)
			continue
		}
		tags = append(tags, tag)
	}

	if tags == nil {
		tags = []Tag{}
	}
	return tags, nil
}

// AddTagToClip adds a tag to a clip
func (a *App) AddTagToClip(clipID, tagID int64) error {
	// Exclude restores for the whole span from tx.Begin through the hook
	// decision (see backupRestoreMu's doc): the mutation either completes
	// entirely before a restore starts or begins entirely after it ends, so it
	// can neither tag a restored row under a stale id nor commit into the
	// window where hooks are suspended and its publication would be silently
	// lost. Released before the plugin event — Lua tags.add_to_clip re-enters
	// this function, and a recursive RLock deadlocks the moment a restore's
	// Lock() queues between the two acquisitions.
	a.backupRestoreMu.RLock()
	restoreLockHeld := true
	releaseRestoreLock := func() {
		if restoreLockHeld {
			restoreLockHeld = false
			a.backupRestoreMu.RUnlock()
		}
	}
	defer releaseRestoreLock()

	// The exclusivity delete and the insert that replaces it are one unit: a
	// failure between them would leave the clip stripped of its old tag and
	// without the new one, or (when the delete failed and was ignored) holding
	// two tags from the same tree.
	tx, err := a.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Enforce tree exclusivity: a clip can only have one tag per root tree.
	// Adding a/b/d removes any existing tags under the same root (a, a/b, a/b/c, etc.)
	if err := removeSameTreeTags(tx, clipID, tagID); err != nil {
		return fmt.Errorf("failed to enforce tree exclusivity for clip %d tag %d: %w", clipID, tagID, err)
	}

	res, err := tx.Exec("INSERT OR IGNORE INTO clip_tags (clip_id, tag_id) VALUES (?, ?)", clipID, tagID)
	if err != nil {
		return fmt.Errorf("failed to add tag to clip: %w", err)
	}
	// OR IGNORE makes a re-add of an already-present tag a no-op, so
	// RowsAffected distinguishes a genuine new association from a repeat.
	// removeSameTreeTags above never deletes tagID itself, so a repeat stays
	// a repeat here.
	newlyTagged := false
	if n, rerr := res.RowsAffected(); rerr == nil && n > 0 {
		newlyTagged = true
	}

	// Take the publication admission BEFORE the commit, not after it. The
	// admission is what a restore's drain waits on, so anything outside it is a
	// window in which a restore can run to completion: committing first left
	// this call free to stall between the commit and the admission, watch a
	// full RestoreBackup drain a WaitGroup it had not joined, and then publish
	// clipID/tagID against the rebuilt manager — where those ids name restored
	// rows, i.e. entirely unrelated content fanned out into a restored share.
	// Admitting first pins any restore that starts after the commit until this
	// call's publication decision is finished.
	//
	// newlyTagged is already known here (RowsAffected ran inside the tx), so the
	// admission is taken only when there is something to publish; the no-op
	// re-add path never touches the WaitGroup at all.
	//
	// A refusal means shutdown closed the gate or a restore has it suspended.
	// The tag operation still commits — user data must never fail because
	// sharing is quiescing — and only the publication is skipped.
	admitted := newlyTagged && a.tryAddShareHook()

	// Everything below observes committed state: the plugin event announces a
	// durable association, and the share hook re-reads the clip from the DB.
	if err := tx.Commit(); err != nil {
		if admitted {
			a.shareHookWG.Done()
		}
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Share hook — if the tag JUST added matches an active publication, fan the
	// clip out to connected followers. This handles the case where a clip is
	// tagged into a shared folder after initial upload.
	//
	// Only the newly-added tag is passed, never the clip's whole tag set:
	// otherwise adding an unrelated tag to a clip that already carries a shared
	// tag would re-match that publication and re-publish the entire clip, which
	// followers ingest as a brand-new clip they cannot dedup.
	//
	// This runs before the plugin event, and the ordering is load-bearing now
	// that the admission spans the commit: EmitEvent runs Lua handlers
	// synchronously and can take arbitrarily long, and an admission held across
	// one could outlast a restore's bounded drain — which is exactly the
	// corruption above, re-entered through the timeout path. The publication
	// goroutine already ran concurrently with the handlers, so nothing that
	// observed the old order can tell the difference.
	//
	// spawnAdmittedShareHook consumes the admission on every path it takes.
	if admitted {
		a.spawnAdmittedShareHook(clipID, tagID, "AddTagToClip")
	} else if newlyTagged {
		log.Printf("share: publication hooks closed or suspended (shutdown/restore); suppressed hook for clip %d under tag %d (AddTagToClip)", clipID, tagID)
	}

	// The coherent span ends here: the tx committed and the hook decision is
	// made. Release before the plugin event (re-entrancy — see the RLock above).
	releaseRestoreLock()

	// Emit plugin event
	if a.pluginManager != nil {
		a.pluginManager.EmitEvent("tag:added_to_clip", map[string]interface{}{
			"tag_id":  tagID,
			"clip_id": clipID,
		})
	}

	return nil
}

// RemoveTagFromClip removes a tag from a clip
func (a *App) RemoveTagFromClip(clipID, tagID int64) error {
	_, err := a.db.Exec("DELETE FROM clip_tags WHERE clip_id = ? AND tag_id = ?", clipID, tagID)
	if err != nil {
		return fmt.Errorf("failed to remove tag from clip: %w", err)
	}

	// Emit plugin event
	if a.pluginManager != nil {
		a.pluginManager.EmitEvent("tag:removed_from_clip", map[string]interface{}{
			"tag_id":  tagID,
			"clip_id": clipID,
		})
	}

	// Clean up orphaned tag
	a.deleteTagIfOrphaned(tagID)
	return nil
}

// removeSameTreeTags removes any existing tags from the same root tree for a clip.
// This enforces the constraint that a clip can only occupy one position in a tag tree.
//
// It takes a handle rather than reading a.db directly so callers can run it in
// the same transaction as the insert that replaces the removed tags — otherwise
// a failure between the two strips a clip of its old tag without giving it the
// new one. A single DELETE also keeps it to one statement, which is what a
// *sql.Tx (one connection) can safely execute at a time.
func removeSameTreeTags(h sqlHandle, clipID, newTagID int64) error {
	// Look up the new tag's name to find its root
	var newTagName string
	if err := h.QueryRow("SELECT name FROM tags WHERE id = ?", newTagID).Scan(&newTagName); err != nil {
		return err
	}
	root := getRootTagName(newTagName)

	// Drop every tag on this clip under the same root tree, except the tag
	// being added — leaving that one alone is what keeps a repeat add a no-op.
	// Escape SQL LIKE wildcards in the root name so _ and % are treated literally.
	escapedRoot := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(root)
	_, err := h.Exec(`
		DELETE FROM clip_tags
		WHERE clip_id = ?
		  AND tag_id != ?
		  AND tag_id IN (
		    SELECT id FROM tags WHERE name = ? OR name LIKE ? ESCAPE '\'
		  )`,
		clipID, newTagID, root, escapedRoot+"/%")
	return err
}

// deleteTagIfOrphaned deletes a tag if it has no associated clips.
// Subtags (names containing '/') are never auto-deleted — they were
// intentionally created as part of a hierarchy.
func (a *App) deleteTagIfOrphaned(tagID int64) {
	var tagName string
	err := a.db.QueryRow("SELECT name FROM tags WHERE id = ?", tagID).Scan(&tagName)
	if err != nil {
		return
	}

	// Never auto-delete hierarchical tags (subtags or parents of subtags)
	if strings.Contains(tagName, "/") {
		return
	}

	var count int
	err = a.db.QueryRow("SELECT COUNT(*) FROM clip_tags WHERE tag_id = ?", tagID).Scan(&count)
	if err != nil || count > 0 {
		return
	}

	// Check if this tag has children — don't delete a parent with descendants
	var childCount int
	a.db.QueryRow("SELECT COUNT(*) FROM tags WHERE name LIKE ?", tagName+"/%").Scan(&childCount)
	if childCount > 0 {
		return
	}

	_, err = a.db.Exec("DELETE FROM tags WHERE id = ?", tagID)
	if err == nil {
		a.emitPluginEvent("tag:deleted", map[string]interface{}{"id": tagID})
	}
}

// getDescendantTagIDs returns all tag IDs whose names are descendants of the
// given tag (i.e., names matching parentName + "/%").
func (a *App) getDescendantTagIDs(tagID int64) ([]int64, error) {
	var parentName string
	err := a.db.QueryRow("SELECT name FROM tags WHERE id = ?", tagID).Scan(&parentName)
	if err != nil {
		return nil, fmt.Errorf("failed to find tag %d: %w", tagID, err)
	}

	rows, err := a.db.Query("SELECT id FROM tags WHERE name LIKE ?", parentName+"/%")
	if err != nil {
		return nil, fmt.Errorf("failed to query descendant tags: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan descendant tag id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// getChildTags returns immediate child tags of the given tag, with clip counts.
func (a *App) getChildTags(tagID int64) ([]Tag, error) {
	var parentName string
	err := a.db.QueryRow("SELECT name FROM tags WHERE id = ?", tagID).Scan(&parentName)
	if err != nil {
		return nil, fmt.Errorf("failed to find tag %d: %w", tagID, err)
	}

	rows, err := a.db.Query(`
		SELECT t.id, t.name, t.color, COUNT(ct.clip_id) as count
		FROM tags t
		LEFT JOIN clip_tags ct ON t.id = ct.tag_id
		WHERE t.name LIKE ?
		GROUP BY t.id
		ORDER BY t.name
	`, parentName+"/%")
	if err != nil {
		return nil, fmt.Errorf("failed to query child tags: %w", err)
	}
	defer rows.Close()

	var children []Tag
	for rows.Next() {
		var tag Tag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Color, &tag.Count); err != nil {
			log.Printf("Failed to scan child tag: %v", err)
			continue
		}
		if isImmediateChildOf(tag.Name, parentName) {
			children = append(children, tag)
		}
	}

	if children == nil {
		children = []Tag{}
	}
	return children, nil
}

// getTopLevelTags returns tags whose names contain no "/" separator, with clip counts.
func (a *App) getTopLevelTags() ([]Tag, error) {
	rows, err := a.db.Query(`
		SELECT t.id, t.name, t.color, COUNT(ct.clip_id) as count
		FROM tags t
		LEFT JOIN clip_tags ct ON t.id = ct.tag_id
		WHERE t.name NOT LIKE '%/%'
		GROUP BY t.id
		ORDER BY t.name
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query top-level tags: %w", err)
	}
	defer rows.Close()

	var tags []Tag
	for rows.Next() {
		var tag Tag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Color, &tag.Count); err != nil {
			log.Printf("Failed to scan top-level tag: %v", err)
			continue
		}
		tags = append(tags, tag)
	}

	if tags == nil {
		tags = []Tag{}
	}
	return tags, nil
}

// getDescendantClipCount returns the number of distinct clips tagged with the
// given tag or any of its descendants, counting only clips that folder mode
// would actually show: matching the archive view and not yet expired.
func (a *App) getDescendantClipCount(tagID int64, archived bool) (int, error) {
	var parentName string
	err := a.db.QueryRow("SELECT name FROM tags WHERE id = ?", tagID).Scan(&parentName)
	if err != nil {
		return 0, fmt.Errorf("failed to find tag %d: %w", tagID, err)
	}

	archivedInt := 0
	if archived {
		archivedInt = 1
	}

	var count int
	err = a.db.QueryRow(`
		SELECT COUNT(DISTINCT ct.clip_id)
		FROM clip_tags ct
		INNER JOIN tags t ON ct.tag_id = t.id
		INNER JOIN clips c ON c.id = ct.clip_id
		WHERE (t.id = ? OR t.name LIKE ?)
		  AND c.is_archived = ?
		  AND (c.expires_at IS NULL OR c.expires_at > CURRENT_TIMESTAMP)
	`, tagID, parentName+"/%", archivedInt).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count descendant clips: %w", err)
	}
	return count, nil
}

// GetChildTags returns immediate child tags of the given tag (exported for Wails binding).
func (a *App) GetChildTags(tagID int64) ([]Tag, error) {
	return a.getChildTags(tagID)
}

// GetTopLevelTags returns tags with no parent (exported for Wails binding).
func (a *App) GetTopLevelTags() ([]Tag, error) {
	return a.getTopLevelTags()
}

// GetDescendantClipCount returns total clip count for a tag and all descendants (exported for Wails binding).
func (a *App) GetDescendantClipCount(tagID int64, archived bool) (int, error) {
	return a.getDescendantClipCount(tagID, archived)
}

// BulkAddTag adds a tag to multiple clips
func (a *App) BulkAddTag(clipIDs []int64, tagID int64) error {
	if len(clipIDs) == 0 {
		return nil
	}

	// Exclude restores for the whole span from tx.Begin through the per-clip
	// hook decisions, exactly as AddTagToClip does (see backupRestoreMu's doc).
	// Released before the plugin events for the same re-entrancy reason.
	a.backupRestoreMu.RLock()
	restoreLockHeld := true
	releaseRestoreLock := func() {
		if restoreLockHeld {
			restoreLockHeld = false
			a.backupRestoreMu.RUnlock()
		}
	}
	defer releaseRestoreLock()

	tx, err := a.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("INSERT OR IGNORE INTO clip_tags (clip_id, tag_id) VALUES (?, ?)")
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	// Enforce tree exclusivity before bulk insert. These run in tx with the
	// inserts so a later failure rolls the removals back too — otherwise an
	// aborted bulk tag leaves the earlier clips stripped of their old tags.
	for _, clipID := range clipIDs {
		if err := removeSameTreeTags(tx, clipID, tagID); err != nil {
			return fmt.Errorf("failed to enforce tree exclusivity for clip %d tag %d: %w", clipID, tagID, err)
		}
	}

	// Track which clips genuinely gained the tag. OR IGNORE turns a re-add
	// into a no-op, so RowsAffected == 0 means the clip already had it and
	// must not be re-published to followers.
	var newlyTagged []int64
	for _, clipID := range clipIDs {
		res, err := stmt.Exec(clipID, tagID)
		if err != nil {
			return fmt.Errorf("failed to add tag to clip %d: %w", clipID, err)
		}
		if n, rerr := res.RowsAffected(); rerr == nil && n > 0 {
			newlyTagged = append(newlyTagged, clipID)
		}
	}

	// One operation-level admission taken before the commit, for the same
	// reason AddTagToClip takes its own there: it is what pins a restore that
	// starts after this commit until the per-clip hooks below are registered.
	// Without it the whole batch could commit, stall, and be published against
	// a rebuilt manager under ids that now name restored rows.
	//
	// The per-clip spawns still take their own admissions — this one only
	// covers the gap between the commit and those registrations, and is
	// released once the loop has handed every clip over. Taken only when the
	// batch actually has something to publish.
	//
	// A refusal means the gate is closed or suspended, which the per-clip
	// spawns would hit too, so the whole batch's publication is skipped. The
	// tag operation itself still commits.
	opAdmitted := len(newlyTagged) > 0 && a.tryAddShareHook()

	if err := tx.Commit(); err != nil {
		if opAdmitted {
			a.shareHookWG.Done()
		}
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Share hook — mirror AddTagToClip so bulk-tag and folder-drag paths
	// (which both route through here) fan the clips out to any connected
	// followers whose publication tag matches. Only clips that actually
	// gained the tag publish, and only under that one tag, so re-tagging a
	// clip that already sits in the shared folder is not a re-publish.
	//
	// Each clip takes its own admission before spawnAdmittedShareHook captures
	// a.shareManager — the ordering RestoreBackup's drain relies on. The
	// manager is read per clip rather than once before the loop, so a restore
	// that swaps it mid-batch cannot leave later clips pointed at a stopped
	// manager.
	//
	// Those per-clip admissions are ADOPTED from the operation admission rather
	// than taken through the gate again. Re-entering it here was a deadlock's
	// worth of lost publications: a restore that suspended the gate after the
	// commit above is, by then, blocked in its drain waiting on this very
	// operation admission, so the gate it is waiting behind would refuse every
	// clip in the batch. The rows are already committed and nothing re-scans
	// tag associations, so if that restore went on to fail and roll back, the
	// tags would survive with their publications never sent. See
	// adoptShareHookAdmission for why transferring is safe.
	//
	// Runs before the plugin events for the reason AddTagToClip documents: the
	// operation admission must not be held across synchronous Lua handlers.
	suppressed := len(newlyTagged)
	if opAdmitted {
		suppressed = 0
		for _, clipID := range newlyTagged {
			if !a.adoptShareHookAdmission() {
				// Only Shutdown's close refuses an adoption, and it never
				// reopens, so every remaining clip in the batch is refused
				// too; count them and log once.
				suppressed++
				continue
			}
			a.spawnAdmittedShareHook(clipID, tagID, "BulkAddTag")
		}
		a.shareHookWG.Done()
	}
	if suppressed > 0 {
		log.Printf("share: publication hooks closed or suspended (shutdown/restore); suppressed %d hook(s) under tag %d (BulkAddTag)", suppressed, tagID)
	}

	// The coherent span ends here: the tx committed and every per-clip hook is
	// registered. Release before the plugin events (re-entrancy — see above).
	releaseRestoreLock()

	// Emit plugin events for each clip
	if a.pluginManager != nil {
		for _, clipID := range clipIDs {
			a.pluginManager.EmitEvent("tag:added_to_clip", map[string]interface{}{
				"tag_id":  tagID,
				"clip_id": clipID,
			})
		}
	}

	return nil
}

// BulkRemoveTag removes a tag from multiple clips
func (a *App) BulkRemoveTag(clipIDs []int64, tagID int64) error {
	if len(clipIDs) == 0 {
		return nil
	}

	placeholders := make([]string, len(clipIDs))
	args := make([]interface{}, len(clipIDs)+1)
	args[0] = tagID
	for i, id := range clipIDs {
		placeholders[i] = "?"
		args[i+1] = id
	}

	query := fmt.Sprintf("DELETE FROM clip_tags WHERE tag_id = ? AND clip_id IN (%s)", strings.Join(placeholders, ","))
	_, err := a.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to bulk remove tag: %w", err)
	}

	// Emit plugin events for each clip
	if a.pluginManager != nil {
		for _, clipID := range clipIDs {
			a.pluginManager.EmitEvent("tag:removed_from_clip", map[string]interface{}{
				"tag_id":  tagID,
				"clip_id": clipID,
			})
		}
	}

	// Clean up orphaned tag
	a.deleteTagIfOrphaned(tagID)
	return nil
}

// GetClipTags returns tags for a specific clip
func (a *App) GetClipTags(clipID int64) ([]Tag, error) {
	rows, err := a.db.Query(`
		SELECT t.id, t.name, t.color
		FROM tags t
		INNER JOIN clip_tags ct ON t.id = ct.tag_id
		WHERE ct.clip_id = ?
		ORDER BY t.name
	`, clipID)
	if err != nil {
		return nil, fmt.Errorf("failed to query clip tags: %w", err)
	}
	defer rows.Close()

	var tags []Tag
	for rows.Next() {
		var tag Tag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Color); err != nil {
			log.Printf("Failed to scan clip tag: %v", err)
			continue
		}
		tags = append(tags, tag)
	}

	if tags == nil {
		tags = []Tag{}
	}
	return tags, nil
}

// BulkDelete deletes multiple clips at once
func (a *App) BulkDelete(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	// Get all tag IDs associated with these clips before deleting
	tagQuery := fmt.Sprintf("SELECT DISTINCT tag_id FROM clip_tags WHERE clip_id IN (%s)", strings.Join(placeholders, ","))
	rows, err := a.db.Query(tagQuery, args...)
	if err != nil {
		return fmt.Errorf("failed to query clip tags: %w", err)
	}
	var tagIDs []int64
	for rows.Next() {
		var tagID int64
		if err := rows.Scan(&tagID); err == nil {
			tagIDs = append(tagIDs, tagID)
		}
	}
	rows.Close()

	// Explicitly delete clip_tags (don't rely on CASCADE)
	clipTagsQuery := fmt.Sprintf("DELETE FROM clip_tags WHERE clip_id IN (%s)", strings.Join(placeholders, ","))
	_, err = a.db.Exec(clipTagsQuery, args...)
	if err != nil {
		return fmt.Errorf("failed to delete clip tags: %w", err)
	}

	// Delete the clips
	query := fmt.Sprintf("DELETE FROM clips WHERE id IN (%s)", strings.Join(placeholders, ","))
	_, err = a.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to bulk delete: %w", err)
	}
	if err := a.deleteTempFilesForClipIDs(ids); err != nil {
		log.Printf("Warning: failed to clean temp files for bulk delete: %v", err)
	}

	// Clean up orphaned tags
	for _, tagID := range tagIDs {
		a.deleteTagIfOrphaned(tagID)
	}
	return nil
}

// BulkArchive archives multiple clips
func (a *App) BulkArchive(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf("UPDATE clips SET is_archived = 1 WHERE id IN (%s)", strings.Join(placeholders, ","))
	_, err := a.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to bulk archive: %w", err)
	}
	return nil
}

// BulkUnarchive restores multiple clips from the archive
func (a *App) BulkUnarchive(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf("UPDATE clips SET is_archived = 0 WHERE id IN (%s)", strings.Join(placeholders, ","))
	_, err := a.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to bulk unarchive: %w", err)
	}
	return nil
}

// BulkSetExpiration sets expiration on multiple clips
func (a *App) BulkSetExpiration(ids []int64, minutes int) error {
	if len(ids) == 0 {
		return nil
	}
	if minutes <= 0 {
		return fmt.Errorf("expiration minutes must be positive")
	}

	expiresAt := expiryTimestamp(minutes)

	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids)+1)
	args[0] = expiresAt
	for i, id := range ids {
		placeholders[i] = "?"
		args[i+1] = id
	}

	query := fmt.Sprintf("UPDATE clips SET expires_at = ? WHERE id IN (%s)", strings.Join(placeholders, ","))
	_, err := a.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to bulk set expiration: %w", err)
	}
	return nil
}

// BulkCancelExpiration removes expiration from multiple clips
func (a *App) BulkCancelExpiration(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf("UPDATE clips SET expires_at = NULL WHERE id IN (%s)", strings.Join(placeholders, ","))
	_, err := a.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to bulk cancel expiration: %w", err)
	}
	return nil
}

// CreateTempFile creates a temporary file from a clip and returns its path
func (a *App) CreateTempFile(id int64) (string, error) {
	item, err := a.prepareClipTransferItem(id, "legacy_create_temp")
	if err != nil {
		return "", err
	}
	return item.AbsPath, nil
}

// DeleteAllTempFiles deletes all files from the temp directory
func (a *App) DeleteAllTempFiles() error {
	if a.tempStore == nil {
		return nil
	}
	return a.tempStore.DeleteAll()
}

// ReadFileFromPath reads a file from disk (for drag-drop)
func (a *App) ReadFileFromPath(path string) (*FileData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Detect content type from extension
	ext := filepath.Ext(path)
	contentType := mime.TypeByExtension(ext)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	return &FileData{
		Name:        filepath.Base(path),
		ContentType: contentType,
		Data:        base64.StdEncoding.EncodeToString(data),
	}, nil
}

// ConfirmRestoreBackup performs the actual restore after user confirmation.
// identityPolicy must be "takeover", "keep", or "none" (see RestoreBackup).
func (a *App) ConfirmRestoreBackup(backupPath, identityPolicy string) error {
	return a.RestoreBackup(backupPath, identityPolicy)
}

// isJSON checks if a string is valid JSON
func isJSON(s string) bool {
	var js json.RawMessage
	return json.Unmarshal([]byte(s), &js) == nil
}

// GetWatchedFolders retrieves all watched folders
func (a *App) GetWatchedFolders() ([]WatchedFolder, error) {
	rows, err := a.db.Query(`
		SELECT id, path, filter_mode, filter_presets, filter_regex,
		       process_existing, auto_archive, auto_tag_id, is_paused, created_at
		FROM watched_folders
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query watched folders: %w", err)
	}
	defer rows.Close()

	var folders []WatchedFolder
	for rows.Next() {
		var f WatchedFolder
		var filterPresets sql.NullString
		var filterRegex sql.NullString
		var autoTagID sql.NullInt64
		var processExisting, autoArchive, isPaused int

		if err := rows.Scan(&f.ID, &f.Path, &f.FilterMode, &filterPresets, &filterRegex,
			&processExisting, &autoArchive, &autoTagID, &isPaused, &f.CreatedAt); err != nil {
			log.Printf("Failed to scan watched folder: %v", err)
			continue
		}

		f.ProcessExisting = processExisting == 1
		f.AutoArchive = autoArchive == 1
		f.IsPaused = isPaused == 1
		f.FilterRegex = filterRegex.String
		if autoTagID.Valid {
			f.AutoTagID = &autoTagID.Int64
		}

		// Parse filter presets JSON
		if filterPresets.Valid && filterPresets.String != "" {
			_ = json.Unmarshal([]byte(filterPresets.String), &f.FilterPresets)
		}
		if f.FilterPresets == nil {
			f.FilterPresets = []string{}
		}

		// Check if folder exists
		if _, err := os.Stat(f.Path); err == nil {
			f.Exists = true
		}

		folders = append(folders, f)
	}

	if folders == nil {
		folders = []WatchedFolder{}
	}
	return folders, nil
}

// GetWatchedFolderByID retrieves a single watched folder by ID
func (a *App) GetWatchedFolderByID(id int64) (*WatchedFolder, error) {
	var f WatchedFolder
	var filterPresets sql.NullString
	var filterRegex sql.NullString
	var autoTagID sql.NullInt64
	var processExisting, autoArchive, isPaused int

	err := a.db.QueryRow(`
		SELECT id, path, filter_mode, filter_presets, filter_regex,
		       process_existing, auto_archive, auto_tag_id, is_paused, created_at
		FROM watched_folders
		WHERE id = ?
	`, id).Scan(&f.ID, &f.Path, &f.FilterMode, &filterPresets, &filterRegex,
		&processExisting, &autoArchive, &autoTagID, &isPaused, &f.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query watched folder: %w", err)
	}

	f.ProcessExisting = processExisting == 1
	f.AutoArchive = autoArchive == 1
	f.IsPaused = isPaused == 1
	f.FilterRegex = filterRegex.String
	if autoTagID.Valid {
		f.AutoTagID = &autoTagID.Int64
	}

	if filterPresets.Valid && filterPresets.String != "" {
		_ = json.Unmarshal([]byte(filterPresets.String), &f.FilterPresets)
	}
	if f.FilterPresets == nil {
		f.FilterPresets = []string{}
	}

	if _, err := os.Stat(f.Path); err == nil {
		f.Exists = true
	}

	return &f, nil
}

// AddWatchedFolder adds a new folder to watch
func (a *App) AddWatchedFolder(config WatchedFolderConfig) (*WatchedFolder, error) {
	// Validate path exists
	if _, err := os.Stat(config.Path); os.IsNotExist(err) {
		return nil, fmt.Errorf("folder does not exist: %s", config.Path)
	}

	// Default filter mode
	if config.FilterMode == "" {
		config.FilterMode = "all"
	}

	// Serialize presets to JSON
	var presetsJSON []byte
	if len(config.FilterPresets) > 0 {
		presetsJSON, _ = json.Marshal(config.FilterPresets)
	}

	result, err := a.db.Exec(`
		INSERT INTO watched_folders (path, filter_mode, filter_presets, filter_regex, process_existing, auto_archive, auto_tag_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, config.Path, config.FilterMode, string(presetsJSON), config.FilterRegex,
		boolToInt(config.ProcessExisting), boolToInt(config.AutoArchive), config.AutoTagID)
	if err != nil {
		return nil, fmt.Errorf("failed to add watched folder: %w", err)
	}

	id, _ := result.LastInsertId()

	return &WatchedFolder{
		ID:              id,
		Path:            config.Path,
		FilterMode:      config.FilterMode,
		FilterPresets:   config.FilterPresets,
		FilterRegex:     config.FilterRegex,
		ProcessExisting: config.ProcessExisting,
		AutoArchive:     config.AutoArchive,
		AutoTagID:       config.AutoTagID,
		IsPaused:        false,
		Exists:          true,
	}, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// UpdateWatchedFolder updates an existing watched folder config.
// Zero-value fields in config are treated as "not provided" and keep existing values.
func (a *App) UpdateWatchedFolder(id int64, config WatchedFolderConfig) error {
	existing, err := a.GetWatchedFolderByID(id)
	if err != nil {
		return fmt.Errorf("watch folder not found: %w", err)
	}
	if existing == nil {
		return fmt.Errorf("watch folder %d not found", id)
	}

	filterMode := existing.FilterMode
	if config.FilterMode != "" {
		filterMode = config.FilterMode
	}

	filterPresets := existing.FilterPresets
	if len(config.FilterPresets) > 0 {
		filterPresets = config.FilterPresets
	}

	filterRegex := existing.FilterRegex
	if config.FilterRegex != "" {
		filterRegex = config.FilterRegex
	}

	autoArchive := existing.AutoArchive
	if config.AutoArchive {
		autoArchive = true
	}

	autoTagID := existing.AutoTagID
	if config.AutoTagID != nil {
		autoTagID = config.AutoTagID
	}

	var presetsJSON []byte
	if len(filterPresets) > 0 {
		presetsJSON, _ = json.Marshal(filterPresets)
	}

	_, err = a.db.Exec(`
		UPDATE watched_folders
		SET filter_mode = ?, filter_presets = ?, filter_regex = ?, auto_archive = ?, auto_tag_id = ?
		WHERE id = ?
	`, filterMode, string(presetsJSON), filterRegex,
		boolToInt(autoArchive), autoTagID, id)
	if err != nil {
		return fmt.Errorf("failed to update watched folder: %w", err)
	}
	return nil
}

// UpdateWatchedFolderPartial updates only the fields present in the raw JSON map.
func (a *App) UpdateWatchedFolderPartial(id int64, fields map[string]json.RawMessage) error {
	existing, err := a.GetWatchedFolderByID(id)
	if err != nil {
		return fmt.Errorf("watch folder query failed: %w", err)
	}
	if existing == nil {
		return fmt.Errorf("watch folder %d not found", id)
	}

	filterMode := existing.FilterMode
	filterPresets := existing.FilterPresets
	filterRegex := existing.FilterRegex
	autoArchive := existing.AutoArchive
	autoTagID := existing.AutoTagID

	if raw, ok := fields["filter_mode"]; ok {
		var v string
		if err := json.Unmarshal(raw, &v); err == nil {
			filterMode = v
		}
	}
	if raw, ok := fields["filter_presets"]; ok {
		var v []string
		if err := json.Unmarshal(raw, &v); err == nil {
			filterPresets = v
		}
	}
	if raw, ok := fields["filter_regex"]; ok {
		var v string
		if err := json.Unmarshal(raw, &v); err == nil {
			filterRegex = v
		}
	}
	if raw, ok := fields["auto_archive"]; ok {
		var v bool
		if err := json.Unmarshal(raw, &v); err == nil {
			autoArchive = v
		}
	}
	if raw, ok := fields["auto_tag_id"]; ok {
		if string(raw) == "null" {
			autoTagID = nil
		} else {
			var v int64
			if err := json.Unmarshal(raw, &v); err == nil {
				autoTagID = &v
			}
		}
	}

	var presetsJSON []byte
	if len(filterPresets) > 0 {
		presetsJSON, _ = json.Marshal(filterPresets)
	}

	_, err = a.db.Exec(`
		UPDATE watched_folders
		SET filter_mode = ?, filter_presets = ?, filter_regex = ?, auto_archive = ?, auto_tag_id = ?
		WHERE id = ?
	`, filterMode, string(presetsJSON), filterRegex,
		boolToInt(autoArchive), autoTagID, id)
	if err != nil {
		return fmt.Errorf("failed to update watched folder: %w", err)
	}
	return nil
}

// RemoveWatchedFolder removes a watched folder
func (a *App) RemoveWatchedFolder(id int64) error {
	_, err := a.db.Exec("DELETE FROM watched_folders WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to remove watched folder: %w", err)
	}
	return nil
}

// GetGlobalWatchPaused returns whether global watching is paused
func (a *App) GetGlobalWatchPaused() bool {
	var value string
	err := a.db.QueryRow("SELECT value FROM settings WHERE key = 'global_watch_paused'").Scan(&value)
	if err != nil {
		return false
	}
	return value == "true"
}

// SetGlobalWatchPaused sets the global watch pause state
func (a *App) SetGlobalWatchPaused(paused bool) error {
	value := "false"
	if paused {
		value = "true"
	}
	_, err := a.db.Exec("UPDATE settings SET value = ? WHERE key = 'global_watch_paused'", value)
	return err
}

// SetFolderPaused sets the pause state for a specific folder
func (a *App) SetFolderPaused(id int64, paused bool) error {
	_, err := a.db.Exec("UPDATE watched_folders SET is_paused = ? WHERE id = ?", boolToInt(paused), id)
	return err
}

// ProcessExistingFilesInFolder processes existing files in a watched folder
func (a *App) ProcessExistingFilesInFolder(folderID int64) error {
	if a.watcherManager != nil {
		return a.watcherManager.ProcessExistingFiles(folderID)
	}
	return nil
}

// WatchStatus represents the current watching state
type WatchStatus struct {
	GlobalPaused bool `json:"global_paused"`
	ActiveCount  int  `json:"active_count"`
	TotalCount   int  `json:"total_count"`
	IsWatching   bool `json:"is_watching"` // true if any folder is actively being watched
}

// GetWatchStatus returns the current watch status
func (a *App) GetWatchStatus() WatchStatus {
	globalPaused := a.GetGlobalWatchPaused()
	folders, err := a.GetWatchedFolders()
	if err != nil {
		log.Printf("Warning: Failed to get watched folders for status: %v", err)
		return WatchStatus{GlobalPaused: globalPaused}
	}

	activeCount := 0
	for _, f := range folders {
		if !f.IsPaused && f.Exists {
			activeCount++
		}
	}

	return WatchStatus{
		GlobalPaused: globalPaused,
		ActiveCount:  activeCount,
		TotalCount:   len(folders),
		IsWatching:   !globalPaused && activeCount > 0,
	}
}

// GetSetting retrieves a setting value by key
func (a *App) GetSetting(key string) (string, error) {
	var value string
	err := a.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return value, nil
}

// SetSetting stores a setting value (insert or update)
func (a *App) SetSetting(key string, value string) error {
	_, err := a.db.Exec(`
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value)
	return err
}

// GetHiddenTags returns the list of hidden tag IDs
func (a *App) GetHiddenTags() ([]int64, error) {
	value, err := a.GetSetting("hidden_tags")
	if err != nil {
		return nil, fmt.Errorf("failed to get hidden_tags setting: %w", err)
	}
	if value == "" {
		return []int64{}, nil
	}
	var ids []int64
	if err := json.Unmarshal([]byte(value), &ids); err != nil {
		return nil, fmt.Errorf("failed to parse hidden_tags setting: %w", err)
	}
	return ids, nil
}

// SetHiddenTags saves the list of hidden tag IDs
func (a *App) SetHiddenTags(ids []int64) error {
	if ids == nil {
		ids = []int64{}
	}
	data, err := json.Marshal(ids)
	if err != nil {
		return fmt.Errorf("failed to marshal hidden tags: %w", err)
	}
	return a.SetSetting("hidden_tags", string(data))
}

// GetImageDiff compares two image clips and returns a visual diff + similarity score.
// threshold controls sensitivity (1-100, default 30).
func (a *App) GetImageDiff(clipIdA, clipIdB int64, threshold int) (*DiffResult, error) {
	if threshold < 1 {
		threshold = 1
	}
	if threshold > 100 {
		threshold = 100
	}

	loadImg := func(clipID int64) (image.Image, error) {
		var data []byte
		var contentType string
		err := a.db.QueryRow("SELECT data, content_type FROM clips WHERE id = ?", clipID).Scan(&data, &contentType)
		if err != nil {
			return nil, fmt.Errorf("clip %d: %w", clipID, err)
		}
		if !strings.HasPrefix(contentType, "image/") {
			return nil, fmt.Errorf("clip %d is not an image", clipID)
		}
		decoded, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("failed to decode clip %d: %w", clipID, err)
		}
		return decoded, nil
	}

	imgA, err := loadImg(clipIdA)
	if err != nil {
		return nil, err
	}
	imgB, err := loadImg(clipIdB)
	if err != nil {
		return nil, err
	}

	diffImg, similarity := plugin.DiffImages(imgA, imgB, float64(threshold))
	diffData, diffMime, err := plugin.EncodeImagePNG(diffImg)
	if err != nil {
		return nil, fmt.Errorf("failed to encode diff: %w", err)
	}

	return &DiffResult{
		Similarity:  similarity,
		DiffDataUrl: fmt.Sprintf("data:%s;base64,%s", diffMime, diffData),
	}, nil
}

// GetClipMetadata returns all metadata key-value pairs for a clip
func (a *App) GetClipMetadata(clipID int64) (map[string]string, error) {
	var raw string
	err := a.db.QueryRow("SELECT COALESCE(metadata, '{}') FROM clips WHERE id = ?", clipID).Scan(&raw)
	if err != nil {
		return nil, fmt.Errorf("failed to get metadata: %w", err)
	}
	var meta map[string]string
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return map[string]string{}, nil
	}
	return meta, nil
}

// UpdateClipMetadata performs an atomic read-modify-write of clip metadata
// inside a transaction. The modify function receives the current metadata and
// mutates it in place; returning an error aborts the transaction.
func (a *App) UpdateClipMetadata(clipID int64, modify func(meta map[string]string) error) error {
	tx, err := a.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	var raw string
	if err := tx.QueryRow("SELECT COALESCE(metadata, '{}') FROM clips WHERE id = ?", clipID).Scan(&raw); err != nil {
		return fmt.Errorf("failed to read metadata: %w", err)
	}
	var meta map[string]string
	if err := json.Unmarshal([]byte(raw), &meta); err != nil || meta == nil {
		meta = map[string]string{}
	}

	if err := modify(meta); err != nil {
		return err
	}

	encoded, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to encode metadata: %w", err)
	}
	if _, err := tx.Exec("UPDATE clips SET metadata = ? WHERE id = ?", string(encoded), clipID); err != nil {
		return err
	}
	return tx.Commit()
}

// SetClipMetadata sets a single metadata key-value pair on a clip (upsert)
func (a *App) SetClipMetadata(clipID int64, key string, value string) error {
	if key == "" {
		return fmt.Errorf("metadata key cannot be empty")
	}
	if utf8.RuneCountInString(key) > 256 {
		return fmt.Errorf("metadata key too long (max 256 chars)")
	}
	if utf8.RuneCountInString(value) > 4096 {
		return fmt.Errorf("metadata value too long (max 4096 chars)")
	}
	return a.UpdateClipMetadata(clipID, func(meta map[string]string) error {
		if len(meta) >= maxMetadataPairs {
			if _, exists := meta[key]; !exists {
				return fmt.Errorf("metadata limit reached (max %d pairs)", maxMetadataPairs)
			}
		}
		meta[key] = value
		return nil
	})
}

// DeleteClipMetadata removes a single metadata key from a clip
func (a *App) DeleteClipMetadata(clipID int64, key string) error {
	return a.UpdateClipMetadata(clipID, func(meta map[string]string) error {
		delete(meta, key)
		return nil
	})
}

// SetClipMetadataBulk replaces all metadata on a clip
func (a *App) SetClipMetadataBulk(clipID int64, metadata map[string]string) error {
	if len(metadata) > maxMetadataPairs {
		return fmt.Errorf("metadata limit exceeded (max %d pairs, got %d)", maxMetadataPairs, len(metadata))
	}
	for k, v := range metadata {
		if k == "" {
			return fmt.Errorf("metadata key cannot be empty")
		}
		if utf8.RuneCountInString(k) > 256 {
			return fmt.Errorf("metadata key too long (max 256 chars)")
		}
		if utf8.RuneCountInString(v) > 4096 {
			return fmt.Errorf("metadata value too long (max 4096 chars)")
		}
	}
	return a.UpdateClipMetadata(clipID, func(meta map[string]string) error {
		for k := range meta {
			delete(meta, k)
		}
		for k, v := range metadata {
			meta[k] = v
		}
		return nil
	})
}

// --- Delegation helpers for APIManager ---
// These unexported methods let APIManager (same package) call through to
// PluginService / ClipboardService functionality without holding direct
// references to those service structs.

// getPluginUIActions returns all UI actions from enabled plugins.
func (a *App) getPluginUIActions() (*UIActionsResponse, error) {
	if a.pluginManager == nil {
		return &UIActionsResponse{
			LightboxButtons: []PluginUIAction{},
			CardActions:     []PluginUIAction{},
			BulkActions:     []PluginUIAction{},
			GlobalActions:   []PluginUIAction{},
			Ready:           a.PluginsReady(),
		}, nil
	}

	response := &UIActionsResponse{
		LightboxButtons: []PluginUIAction{},
		CardActions:     []PluginUIAction{},
		BulkActions:     []PluginUIAction{},
		GlobalActions:   []PluginUIAction{},
		Ready:           a.PluginsReady(),
	}

	plugins := a.pluginManager.GetPlugins()
	for _, p := range plugins {
		if !p.Enabled || p.Manifest == nil || p.Manifest.UI == nil {
			continue
		}

		for _, btn := range p.Manifest.UI.LightboxButtons {
			response.LightboxButtons = append(response.LightboxButtons, PluginUIAction{
				PluginID:   p.ID,
				PluginName: p.Name,
				ID:         btn.ID,
				Label:      btn.Label,
				Icon:       btn.Icon,
				Async:      btn.Async,
				Options:    btn.Options,
				FileTypes:  btn.FileTypes,
				MaxSize:    btn.MaxSize,
			})
		}

		for _, action := range p.Manifest.UI.CardActions {
			response.CardActions = append(response.CardActions, PluginUIAction{
				PluginID:   p.ID,
				PluginName: p.Name,
				ID:         action.ID,
				Label:      action.Label,
				Icon:       action.Icon,
				Async:      action.Async,
				Options:    action.Options,
				FileTypes:  action.FileTypes,
				MaxSize:    action.MaxSize,
			})
		}

		for _, action := range p.Manifest.UI.BulkActions {
			response.BulkActions = append(response.BulkActions, PluginUIAction{
				PluginID:   p.ID,
				PluginName: p.Name,
				ID:         action.ID,
				Label:      action.Label,
				Icon:       action.Icon,
				Async:      action.Async,
				Options:    action.Options,
				FileTypes:  action.FileTypes,
				MaxSize:    action.MaxSize,
			})
		}

		for _, action := range p.Manifest.UI.GlobalActions {
			response.GlobalActions = append(response.GlobalActions, PluginUIAction{
				PluginID:   p.ID,
				PluginName: p.Name,
				ID:         action.ID,
				Label:      action.Label,
				Icon:       action.Icon,
				Async:      action.Async,
				Options:    action.Options,
			})
		}
	}

	return response, nil
}

// installPluginFromSource installs a plugin from a URL or file path.
func (a *App) installPluginFromSource(source string) (*PluginInfo, error) {
	if a.pluginManager == nil {
		return nil, fmt.Errorf("plugin manager not initialized")
	}

	isURL := strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://")

	var p *plugin.Plugin
	var err error

	if cachedSource := a.pluginManager.PopPendingInstall(source); cachedSource != "" {
		// Content was fetched (URL) or uploaded (file) during preview; install
		// exactly what was reviewed to avoid a TOCTOU re-fetch. Uploaded content
		// has no update URL, so only carry the source URL forward for real URLs.
		sourceURL := source
		if !isURL {
			sourceURL = ""
		}
		p, err = a.pluginManager.ImportPluginFromSource(cachedSource, sourceURL)
	} else if isURL {
		p, err = a.pluginManager.ImportPluginFromURL(source)
	} else {
		p, err = a.pluginManager.ImportPlugin(source)
	}
	if err != nil {
		return nil, err
	}

	return pluginToInfo(p), nil
}

// executePluginAction calls a plugin's on_ui_action handler.
// context carries invocation context (e.g. the active folder's tag) and may be nil.
func (a *App) executePluginAction(pluginID int64, actionID string, clipIDs []int64, options map[string]interface{}, context map[string]interface{}) (*ActionResult, error) {
	if a.pluginManager == nil {
		return &ActionResult{Success: false, Error: "plugin manager not initialized"}, nil
	}

	result, err := a.pluginManager.ExecuteUIAction(pluginID, actionID, clipIDs, options, context)
	if err != nil {
		return &ActionResult{Success: false, Error: err.Error()}, nil
	}

	return result, nil
}

// getAllPluginStorage retrieves all storage key-value pairs for a plugin.
func (a *App) getAllPluginStorage(pluginID int64) (map[string]string, error) {
	if a.db == nil {
		return map[string]string{}, nil
	}

	rows, err := a.db.Query(`
		SELECT key, value FROM plugin_storage WHERE plugin_id = ?
	`, pluginID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var key string
		var value []byte
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}
		result[key] = string(value)
	}

	return result, nil
}

// getPluginStorage retrieves a value from a plugin's storage.
func (a *App) getPluginStorage(pluginID int64, key string) (string, error) {
	if a.db == nil {
		return "", fmt.Errorf("database not initialized")
	}

	var value string
	err := a.db.QueryRow(`
		SELECT value FROM plugin_storage WHERE plugin_id = ? AND key = ?
	`, pluginID, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return value, nil
}

// setPluginStorage sets a value in a plugin's storage.
func (a *App) setPluginStorage(pluginID int64, key, value string) error {
	if a.db == nil {
		return fmt.Errorf("database not initialized")
	}

	_, err := a.db.Exec(`
		INSERT INTO plugin_storage (plugin_id, key, value)
		VALUES (?, ?, ?)
		ON CONFLICT (plugin_id, key) DO UPDATE SET value = ?
	`, pluginID, key, value, value)
	return err
}

// updatePlugin fetches the latest version from the source URL and applies the update.
func (a *App) updatePlugin(pluginID int64) (*UpdateResult, error) {
	if a.pluginManager == nil {
		return &UpdateResult{Error: "plugin manager not initialized"}, nil
	}

	var sourceURL, currentVersion string
	err := a.db.QueryRow("SELECT source_url, version FROM plugins WHERE id = ?", pluginID).Scan(&sourceURL, &currentVersion)
	if err != nil {
		return &UpdateResult{Error: "plugin not found"}, nil
	}
	if sourceURL == "" {
		return &UpdateResult{Error: "plugin was not installed from a URL"}, nil
	}

	source, err := plugin.FetchPluginSource(sourceURL)
	if err != nil {
		return &UpdateResult{Error: fmt.Sprintf("failed to fetch update: %v", err)}, nil
	}

	remoteManifest, err := plugin.ParseManifest(source)
	if err != nil {
		return &UpdateResult{Error: fmt.Sprintf("invalid remote plugin: %v", err)}, nil
	}

	if !plugin.IsNewerVersion(currentVersion, remoteManifest.Version) {
		return &UpdateResult{Error: fmt.Sprintf("remote version %s is not newer than installed %s", remoteManifest.Version, currentVersion)}, nil
	}

	// Get current manifest for permission comparison
	a.pluginManager.RLock()
	loaded, ok := a.pluginManager.GetPluginByID(pluginID)
	a.pluginManager.RUnlock()

	var currentManifest *plugin.Manifest
	if ok && loaded.Manifest != nil {
		currentManifest = loaded.Manifest
	} else {
		currentManifest = &plugin.Manifest{}
	}

	hasChanges := plugin.ManifestsHavePermissionChanges(currentManifest, remoteManifest)

	if hasChanges {
		preview := &PluginPreview{
			Name:        remoteManifest.Name,
			Version:     remoteManifest.Version,
			Description: remoteManifest.Description,
			Author:      remoteManifest.Author,
			Network:     remoteManifest.Network,
			Filesystem:  remoteManifest.Filesystem,
			Clipboard:   remoteManifest.Clipboard,
			Events:      remoteManifest.Events,
			Source:      sourceURL,
		}
		a.pluginManager.StorePendingUpdate(pluginID, source)
		return &UpdateResult{NeedsReview: true, Preview: preview}, nil
	}

	info, err := a.applyPluginUpdate(pluginID, source, remoteManifest, sourceURL)
	if err != nil {
		return &UpdateResult{Error: err.Error()}, nil
	}

	if uc := a.pluginManager.GetUpdateChecker(); uc != nil {
		uc.ClearUpdate(pluginID)
	}

	return &UpdateResult{Success: true, PluginInfo: info}, nil
}

// applyPluginUpdate writes updated source, reloads the plugin, and updates the DB record.
func (a *App) applyPluginUpdate(pluginID int64, source string, manifest *plugin.Manifest, sourceURL string) (*PluginInfo, error) {
	var filename string
	err := a.db.QueryRow("SELECT filename FROM plugins WHERE id = ?", pluginID).Scan(&filename)
	if err != nil {
		return nil, fmt.Errorf("plugin not found: %w", err)
	}

	destPath := filepath.Join(a.pluginManager.PluginsDir(), filename)

	oldContent, readErr := os.ReadFile(destPath)

	if err := os.WriteFile(destPath, []byte(source), 0644); err != nil {
		return nil, fmt.Errorf("failed to write updated plugin: %w", err)
	}

	a.pluginManager.UnloadPlugin(pluginID)

	p := &plugin.Plugin{
		ID: pluginID, Filename: filename, Name: manifest.Name,
		Version: manifest.Version, Enabled: true, Status: "enabled",
	}
	if err := a.pluginManager.LoadPluginPublic(p); err != nil {
		if readErr == nil {
			_ = os.WriteFile(destPath, oldContent, 0644)
			oldP := &plugin.Plugin{
				ID: pluginID, Filename: filename, Enabled: true, Status: "enabled",
			}
			_ = a.pluginManager.LoadPluginPublic(oldP)
		}
		return nil, fmt.Errorf("failed to reload plugin (rolled back): %w", err)
	}

	_, err = a.db.Exec(`
		UPDATE plugins SET name = ?, version = ?, enabled = 1, status = 'enabled', error_count = 0, source_url = ?
		WHERE id = ?
	`, manifest.Name, manifest.Version, sourceURL, pluginID)
	if err != nil {
		return nil, fmt.Errorf("failed to update plugin record: %w", err)
	}

	return pluginToInfo(p), nil
}
