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
	// activateOnAttach records an activation a later launch asked for
	// before there was a window to activate (SubmitRelaunch).
	activateOnAttach bool
	// activationAsked is set once any request asked for the window.
	activationAsked bool
	// showMu orders activate against HideUnlessAsked's hide, so a load-time
	// hide can never land after the show a request asked for.
	showMu sync.Mutex
}

// NewQueue returns an empty queue that notifies delay after the last Submit.
func NewQueue(delay time.Duration) *Queue {
	return &Queue{delay: delay, seen: map[string]bool{}}
}

// Attach connects the queue to a running window. activate shows and raises it;
// notify tells the frontend there are requests to take. Either may be nil.
// Anything submitted before Attach is announced right away, and an activation
// a later launch asked for before it (SubmitRelaunch) happens now.
func (q *Queue) Attach(activate, notify func()) {
	q.mu.Lock()
	q.activate = activate
	q.notify = notify
	if !q.pending.Empty() {
		q.scheduleLocked()
	}
	replay := q.activateOnAttach && activate != nil
	if replay {
		q.activateOnAttach = false
	}
	q.mu.Unlock()

	if replay {
		q.showMu.Lock()
		activate()
		q.showMu.Unlock()
	}
}

// HideUnlessAsked runs hide unless a request has asked for the window since
// the process started. A hidden start hides once its page has loaded (macOS);
// a launch or file open that arrived before that was told it was taken, and
// hiding after it would leave the user with nothing on screen. This process's
// own --hidden arguments never ask (FromArgs clears Activate). A request that
// arrives while hide runs is activated after it.
func (q *Queue) HideUnlessAsked(hide func()) {
	q.showMu.Lock()
	defer q.showMu.Unlock()
	q.mu.Lock()
	asked := q.activationAsked
	q.mu.Unlock()
	if !asked {
		hide()
	}
}

// Submit adds a request. Activation happens at once (before the debounce), so
// the window comes forward as soon as the user asks for it. Before Attach
// there is no window to bring forward and the activation is dropped: the
// requests of this process's own start (its arguments, a macOS file open on
// a cold launch) come with a window that is about to show by itself.
func (q *Queue) Submit(r Request) { q.submit(r, false) }

// SubmitRelaunch adds a request another launch sent over the instance socket.
// Unlike Submit, an activation that arrives before Attach is kept and happens
// at Attach. That launch was told its request was taken and has exited; when
// this process started without a window (--hidden at login, WebView2 still
// loading) and the user then started the app, dropping the activation would
// leave them with nothing on screen and, on Windows and Linux, no tray icon
// to recover from.
func (q *Queue) SubmitRelaunch(r Request) { q.submit(r, true) }

func (q *Queue) submit(r Request, keepActivation bool) {
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
	if r.Activate {
		// Set before activate runs: see HideUnlessAsked.
		q.activationAsked = true
	}
	if r.Activate && activate == nil && keepActivation {
		q.activateOnAttach = true
	}
	q.mu.Unlock()

	if r.Activate && activate != nil {
		q.showMu.Lock()
		activate()
		q.showMu.Unlock()
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
