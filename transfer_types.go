package main

import "time"

// TransferCapabilities describes which transfer channels are currently supported.
type TransferCapabilities struct {
	DragOut       DragCapability `json:"drag_out"`
	ClipboardFile bool           `json:"clipboard_file"`
}

// DragCapability describes outbound drag support for the current platform.
type DragCapability struct {
	Enabled  bool   `json:"enabled"`
	Strategy string `json:"strategy"`
	Reason   string `json:"reason"`
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
	Filename       string    `json:"filename"`
	ContentType    string    `json:"content_type"`
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
}
