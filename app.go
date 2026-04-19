package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"go-clipboard/internal/wailsbridge"
	"go-clipboard/plugin"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.design/x/clipboard"
	_ "golang.org/x/image/webp"
)

const maxMetadataPairs = 50

// App struct holds the application state
type App struct {
	ctx              context.Context
	bridge           *wailsbridge.Bridge
	db               *sql.DB
	tempDir          string
	tempStore        *TempClipStore
	transferHandler  *TransferFileHandler
	mu               sync.Mutex
	watcherManager   *WatcherManager
	serveManager     *ServeManager
	apiManager       *APIManager
	pluginManager    *plugin.Manager
	clipboardService *ClipboardService
	shareManager     *ShareManager
}

// NewApp creates a new App instance
func NewApp() *App {
	return &App{}
}

// computeContentHash returns the hex-encoded SHA-256 hash of data.
func computeContentHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// emitEvent sends a frontend event, guarded for nil ctx (e.g. in tests).
func (a *App) emitEvent(event string, data ...interface{}) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, event, data...)
	}
}

// emitPluginEvent dispatches a plugin event, guarded for nil pluginManager.
func (a *App) emitPluginEvent(name string, data map[string]interface{}) {
	if a.pluginManager != nil {
		a.pluginManager.EmitEvent(name, data)
	}
}

// emitWatchError sends an error event to the frontend
func (a *App) emitWatchError(filePath string, errMsg string) {
	runtime.EventsEmit(a.ctx, "watch:error", map[string]string{
		"file":  filepath.Base(filePath),
		"error": errMsg,
	})
}

// emitWatchImport sends an import event to the frontend with full clip data
func (a *App) emitWatchImport(clip ClipPreview) {
	runtime.EventsEmit(a.ctx, "watch:import", clip)
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

// startup is called when the app starts
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.bridge = wailsbridge.New(ctx)

	// Initialize database
	db, err := initDB()
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	a.db = db

	// Start cleanup job for expired clips
	startCleanupJob(a.db)

	// Initialize temp directory
	if err := a.initTempDir(); err != nil {
		log.Printf("Warning: Failed to initialize temp directory: %v", err)
	} else {
		a.tempStore = NewTempClipStore(a.db, a.tempDir, defaultTempLeaseTTL, defaultTempPruneInterval)
		if err := a.tempStore.Prune(true); err != nil {
			log.Printf("Warning: Failed to prune temp clip files on startup: %v", err)
		}
	}

	// Initialize clipboard
	if err := clipboard.Init(); err != nil {
		log.Printf("Warning: Failed to initialize clipboard: %v", err)
	}

	// Initialize watcher manager
	wm, err := NewWatcherManager(a)
	if err != nil {
		log.Printf("Warning: Failed to initialize watcher manager: %v", err)
	} else {
		a.watcherManager = wm
		if err := wm.Start(); err != nil {
			log.Printf("Warning: Failed to start watcher: %v", err)
		}
	}

	// Initialize serve manager
	a.serveManager = NewServeManager(a)

	// Initialize share manager (scoped block so dataDir doesn't shadow the
	// one used by plugin init below)
	{
		dataDir, _ := getDataDir()
		sm, smErr := NewShareManager(ctx, a.db, dataDir)
		if smErr != nil {
			log.Printf("Warning: Failed to initialize share manager: %v", smErr)
		} else {
			a.shareManager = sm
			// Push Wails events to the frontend from the manager.
			sm.SetEventFn(func(name string, data ...any) {
				if len(data) == 1 {
					runtime.EventsEmit(ctx, name, data[0])
				} else {
					runtime.EventsEmit(ctx, name, data...)
				}
			})
			if err := sm.ResumeAll(); err != nil {
				log.Printf("Warning: ShareManager ResumeAll: %v", err)
			}
			sm.startSweepers()
		}
	}

	// Initialize API manager
	a.apiManager = NewAPIManager(a)

	// Initialize plugin manager
	dataDir, _ := getDataDir()
	pluginsDir := filepath.Join(dataDir, "plugins")
	pm, err := plugin.NewManager(ctx, a.db, pluginsDir)
	if err != nil {
		log.Printf("Warning: Failed to initialize plugin manager: %v", err)
	} else {
		a.pluginManager = pm
		// Wire metadata functions so plugin API delegates to App
		pm.SetMetadataFuncs(a.GetClipMetadata, a.updateClipMetadata)
		// Wire tag creation so plugin tags.create() delegates to App.CreateTag
		// (ensures subtag auto-creation of ancestor tags works)
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
		// Set up permission callback for filesystem access
		pm.SetPermissionCallback(func(pluginName, permType, requestedPath string) string {
			// Use Wails runtime dialog for folder selection
			path, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
				Title:                fmt.Sprintf("Plugin '%s' requests %s access", pluginName, permType),
				DefaultDirectory:     filepath.Dir(requestedPath),
				CanCreateDirectories: permType == "fs_write",
			})
			if err != nil || path == "" {
				return ""
			}
			return path
		})

		// Load plugins
		if err := pm.LoadPlugins(); err != nil {
			log.Printf("Warning: Failed to load plugins: %v", err)
		}

		// Emit startup event
		pm.EmitEvent("app:startup", nil)

		// Start plugin update checker
		uc := plugin.NewUpdateChecker(a.ctx, a.db, pm)
		pm.SetUpdateChecker(uc)
		interval := a.getUpdateCheckInterval()
		if interval != "disabled" {
			uc.Start(parseUpdateInterval(interval))
		}
	}
}

// shutdown is called when the app is closing
func (a *App) shutdown(ctx context.Context) {
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

func (a *App) getUpdateCheckInterval() string {
	var value string
	err := a.db.QueryRow("SELECT value FROM app_settings WHERE key = 'plugin_update_interval'").Scan(&value)
	if err != nil {
		return "24h"
	}
	return value
}

func parseUpdateInterval(interval string) time.Duration {
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

// initTempDir creates the directory for storing temporary files
func (a *App) initTempDir() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	dataDir, err := getDataDir()
	if err != nil {
		return err
	}

	a.tempDir = filepath.Join(dataDir, "clip_temp_files")
	if err := os.MkdirAll(a.tempDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp dir '%s': %w", a.tempDir, err)
	}
	log.Printf("Temporary files will be stored in %s\n", a.tempDir)
	return nil
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
	ID          int64  `json:"id"`
	ContentType string `json:"content_type"`
	Data        string `json:"data"` // base64 encoded for binary, raw for text
	Filename    string `json:"filename"`
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
	return a.getClipsInternal(archived, tagIDs, hiddenTagIDs, sortField, sortDir, true)
}

func (a *App) GetClipsDirect(archived bool, tagIDs []int64, hiddenTagIDs []int64, sortField string, sortDir string) ([]ClipPreview, error) {
	return a.getClipsInternal(archived, tagIDs, hiddenTagIDs, sortField, sortDir, false)
}

// GetFolderClips returns clips tagged with the given tag but NOT tagged with any
// descendant of that tag. Used by folder mode so clips appear only at their
// deepest folder level.
func (a *App) GetFolderClips(archived bool, tagID int64, hiddenTagIDs []int64, sortField string, sortDir string) ([]ClipPreview, error) {
	descendantIDs, err := a.getDescendantTagIDs(tagID)
	if err != nil {
		return nil, err
	}
	// Merge descendant IDs into hidden list so they act as exclusions
	excludeIDs := make([]int64, 0, len(hiddenTagIDs)+len(descendantIDs))
	excludeIDs = append(excludeIDs, hiddenTagIDs...)
	excludeIDs = append(excludeIDs, descendantIDs...)
	return a.getClipsInternal(archived, []int64{tagID}, excludeIDs, sortField, sortDir, false)
}

// GetUntaggedClips returns clips that have no tags at all.
func (a *App) GetUntaggedClips(archived bool, hiddenTagIDs []int64, sortField string, sortDir string) ([]ClipPreview, error) {
	return a.getClipsInternal(archived, nil, hiddenTagIDs, sortField, sortDir, false, true)
}

func (a *App) getClipsInternal(archived bool, tagIDs []int64, hiddenTagIDs []int64, sortField string, sortDir string, expandFilters bool, untaggedOnly ...bool) ([]ClipPreview, error) {
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
	type tagFilterGroup struct {
		ids []int64
	}
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

	var query string
	var args []interface{}

	wantUntagged := len(untaggedOnly) > 0 && untaggedOnly[0]
	untaggedClause := ""
	if wantUntagged {
		untaggedClause = "\n\t\t  AND NOT EXISTS (SELECT 1 FROM clip_tags ct2 WHERE ct2.clip_id = c.id)"
	}

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

		args = append(args, archivedInt)

		query = fmt.Sprintf(`
		SELECT %s
		FROM clips c
		WHERE %s%s%s
		  AND c.is_archived = ?
		  AND (c.expires_at IS NULL OR c.expires_at > CURRENT_TIMESTAMP)
		%s
		LIMIT %d`, selectCols, strings.Join(existsClauses, "\n\t\t  AND "), hiddenClause, untaggedClause, orderClause, defaultClipLimit)
	} else if len(effectiveHidden) > 0 {
		// No tag filters but has hidden tags - use NOT EXISTS anti-join
		hiddenPlaceholders := make([]string, len(effectiveHidden))
		for i, id := range effectiveHidden {
			hiddenPlaceholders[i] = "?"
			args = append(args, id)
		}
		args = append(args, archivedInt)

		query = fmt.Sprintf(`
		SELECT %s
		FROM clips c
		WHERE NOT EXISTS (SELECT 1 FROM clip_tags ct WHERE ct.clip_id = c.id AND ct.tag_id IN (%s))%s
		  AND c.is_archived = ?
		  AND (c.expires_at IS NULL OR c.expires_at > CURRENT_TIMESTAMP)
		%s
		LIMIT %d`, selectCols, strings.Join(hiddenPlaceholders, ","), untaggedClause, orderClause, defaultClipLimit)
	} else {
		// No filters, no hidden tags - original simple query
		args = append(args, archivedInt)
		query = fmt.Sprintf(`
		SELECT %s
		FROM clips c
		WHERE c.is_archived = ?%s AND (c.expires_at IS NULL OR c.expires_at > CURRENT_TIMESTAMP)
		%s
		LIMIT %d`, selectCols, untaggedClause, orderClause, defaultClipLimit)
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
		ID:          id,
		ContentType: contentType,
		Filename:    filename.String,
	}

	// For text content, return as-is; for binary, base64 encode
	if strings.HasPrefix(contentType, "text/") || contentType == "application/json" {
		clip.Data = string(data)
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

	contentType := file.ContentType

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
		runtime.EventsEmit(a.ctx, "clip:duplicate", map[string]interface{}{
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
func (a *App) UploadFiles(files []FileData, expirationMinutes int, autoTagID int64) error {
	var expiresAt *time.Time
	if expirationMinutes > 0 {
		t := time.Now().Add(time.Duration(expirationMinutes) * time.Minute)
		expiresAt = &t
	}

	for _, file := range files {
		// Decode base64 data
		data, err := base64.StdEncoding.DecodeString(file.Data)
		if err != nil {
			log.Printf("Failed to decode base64 data for file %s: %v", file.Name, err)
			continue
		}

		contentType := file.ContentType

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
			runtime.EventsEmit(a.ctx, "clip:duplicate", map[string]interface{}{
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

	contentHash := computeContentHash(data)
	_, err = a.db.Exec(
		"UPDATE clips SET data = ?, content_type = ?, filename = ?, content_hash = ? WHERE id = ?",
		data, contentType, filename, contentHash, id,
	)
	if err != nil {
		return fmt.Errorf("failed to update clip: %w", err)
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

	result, err := a.db.Exec("UPDATE clips SET filename = ? WHERE id = ?", newFilename, id)
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
	expiresAt := time.Now().Add(time.Duration(minutes) * time.Minute)
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

	return nil
}

// DeleteTag deletes a tag (clip_tags cascade delete handles associations)
func (a *App) DeleteTag(id int64) error {
	// If this tag has an active share, stop it first so the publication row
	// is removed before the tag row goes away. No-op if not shared.
	if a.shareManager != nil {
		_ = a.shareManager.StopShare(id)
	}

	_, err := a.db.Exec("DELETE FROM tags WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete tag: %w", err)
	}

	// Emit plugin event
	if a.pluginManager != nil {
		a.pluginManager.EmitEvent("tag:deleted", id)
	}

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

// getTagIDsForClip returns all tag IDs currently associated with a clip.
// Used by the share hook to determine which publications should receive
// the new clip.
func (a *App) getTagIDsForClip(clipID int64) ([]int64, error) {
	rows, err := a.db.Query(`SELECT tag_id FROM clip_tags WHERE clip_id = ?`, clipID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			out = append(out, id)
		}
	}
	return out, nil
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
	// Enforce tree exclusivity: a clip can only have one tag per root tree.
	// Adding a/b/d removes any existing tags under the same root (a, a/b, a/b/c, etc.)
	if err := a.removeSameTreeTags(clipID, tagID); err != nil {
		log.Printf("Failed to enforce tree exclusivity for clip %d tag %d: %v", clipID, tagID, err)
	}

	_, err := a.db.Exec("INSERT OR IGNORE INTO clip_tags (clip_id, tag_id) VALUES (?, ?)", clipID, tagID)
	if err != nil {
		return fmt.Errorf("failed to add tag to clip: %w", err)
	}

	// Emit plugin event
	if a.pluginManager != nil {
		a.pluginManager.EmitEvent("tag:added_to_clip", map[string]interface{}{
			"tag_id":  tagID,
			"clip_id": clipID,
		})
	}

	// Share hook — if this tag matches an active publication, fan the clip out
	// to connected followers. This handles the case where a clip is tagged into
	// a shared folder after initial upload.
	if a.shareManager != nil {
		go func(cid int64) {
			tagIDs, _ := a.getTagIDsForClip(cid)
			if err := a.shareManager.OnClipCreated(cid, tagIDs); err != nil {
				log.Printf("share: OnClipCreated(%d) from AddTagToClip: %v", cid, err)
			}
		}(clipID)
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
func (a *App) removeSameTreeTags(clipID, newTagID int64) error {
	// Look up the new tag's name to find its root
	var newTagName string
	if err := a.db.QueryRow("SELECT name FROM tags WHERE id = ?", newTagID).Scan(&newTagName); err != nil {
		return err
	}
	root := getRootTagName(newTagName)

	// Find all tags on this clip that share the same root tree.
	// Escape SQL LIKE wildcards in the root name so _ and % are treated literally.
	escapedRoot := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(root)
	rows, err := a.db.Query(`
		SELECT t.id FROM clip_tags ct
		INNER JOIN tags t ON ct.tag_id = t.id
		WHERE ct.clip_id = ? AND (t.name = ? OR t.name LIKE ? ESCAPE '\')`,
		clipID, root, escapedRoot+"/%")
	if err != nil {
		return err
	}
	defer rows.Close()

	var toRemove []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			continue
		}
		if id != newTagID {
			toRemove = append(toRemove, id)
		}
	}

	for _, oldTagID := range toRemove {
		if _, err := a.db.Exec("DELETE FROM clip_tags WHERE clip_id = ? AND tag_id = ?", clipID, oldTagID); err != nil {
			log.Printf("Failed to remove old tree tag %d from clip %d: %v", oldTagID, clipID, err)
		}
	}
	return nil
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
// given tag or any of its descendants.
func (a *App) getDescendantClipCount(tagID int64) (int, error) {
	var parentName string
	err := a.db.QueryRow("SELECT name FROM tags WHERE id = ?", tagID).Scan(&parentName)
	if err != nil {
		return 0, fmt.Errorf("failed to find tag %d: %w", tagID, err)
	}

	var count int
	err = a.db.QueryRow(`
		SELECT COUNT(DISTINCT ct.clip_id)
		FROM clip_tags ct
		INNER JOIN tags t ON ct.tag_id = t.id
		WHERE t.id = ? OR t.name LIKE ?
	`, tagID, parentName+"/%").Scan(&count)
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
func (a *App) GetDescendantClipCount(tagID int64) (int, error) {
	return a.getDescendantClipCount(tagID)
}

// BulkAddTag adds a tag to multiple clips
func (a *App) BulkAddTag(clipIDs []int64, tagID int64) error {
	if len(clipIDs) == 0 {
		return nil
	}

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

	// Enforce tree exclusivity before bulk insert
	for _, clipID := range clipIDs {
		if err := a.removeSameTreeTags(clipID, tagID); err != nil {
			log.Printf("Failed to enforce tree exclusivity for clip %d tag %d: %v", clipID, tagID, err)
		}
	}

	for _, clipID := range clipIDs {
		if _, err := stmt.Exec(clipID, tagID); err != nil {
			return fmt.Errorf("failed to add tag to clip %d: %w", clipID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Emit plugin events for each clip
	if a.pluginManager != nil {
		for _, clipID := range clipIDs {
			a.pluginManager.EmitEvent("tag:added_to_clip", map[string]interface{}{
				"tag_id":  tagID,
				"clip_id": clipID,
			})
		}
	}

	// Share hook — mirror AddTagToClip so bulk-tag and folder-drag paths
	// (which both route through here) fan the clips out to any connected
	// followers whose publication tag matches.
	if a.shareManager != nil {
		for _, clipID := range clipIDs {
			go func(cid int64) {
				tagIDs, _ := a.getTagIDsForClip(cid)
				if err := a.shareManager.OnClipCreated(cid, tagIDs); err != nil {
					log.Printf("share: OnClipCreated(%d) from BulkAddTag: %v", cid, err)
				}
			}(clipID)
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

	expiresAt := time.Now().Add(time.Duration(minutes) * time.Minute)

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

// BulkDownloadToFile creates a ZIP archive and saves it using native save dialog
func (a *App) BulkDownloadToFile(ids []int64) error {
	if len(ids) == 0 {
		return fmt.Errorf("no IDs provided")
	}

	// Create placeholders for the IN clause
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf("SELECT id, content_type, filename, data FROM clips WHERE id IN (%s)", strings.Join(placeholders, ","))
	rows, err := a.db.Query(query, args...)
	if err != nil {
		return fmt.Errorf("failed to query clips: %w", err)
	}
	defer rows.Close()

	// Create ZIP in memory
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	for rows.Next() {
		var id int64
		var contentType string
		var filename sql.NullString
		var data []byte

		if err := rows.Scan(&id, &contentType, &filename, &data); err != nil {
			log.Printf("Failed to scan clip for download: %v\n", err)
			continue
		}

		// Determine a filename for the zip entry
		name := filename.String
		if name == "" {
			name = fmt.Sprintf("clip_%d", id)
			exts, _ := mime.ExtensionsByType(contentType)
			if len(exts) > 0 {
				name += exts[0]
			}
		} else {
			name = fmt.Sprintf("%d_%s", id, name)
		}

		f, err := zw.Create(name)
		if err != nil {
			log.Printf("Failed to create zip entry for %s: %v\n", name, err)
			continue
		}

		if _, err := f.Write(data); err != nil {
			log.Printf("Failed to write data to zip entry for %s: %v\n", name, err)
			continue
		}
	}

	if err := zw.Close(); err != nil {
		return fmt.Errorf("failed to close zip: %w", err)
	}

	// Show save dialog
	defaultFilename := fmt.Sprintf("clips_%s.zip", time.Now().Format("20060102150405"))
	savePath, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		DefaultFilename: defaultFilename,
		Title:           "Save Clips Archive",
		Filters: []runtime.FileFilter{
			{DisplayName: "ZIP Archives", Pattern: "*.zip"},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to show save dialog: %w", err)
	}

	if savePath == "" {
		return nil // User cancelled
	}

	// Write the ZIP file
	if err := os.WriteFile(savePath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
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

// OpenClipWithDefaultApp opens a clip file with the system default application
func (a *App) OpenClipWithDefaultApp(id int64) error {
	item, err := a.prepareClipTransferItem(id, "open_default")
	if err != nil {
		return err
	}
	return openFileWithDefaultApp(item.AbsPath)
}

// OpenClipWithApp opens a clip file with a specific application.
// appPath must come from ChooseApplication (file dialog) — it is validated
// to be a real application path (.app on macOS, .exe on Windows).
func (a *App) OpenClipWithApp(id int64, appPath string) error {
	if appPath == "" {
		return fmt.Errorf("application path is required")
	}
	item, err := a.prepareClipTransferItem(id, "open_with")
	if err != nil {
		return err
	}
	return openFileWithApp(item.AbsPath, appPath)
}

// ChooseApplication opens a file dialog to select an application
func (a *App) ChooseApplication() (string, error) {
	return chooseApplicationDialog(a.ctx)
}

// DeleteAllTempFiles deletes all files from the temp directory
func (a *App) DeleteAllTempFiles() error {
	if a.tempStore == nil {
		return nil
	}
	return a.tempStore.DeleteAll()
}

// CopyToClipboard copies text to the system clipboard
func (a *App) CopyToClipboard(text string) error {
	clipboard.Write(clipboard.FmtText, []byte(text))
	return nil
}

// GetClipboardText gets text from the system clipboard
func (a *App) GetClipboardText() (string, error) {
	data := clipboard.Read(clipboard.FmtText)
	return string(data), nil
}

// GetClipboardImage gets an image from the system clipboard
// Returns base64 data and content type
func (a *App) GetClipboardImage() (string, string, error) {
	data := clipboard.Read(clipboard.FmtImage)
	if len(data) == 0 {
		return "", "", fmt.Errorf("no image in clipboard")
	}
	return base64.StdEncoding.EncodeToString(data), "image/png", nil
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

// SaveClipToFile saves a single clip to file using native save dialog
func (a *App) SaveClipToFile(id int64) error {
	var data []byte
	var filename sql.NullString
	var contentType string

	row := a.db.QueryRow("SELECT data, filename, content_type FROM clips WHERE id = ?", id)
	if err := row.Scan(&data, &filename, &contentType); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("clip not found")
		}
		return fmt.Errorf("failed to get clip: %w", err)
	}

	// Determine default filename
	defaultFilename := filename.String
	if defaultFilename == "" {
		defaultFilename = fmt.Sprintf("clip_%d", id)
		exts, _ := mime.ExtensionsByType(contentType)
		if len(exts) > 0 {
			defaultFilename += exts[0]
		}
	}

	// Show save dialog
	savePath, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		DefaultFilename: defaultFilename,
		Title:           "Save Clip",
	})
	if err != nil {
		return fmt.Errorf("failed to show save dialog: %w", err)
	}

	if savePath == "" {
		return nil // User cancelled
	}

	// Write the file
	if err := os.WriteFile(savePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// ShowCreateBackupDialog opens a save dialog and creates a backup
func (a *App) ShowCreateBackupDialog() (string, error) {
	defaultFilename := fmt.Sprintf("mahpastes-backup-%s.zip", time.Now().Format("2006-01-02"))

	savePath, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		DefaultFilename: defaultFilename,
		Title:           "Create Backup",
		Filters: []runtime.FileFilter{
			{DisplayName: "ZIP Archives", Pattern: "*.zip"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to show save dialog: %w", err)
	}

	if savePath == "" {
		return "", nil // User cancelled
	}

	if err := a.CreateBackup(savePath); err != nil {
		return "", err
	}

	return savePath, nil
}

// ShowRestoreBackupDialog opens a file picker and validates the selected backup
func (a *App) ShowRestoreBackupDialog() (*BackupManifest, string, error) {
	openPath, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Backup to Restore",
		Filters: []runtime.FileFilter{
			{DisplayName: "ZIP Archives", Pattern: "*.zip"},
		},
	})
	if err != nil {
		return nil, "", fmt.Errorf("failed to show open dialog: %w", err)
	}

	if openPath == "" {
		return nil, "", nil // User cancelled
	}

	manifest, err := ValidateBackup(openPath)
	if err != nil {
		return nil, "", err
	}

	return manifest, openPath, nil
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

// SelectFolder opens a native folder picker dialog
func (a *App) SelectFolder() (string, error) {
	path, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Folder to Watch",
	})
	if err != nil {
		return "", fmt.Errorf("failed to open folder dialog: %w", err)
	}
	return path, nil
}

// IsDirectory checks if a path is a directory
func (a *App) IsDirectory(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
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

// updateClipMetadata performs an atomic read-modify-write of clip metadata
// inside a transaction. The modify function receives the current metadata and
// mutates it in place; returning an error aborts the transaction.
func (a *App) updateClipMetadata(clipID int64, modify func(meta map[string]string) error) error {
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
	return a.updateClipMetadata(clipID, func(meta map[string]string) error {
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
	return a.updateClipMetadata(clipID, func(meta map[string]string) error {
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
	return a.updateClipMetadata(clipID, func(meta map[string]string) error {
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
			GlobalActions:   []PluginUIAction{},
		}, nil
	}

	response := &UIActionsResponse{
		LightboxButtons: []PluginUIAction{},
		CardActions:     []PluginUIAction{},
		GlobalActions:   []PluginUIAction{},
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

	var p *plugin.Plugin
	var err error

	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		cachedSource := a.pluginManager.PopPendingInstall(source)
		if cachedSource != "" {
			p, err = a.pluginManager.ImportPluginFromSource(cachedSource, source)
		} else {
			p, err = a.pluginManager.ImportPluginFromURL(source)
		}
	} else {
		p, err = a.pluginManager.ImportPlugin(source)
	}
	if err != nil {
		return nil, err
	}

	return pluginToInfo(p), nil
}

// executePluginAction calls a plugin's on_ui_action handler.
func (a *App) executePluginAction(pluginID int64, actionID string, clipIDs []int64, options map[string]interface{}) (*ActionResult, error) {
	if a.pluginManager == nil {
		return &ActionResult{Success: false, Error: "plugin manager not initialized"}, nil
	}

	result, err := a.pluginManager.ExecuteUIAction(pluginID, actionID, clipIDs, options)
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
