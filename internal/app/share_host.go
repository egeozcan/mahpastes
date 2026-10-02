package app

import (
	"context"
	"math/rand/v2"
	"slices"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/host/autorelay"
	circuitproto "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/proto"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

// shareHostOptions is the libp2p option set every share host runs with.
// NewShareManager builds its host from exactly this list, and so does the
// relay-reservation test — which is the reason it lives in one place: a test
// that assembled its own options would pass against a configuration
// production never runs.
//
// relays feeds AutoRelay its candidates (see relayCandidateSource). arOpts are
// appended to AutoRelay's own options; production passes none and keeps
// go-libp2p's defaults (a 3-minute boot delay before settling for fewer than
// four candidates, 30s between candidate polls), and a test shortens them so a
// reservation lands in milliseconds.
func shareHostOptions(priv crypto.PrivKey, relays autorelay.PeerSource, arOpts ...autorelay.Option) []libp2p.Option {
	return []libp2p.Option{
		libp2p.Identity(priv),
		// Neutral AgentVersion to avoid install-specific fingerprinting (spec §5.4).
		libp2p.UserAgent("mahpastes"),
		libp2p.EnableRelay(),
		libp2p.EnableHolePunching(),
		libp2p.EnableAutoRelayWithPeerSource(relays, arOpts...),
		libp2p.ListenAddrStrings(
			"/ip4/0.0.0.0/tcp/0",
			"/ip4/0.0.0.0/udp/0/quic-v1",
		),
	}
}

// relayCandidateSource feeds AutoRelay the peers it may reserve a circuit-v2
// slot on. A host behind NAT is reachable from another network only through
// such a reservation: it is what puts a /p2p-circuit address in the host's
// peer record, and the relayed connection a follower then makes is what DCUtR
// coordinates its hole punch over. With no candidates there is no reservation,
// no relayed connection, and no hole punching either. (The empty static relay
// list this replaced was worse than it looked: go-libp2p's WithStaticRelays
// sizes AutoRelay's desired relay count to the list, so nil meant "want zero
// relays" and AutoRelay never even asked.)
//
// Candidates come from what the host already knows — its live connections and
// the DHT routing table, in production mostly public DHT servers, many of
// which run the hop service — so no relay is pinned in source. AutoRelay
// itself still dials each candidate, waits for identify, and drops any that
// does not speak /libp2p/circuit/relay/0.2.0/hop; filtering here only keeps
// the candidate budget for peers that can actually help.
//
// Only relays with a public address are offered, because AutoRelay builds the
// /p2p-circuit addresses it advertises from the relay's public addresses alone:
// a LAN-only relay — another mahpastes instance found over mDNS, say — would
// take one of AutoRelay's two reservation slots and give a follower outside
// the LAN nothing to dial.
//
// The source is wired before the host exists (it is a libp2p.New option) and
// AutoRelay may poll it before libp2p.New has returned, or before the DHT is
// built; until setHost it yields nothing, and AutoRelay polls again after its
// minimum interval.
type relayCandidateSource struct {
	mu           sync.RWMutex
	host         host.Host
	routingPeers func() []peer.ID
}

func newRelayCandidateSource() *relayCandidateSource { return &relayCandidateSource{} }

func (s *relayCandidateSource) setHost(h host.Host) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.host = h
}

// setRoutingPeers installs the DHT routing-table lister (nil removes it). The
// DHT is built after the host, so it arrives separately.
func (s *relayCandidateSource) setRoutingPeers(fn func() []peer.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routingPeers = fn
}

// peerSource is the autorelay.PeerSource. It takes one snapshot, sends at most
// numPeers candidates into a buffered channel and closes it at once — the
// same shape as go-libp2p's own static source — so it never blocks AutoRelay
// and a canceled ctx needs no goroutine to unwind. AutoRelay calls again
// (rate-limited by its minimum interval) for as long as it is short of
// candidates, so peers learned later are picked up on a later poll.
func (s *relayCandidateSource) peerSource(ctx context.Context, numPeers int) <-chan peer.AddrInfo {
	var cands []peer.AddrInfo
	if ctx.Err() == nil {
		cands = s.candidates(numPeers)
	}
	out := make(chan peer.AddrInfo, len(cands))
	defer close(out)
	for _, c := range cands {
		out <- c
	}
	return out
}

// candidates snapshots up to numPeers relay candidates: peers the peerstore
// already records as speaking the hop protocol first, then peers not yet
// identified (AutoRelay checks those itself). A peer identified without the
// hop protocol is left out, as is any peer with no address worth dialing.
// Each tier is shuffled: AutoRelay backs off a relay that refused it for an
// hour, and a fixed order truncated at numPeers could keep offering the same
// refusers while a willing relay sat just past the cut.
func (s *relayCandidateSource) candidates(numPeers int) []peer.AddrInfo {
	s.mu.RLock()
	h, routing := s.host, s.routingPeers
	s.mu.RUnlock()
	if h == nil || numPeers <= 0 {
		return nil
	}

	ps := h.Peerstore()
	seen := map[peer.ID]bool{h.ID(): true}
	var hop, unknown []peer.AddrInfo
	consider := func(p peer.ID) {
		if seen[p] {
			return
		}
		seen[p] = true
		protos, err := ps.GetProtocols(p)
		if err != nil {
			return
		}
		speaksHop := slices.Contains(protos, circuitproto.ProtoIDv2Hop)
		if len(protos) > 0 && !speaksHop {
			return
		}
		// Installs from before the hop service was removed (see
		// NewShareManager) still run it, so another mahpastes node — a
		// follower, the publisher this host follows, a LAN peer — can qualify
		// on protocols alone. Reserving on one would put its IP and
		// peer id into this host's advertised addresses and route each
		// follower dialing the circuit through it, linking who follows whom
		// for anyone who looks this host up. Only an identified peer can be
		// recognised; AutoRelay's own checks still apply to the rest.
		if slices.Contains(protos, ShareProtocolID) {
			return
		}
		addrs := slices.DeleteFunc(slices.Clone(ps.Addrs(p)), func(a ma.Multiaddr) bool {
			return !relayDialable(a)
		})
		if len(addrs) == 0 {
			return
		}
		ai := peer.AddrInfo{ID: p, Addrs: addrs}
		if speaksHop {
			hop = append(hop, ai)
		} else {
			unknown = append(unknown, ai)
		}
	}
	for _, p := range h.Network().Peers() {
		consider(p)
	}
	if routing != nil {
		for _, p := range routing() {
			consider(p)
		}
	}

	shuffle := func(ais []peer.AddrInfo) {
		rand.Shuffle(len(ais), func(i, j int) { ais[i], ais[j] = ais[j], ais[i] })
	}
	shuffle(hop)
	shuffle(unknown)
	out := append(hop, unknown...)
	if len(out) > numPeers {
		out = out[:numPeers]
	}
	return out
}

// relayDialable reports whether a is an address a relay can usefully be
// reached at: a public one, and never a relayed one (a peer reachable only
// through a relay cannot be one).
func relayDialable(a ma.Multiaddr) bool {
	if _, err := a.ValueForProtocol(ma.P_CIRCUIT); err == nil {
		return false
	}
	return manet.IsPublicAddr(a)
}

// followStreamOpenTimeout bounds opening a follower's stream to its publisher
// (see openFollowStream). It spans DCUtR's whole retry ladder — three
// attempts, each allowed a 10s direct dial — and stays under the swarm's own
// 60s cap on waiting for a connection to turn direct.
const followStreamOpenTimeout = 45 * time.Second

// openFollowStream opens the share protocol stream to pid, giving up after
// timeout.
//
// It deliberately does not pass network.WithAllowLimitedConn. When the only
// connection to the publisher is relayed — circuit v2, which go-libp2p marks
// Limited — the swarm holds the open until DCUtR upgrades it to a direct
// connection. That is the point: a public relay caps a relayed connection at
// about two minutes and 128 KiB, far too little to carry clips, so the relay
// is where the hole punch is arranged, never the data path.
//
// The bound is what turns a hole punch that cannot succeed into a failed
// attempt the follow loop logs and backs off from. Without a deadline of its
// own the open is cut only by the host's 10s protocol-negotiation timeout,
// which go-libp2p applies solely to a context that has none — shorter than
// DCUtR's retry ladder, so a punch that would have landed on its second
// attempt was abandoned, and an accident of the caller's context rather than
// a decision.
func openFollowStream(ctx context.Context, h host.Host, pid peer.ID, timeout time.Duration) (network.Stream, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return h.NewStream(ctx, pid, ShareProtocolID)
}
