package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"go-clipboard/plugin"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// PluginService handles plugin-related operations
// This is a separate struct to work around Wails method binding limits
type PluginService struct {
	app *App
}

// NewPluginService creates a new plugin service
func NewPluginService(app *App) *PluginService {
	return &PluginService{app: app}
}

// PluginInfo represents a plugin for the frontend
type PluginInfo struct {
	ID          int64                 `json:"id"`
	Name        string                `json:"name"`
	Version     string                `json:"version"`
	Description string                `json:"description"`
	Author      string                `json:"author"`
	Enabled     bool                  `json:"enabled"`
	Status      string                `json:"status"`
	Events      []string              `json:"events"`
	Settings    []plugin.SettingField `json:"settings"`
}

// PluginUIAction represents a UI action with plugin context
type PluginUIAction struct {
	PluginID   int64              `json:"plugin_id"`
	PluginName string             `json:"plugin_name"`
	ID         string             `json:"id"`
	Label      string             `json:"label"`
	Icon       string             `json:"icon,omitempty"`
	Async      bool               `json:"async,omitempty"`
	Options    []plugin.FormField `json:"options,omitempty"`
	FileTypes  []string           `json:"file_types,omitempty"`
	MaxSize    int64              `json:"max_size,omitempty"`
}

// UIActionsResponse contains all plugin UI actions
type UIActionsResponse struct {
	LightboxButtons []PluginUIAction `json:"lightbox_buttons"`
	CardActions     []PluginUIAction `json:"card_actions"`
}

// ActionResult is an alias for plugin.ActionResult to avoid duplication
type ActionResult = plugin.ActionResult

// PluginPreview is an alias for plugin.PluginPreview
type PluginPreview = plugin.PluginPreview

// GetPlugins returns all plugins
func (s *PluginService) GetPlugins() ([]PluginInfo, error) {
	if s.app.db == nil {
		return []PluginInfo{}, nil
	}

	rows, err := s.app.db.Query(`
		SELECT id, name, version, enabled, status
		FROM plugins ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query plugins: %w", err)
	}
	defer rows.Close()

	var plugins []PluginInfo
	for rows.Next() {
		var p PluginInfo
		var enabled int
		if err := rows.Scan(&p.ID, &p.Name, &p.Version, &enabled, &p.Status); err != nil {
			log.Printf("GetPlugins: failed to scan row: %v", err)
			continue
		}
		p.Enabled = enabled == 1

		// Get additional info from loaded plugin if available
		if s.app.pluginManager != nil {
			for _, loaded := range s.app.pluginManager.GetPlugins() {
				if loaded.ID == p.ID && loaded.Manifest != nil {
					p.Description = loaded.Manifest.Description
					p.Author = loaded.Manifest.Author
					p.Events = loaded.Manifest.Events
					p.Settings = loaded.Manifest.Settings
					break
				}
			}
		}

		plugins = append(plugins, p)
	}

	if plugins == nil {
		plugins = []PluginInfo{}
	}
	return plugins, nil
}

// ImportPlugin opens a file dialog and returns a preview for review.
// The frontend should call ConfirmPluginInstall(path) after user approves.
func (s *PluginService) ImportPlugin() (*PluginPreview, error) {
	path, err := runtime.OpenFileDialog(s.app.ctx, runtime.OpenDialogOptions{
		Title: "Select Plugin File",
		Filters: []runtime.FileFilter{
			{DisplayName: "Lua Scripts", Pattern: "*.lua"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open file dialog: %w", err)
	}
	if path == "" {
		return nil, nil // User cancelled
	}

	return plugin.PreviewFromFile(path)
}

// EnablePlugin enables a plugin
func (s *PluginService) EnablePlugin(id int64) error {
	if s.app.pluginManager == nil {
		return fmt.Errorf("plugin manager not initialized")
	}
	return s.app.pluginManager.EnablePlugin(id)
}

// DisablePlugin disables a plugin
func (s *PluginService) DisablePlugin(id int64) error {
	if s.app.pluginManager == nil {
		return fmt.Errorf("plugin manager not initialized")
	}
	return s.app.pluginManager.DisablePlugin(id)
}

// RemovePlugin removes a plugin
func (s *PluginService) RemovePlugin(id int64) error {
	if s.app.pluginManager == nil {
		return fmt.Errorf("plugin manager not initialized")
	}
	return s.app.pluginManager.RemovePlugin(id)
}

// GetPluginPermissions returns permissions granted to a plugin
func (s *PluginService) GetPluginPermissions(id int64) ([]map[string]string, error) {
	if s.app.db == nil {
		return []map[string]string{}, nil
	}

	rows, err := s.app.db.Query(`
		SELECT permission_type, path, granted_at
		FROM plugin_permissions WHERE plugin_id = ?
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var perms []map[string]string
	for rows.Next() {
		var permType, path string
		var grantedAt string
		if err := rows.Scan(&permType, &path, &grantedAt); err != nil {
			log.Printf("GetPluginPermissions: failed to scan row: %v", err)
			continue
		}
		perms = append(perms, map[string]string{
			"type":       permType,
			"path":       path,
			"granted_at": grantedAt,
		})
	}

	if perms == nil {
		perms = []map[string]string{}
	}
	return perms, nil
}

// RevokePluginPermission revokes a filesystem permission
func (s *PluginService) RevokePluginPermission(pluginID int64, permType, path string) error {
	if s.app.db == nil {
		return fmt.Errorf("database not initialized")
	}

	_, err := s.app.db.Exec(`
		DELETE FROM plugin_permissions
		WHERE plugin_id = ? AND permission_type = ? AND path = ?
	`, pluginID, permType, path)
	return err
}

// PreviewPluginFromURL fetches a plugin URL and returns a preview for review.
// The fetched content is cached so ConfirmPluginInstall uses the reviewed bytes.
func (s *PluginService) PreviewPluginFromURL(rawURL string) (*PluginPreview, error) {
	source, err := plugin.FetchPluginSource(rawURL)
	if err != nil {
		return nil, err
	}
	preview, err := plugin.PreviewFromSource(source, rawURL)
	if err != nil {
		return nil, err
	}
	// Cache the fetched content so ConfirmPluginInstall installs exactly what was reviewed
	if s.app.pluginManager != nil {
		s.app.pluginManager.StorePendingInstall(rawURL, source)
	}
	return preview, nil
}

// PreviewPluginFromPath reads a plugin file and returns a preview for review
func (s *PluginService) PreviewPluginFromPath(path string) (*PluginPreview, error) {
	return plugin.PreviewFromFile(path)
}

// ConfirmPluginInstall installs a plugin after user has reviewed permissions.
// Source can be a URL (http:// or https://) or a local file path.
// For URLs, installs the exact content that was previewed (not a re-fetch).
func (s *PluginService) ConfirmPluginInstall(source string) (*PluginInfo, error) {
	if s.app.pluginManager == nil {
		return nil, fmt.Errorf("plugin manager not initialized")
	}

	var p *plugin.Plugin
	var err error

	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		// Use cached content from preview to avoid TOCTOU: install exactly what was reviewed
		cachedSource := s.app.pluginManager.PopPendingInstall(source)
		if cachedSource != "" {
			p, err = s.app.pluginManager.ImportPluginFromSource(cachedSource, source)
		} else {
			// Fallback: no cached content (e.g. called without preview), re-fetch
			p, err = s.app.pluginManager.ImportPluginFromURL(source)
		}
	} else {
		p, err = s.app.pluginManager.ImportPlugin(source)
	}
	if err != nil {
		return nil, err
	}

	return pluginToInfo(p), nil
}

// Helper function to convert plugin.Plugin to PluginInfo
func pluginToInfo(p *plugin.Plugin) *PluginInfo {
	info := &PluginInfo{
		ID:      p.ID,
		Name:    p.Name,
		Version: p.Version,
		Enabled: p.Enabled,
		Status:  p.Status,
	}
	if p.Manifest != nil {
		info.Description = p.Manifest.Description
		info.Author = p.Manifest.Author
		info.Events = p.Manifest.Events
		info.Settings = p.Manifest.Settings
	}
	return info
}

// TryAcquireModalGuard attempts to acquire the app-wide modal guard for showing
// a plugin result modal from the frontend. Returns true if acquired.
func (s *PluginService) TryAcquireModalGuard() bool {
	if s.app.pluginManager == nil {
		return false
	}
	return s.app.pluginManager.TryAcquireModalGuard()
}

// IsPluginURLAllowed checks whether a URL is permitted by the plugin's network allowlist
func (s *PluginService) IsPluginURLAllowed(pluginID int64, rawURL string, method string) bool {
	if s.app.pluginManager == nil {
		return false
	}
	return s.app.pluginManager.IsPluginURLAllowed(pluginID, rawURL, method)
}

// GetPluginStorage retrieves a value from a plugin's storage
func (s *PluginService) GetPluginStorage(pluginID int64, key string) (string, error) {
	if s.app.db == nil {
		return "", fmt.Errorf("database not initialized")
	}

	var value string
	err := s.app.db.QueryRow(`
		SELECT value FROM plugin_storage WHERE plugin_id = ? AND key = ?
	`, pluginID, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil // Key not found, return empty string
	}
	if err != nil {
		return "", err
	}
	return value, nil
}

// ImportPluginFromPath imports a plugin from a file path (for testing/CLI use)
func (s *PluginService) ImportPluginFromPath(path string) (*PluginInfo, error) {
	if s.app.pluginManager == nil {
		return nil, fmt.Errorf("plugin manager not initialized")
	}

	p, err := s.app.pluginManager.ImportPlugin(path)
	if err != nil {
		return nil, err
	}

	return pluginToInfo(p), nil
}

// SetPluginStorage sets a value in a plugin's storage (for testing)
func (s *PluginService) SetPluginStorage(pluginID int64, key, value string) error {
	if s.app.db == nil {
		return fmt.Errorf("database not initialized")
	}

	_, err := s.app.db.Exec(`
		INSERT INTO plugin_storage (plugin_id, key, value)
		VALUES (?, ?, ?)
		ON CONFLICT (plugin_id, key) DO UPDATE SET value = ?
	`, pluginID, key, value, value)
	return err
}

// GetAllPluginStorage retrieves all storage key-value pairs for a plugin
func (s *PluginService) GetAllPluginStorage(pluginID int64) (map[string]string, error) {
	if s.app.db == nil {
		return map[string]string{}, nil
	}

	rows, err := s.app.db.Query(`
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

// GetPluginUIActions returns all UI actions from enabled plugins
func (s *PluginService) GetPluginUIActions() (*UIActionsResponse, error) {
	if s.app.pluginManager == nil {
		return &UIActionsResponse{
			LightboxButtons: []PluginUIAction{},
			CardActions:     []PluginUIAction{},
		}, nil
	}

	response := &UIActionsResponse{
		LightboxButtons: []PluginUIAction{},
		CardActions:     []PluginUIAction{},
	}

	plugins := s.app.pluginManager.GetPlugins()
	for _, p := range plugins {
		if !p.Enabled || p.Manifest == nil || p.Manifest.UI == nil {
			continue
		}

		// Add lightbox buttons
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

		// Add card actions
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
	}

	return response, nil
}

// ExecutePluginAction calls a plugin's on_ui_action handler
func (s *PluginService) ExecutePluginAction(pluginID int64, actionID string, clipIDs []int64, options map[string]interface{}) (*ActionResult, error) {
	if s.app.pluginManager == nil {
		return &ActionResult{Success: false, Error: "plugin manager not initialized"}, nil
	}

	result, err := s.app.pluginManager.ExecuteUIAction(pluginID, actionID, clipIDs, options)
	if err != nil {
		return &ActionResult{Success: false, Error: err.Error()}, nil
	}

	return result, nil
}

// GetUpdateCheckInterval returns the current plugin update check interval setting.
func (s *PluginService) GetUpdateCheckInterval() (string, error) {
	if s.app.db == nil {
		return "24h", nil
	}
	var value string
	err := s.app.db.QueryRow("SELECT value FROM app_settings WHERE key = 'plugin_update_interval'").Scan(&value)
	if err != nil {
		return "24h", nil
	}
	return value, nil
}

// SetUpdateCheckInterval updates the plugin update check interval and restarts the checker.
func (s *PluginService) SetUpdateCheckInterval(interval string) error {
	valid := map[string]bool{"startup": true, "6h": true, "24h": true, "disabled": true}
	if !valid[interval] {
		return fmt.Errorf("invalid interval %q: must be startup, 6h, 24h, or disabled", interval)
	}

	if s.app.db == nil {
		return fmt.Errorf("database not initialized")
	}

	_, err := s.app.db.Exec(`
		INSERT INTO app_settings (key, value) VALUES ('plugin_update_interval', ?)
		ON CONFLICT(key) DO UPDATE SET value = ?
	`, interval, interval)
	if err != nil {
		return err
	}

	if s.app.pluginManager != nil {
		uc := s.app.pluginManager.GetUpdateChecker()
		if uc != nil {
			if interval == "disabled" {
				uc.Stop()
			} else {
				uc.Start(parseUpdateInterval(interval))
			}
		}
	}

	return nil
}

// CheckForUpdates triggers an immediate update check and returns results.
func (s *PluginService) CheckForUpdates() ([]*plugin.PluginUpdateInfo, error) {
	if s.app.pluginManager == nil {
		return []*plugin.PluginUpdateInfo{}, nil
	}
	uc := s.app.pluginManager.GetUpdateChecker()
	if uc == nil {
		return []*plugin.PluginUpdateInfo{}, nil
	}
	uc.CheckAll()
	return uc.GetUpdates(), nil
}

// UpdateResult represents the result of an update attempt.
type UpdateResult struct {
	Success     bool           `json:"success"`
	NeedsReview bool           `json:"needs_review"`
	Preview     *PluginPreview `json:"preview,omitempty"`
	PluginInfo  *PluginInfo    `json:"plugin_info,omitempty"`
	Error       string         `json:"error,omitempty"`
}

// UpdatePlugin fetches the latest version from the source URL, compares permissions,
// and either applies the update or returns a preview for permission re-review.
func (s *PluginService) UpdatePlugin(pluginID int64) (*UpdateResult, error) {
	if s.app.pluginManager == nil {
		return &UpdateResult{Error: "plugin manager not initialized"}, nil
	}

	var sourceURL, currentVersion string
	err := s.app.db.QueryRow("SELECT source_url, version FROM plugins WHERE id = ?", pluginID).Scan(&sourceURL, &currentVersion)
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

	// Block downgrades and same-version "updates"
	if !plugin.IsNewerVersion(currentVersion, remoteManifest.Version) {
		return &UpdateResult{Error: fmt.Sprintf("remote version %s is not newer than installed %s", remoteManifest.Version, currentVersion)}, nil
	}

	// Get current manifest for permission comparison
	s.app.pluginManager.RLock()
	loaded, ok := s.app.pluginManager.GetPluginByID(pluginID)
	s.app.pluginManager.RUnlock()

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
		s.app.pluginManager.StorePendingUpdate(pluginID, source)
		return &UpdateResult{NeedsReview: true, Preview: preview}, nil
	}

	info, err := s.applyPluginUpdate(pluginID, source, remoteManifest, sourceURL)
	if err != nil {
		return &UpdateResult{Error: err.Error()}, nil
	}

	if uc := s.app.pluginManager.GetUpdateChecker(); uc != nil {
		uc.ClearUpdate(pluginID)
	}

	return &UpdateResult{Success: true, PluginInfo: info}, nil
}

// ConfirmPluginUpdate applies a pending update after the user approves permission changes.
func (s *PluginService) ConfirmPluginUpdate(pluginID int64) (*PluginInfo, error) {
	if s.app.pluginManager == nil {
		return nil, fmt.Errorf("plugin manager not initialized")
	}

	source := s.app.pluginManager.PopPendingUpdate(pluginID)
	if source == "" {
		return nil, fmt.Errorf("no pending update for plugin %d", pluginID)
	}

	manifest, err := plugin.ParseManifest(source)
	if err != nil {
		return nil, fmt.Errorf("invalid plugin source: %w", err)
	}

	var sourceURL string
	s.app.db.QueryRow("SELECT source_url FROM plugins WHERE id = ?", pluginID).Scan(&sourceURL)

	info, err := s.applyPluginUpdate(pluginID, source, manifest, sourceURL)
	if err != nil {
		return nil, err
	}

	if uc := s.app.pluginManager.GetUpdateChecker(); uc != nil {
		uc.ClearUpdate(pluginID)
	}

	return info, nil
}

func (s *PluginService) applyPluginUpdate(pluginID int64, source string, manifest *plugin.Manifest, sourceURL string) (*PluginInfo, error) {
	var filename string
	err := s.app.db.QueryRow("SELECT filename FROM plugins WHERE id = ?", pluginID).Scan(&filename)
	if err != nil {
		return nil, fmt.Errorf("plugin not found: %w", err)
	}

	destPath := filepath.Join(s.app.pluginManager.PluginsDir(), filename)

	// Backup original file for rollback
	oldContent, readErr := os.ReadFile(destPath)

	// Write new source to file first (before unloading)
	if err := os.WriteFile(destPath, []byte(source), 0644); err != nil {
		return nil, fmt.Errorf("failed to write updated plugin: %w", err)
	}

	// Now unload the old plugin and attempt reload
	s.app.pluginManager.UnloadPlugin(pluginID)

	p := &plugin.Plugin{
		ID: pluginID, Filename: filename, Name: manifest.Name,
		Version: manifest.Version, Enabled: true, Status: "enabled",
	}
	if err := s.app.pluginManager.LoadPluginPublic(p); err != nil {
		// Rollback: restore old file and reload previous version
		if readErr == nil {
			_ = os.WriteFile(destPath, oldContent, 0644)
			oldP := &plugin.Plugin{
				ID: pluginID, Filename: filename, Enabled: true, Status: "enabled",
			}
			_ = s.app.pluginManager.LoadPluginPublic(oldP)
		}
		return nil, fmt.Errorf("failed to reload plugin (rolled back): %w", err)
	}

	// Plugin loaded successfully — now update DB
	_, err = s.app.db.Exec(`
		UPDATE plugins SET name = ?, version = ?, enabled = 1, status = 'enabled', error_count = 0, source_url = ?
		WHERE id = ?
	`, manifest.Name, manifest.Version, sourceURL, pluginID)
	if err != nil {
		return nil, fmt.Errorf("failed to update plugin record: %w", err)
	}

	return pluginToInfo(p), nil
}
