package main

import (
	"fmt"
	"log"

	coreapp "go-clipboard/internal/app"
)

// APIService exposes REST API operations to the frontend via Wails.
type APIService struct {
	app *coreapp.App
	// launchFailure is why "Start on launch" did not start the server, if it
	// did not; the modal shows it next to "Stopped".
	launchFailure coreapp.APILaunchFailure
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
		s.launchFailure.Clear()
		// Remember the port for "Start on launch". bind_all is deliberately
		// never persisted: an autostarted listener is always loopback-only.
		if err := s.app.RememberAPIPort(status.Port); err != nil {
			log.Printf("api: could not remember port %d: %v", status.Port, err)
		}
	}
	return status, err
}

// GetAPIAutostart reports whether the API server starts when the app launches.
func (s *APIService) GetAPIAutostart() bool {
	enabled, _ := s.app.APILaunchSettings()
	return enabled
}

// GetAPIPort returns the port "Start on launch" uses: the one the server was
// last started on, or the default.
func (s *APIService) GetAPIPort() int {
	_, port := s.app.APILaunchSettings()
	return port
}

// SetAPIAutostart persists the "Start on launch" choice; enabling it also
// records port as the port to listen on.
func (s *APIService) SetAPIAutostart(enabled bool, port int) error {
	return s.app.SetAPILaunchSettings(enabled, port)
}

// startOnLaunch starts the API server at desktop startup when the user opted
// in. It runs from OnStartup, not the shared Bootstrap, because mahpastesd
// starts its own listener. Failures (e.g. port in use) are never fatal: they
// are logged and kept for GetAPILaunchError.
func (s *APIService) startOnLaunch() {
	if _, err := s.app.StartAPIOnLaunch(); err != nil {
		log.Printf("api: start on launch failed: %v", err)
		_, port := s.app.APILaunchSettings()
		s.launchFailure.Record(port, err)
	}
}

// GetAPILaunchError returns why "Start on launch" could not start the server
// this run (e.g. "port 44557 is already in use"), or "" when it did not fail,
// the server is running, or the user has started or stopped it since.
func (s *APIService) GetAPILaunchError() string {
	if am := s.app.APIManager(); am != nil && am.GetStatus().Running {
		return ""
	}
	return s.launchFailure.Message()
}

// StopAPI stops the REST API server.
func (s *APIService) StopAPI() error {
	if s.app.APIManager() == nil {
		return fmt.Errorf("API manager not initialized")
	}
	err := s.app.APIManager().Stop()
	if err == nil {
		s.launchFailure.Clear()
	}
	return err
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
