package app

import "time"

// MediaPreviewChannel is the transfer channel used for in-app video playback.
// It shares the leased temp-file store with drag-out but is served over the
// /media/ route with an inline disposition.
const MediaPreviewChannel = "media_preview"

// mediaPreviewChannel is the package-internal spelling of MediaPreviewChannel.
const mediaPreviewChannel = MediaPreviewChannel

// TransferCapabilities describes which transfer channels are currently supported.
type TransferCapabilities struct {
	DragOut       DragCapability `json:"drag_out"`
	ClipboardFile bool           `json:"clipboard_file"`
}

// DragCapability describes outbound drag support for the current platform.
type DragCapability struct {
	Enabled    bool   `json:"enabled"`
	Strategy   string `json:"strategy"`
	Reason     string `json:"reason"`
	NativeDrag bool   `json:"native_drag"`
}

// PrepareTransferRequest describes a request to prepare a clip for transfer.
type PrepareTransferRequest struct {
	ClipID  int64  `json:"clip_id"`
	Channel string `json:"channel"`
}

// StartNativeDragRequest describes a request to begin a native OS drag operation.
type StartNativeDragRequest struct {
	ClipID  int64  `json:"clip_id"`
	AbsPath string `json:"abs_path"`
}

// PreparedTransferItem is a transfer-ready descriptor for one clip.
type PreparedTransferItem struct {
	ClipID         int64     `json:"clip_id"`
	AbsPath        string    `json:"abs_path"`
	FileURL        string    `json:"file_url"`
	TransferURL    string    `json:"transfer_url"`
	Filename       string    `json:"filename"`
	ContentType    string    `json:"content_type"`
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
}
