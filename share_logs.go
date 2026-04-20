package main

import (
	"log"
	"sync"
	"time"
)

// shareLogRingCap bounds the in-memory log buffer. 500 entries is enough for
// diagnosing a few minutes of activity without bloating memory. Older entries
// roll off silently.
const shareLogRingCap = 500

// shareLogBuffer is a bounded circular buffer of share-system events. Writes
// are non-blocking and self-evicting; reads snapshot under an RLock and
// reorder newest-first for the UI.
//
// The buffer is diagnostic-only — entries are lost on app restart. If we ever
// need audit history, promote this to a persisted share_logs table.
type shareLogBuffer struct {
	mu      sync.RWMutex
	entries [shareLogRingCap]ShareLogEntry
	next    int // circular write index into entries
	count   int // number of valid entries (grows to cap, then stays)
}

func newShareLogBuffer() *shareLogBuffer { return &shareLogBuffer{} }

// append records an entry. Also forwards to log.Printf so the CLI/journal
// still carries the same messages (keeps console debugging parity).
func (b *shareLogBuffer) append(e ShareLogEntry) {
	if e.Timestamp == 0 {
		e.Timestamp = time.Now().Unix()
	}
	b.mu.Lock()
	b.entries[b.next] = e
	b.next = (b.next + 1) % shareLogRingCap
	if b.count < shareLogRingCap {
		b.count++
	}
	b.mu.Unlock()

	// Mirror to stdlib log with a compact prefix so operators grepping
	// stderr see the same events the UI shows.
	scope := e.Scope
	if scope == "" {
		scope = "share"
	}
	switch e.Level {
	case "error":
		log.Printf("share[%s] ERROR: %s", scope, e.Message)
	case "warn":
		log.Printf("share[%s] WARN: %s", scope, e.Message)
	default:
		log.Printf("share[%s]: %s", scope, e.Message)
	}
}

// snapshot returns entries in newest-first order. If followID > 0, restricts
// to entries tagged with that follow id. If publicationID > 0, restricts to
// that publication id. Both filters can be applied together (AND).
func (b *shareLogBuffer) snapshot(followID, publicationID int64) []ShareLogEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	out := make([]ShareLogEntry, 0, b.count)
	// Walk newest-first: start at the slot before next (which is the most
	// recent write) and walk backwards count times, wrapping at 0.
	for i := 0; i < b.count; i++ {
		idx := (b.next - 1 - i + shareLogRingCap) % shareLogRingCap
		e := b.entries[idx]
		if followID > 0 && e.FollowID != followID {
			continue
		}
		if publicationID > 0 && e.PublicationID != publicationID {
			continue
		}
		out = append(out, e)
	}
	return out
}
