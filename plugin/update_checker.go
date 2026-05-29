package plugin

import (
	"context"
	"database/sql"
	"log"
	"sync"
	"time"

	"go-clipboard/internal/bridgeiface"
)

// PluginUpdateInfo represents an available update for a plugin.
type PluginUpdateInfo struct {
	PluginID             int64  `json:"plugin_id"`
	CurrentVersion       string `json:"current_version"`
	NewVersion           string `json:"new_version"`
	HasPermissionChanges bool   `json:"has_permission_changes"`
}

// CheckPluginUpdate fetches the latest version from a URL and compares it
// against the current version. Returns nil if no update is available.
func CheckPluginUpdate(sourceURL, currentVersion string, currentManifest *Manifest) (*PluginUpdateInfo, error) {
	source, err := FetchPluginSource(sourceURL)
	if err != nil {
		return nil, err
	}

	remoteManifest, err := ParseManifest(source)
	if err != nil {
		return nil, err
	}

	if !IsNewerVersion(currentVersion, remoteManifest.Version) {
		return nil, nil
	}

	hasChanges := ManifestsHavePermissionChanges(currentManifest, remoteManifest)

	return &PluginUpdateInfo{
		CurrentVersion:       currentVersion,
		NewVersion:           remoteManifest.Version,
		HasPermissionChanges: hasChanges,
	}, nil
}

// UpdateChecker periodically checks for plugin updates.
type UpdateChecker struct {
	ctx     context.Context    // used for context.WithCancel; not passed to the runtime
	bridge  bridgeiface.Bridge // used for frontend event emits
	db      *sql.DB
	manager *Manager
	mu      sync.Mutex
	cancel  context.CancelFunc
	updates map[int64]*PluginUpdateInfo
}

// NewUpdateChecker creates a new update checker.
func NewUpdateChecker(ctx context.Context, bridge bridgeiface.Bridge, db *sql.DB, manager *Manager) *UpdateChecker {
	return &UpdateChecker{
		ctx:     ctx,
		bridge:  bridge,
		db:      db,
		manager: manager,
		updates: make(map[int64]*PluginUpdateInfo),
	}
}

// Start begins periodic update checking with the given interval.
// Pass 0 for one-shot check (startup only).
func (uc *UpdateChecker) Start(interval time.Duration) {
	uc.Stop()

	ctx, cancel := context.WithCancel(uc.ctx)
	uc.mu.Lock()
	uc.cancel = cancel
	uc.mu.Unlock()

	go func() {
		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			return
		}

		uc.CheckAll()

		if interval <= 0 {
			return
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				uc.CheckAll()
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Stop cancels the periodic checker.
func (uc *UpdateChecker) Stop() {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	if uc.cancel != nil {
		uc.cancel()
		uc.cancel = nil
	}
}

// GetUpdates returns all currently known available updates.
func (uc *UpdateChecker) GetUpdates() []*PluginUpdateInfo {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	result := make([]*PluginUpdateInfo, 0, len(uc.updates))
	for _, info := range uc.updates {
		result = append(result, info)
	}
	return result
}

// ClearUpdate removes an update entry (after it's been applied).
func (uc *UpdateChecker) ClearUpdate(pluginID int64) {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	delete(uc.updates, pluginID)
}

// CheckAll checks all URL-installed plugins for updates.
func (uc *UpdateChecker) CheckAll() {
	rows, err := uc.db.Query(`
		SELECT id, version, source_url FROM plugins
		WHERE source_url != '' AND enabled = 1
	`)
	if err != nil {
		log.Printf("UpdateChecker: failed to query plugins: %v", err)
		return
	}
	defer rows.Close()

	type pluginRow struct {
		id        int64
		version   string
		sourceURL string
	}
	var plugins []pluginRow
	for rows.Next() {
		var p pluginRow
		if err := rows.Scan(&p.id, &p.version, &p.sourceURL); err != nil {
			continue
		}
		plugins = append(plugins, p)
	}

	for _, p := range plugins {
		uc.manager.mu.RLock()
		loaded, ok := uc.manager.plugins[p.id]
		uc.manager.mu.RUnlock()

		var currentManifest *Manifest
		if ok && loaded.Manifest != nil {
			currentManifest = loaded.Manifest
		} else {
			currentManifest = &Manifest{Version: p.version}
		}

		info, err := CheckPluginUpdate(p.sourceURL, p.version, currentManifest)
		if err != nil {
			log.Printf("UpdateChecker: failed to check %d: %v", p.id, err)
			continue
		}

		if info != nil {
			info.PluginID = p.id
			uc.mu.Lock()
			uc.updates[p.id] = info
			uc.mu.Unlock()

			uc.bridge.Emit("plugin:update_available", info)
			log.Printf("UpdateChecker: update available for plugin %d: %s -> %s", p.id, info.CurrentVersion, info.NewVersion)
		}
	}
}
