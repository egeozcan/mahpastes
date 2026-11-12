package openrequest

import (
	"log"
	"sync"
	"time"
)

// Queue limits. A request beyond them is still activated; only the overflow
// files or URLs are dropped (and logged). Explorer caps one command line at
// about 32K characters, so a real Send To never comes near maxQueuedFiles.
const (
	maxQueuedFiles = 10000
	maxQueuedURLs  = 100
)

// DefaultNotifyDelay coalesces bursts into one notification. macOS calls
// application:openFile: once per file, a few milliseconds apart, and each
// notification costs the frontend a drain and an upload summary toast.
const DefaultNotifyDelay = 150 * time.Millisecond

// Queue buffers open requests until the frontend takes them.
//
// Requests can arrive before there is a window to show them in: the launch
// arguments of the first process, a macOS file open on cold launch (AppKit
// delivers it before Wails' OnStartup), or a second launch while the first is
// still starting. So nothing is pushed at the frontend: the queue holds the
// work, and once a frontend is attached it is told (after a short debounce)
// that there is something to take. A frontend that loads later drains on its
// own at startup, so a notification lost to a page that was not listening yet
// loses nothing.
//
// The pull model also means exactly one frontend imports each file even when
// more than one is attached (wails dev serves the native window and a browser
// at the same time).
type Queue struct {
	mu       sync.Mutex
	pending  Request
	seen     map[string]bool // files already pending, to drop exact repeats
	delay    time.Duration
	timer    *time.Timer
	activate func()
	notify   func()
}

// NewQueue returns an empty queue that notifies delay after the last Submit.
func NewQueue(delay time.Duration) *Queue {
	return &Queue{delay: delay, seen: map[string]bool{}}
}

// Attach connects the queue to a running window. activate shows and raises it;
// notify tells the frontend there are requests to take. Either may be nil.
// Anything submitted before Attach is announced right away.
func (q *Queue) Attach(activate, notify func()) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.activate = activate
	q.notify = notify
	if !q.pending.Empty() {
		q.scheduleLocked()
	}
}

// Submit adds a request. Activation happens at once (before the debounce), so
// the window comes forward as soon as the user asks for it.
func (q *Queue) Submit(r Request) {
	q.mu.Lock()
	activate := q.activate
	for _, f := range r.Files {
		if q.seen[f] {
			continue
		}
		if len(q.pending.Files) >= maxQueuedFiles {
			log.Printf("open request: queue full, dropping %s", f)
			continue
		}
		q.seen[f] = true
		q.pending.Files = append(q.pending.Files, f)
	}
	for _, u := range r.URLs {
		if len(q.pending.URLs) >= maxQueuedURLs {
			log.Printf("open request: queue full, dropping URL")
			break
		}
		q.pending.URLs = append(q.pending.URLs, u)
	}
	// Repeated paste requests before a drain collapse into one paste: the
	// clipboard has one content.
	if r.PasteClipboard {
		q.pending.PasteClipboard = true
	}
	if !q.pending.Empty() {
		q.scheduleLocked()
	}
	q.mu.Unlock()

	if r.Activate && activate != nil {
		activate()
	}
}

// Take returns everything pending and empties the queue.
func (q *Queue) Take() Request {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := Request{Files: q.pending.Files, URLs: q.pending.URLs, PasteClipboard: q.pending.PasteClipboard}
	q.pending = Request{}
	q.seen = map[string]bool{}
	if q.timer != nil {
		q.timer.Stop()
		q.timer = nil
	}
	return out
}

// scheduleLocked (re)starts the debounce timer. Requires q.mu.
func (q *Queue) scheduleLocked() {
	if q.notify == nil {
		return
	}
	if q.timer != nil {
		q.timer.Stop()
	}
	q.timer = time.AfterFunc(q.delay, q.fire)
}

func (q *Queue) fire() {
	q.mu.Lock()
	notify := q.notify
	pending := !q.pending.Empty()
	q.timer = nil
	q.mu.Unlock()
	if pending && notify != nil {
		notify()
	}
}
