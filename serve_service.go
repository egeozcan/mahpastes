package main

import "fmt"

// ServeService exposes tag-serving operations to the frontend via Wails.
type ServeService struct {
	app *App
}

// NewServeService creates a new serve service.
func NewServeService(app *App) *ServeService {
	return &ServeService{app: app}
}

// StartServing starts an HTTP server for the given tag.
func (s *ServeService) StartServing(tagID int64, port int, bindAll bool) (ServeInfo, error) {
	if s.app.serveManager == nil {
		return ServeInfo{}, fmt.Errorf("serve manager not initialized")
	}
	return s.app.serveManager.StartServing(tagID, port, bindAll)
}

// StopServing stops the HTTP server for the given tag.
func (s *ServeService) StopServing(tagID int64) error {
	if s.app.serveManager == nil {
		return fmt.Errorf("serve manager not initialized")
	}
	return s.app.serveManager.StopServing(tagID)
}

// GetServeStatus returns the status of all running tag servers.
func (s *ServeService) GetServeStatus() []ServeInfo {
	if s.app.serveManager == nil {
		return nil
	}
	return s.app.serveManager.GetStatus()
}

// GetRandomPort finds an available TCP port.
func (s *ServeService) GetRandomPort() (int, error) {
	return GetRandomPort()
}
