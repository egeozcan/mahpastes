package app

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
	ChunkSize = 1 << 20 // 1 MiB plaintext per chunk
	// MaxEnvelopeLen caps the on-wire body (nonce||ciphertext). Headroom over
	// ChunkSize must cover: 12B nonce + 16B GCM tag + CBOR payload overhead
	// (field keys, kind string, length prefixes). Measured worst case for
	// ClipChunkPayload is ~76B; 256 gives comfortable margin.
	MaxEnvelopeLen = ChunkSize + 256 // length-prefixed ciphertext cap

	// Ring retention
	RingTTLSeconds     = 3600            // 1h
	RingBytesCapPerPub = 256 * (1 << 20) // 256 MiB

	// Per-follower send scheduler.
	//
	// SendQueueBytesCap bounds the bytes one follower may have queued but
	// not yet written; overflowing it drops the follower. It is therefore
	// also the ceiling on the size of a clip this node can share at all:
	// handshake catch-up replays a clip as one indivisible batch (a batch is
	// never cut mid-clip — see planCatchupBatch), so a clip whose envelopes
	// do not fit the queue can never reach a reconnecting follower. With
	// 1 MiB chunks the effective maximum is ~31 MiB of clip payload once
	// framing, per-chunk envelope overhead and the reserved gap slot are
	// taken out; larger clips are skipped during catch-up with a warning.
	// Raise both caps together if that ceiling ever needs to move.
	SendQueueBytesCap     = 32 * (1 << 20) // 32 MiB
	SendQueueEnvelopesCap = 256

	// Handshake catch-up budget. A reconnecting follower's backlog can be as
	// large as the whole ring (RingBytesCapPerPub, 256 MiB), which does not
	// fit the send queue — so catch-up is delivered in batches cut at clip
	// boundaries, one per connection.
	//
	// The soft budget is half of each queue cap. The other half is headroom
	// for live fan-out, which resumes into this same queue the instant the
	// handshake releases pub.fmu: a full-to-the-brim catch-up batch plus one
	// live clip would otherwise shed the follower we just served.
	CatchupSoftBytesCap     = SendQueueBytesCap / 2
	CatchupSoftEnvelopesCap = SendQueueEnvelopesCap / 2

	// Hard budget. A batch that had to be truncated is excluded from live
	// fan-out (it closes as soon as it drains), so it owns the whole queue
	// and a single clip too big for the soft budget can still be sent alone
	// up to this limit. The reserve covers the synthesized gap envelope,
	// which is enqueued ahead of the batch and counts against both caps.
	CatchupGapReserveBytes  = 4096
	CatchupHardBytesCap     = SendQueueBytesCap - CatchupGapReserveBytes
	CatchupHardEnvelopesCap = SendQueueEnvelopesCap - 1

	// CatchupMetaRowCap bounds the metadata window one handshake reads from
	// the ring. 4096 rows is ~16 hard-cap batches of chunked clips (a 30 MiB
	// clip is ~32 rows) and far beyond the longest possible orphan head run
	// (one partially-evicted clip, ≤ ~32 rows), while capping the metadata
	// allocation at a few hundred KiB even for a ring of millions of minimal
	// clips. A window that fills marks the plan truncated, and the follower
	// pages through the rest across reconnects like any other truncated
	// batch.
	CatchupMetaRowCap = 4096

	// MaxClipStartFieldBytes bounds the combined size of the fields embedded
	// in a clip_start envelope (filename + content type + metadata JSON).
	// The check runs on the raw column values BEFORE the payload is built,
	// because it exists to stop the allocation itself: RenameClip and the
	// metadata APIs accept arbitrarily long strings, and CBOR-marshaling a
	// huge filename would allocate the whole thing only for EncryptEnvelope
	// to reject the frame afterwards. 256 KiB is orders of magnitude beyond
	// any legitimate filename or metadata blob while staying far inside the
	// MaxEnvelopeLen frame cap.
	MaxClipStartFieldBytes = 256 * (1 << 10)

	// MaxShareableClipBytes is the publication-side ceiling on a clip's
	// payload. Anything larger is refused up front (warn-logged, no ring
	// rows, no seqs consumed) rather than emitted: its envelope burst could
	// never fit a follower's send queue (SendQueueBytesCap), so emitting it
	// would only buffer the whole burst in memory — an unbounded heap spike
	// for a multi-GB clip — and bloat the ring with rows every catch-up
	// batch would have to skip anyway. 2 MiB of headroom under the queue cap
	// covers per-chunk envelope overhead, the clip_start/clip_end frames,
	// and the synthesized gap a catch-up may prepend.
	MaxShareableClipBytes = SendQueueBytesCap - 2*(1<<20)

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
	ID           int64  `json:"id"`
	RemotePeerID string `json:"remote_peer_id"`
	LocalTagID   int64  `json:"local_tag_id"`
	LocalTagName string `json:"local_tag_name"`
	// "connecting" (row committed, no handshake yet) | "connected" |
	// "connected_relayed" (connected over a circuit-v2 relay) | "offline".
	Status        string `json:"status"`
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
