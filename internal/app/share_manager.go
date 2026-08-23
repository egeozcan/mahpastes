package app

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
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
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

	host    host.Host
	dht     *dht.IpfsDHT
	mdnsSvc mdns.Service

	mu           sync.RWMutex
	publications map[int64]*publication // keyed by shares.id
	follows      map[int64]*follow      // keyed by follows.id

	// eventFn is the runtime.EventsEmit bound at App startup time
	// (lets the manager push card updates to the frontend without a
	// direct dependency on the Wails runtime). evMu guards it: the libp2p
	// stream handler is installed by NewShareManager, so background
	// goroutines are already live by the time App.startup calls SetEventFn.
	evMu    sync.RWMutex
	eventFn func(name string, data ...any)

	// testHookBeforeRegister — test-only. Invoked by handlePublisherStream
	// after the handshake verifies but before pub.fmu is taken, so a test
	// can land a teardown in exactly that window. Set it before the
	// publication is registered: registerPublication's m.mu write is the
	// happens-before edge to the handler's read.
	testHookBeforeRegister func(*publication)

	// testHookAfterStopMark — test-only. Invoked by StopShare once the
	// publication is marked removed and its followers are closed, but
	// before the shares row is deleted. That ordering is the whole point:
	// after the DELETE a late handshake fails its own last_seq lookup and
	// resets by accident, so only a hook placed here can exercise the
	// removed check on its merits. Set it before StartShare.
	testHookAfterStopMark func()

	// maxStreamsPerPubOverride — test-only knob. 0 means "use
	// MaxStreamsPerPublication". setMaxStreamsPerPubForTest sets this so
	// cap-enforcement can be exercised without saturating with 128 real
	// libp2p streams.
	maxStreamsPerPubOverride int

	// maxShareableClipOverride — test-only knob. 0 means "use
	// MaxShareableClipBytes". Lets the oversized-clip refusal be exercised
	// without allocating a 30 MiB clip in a unit test.
	maxShareableClipOverride int64

	// catchupCapsOverride — test-only knob. nil means defaultCatchupCaps().
	// Lets a test drive multi-batch catch-up with a handful of small clips
	// instead of the 16 MiB of real data the production budget would need.
	catchupCapsOverride *catchupCaps

	// logs is an in-memory ring buffer of share-system events surfaced to
	// the UI via GetShareLogs. Never nil after NewShareManager.
	logs *shareLogBuffer

	// loopWG tracks every goroutine the manager spawns that can touch the
	// database (see spawnLoop/admitLoop). Stop waits on it, bounded, so a
	// caller that stops the manager before replacing the database
	// (RestoreBackup) knows no receive loop, sweeper, or in-flight publisher
	// handshake is still mid-work. loopMu guards stopping, which flips once
	// in Stop and makes every later admission refuse — without the gate, an
	// Add racing Stop's Wait is invalid WaitGroup usage and the late
	// goroutine escapes the drain entirely.
	loopMu   sync.Mutex
	stopping bool
	loopWG   sync.WaitGroup
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
func (m *ShareManager) SetEventFn(fn func(string, ...any)) {
	m.evMu.Lock()
	defer m.evMu.Unlock()
	m.eventFn = fn
}

// loopStopTimeout bounds how long Stop waits for the manager's DB-touching
// goroutines (follow receive loops, ring sweeper) to exit. They normally exit
// almost instantly — ctx cancel resets every live stream, which unblocks the
// reads — so the bound only matters for a wedged loop, which must not make
// Stop (and therefore shutdown or a restore) unfinishable.
const loopStopTimeout = 5 * time.Second

// Stop shuts down mDNS, the DHT, host, cancels all follow reconnect loops, and
// waits (bounded) for the manager's goroutines to exit.
//
// The wait is what makes Stop safe to call before RestoreBackup replaces the
// database: without it, a follow receive loop descheduled mid-clip could
// resume after the replacement and run its clip insert against the restored
// rows — storing a pre-restore remote clip under a stale local tag id and
// advancing whatever restored follows row happens to carry its follow id. A
// loop that outlives the bounded wait is logged and becomes the same accepted
// straggler class as the hook drain's.
func (m *ShareManager) Stop() {
	// Close the admission gate FIRST: after this, no new follow loop,
	// sweeper, or publisher-handshake body can join loopWG, so the Wait
	// below is racing nothing (an Add from zero concurrent with Wait is
	// invalid) and nothing admitted later can escape the drain.
	m.loopMu.Lock()
	m.stopping = true
	m.loopMu.Unlock()

	m.mu.Lock()
	for _, f := range m.follows {
		f.cancel()
	}
	for _, p := range m.publications {
		// removed, not just closed: a handshake that looked this
		// publication up before the gate flipped re-checks removed under
		// fmu before registering, so it cannot attach a follower to a
		// stopped manager's corpse.
		p.fmu.Lock()
		p.removed = true
		p.closeAllFollowersLocked()
		p.fmu.Unlock()
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
	m.mu.Unlock()
	m.cancel()

	// Wait outside m.mu: the loops never take m.mu on their exit paths today,
	// but waiting under the manager lock would turn any future violation of
	// that into a deadlock instead of a slow shutdown.
	done := make(chan struct{})
	go func() {
		m.loopWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(loopStopTimeout):
		log.Printf("share: timed out after %s waiting for follow/sweeper goroutines to stop; a straggler may still touch the database", loopStopTimeout)
	}
}

// spawnLoop runs fn on a goroutine tracked by loopWG so Stop can wait for it.
// Every manager goroutine that touches the database goes through here: the
// follow receive loops (their clip inserts are exactly what must not land in a
// freshly restored database) and the ring sweeper (RingEvict deletes rows).
// The staging janitor is included for uniformity — it only touches files, but
// a stray temp-file delete after Stop costs nothing to prevent. Excluded, with
// reason: handlePublisherStream's drain goroutines and followerConn senders
// never write the DB (senders write the network; drain goroutines only mutate
// the fmu-guarded followers map) and both exit when the host closes their
// streams. handlePublisherStream's own body DOES read the DB, so it takes the
// same admission via admitLoop at its entry.
//
// Returns false, without running fn, once Stop has begun: an unguarded Add
// would race Stop's Wait (a from-zero Add concurrent with Wait is invalid),
// and a goroutine registered after the wait passed would escape the drain and
// touch whatever database exists by the time it runs. A follow whose loop is
// refused here simply has no loop this process lifetime; its row persists and
// the next startup's ResumeAll spawns it.
func (m *ShareManager) spawnLoop(fn func()) bool {
	if !m.admitLoop() {
		return false
	}
	go func() {
		defer m.loopWG.Done()
		fn()
	}()
	return true
}

// admitLoop registers one unit of DB-touching work with loopWG, refusing once
// Stop has begun. The caller owns the matching loopWG.Done. It exists
// separately from spawnLoop for work the manager does not spawn itself:
// libp2p invokes handlePublisherStream on its own goroutine, and its body
// reads the DB (ring retransmit, last_seq), so it must be inside the drain.
func (m *ShareManager) admitLoop() bool {
	m.loopMu.Lock()
	defer m.loopMu.Unlock()
	if m.stopping {
		return false
	}
	m.loopWG.Add(1)
	return true
}

// publication — publisher-side per-tag state.
//
// followers is keyed by the libp2p stream pointer (not peer.ID) so a single
// peer may hold up to MaxStreamsPerPeer concurrent streams against the same
// publication, which the cap check in Task 5.2 enforces by iteration. fmu
// guards the map; Task 5.2 takes it for atomic cap-check-then-insert.
// id, tagID, shareID and symkey are immutable after construction —
// registerPublication replaces the whole object rather than mutating one — so
// they are safe to read without a lock. status, removed and followers are all
// guarded by fmu, which is what makes a pause or a stop atomic against a
// handshake that has already found this publication.
type publication struct {
	id      int64
	tagID   int64
	shareID []byte // 16 bytes
	symkey  []byte // 32 bytes

	status string // "active" | "paused" | "invalid"; guarded by fmu
	// removed is set by StopShare once the publication has left
	// m.publications. The object can still be reachable by a handshake that
	// looked it up moments earlier, which must not register into a corpse.
	removed   bool
	followers map[network.Stream]*followerConn
	fmu       sync.Mutex
}

// currentStatus reads status under fmu. Callers that already hold fmu must
// read the field directly.
func (p *publication) currentStatus() string {
	p.fmu.Lock()
	defer p.fmu.Unlock()
	return p.status
}

func (p *publication) closeAllFollowers() {
	p.fmu.Lock()
	defer p.fmu.Unlock()
	p.closeAllFollowersLocked()
}

// closeAllFollowersLocked must be called with p.fmu held.
func (p *publication) closeAllFollowersLocked() {
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
	writer  io.Writer   // indirection so tests can substitute a writer
	queue   chan []byte // buffered channel; SendQueueEnvelopesCap slots
	pending int64       // bytes currently sitting in queue; guarded by mu
	onClose func()      // test hook; invoked under mu
	mu      sync.Mutex
	closed  bool
	// draining means "the queue is closed, no further envelopes". Set by
	// finishAfterDrain for a truncated catch-up batch, and by closeLocked so
	// that a late enqueue takes the early return instead of sending on a
	// closed channel. It is what excludes this connection from live fan-out.
	draining bool
}

// enqueue hands env to the sender goroutine. Non-blocking; if either cap is
// exceeded the connection is closed immediately.
func (fc *followerConn) enqueue(env []byte) {
	fc.mu.Lock()
	if fc.closed || fc.draining {
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

// finishAfterDrain marks the connection "batch complete, nothing more to
// send". The queue is closed so the sender goroutine writes everything
// already queued and then shuts the write side of the stream down cleanly
// (CloseWrite, not Reset, so the bytes still in the muxer are delivered
// rather than discarded).
//
// This is the truncated-catch-up path: the follower consumes the batch,
// advances its durable boundary to the batch's last clip_end, sees EOF, and
// reconnects for the next batch. runFollowLoop resets the backoff ladder to
// ReconnectFloor after any handshaked session, so that costs ~1s per batch
// instead of a growing backoff.
func (fc *followerConn) finishAfterDrain() {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if fc.closed || fc.draining {
		return
	}
	fc.draining = true
	close(fc.queue)
}

// isDraining reports whether the queue has been closed. liveFanOutLocked uses
// it to skip a follower that is finishing a truncated catch-up batch.
func (fc *followerConn) isDraining() bool {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	return fc.draining
}

// runSender drains the queue into the writer. A write error closes the
// connection; the sender exits when the queue is closed by closeLocked or by
// finishAfterDrain.
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
	// The queue is closed. If closeLocked did it, the stream is already
	// reset and closed is set — nothing to do. If finishAfterDrain did it,
	// the last envelope of a truncated catch-up batch has just been written,
	// so end the stream gracefully. Whichever of the two paths reaches the
	// lock first wins; the other sees closed and no-ops.
	fc.mu.Lock()
	if !fc.closed {
		fc.closed = true
		if fc.stream != nil {
			_ = fc.stream.CloseWrite()
		}
		if fc.onClose != nil {
			fc.onClose()
		}
	}
	fc.mu.Unlock()
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
	if !fc.draining {
		close(fc.queue)
	}
	// The queue is closed either way now, so keep draining set: it is the
	// flag enqueue checks before touching the channel.
	fc.draining = true
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
	// lastSeq is the durable resume point, owned exclusively by this
	// follow's runFollowLoop goroutine (→ followSession → consumeStream).
	// Nothing outside that chain may read or write it; anything that needs
	// the value from another goroutine reads follows.last_seq from the DB.
	lastSeq uint64
	// status is guarded by mu (see setFollowStatus).
	status string
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

// maxShareableClipBytes returns the effective clip-size ceiling for
// publication. Production uses MaxShareableClipBytes; tests lower it via the
// override so the refusal path runs without a 30 MiB allocation.
func (m *ShareManager) maxShareableClipBytes() int64 {
	if m.maxShareableClipOverride > 0 {
		return m.maxShareableClipOverride
	}
	return MaxShareableClipBytes
}

// setMaxStreamsPerPubForTest lowers the per-publication cap so cap-enforcement
// can be exercised in tests without opening 128 real streams.
func (m *ShareManager) setMaxStreamsPerPubForTest(n int) {
	m.maxStreamsPerPubOverride = n
}

// catchupCaps returns the effective per-batch catch-up budget.
func (m *ShareManager) catchupCaps() catchupCaps {
	if m.catchupCapsOverride != nil {
		return *m.catchupCapsOverride
	}
	return defaultCatchupCaps()
}

// setCatchupCapsForTest shrinks the catch-up budget. Call it before any
// follower can handshake — the field is read without a lock, exactly like
// maxStreamsPerPubOverride.
func (m *ShareManager) setCatchupCapsForTest(c catchupCaps) {
	m.catchupCapsOverride = &c
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

// planRetransmit decides what a freshly-handshaked follower must receive to
// resume from sinceSeq, given the metadata of the ring rows that survived
// TTL/cap eviction and the publication's current last_seq.
//
// It plans over RingRowMeta rather than whole rows: only Seq, Kind and the
// envelope's byte length take part in the decision, and loading the blobs of
// a full ring to answer it is what used to make catch-up unbounded in memory
// (see RingRetransmitMeta). The chosen batch's blobs are fetched afterwards.
//
// It returns the rows to send and, if the follower must skip forward over seqs
// that can never be replayed, the seq to advance it to (0 means "no gap").
//
// Two eviction shapes force a jump:
//
//   - Head evicted: the surviving rows start above sinceSeq+1. The follower
//     would try to decrypt the first row at sinceSeq+1, fail (seq is in the
//     AAD), and error out the session — forever, since it reconnects with the
//     same sinceSeq.
//   - Everything evicted: no rows survive but last_seq has moved on. Same
//     brick, one seq later, when the next live envelope arrives.
//
// Leading rows that are not clip_start are dropped: their clip_start was
// evicted, so they would feed an assembler that never saw a header. Skipping
// them means the gap covers their seqs too.
func planRetransmit(rows []RingRowMeta, sinceSeq, pubLastSeq uint64) (send []RingRowMeta, gapTarget uint64) {
	send = rows
	for len(send) > 0 && send[0].Kind != KindClipStart {
		send = send[1:]
	}
	switch {
	case len(send) > 0:
		if send[0].Seq > sinceSeq+1 {
			gapTarget = send[0].Seq - 1
		}
	case pubLastSeq > sinceSeq:
		gapTarget = pubLastSeq
	}
	return send, gapTarget
}

// catchupCaps bounds one handshake catch-up batch. Two tiers:
//
//   - soft — the normal per-batch target. Half of each send-queue cap, so the
//     other half stays free for the live fan-out that resumes the moment the
//     handshake releases pub.fmu.
//   - hard — what a batch may occupy when it is the only thing that will ever
//     be on this connection. A truncated batch closes as soon as it drains and
//     is skipped by live fan-out, so it owns the whole queue. This is what
//     lets a clip larger than the soft budget still be delivered (alone)
//     rather than skipped.
//
// A clip over the hard tier cannot be enqueued at all — enqueue would shed the
// follower before the batch finished — so catch-up skips it.
type catchupCaps struct {
	softBytes int64
	softSlots int
	hardBytes int64
	hardSlots int
}

func defaultCatchupCaps() catchupCaps {
	return catchupCaps{
		softBytes: CatchupSoftBytesCap,
		softSlots: CatchupSoftEnvelopesCap,
		hardBytes: CatchupHardBytesCap,
		hardSlots: CatchupHardEnvelopesCap,
	}
}

// seqRange is an inclusive span of ring seqs.
type seqRange struct{ Start, End uint64 }

// catchupPlan is what one handshake should put on the wire.
//
// Truncated means the connection must send Send and then close once it
// drains, so the follower advances its durable boundary and reconnects for
// whatever comes next. It fires for two reasons: surviving rows were left
// behind, where leaving the connection registered for live fan-out would
// splice live envelopes onto a stream with a seq hole in it that the follower
// cannot decrypt past; or the batch carries a clip over the soft caps, which
// owns the whole connection because it plus live fan-out cannot both fit the
// follower's send queue. So Truncated does not imply rows remain.
//
// Skipped names clips that no batch can ever carry (their envelopes exceed
// the hard cap). GapTarget already covers them; the caller logs them.
//
// Rewind marks the one case where GapTarget moves the follower BACKWARDS —
// see the publisher-regression branch in planCatchupBatch. It carries no
// protocol weight (the gap envelope is identical either way); the caller uses
// it to log the event, which is worth a diagnostic trail.
type catchupPlan struct {
	Send      []RingRowMeta
	GapTarget uint64 // 0 == no gap needed
	Truncated bool
	Rewind    bool
	Skipped   []seqRange
}

// groupRingRowsByClip splits a retransmit run into per-clip groups: a
// clip_start through the clip_end that terminates it. rows must already start
// at a clip_start (planRetransmit guarantees that). A trailing run with no
// clip_end forms its own group so it is still taken or left whole.
//
// Grouping is what keeps a batch from being cut mid-clip. A follower that
// received a clip_start but no clip_end never advances its boundary past
// either, so the split half would be re-sent from scratch on the next
// connection — and a batch ending on a chunk would leave the follower's
// assembler holding a partial clip it must throw away.
func groupRingRowsByClip(rows []RingRowMeta) [][]RingRowMeta {
	var groups [][]RingRowMeta
	start := 0
	for i, r := range rows {
		if r.Kind == KindClipEnd {
			groups = append(groups, rows[start:i+1])
			start = i + 1
		}
	}
	if start < len(rows) {
		groups = append(groups, rows[start:])
	}
	return groups
}

// groupCost returns what a clip group would occupy in a follower's send queue.
func groupCost(g []RingRowMeta) (bytes int64, slots int) {
	for _, r := range g {
		bytes += r.ByteLen
	}
	return bytes, len(g)
}

// planCatchupBatch turns planRetransmit's answer into a batch that actually
// fits a follower's send queue.
//
// The ring holds up to RingBytesCapPerPub (256 MiB) per publication while the
// queue holds SendQueueBytesCap (32 MiB) and sheds the follower on overflow,
// so "enqueue everything that survived" could never work for a real backlog:
// a large one killed the connection instantly, and a single clip bigger than
// the queue was undeliverable forever — the follower looped until the ring
// evicted the clip and the gap then skipped it silently.
//
// Batching converges instead. Each connection carries a whole number of
// clips; when rows are left over the caller closes the stream after the batch
// drains and the follower comes straight back for the next one.
func planCatchupBatch(rows []RingRowMeta, sinceSeq, pubLastSeq uint64, caps catchupCaps, metaTruncated bool) catchupPlan {
	// Publisher sequence regression: this follower is ahead of the history
	// the publisher still has. The only way for last_seq to move backwards is
	// a restore from an older backup under identity policy "takeover", which
	// adopts the backup's identity and keeps the restored shares rows active —
	// so the follower's symkey and share_id still verify, but every seq it has
	// already consumed is about to be handed out again to a different clip.
	//
	// Left alone this is a permanent brick: no surviving row has seq >
	// sinceSeq, pubLastSeq < sinceSeq means the forward-only gap below stays
	// 0, and the follower idles connected until the next clip arrives sealed
	// at pubLastSeq+1 — far below the seq it will try — whereupon the session
	// dies, reconnects with the identical since_seq, and repeats forever.
	//
	// Rewinding the follower to pubLastSeq costs duplicates: clips the
	// publisher re-emits over the reused seq range arrive as clips the
	// follower has already stored, and it inserts them again. They carry
	// content_hash, so the dedup feature can find them. Recoverable-with-
	// duplicates beats permanently bricked.
	//
	// rows is ignored deliberately. Every ring row is written in the same
	// transaction that raises last_seq past it, and a restore replaces shares
	// and share_ring together, so rows.seq <= pubLastSeq < sinceSeq holds and
	// RingRetransmitMeta (seq > sinceSeq) can only have returned nothing. If a
	// corrupted database breaks that, sending those rows behind a gap that
	// lands below them would just desync the follower again; dropping them
	// leaves the next handshake — which comes in at the rewound boundary — to
	// replay them through the ordinary path.
	if pubLastSeq < sinceSeq {
		return catchupPlan{GapTarget: pubLastSeq, Rewind: true}
	}

	send, gapTarget := planRetransmit(rows, sinceSeq, pubLastSeq)
	plan := catchupPlan{GapTarget: gapTarget}

	groups := groupRingRowsByClip(send)
	// The taken groups are always a contiguous run: batchStart only moves for
	// skips, which can only happen while nothing has been taken yet.
	batchStart, batchEnd := 0, 0
	var usedBytes int64
	var usedSlots int

	for i, g := range groups {
		gBytes, gSlots := groupCost(g)
		if usedSlots+gSlots <= caps.softSlots && usedBytes+gBytes <= caps.softBytes {
			usedBytes += gBytes
			usedSlots += gSlots
			batchEnd = i + 1
			continue
		}
		if batchEnd > batchStart {
			// Whole clips are already batched. Stop on this boundary and let
			// the follower reconnect for the rest.
			plan.Truncated = true
			break
		}
		// Nothing batched yet, so this group is the head and it is over the
		// soft budget on its own.
		if gSlots <= caps.hardSlots && gBytes <= caps.hardBytes {
			// Deliverable, but only with the queue to itself: a hard-tier
			// clip plus live fan-out cannot both fit SendQueueBytesCap, and
			// an overflow resets the connection mid-clip. So a batch holding
			// an over-soft clip always owns the whole connection and is
			// always drain-closed — even when it is the last group and
			// nothing is left behind. The follower reconnects (~1s) and the
			// next handshake finds either nothing or whatever arrived
			// meanwhile; that costs one round trip, whereas staying
			// registered risks losing the batch that is still draining.
			batchEnd = i + 1
			plan.Truncated = true
			break
		}
		// Too big for any batch. Skipping it is data loss, but it is the only
		// alternative to reconnecting forever; the caller logs a warning. The
		// gap moves past the clip's last seq so the follower lands exactly on
		// the next clip's clip_start.
		plan.Skipped = append(plan.Skipped, seqRange{Start: g[0].Seq, End: g[len(g)-1].Seq})
		plan.GapTarget = g[len(g)-1].Seq
		batchStart, batchEnd = i+1, i+1
	}

	for _, g := range groups[batchStart:batchEnd] {
		plan.Send = append(plan.Send, g...)
	}
	// The metadata window was LIMIT-truncated: survivors exist beyond
	// rows[len(rows)-1], so this connection must page (drain-close and let
	// the follower reconnect for the next window), and no gap may jump past
	// the window's end — planRetransmit's all-orphan fallback targets
	// pubLastSeq, which would silently skip every unseen row. Clamping to the
	// window's last seq keeps the follower walking the backlog instead.
	if metaTruncated && len(rows) > 0 {
		if lastWindow := rows[len(rows)-1].Seq; plan.GapTarget > lastWindow {
			plan.GapTarget = lastWindow
		}
		plan.Truncated = true
	}
	// Nothing survived that we can send and nothing is being held back: the
	// follower still has to be carried up to the publication's head, exactly
	// as planRetransmit's empty-ring case does.
	if len(plan.Send) == 0 && !plan.Truncated && pubLastSeq > sinceSeq && pubLastSeq > plan.GapTarget {
		plan.GapTarget = pubLastSeq
	}
	return plan
}

// fetchPlannedEnvelopes loads the envelope blobs for a planned catch-up batch.
//
// plan.Send is always a contiguous run of surviving ring rows: planCatchupBatch
// takes whole clip groups in seq order and batchStart only advances for skips,
// which can only happen while nothing has been taken yet. So one range query
// bounded by the batch's first and last seq covers it exactly, and what this
// loads is capped by the catch-up budget (CatchupHardBytesCap, ~32 MiB) rather
// than by the ring (RingBytesCapPerPub, 256 MiB).
//
// nowUnix must be the value passed to RingRetransmitMeta, so both halves of a
// catch-up share one TTL cutoff and no row can age out between them.
//
// A disagreement between what was planned and what comes back means eviction
// raced the two queries, which is reachable rather than theoretical: RingEvict
// deletes globally (the age sweep spans every publication, and the per-
// publication cap trim can target this one) and runs both from the 15-minute
// ring sweeper, which holds no fmu at all, and from emitClipForPublication,
// which holds only the emitting publication's fmu. Neither is excluded by the
// fmu this handshake holds. Enqueueing a batch with a hole in it would hand the
// follower envelopes it cannot decrypt across, so the caller fails the
// handshake and the follower reconnects into a freshly planned batch.
func fetchPlannedEnvelopes(db *sql.DB, publicationID int64, send []RingRowMeta, nowUnix int64) ([][]byte, error) {
	if len(send) == 0 {
		return nil, nil
	}
	first, last := send[0].Seq, send[len(send)-1].Seq
	rows, err := RingFetchRange(db, publicationID, first, last, nowUnix)
	if err != nil {
		return nil, err
	}
	if len(rows) != len(send) {
		return nil, fmt.Errorf(
			"ring batch %d..%d: fetched %d rows, planned %d — evicted mid-handshake",
			first, last, len(rows), len(send))
	}
	out := make([][]byte, len(rows))
	for i, r := range rows {
		want := send[i]
		if r.Seq != want.Seq || r.Kind != want.Kind || int64(len(r.EnvelopeBytes)) != want.ByteLen {
			return nil, fmt.Errorf(
				"ring batch %d..%d: row %d is seq %d kind %q len %d, planned seq %d kind %q len %d — evicted mid-handshake",
				first, last, i, r.Seq, r.Kind, len(r.EnvelopeBytes), want.Seq, want.Kind, want.ByteLen)
		}
		out[i] = r.EnvelopeBytes
	}
	return out, nil
}

// encodeGapEnvelope seals a gap payload instructing a follower to advance its
// lastSeq to target. aadSeq must be the seq that follower will try to decrypt
// next (its since_seq+1) — the gap is per-follower and never hits the ring.
func encodeGapEnvelope(symkey, shareID []byte, aadSeq, target uint64) ([]byte, error) {
	b, err := MarshalPayload(GapPayload{Seq: target, Kind: KindGap})
	if err != nil {
		return nil, err
	}
	return EncryptEnvelope(symkey, shareID, aadSeq, b)
}

// handlePublisherStream implements:
//  1. read handshake (72 bytes)
//  2. lookup share_id; if unknown → Reset
//  3. verify HMAC; if bad → Reset
//  4. if the publication was stopped or is not active → Reset (re-checked
//     under pub.fmu, since both flags move under that lock)
//  5. enforce per-publication + per-peer stream caps
//  6. plan a catch-up batch from share_ring metadata with seq > since_seq
//     (TTL enforced in SQL), fetch only that batch's envelopes, prefixed
//     by a synthesized gap envelope when eviction left a hole (planRetransmit)
//  7. register the followerConn so future live envelopes flow here
func (m *ShareManager) handlePublisherStream(s network.Stream) {
	// libp2p invokes this on its own goroutine, and the body reads the DB
	// (ring retransmit, last_seq), so it joins the same drain Stop waits on:
	// a handshake in flight when a restore stops the manager finishes against
	// the pre-restore database or never starts. The admission covers only the
	// handler body — the drain goroutine spawned at the end never touches the
	// DB and is deliberately outside it.
	if !m.admitLoop() {
		_ = s.Reset()
		return
	}
	defer m.loopWG.Done()

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
	if pub == nil {
		_ = s.Reset()
		return
	}
	if err := VerifyHandshake(pub.symkey, hs); err != nil {
		_ = s.Reset()
		return
	}
	if m.testHookBeforeRegister != nil {
		m.testHookBeforeRegister(pub)
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
	// Status and removal are re-checked here rather than before the lock.
	// PauseShare and StopShare mutate them under fmu, so a check taken
	// outside it can be overtaken between the lookup above and the
	// registration below — leaving a follower attached to a share that was
	// just paused, or to one that no longer exists at all.
	if pub.removed || pub.status != "active" {
		pub.fmu.Unlock()
		_ = s.Reset()
		return
	}
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

	// Catch-up retransmit: the METADATA of ring rows with seq > since_seq that
	// are still within TTL — seq, kind and envelope length, no blobs. The
	// backlog can be the whole ring (256 MiB) while a batch is at most ~32 MiB,
	// so the blobs are fetched after the batch is chosen, not before.
	// Because we hold pub.fmu, no emission can commit a new ring row during
	// this query — every row here has seq ≤ shares.last_seq at the moment we
	// acquired the lock, and every row with a larger seq will arrive via the
	// blocked emission's liveFanOutLocked once we release.
	//
	// One nowUnix serves both queries so they share a TTL cutoff.
	nowUnix := time.Now().Unix()
	rows, metaTruncated, err := RingRetransmitMeta(m.db, pub.id, hs.SinceSeq, nowUnix, CatchupMetaRowCap)
	if err != nil {
		log.Printf("share: retransmit query: %v", err)
		delete(pub.followers, s)
		pub.fmu.Unlock()
		// fc.close() rather than a bare s.Reset(): the sender goroutine is
		// already running and would otherwise block on its queue forever.
		// closeLocked resets the stream on the way out, so this covers both.
		fc.close()
		return
	}
	// last_seq is read under pub.fmu, so it cannot move while we decide
	// whether the follower needs to skip forward: emitClipForPublication
	// holds the same lock for the whole of an emission.
	var lastSeqI int64
	if err := m.db.QueryRow(`SELECT last_seq FROM shares WHERE id = ?`, pub.id).Scan(&lastSeqI); err != nil {
		log.Printf("share: read last_seq for retransmit: %v", err)
		delete(pub.followers, s)
		pub.fmu.Unlock()
		fc.close()
		return
	}
	// The backlog can be far larger than one follower's send queue, so it is
	// cut into whole-clip batches; anything left over is fetched by the next
	// connection.
	plan := planCatchupBatch(rows, hs.SinceSeq, uint64(lastSeqI), m.catchupCaps(), metaTruncated)
	// Only now, with the batch fixed, are the envelopes themselves loaded.
	// Fetch and validate before anything is enqueued: a mismatch here must
	// abort a handshake that has not started, not truncate one already
	// half-written.
	envelopes, err := fetchPlannedEnvelopes(m.db, pub.id, plan.Send, nowUnix)
	if err != nil {
		log.Printf("share: catch-up fetch: %v", err)
		delete(pub.followers, s)
		pub.fmu.Unlock()
		fc.close()
		return
	}
	for _, sr := range plan.Skipped {
		m.logs.append(ShareLogEntry{
			Level: "warn", Scope: "share", PublicationID: pub.id,
			Message: fmt.Sprintf(
				"clip at seqs %d..%d is too large to replay (over the %d-byte follower send queue) — skipping it for %s, which will never receive this clip",
				sr.Start, sr.End, CatchupHardBytesCap, peerID.String()),
		})
	}
	if plan.Rewind {
		m.logs.append(ShareLogEntry{
			Level: "warn", Scope: "share", PublicationID: pub.id,
			Message: fmt.Sprintf(
				"%s is at seq %d but this share's history only reaches %d — rewinding it, most likely after a restore from an older backup. Clips republished over the reused seqs will reach that follower twice",
				peerID.String(), hs.SinceSeq, plan.GapTarget),
		})
	}
	// GapTarget == 0 normally means "no gap needed", but a rewind to 0 is a
	// real instruction: the publisher is back at the start of its sequence and
	// the follower must go with it, or the next clip arrives at seq 1 and the
	// follower is still trying to decrypt above it.
	if plan.GapTarget > 0 || plan.Rewind {
		// Synthesized per handshake, never stored in the ring: the AAD seq
		// is the exact seq THIS follower will try to decrypt first.
		gapEnv, err := encodeGapEnvelope(pub.symkey, pub.shareID, hs.SinceSeq+1, plan.GapTarget)
		if err != nil {
			// Sending the rows without the gap would leave the follower
			// unable to decrypt any of them — reset and let it retry.
			log.Printf("share: gap envelope: %v", err)
			delete(pub.followers, s)
			pub.fmu.Unlock()
			fc.close()
			return
		}
		fc.enqueue(gapEnv)
	}
	for _, env := range envelopes {
		fc.enqueue(env)
	}
	if plan.Truncated {
		// Either backlog is left over — registering this follower for live
		// fan-out would splice live envelopes onto a stream that stops short
		// of them, and it cannot decrypt across the hole — or the batch holds
		// a clip over the soft caps, which needs the send queue to itself.
		// Send the batch and close instead: the follower stores the clips,
		// advances its durable boundary to the batch's last clip_end, and
		// reconnects (~1s, the backoff floor after a handshaked session).
		//
		// finishAfterDrain must happen before pub.fmu is released — that is
		// what guarantees no emission slips a live envelope in behind us.
		fc.finishAfterDrain()
		m.logs.append(ShareLogEntry{
			Level: "info", Scope: "share", PublicationID: pub.id,
			Message: fmt.Sprintf(
				"sent %d catch-up envelopes to %s and closing after drain; it reconnects for any backlog left over or published meanwhile",
				len(plan.Send), peerID.String()),
		})
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
		// removed and the follower teardown land in one critical section so
		// an in-flight handshake either registers before the stop (and is
		// closed by closeAllFollowersLocked) or sees removed and resets.
		pub.fmu.Lock()
		pub.removed = true
		pub.closeAllFollowersLocked()
		pub.fmu.Unlock()
		if m.testHookAfterStopMark != nil {
			m.testHookAfterStopMark()
		}
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
	// The whole read-modify-write runs under fmu: handlePublisherStream
	// reads status under the same lock, and emitClipForPublication holds it
	// for a full emission, so a pause can never land halfway through a clip
	// or race a handshake into an accepted-then-paused state.
	pub.fmu.Lock()
	switch pub.status {
	case "paused":
		pub.fmu.Unlock()
		return nil
	case "invalid":
		pub.fmu.Unlock()
		return fmt.Errorf("cannot pause an invalid share — re-create it instead")
	}
	if _, err := m.db.Exec(`UPDATE shares SET status = 'paused' WHERE tag_id = ?`, tagID); err != nil {
		pub.fmu.Unlock()
		return fmt.Errorf("persist paused: %w", err)
	}
	pub.status = "paused"
	pub.closeAllFollowersLocked()
	pub.fmu.Unlock()

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
	pub.fmu.Lock()
	switch pub.status {
	case "active":
		pub.fmu.Unlock()
		return nil
	case "invalid":
		pub.fmu.Unlock()
		return fmt.Errorf("cannot resume an invalid share — re-create it instead")
	}
	if _, err := m.db.Exec(`UPDATE shares SET status = 'active' WHERE tag_id = ?`, tagID); err != nil {
		pub.fmu.Unlock()
		return fmt.Errorf("persist active: %w", err)
	}
	pub.status = "active"
	pub.fmu.Unlock()

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
	status := pub.status
	pub.fmu.Unlock()
	pubKeyBytes, _ := PublicKeyBytes(m.host.Peerstore().PrivKey(m.host.ID()))
	shareStr, _ := EncodeShareString(pubKeyBytes, pub.symkey)
	m.emitEvent("share:publication-updated", ShareInfo{
		ID: pub.id, TagID: tagID, TagName: tagName,
		ShareString: shareStr, Status: status,
		Followers: fCount, ClipsPushed: clipsSent,
		LastSeq:   lastSeqDB,
		CreatedAt: createdAt,
	})
}

// emitEvent forwards a frontend event if an emitter is installed.
func (m *ShareManager) emitEvent(name string, data any) {
	m.evMu.RLock()
	fn := m.eventFn
	m.evMu.RUnlock()
	if fn != nil {
		fn(name, data)
	}
}

// OnClipCreated is the hook called by App after every clip insert. It finds
// every publication whose tag is on the clip and emits a chunked envelope
// burst into share_ring (and to any connected followers).
func (m *ShareManager) OnClipCreated(clipID int64, tagIDs []int64) error {
	m.mu.RLock()
	var matches []*publication
	for _, p := range m.publications {
		for _, tid := range tagIDs {
			if tid == p.tagID {
				matches = append(matches, p)
				break
			}
		}
	}
	m.mu.RUnlock()
	// Status filtering happens after m.mu is released so a long in-flight
	// emission (which holds fmu) never stalls the manager lock. This is only
	// a fast path — emitClipForPublication re-checks under the fmu it holds
	// for the whole emission, which is the check that is actually atomic
	// against PauseShare and StopShare.
	active := matches[:0]
	for _, p := range matches {
		if p.currentStatus() == "active" {
			active = append(active, p)
		}
	}
	matches = active
	if len(matches) == 0 {
		return nil
	}

	// Two-step read inside one snapshot: byte lengths first, values only
	// after they pass the ceilings. Scanning the columns wholesale would
	// itself allocate an arbitrarily large filename or metadata blob before
	// any Go-side check could refuse it — the ceilings must run on numbers,
	// not on the strings they bound.
	tx, err := m.db.Begin()
	if err != nil {
		return fmt.Errorf("read clip %d: %w", clipID, err)
	}
	defer tx.Rollback() // read-only; always rolled back

	// octet_length, not LENGTH(CAST(... AS BLOB)): the CAST would make SQLite
	// materialize the whole value just to measure it — the allocation this
	// preflight exists to prevent — while octet_length on a bare column reads
	// the byte count from the record header without loading the content.
	// (Available since SQLite 3.43; the bundled modernc build is newer.)
	// The COALESCE fallbacks mirror the value read below: a NULL filename
	// reads as '' (0 bytes) and NULL metadata as '{}' (2 bytes).
	var totalSize, headerBytes int64
	if err := tx.QueryRow(
		`SELECT LENGTH(data),
		        COALESCE(octet_length(filename), 0) +
		        octet_length(content_type) +
		        COALESCE(octet_length(metadata), 2)
		   FROM clips WHERE id = ?`,
		clipID,
	).Scan(&totalSize, &headerBytes); err != nil {
		return fmt.Errorf("read clip %d: %w", clipID, err)
	}

	// Refuse clips whose envelope burst could never fit a follower's send
	// queue BEFORE any ring write or buffering. Emitting one anyway would
	// hold the entire encrypted burst in memory until commit (an unbounded
	// heap spike — a multi-GB clip could OOM the app) and fill the ring with
	// rows every catch-up batch skips. Refusing up front consumes no seqs,
	// so followers need no gap; they simply never hear about the clip, which
	// the warn log makes diagnosable.
	if totalSize > m.maxShareableClipBytes() {
		m.logs.append(ShareLogEntry{
			Level: "warn", Scope: "share",
			Message: fmt.Sprintf(
				"clip %d is %d bytes — over the %d-byte share ceiling (a follower's send queue could never hold it); not publishing it",
				clipID, totalSize, m.maxShareableClipBytes()),
		})
		return nil
	}
	// Same refusal for the clip_start header fields, decided on the SQL byte
	// lengths BEFORE the values are ever scanned into Go: a rename or
	// metadata write can make these arbitrarily large, and either the Scan or
	// the payload build would allocate them wholesale just for the envelope
	// cap to reject the frame after the fact. Refusing here consumes no
	// seqs, exactly like the size refusal.
	if headerBytes > MaxClipStartFieldBytes {
		m.logs.append(ShareLogEntry{
			Level: "warn", Scope: "share",
			Message: fmt.Sprintf(
				"clip %d has %d bytes of filename/content-type/metadata — over the %d-byte header ceiling; not publishing it",
				clipID, headerBytes, MaxClipStartFieldBytes),
		})
		return nil
	}

	// Both ceilings passed — the values are known-small, safe to scan. Same
	// snapshot as the length read, so a concurrent rename cannot swap a huge
	// value in between.
	var contentType, filename, metaJSON string
	if err := tx.QueryRow(
		`SELECT content_type, COALESCE(filename,''), COALESCE(metadata,'{}') FROM clips WHERE id = ?`,
		clipID,
	).Scan(&contentType, &filename, &metaJSON); err != nil {
		return fmt.Errorf("read clip %d fields: %w", clipID, err)
	}

	metadata := map[string]string{}
	_ = json.Unmarshal([]byte(metaJSON), &metadata)

	chunkCount := uint32((totalSize + int64(ChunkSize) - 1) / int64(ChunkSize))
	if chunkCount == 0 {
		chunkCount = 1 // empty clip still emits one zero-length chunk for symmetry
	}

	// Close the read snapshot BEFORE the emissions: emitClipForPublication
	// opens its own transaction, and holding this one across that call
	// deadlocks a single-connection pool (the deferred Rollback above then
	// becomes a harmless no-op).
	_ = tx.Rollback()

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

	// Authoritative pause/stop gate: both flags move under this same lock,
	// so a publication that flipped after OnClipCreated's fast-path filter
	// stops here rather than emitting into a paused or deleted share.
	if p.removed || p.status != "active" {
		return nil
	}

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

	// Envelopes are buffered here and fanned out only once the transaction
	// commits. Fanning out per envelope inside the tx was a durability
	// inversion: a failed UPDATE or Commit rolled the ring rows and last_seq
	// back, but followers had already stored the clip and moved their durable
	// boundary past it — so the next emission reused the same seqs, every
	// envelope arrived sealed under an AAD seq below what the follower
	// expected, and the follow desynced with a phantom clip on disk.
	//
	// The cost is one clip's envelopes in RAM for the length of the emission,
	// which is the same order as the whole-clip buffering an upload already
	// does. The chunk reads below stay SUBSTR-based, so the clip blob itself
	// is still never materialized twice.
	pending := make([][]byte, 0, chunkCount+2)

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
	pending = append(pending, startEnv)
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
		pending = append(pending, cpEnv)
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
	pending = append(pending, endEnv)

	// Bump last_seq AND clips_sent (one clip_end = one published clip). They
	// stay in sync by construction.
	if _, err := tx.Exec(`UPDATE shares SET last_seq = ?, clips_sent = clips_sent + 1 WHERE id = ?`, int64(nextSeq), p.id); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	// Durable now — and still under p.fmu, so ordering against a concurrent
	// handshake is exactly what it was before: a new follower gets this clip
	// wholly from the ring catch-up or wholly from here, never a mix.
	for _, env := range pending {
		m.liveFanOutLocked(p, env)
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
		// A follower finishing a truncated catch-up batch is deliberately
		// behind: it closes as soon as the batch drains and reconnects for
		// the rest, so a live envelope here would sit on the far side of a
		// seq hole. (enqueue enforces this too, since the queue is already
		// closed — this is the readable half of the same rule.)
		if fc.isDraining() {
			continue
		}
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

	if !m.spawnLoop(func() { m.runFollowLoop(f) }) {
		log.Printf("share: manager stopping; follow %d gets no loop this run (resumed on next startup)", f.id)
	}

	// Report the status the follow actually holds. Nothing has handshaked yet
	// — runFollowLoop has only just been started, and FollowWithoutDial commits
	// follows whose peer is known to be unreachable — so claiming "connected"
	// here put a green pill on a card that had never talked to anyone.
	info = FollowInfo{
		ID: id, RemotePeerID: pid.String(),
		LocalTagID: localTagID, LocalTagName: localTagName,
		Status: "connecting", ClipsReceived: 0, LastSeq: 0,
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
//     (ReconnectFloor → ReconnectCap, doubling each *consecutive failed*
//     retry; see nextFollowBackoff).
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

		handshaked, err := m.followSession(f)
		if err != nil {
			m.logs.append(ShareLogEntry{
				Level: "warn", Scope: "follow", FollowID: f.id,
				Message: "session ended: " + err.Error(),
			})
		}
		if handshaked {
			// The session that just ended proved the peer reachable, so the
			// ladder starts over — including the wait we are about to take.
			// Without this, a follow that flapped in its first minute stayed
			// pinned at the 30s cap for the rest of the app's life, no matter
			// how many hours of clean streaming came after.
			backoff = ReconnectFloor
		}
		select {
		case <-f.ctx.Done():
			return
		case <-f.reconnectSignal:
			backoff = ReconnectFloor
		case <-time.After(backoff):
			backoff = nextFollowBackoff(backoff, handshaked)
		}
	}
}

// nextFollowBackoff returns the reconnect wait for a follow whose session just
// ended. handshaked reports whether that session got as far as a completed
// handshake: a session that actually worked resets the ladder to the floor, so
// the backoff measures *consecutive failed dials* rather than the count of
// sessions since the app started. Only genuine repeat failures grow, capped at
// ReconnectCap.
func nextFollowBackoff(current time.Duration, handshaked bool) time.Duration {
	if handshaked {
		return ReconnectFloor
	}
	next := current * 2
	if next > ReconnectCap {
		return ReconnectCap
	}
	return next
}

// followSession dials, handshakes, and streams until either the stream
// closes on its own or the per-session context is canceled (e.g. by
// DisconnectFollowForTest). Returns when the session ends for any reason;
// the caller (runFollowLoop) then decides whether to reconnect. The bool
// reports whether the handshake was written — the signal runFollowLoop uses
// to tell "the peer is fine, the stream just ended" from "the dial failed",
// which are the two cases the backoff ladder must not conflate.
//
// Status discipline: status flips to "connected" exactly once the
// handshake is written; the deferred cleanup flips it back to "offline"
// on every exit path (dial error, handshake error, mid-stream cancel,
// EOF, consumeStream error). This guarantees the UI card reflects real
// transport state regardless of which branch returns.
func (m *ShareManager) followSession(f *follow) (handshaked bool, err error) {
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
		return false, err
	}
	s, err := m.host.NewStream(sessCtx, f.remotePeerID, ShareProtocolID)
	if err != nil {
		return false, err
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
		return false, err
	}
	m.setFollowStatus(f, connStatusLabel(s.Conn()))
	m.logs.append(ShareLogEntry{
		Level: "info", Scope: "follow", FollowID: f.id,
		Message: fmt.Sprintf("handshake complete with %s", f.remotePeerID.String()),
	})

	return true, m.consumeStream(sessCtx, f, s)
}

// connStatusLabel names the transport a live session is riding. go-libp2p
// marks circuit-v2 relay connections as Limited (they are byte- and
// time-budgeted), which is exactly the "connected, but through a relay"
// state the UI distinguishes with its own pill. Takes the narrow Stat()
// interface rather than network.Conn so it is testable without a relay.
func connStatusLabel(c interface{ Stat() network.ConnStats }) string {
	if c == nil {
		return "connected"
	}
	if c.Stat().Limited {
		return "connected_relayed"
	}
	return "connected"
}

// consumeStream reads frames until the session context is canceled or the
// stream closes. Takes the session ctx (not the follow-lifetime ctx) so
// DisconnectFollowForTest ends reads promptly without killing the follow.
//
// Two cursors, deliberately separate:
//
//   - wireSeq is session-local and advances on EVERY frame. It is only the
//     AAD seq the next envelope is sealed under.
//   - The durable resume point (f.lastSeq, mirrored in follows.last_seq) moves
//     only at clip boundaries: a fully processed clip_end, or a gap. It is what
//     the next handshake carries as since_seq.
//
// Persisting mid-clip frames is what used to lose data: a session that died
// between clip_start and clip_end resumed after the clip_start, so the next
// session's fresh assembler only ever saw orphan chunks and the interrupted
// clip was never delivered. Resuming from the last boundary instead makes the
// publisher replay the whole clip from its clip_start.
func (m *ShareManager) consumeStream(sessCtx context.Context, f *follow, r io.Reader) error {
	asm := newClipAssembler(filepath.Join(m.dataDir, ShareStagingDirName), f.id)
	// Sessions ending mid-clip are now a designed-for path, not an anomaly:
	// drop the staging file on the way out instead of holding the fd open
	// until some later session's onStart clears it.
	defer asm.cleanup()
	shareID := DeriveShareID(f.symkey)
	wireSeq := f.lastSeq
	// The publisher's handshake answer, if it sends one at all, is always the
	// first frame of the session. That is the only position where a gap is
	// allowed to move the cursor backwards — see the KindGap case.
	firstFrame := true
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
		// Seq is part of the AAD, so the stream is strictly ordered: the next
		// envelope is always wireSeq+1. Eviction holes are covered by the
		// publisher's synthesized gap envelope, which is itself sealed at
		// since_seq+1 and carries the seq to jump to. A decrypt failure here
		// therefore means real desync or tampering — end the session and let
		// the reconnect re-handshake with the persisted boundary, which the
		// publisher will answer with a fresh gap.
		wireSeq++
		pt, err := DecryptEnvelope(f.symkey, shareID, wireSeq, frame)
		if err != nil {
			return fmt.Errorf("decrypt failed at seq %d: %w", wireSeq, err)
		}
		kind, raw, err := PeekPayloadKind(pt)
		if err != nil {
			return err
		}
		// The AAD seq is authoritative for advancing — it is what the sender
		// actually sealed under. The plaintext Seq field is informational
		// except on a gap, where it is the advance target.
		switch kind {
		case KindClipStart:
			var p ClipStartPayload
			if err := UnmarshalPayload(raw, &p); err != nil {
				return err
			}
			asm.onStart(p)
		case KindClipChunk:
			var p ClipChunkPayload
			if err := UnmarshalPayload(raw, &p); err != nil {
				return err
			}
			asm.onChunk(p)
		case KindClipEnd:
			var p ClipEndPayload
			if err := UnmarshalPayload(raw, &p); err != nil {
				return err
			}
			// UpdateFollowTag can swap the destination tag mid-session, so
			// take one snapshot under f.mu and use it for both the insert
			// and the event rather than reading the field twice unlocked.
			f.mu.Lock()
			localTagID := f.localTagID
			f.mu.Unlock()
			// The clip insert and the boundary advance share one transaction,
			// so a crash can never leave "clip stored" without "resume point
			// past it" (which would redeliver the clip as a duplicate).
			newClipID, err := asm.onEnd(p, m.db, localTagID, &followCursor{followID: f.id, seq: wireSeq})
			switch {
			case err == nil:
				f.lastSeq = wireSeq
				m.emitEvent("share:clip-received", map[string]any{
					"clip_id":      newClipID,
					"local_tag_id": localTagID,
					"follow_id":    f.id,
				})
				m.logs.append(ShareLogEntry{
					Level: "info", Scope: "follow", FollowID: f.id,
					Message: fmt.Sprintf("received clip id=%d", newClipID),
				})
			case errors.Is(err, errClipPoisoned):
				// The bytes are terminally bad and would fail identically on
				// every replay, so step the boundary past the clip rather than
				// asking for it again on every reconnect, forever.
				log.Printf("share: follow %d: dropping clip at seq %d: %v", f.id, wireSeq, err)
				m.logs.append(ShareLogEntry{
					Level: "warn", Scope: "follow", FollowID: f.id,
					Message: fmt.Sprintf("dropped corrupt clip at seq %d", wireSeq),
				})
				m.advanceFollowBoundary(f, wireSeq)
			default:
				// Local I/O or database failure — the clip itself may be fine.
				// Leave the boundary where it is and end the session so the
				// reconnect replays this clip from its clip_start.
				return fmt.Errorf("assemble clip at seq %d: %w", wireSeq, err)
			}
		case KindGap:
			var p GapPayload
			if err := UnmarshalPayload(raw, &p); err != nil {
				return err
			}
			// Mid-session a gap is forward-only: a backwards target there
			// would rewind the cursor and replay seqs already consumed, which
			// is the shape a replay attack takes. A gap is a boundary by
			// definition, so it can only ever sit between clips either way.
			//
			// The first frame is the exception, and it has to be. A publisher
			// restored from an older backup has a last_seq BELOW what this
			// follower already consumed; nothing it publishes from then on can
			// ever be decrypted here, and every reconnect reproduces the same
			// state, so the follow is bricked with no user-visible cause. The
			// handshake answer is the publisher saying "my history rewound" —
			// the only frame that can carry that, since after it the stream is
			// live envelopes. Accepting it costs duplicates: clips the
			// publisher re-emits over the reused seqs get stored again as new
			// clips. They carry content_hash, so dedup can find them.
			if firstFrame || p.Seq > wireSeq {
				if p.Seq < wireSeq {
					m.logs.append(ShareLogEntry{
						Level: "warn", Scope: "follow", FollowID: f.id,
						Message: fmt.Sprintf(
							"publisher history rewound from %d to %d — some clips may be redelivered",
							f.lastSeq, p.Seq),
					})
				}
				wireSeq = p.Seq
			}
			// Moves the durable boundary DOWN on a rewind. advanceFollowBoundary
			// is an unconditional UPDATE, which is what makes that work.
			m.advanceFollowBoundary(f, wireSeq)
		default:
			// Forward compatibility: a newer publisher may add kinds. Step
			// over the envelope rather than killing a session that is
			// otherwise healthy. Wire-only: an unknown kind is not a known
			// boundary, and re-skipping it after a reconnect costs nothing.
			log.Printf("share: follow %d: unknown envelope kind %q at seq %d — skipping", f.id, kind, wireSeq)
		}
		firstFrame = false
		// Liveness, not a cursor: safe to touch per frame so the UI does not
		// look stale for the whole of a large clip.
		_, _ = m.db.Exec(`UPDATE follows SET last_seen_at = ? WHERE id = ?`, time.Now().Unix(), f.id)
	}
}

// advanceFollowBoundary moves the durable resume point to seq for boundaries
// that carry no clip insert to ride along with — a gap, or a clip dropped for
// failing its integrity check. The success path instead advances inside the
// assembler's transaction.
func (m *ShareManager) advanceFollowBoundary(f *follow, seq uint64) {
	f.lastSeq = seq
	_, _ = m.db.Exec(`UPDATE follows SET last_seq = ? WHERE id = ?`, int64(seq), f.id)
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
		if !m.spawnLoop(func() { m.runFollowLoop(f) }) {
			log.Printf("share: manager stopping; follow %d gets no loop this run (resumed on next startup)", f.id)
		}
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

// StartSweepers launches background timers for ring eviction and staging cleanup.
// Call once from App.startup after ResumeAll.
func (m *ShareManager) StartSweepers() {
	m.spawnLoop(m.runRingSweeper)
	m.spawnLoop(m.runStagingJanitor)
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
		status := p.status
		p.fmu.Unlock()

		// Reconstruct share string from stored key + our pubkey.
		pubKeyBytes, _ := PublicKeyBytes(m.host.Peerstore().PrivKey(m.host.ID()))
		shareStr, _ := EncodeShareString(pubKeyBytes, p.symkey)

		shares = append(shares, ShareInfo{
			ID: p.id, TagID: p.tagID, TagName: tagName,
			ShareString: shareStr, Status: status,
			Followers: fCount, ClipsPushed: clipsSent,
			LastSeq:   lastSeqDB,
			CreatedAt: createdAt,
		})
	}

	for _, f := range m.follows {
		f.mu.Lock()
		status := f.status
		paused := f.paused
		localTagID := f.localTagID
		f.mu.Unlock()

		var localTagName string
		var createdAt, lastSeqDB, clipsRecv int64
		var lastSeenSQL sql.NullInt64
		m.db.QueryRow(`SELECT name FROM tags WHERE id = ?`, localTagID).Scan(&localTagName)
		m.db.QueryRow(`SELECT created_at, last_seq, clips_received, last_seen_at FROM follows WHERE id = ?`, f.id).Scan(&createdAt, &lastSeqDB, &clipsRecv, &lastSeenSQL)
		var lastSeenPtr *int64
		if lastSeenSQL.Valid {
			v := lastSeenSQL.Int64
			lastSeenPtr = &v
		}
		follows = append(follows, FollowInfo{
			ID: f.id, RemotePeerID: f.remotePeerID.String(),
			LocalTagID: localTagID, LocalTagName: localTagName,
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
