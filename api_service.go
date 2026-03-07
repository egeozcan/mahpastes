package main

import "fmt"

// APIService exposes REST API operations to the frontend via Wails.
type APIService struct {
	app *App
}

// NewAPIService creates a new API service.
func NewAPIService(app *App) *APIService {
	return &APIService{app: app}
}

// StartAPI starts the REST API server.
func (s *APIService) StartAPI(port int, bindAll bool) (APIStatus, error) {
	if s.app.apiManager == nil {
		return APIStatus{}, fmt.Errorf("API manager not initialized")
	}
	return s.app.apiManager.Start(port, bindAll)
}

// StopAPI stops the REST API server.
func (s *APIService) StopAPI() error {
	if s.app.apiManager == nil {
		return fmt.Errorf("API manager not initialized")
	}
	return s.app.apiManager.Stop()
}

// GetAPIStatus returns the current API server status.
func (s *APIService) GetAPIStatus() APIStatus {
	if s.app.apiManager == nil {
		return APIStatus{}
	}
	return s.app.apiManager.GetStatus()
}

// CreateAPIKey generates a new API key.
func (s *APIService) CreateAPIKey(name, role string, scopedTagID int64) (*APIKeyCreateResult, error) {
	if s.app.apiManager == nil {
		return nil, fmt.Errorf("API manager not initialized")
	}
	return s.app.apiManager.CreateKey(name, role, scopedTagID)
}

// ListAPIKeys returns all API keys.
func (s *APIService) ListAPIKeys() ([]APIKeyInfo, error) {
	if s.app.apiManager == nil {
		return nil, fmt.Errorf("API manager not initialized")
	}
	return s.app.apiManager.ListKeys()
}

// RevokeAPIKey revokes an API key.
func (s *APIService) RevokeAPIKey(id int64) error {
	if s.app.apiManager == nil {
		return fmt.Errorf("API manager not initialized")
	}
	return s.app.apiManager.RevokeKey(id)
}
