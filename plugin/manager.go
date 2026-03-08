package plugin

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	MaxConsecutiveErrors = 3
)

// ModalData represents data for a plugin result modal
type ModalData struct {
	PluginID         int64  `json:"plugin_id"`
	Title            string `json:"title"`
	Content          string `json:"content"`
	Format           string `json:"format"`
	CopyData         string `json:"copy_data,omitempty"`
	PasteData        string `json:"paste_data,omitempty"`
	PasteDataBase64  bool   `json:"paste_data_base64,omitempty"`
	PasteName        string `json:"paste_name,omitempty"`
	PasteContentType string `json:"paste_content_type,omitempty"`
}

// ToEventMap converts ModalData to a map suitable for Wails EventsEmit.
func (d *ModalData) ToEventMap() map[string]interface{} {
	return map[string]interface{}{
		"plugin_id":          d.PluginID,
		"title":              d.Title,
		"content":            d.Content,
		"format":             d.Format,
		"copy_data":          d.CopyData,
		"paste_data":         d.PasteData,
		"paste_data_base64":  d.PasteDataBase64,
		"paste_name":         d.PasteName,
		"paste_content_type": d.PasteContentType,
	}
}

// ActionResult represents the result of a plugin action execution
type ActionResult struct {
	Success      bool       `json:"success"`
	Error        string     `json:"error,omitempty"`
	ResultClipID int64      `json:"result_clip_id,omitempty"`
	Modal        *ModalData `json:"modal,omitempty"`
}

// Plugin represents a loaded plugin
type Plugin struct {
	ID       int64
	Filename string
	Name     string
	Version  string
	Enabled  bool
	Status   string
	Manifest *Manifest
	Sandbox  *Sandbox
}

// PluginPreview represents a parsed plugin manifest for review before install/update.
type PluginPreview struct {
	Name        string              `json:"name"`
	Version     string              `json:"version"`
	Description string              `json:"description"`
	Author      string              `json:"author"`
	Network     map[string][]string `json:"network"`
	Filesystem  FilesystemPerms     `json:"filesystem"`
	Clipboard   bool                `json:"clipboard"`
	Events      []string            `json:"events"`
	Source      string              `json:"source"`
}

// PreviewFromSource parses a plugin source string and returns a preview.
func PreviewFromSource(source, sourcePath string) (*PluginPreview, error) {
	manifest, err := ParseManifest(source)
	if err != nil {
		return nil, fmt.Errorf("invalid plugin: %w", err)
	}
	return &PluginPreview{
		Name:        manifest.Name,
		Version:     manifest.Version,
		Description: manifest.Description,
		Author:      manifest.Author,
		Network:     manifest.Network,
		Filesystem:  manifest.Filesystem,
		Clipboard:   manifest.Clipboard,
		Events:      manifest.Events,
		Source:      sourcePath,
	}, nil
}

// PreviewFromFile reads a plugin file and returns a preview.
func PreviewFromFile(path string) (*PluginPreview, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read plugin file: %w", err)
	}
	return PreviewFromSource(string(source), path)
}

// PreviewFromURL fetches a plugin from a URL and returns a preview.
func PreviewFromURL(rawURL string) (*PluginPreview, error) {
	source, err := FetchPluginSource(rawURL)
	if err != nil {
		return nil, err
	}
	return PreviewFromSource(source, rawURL)
}

// Manager manages all plugins
type Manager struct {
	ctx              context.Context
	db               *sql.DB
	plugins          map[int64]*Plugin
	eventSubscribers map[string][]int64 // event -> plugin IDs
	scheduler        *Scheduler
	permCallback     PermissionCallback
	mu               sync.RWMutex
	pluginsDir       string
	modalGuard       *modalGuard
	pendingUpdates  map[int64]string
	pendingInstalls map[string]string // source URL/path -> fetched content
	updateChecker   *UpdateChecker
	metadataGet    MetadataGetFunc
	metadataUpdate MetadataUpdateFunc
	tagCreateFn    TagCreateFunc
}

// NewManager creates a new plugin manager
func NewManager(ctx context.Context, db *sql.DB, pluginsDir string) (*Manager, error) {
	// Ensure plugins directory exists
	if err := os.MkdirAll(pluginsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create plugins directory: %w", err)
	}

	m := &Manager{
		ctx:              ctx,
		db:               db,
		plugins:          make(map[int64]*Plugin),
		eventSubscribers: make(map[string][]int64),
		scheduler:        NewScheduler(),
		pluginsDir:       pluginsDir,
		modalGuard:       newModalGuard(ctx),
		pendingUpdates:  make(map[int64]string),
		pendingInstalls: make(map[string]string),
	}

	return m, nil
}

// SetMetadataFuncs sets the metadata get/update functions used by the Lua API.
func (m *Manager) SetMetadataFuncs(getFn MetadataGetFunc, updateFn MetadataUpdateFunc) {
	m.metadataGet = getFn
	m.metadataUpdate = updateFn
}

// SetTagCreateFunc sets the tag creation function used by the tags Lua API.
// When set, tags.create() delegates to App.CreateTag for subtag auto-creation.
func (m *Manager) SetTagCreateFunc(fn TagCreateFunc) {
	m.tagCreateFn = fn
}

// SetPermissionCallback sets the callback for filesystem permission requests
func (m *Manager) SetPermissionCallback(callback PermissionCallback) {
	m.permCallback = callback
}

// LoadPlugins loads all enabled plugins from the database
func (m *Manager) LoadPlugins() error {
	rows, err := m.db.Query(`
		SELECT id, filename, name, version, enabled, status
		FROM plugins WHERE enabled = 1 AND status != 'error'
	`)
	if err != nil {
		return fmt.Errorf("failed to query plugins: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var p Plugin
		var enabled int
		if err := rows.Scan(&p.ID, &p.Filename, &p.Name, &p.Version, &enabled, &p.Status); err != nil {
			log.Printf("Failed to scan plugin row: %v", err)
			continue
		}
		p.Enabled = enabled == 1

		if err := m.loadPlugin(&p); err != nil {
			log.Printf("Failed to load plugin %s: %v", p.Name, err)
			m.incrementErrorCount(p.ID)
			continue
		}
	}

	return nil
}

func (m *Manager) loadPlugin(p *Plugin) error {
	// Read plugin source
	sourcePath := filepath.Join(m.pluginsDir, p.Filename)
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to read plugin file: %w", err)
	}

	// Parse manifest
	manifest, err := ParseManifest(string(source))
	if err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}
	p.Manifest = manifest

	// Create sandbox
	sandbox := NewSandbox(manifest, p.ID)

	// Register APIs
	clipsAPI := NewClipsAPI(m.db, manifest.Network)
	clipsAPI.Register(sandbox.GetState())

	storageAPI := NewStorageAPI(m.db, p.ID)
	storageAPI.Register(sandbox.GetState())

	httpAPI := NewHTTPAPI(manifest.Network)
	httpAPI.Register(sandbox.GetState())

	fsAPI := NewFilesystemAPI(m.db, p.ID, manifest.Name, manifest.Filesystem, m.permCallback)
	fsAPI.Register(sandbox.GetState())

	utilsAPI := NewUtilsAPI(manifest.Name, manifest.Clipboard)
	utilsAPI.Register(sandbox.GetState())

	tagsAPI := NewTagsAPI(m.db, m.tagCreateFn)
	tagsAPI.Register(sandbox.GetState())

	toastAPI := NewToastAPI(m.ctx, p.ID)
	toastAPI.Register(sandbox.GetState())

	taskAPI := NewTaskAPI(m.ctx, p.ID)
	taskAPI.Register(sandbox.GetState())

	modalAPI := NewModalAPI(m.ctx, p.ID, m.modalGuard)
	modalAPI.Register(sandbox.GetState())

	imageAPI := NewImageAPI(m.db)
	imageAPI.Register(sandbox.GetState())

	metadataAPI := NewMetadataAPI(m.metadataGet, m.metadataUpdate)
	metadataAPI.Register(sandbox.GetState())

	// Load the plugin source
	if err := sandbox.LoadSource(string(source)); err != nil {
		sandbox.Close()
		return fmt.Errorf("failed to load source: %w", err)
	}

	p.Sandbox = sandbox

	// Register plugin
	m.mu.Lock()
	m.plugins[p.ID] = p

	// Subscribe to events (validate and warn for unknown events)
	for _, event := range manifest.Events {
		if !IsValidEvent(event) {
			log.Printf("Warning: Plugin %s subscribes to unknown event '%s'", manifest.Name, event)
		}
		m.eventSubscribers[event] = append(m.eventSubscribers[event], p.ID)
	}
	m.mu.Unlock()

	// Register scheduled tasks
	for _, sched := range manifest.Schedules {
		m.scheduler.AddTask(p.ID, sched.Name, sched.Interval, sandbox)
	}

	log.Printf("Loaded plugin: %s v%s", manifest.Name, manifest.Version)
	return nil
}

// UnloadPlugin unloads a plugin
func (m *Manager) UnloadPlugin(pluginID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	p, ok := m.plugins[pluginID]
	if !ok {
		return
	}

	// Stop scheduled tasks
	m.scheduler.RemovePluginTasks(pluginID)

	// Close sandbox
	if p.Sandbox != nil {
		p.Sandbox.Close()
	}

	// Remove from event subscribers
	for event, subscribers := range m.eventSubscribers {
		newSubscribers := make([]int64, 0, len(subscribers))
		for _, id := range subscribers {
			if id != pluginID {
				newSubscribers = append(newSubscribers, id)
			}
		}
		m.eventSubscribers[event] = newSubscribers
	}

	delete(m.plugins, pluginID)
	log.Printf("Unloaded plugin: %s", p.Name)
}

// EmitEvent sends an event to all subscribed plugins
func (m *Manager) EmitEvent(event string, data interface{}) {
	m.mu.RLock()
	// Copy subscriber list to prevent race conditions during iteration
	subscribers := make([]int64, len(m.eventSubscribers[event]))
	copy(subscribers, m.eventSubscribers[event])
	m.mu.RUnlock()

	// Convert event name to handler name: "clip:created" -> "on_clip_created"
	handlerName := eventToHandler(event)

	for _, pluginID := range subscribers {
		m.mu.RLock()
		p, ok := m.plugins[pluginID]
		m.mu.RUnlock()

		if !ok || p.Sandbox == nil {
			continue
		}

		// Call handler with data conversion happening inside the sandbox's mutex
		if err := p.Sandbox.CallHandlerWithData(handlerName, data); err != nil {
			log.Printf("Plugin %s handler %s failed: %v", p.Name, handlerName, err)
			m.incrementErrorCount(pluginID)
		} else {
			m.resetErrorCount(pluginID)
		}
	}
}

func eventToHandler(event string) string {
	// "clip:created" -> "on_clip_created"
	// "app:startup" -> "on_startup" (app: prefix stripped for cleaner API)
	// "tag:created" -> "on_tag_created"

	// Special case: strip "app:" prefix for cleaner handler names
	if strings.HasPrefix(event, "app:") {
		return "on_" + strings.TrimPrefix(event, "app:")
	}

	// For other events, replace : with _
	return "on_" + strings.ReplaceAll(event, ":", "_")
}

func (m *Manager) incrementErrorCount(pluginID int64) {
	_, err := m.db.Exec(
		"UPDATE plugins SET error_count = error_count + 1 WHERE id = ?",
		pluginID,
	)
	if err != nil {
		return
	}

	// Check if we need to disable the plugin
	var errorCount int
	if err := m.db.QueryRow("SELECT error_count FROM plugins WHERE id = ?", pluginID).Scan(&errorCount); err != nil {
		log.Printf("Failed to get error count for plugin %d: %v", pluginID, err)
		return
	}

	if errorCount >= MaxConsecutiveErrors {
		m.db.Exec("UPDATE plugins SET status = 'error' WHERE id = ?", pluginID)
		m.UnloadPlugin(pluginID)
		log.Printf("Plugin %d disabled after %d consecutive errors", pluginID, errorCount)
	}
}

func (m *Manager) resetErrorCount(pluginID int64) {
	if _, err := m.db.Exec("UPDATE plugins SET error_count = 0 WHERE id = ?", pluginID); err != nil {
		log.Printf("Failed to reset error count for plugin %d: %v", pluginID, err)
	}
}

// ImportPlugin imports a plugin from a file path
func (m *Manager) ImportPlugin(sourcePath string) (*Plugin, error) {
	// Read source
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read plugin file: %w", err)
	}

	// Parse manifest to validate
	manifest, err := ParseManifest(string(source))
	if err != nil {
		return nil, fmt.Errorf("invalid plugin: %w", err)
	}

	// Copy to plugins directory
	filename := filepath.Base(sourcePath)
	destPath := filepath.Join(m.pluginsDir, filename)

	if err := os.WriteFile(destPath, source, 0644); err != nil {
		return nil, fmt.Errorf("failed to copy plugin: %w", err)
	}

	// Insert into database
	_, err = m.db.Exec(`
		INSERT INTO plugins (filename, name, version, enabled, status, source_url)
		VALUES (?, ?, ?, 1, 'enabled', ?)
		ON CONFLICT(filename) DO UPDATE SET
			name = excluded.name,
			version = excluded.version,
			enabled = 1,
			status = 'enabled',
			error_count = 0,
			source_url = excluded.source_url
	`, filename, manifest.Name, manifest.Version, "")
	if err != nil {
		return nil, fmt.Errorf("failed to register plugin: %w", err)
	}

	// Query for the ID (LastInsertId returns 0 on upsert update)
	var id int64
	err = m.db.QueryRow("SELECT id FROM plugins WHERE filename = ?", filename).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("failed to get plugin ID: %w", err)
	}

	// Load the plugin
	p := &Plugin{
		ID:       id,
		Filename: filename,
		Name:     manifest.Name,
		Version:  manifest.Version,
		Enabled:  true,
		Status:   "enabled",
	}

	if err := m.loadPlugin(p); err != nil {
		return nil, fmt.Errorf("failed to load plugin: %w", err)
	}

	return p, nil
}

// ImportPluginFromURL imports a plugin from a URL.
// The URL is stored as source_url for future update checks.
func (m *Manager) ImportPluginFromURL(rawURL string) (*Plugin, error) {
	source, err := FetchPluginSource(rawURL)
	if err != nil {
		return nil, err
	}

	manifest, err := ParseManifest(source)
	if err != nil {
		return nil, fmt.Errorf("invalid plugin: %w", err)
	}

	filename := filenameFromURL(rawURL, manifest.Name)
	destPath := filepath.Join(m.pluginsDir, filename)

	if err := os.WriteFile(destPath, []byte(source), 0644); err != nil {
		return nil, fmt.Errorf("failed to write plugin: %w", err)
	}

	_, err = m.db.Exec(`
		INSERT INTO plugins (filename, name, version, enabled, status, source_url)
		VALUES (?, ?, ?, 1, 'enabled', ?)
		ON CONFLICT(filename) DO UPDATE SET
			name = excluded.name,
			version = excluded.version,
			enabled = 1,
			status = 'enabled',
			error_count = 0,
			source_url = excluded.source_url
	`, filename, manifest.Name, manifest.Version, rawURL)
	if err != nil {
		return nil, fmt.Errorf("failed to register plugin: %w", err)
	}

	var id int64
	err = m.db.QueryRow("SELECT id FROM plugins WHERE filename = ?", filename).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("failed to get plugin ID: %w", err)
	}

	p := &Plugin{
		ID:       id,
		Filename: filename,
		Name:     manifest.Name,
		Version:  manifest.Version,
		Enabled:  true,
		Status:   "enabled",
	}

	if err := m.loadPlugin(p); err != nil {
		return nil, fmt.Errorf("failed to load plugin: %w", err)
	}

	return p, nil
}

// ImportPluginFromSource installs a plugin from already-fetched source content.
// The sourceURL is stored for future update checks.
func (m *Manager) ImportPluginFromSource(source, sourceURL string) (*Plugin, error) {
	manifest, err := ParseManifest(source)
	if err != nil {
		return nil, fmt.Errorf("invalid plugin: %w", err)
	}

	filename := filenameFromURL(sourceURL, manifest.Name)
	destPath := filepath.Join(m.pluginsDir, filename)

	if err := os.WriteFile(destPath, []byte(source), 0644); err != nil {
		return nil, fmt.Errorf("failed to write plugin: %w", err)
	}

	_, err = m.db.Exec(`
		INSERT INTO plugins (filename, name, version, enabled, status, source_url)
		VALUES (?, ?, ?, 1, 'enabled', ?)
		ON CONFLICT(filename) DO UPDATE SET
			name = excluded.name,
			version = excluded.version,
			enabled = 1,
			status = 'enabled',
			error_count = 0,
			source_url = excluded.source_url
	`, filename, manifest.Name, manifest.Version, sourceURL)
	if err != nil {
		return nil, fmt.Errorf("failed to register plugin: %w", err)
	}

	var id int64
	err = m.db.QueryRow("SELECT id FROM plugins WHERE filename = ?", filename).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("failed to get plugin ID: %w", err)
	}

	p := &Plugin{
		ID:       id,
		Filename: filename,
		Name:     manifest.Name,
		Version:  manifest.Version,
		Enabled:  true,
		Status:   "enabled",
	}

	if err := m.loadPlugin(p); err != nil {
		return nil, fmt.Errorf("failed to load plugin: %w", err)
	}

	return p, nil
}

func filenameFromURL(rawURL, fallbackName string) string {
	parsed, err := url.Parse(rawURL)
	if err == nil {
		base := filepath.Base(parsed.Path)
		if strings.HasSuffix(base, ".lua") {
			return base
		}
	}
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, fallbackName)
	return strings.ToLower(safe) + ".lua"
}

// GetPlugins returns all plugins
func (m *Manager) GetPlugins() []*Plugin {
	m.mu.RLock()
	defer m.mu.RUnlock()

	plugins := make([]*Plugin, 0, len(m.plugins))
	for _, p := range m.plugins {
		plugins = append(plugins, p)
	}
	return plugins
}

// TryAcquireModalGuard attempts to acquire the app-wide modal guard.
// Returns true if acquired (caller must ensure plugin:modal:closed is emitted to release).
func (m *Manager) TryAcquireModalGuard() bool {
	ok, gen := m.modalGuard.acquire()
	if ok {
		m.modalGuard.startAckTimeout(gen)
	}
	return ok
}

// IsPluginURLAllowed checks whether a URL is permitted by the plugin's network allowlist
func (m *Manager) IsPluginURLAllowed(pluginID int64, rawURL string, method string) bool {
	m.mu.RLock()
	p, ok := m.plugins[pluginID]
	m.mu.RUnlock()
	if !ok || p.Manifest == nil {
		return false
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	domain := parsed.Hostname()
	allowedMethods, ok := FindAllowedMethods(p.Manifest.Network, domain)
	if !ok {
		return false
	}
	for _, am := range allowedMethods {
		if strings.EqualFold(am, method) {
			return true
		}
	}
	return false
}

// EnablePlugin enables a plugin
func (m *Manager) EnablePlugin(pluginID int64) error {
	// Check if plugin is already loaded
	m.mu.RLock()
	_, alreadyLoaded := m.plugins[pluginID]
	m.mu.RUnlock()

	if alreadyLoaded {
		return nil // Already enabled and loaded
	}

	_, err := m.db.Exec(
		"UPDATE plugins SET enabled = 1, status = 'enabled', error_count = 0 WHERE id = ?",
		pluginID,
	)
	if err != nil {
		return err
	}

	// Load the plugin
	var p Plugin
	err = m.db.QueryRow(`
		SELECT id, filename, name, version, enabled, status
		FROM plugins WHERE id = ?
	`, pluginID).Scan(&p.ID, &p.Filename, &p.Name, &p.Version, &p.Enabled, &p.Status)
	if err != nil {
		return err
	}
	p.Enabled = true

	return m.loadPlugin(&p)
}

// DisablePlugin disables a plugin
func (m *Manager) DisablePlugin(pluginID int64) error {
	m.UnloadPlugin(pluginID)
	_, err := m.db.Exec("UPDATE plugins SET enabled = 0 WHERE id = ?", pluginID)
	return err
}

// RemovePlugin removes a plugin completely
func (m *Manager) RemovePlugin(pluginID int64) error {
	m.mu.RLock()
	p, ok := m.plugins[pluginID]
	filename := ""
	if ok {
		filename = p.Filename
	} else {
		// Get filename from DB
		m.db.QueryRow("SELECT filename FROM plugins WHERE id = ?", pluginID).Scan(&filename)
	}
	m.mu.RUnlock()

	m.UnloadPlugin(pluginID)

	// Delete from database (cascades to permissions and storage)
	if _, err := m.db.Exec("DELETE FROM plugins WHERE id = ?", pluginID); err != nil {
		return err
	}

	// Delete file
	if filename != "" {
		os.Remove(filepath.Join(m.pluginsDir, filename))
	}

	return nil
}

// Shutdown stops all plugins
func (m *Manager) Shutdown() {
	// Emit shutdown event
	m.EmitEvent("app:shutdown", nil)

	// Stop update checker
	if m.updateChecker != nil {
		m.updateChecker.Stop()
	}

	// Stop scheduler
	m.scheduler.StopAll()

	// Close all sandboxes
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, p := range m.plugins {
		if p.Sandbox != nil {
			p.Sandbox.Close()
		}
	}

	m.plugins = make(map[int64]*Plugin)
	m.eventSubscribers = make(map[string][]int64)
}

// StorePendingUpdate stores a pending update source for later confirmation.
func (m *Manager) StorePendingUpdate(pluginID int64, source string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pendingUpdates[pluginID] = source
}

// PopPendingUpdate retrieves and removes a pending update source.
func (m *Manager) PopPendingUpdate(pluginID int64) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	source := m.pendingUpdates[pluginID]
	delete(m.pendingUpdates, pluginID)
	return source
}

// StorePendingInstall caches fetched plugin content keyed by source (URL or path).
func (m *Manager) StorePendingInstall(source, content string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pendingInstalls[source] = content
}

// PopPendingInstall retrieves and removes cached plugin content for a source.
func (m *Manager) PopPendingInstall(source string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	content := m.pendingInstalls[source]
	delete(m.pendingInstalls, source)
	return content
}

// LoadPluginPublic is a public wrapper around loadPlugin for use by PluginService.
func (m *Manager) LoadPluginPublic(p *Plugin) error {
	return m.loadPlugin(p)
}

// PluginsDir returns the plugins directory path.
func (m *Manager) PluginsDir() string {
	return m.pluginsDir
}

// GetUpdateChecker returns the update checker instance.
func (m *Manager) GetUpdateChecker() *UpdateChecker {
	return m.updateChecker
}

// SetUpdateChecker sets the update checker.
func (m *Manager) SetUpdateChecker(uc *UpdateChecker) {
	m.updateChecker = uc
}

// RLock locks the manager for reading.
func (m *Manager) RLock() {
	m.mu.RLock()
}

// RUnlock unlocks the manager read lock.
func (m *Manager) RUnlock() {
	m.mu.RUnlock()
}

// GetPluginByID returns a plugin by ID. Must be called with RLock held.
func (m *Manager) GetPluginByID(id int64) (*Plugin, bool) {
	p, ok := m.plugins[id]
	return p, ok
}

// ExecuteUIAction calls a plugin's on_ui_action handler.
// If the action has async=true in the manifest, it runs in a background goroutine
// and returns immediately. The plugin should use task.start/progress/complete for feedback.
func (m *Manager) ExecuteUIAction(pluginID int64, actionID string, clipIDs []int64, options map[string]interface{}) (*ActionResult, error) {
	m.mu.RLock()
	p, ok := m.plugins[pluginID]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("plugin not found: %d", pluginID)
	}

	if !p.Enabled {
		return nil, fmt.Errorf("plugin is disabled: %s", p.Name)
	}

	if p.Sandbox == nil {
		return nil, fmt.Errorf("plugin sandbox not initialized: %s", p.Name)
	}

	// Find the action in the manifest
	action := findUIAction(p.Manifest, actionID)
	if action == nil {
		return nil, fmt.Errorf("unknown action ID: %s", actionID)
	}

	// Async actions run in a background goroutine with extended timeout
	if action.Async {
		go func() {
			luaResult, err := p.Sandbox.CallUIAction(actionID, clipIDs, options, MaxUIActionTime)
			if err != nil {
				log.Printf("Plugin %s async action %s failed: %v", p.Name, actionID, err)
				m.incrementErrorCount(pluginID)
				return
			}
			m.resetErrorCount(pluginID)
			actionResult := luaResultToActionResult(luaResult)
			if actionResult.Modal != nil {
				actionResult.Modal.PluginID = p.ID
				ok, gen := m.modalGuard.acquire()
				if ok {
					m.modalGuard.startAckTimeout(gen)
					runtime.EventsEmit(m.ctx, "plugin:modal", actionResult.Modal.ToEventMap())
				} else {
					runtime.EventsEmit(m.ctx, "plugin:toast", map[string]string{
						"message": "Cannot show result — another modal is open",
						"type":    "error",
					})
				}
			} else if !actionResult.Success && actionResult.Error != "" {
				runtime.EventsEmit(m.ctx, "plugin:toast", map[string]string{
					"message": actionResult.Error,
					"type":    "error",
				})
			}
		}()
		return &ActionResult{Success: true}, nil
	}

	// Synchronous actions block and return the result
	luaResult, err := p.Sandbox.CallUIAction(actionID, clipIDs, options, MaxExecutionTime)
	if err != nil {
		return nil, fmt.Errorf("plugin action failed: %w", err)
	}

	result := luaResultToActionResult(luaResult)
	if result.Modal != nil {
		result.Modal.PluginID = p.ID
	}
	return result, nil
}

// luaResultToActionResult converts a Lua return table to an ActionResult
func luaResultToActionResult(luaResult map[string]interface{}) *ActionResult {
	result := &ActionResult{Success: true}

	if success, ok := luaResult["success"]; ok {
		if successBool, ok := success.(bool); ok {
			result.Success = successBool
		}
	}

	if errMsg, ok := luaResult["error"]; ok {
		if errStr, ok := errMsg.(string); ok {
			result.Error = errStr
		}
	}

	if clipID, ok := luaResult["result_clip_id"]; ok {
		if id, ok := clipID.(int64); ok {
			result.ResultClipID = id
		}
	}

	if modalRaw, ok := luaResult["modal"]; ok {
		if modalMap, ok := modalRaw.(map[string]interface{}); ok {
			modal := &ModalData{}
			if v, ok := modalMap["title"].(string); ok {
				modal.Title = v
			}
			if v, ok := modalMap["content"].(string); ok {
				modal.Content = v
			}
			if v, ok := modalMap["format"].(string); ok {
				modal.Format = v
			}
			if v, ok := modalMap["copy_data"].(string); ok {
				modal.CopyData = v
			}
			if v, ok := modalMap["paste_data"].(string); ok {
				modal.PasteData = v
			}
			if v, ok := modalMap["paste_name"].(string); ok {
				modal.PasteName = v
			}
			if v, ok := modalMap["paste_content_type"].(string); ok {
				modal.PasteContentType = v
			}
			if v, ok := modalMap["paste_data_base64"].(bool); ok {
				modal.PasteDataBase64 = v
			}
			// Validate required fields and limits
			if modal.Title == "" || modal.Content == "" || modal.Format == "" {
				result.Success = false
				result.Error = "modal requires title, content, and format"
			} else if len(modal.Content) > 1<<20 || len(modal.CopyData) > 1<<20 || len(modal.PasteData) > 10<<20 {
				result.Success = false
				result.Error = "modal content/data exceeds size limit"
			} else {
				validFormats := map[string]bool{"markdown": true, "image": true, "text": true}
				if !validFormats[modal.Format] {
					result.Success = false
					result.Error = "modal format must be 'markdown', 'image', or 'text'"
				} else {
					if len(modal.Title) > 200 {
						modal.Title = modal.Title[:200]
					}
					result.Modal = modal
				}
			}
		}
	}

	return result
}

// findUIAction finds a UI action by ID in the manifest, returning nil if not found
func findUIAction(manifest *Manifest, actionID string) *UIAction {
	if manifest == nil || manifest.UI == nil {
		return nil
	}
	for i := range manifest.UI.LightboxButtons {
		if manifest.UI.LightboxButtons[i].ID == actionID {
			return &manifest.UI.LightboxButtons[i]
		}
	}
	for i := range manifest.UI.CardActions {
		if manifest.UI.CardActions[i].ID == actionID {
			return &manifest.UI.CardActions[i]
		}
	}
	for i := range manifest.UI.GlobalActions {
		if manifest.UI.GlobalActions[i].ID == actionID {
			return &manifest.UI.GlobalActions[i]
		}
	}
	return nil
}
