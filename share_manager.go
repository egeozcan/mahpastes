package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	dht "github.com/libp2p/go-libp2p-kad-dht"
)

// mdnsServiceTag identifies mahpastes instances on the local network via mDNS.
// All instances on the same host/LAN share this tag, so they can discover each
// other without needing a populated DHT routing table.
const mdnsServiceTag = "mahpastes-share"

// mdnsNotifee connects to every peer discovered via mDNS. Once connected,
// libp2p adds the peer to the peerstore automatically, which is exactly what
// dialByPeerID's fast path needs.
type mdnsNotifee struct {
	host host.Host
	ctx  context.Context
}

func (n *mdnsNotifee) HandlePeerFound(pi peer.AddrInfo) {
	cctx, cancel := context.WithTimeout(n.ctx, 10*time.Second)
	defer cancel()
	if err := n.host.Connect(cctx, pi); err != nil {
		log.Printf("share: mdns peer connect %s: %v", pi.ID, err)
	}
}

// ShareManager owns the libp2p host and tracks publications + follows.
type ShareManager struct {
	ctx     context.Context
	cancel  context.CancelFunc
	db      *sql.DB
	dataDir string

	host     host.Host
	dht      *dht.IpfsDHT
	mdnsSvc  mdns.Service

	mu           sync.RWMutex
	publications map[int64]*publication // keyed by shares.id
	follows      map[int64]*follow      // keyed by follows.id

	// eventFn is the runtime.EventsEmit bound at App startup time
	// (lets the manager push card updates to the frontend without a
	// direct dependency on the Wails runtime).
	eventFn func(name string, data ...any)

	// maxStreamsPerPubOverride — test-only knob. 0 means "use
	// MaxStreamsPerPublication". setMaxStreamsPerPubForTest sets this so
	// cap-enforcement can be exercised without saturating with 128 real
	// libp2p streams.
	maxStreamsPerPubOverride int

	// logs is an in-memory ring buffer of share-system events surfaced to
	// the UI via GetShareLogs. Never nil after NewShareManager.
	logs *shareLogBuffer
}

// NewShareManager loads identity, starts the libp2p host + DHT, and returns
// the manager ready for ResumeAll.
func NewShareManager(parent context.Context, db *sql.DB, dataDir string) (*ShareManager, error) {
	ctx, cancel := context.WithCancel(parent)

	priv, err := LoadOrCreateIdentity(filepath.Join(dataDir, ShareIdentityFile))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("identity: %w", err)
	}

	// Neutral AgentVersion to avoid install-specific fingerprinting (spec §5.4).
	h, err := libp2p.New(
		libp2p.Identity(priv),
		libp2p.UserAgent("mahpastes"),
		libp2p.EnableRelay(),
		libp2p.EnableHolePunching(),
		libp2p.EnableAutoRelayWithStaticRelays(defaultStaticRelays()),
		libp2p.ListenAddrStrings(
			"/ip4/0.0.0.0/tcp/0",
			"/ip4/0.0.0.0/udp/0/quic-v1",
		),
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("libp2p.New: %w", err)
	}

	// Register circuit-relay v2 stop handler so we can be reached via relay.
	if _, err := relay.New(h); err == nil {
		// OK if this fails — libp2p host still usable; relay reservation handled by AutoRelay
	}

	// Kademlia DHT. Publisher=auto-server, but we don't know yet which role
	// this node plays; auto-server is safe for both because a node without
	// publications just doesn't serve publication lookups.
	kad, err := dht.New(ctx, h, dht.Mode(dht.ModeAutoServer))
	if err != nil {
		h.Close()
		cancel()
		return nil, fmt.Errorf("dht.New: %w", err)
	}

	// Seeded WAN bootstrap: connect to well-known bootstrap peers so the DHT
	// routing table is populated and FindPeer works for cross-network discovery.
	// Guarded by MAHPASTES_SHARE_DISABLE_WAN_BOOTSTRAP=1 so e2e tests (which run
	// fully offline on localhost) skip the public network connections and rely on
	// mDNS discovery instead.
	if os.Getenv("MAHPASTES_SHARE_DISABLE_WAN_BOOTSTRAP") != "1" {
		var wg sync.WaitGroup
		for _, addr := range dht.DefaultBootstrapPeers {
			pi, err := peer.AddrInfoFromP2pAddr(addr)
			if err != nil {
				log.Printf("share: invalid bootstrap addr %s: %v", addr, err)
				continue
			}
			wg.Add(1)
			go func(pi peer.AddrInfo) {
				defer wg.Done()
				cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
				defer cancel()
				if err := h.Connect(cctx, pi); err != nil {
					// Non-fatal: many bootstrap peers reject new connections;
					// we only need one to succeed for the DHT to work.
					return
				}
			}(*pi)
		}
		// Don't wait — connections happen in background. Bootstrap can proceed.
	}

	if err := kad.Bootstrap(ctx); err != nil {
		log.Printf("share: DHT bootstrap returned error (non-fatal): %v", err)
	}

	m := &ShareManager{
		ctx:          ctx,
		cancel:       cancel,
		db:           db,
		dataDir:      dataDir,
		host:         h,
		dht:          kad,
		publications: map[int64]*publication{},
		follows:      map[int64]*follow{},
		logs:         newShareLogBuffer(),
	}

	// mDNS discovery: automatically find and connect to other mahpastes instances
	// on the same host or LAN without needing a populated DHT routing table.
	// This is the primary discovery mechanism for same-host e2e tests.
	mdnsSvc := mdns.NewMdnsService(h, mdnsServiceTag, &mdnsNotifee{host: h, ctx: ctx})
	if err := mdnsSvc.Start(); err != nil {
		log.Printf("share: mdns start (non-fatal): %v", err)
		mdnsSvc = nil
	}
	m.mdnsSvc = mdnsSvc

	// Register the single application protocol.
	h.SetStreamHandler(ShareProtocolID, m.handlePublisherStream)

	return m, nil
}

// Host exposes the libp2p host for tests and advanced operations.
func (m *ShareManager) Host() host.Host { return m.host }

// SetEventFn installs the frontend event emitter (called from App.startup).
func (m *ShareManager) SetEventFn(fn func(string, ...any)) { m.eventFn = fn }

// Stop shuts down mDNS, the DHT, host, and cancels all follow reconnect loops.
func (m *ShareManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range m.follows {
		f.cancel()
	}
	for _, p := range m.publications {
		p.closeAllFollowers()
	}
	if m.mdnsSvc != nil {
		_ = m.mdnsSvc.Close()
	}
	if m.dht != nil {
		_ = m.dht.Close()
	}
	if m.host != nil {
		_ = m.host.Close()
	}
	m.cancel()
}

// publication — publisher-side per-tag state.
//
// followers is keyed by the libp2p stream pointer (not peer.ID) so a single
// peer may hold up to MaxStreamsPerPeer concurrent streams against the same
// publication, which the cap check in Task 5.2 enforces by iteration. fmu
// guards the map; Task 5.2 takes it for atomic cap-check-then-insert.
type publication struct {
	id        int64
	tagID     int64
	shareID   []byte // 16 bytes
	symkey    []byte // 32 bytes
	status    string // "active" | "invalid"
	followers map[network.Stream]*followerConn
	fmu       sync.Mutex
}

func (p *publication) closeAllFollowers() {
	p.fmu.Lock()
	defer p.fmu.Unlock()
	for _, fc := range p.followers {
		fc.close()
	}
	p.followers = map[network.Stream]*followerConn{}
}

// followerConn owns one follower's stream and a bounded async send queue.
// The sender goroutine drains queue → writer. enqueue is non-blocking and
// closes the connection if either the envelope slot cap or the total byte
// cap is exceeded — that's the Task 6.3 back-pressure policy: a stalled
// follower is dropped, not buffered indefinitely.
type followerConn struct {
	peerID  peer.ID
	stream  network.Stream
	writer  io.Writer    // indirection so tests can substitute a writer
	queue   chan []byte  // buffered channel; SendQueueEnvelopesCap slots
	pending int64        // bytes currently sitting in queue; guarded by mu
	onClose func()       // test hook; invoked under mu
	mu      sync.Mutex
	closed  bool
}

// enqueue hands env to the sender goroutine. Non-blocking; if either cap is
// exceeded the connection is closed immediately.
func (fc *followerConn) enqueue(env []byte) {
	fc.mu.Lock()
	if fc.closed {
		fc.mu.Unlock()
		return
	}
	// Byte cap.
	if fc.pending+int64(len(env)) > int64(SendQueueBytesCap) {
		fc.closeLocked()
		fc.mu.Unlock()
		return
	}
	// Envelope cap (channel buffer).
	select {
	case fc.queue <- env:
		fc.pending += int64(len(env))
	default:
		fc.closeLocked()
	}
	fc.mu.Unlock()
}

// runSender drains the queue into the writer. A write error closes the
// connection; the sender exits when the queue is closed by closeLocked.
func (fc *followerConn) runSender() {
	for env := range fc.queue {
		if _, err := fc.writer.Write(env); err != nil {
			fc.mu.Lock()
			fc.closeLocked()
			fc.mu.Unlock()
			return
		}
		fc.mu.Lock()
		fc.pending -= int64(len(env))
		fc.mu.Unlock()
	}
}

func (fc *followerConn) close() {
	if fc == nil {
		return
	}
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.closeLocked()
}

// closeLocked must be called with fc.mu held. Idempotent.
func (fc *followerConn) closeLocked() {
	if fc.closed {
		return
	}
	fc.closed = true
	close(fc.queue)
	if fc.stream != nil {
		_ = fc.stream.Reset()
	}
	if fc.onClose != nil {
		fc.onClose()
	}
}

// follow — follower-side per-subscription state.
type follow struct {
	id           int64
	remotePeerID peer.ID
	symkey       []byte
	localTagID   int64
	lastSeq      uint64
	status       string
	// ctx / cancel are the follow-lifetime handles. Cancel them to stop the
	// follow permanently (Unfollow). They stay alive across reconnects.
	ctx    context.Context
	cancel context.CancelFunc
	// mu guards sessionCancel, which points at the current session's cancel
	// function (if any). Set by followSession on entry, cleared on exit.
	// DisconnectFollowForTest uses this to cancel only the live session
	// without touching follow-lifetime state.
	// mu also guards paused, so PauseFollow/ResumeFollow can read-modify-
	// write atomically against runFollowLoop's pre-session check.
	mu            sync.Mutex
	sessionCancel context.CancelFunc
	paused        bool
	// reconnectSignal lets ReconnectFollow (and ResumeFollow) kick
	// runFollowLoop out of its backoff or paused wait so a user-requested
	// retry dials immediately instead of sleeping for up to ReconnectCap.
	// Buffered 1 + non-blocking send so repeated kicks coalesce.
	reconnectSignal chan struct{}
}

// defaultStaticRelays returns a placeholder relay list. In production libp2p
// has a maintained public relay set; we use a small well-known list.
// See https://github.com/libp2p/go-libp2p/blob/master/p2p/host/autorelay for
// the upstream convention. For the initial implementation it's acceptable to
// fall back to AutoRelay without a static list (libp2p discovers via DHT).
func defaultStaticRelays() []peer.AddrInfo {
	// Intentionally empty: AutoRelay will discover relays via the DHT. This
	// returns [] to avoid pinning specific relay nodes in-source; revisit if
	// libp2p EnableAutoRelayWithPeerSource becomes the blessed API.
	return nil
}

// registerPublication adds (or updates) a publication in the in-memory map.
// Called from StartShare and ResumeAll.
func (m *ShareManager) registerPublication(id, tagID int64, shareID, symkey []byte, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.publications[id] = &publication{
		id:        id,
		tagID:     tagID,
		shareID:   append([]byte(nil), shareID...),
		symkey:    append([]byte(nil), symkey...),
		status:    status,
		followers: map[network.Stream]*followerConn{},
	}
}

// findPublicationByShareID linear-scans the map (small N) to find which
// publication a given share_id belongs to. Returns nil if unknown.
func (m *ShareManager) findPublicationByShareID(shareID []byte) *publication {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.publications {
		if bytes.Equal(p.shareID, shareID) {
			return p
		}
	}
	return nil
}

// maxStreamsPerPub returns the effective per-publication stream cap. In
// production this is MaxStreamsPerPublication; tests lower it via
// setMaxStreamsPerPubForTest.
func (m *ShareManager) maxStreamsPerPub() int {
	if m.maxStreamsPerPubOverride > 0 {
		return m.maxStreamsPerPubOverride
	}
	return MaxStreamsPerPublication
}

// setMaxStreamsPerPubForTest lowers the per-publication cap so cap-enforcement
// can be exercised in tests without opening 128 real streams.
func (m *ShareManager) setMaxStreamsPerPubForTest(n int) {
	m.maxStreamsPerPubOverride = n
}

// newFollowerConn constructs a followerConn with its send queue initialized
// and a sender goroutine started. Passing a nil stream is supported so tests
// can drive the scheduler against a synthetic writer.
func newFollowerConn(s network.Stream, w io.Writer) *followerConn {
	fc := &followerConn{
		stream: s,
		writer: w,
		queue:  make(chan []byte, SendQueueEnvelopesCap),
	}
	if s != nil {
		fc.peerID = s.Conn().RemotePeer()
	}
	go fc.runSender()
	return fc
}

// handlePublisherStream implements:
//  1. read handshake (72 bytes)
//  2. lookup share_id; if unknown → Reset
//  3. verify HMAC; if bad → Reset
//  4. if status != active → Reset
//  5. enforce per-publication + per-peer stream caps
//  6. replay share_ring with seq > since_seq (TTL enforced in SQL)
//  7. register the followerConn so future live envelopes flow here
func (m *ShareManager) handlePublisherStream(s network.Stream) {
	_ = s.SetReadDeadline(time.Now().Add(HandshakeTimeout))
	hsBuf := make([]byte, HandshakeBytesLen)
	if _, err := io.ReadFull(s, hsBuf); err != nil {
		_ = s.Reset()
		return
	}
	_ = s.SetReadDeadline(time.Time{})

	hs, err := ParseHandshake(hsBuf)
	if err != nil {
		_ = s.Reset()
		return
	}
	pub := m.findPublicationByShareID(hs.ShareID)
	if pub == nil || pub.status != "active" {
		_ = s.Reset()
		return
	}
	if err := VerifyHandshake(pub.symkey, hs); err != nil {
		_ = s.Reset()
		return
	}

	peerID := s.Conn().RemotePeer()

	// Hold pub.fmu across BOTH the stream-cap check AND the ring
	// retransmit + register. Any concurrent emitClipForPublication is
	// blocked from running live fan-out or extending the ring until we
	// finish, so the follower either receives a clip entirely via the
	// retransmit (for clips committed before we acquire the lock) or
	// entirely via live fan-out (for clips emitted after we release).
	// No clip can be split across the two paths or dropped entirely.
	pub.fmu.Lock()
	if len(pub.followers) >= m.maxStreamsPerPub() {
		pub.fmu.Unlock()
		_ = s.Reset()
		return
	}
	perPeer := 0
	for _, fc := range pub.followers {
		if fc.peerID == peerID {
			perPeer++
		}
	}
	if perPeer >= MaxStreamsPerPeer {
		pub.fmu.Unlock()
		_ = s.Reset()
		return
	}

	// Register follower FIRST so that liveFanOutLocked (called by an
	// emission we've just blocked) will enqueue to this follower once we
	// release the lock. We enqueue retransmit envelopes through the same
	// followerConn.enqueue path to serialise all writes through its single
	// sender goroutine; writing to s directly here would race the sender.
	fc := newFollowerConn(s, s)
	pub.followers[s] = fc

	// Catch-up retransmit: ring rows with seq > since_seq that are still
	// within TTL. Because we hold pub.fmu, no emission can commit a new
	// ring row during this query — every row here has seq ≤ shares.last_seq
	// at the moment we acquired the lock, and every row with a larger seq
	// will arrive via the blocked emission's liveFanOutLocked once we release.
	rows, err := RingRetransmit(m.db, pub.id, hs.SinceSeq, time.Now().Unix())
	if err != nil {
		log.Printf("share: retransmit query: %v", err)
		delete(pub.followers, s)
		pub.fmu.Unlock()
		_ = s.Reset()
		return
	}
	for _, r := range rows {
		fc.enqueue(r.EnvelopeBytes)
	}
	pub.fmu.Unlock()

	// Keep this goroutine alive until the stream closes. io.Copy drains any
	// follower→publisher bytes (currently none per protocol) and unblocks on
	// close so we can remove the registration.
	go func() {
		_, _ = io.Copy(io.Discard, s)
		pub.fmu.Lock()
		delete(pub.followers, s)
		pub.fmu.Unlock()
		fc.close()
	}()
}

// StartShare creates a new publication for tagID and returns the share string.
// One publication per tag — returns an error if one already exists (enforced
// by UNIQUE INDEX idx_shares_tag_id).
func (m *ShareManager) StartShare(tagID int64) (ShareInfo, error) {
	var info ShareInfo

	// Tag can have at most one share (enforced by UNIQUE index on
	// shares.tag_id). Fail fast with a user-friendly message so the UI
	// doesn't surface a raw SQLite constraint error.
	var existing int
	if err := m.db.QueryRow(`SELECT COUNT(*) FROM shares WHERE tag_id = ?`, tagID).Scan(&existing); err == nil && existing > 0 {
		return info, fmt.Errorf("this tag is already shared — stop the existing share first")
	}

	symkey := make([]byte, 32)
	if _, err := rand.Read(symkey); err != nil {
		return info, fmt.Errorf("rand: %w", err)
	}
	shareID := DeriveShareID(symkey)
	now := time.Now().Unix()

	res, err := m.db.Exec(
		`INSERT INTO shares (tag_id, symkey, share_id, last_seq, status, created_at) VALUES (?, ?, ?, 0, 'active', ?)`,
		tagID, symkey, shareID, now,
	)
	if err != nil {
		return info, fmt.Errorf("insert share: %w", err)
	}
	id, _ := res.LastInsertId()

	var tagName string
	_ = m.db.QueryRow(`SELECT name FROM tags WHERE id = ?`, tagID).Scan(&tagName)

	pubKeyBytes, err := PublicKeyBytes(m.host.Peerstore().PrivKey(m.host.ID()))
	if err != nil {
		return info, fmt.Errorf("pubkey: %w", err)
	}
	s, err := EncodeShareString(pubKeyBytes, symkey)
	if err != nil {
		return info, fmt.Errorf("encode: %w", err)
	}

	m.registerPublication(id, tagID, shareID, symkey, "active")

	info = ShareInfo{
		ID: id, TagID: tagID, TagName: tagName,
		ShareString: s, Status: "active",
		Followers: 0, ClipsPushed: 0, CreatedAt: now,
	}
	m.emitEvent("share:publication-updated", info)
	m.logs.append(ShareLogEntry{
		Level: "info", Scope: "share", PublicationID: id,
		Message: fmt.Sprintf("started share for tag_id=%d", tagID),
	})
	return info, nil
}

// StopShare closes streams, drops ring entries, and deletes the shares row.
// ON DELETE CASCADE on share_ring.publication_id makes the ring cleanup
// transitive.
func (m *ShareManager) StopShare(tagID int64) error {
	m.mu.Lock()
	var pub *publication
	var id int64
	for pid, p := range m.publications {
		if p.tagID == tagID {
			pub = p
			id = pid
			break
		}
	}
	if pub != nil {
		delete(m.publications, id)
	}
	m.mu.Unlock()

	if pub != nil {
		pub.closeAllFollowers()
	}

	if _, err := m.db.Exec(`DELETE FROM shares WHERE tag_id = ?`, tagID); err != nil {
		return fmt.Errorf("delete share: %w", err)
	}
	if pub != nil {
		m.emitEvent("share:publication-removed", map[string]any{"tag_id": tagID})
		m.logs.append(ShareLogEntry{
			Level: "info", Scope: "share", PublicationID: id,
			Message: fmt.Sprintf("stopped sharing tag_id=%d", tagID),
		})
	}
	return nil
}

// PauseShare flips a publication to status="paused", closes every active
// follower stream, and persists the state so it survives restart. Incoming
// handshakes for this share_id are rejected by handlePublisherStream's
// status check. ResumeShare reverses this. Idempotent.
func (m *ShareManager) PauseShare(tagID int64) error {
	m.mu.RLock()
	var pub *publication
	for _, p := range m.publications {
		if p.tagID == tagID {
			pub = p
			break
		}
	}
	m.mu.RUnlock()
	if pub == nil {
		return fmt.Errorf("no publication for tag %d", tagID)
	}
	if pub.status == "paused" {
		return nil
	}
	if pub.status == "invalid" {
		return fmt.Errorf("cannot pause an invalid share — re-create it instead")
	}

	if _, err := m.db.Exec(`UPDATE shares SET status = 'paused' WHERE tag_id = ?`, tagID); err != nil {
		return fmt.Errorf("persist paused: %w", err)
	}
	pub.status = "paused"
	pub.closeAllFollowers()

	m.logs.append(ShareLogEntry{
		Level: "info", Scope: "share", PublicationID: pub.id,
		Message: fmt.Sprintf("paused share for tag_id=%d", tagID),
	})
	m.emitPublicationUpdated(pub, tagID)
	return nil
}

// ResumeShare flips status back to "active" and re-enables handshake
// acceptance. Existing followers reconnect on their own via runFollowLoop.
// Idempotent.
func (m *ShareManager) ResumeShare(tagID int64) error {
	m.mu.RLock()
	var pub *publication
	for _, p := range m.publications {
		if p.tagID == tagID {
			pub = p
			break
		}
	}
	m.mu.RUnlock()
	if pub == nil {
		return fmt.Errorf("no publication for tag %d", tagID)
	}
	if pub.status == "active" {
		return nil
	}
	if pub.status == "invalid" {
		return fmt.Errorf("cannot resume an invalid share — re-create it instead")
	}

	if _, err := m.db.Exec(`UPDATE shares SET status = 'active' WHERE tag_id = ?`, tagID); err != nil {
		return fmt.Errorf("persist active: %w", err)
	}
	pub.status = "active"

	m.logs.append(ShareLogEntry{
		Level: "info", Scope: "share", PublicationID: pub.id,
		Message: fmt.Sprintf("resumed share for tag_id=%d", tagID),
	})
	m.emitPublicationUpdated(pub, tagID)
	return nil
}

// PauseFollow stops the reconnect loop for a follow. It persists paused=1
// so the state survives restart, marks the in-memory flag, cancels any
// active session, and kicks runFollowLoop which then blocks in its paused
// branch. Status flips to "offline" (the session-end defer runs) but the UI
// reads the Paused flag separately. Idempotent.
func (m *ShareManager) PauseFollow(id int64) error {
	m.mu.RLock()
	f, ok := m.follows[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("no follow %d", id)
	}

	if _, err := m.db.Exec(`UPDATE follows SET paused = 1 WHERE id = ?`, id); err != nil {
		return fmt.Errorf("persist paused: %w", err)
	}
	f.mu.Lock()
	alreadyPaused := f.paused
	f.paused = true
	cancel := f.sessionCancel
	f.mu.Unlock()
	if alreadyPaused {
		return nil
	}
	if cancel != nil {
		cancel()
	}
	// Kick the loop so it re-checks paused and enters the paused branch
	// immediately instead of waiting out the backoff.
	select {
	case f.reconnectSignal <- struct{}{}:
	default:
	}

	m.logs.append(ShareLogEntry{
		Level: "info", Scope: "follow", FollowID: id,
		Message: "paused follow",
	})
	m.emitEvent("share:follow-updated", map[string]any{"id": id, "paused": true})
	return nil
}

// ResumeFollow clears the paused flag and kicks runFollowLoop so it exits
// the paused wait and tries to dial immediately. Idempotent.
func (m *ShareManager) ResumeFollow(id int64) error {
	m.mu.RLock()
	f, ok := m.follows[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("no follow %d", id)
	}

	if _, err := m.db.Exec(`UPDATE follows SET paused = 0 WHERE id = ?`, id); err != nil {
		return fmt.Errorf("persist resume: %w", err)
	}
	f.mu.Lock()
	wasPaused := f.paused
	f.paused = false
	f.mu.Unlock()
	if !wasPaused {
		return nil
	}
	select {
	case f.reconnectSignal <- struct{}{}:
	default:
	}

	m.logs.append(ShareLogEntry{
		Level: "info", Scope: "follow", FollowID: id,
		Message: "resumed follow",
	})
	m.emitEvent("share:follow-updated", map[string]any{"id": id, "paused": false})
	return nil
}

// GetShareLogs returns recent share-system log entries, newest-first. Pass
// followID or publicationID > 0 to filter; pass both zero for everything.
func (m *ShareManager) GetShareLogs(followID, publicationID int64) []ShareLogEntry {
	return m.logs.snapshot(followID, publicationID)
}

// emitPublicationUpdated is a small DRY helper — constructing a full
// ShareInfo payload requires a few queries, and Pause/Resume both need it.
func (m *ShareManager) emitPublicationUpdated(pub *publication, tagID int64) {
	var tagName string
	_ = m.db.QueryRow(`SELECT name FROM tags WHERE id = ?`, tagID).Scan(&tagName)
	var clipsSent, createdAt, lastSeqDB int64
	_ = m.db.QueryRow(`SELECT clips_sent, last_seq, created_at FROM shares WHERE id = ?`, pub.id).Scan(&clipsSent, &lastSeqDB, &createdAt)
	pub.fmu.Lock()
	fCount := len(pub.followers)
	pub.fmu.Unlock()
	pubKeyBytes, _ := PublicKeyBytes(m.host.Peerstore().PrivKey(m.host.ID()))
	shareStr, _ := EncodeShareString(pubKeyBytes, pub.symkey)
	m.emitEvent("share:publication-updated", ShareInfo{
		ID: pub.id, TagID: tagID, TagName: tagName,
		ShareString: shareStr, Status: pub.status,
		Followers: fCount, ClipsPushed: clipsSent,
		LastSeq:   lastSeqDB,
		CreatedAt: createdAt,
	})
}

// emitEvent forwards a frontend event if an emitter is installed.
func (m *ShareManager) emitEvent(name string, data any) {
	if m.eventFn != nil {
		m.eventFn(name, data)
	}
}

// OnClipCreated is the hook called by App after every clip insert. It finds
// every publication whose tag is on the clip and emits a chunked envelope
// burst into share_ring (and to any connected followers).
func (m *ShareManager) OnClipCreated(clipID int64, tagIDs []int64) error {
	m.mu.RLock()
	var matches []*publication
	for _, p := range m.publications {
		if p.status != "active" {
			continue
		}
		for _, tid := range tagIDs {
			if tid == p.tagID {
				matches = append(matches, p)
				break
			}
		}
	}
	m.mu.RUnlock()
	if len(matches) == 0 {
		return nil
	}

	var contentType, filename, metaJSON string
	var totalSize int64
	err := m.db.QueryRow(
		`SELECT content_type, COALESCE(filename,''), COALESCE(metadata,'{}'), LENGTH(data) FROM clips WHERE id = ?`,
		clipID,
	).Scan(&contentType, &filename, &metaJSON, &totalSize)
	if err != nil {
		return fmt.Errorf("read clip %d: %w", clipID, err)
	}

	metadata := map[string]string{}
	_ = json.Unmarshal([]byte(metaJSON), &metadata)

	chunkCount := uint32((totalSize + int64(ChunkSize) - 1) / int64(ChunkSize))
	if chunkCount == 0 {
		chunkCount = 1 // empty clip still emits one zero-length chunk for symmetry
	}

	for _, p := range matches {
		if err := m.emitClipForPublication(p, clipID, contentType, filename, metadata, totalSize, chunkCount); err != nil {
			log.Printf("share: emit clip %d to pub %d: %v", clipID, p.id, err)
		}
	}
	return nil
}

func (m *ShareManager) emitClipForPublication(
	p *publication,
	clipID int64,
	contentType, filename string,
	metadata map[string]string,
	totalSize int64,
	chunkCount uint32,
) error {
	// Hold p.fmu for the ENTIRE emission so that handlePublisherStream's
	// ring-catchup-then-register path cannot interleave with a clip's
	// envelopes. Without this lock a new follower registering mid-emission
	// could miss chunks that were fan-out'd before it joined but had not yet
	// been written to the ring (live fan-out fires per envelope, ring
	// commits at tx commit). All callers of liveFanOutLocked below rely on
	// the caller holding p.fmu, so every envelope is emitted to the stable
	// follower set captured at the start of the emission.
	p.fmu.Lock()
	defer p.fmu.Unlock()

	tx, err := m.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var seqI int64
	if err := tx.QueryRow(`SELECT last_seq FROM shares WHERE id = ?`, p.id).Scan(&seqI); err != nil {
		return fmt.Errorf("read last_seq: %w", err)
	}
	lastSeq := uint64(seqI)

	hasher := sha256.New()
	now := time.Now().Unix()
	tsMillis := time.Now().UnixMilli()
	nextSeq := lastSeq + 1

	// 1) clip_start
	start := ClipStartPayload{
		Seq: nextSeq, TS: tsMillis, Kind: KindClipStart,
		ClipID: uint64(clipID), Filename: filename,
		ContentType: contentType, Metadata: metadata,
		TotalSize: uint64(totalSize), ChunkCount: chunkCount,
	}
	startBytes, _ := MarshalPayload(start)
	startEnv, err := EncryptEnvelope(p.symkey, p.shareID, nextSeq, startBytes)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(
		`INSERT INTO share_ring (publication_id, seq, kind, envelope_bytes, ts) VALUES (?, ?, ?, ?, ?)`,
		p.id, int64(nextSeq), KindClipStart, startEnv, now,
	); err != nil {
		return err
	}
	m.liveFanOutLocked(p, startEnv)
	nextSeq++

	// 2) clip_chunk × N — streamed via SUBSTR so we never hold the full blob in RAM.
	for idx := uint32(0); idx < chunkCount; idx++ {
		offset := int64(idx) * int64(ChunkSize)
		length := int64(ChunkSize)
		if offset+length > totalSize {
			length = totalSize - offset
			if length < 0 {
				length = 0
			}
		}
		var chunk []byte
		if err := tx.QueryRow(
			`SELECT SUBSTR(data, ?, ?) FROM clips WHERE id = ?`,
			offset+1, length, clipID,
		).Scan(&chunk); err != nil {
			return fmt.Errorf("read chunk %d: %w", idx, err)
		}
		hasher.Write(chunk)

		cp := ClipChunkPayload{
			Seq: nextSeq, Kind: KindClipChunk,
			ClipID: uint64(clipID), Index: idx, Data: chunk,
		}
		cpBytes, _ := MarshalPayload(cp)
		cpEnv, err := EncryptEnvelope(p.symkey, p.shareID, nextSeq, cpBytes)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(
			`INSERT INTO share_ring (publication_id, seq, kind, envelope_bytes, ts) VALUES (?, ?, ?, ?, ?)`,
			p.id, int64(nextSeq), KindClipChunk, cpEnv, now,
		); err != nil {
			return err
		}
		m.liveFanOutLocked(p, cpEnv)
		nextSeq++
	}

	// 3) clip_end
	end := ClipEndPayload{
		Seq: nextSeq, Kind: KindClipEnd,
		ClipID: uint64(clipID), SHA256: hasher.Sum(nil),
	}
	endBytes, _ := MarshalPayload(end)
	endEnv, err := EncryptEnvelope(p.symkey, p.shareID, nextSeq, endBytes)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(
		`INSERT INTO share_ring (publication_id, seq, kind, envelope_bytes, ts) VALUES (?, ?, ?, ?, ?)`,
		p.id, int64(nextSeq), KindClipEnd, endEnv, now,
	); err != nil {
		return err
	}
	m.liveFanOutLocked(p, endEnv)

	// Bump last_seq AND clips_sent (one clip_end = one published clip). They
	// stay in sync by construction.
	if _, err := tx.Exec(`UPDATE shares SET last_seq = ?, clips_sent = clips_sent + 1 WHERE id = ?`, int64(nextSeq), p.id); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	// Ring eviction (age + cap) outside the tx.
	if err := RingEvict(m.db, time.Now().Unix(), int64(RingBytesCapPerPub)); err != nil {
		log.Printf("share: evict: %v", err)
	}
	return nil
}

// liveFanOutLocked enqueues an envelope onto every connected follower's send
// queue. enqueue is non-blocking; if either the envelope slot cap or the
// byte cap is exceeded for a follower, enqueue closes that follower and
// the drain goroutine in handlePublisherStream will remove the entry.
//
// The caller MUST hold p.fmu. This invariant lets emitClipForPublication
// hold p.fmu for the entire emission of a clip's envelopes so that a
// newly-handshaking follower in handlePublisherStream cannot register
// partway through — a new follower either sees the whole clip via the
// ring catch-up path or via live fan-out, never a mix.
func (m *ShareManager) liveFanOutLocked(p *publication, envelope []byte) {
	for _, fc := range p.followers {
		fc.enqueue(envelope)
	}
}

// Sentinel errors surfaced by Follow, TestFollowConnection, and FollowWithoutDial.
// The Wails boundary flattens errors to plain strings on the frontend, so UI
// code pattern-matches on the wrapped message; backend callers can use
// errors.Is against these values.
var (
	ErrSelfFollow      = errors.New("cannot follow your own share")
	ErrInvalidShareStr = errors.New("invalid share string")
)

// decodeAndCheckSelf parses a share string into (peerID, symkey) and rejects
// self-follows. Malformed input wraps ErrInvalidShareStr.
func (m *ShareManager) decodeAndCheckSelf(shareString string) (peer.ID, []byte, error) {
	peerIDBytes, symkey, err := DecodeShareString(shareString)
	if err != nil {
		return "", nil, fmt.Errorf("%w: %w", ErrInvalidShareStr, err)
	}
	pubKey, err := cryptoPublicKeyFromBytes(peerIDBytes)
	if err != nil {
		return "", nil, fmt.Errorf("%w: peer id from key: %w", ErrInvalidShareStr, err)
	}
	pid, err := peer.IDFromPublicKey(pubKey)
	if err != nil {
		return "", nil, fmt.Errorf("%w: peer id: %w", ErrInvalidShareStr, err)
	}
	if pid == m.host.ID() {
		return "", nil, ErrSelfFollow
	}
	return pid, symkey, nil
}

// followCommit resolves/creates the local tag, inserts the follows row, starts
// runFollowLoop, and emits share:follow-updated. Shared by Follow (after a
// successful initial dial) and FollowWithoutDial (which skips the dial).
func (m *ShareManager) followCommit(pid peer.ID, symkey []byte, localTagName string) (FollowInfo, error) {
	var info FollowInfo

	localTagID, err := m.resolveOrCreateTag(localTagName)
	if err != nil {
		return info, fmt.Errorf("tag: %w", err)
	}

	now := time.Now().Unix()
	res, err := m.db.Exec(
		`INSERT INTO follows (remote_peer_id, symkey, local_tag_id, last_seq, last_seen_at, created_at) VALUES (?, ?, ?, 0, ?, ?)`,
		pid.String(), symkey, localTagID, now, now,
	)
	if err != nil {
		return info, fmt.Errorf("insert follow: %w", err)
	}
	id, _ := res.LastInsertId()

	fctx, fcancel := context.WithCancel(m.ctx)
	f := &follow{
		id: id, remotePeerID: pid, symkey: symkey,
		localTagID: localTagID, lastSeq: 0,
		// "connecting" = row inserted, runFollowLoop about to start. flips to
		// "connected" only when followSession completes the handshake, so the
		// TestFollowSessionEndFlipsStatusOffline poll gates on an actual live
		// session (and DisconnectFollowForTest has something to cancel).
		status: "connecting", ctx: fctx, cancel: fcancel,
		reconnectSignal: make(chan struct{}, 1),
	}
	m.mu.Lock()
	m.follows[id] = f
	m.mu.Unlock()

	go m.runFollowLoop(f)

	info = FollowInfo{
		ID: id, RemotePeerID: pid.String(),
		LocalTagID: localTagID, LocalTagName: localTagName,
		Status: "connected", ClipsReceived: 0, LastSeq: 0,
		CreatedAt: now,
	}
	m.emitEvent("share:follow-updated", info)
	return info, nil
}

// Follow validates a share string, dials the peer, creates/resolves the local
// tag, persists a follows row, and starts the background reconnect loop.
func (m *ShareManager) Follow(shareString, localTagName string) (FollowInfo, error) {
	var info FollowInfo
	pid, symkey, err := m.decodeAndCheckSelf(shareString)
	if err != nil {
		return info, err
	}
	dctx, dcancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer dcancel()
	if err := m.dialByPeerID(dctx, pid); err != nil {
		return info, fmt.Errorf("initial dial: %w", err)
	}
	return m.followCommit(pid, symkey, localTagName)
}

// TestFollowConnection probes reachability without committing a follow. Uses a
// 5s timeout — tighter than Follow's 10s because the UI shows a spinner while
// this runs and should fail fast so the user can Retry or fall back to
// FollowWithoutDial.
func (m *ShareManager) TestFollowConnection(shareString string) error {
	pid, _, err := m.decodeAndCheckSelf(shareString)
	if err != nil {
		return err
	}
	dctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
	defer cancel()
	if err := m.dialByPeerID(dctx, pid); err != nil {
		return fmt.Errorf("initial dial: %w", err)
	}
	return nil
}

// FollowWithoutDial commits a follow without requiring an initial dial to
// succeed. The frontend's "Follow anyway" escape hatch calls this when
// TestFollowConnection has already failed — runFollowLoop retries through its
// normal backoff until the peer comes online.
func (m *ShareManager) FollowWithoutDial(shareString, localTagName string) (FollowInfo, error) {
	var info FollowInfo
	pid, symkey, err := m.decodeAndCheckSelf(shareString)
	if err != nil {
		return info, err
	}
	return m.followCommit(pid, symkey, localTagName)
}

// UpdateFollowTag changes which local tag receives clips from this follow
// going forward. Existing received clips are NOT re-tagged — they keep their
// original tag. Resolves or creates the new tag (slash-paths supported).
func (m *ShareManager) UpdateFollowTag(followID int64, newLocalTagName string) (FollowInfo, error) {
	var info FollowInfo
	if newLocalTagName == "" {
		return info, errors.New("local tag name required")
	}

	newTagID, err := m.resolveOrCreateTag(newLocalTagName)
	if err != nil {
		return info, fmt.Errorf("tag: %w", err)
	}

	m.mu.RLock()
	f, ok := m.follows[followID]
	m.mu.RUnlock()
	if !ok {
		return info, fmt.Errorf("follow %d not found", followID)
	}

	if _, err := m.db.Exec(`UPDATE follows SET local_tag_id = ? WHERE id = ?`, newTagID, followID); err != nil {
		return info, fmt.Errorf("update follow: %w", err)
	}

	// Swap the in-memory id under the follow's lock. followSession reads
	// f.localTagID per clip-end; a clip already in flight may still land
	// under the old tag, but every clip after this point uses the new one.
	f.mu.Lock()
	f.localTagID = newTagID
	status := f.status
	f.mu.Unlock()

	// Look up other fields needed for FollowInfo so callers (and the UI
	// event listener) get a complete row without a separate refresh.
	var createdAt, lastSeqDB, clipsRecv int64
	var lastSeenSQL sql.NullInt64
	_ = m.db.QueryRow(
		`SELECT created_at, last_seq, clips_received, last_seen_at FROM follows WHERE id = ?`,
		followID,
	).Scan(&createdAt, &lastSeqDB, &clipsRecv, &lastSeenSQL)
	var lastSeenPtr *int64
	if lastSeenSQL.Valid {
		v := lastSeenSQL.Int64
		lastSeenPtr = &v
	}

	info = FollowInfo{
		ID: followID, RemotePeerID: f.remotePeerID.String(),
		LocalTagID: newTagID, LocalTagName: newLocalTagName,
		Status:        status,
		ClipsReceived: clipsRecv,
		LastSeq:       lastSeqDB,
		LastSeenAt:    lastSeenPtr,
		CreatedAt:     createdAt,
	}
	m.emitEvent("share:follow-updated", info)
	return info, nil
}

// Unfollow cancels the reconnect loop and deletes the follows row.
func (m *ShareManager) Unfollow(followID int64) error {
	m.mu.Lock()
	f, ok := m.follows[followID]
	if ok {
		delete(m.follows, followID)
	}
	m.mu.Unlock()
	if ok {
		f.cancel()
	}
	if _, err := m.db.Exec(`DELETE FROM follows WHERE id = ?`, followID); err != nil {
		return err
	}
	m.emitEvent("share:follow-removed", map[string]any{"id": followID})
	return nil
}

// dialByPeerID finds addrs via peerstore (fast path) or DHT (slow path) and
// opens the stream with a handshake at since_seq = lastSeq.
func (m *ShareManager) dialByPeerID(ctx context.Context, pid peer.ID) error {
	// Fast path: cached addrs.
	if addrs := m.host.Peerstore().Addrs(pid); len(addrs) > 0 {
		if err := m.host.Connect(ctx, peer.AddrInfo{ID: pid, Addrs: addrs}); err == nil {
			return nil
		}
	}
	// Slow path: DHT FindPeer.
	fctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ai, err := m.dht.FindPeer(fctx, pid)
	if err != nil {
		return fmt.Errorf("dht find peer: %w", err)
	}
	m.host.Peerstore().AddAddrs(pid, ai.Addrs, time.Hour)
	return m.host.Connect(ctx, ai)
}

// runFollowLoop is the reconnect + receive loop for one follow.
//
// Wake-up sources once a session has ended OR while paused:
//   - f.ctx.Done()       — Unfollow: exit the loop.
//   - f.reconnectSignal  — user action (ReconnectFollow or ResumeFollow):
//     wake now, reset the backoff ladder, re-check paused.
//   - time.After(backoff) — normal exponential backoff between dial retries
//     (ReconnectFloor → ReconnectCap, doubling each natural retry).
//
// Paused follows skip the session entirely and block on reconnectSignal/ctx.
// Resuming kicks the signal which drops us back to the session attempt.
func (m *ShareManager) runFollowLoop(f *follow) {
	backoff := ReconnectFloor
	for {
		f.mu.Lock()
		paused := f.paused
		f.mu.Unlock()
		if paused {
			// Block until the user resumes or unfollows. No dial attempts.
			select {
			case <-f.ctx.Done():
				return
			case <-f.reconnectSignal:
				backoff = ReconnectFloor
				continue
			}
		}

		if err := m.followSession(f); err != nil {
			m.logs.append(ShareLogEntry{
				Level: "warn", Scope: "follow", FollowID: f.id,
				Message: "session ended: " + err.Error(),
			})
		}
		select {
		case <-f.ctx.Done():
			return
		case <-f.reconnectSignal:
			backoff = ReconnectFloor
		case <-time.After(backoff):
			backoff *= 2
			if backoff > ReconnectCap {
				backoff = ReconnectCap
			}
		}
	}
}

// followSession dials, handshakes, and streams until either the stream
// closes on its own or the per-session context is canceled (e.g. by
// DisconnectFollowForTest). Returns when the session ends for any reason;
// the caller (runFollowLoop) then decides whether to reconnect.
//
// Status discipline: status flips to "connected" exactly once the
// handshake is written; the deferred cleanup flips it back to "offline"
// on every exit path (dial error, handshake error, mid-stream cancel,
// EOF, consumeStream error). This guarantees the UI card reflects real
// transport state regardless of which branch returns.
func (m *ShareManager) followSession(f *follow) error {
	sessCtx, sessCancel := context.WithCancel(f.ctx)
	f.mu.Lock()
	f.sessionCancel = sessCancel
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.sessionCancel = nil
		f.mu.Unlock()
		sessCancel()
		// Session ended — always surface offline so the UI and tests have
		// a reliable signal that catch-up (not live) is next on the wire.
		m.setFollowStatus(f, "offline")
	}()

	if err := m.dialByPeerID(sessCtx, f.remotePeerID); err != nil {
		return err
	}
	s, err := m.host.NewStream(sessCtx, f.remotePeerID, ShareProtocolID)
	if err != nil {
		return err
	}
	defer s.Reset()

	// Cancel the stream whenever the session context is canceled (covers
	// DisconnectFollowForTest which may fire mid-read on consumeStream).
	go func() {
		<-sessCtx.Done()
		_ = s.Reset()
	}()

	shareID := DeriveShareID(f.symkey)
	hs := BuildHandshake(f.symkey, shareID, f.lastSeq)
	if _, err := s.Write(hs); err != nil {
		return err
	}
	m.setFollowStatus(f, "connected")
	m.logs.append(ShareLogEntry{
		Level: "info", Scope: "follow", FollowID: f.id,
		Message: fmt.Sprintf("handshake complete with %s", f.remotePeerID.String()),
	})

	return m.consumeStream(sessCtx, f, s)
}

// consumeStream reads frames until the session context is canceled or the
// stream closes. Takes the session ctx (not the follow-lifetime ctx) so
// DisconnectFollowForTest ends reads promptly without killing the follow.
func (m *ShareManager) consumeStream(sessCtx context.Context, f *follow, r io.Reader) error {
	asm := newClipAssembler(filepath.Join(m.dataDir, ShareStagingDirName))
	shareID := DeriveShareID(f.symkey)
	for {
		select {
		case <-sessCtx.Done():
			return sessCtx.Err()
		default:
		}
		frame, err := ReadFrame(r)
		if err != nil {
			return err
		}
		// Seq is part of AAD; we need it to decrypt. Because we don't know it
		// yet, peek into the CBOR after decrypting with candidate seqs — but
		// we can cheat: plaintext also includes the seq. Try lastSeq+1 first.
		pt, err := DecryptEnvelope(f.symkey, shareID, f.lastSeq+1, frame)
		if err != nil {
			// Retransmit may skip seqs (eviction gap); try bumping.
			var ok bool
			for jump := uint64(2); jump < 32; jump++ {
				pt, err = DecryptEnvelope(f.symkey, shareID, f.lastSeq+jump, frame)
				if err == nil {
					f.lastSeq += (jump - 1) // advance over the gap; will be fixed to exact in next step
					ok = true
					break
				}
			}
			if !ok {
				return fmt.Errorf("decrypt failed: %w", err)
			}
		}
		kind, raw, err := PeekPayloadKind(pt)
		if err != nil {
			return err
		}
		// Trust the seq that's inside the plaintext — that's the authoritative one.
		var inSeq uint64
		switch kind {
		case KindClipStart:
			var p ClipStartPayload
			if err := UnmarshalPayload(raw, &p); err != nil {
				return err
			}
			inSeq = p.Seq
			asm.onStart(p)
		case KindClipChunk:
			var p ClipChunkPayload
			if err := UnmarshalPayload(raw, &p); err != nil {
				return err
			}
			inSeq = p.Seq
			asm.onChunk(p)
		case KindClipEnd:
			var p ClipEndPayload
			if err := UnmarshalPayload(raw, &p); err != nil {
				return err
			}
			inSeq = p.Seq
			newClipID, err := asm.onEnd(p, m.db, f.localTagID)
			if err != nil {
				log.Printf("share: assemble failed: %v", err)
			} else {
				// Assembly succeeded → bump clips_received and tell the UI
				// so it can surface the new clip without a full reload.
				_, _ = m.db.Exec(`UPDATE follows SET clips_received = clips_received + 1 WHERE id = ?`, f.id)
				m.emitEvent("share:clip-received", map[string]any{
					"clip_id":      newClipID,
					"local_tag_id": f.localTagID,
					"follow_id":    f.id,
				})
				m.logs.append(ShareLogEntry{
					Level: "info", Scope: "follow", FollowID: f.id,
					Message: fmt.Sprintf("received clip id=%d", newClipID),
				})
			}
		case KindGap:
			var p GapPayload
			if err := UnmarshalPayload(raw, &p); err != nil {
				return err
			}
			inSeq = p.Seq
		}
		f.lastSeq = inSeq
		_, _ = m.db.Exec(`UPDATE follows SET last_seq = ?, last_seen_at = ? WHERE id = ?`, int64(inSeq), time.Now().Unix(), f.id)
	}
}

func (m *ShareManager) setFollowStatus(f *follow, s string) {
	f.mu.Lock()
	f.status = s
	f.mu.Unlock()
	m.emitEvent("share:follow-updated", map[string]any{"id": f.id, "status": s})
}

// resolveOrCreateTag looks up the tag row for the given name, creating it if
// missing. Validates against the same rules as App.CreateTag (empty segments,
// reserved "_api", length limit) so follow flows can't create malformed rows.
func (m *ShareManager) resolveOrCreateTag(name string) (int64, error) {
	name, err := validateTagName(name)
	if err != nil {
		return 0, err
	}
	var id int64
	if err := m.db.QueryRow(`SELECT id FROM tags WHERE name = ?`, name).Scan(&id); err == nil {
		return id, nil
	} else if err != sql.ErrNoRows {
		return 0, err
	}
	res, err := m.db.Exec(`INSERT INTO tags (name, color) VALUES (?, '#888')`, name)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// cryptoPublicKeyFromBytes reconstructs a libp2p PubKey from 32 raw Ed25519 bytes.
func cryptoPublicKeyFromBytes(raw []byte) (crypto.PubKey, error) {
	if len(raw) != 32 {
		return nil, errors.New("pubkey must be 32 bytes")
	}
	return crypto.UnmarshalEd25519PublicKey(raw)
}

// ResumeAll loads every persisted publication and follow into memory and
// starts background loops. Called once from App.startup after Init.
func (m *ShareManager) ResumeAll() error {
	// Publications
	rows, err := m.db.Query(`SELECT id, tag_id, symkey, share_id, status FROM shares`)
	if err != nil {
		return fmt.Errorf("query shares: %w", err)
	}
	for rows.Next() {
		var id, tagID int64
		var symkey, shareID []byte
		var status string
		if err := rows.Scan(&id, &tagID, &symkey, &shareID, &status); err != nil {
			rows.Close()
			return err
		}
		m.registerPublication(id, tagID, shareID, symkey, status)
	}
	rows.Close()

	// Follows
	frows, err := m.db.Query(`SELECT id, remote_peer_id, symkey, local_tag_id, last_seq, paused FROM follows`)
	if err != nil {
		return fmt.Errorf("query follows: %w", err)
	}
	defer frows.Close()
	for frows.Next() {
		var id, localTagID, lastSeqI int64
		var pidStr string
		var symkey []byte
		var pausedI int64
		if err := frows.Scan(&id, &pidStr, &symkey, &localTagID, &lastSeqI, &pausedI); err != nil {
			return err
		}
		pid, err := peer.Decode(pidStr)
		if err != nil {
			log.Printf("share: resume skip follow %d: bad peer id %q", id, pidStr)
			continue
		}
		fctx, fcancel := context.WithCancel(m.ctx)
		f := &follow{
			id: id, remotePeerID: pid, symkey: symkey,
			localTagID: localTagID, lastSeq: uint64(lastSeqI),
			status: "offline", ctx: fctx, cancel: fcancel,
			reconnectSignal: make(chan struct{}, 1),
			paused:          pausedI != 0,
		}
		m.mu.Lock()
		m.follows[id] = f
		m.mu.Unlock()
		// runFollowLoop honors f.paused at the top of every iteration, so
		// paused follows enter the paused branch immediately and stay idle
		// until ResumeFollow.
		go m.runFollowLoop(f)
	}
	return nil
}

// ReconnectFollow forces an immediate dial/handshake cycle for a follow. It
// cancels the live session (if any) so followSession returns to the loop and
// kicks runFollowLoop out of its backoff wait, so the next dial happens now
// rather than after up to ReconnectCap. The follow row, last_seq, and
// follow-lifetime context are preserved.
//
// Status transitions are unchanged: the session defer will flip to "offline"
// on exit, then "connected" on the next successful handshake. The existing
// share:follow-updated event stream surfaces both flips to the UI.
func (m *ShareManager) ReconnectFollow(id int64) error {
	m.mu.RLock()
	f, ok := m.follows[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("no follow %d", id)
	}

	f.mu.Lock()
	cancel := f.sessionCancel
	f.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	select {
	case f.reconnectSignal <- struct{}{}:
	default:
	}
	return nil
}

// DisconnectFollowForTest cancels the follow's CURRENT SESSION context so
// the active session exits (unblocks consumeStream, resets the stream)
// but the follow-lifetime context, the follows row, and last_seq all
// stay. The runFollowLoop re-enters its backoff → reconnect path.
//
// Thread safety: reads and swaps f.sessionCancel under f.mu. Never
// touches f.ctx or f.cancel — those belong to Unfollow.
func (m *ShareManager) DisconnectFollowForTest(id int64) error {
	m.mu.RLock()
	f, ok := m.follows[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("no follow %d", id)
	}

	f.mu.Lock()
	cancel := f.sessionCancel
	f.mu.Unlock()
	if cancel == nil {
		// No live session to kill. Nothing to do — the loop is already
		// between sessions (in its backoff wait); it will redial shortly.
		return nil
	}
	cancel()
	return nil
}

// AgeShareRingForTest rewinds every share_ring row's `ts` column by the given
// number of seconds. Used by e2e tests that simulate TTL expiry without waiting
// an actual hour. test-only — must not be wired to production code.
func (m *ShareManager) AgeShareRingForTest(seconds int64) error {
	_, err := m.db.Exec(`UPDATE share_ring SET ts = ts - ?`, seconds)
	return err
}

// startSweepers launches background timers for ring eviction and staging cleanup.
// Call once from App.startup after ResumeAll.
func (m *ShareManager) startSweepers() {
	go m.runRingSweeper()
	go m.runStagingJanitor()
}

func (m *ShareManager) runRingSweeper() {
	// Run once immediately (catches stale rows from restored backups).
	if err := RingEvict(m.db, time.Now().Unix(), int64(RingBytesCapPerPub)); err != nil {
		log.Printf("share: initial ring evict: %v", err)
	}
	t := time.NewTicker(RingSweepInterval)
	defer t.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-t.C:
			if err := RingEvict(m.db, time.Now().Unix(), int64(RingBytesCapPerPub)); err != nil {
				log.Printf("share: ring evict: %v", err)
			}
		}
	}
}

// GetShareStatus returns DTOs for every publication and follow currently
// registered, combining in-memory state with DB counters.
func (m *ShareManager) GetShareStatus() (shares []ShareInfo, follows []FollowInfo) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, p := range m.publications {
		var tagName string
		m.db.QueryRow(`SELECT name FROM tags WHERE id = ?`, p.tagID).Scan(&tagName)
		var clipsSent, createdAt, lastSeqDB int64
		// Authoritative counters live in the DB; no envelope-count heuristics.
		m.db.QueryRow(`SELECT clips_sent, last_seq, created_at FROM shares WHERE id = ?`, p.id).Scan(&clipsSent, &lastSeqDB, &createdAt)
		p.fmu.Lock()
		fCount := len(p.followers)
		p.fmu.Unlock()

		// Reconstruct share string from stored key + our pubkey.
		pubKeyBytes, _ := PublicKeyBytes(m.host.Peerstore().PrivKey(m.host.ID()))
		shareStr, _ := EncodeShareString(pubKeyBytes, p.symkey)

		shares = append(shares, ShareInfo{
			ID: p.id, TagID: p.tagID, TagName: tagName,
			ShareString: shareStr, Status: p.status,
			Followers: fCount, ClipsPushed: clipsSent,
			LastSeq:   lastSeqDB,
			CreatedAt: createdAt,
		})
	}

	for _, f := range m.follows {
		var localTagName string
		var createdAt, lastSeqDB, clipsRecv int64
		var lastSeenSQL sql.NullInt64
		m.db.QueryRow(`SELECT name FROM tags WHERE id = ?`, f.localTagID).Scan(&localTagName)
		m.db.QueryRow(`SELECT created_at, last_seq, clips_received, last_seen_at FROM follows WHERE id = ?`, f.id).Scan(&createdAt, &lastSeqDB, &clipsRecv, &lastSeenSQL)
		var lastSeenPtr *int64
		if lastSeenSQL.Valid {
			v := lastSeenSQL.Int64
			lastSeenPtr = &v
		}
		f.mu.Lock()
		status := f.status
		paused := f.paused
		f.mu.Unlock()
		follows = append(follows, FollowInfo{
			ID: f.id, RemotePeerID: f.remotePeerID.String(),
			LocalTagID: f.localTagID, LocalTagName: localTagName,
			Status:        status,
			Paused:        paused,
			ClipsReceived: clipsRecv,
			LastSeq:       lastSeqDB, LastSeenAt: lastSeenPtr,
			CreatedAt: createdAt,
		})
	}
	return
}

func (m *ShareManager) runStagingJanitor() {
	dir := filepath.Join(m.dataDir, ShareStagingDirName)
	cleanup := func() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		cutoff := time.Now().Add(-StagingMaxAge)
		for _, e := range entries {
			info, err := e.Info()
			if err != nil {
				continue
			}
			if info.ModTime().Before(cutoff) {
				_ = os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
	cleanup()
	t := time.NewTicker(StagingSweepInterval)
	defer t.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-t.C:
			cleanup()
		}
	}
}
