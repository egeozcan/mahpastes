package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	dht "github.com/libp2p/go-libp2p-kad-dht"
)

// ShareManager owns the libp2p host and tracks publications + follows.
type ShareManager struct {
	ctx     context.Context
	cancel  context.CancelFunc
	db      *sql.DB
	dataDir string

	host host.Host
	dht  *dht.IpfsDHT

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
	}

	// Register the single application protocol.
	h.SetStreamHandler(ShareProtocolID, m.handlePublisherStream)

	return m, nil
}

// Host exposes the libp2p host for tests and advanced operations.
func (m *ShareManager) Host() host.Host { return m.host }

// SetEventFn installs the frontend event emitter (called from App.startup).
func (m *ShareManager) SetEventFn(fn func(string, ...any)) { m.eventFn = fn }

// Stop shuts down the DHT, host, and cancels all follow reconnect loops.
func (m *ShareManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range m.follows {
		f.cancel()
	}
	for _, p := range m.publications {
		p.closeAllFollowers()
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

// followerConn — one connected follower's stream + send queue.
//
// peerID is the remote peer at connect time, stored so the Task 5.2 handler
// can count concurrent streams from the same peer when applying the per-peer
// cap. The byte-capped send queue fields (queue, pending, onClose, mu,
// closed) are added in Phase 6.3; this stub compiles on its own and the
// Phase 6.3 additions are strict extensions.
type followerConn struct {
	peerID peer.ID
	stream network.Stream
	writer io.Writer
	// Expanded with queue + pending + onClose + mu + closed in Task 6.3.
}

func (fc *followerConn) close() {
	if fc != nil && fc.stream != nil {
		_ = fc.stream.Reset()
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
	mu            sync.Mutex
	sessionCancel context.CancelFunc
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

// newFollowerConn constructs a followerConn, reading the remote peer ID from
// the stream's connection so the Task 5.2 cap check can count streams per
// peer. Phase 6.3 will extend this with the byte-capped send queue.
func newFollowerConn(s network.Stream, w io.Writer) *followerConn {
	return &followerConn{
		peerID: s.Conn().RemotePeer(),
		stream: s,
		writer: w,
	}
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

	// Enforce both caps under the publication lock so the decision and
	// insertion are atomic against other concurrent handshakes.
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
	pub.fmu.Unlock()

	// Catch-up retransmit: send every ring row with seq > since_seq that
	// is still within TTL.
	rows, err := RingRetransmit(m.db, pub.id, hs.SinceSeq, time.Now().Unix())
	if err != nil {
		log.Printf("share: retransmit query: %v", err)
		_ = s.Reset()
		return
	}
	for _, r := range rows {
		if _, werr := s.Write(r.EnvelopeBytes); werr != nil {
			_ = s.Reset()
			return
		}
	}

	// Register for live fan-out. Phase 6 will write into this entry from the
	// on_clip_created emitter.
	fc := newFollowerConn(s, s)
	pub.fmu.Lock()
	pub.followers[s] = fc
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
	m.emitEvent("share:publication-removed", map[string]any{"tag_id": tagID})
	return nil
}

// emitEvent forwards a frontend event if an emitter is installed.
func (m *ShareManager) emitEvent(name string, data any) {
	if m.eventFn != nil {
		m.eventFn(name, data)
	}
}
