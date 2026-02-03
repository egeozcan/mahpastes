package main

import (
	"database/sql"
	"fmt"

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
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	Enabled     bool     `json:"enabled"`
	Status      string   `json:"status"`
	Events      []string `json:"events"`
}

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
	}
	return info
}

// Ensure sql.DB is used (to avoid import error if db operations are simplified)
var _ *sql.DB
