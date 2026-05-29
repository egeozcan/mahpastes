package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"strings"

	coreapp "go-clipboard/internal/app"

	"golang.design/x/clipboard"
	_ "golang.org/x/image/webp"
)

// ClipboardService handles clipboard copy operations.
// This is a separate struct to stay under the Wails method binding limit (~49 per struct).
type ClipboardService struct {
	app *coreapp.App
}

// NewClipboardService creates a new clipboard service.
func NewClipboardService(app *coreapp.App) *ClipboardService {
	return &ClipboardService{app: app}
}

// CopyFileToClipboard copies a clip as a file reference to the system clipboard.
func (s *ClipboardService) CopyFileToClipboard(id int64) error {
	prepared, err := s.app.PrepareClipTransferItem(id, "clipboard_file")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	return copyFilesToClipboard([]string{prepared.AbsPath})
}

// BulkCopyFilesToClipboard copies multiple clips as files to the system clipboard.
func (s *ClipboardService) BulkCopyFilesToClipboard(ids []int64) error {
	if len(ids) == 0 {
		return fmt.Errorf("no IDs provided")
	}
	var paths []string
	for _, id := range ids {
		prepared, err := s.app.PrepareClipTransferItem(id, "clipboard_file")
		if err != nil {
			return fmt.Errorf("failed to create temp file for clip %d: %w", id, err)
		}
		paths = append(paths, prepared.AbsPath)
	}
	return copyFilesToClipboard(paths)
}

// CopyClipContents copies the raw content of a clip to the system clipboard.
func (s *ClipboardService) CopyClipContents(id int64) error {
	var data []byte
	var contentType string

	row := s.app.DB().QueryRow("SELECT data, content_type FROM clips WHERE id = ?", id)
	if err := row.Scan(&data, &contentType); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("clip not found")
		}
		return fmt.Errorf("failed to get clip: %w", err)
	}

	if strings.HasPrefix(contentType, "text/") || contentType == "application/json" {
		clipboard.Write(clipboard.FmtText, data)
		return nil
	}

	if strings.HasPrefix(contentType, "image/") {
		if contentType == "image/png" {
			return copyImageToClipboard(data)
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("failed to decode image: %w", err)
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return fmt.Errorf("failed to encode image as PNG: %w", err)
		}
		return copyImageToClipboard(buf.Bytes())
	}

	return fmt.Errorf("unsupported content type for clipboard copy: %s", contentType)
}
