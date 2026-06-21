package main

import (
	"fmt"

	coreapp "go-clipboard/internal/app"
)

// ServeService exposes tag-serving operations to the frontend via Wails.
type ServeService struct {
	app *coreapp.App
}

// NewServeService creates a new serve service.
func NewServeService(app *coreapp.App) *ServeService {
	return &ServeService{app: app}
}

// StartServing starts an HTTP server for the given tag.
func (s *ServeService) StartServing(tagID int64, port int, bindAll bool, apiAccess string) (ServeInfo, error) {
	if s.app.ServeManager() == nil {
		return ServeInfo{}, fmt.Errorf("serve manager not initialized")
	}
	return s.app.ServeManager().StartServing(tagID, port, bindAll, apiAccess)
}

// StopServing stops the HTTP server for the given tag.
func (s *ServeService) StopServing(tagID int64) error {
	if s.app.ServeManager() == nil {
		return fmt.Errorf("serve manager not initialized")
	}
	return s.app.ServeManager().StopServing(tagID)
}

// GetServeStatus returns the status of all running tag servers.
func (s *ServeService) GetServeStatus() []ServeInfo {
	if s.app.ServeManager() == nil {
		return nil
	}
	return s.app.ServeManager().GetStatus()
}

// GetRandomPort finds an available TCP port.
func (s *ServeService) GetRandomPort() (int, error) {
	return coreapp.GetRandomPort()
}
