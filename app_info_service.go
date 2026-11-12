package main

import (
	"context"
	"log"
	"sync"
	"time"

	coreapp "go-clipboard/internal/app"
	"go-clipboard/internal/buildinfo"
	"go-clipboard/internal/wailsbridge"
)

// updateAvailableEvent is emitted when the release check finds a newer
// release; the payload is a coreapp.ReleaseInfo.
const updateAvailableEvent = "app:update_available"

// updateCheckStartDelay lets the window finish loading before the first
// check so the event is not emitted into the void.
const updateCheckStartDelay = 20 * time.Second

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
	core *coreapp.App

	mu     sync.Mutex
	bridge *wailsbridge.Bridge
	latest *coreapp.ReleaseInfo
	cancel context.CancelFunc
}

// NewAppInfoService creates the bound service.
func NewAppInfoService(core *coreapp.App) *AppInfoService {
	return &AppInfoService{core: core}
}

// GetAppInfo returns the version and the current update state.
func (s *AppInfoService) GetAppInfo() AppInfo {
	s.mu.Lock()
	latest := s.latest
	s.mu.Unlock()
	enabled := s.core.UpdateCheckEnabled()
	if !enabled {
		latest = nil
	}
	return AppInfo{
		Version:            buildinfo.Current(),
		Commit:             buildinfo.Commit,
		UpdateCheckEnabled: enabled,
		Update:             latest,
	}
}

// SetUpdateCheckEnabled persists the opt-out. Turning it off forgets any
// release already found; turning it on checks right away.
func (s *AppInfoService) SetUpdateCheckEnabled(enabled bool) error {
	if err := s.core.SetUpdateCheckEnabled(enabled); err != nil {
		return err
	}
	if !enabled {
		s.mu.Lock()
		s.latest = nil
		s.mu.Unlock()
		return nil
	}
	go s.check(context.Background(), true)
	return nil
}

// start runs the daily check until stop. Nothing is requested while the
// setting is off, and a check that ran within the last day is not repeated.
func (s *AppInfoService) start(ctx context.Context, bridge *wailsbridge.Bridge) {
	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.bridge = bridge
	s.cancel = cancel
	s.mu.Unlock()
	go func() {
		select {
		case <-time.After(updateCheckStartDelay):
		case <-ctx.Done():
			return
		}
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			s.check(ctx, false)
			select {
			case <-ticker.C:
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (s *AppInfoService) stop() {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// check performs one release check. force skips the once-a-day gate.
func (s *AppInfoService) check(ctx context.Context, force bool) {
	if !s.core.UpdateCheckEnabled() {
		return
	}
	now := time.Now()
	if !force && !s.core.UpdateCheckDue(now) {
		return
	}
	rctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	rel, err := coreapp.FetchLatestRelease(rctx, nil, coreapp.LatestReleaseURL, buildinfo.Current())
	if err != nil {
		log.Printf("update check: %v", err)
		return
	}
	s.core.MarkUpdateChecked(now)
	if rel == nil || !s.core.UpdateCheckEnabled() {
		return
	}
	s.mu.Lock()
	s.latest = rel
	bridge := s.bridge
	s.mu.Unlock()
	if bridge != nil {
		bridge.Emit(updateAvailableEvent, *rel)
	}
}
