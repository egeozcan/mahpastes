package main

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"

	coreapp "go-clipboard/internal/app"
)

// TransferService handles transfer-oriented capability and preparation flows.
// This is separate from App to keep Wails binding counts manageable.
type TransferService struct {
	app *coreapp.App
}

// NewTransferService creates a new transfer service.
func NewTransferService(app *coreapp.App) *TransferService {
	return &TransferService{app: app}
}

// GetTransferCapabilities returns platform-specific transfer support.
func (s *TransferService) GetTransferCapabilities() TransferCapabilities {
	return buildTransferCapabilities(goruntime.GOOS)
}

func buildTransferCapabilities(goos string) TransferCapabilities {
	caps := TransferCapabilities{
		DragOut: DragCapability{
			Enabled:  false,
			Strategy: "",
			Reason:   "drag-out is not implemented on this platform yet",
		},
		ClipboardFile: goos == "darwin" || goos == "windows",
	}

	switch goos {
	case "darwin":
		caps.DragOut = DragCapability{
			Enabled:    true,
			Strategy:   "file-uri-v1",
			NativeDrag: true,
		}
	case "windows":
		// Strategy is "file-uri-v1" (same adapter as macOS), but on Windows the
		// DownloadURL payload uses an HTTP transfer_url instead of a file:// URI
		// because Chromium's DownloadURL rejects file:// with a network error.
		caps.DragOut = DragCapability{
			Enabled:    true,
			Strategy:   "file-uri-v1",
			NativeDrag: false,
		}
	case "linux":
		caps.DragOut = DragCapability{
			Enabled:  false,
			Strategy: "linux-fileuri-v1",
			Reason:   "planned for a future release",
		}
	}

	return caps
}

// PrepareClipForTransfer prepares a clip temp file and returns transfer metadata.
func (s *TransferService) PrepareClipForTransfer(req PrepareTransferRequest) (*PreparedTransferItem, error) {
	if req.ClipID <= 0 {
		return nil, fmt.Errorf("invalid clip_id: %d", req.ClipID)
	}
	if strings.TrimSpace(req.Channel) == "" {
		req.Channel = "drag_out"
	}
	if req.Channel == coreapp.MediaPreviewChannel {
		return s.app.PrepareClipMediaItem(req.ClipID)
	}
	return s.app.PrepareClipTransferItem(req.ClipID, req.Channel)
}

// GetExistingPreparedClipForTransfer returns an already prepared temp file descriptor if present.
// It does not create a new temp file.
func (s *TransferService) GetExistingPreparedClipForTransfer(req PrepareTransferRequest) (*PreparedTransferItem, error) {
	if req.ClipID <= 0 {
		return nil, fmt.Errorf("invalid clip_id: %d", req.ClipID)
	}
	if strings.TrimSpace(req.Channel) == "" {
		req.Channel = "drag_out"
	}
	return s.app.LookupPreparedClipTransferItem(req.ClipID, req.Channel)
}

// StartNativeDragOut starts a native OS file drag operation when supported.
// It prefers req.AbsPath and falls back to preparing the clip from req.ClipID.
func (s *TransferService) StartNativeDragOut(req StartNativeDragRequest) (bool, error) {
	caps := buildTransferCapabilities(goruntime.GOOS)
	if !caps.DragOut.NativeDrag {
		return false, fmt.Errorf("native drag is not supported on this platform")
	}

	absPath := strings.TrimSpace(req.AbsPath)

	resolvePathFromClip := func() (string, error) {
		if req.ClipID <= 0 {
			return "", fmt.Errorf("either abs_path or clip_id must be provided")
		}
		prepared, err := s.app.PrepareClipTransferItem(req.ClipID, "drag_out")
		if err != nil {
			return "", err
		}
		return prepared.AbsPath, nil
	}

	if absPath == "" {
		var err error
		absPath, err = resolvePathFromClip()
		if err != nil {
			return false, err
		}
	}

	if !filepath.IsAbs(absPath) {
		return false, fmt.Errorf("abs_path must be absolute")
	}

	if _, err := os.Stat(absPath); err != nil {
		var resolveErr error
		absPath, resolveErr = resolvePathFromClip()
		if resolveErr != nil {
			return false, resolveErr
		}
	}

	if err := startNativeFileDrag(absPath); err != nil {
		return false, err
	}
	return true, nil
}

func fileURLFromAbsPath(absPath string) string {
	p := filepath.ToSlash(absPath)
	if len(p) > 0 && p[0] != '/' {
		p = "/" + p
	}
	u := &url.URL{Scheme: "file", Path: p}
	return u.String()
}
