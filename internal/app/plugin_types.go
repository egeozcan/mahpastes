package app

import "go-clipboard/plugin"

// PluginInfo represents a plugin for API and web UI responses.
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

type UIActionsResponse struct {
	LightboxButtons []PluginUIAction `json:"lightbox_buttons"`
	CardActions     []PluginUIAction `json:"card_actions"`
	BulkActions     []PluginUIAction `json:"bulk_actions"`
	GlobalActions   []PluginUIAction `json:"global_actions"`
}

type ActionResult = plugin.ActionResult
type PluginPreview = plugin.PluginPreview

type UpdateResult struct {
	Success     bool           `json:"success"`
	NeedsReview bool           `json:"needs_review"`
	Preview     *PluginPreview `json:"preview,omitempty"`
	PluginInfo  *PluginInfo    `json:"plugin_info,omitempty"`
	Error       string         `json:"error,omitempty"`
}

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
