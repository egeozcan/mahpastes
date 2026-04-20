package main

import "time"

// Envelope kinds (wire-level plaintext discriminators).
const (
	KindClipStart = "clip_start"
	KindClipChunk = "clip_chunk"
	KindClipEnd   = "clip_end"
	KindGap       = "gap"
)

// Protocol constants — untyped int and string values.
const (
	ShareProtocolID     = "/mahpastes/share/1.0.0"
	ShareStringPrefix   = "mp-share:v1:"
	ShareIdentityFile   = "share_identity.key"
	ShareStagingDirName = "share-staging"

	// Handshake
	HandshakeHMACContext = "mp-share-v1-follow"

	// Size limits
	ChunkSize      = 1 << 20          // 1 MiB plaintext per chunk
	// MaxEnvelopeLen caps the on-wire body (nonce||ciphertext). Headroom over
	// ChunkSize must cover: 12B nonce + 16B GCM tag + CBOR payload overhead
	// (field keys, kind string, length prefixes). Measured worst case for
	// ClipChunkPayload is ~76B; 256 gives comfortable margin.
	MaxEnvelopeLen = ChunkSize + 256  // length-prefixed ciphertext cap

	// Ring retention
	RingTTLSeconds     = 3600            // 1h
	RingBytesCapPerPub = 256 * (1 << 20) // 256 MiB

	// Per-follower send scheduler
	SendQueueBytesCap     = 32 * (1 << 20) // 32 MiB
	SendQueueEnvelopesCap = 256

	// Rate limits
	MaxStreamsPerPublication = 128
	MaxStreamsPerPeer        = 4
)

// Protocol constants — time.Duration values.
const (
	RingSweepInterval    time.Duration = 15 * time.Minute
	StagingSweepInterval time.Duration = 6 * time.Hour
	StagingMaxAge        time.Duration = 24 * time.Hour
	HandshakeTimeout     time.Duration = 5 * time.Second
	ReconnectFloor       time.Duration = time.Second
	ReconnectCap         time.Duration = 30 * time.Second
)

// ShareInfo — one entry in the publisher-side Sharing list (frontend DTO).
type ShareInfo struct {
	ID          int64  `json:"id"`
	TagID       int64  `json:"tag_id"`
	TagName     string `json:"tag_name"`
	ShareString string `json:"share_string"`
	Status      string `json:"status"` // "active" | "invalid" | "paused"
	Followers   int    `json:"followers"`
	ClipsPushed int64  `json:"clips_pushed"`
	LastSeq     int64  `json:"last_seq"`
	CreatedAt   int64  `json:"created_at"`
}

// FollowInfo — one entry in the Following list (frontend DTO). Paused is
// orthogonal to Status: a paused follow never dials so Status stays at
// whatever it held when pause was requested.
type FollowInfo struct {
	ID            int64  `json:"id"`
	RemotePeerID  string `json:"remote_peer_id"`
	LocalTagID    int64  `json:"local_tag_id"`
	LocalTagName  string `json:"local_tag_name"`
	Status        string `json:"status"` // "connected" | "connected_relayed" | "offline"
	Paused        bool   `json:"paused"`
	ClipsReceived int64  `json:"clips_received"`
	LastSeq       int64  `json:"last_seq"`
	LastSeenAt    *int64 `json:"last_seen_at"`
	CreatedAt     int64  `json:"created_at"`
}

// ShareLogEntry — one entry in the share-system event log (frontend DTO).
// The log is an in-memory ring buffer surfaced via ShareService.GetShareLogs.
// Entries disappear on app restart — this is diagnostic, not audit history.
type ShareLogEntry struct {
	Timestamp     int64  `json:"timestamp"` // unix seconds
	Level         string `json:"level"`     // "info" | "warn" | "error"
	Scope         string `json:"scope"`     // "follow" | "share" | "system"
	FollowID      int64  `json:"follow_id,omitempty"`
	PublicationID int64  `json:"publication_id,omitempty"`
	Message       string `json:"message"`
}
