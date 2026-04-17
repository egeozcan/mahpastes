package main

import (
	"context"
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

// handlePublisherStream is installed as the libp2p stream handler. Phase 5.2
// fills in the handshake + retransmit logic.
func (m *ShareManager) handlePublisherStream(s network.Stream) {
	_ = s.Reset() // stub: reject until Phase 5.2 implements
}

// stub for Phase 8 — prevents unused time import warning.
var _ = time.Second
