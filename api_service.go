package main

import (
	"fmt"

	coreapp "go-clipboard/internal/app"
)

// APIService exposes REST API operations to the frontend via Wails.
type APIService struct {
	app *coreapp.App
}

// NewAPIService creates a new API service.
func NewAPIService(app *coreapp.App) *APIService {
	return &APIService{app: app}
}

// StartAPI starts the REST API server.
func (s *APIService) StartAPI(port int, bindAll bool) (APIStatus, error) {
	if s.app.APIManager() == nil {
		return APIStatus{}, fmt.Errorf("API manager not initialized")
	}
	return s.app.APIManager().Start(port, bindAll)
}

// StopAPI stops the REST API server.
func (s *APIService) StopAPI() error {
	if s.app.APIManager() == nil {
		return fmt.Errorf("API manager not initialized")
	}
	return s.app.APIManager().Stop()
}

// GetAPIStatus returns the current API server status.
func (s *APIService) GetAPIStatus() APIStatus {
	if s.app.APIManager() == nil {
		return APIStatus{}
	}
	return s.app.APIManager().GetStatus()
}

// CreateAPIKey generates a new API key.
func (s *APIService) CreateAPIKey(name, role string, scopedTagID int64) (*APIKeyCreateResult, error) {
	if s.app.APIManager() == nil {
		return nil, fmt.Errorf("API manager not initialized")
	}
	return s.app.APIManager().CreateKey(name, role, scopedTagID)
}

// ListAPIKeys returns all API keys.
func (s *APIService) ListAPIKeys() ([]APIKeyInfo, error) {
	if s.app.APIManager() == nil {
		return nil, fmt.Errorf("API manager not initialized")
	}
	return s.app.APIManager().ListKeys()
}

// RevokeAPIKey revokes an API key.
func (s *APIService) RevokeAPIKey(id int64) error {
	if s.app.APIManager() == nil {
		return fmt.Errorf("API manager not initialized")
	}
	return s.app.APIManager().RevokeKey(id)
}
