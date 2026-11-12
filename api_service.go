package main

import (
	"fmt"
	"log"
	"strconv"

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
	status, err := s.app.APIManager().Start(port, bindAll)
	if err == nil {
		// Remember the port for "Start on launch". bind_all is deliberately
		// never persisted: an autostarted listener is always loopback-only.
		_ = s.app.SetSetting(coreapp.SettingAPIPort, strconv.Itoa(port))
	}
	return status, err
}

// GetAPIAutostart reports whether the API server starts when the app launches.
func (s *APIService) GetAPIAutostart() bool {
	v, _ := s.app.GetSetting(coreapp.SettingAPIAutostart)
	return v == "true"
}

// SetAPIAutostart persists the "Start on launch" choice.
func (s *APIService) SetAPIAutostart(enabled bool) error {
	return s.app.SetSetting(coreapp.SettingAPIAutostart, strconv.FormatBool(enabled))
}

// startOnLaunch starts the API server at desktop startup when the user opted
// in. It lives here, not in the shared Bootstrap, because mahpastesd starts its
// own listener. Failures (e.g. port in use) are logged and never fatal.
func (s *APIService) startOnLaunch() {
	am := s.app.APIManager()
	if am == nil {
		return
	}
	enabled, _ := s.app.GetSetting(coreapp.SettingAPIAutostart)
	port, _ := s.app.GetSetting(coreapp.SettingAPIPort)
	p, ok := coreapp.ParseAutostart(enabled, port)
	if !ok {
		return
	}
	if _, err := am.Start(p, false); err != nil {
		log.Printf("api autostart on port %d failed: %v", p, err)
	}
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
