package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// SSEBroker broadcasts backend events to all connected browser clients.
type SSEBroker struct {
	mu       sync.RWMutex
	channels map[chan string]struct{}
}

func NewSSEBroker() *SSEBroker {
	return &SSEBroker{channels: make(map[chan string]struct{})}
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
func (b *SSEBroker) On(name string, cb func(data ...any)) {}

func (b *SSEBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan string, 64)
	b.mu.Lock()
	b.channels[ch] = struct{}{}
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.channels, ch)
		b.mu.Unlock()
		close(ch)
	}()

	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()

	for {
		select {
		case frame := <-ch:
			fmt.Fprint(w, frame)
			flusher.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
