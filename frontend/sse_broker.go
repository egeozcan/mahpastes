package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"go-clipboard/internal/bridgeiface"
)

const (
	// sseMaxConnections caps the total number of concurrent event streams so a
	// network-exposed daemon cannot be exhausted by opening streams without
	// bound (each stream pins a goroutine and a buffered channel).
	sseMaxConnections = 256
	// sseMaxConnectionsPerKey caps concurrent streams for a single key/session.
	sseMaxConnectionsPerKey = 8
)

// ssePingInterval is how often a keep-alive comment is written. Each tick also
// re-checks that the connection is still authorized. A var (not const) so tests
// can shrink it.
var ssePingInterval = 30 * time.Second

// SSEBroker broadcasts backend events to all connected browser clients.
type SSEBroker struct {
	mu        sync.RWMutex
	channels  map[chan string]struct{}
	perKey    map[int64]int
	closed    chan struct{}
	closeOnce sync.Once
}

func NewSSEBroker() *SSEBroker {
	return &SSEBroker{
		channels: make(map[chan string]struct{}),
		perKey:   make(map[int64]int),
		closed:   make(chan struct{}),
	}
}

func (b *SSEBroker) Emit(name string, data ...any) {
	var payload []byte
	if len(data) == 1 {
		payload, _ = json.Marshal(data[0])
	} else {
		payload, _ = json.Marshal(data)
	}
	frame := fmt.Sprintf("event: %s\ndata: %s\n\n", name, payload)

	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.channels {
		select {
		case ch <- frame:
		default:
			// Drop frames for slow consumers. The UI can resync with status/list APIs.
		}
	}
}

// On is intentionally a no-op: browsers cannot send upstream events via SSE.
// Backend state that the desktop bridge would receive via On (e.g. the plugin
// modal ack/close) is delivered over a dedicated REST endpoint in server mode.
func (b *SSEBroker) On(name string, cb func(data ...any)) {}

// Shutdown terminates every open SSE connection so their handler goroutines
// return promptly. Without this, a graceful http.Server.Shutdown would block
// for its full timeout waiting on streams that only notice cancellation when
// the request context is finally torn down.
func (b *SSEBroker) Shutdown() {
	b.closeOnce.Do(func() { close(b.closed) })
}

// register reserves a slot for a new stream, enforcing the global and per-key
// caps. It returns false (and reserves nothing) when a cap is hit or the broker
// is shutting down.
func (b *SSEBroker) register(ch chan string, keyID int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	select {
	case <-b.closed:
		return false
	default:
	}
	if len(b.channels) >= sseMaxConnections {
		return false
	}
	if keyID != 0 && b.perKey[keyID] >= sseMaxConnectionsPerKey {
		return false
	}
	b.channels[ch] = struct{}{}
	if keyID != 0 {
		b.perKey[keyID]++
	}
	return true
}

func (b *SSEBroker) unregister(ch chan string, keyID int64) {
	b.mu.Lock()
	delete(b.channels, ch)
	if keyID != 0 {
		if n := b.perKey[keyID] - 1; n > 0 {
			b.perKey[keyID] = n
		} else {
			delete(b.perKey, keyID)
		}
	}
	b.mu.Unlock()
	close(ch)
}

func (b *SSEBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	auth := bridgeiface.SSEAuthFrom(r.Context())
	var keyID int64
	if auth != nil {
		keyID = auth.KeyID
	}

	ch := make(chan string, 64)
	if !b.register(ch, keyID) {
		http.Error(w, "too many event streams", http.StatusServiceUnavailable)
		return
	}
	defer b.unregister(ch, keyID)

	// This is a long-lived response. The server sets a finite ReadTimeout to
	// bound Slowloris on every other route; clear it here so the connection's
	// read deadline does not abort the stream mid-flight. A failure here is not
	// fatal — it just falls back to the server's ReadTimeout behaviour.
	_ = http.NewResponseController(w).SetReadDeadline(time.Time{})

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	ping := time.NewTicker(ssePingInterval)
	defer ping.Stop()

	for {
		select {
		case frame := <-ch:
			fmt.Fprint(w, frame)
			flusher.Flush()
		case <-ping.C:
			// Re-validate auth on every keep-alive so revoked keys and expired
			// sessions stop streaming instead of running until the client
			// happens to disconnect.
			if auth != nil && auth.Revalidate != nil && !auth.Revalidate() {
				return
			}
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case <-b.closed:
			return
		case <-r.Context().Done():
			return
		}
	}
}
