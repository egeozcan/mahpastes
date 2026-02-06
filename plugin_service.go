package main

import (
	"database/sql"
	"fmt"
	"log"

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
	Options    []plugin.FormField `json:"options,omitempty"`
}

// UIActionsResponse contains all plugin UI actions
type UIActionsResponse struct {
	LightboxButtons []PluginUIAction `json:"lightbox_buttons"`
	CardActions     []PluginUIAction `json:"card_actions"`
}

// ActionResult is an alias for plugin.ActionResult to avoid duplication
type ActionResult = plugin.ActionResult

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

// ImportPlugin imports a plugin from a file path
func (s *PluginService) ImportPlugin() (*PluginInfo, error) {
	if s.app.pluginManager == nil {
		return nil, fmt.Errorf("plugin manager not initialized")
	}

	// Open file dialog
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

	p, err := s.app.pluginManager.ImportPlugin(path)
	if err != nil {
		return nil, err
	}

	return pluginToInfo(p), nil
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
				Options:    btn.Options,
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
				Options:    action.Options,
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
