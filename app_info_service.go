package main

import (
	"context"

	coreapp "go-clipboard/internal/app"
	"go-clipboard/internal/buildinfo"
	"go-clipboard/internal/wailsbridge"
)

// updateAvailableEvent is emitted when the release check finds a newer
// release; the payload is a coreapp.ReleaseInfo.
const updateAvailableEvent = "app:update_available"

// AppInfo is what the About panel and the update banner show.
type AppInfo struct {
	Version            string `json:"version"`
	Commit             string `json:"commit"`
	UpdateCheckEnabled bool   `json:"update_check_enabled"`
	// Update is the newest release found by the check, when it is newer
	// than this build.
	Update *coreapp.ReleaseInfo `json:"update"`
}

// AppInfoService exposes the app version and the opt-out release check.
type AppInfoService struct {
	core    *coreapp.App
	checker *coreapp.ReleaseChecker
}

// NewAppInfoService creates the bound service.
func NewAppInfoService(core *coreapp.App) *AppInfoService {
	return &AppInfoService{core: core, checker: coreapp.NewReleaseChecker(core, buildinfo.Current())}
}

// GetAppInfo returns the version and the current update state.
func (s *AppInfoService) GetAppInfo() AppInfo {
	return AppInfo{
		Version:            buildinfo.Current(),
		Commit:             buildinfo.Commit,
		UpdateCheckEnabled: s.core.UpdateCheckEnabled(),
		Update:             s.checker.Latest(),
	}
}

// SetUpdateCheckEnabled persists the opt-out. Turning it off forgets any
// release already found; turning it on checks right away (desktop runs
// only: headless runs never start the checker, so they never send a request).
func (s *AppInfoService) SetUpdateCheckEnabled(enabled bool) error {
	return s.checker.SetEnabled(enabled)
}

// start runs the daily check until stop.
func (s *AppInfoService) start(ctx context.Context, bridge *wailsbridge.Bridge) {
	s.checker.Start(ctx, func(rel coreapp.ReleaseInfo) {
		bridge.Emit(updateAvailableEvent, rel)
	})
}

func (s *AppInfoService) stop() {
	s.checker.Stop()
}
