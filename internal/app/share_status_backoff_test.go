package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
)

// captureFollowInfoEvents installs an event function that records the
// FollowInfo payloads emitted on share:follow-updated. setFollowStatus emits
// the same event name with a plain map, so filtering on the DTO type isolates
// the commit-time snapshot from later transport transitions.
func captureFollowInfoEvents(m *ShareManager) func() []FollowInfo {
	var mu sync.Mutex
	var got []FollowInfo
	m.SetEventFn(func(name string, data ...any) {
		if name != "share:follow-updated" || len(data) == 0 {
			return
		}
		if fi, ok := data[0].(FollowInfo); ok {
			mu.Lock()
			got = append(got, fi)
			mu.Unlock()
		}
	})
	return func() []FollowInfo {
		mu.Lock()
		defer mu.Unlock()
		return append([]FollowInfo(nil), got...)
	}
}

// FollowWithoutDial commits a follow whose peer is known to be unreachable.
// Reporting "connected" there put a green pill on a card that had never
// exchanged a byte with anyone.
func TestFollowWithoutDialReportsConnectingStatus(t *testing.T) {
	ctx := context.Background()

	pubDB := newTestDB(t)
	pubM, _ := NewShareManager(ctx, pubDB, t.TempDir())
	pubDB.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'x', '#aaa')`)
	info, _ := pubM.StartShare(1)
	pubM.Stop() // publisher gone: no dial can succeed

	fDB := newTestDB(t)
	fM, err := NewShareManager(ctx, fDB, t.TempDir())
	if err != nil {
		t.Fatalf("follower NewShareManager: %v", err)
	}
	defer fM.Stop()
	events := captureFollowInfoEvents(fM)

	fi, err := fM.FollowWithoutDial(info.ShareString, "inbox-offline")
	if err != nil {
		t.Fatalf("FollowWithoutDial: %v", err)
	}
	if fi.Status != "connecting" {
		t.Fatalf("returned status %q want connecting", fi.Status)
	}
	emitted := events()
	if len(emitted) != 1 {
		t.Fatalf("emitted %d FollowInfo events, want 1", len(emitted))
	}
	if emitted[0].Status != "connecting" {
		t.Fatalf("emitted status %q want connecting", emitted[0].Status)
	}
}

// Follow dials before committing, but a successful dial is still not a
// handshake — the status the caller gets back must be the one the follow row
// actually holds.
func TestFollowReportsConnectingBeforeHandshake(t *testing.T) {
	ctx := context.Background()

	pubDB := newTestDB(t)
	pubM, _ := NewShareManager(ctx, pubDB, t.TempDir())
	defer pubM.Stop()
	pubDB.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'recipes', '#aaa')`)
	info, _ := pubM.StartShare(1)

	fDB := newTestDB(t)
	fM, _ := NewShareManager(ctx, fDB, t.TempDir())
	defer fM.Stop()
	fM.Host().Peerstore().AddAddrs(pubM.Host().ID(), pubM.Host().Addrs(), time.Hour)
	events := captureFollowInfoEvents(fM)

	fi, err := fM.Follow(info.ShareString, "inbox")
	if err != nil {
		t.Fatalf("Follow: %v", err)
	}
	if fi.Status != "connecting" {
		t.Fatalf("returned status %q want connecting", fi.Status)
	}
	emitted := events()
	if len(emitted) != 1 || emitted[0].Status != "connecting" {
		t.Fatalf("emitted FollowInfo events %+v, want one with status connecting", emitted)
	}
}

// A follow that flapped early used to sit at the 30s cap for the rest of the
// app's life: the ladder counted sessions, not consecutive failures.
func TestNextFollowBackoff(t *testing.T) {
	cases := []struct {
		name       string
		current    time.Duration
		handshaked bool
		want       time.Duration
	}{
		{"floor doubles after a failed dial", ReconnectFloor, false, 2 * ReconnectFloor},
		{"growth clamps at the cap", 20 * time.Second, false, ReconnectCap},
		{"cap stays at the cap", ReconnectCap, false, ReconnectCap},
		{"handshake resets from the cap", ReconnectCap, true, ReconnectFloor},
		{"handshake resets from mid-ladder", 8 * time.Second, true, ReconnectFloor},
		{"handshake keeps the floor at the floor", ReconnectFloor, true, ReconnectFloor},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nextFollowBackoff(tc.current, tc.handshaked); got != tc.want {
				t.Fatalf("nextFollowBackoff(%v, %v) = %v want %v", tc.current, tc.handshaked, got, tc.want)
			}
		})
	}
}

// fakeConn implements just the Stat() method connStatusLabel needs, so relay
// classification is testable without standing up a circuit-v2 relay.
type fakeConn struct{ limited bool }

func (c fakeConn) Stat() network.ConnStats {
	return network.ConnStats{Stats: network.Stats{Limited: c.limited}}
}

func TestConnStatusLabel(t *testing.T) {
	if got := connStatusLabel(fakeConn{limited: false}); got != "connected" {
		t.Fatalf("direct conn labeled %q want connected", got)
	}
	if got := connStatusLabel(fakeConn{limited: true}); got != "connected_relayed" {
		t.Fatalf("limited conn labeled %q want connected_relayed", got)
	}
	if got := connStatusLabel(nil); got != "connected" {
		t.Fatalf("nil conn labeled %q want connected", got)
	}
}
