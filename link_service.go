package main

import (
	"fmt"

	coreapp "go-clipboard/internal/app"
)

// LinkService exposes revocable share-link operations to the frontend via Wails.
// In the headless server these same operations are reached over REST
// (/api/v1/links) through rest-glue.js, so the frontend call sites are
// identical in both modes. Minting through the desktop binding has no API key
// context, so created_by is recorded as 0 (none).
type LinkService struct {
	app *coreapp.App
}

// NewLinkService creates a new link service.
func NewLinkService(app *coreapp.App) *LinkService {
	return &LinkService{app: app}
}

// CreateShareLink mints a revocable public link for a clip.
func (s *LinkService) CreateShareLink(clipID int64, name string, expiresInSeconds, maxDownloads int64) (*ShareLinkCreateResult, error) {
	if s.app.APIManager() == nil {
		return nil, fmt.Errorf("API manager not initialized")
	}
	return s.app.APIManager().CreateShareLink(clipID, name, expiresInSeconds, maxDownloads, 0)
}

// ListShareLinks returns all share links.
func (s *LinkService) ListShareLinks() ([]ShareLinkInfo, error) {
	if s.app.APIManager() == nil {
		return nil, fmt.Errorf("API manager not initialized")
	}
	return s.app.APIManager().ListShareLinks()
}

// RevokeShareLink revokes a share link by id.
func (s *LinkService) RevokeShareLink(id int64) error {
	if s.app.APIManager() == nil {
		return fmt.Errorf("API manager not initialized")
	}
	return s.app.APIManager().RevokeShareLink(id)
}
