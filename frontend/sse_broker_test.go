package webui

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-clipboard/internal/bridgeiface"
)

func brokerConnCount(b *SSEBroker) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.channels)
}

func TestSSEBrokerEmitBroadcastsTaggedFrame(t *testing.T) {
	b := NewSSEBroker()
	ch := make(chan string, 4)
	if !b.register(ch, 0) {
		t.Fatal("register should succeed")
	}
	b.Emit("clip:created", map[string]any{"id": 7})
	select {
	case frame := <-ch:
		if !strings.Contains(frame, "event: clip:created") || !strings.Contains(frame, `"id":7`) {
			t.Fatalf("unexpected frame %q", frame)
		}
	default:
		t.Fatal("expected a broadcast frame")
	}
}

func TestSSEBrokerEnforcesGlobalCap(t *testing.T) {
	b := NewSSEBroker()
	for i := 0; i < sseMaxConnections; i++ {
		if !b.register(make(chan string, 1), 0) {
			t.Fatalf("registration %d under cap should succeed", i)
		}
	}
	if b.register(make(chan string, 1), 0) {
		t.Fatal("registration past global cap must be refused")
	}
}

func TestSSEBrokerEnforcesPerKeyCap(t *testing.T) {
	b := NewSSEBroker()
	const key = int64(99)
	for i := 0; i < sseMaxConnectionsPerKey; i++ {
		if !b.register(make(chan string, 1), key) {
			t.Fatalf("per-key registration %d under cap should succeed", i)
		}
	}
	if b.register(make(chan string, 1), key) {
		t.Fatal("registration past per-key cap must be refused")
	}
	if !b.register(make(chan string, 1), key+1) {
		t.Fatal("a different key must not be capped by another key's streams")
	}
}

func TestSSEBrokerUnregisterReleasesPerKeySlot(t *testing.T) {
	b := NewSSEBroker()
	const key = int64(5)
	ch := make(chan string, 1)
	if !b.register(ch, key) {
		t.Fatal("register should succeed")
	}
	b.unregister(ch, key)
	b.mu.RLock()
	_, stillTracked := b.perKey[key]
	b.mu.RUnlock()
	if stillTracked {
		t.Fatal("per-key counter must be deleted when it reaches zero")
	}
}

func TestSSEBrokerShutdownClosesStreamsAndRefusesNew(t *testing.T) {
	b := NewSSEBroker()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/events", nil)

	done := make(chan struct{})
	go func() {
		b.ServeHTTP(rec, req)
		close(done)
	}()

	waitFor(t, time.Second, func() bool { return brokerConnCount(b) == 1 })

	b.Shutdown()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ServeHTTP did not return after Shutdown")
	}

	if b.register(make(chan string, 1), 0) {
		t.Fatal("register after Shutdown must be refused")
	}
}

func TestSSEBrokerClosesWhenRevalidationFails(t *testing.T) {
	old := ssePingInterval
	ssePingInterval = 10 * time.Millisecond
	defer func() { ssePingInterval = old }()

	b := NewSSEBroker()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/events", nil)
	// Simulate a key that has just been revoked: revalidation returns false.
	auth := &bridgeiface.SSEAuth{KeyID: 1, Revalidate: func() bool { return false }}
	req = req.WithContext(bridgeiface.WithSSEAuth(req.Context(), auth))

	done := make(chan struct{})
	go func() {
		b.ServeHTTP(rec, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream should close once revalidation fails on a ping tick")
	}
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}
