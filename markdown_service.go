package main

import coreapp "go-clipboard/internal/app"

// MarkdownService exposes desktop-only Markdown reference and image operations.
type MarkdownService struct {
	app *coreapp.App
}

func NewMarkdownService(app *coreapp.App) *MarkdownService {
	return &MarkdownService{app: app}
}

func (s *MarkdownService) ResolveReferences(sourceClipID int64, references []string) ([]MarkdownReferenceResult, error) {
	return s.app.ResolveMarkdownReferences(sourceClipID, references)
}

func (s *MarkdownService) GetLocalImage(clipID int64) (MarkdownImageData, error) {
	return s.app.GetMarkdownImage(clipID)
}

func (s *MarkdownService) ValidateEmbeddedImage(dataBase64, contentType string) (MarkdownImageData, error) {
	return s.app.ValidateEmbeddedMarkdownImage(dataBase64, contentType)
}

func (s *MarkdownService) GetCachedRemoteImage(rawURL string) (MarkdownCachedImageResult, error) {
	return s.app.GetCachedMarkdownImage(rawURL)
}

func (s *MarkdownService) LoadRemoteImage(requestID, rawURL string) (MarkdownRemoteImageResult, error) {
	return s.app.LoadRemoteMarkdownImage(requestID, rawURL)
}

func (s *MarkdownService) CancelRemoteImage(requestID string) bool {
	return s.app.CancelRemoteMarkdownImage(requestID)
}

func (s *MarkdownService) GetImageCacheStats() (MarkdownImageCacheStats, error) {
	return s.app.GetMarkdownImageCacheStats()
}

func (s *MarkdownService) ClearImageCache() error {
	return s.app.ClearMarkdownImageCache()
}
