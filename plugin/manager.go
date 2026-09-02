package plugin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"go-clipboard/internal/bridgeiface"
)

const (
	MaxConsecutiveErrors = 3
	// MaxSearchQueryLength caps the query handed to on_search so both the
	// desktop combobox and the REST endpoint inherit one bound.
	MaxSearchQueryLength = 256
)

// ErrUnknownSearchSource is returned when a search source was not declared by
// the plugin's manifest (settings or UI action options).
var ErrUnknownSearchSource = errors.New("unknown search source")

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

	// networkPolicy resolves the plugin's live network permissions:
	// manifest hosts plus user-granted url-setting hosts.
	networkPolicy *NetworkPolicy
}

// URLSettingPreview describes one url-typed setting for the review modal:
// the user will be asked to grant its methods to whatever host they type.
type URLSettingPreview struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Methods []string `json:"methods"`
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
	URLSettings []URLSettingPreview `json:"url_settings,omitempty"`
}

// PreviewFromManifest builds a PluginPreview from an already-parsed manifest.
// Update paths that have parsed the remote manifest themselves must use this
// (not a hand-built literal) so the review modal discloses every permission,
// including the url settings' network grants.
func PreviewFromManifest(manifest *Manifest, source string) *PluginPreview {
	preview := &PluginPreview{
		Name:        manifest.Name,
		Version:     manifest.Version,
		Description: manifest.Description,
		Author:      manifest.Author,
		Network:     manifest.Network,
		Filesystem:  manifest.Filesystem,
		Clipboard:   manifest.Clipboard,
		Events:      manifest.Events,
		Source:      source,
	}
	for _, s := range manifest.Settings {
		if s.Type == "url" && len(s.GrantsNetwork) > 0 {
			preview.URLSettings = append(preview.URLSettings, URLSettingPreview{
				Key:     s.Key,
				Label:   s.Label,
				Methods: s.GrantsNetwork,
			})
		}
	}
	return preview
}

// PreviewFromSource parses a plugin source string and returns a preview.
func PreviewFromSource(source, sourcePath string) (*PluginPreview, error) {
	manifest, err := ParseManifest(source)
	if err != nil {
		return nil, fmt.Errorf("invalid plugin: %w", err)
	}
	return PreviewFromManifest(manifest, sourcePath), nil
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
	bridge           bridgeiface.Bridge
	db               *sql.DB
	plugins          map[int64]*Plugin
	eventSubscribers map[string][]int64 // event -> plugin IDs
	scheduler        *Scheduler
	permCallback     PermissionCallback
	fsConfinement    string // when set, all plugin fs access is confined under this root (headless)
	mu               sync.RWMutex
	loading          bool // true while the initial enabled-plugin set is being loaded
	pluginsDir       string
	modalGuard       *modalGuard
	grantMu          sync.Mutex // serializes SetStorageWithGrant reconciliation per manager
	pendingUpdates   map[int64]string
	pendingInstalls  map[string]string // source URL/path -> fetched content
	updateChecker    *UpdateChecker
	metadataGet      MetadataGetFunc
	metadataUpdate   MetadataUpdateFunc
	tagCreateFn      TagCreateFunc
}

// NewManager creates a new plugin manager
func NewManager(ctx context.Context, bridge bridgeiface.Bridge, db *sql.DB, pluginsDir string) (*Manager, error) {
	// Ensure plugins directory exists
	if err := os.MkdirAll(pluginsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create plugins directory: %w", err)
	}

	m := &Manager{
		ctx:              ctx,
		bridge:           bridge,
		db:               db,
		plugins:          make(map[int64]*Plugin),
		eventSubscribers: make(map[string][]int64),
		scheduler:        NewScheduler(),
		pluginsDir:       pluginsDir,
		modalGuard:       newModalGuard(bridge),
		pendingUpdates:   make(map[int64]string),
		pendingInstalls:  make(map[string]string),
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

// SetFSConfinementRoot confines all plugin filesystem access under root. The
// headless server sets this to its data dir so plugins cannot reach files
// outside it, even via permissions the shared DB inherited from the desktop
// build. The desktop build leaves it unset (arbitrary user-approved dirs).
func (m *Manager) SetFSConfinementRoot(root string) {
	m.fsConfinement = root
}

// LoadPlugins loads all enabled plugins from the database
func (m *Manager) LoadPlugins() error {
	m.mu.Lock()
	m.loading = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.loading = false
		m.mu.Unlock()
	}()

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

	// urlKeys: settings whose value only the user may write (a url setting
	// carries the plugin's network grant; Lua must not edit it).
	urlKeys := make(map[string]bool)
	for _, s := range manifest.Settings {
		if s.Type == "url" {
			urlKeys[s.Key] = true
		}
	}
	storageAPI := NewStorageAPI(m.db, p.ID, urlKeys)
	storageAPI.Register(sandbox.GetState())

	// The network policy is live: manifest hosts plus user-granted hosts from
	// url-setting saves, so a grant takes effect without reloading the plugin.
	networkPolicy := NewNetworkPolicy(m.db, p.ID, manifest)
	p.networkPolicy = networkPolicy

	httpAPI := NewHTTPAPI(networkPolicy)
	httpAPI.Register(sandbox.GetState())
	// The sandbox learns the HTTP budget so CallSearch can scope a deadline to
	// one on_search call; other entry points leave it unset.
	sandbox.SetHTTPBudget(httpAPI.Budget())

	fsAPI := NewFilesystemAPI(m.db, p.ID, manifest.Name, manifest.Filesystem, m.permCallback, m.fsConfinement)
	fsAPI.Register(sandbox.GetState())

	utilsAPI := NewUtilsAPI(manifest.Name, manifest.Clipboard)
	utilsAPI.Register(sandbox.GetState())

	tagsAPI := NewTagsAPI(m.db, m.tagCreateFn)
	tagsAPI.Register(sandbox.GetState())

	toastAPI := NewToastAPI(m.bridge, p.ID)
	toastAPI.Register(sandbox.GetState())

	taskAPI := NewTaskAPI(m.bridge, p.ID)
	taskAPI.Register(sandbox.GetState())

	modalAPI := NewModalAPI(m.bridge, p.ID, m.modalGuard)
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
// NotifyModalAcked records that the frontend received the current plugin modal.
// In server mode the browser cannot push upstream over SSE, so the REST layer
// calls this when the web client acknowledges a modal.
func (m *Manager) NotifyModalAcked() {
	if m.modalGuard != nil {
		m.modalGuard.markAcked()
	}
}

// NotifyModalClosed releases the app-wide modal slot. Server-mode counterpart of
// the desktop "plugin:modal:closed" upstream event.
func (m *Manager) NotifyModalClosed() {
	if m.modalGuard != nil {
		m.modalGuard.release()
	}
}

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
	// UI action discovery must be atomic. Returning the plugins loaded so far
	// lets the frontend cache a partial list and miss later plugins until a
	// manual disable/enable refreshes it.
	if m.loading {
		return []*Plugin{}
	}

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
	if p.networkPolicy != nil {
		return p.networkPolicy.Allowed(domain, method) == nil
	}
	// Fallback for a plugin loaded before the policy existed: manifest only.
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

// InvalidateNetworkPolicy drops the cached network grant set of one plugin so
// a grant or revoke made through the settings panel takes effect on the next
// request.
func (m *Manager) InvalidateNetworkPolicy(pluginID int64) {
	m.mu.RLock()
	p, ok := m.plugins[pluginID]
	m.mu.RUnlock()
	if ok && p.networkPolicy != nil {
		p.networkPolicy.Invalidate()
	}
}

// URLSettingKeys returns the manifest keys declared as url-typed settings for
// a loaded plugin. Returns nil when the plugin is not loaded or declares none.
func (m *Manager) URLSettingKeys(pluginID int64) map[string]bool {
	m.mu.RLock()
	p, ok := m.plugins[pluginID]
	m.mu.RUnlock()
	if !ok || p.Manifest == nil {
		return nil
	}
	keys := make(map[string]bool)
	for _, s := range p.Manifest.Settings {
		if s.Type == "url" {
			keys[s.Key] = true
		}
	}
	if len(keys) == 0 {
		return nil
	}
	return keys
}

// SetStorageWithGrant writes a plugin storage key and, when the key is a
// url-typed setting, reconciles the plugin's network grants with the hosts
// its url settings currently point at.
//
// The grant is recorded here — the single user-facing write path, shared by
// the desktop binding and the REST endpoint — and not in the frontend, and
// never as a request-time lookup of the setting value: Lua can write storage,
// so a value-derived rule would be a one-line self-grant. On a url-key write:
//
//  1. the new value is parsed into a host (NormalizeGrantHost; a value
//     containing `*` is rejected),
//  2. a (plugin_id, 'network', host) row is inserted if absent,
//  3. this plugin's network rows whose host is no longer the value of any
//     url setting are deleted — self-healing revocation when the user
//     retargets the server. An empty value revokes everything the settings
//     granted.
func (m *Manager) SetStorageWithGrant(pluginID int64, key, value string) error {
	urlKeys := m.URLSettingKeys(pluginID)

	// Validate the new value's host up front so a rejected value (wildcard,
	// malformed) fails the save before the storage write persists it.
	if urlKeys[key] && strings.TrimSpace(value) != "" {
		if _, err := NormalizeGrantHost(value); err != nil {
			return err
		}
	}

	// Serialize per manager: concurrent saves of two url fields must not
	// interleave their read-modify-write reconciliation.
	m.grantMu.Lock()
	defer m.grantMu.Unlock()

	// One transaction: the storage write and the full grant reconciliation
	// commit together or not at all.
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		INSERT INTO plugin_storage (plugin_id, key, value)
		VALUES (?, ?, ?)
		ON CONFLICT (plugin_id, key) DO UPDATE SET value = excluded.value
	`, pluginID, key, value); err != nil {
		return err
	}

	if !urlKeys[key] {
		return tx.Commit()
	}

	// Consent is field-specific: only the host of the key the user actually
	// saved is granted (or re-approved by clearing pending_reconfirm). The
	// other url settings' hosts matter only for stale-row deletion below —
	// inserting or reactivating them here would approve hosts the user did
	// not just save, and could resurrect grants revoked from the card.
	savedHosts := make(map[string]bool)
	if strings.TrimSpace(value) != "" {
		if h, err := NormalizeGrantHost(value); err == nil {
			savedHosts[h] = true
		}
	}

	// Collect the host every url setting currently points at (the just-saved
	// key uses the new value; the others read their stored value).
	allHosts := make(map[string]bool)
	for h := range savedHosts {
		allHosts[h] = true
	}
	for k := range urlKeys {
		if k == key {
			continue
		}
		var v []byte
		if err := tx.QueryRow(
			"SELECT value FROM plugin_storage WHERE plugin_id = ? AND key = ?", pluginID, k,
		).Scan(&v); err != nil {
			continue
		}
		if strings.TrimSpace(string(v)) == "" {
			continue
		}
		h, err := NormalizeGrantHost(string(v))
		if err != nil {
			continue
		}
		allHosts[h] = true
	}

	// Grant the saved host. The table has no unique index on
	// (plugin_id, permission_type, path), so guard the insert with NOT EXISTS
	// to keep repeat saves from stacking duplicate rows. The save is the
	// user's explicit re-approval, so it also clears any pending_reconfirm
	// flag the row carried (e.g. imported from a restored backup).
	for h := range savedHosts {
		if _, err := tx.Exec(`
			INSERT INTO plugin_permissions (plugin_id, permission_type, path)
			SELECT ?, 'network', ?
			WHERE NOT EXISTS (
				SELECT 1 FROM plugin_permissions
				WHERE plugin_id = ? AND permission_type = 'network' AND path = ?
			)
		`, pluginID, h, pluginID, h); err != nil {
			return err
		}
		if _, err := tx.Exec(`
			UPDATE plugin_permissions SET pending_reconfirm = 0
			WHERE plugin_id = ? AND permission_type = 'network' AND path = ?
		`, pluginID, h); err != nil {
			return err
		}
	}

	// Revoke hosts no url setting points at anymore. Rows are exact hosts, so
	// a deleted row can never take a wildcard grant with it. Sibling settings'
	// hosts keep their rows (pending or not) — only saving that field changes
	// their consent state. Rows still pending re-approval for an abandoned
	// host are deleted like any other stale row.
	rows, err := tx.Query(
		"SELECT path FROM plugin_permissions WHERE plugin_id = ? AND permission_type = 'network'",
		pluginID,
	)
	if err != nil {
		return err
	}
	var stale []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err == nil && !allHosts[path] {
			stale = append(stale, path)
		}
	}
	rows.Close()
	for _, path := range stale {
		if _, err := tx.Exec(
			"DELETE FROM plugin_permissions WHERE plugin_id = ? AND permission_type = 'network' AND path = ?",
			pluginID, path,
		); err != nil {
			return err
		}
	}

	// A saved url key with an empty value revokes: the loop above already
	// deleted every settings-derived row.

	if err := tx.Commit(); err != nil {
		return err
	}
	m.InvalidateNetworkPolicy(pluginID)
	return nil
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
func (m *Manager) ExecuteUIAction(pluginID int64, actionID string, clipIDs []int64, options map[string]interface{}, context map[string]interface{}) (*ActionResult, error) {
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
			luaResult, err := p.Sandbox.CallUIAction(actionID, clipIDs, options, context, MaxUIActionTime)
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
					m.bridge.Emit("plugin:modal", actionResult.Modal.ToEventMap())
				} else {
					m.bridge.Emit("plugin:toast", map[string]string{
						"message": "Cannot show result — another modal is open",
						"type":    "error",
					})
				}
			} else if !actionResult.Success && actionResult.Error != "" {
				m.bridge.Emit("plugin:toast", map[string]string{
					"message": actionResult.Error,
					"type":    "error",
				})
			}
		}()
		return &ActionResult{Success: true}, nil
	}

	// Synchronous actions block and return the result
	luaResult, err := p.Sandbox.CallUIAction(actionID, clipIDs, options, context, MaxExecutionTime)
	if err != nil {
		return nil, fmt.Errorf("plugin action failed: %w", err)
	}

	result := luaResultToActionResult(luaResult)
	if result.Modal != nil {
		result.Modal.PluginID = p.ID
	}
	return result, nil
}

// declaredSearchSources collects the source names a manifest declares for
// search fields, in settings and in every UI action's options. Anything else
// is rejected before Lua runs, so a REST caller cannot invoke on_search with
// an attacker-chosen source reaching branches the UI never offers.
func declaredSearchSources(manifest *Manifest) map[string]bool {
	sources := make(map[string]bool)
	if manifest == nil {
		return sources
	}
	for _, s := range manifest.Settings {
		if s.Type == "search" && s.Source != "" {
			sources[s.Source] = true
		}
	}
	if manifest.UI != nil {
		groups := [][]UIAction{
			manifest.UI.LightboxButtons,
			manifest.UI.CardActions,
			manifest.UI.BulkActions,
			manifest.UI.GlobalActions,
		}
		for _, group := range groups {
			for _, action := range group {
				for _, field := range action.Options {
					if field.Type == "search" && field.Source != "" {
						sources[field.Source] = true
					}
				}
			}
		}
	}
	return sources
}

// SearchOptions invokes a plugin's on_search hook for a picker field. The
// source must be declared by the plugin's manifest and the query is capped at
// MaxSearchQueryLength. Returns plugin.ErrPluginBusy when the sandbox is
// already running another entry point; the picker surfaces that as a transient
// row rather than queueing.
func (m *Manager) SearchOptions(pluginID int64, source, query string) ([]Choice, error) {
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

	if !declaredSearchSources(p.Manifest)[source] {
		return nil, fmt.Errorf("%w: %s", ErrUnknownSearchSource, source)
	}

	if len(query) > MaxSearchQueryLength {
		runes := []rune(query)
		if len(runes) > MaxSearchQueryLength {
			runes = runes[:MaxSearchQueryLength]
		}
		query = string(runes)
	}

	choices, err := p.Sandbox.CallSearch(source, query, MaxSearchTime)
	if err != nil {
		if errors.Is(err, ErrPluginBusy) {
			return nil, err
		}
		// A plugin-supplied failure passes through unwrapped so the picker
		// can show the plugin's own message (e.g. a permission problem)
		// instead of a blanket "search failed".
		var searchErr *SearchError
		if errors.As(err, &searchErr) {
			return nil, searchErr
		}
		return nil, fmt.Errorf("plugin search failed: %w", err)
	}
	return choices, nil
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
	for i := range manifest.UI.BulkActions {
		if manifest.UI.BulkActions[i].ID == actionID {
			return &manifest.UI.BulkActions[i]
		}
	}
	for i := range manifest.UI.GlobalActions {
		if manifest.UI.GlobalActions[i].ID == actionID {
			return &manifest.UI.GlobalActions[i]
		}
	}
	return nil
}
