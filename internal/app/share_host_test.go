package app

import (
	"context"
	cryptoRand "crypto/rand"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	libp2pCrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/host/autorelay"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"
	circuitproto "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/proto"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

// A publisher behind an ordinary home NAT is reachable from another network
// only through a circuit-v2 reservation: that is what puts a /p2p-circuit
// address in its peer record, and the relayed connection a follower makes to
// it is what DCUtR punches a direct hole from. These tests stand the pieces up
// in-process on loopback.

// reserveCountingACL admits everything and counts reservation requests.
type reserveCountingACL struct{ reserves atomic.Int64 }

func (a *reserveCountingACL) AllowReserve(peer.ID, ma.Multiaddr) bool {
	a.reserves.Add(1)
	return true
}

func (a *reserveCountingACL) AllowConnect(peer.ID, ma.Multiaddr, peer.ID) bool { return true }

// newLoopbackHost starts a host listening only on loopback.
func newLoopbackHost(t *testing.T, opts ...libp2p.Option) host.Host {
	t.Helper()
	h, err := libp2p.New(append([]libp2p.Option{libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0")}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close() })
	return h
}

// advertisePublic makes a loopback host also advertise a public address that
// nothing listens on. Relay candidates and the circuit addresses AutoRelay
// builds from them are public-only, so a pure-loopback relay would be skipped
// for the very reason production skips LAN-only ones; the tests connect over
// loopback first, so nothing needs to dial the advertised address.
func advertisePublic(ip string) libp2p.Option {
	fake := ma.StringCast("/ip4/" + ip + "/tcp/4001")
	return libp2p.AddrsFactory(func(addrs []ma.Multiaddr) []ma.Multiaddr {
		return append(addrs, fake)
	})
}

// newTestRelay starts a circuit-v2 relay (hop) service on loopback that also
// advertises a public address (see advertisePublic).
func newTestRelay(t *testing.T, acl relay.ACLFilter) host.Host {
	t.Helper()
	rh := newLoopbackHost(t, libp2p.ForceReachabilityPublic(), advertisePublic("1.2.3.4"))
	opts := []relay.Option{}
	if acl != nil {
		opts = append(opts, relay.WithACL(acl))
	}
	r, err := relay.New(rh, opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return rh
}

// loopbackInfo is h's AddrInfo restricted to loopback, so a test dials only
// what is actually listening.
func loopbackInfo(h host.Host) peer.AddrInfo {
	var addrs []ma.Multiaddr
	for _, a := range h.Addrs() {
		if manet.IsIPLoopback(a) {
			addrs = append(addrs, a)
		}
	}
	return peer.AddrInfo{ID: h.ID(), Addrs: addrs}
}

func hasCircuitAddr(addrs []ma.Multiaddr) bool {
	for _, a := range addrs {
		if _, err := a.ValueForProtocol(ma.P_CIRCUIT); err == nil {
			return true
		}
	}
	return false
}

// The regression for the shipped configuration: AutoRelay fed a nil static
// relay list desires zero relays, so even a host that knows it is behind NAT
// and is already connected to a relay never asks it for a slot. This host runs
// the production option set and candidate source — shareHostOptions, the very
// list NewShareManager passes — plus only what a test needs: reachability
// forced private (what AutoNAT concludes behind a NAT) and AutoRelay's polling
// shortened from minutes to milliseconds.
func TestShareHostReservesRelaySlotWhenPrivate(t *testing.T) {
	acl := &reserveCountingACL{}
	rh := newTestRelay(t, acl)

	priv, _, err := libp2pCrypto.GenerateEd25519Key(cryptoRand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	relays := newRelayCandidateSource()
	opts := append(
		shareHostOptions(priv, relays.peerSource,
			autorelay.WithBootDelay(0),
			autorelay.WithMinInterval(50*time.Millisecond),
		),
		libp2p.ForceReachabilityPrivate(),
	)
	h, err := libp2p.New(opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close() })
	relays.setHost(h)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := h.Connect(ctx, loopbackInfo(rh)); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for acl.reserves.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if n := acl.reserves.Load(); n == 0 {
		t.Fatal("no reservation requested in 10s — a NATed publisher would have no /p2p-circuit address and DCUtR would never run")
	}
	// The reservation is only worth anything once it is advertised: the
	// circuit address is what a follower on another network dials.
	for !hasCircuitAddr(h.Addrs()) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !hasCircuitAddr(h.Addrs()) {
		t.Fatalf("reserved, but no /p2p-circuit address advertised: %v", h.Addrs())
	}
}

// drainCandidates collects everything a peer source yields, failing if the
// channel is not closed promptly — AutoRelay never calls the source again
// until it is.
func drainCandidates(t *testing.T, ch <-chan peer.AddrInfo) []peer.AddrInfo {
	t.Helper()
	var out []peer.AddrInfo
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ai, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, ai)
		case <-timeout:
			t.Fatal("peer source channel not closed within 5s")
		}
	}
}

func idSet(ais []peer.AddrInfo) map[peer.ID]bool {
	s := map[peer.ID]bool{}
	for _, ai := range ais {
		s[ai.ID] = true
	}
	return s
}

// waitIdentified blocks until h's peerstore has recorded p's protocols.
func waitIdentified(t *testing.T, h host.Host, p peer.ID) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if protos, _ := h.Peerstore().GetProtocols(p); len(protos) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("identify with %s did not complete", p)
}

func TestRelayCandidateSource(t *testing.T) {
	ctx := context.Background()
	self := newLoopbackHost(t)

	// Hosts that advertise the hop protocol, as a relay's identify does, and
	// a public address; one that advertises the protocol but only loopback,
	// as a relay found on the LAN would; and one identified without it.
	speakHop := func(h host.Host) host.Host {
		h.SetStreamHandler(circuitproto.ProtoIDv2Hop, func(s network.Stream) { s.Reset() })
		return h
	}
	var hops []host.Host
	for i := 0; i < 3; i++ {
		hops = append(hops, speakHop(newLoopbackHost(t, advertisePublic(fmt.Sprintf("1.2.3.%d", 10+i)))))
	}
	lanOnly := speakHop(newLoopbackHost(t))
	plain := newLoopbackHost(t, advertisePublic("1.2.3.20"))
	// Another mahpastes node: older versions also ran the hop service, so a
	// follower or publisher with a public address can look like any relay.
	sharePeer := speakHop(newLoopbackHost(t, advertisePublic("1.2.3.21")))
	sharePeer.SetStreamHandler(ShareProtocolID, func(s network.Stream) { s.Reset() })
	for _, h := range append(append([]host.Host{}, hops...), lanOnly, plain, sharePeer) {
		if err := self.Connect(ctx, loopbackInfo(h)); err != nil {
			t.Fatal(err)
		}
		waitIdentified(t, self, h.ID())
	}

	src := newRelayCandidateSource()
	src.setHost(self)

	t.Run("yields connected relays with their addresses", func(t *testing.T) {
		got := drainCandidates(t, src.peerSource(ctx, 20))
		ids := idSet(got)
		if len(got) != len(hops) {
			t.Fatalf("yielded %d candidates, want %d (the hop peers): %v", len(got), len(hops), got)
		}
		for _, h := range hops {
			if !ids[h.ID()] {
				t.Fatalf("hop peer %s not yielded", h.ID())
			}
		}
		if ids[plain.ID()] {
			t.Fatal("yielded a peer identified without the hop protocol")
		}
		if ids[lanOnly.ID()] {
			// AutoRelay advertises a reservation only through the relay's
			// public addresses, so this one would burn one of its two slots
			// on a reservation nobody outside the LAN can use.
			t.Fatal("yielded a relay with no public address")
		}
		if ids[self.ID()] {
			t.Fatal("yielded the host itself")
		}
		if ids[sharePeer.ID()] {
			// Reserving on it would put that node's IP and peer id into this
			// host's advertised addresses, and route every follower that
			// dials the circuit through it.
			t.Fatal("yielded another mahpastes node as a relay")
		}
		for _, ai := range got {
			if len(ai.Addrs) == 0 {
				t.Fatalf("candidate %s carries no addresses — AutoRelay could not dial it", ai.ID)
			}
			for _, a := range ai.Addrs {
				if !manet.IsPublicAddr(a) {
					t.Fatalf("candidate %s offered with non-public address %s", ai.ID, a)
				}
			}
		}
	})

	t.Run("sends at most numPeers", func(t *testing.T) {
		if got := drainCandidates(t, src.peerSource(ctx, 2)); len(got) != 2 {
			t.Fatalf("yielded %d candidates for numPeers=2", len(got))
		}
	})

	t.Run("includes routing-table peers it is not connected to", func(t *testing.T) {
		// Known only from the peerstore, as a DHT routing-table entry is.
		far := peer.ID("routing-table-relay")
		addr := ma.StringCast("/ip4/1.2.3.30/tcp/4001")
		self.Peerstore().AddAddrs(far, []ma.Multiaddr{addr}, time.Hour)
		if err := self.Peerstore().AddProtocols(far, circuitproto.ProtoIDv2Hop); err != nil {
			t.Fatal(err)
		}
		src.setRoutingPeers(func() []peer.ID { return []peer.ID{far, hops[0].ID()} })
		defer src.setRoutingPeers(nil)

		got := drainCandidates(t, src.peerSource(ctx, 20))
		if !idSet(got)[far] {
			t.Fatalf("routing-table relay not yielded: %v", got)
		}
		if len(got) != len(hops)+1 {
			t.Fatalf("yielded %d candidates, want %d — a peer both connected and in the table must appear once", len(got), len(hops)+1)
		}
	})

	t.Run("closes without yielding once ctx is done", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		if got := drainCandidates(t, src.peerSource(cctx, 20)); len(got) != 0 {
			t.Fatalf("yielded %d candidates on a canceled ctx", len(got))
		}
	})

	t.Run("yields nothing before the host exists", func(t *testing.T) {
		// AutoRelay can poll before libp2p.New has even returned the host.
		if got := drainCandidates(t, newRelayCandidateSource().peerSource(ctx, 20)); len(got) != 0 {
			t.Fatalf("yielded %d candidates with no host", len(got))
		}
	})
}

// A follower whose only connection to the publisher is relayed must not open
// its stream there: a public relay caps a relayed connection at a couple of
// minutes and 128 KiB, far too little for clip data, so the stream waits for
// DCUtR to upgrade the connection to a direct one. When hole punching cannot
// succeed, that wait must end at the bound so the follow loop logs it and
// backs off — here the publisher has no direct address at all and runs no
// hole punching, so nothing will ever upgrade the connection.
func TestFollowStreamOpenOverRelayIsBounded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rh := newTestRelay(t, nil)

	// No listen address: the only way in is a relayed connection, as for a
	// publisher behind NAT. NoListenAddrs also drops the relay transport
	// unless it is asked for explicitly.
	pub, err := libp2p.New(libp2p.NoListenAddrs, libp2p.EnableRelay())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pub.Close() })
	pub.SetStreamHandler(ShareProtocolID, func(s network.Stream) { s.Close() })
	if err := pub.Connect(ctx, loopbackInfo(rh)); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Reserve(ctx, pub, loopbackInfo(rh)); err != nil {
		t.Fatal(err)
	}

	fol := newLoopbackHost(t)
	circuit := ma.StringCast("/p2p/" + rh.ID().String() + "/p2p-circuit")
	var viaRelay []ma.Multiaddr
	for _, a := range loopbackInfo(rh).Addrs {
		viaRelay = append(viaRelay, a.Encapsulate(circuit))
	}
	if err := fol.Connect(ctx, peer.AddrInfo{ID: pub.ID(), Addrs: viaRelay}); err != nil {
		t.Fatalf("connect via %v: %+v", viaRelay, err)
	}
	if c := fol.Network().Connectedness(pub.ID()); c != network.Limited {
		t.Fatalf("follower connectedness %v, want Limited — the harness must leave only a relayed path", c)
	}

	// Like followSession's session context, this one has no deadline of its
	// own: the bound has to come from openFollowStream.
	sessCtx, sessCancel := context.WithCancel(context.Background())
	defer sessCancel()
	const bound = 300 * time.Millisecond
	start := time.Now()
	s, err := openFollowStream(sessCtx, fol, pub.ID(), bound)
	elapsed := time.Since(start)
	if err == nil {
		s.Reset()
		t.Fatal("stream opened over a relayed connection — clip data would ride a byte-capped relay")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("stream open took %v to give up, want about %v — the follow loop is stuck there instead of backing off", elapsed, bound)
	}
	if elapsed < bound-50*time.Millisecond {
		t.Fatalf("stream open failed after %v (%v) — before the bound, so the harness is not exercising the wait", elapsed, err)
	}

	// Control: the connection itself is healthy; only the limited-conn rule
	// stood in the way.
	s, err = fol.NewStream(network.WithAllowLimitedConn(ctx, "test control"), pub.ID(), ShareProtocolID)
	if err != nil {
		t.Fatalf("control stream over the relayed connection failed: %v", err)
	}
	s.Reset()
}

// A share host is a relay client, never a relay. NewShareManager used to call
// relay.New on its host, which starts the circuit-v2 hop service: any peer on
// the public network could then reserve a slot and pipe its traffic through
// the user's machine, and an install with a public address advertises that
// service to the DHT. Being reached through someone else's relay needs only
// the stop protocol, which libp2p.EnableRelay registers on its own.
func TestShareManagerDoesNotRunRelayHop(t *testing.T) {
	m, err := NewShareManager(context.Background(), newTestDB(t), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	var hop, stop bool
	for _, p := range m.Host().Mux().Protocols() {
		switch p {
		case circuitproto.ProtoIDv2Hop:
			hop = true
		case circuitproto.ProtoIDv2Stop:
			stop = true
		}
	}
	if hop {
		t.Fatalf("share host serves %s — it is relaying traffic for arbitrary peers", circuitproto.ProtoIDv2Hop)
	}
	if !stop {
		t.Fatalf("share host does not serve %s — it can no longer be reached through a relay", circuitproto.ProtoIDv2Stop)
	}

	// libp2p.EnableRelayService would register the hop handler later, and only
	// once AutoNAT reports public reachability — the check above cannot see it.
	var cfg libp2p.Config
	priv, _, err := libp2pCrypto.GenerateEd25519Key(cryptoRand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Apply(shareHostOptions(priv, newRelayCandidateSource().peerSource)...); err != nil {
		t.Fatal(err)
	}
	if cfg.EnableRelayService {
		t.Fatal("shareHostOptions enables the relay service")
	}
}
