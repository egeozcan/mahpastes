package plugin

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	MaxConsecutiveErrors = 3
)

// ActionResult represents the result of a plugin action execution
type ActionResult struct {
	Success      bool   `json:"success"`
	Error        string `json:"error,omitempty"`
	ResultClipID int64  `json:"result_clip_id,omitempty"`
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
	}

	return m, nil
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
	clipsAPI := NewClipsAPI(m.db)
	clipsAPI.Register(sandbox.GetState())

	storageAPI := NewStorageAPI(m.db, p.ID)
	storageAPI.Register(sandbox.GetState())

	httpAPI := NewHTTPAPI(manifest.Network)
	httpAPI.Register(sandbox.GetState())

	fsAPI := NewFilesystemAPI(m.db, p.ID, manifest.Name, manifest.Filesystem, m.permCallback)
	fsAPI.Register(sandbox.GetState())

	utilsAPI := NewUtilsAPI(manifest.Name)
	utilsAPI.Register(sandbox.GetState())

	tagsAPI := NewTagsAPI(m.db)
	tagsAPI.Register(sandbox.GetState())

	toastAPI := NewToastAPI(m.ctx, p.ID)
	toastAPI.Register(sandbox.GetState())

	taskAPI := NewTaskAPI(m.ctx, p.ID)
	taskAPI.Register(sandbox.GetState())

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
	result := "on_"
	for _, c := range event {
		if c == ':' {
			result += "_"
		} else {
			result += string(c)
		}
	}
	return result
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
		INSERT INTO plugins (filename, name, version, enabled, status)
		VALUES (?, ?, ?, 1, 'enabled')
		ON CONFLICT(filename) DO UPDATE SET
			name = excluded.name,
			version = excluded.version,
			enabled = 1,
			status = 'enabled',
			error_count = 0
	`, filename, manifest.Name, manifest.Version)
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

// ExecuteUIAction calls a plugin's on_ui_action handler
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

	// Use sandbox's CallUIAction method which handles context and locking properly
	luaResult, err := p.Sandbox.CallUIAction(actionID, clipIDs, options)
	if err != nil {
		return nil, fmt.Errorf("plugin action failed: %w", err)
	}

	// Convert Lua result to ActionResult
	result := &ActionResult{Success: true}

	// Check if the plugin explicitly set success to false
	if success, ok := luaResult["success"]; ok {
		if successBool, ok := success.(bool); ok {
			result.Success = successBool
		}
	}

	// Extract error message if present
	if errMsg, ok := luaResult["error"]; ok {
		if errStr, ok := errMsg.(string); ok {
			result.Error = errStr
		}
	}

	// Extract result_clip_id if present
	if clipID, ok := luaResult["result_clip_id"]; ok {
		if id, ok := clipID.(int64); ok {
			result.ResultClipID = id
		}
	}

	return result, nil
}
