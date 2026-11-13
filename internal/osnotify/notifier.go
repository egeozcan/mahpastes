package osnotify

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// Sender is the OS notification backend (*wailsbridge.Bridge in the app).
// Its methods may block; the Notifier only calls them from its worker.
type Sender interface {
	// InitNotifications prepares the backend and asks for permission where
	// the OS requires it.
	InitNotifications() error
	SendNotification(id, title, body string) error
	CleanupNotifications()
}

// State is how far the OS backend got.
type State string

const (
	// StateIdle: not initialised yet. That happens on the first
	// notification, or when the user switches notifications on.
	StateIdle State = "idle"
	// StatePending: initialising (on macOS, possibly waiting for the user
	// to answer the permission prompt).
	StatePending State = "pending"
	// StateReady: notifications can be shown.
	StateReady State = "ready"
	// StateFailed: the OS refused or the backend is unavailable. Nothing is
	// retried until the user switches notifications on again.
	StateFailed State = "failed"
)

// Status reports the backend state to the settings UI.
type Status struct {
	State State
	Error string
}

// maxSendFailures consecutive failed sends mark the backend as failed, so a
// broken notification daemon is not called (and waited on) for every event.
const maxSendFailures = 3

var (
	errNotRunning = errors.New("notifications are not running")
	errBusy       = errors.New("timed out waiting for the notification service")
)

// Options configures a Notifier.
type Options struct {
	Sender Sender
	// Enabled reports the persisted on/off setting. Called on the worker
	// just before sending, so a change takes effect without a restart.
	Enabled func() bool
	// Limits defaults to DefaultLimits when zero.
	Limits Limits
}

type request struct {
	activate bool // the user switched notifications on: retry a failed init
	test     bool // send a test notification now
	reply    chan error
}

// Notifier coalesces background events and shows them as OS notifications
// while the window is not focused.
type Notifier struct {
	sender  Sender
	enabled func() bool
	now     func() time.Time

	mu           sync.Mutex
	q            *queue
	focused      bool
	focusSeen    bool // a sequenced focus report was applied
	focusSession string
	focusSeq     float64
	started      bool
	stopped      bool
	state        State
	lastErr      string
	sendFailures int
	nextID       int

	wake chan struct{}
	reqs chan request
	stop chan struct{}
	done chan struct{}
}

// New returns a Notifier. Nothing is shown until Start is called.
func New(opts Options) *Notifier {
	limits := opts.Limits
	if limits == (Limits{}) {
		limits = DefaultLimits
	}
	enabled := opts.Enabled
	if enabled == nil {
		enabled = func() bool { return false }
	}
	return &Notifier{
		sender:  opts.Sender,
		enabled: enabled,
		now:     time.Now,
		q:       newQueue(limits),
		// Treat the window as focused until the frontend says otherwise, so
		// a page (re)load never produces notifications by itself.
		focused: true,
		state:   StateIdle,
		wake:    make(chan struct{}, 1),
		reqs:    make(chan request),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
}

// Start launches the worker. Calling it more than once has no effect.
func (n *Notifier) Start() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.started || n.stopped || n.sender == nil {
		return
	}
	n.started = true
	go n.run()
}

// Stop ends the worker and releases the backend. It waits at most wait for
// a worker stuck in a backend call; the process is shutting down anyway.
func (n *Notifier) Stop(wait time.Duration) {
	n.mu.Lock()
	if !n.started || n.stopped {
		n.stopped = true
		n.mu.Unlock()
		return
	}
	n.stopped = true
	n.q.clear()
	n.mu.Unlock()
	close(n.stop)
	select {
	case <-n.done:
	case <-time.After(wait):
		log.Printf("notifications: worker still busy in a system call at shutdown")
	}
}

// Accepting reports whether Post would keep an event right now: the worker
// runs and the window is not focused. Callers use it to skip building a
// Notice for events that would be dropped.
func (n *Notifier) Accepting() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.started && !n.stopped && !n.focused
}

// Post records a background event. It never blocks on the backend.
func (n *Notifier) Post(notice Notice) {
	if notice.Key == "" || notice.Format == nil {
		return
	}
	n.mu.Lock()
	if !n.started || n.stopped || n.focused {
		n.mu.Unlock()
		return
	}
	n.q.add(n.now(), notice)
	n.mu.Unlock()
	n.poke()
}

// SetFocused records whether the window has focus. Gaining focus drops what
// is waiting: the in-app toasts for those events are on screen.
func (n *Notifier) SetFocused(focused bool) {
	n.mu.Lock()
	n.focused = focused
	if focused {
		n.q.clear()
	}
	n.mu.Unlock()
}

// SetFocusedSeq is SetFocused for reports that can arrive out of order (Wails
// runs each frontend event on its own goroutine). seq increases within one
// page load, named by session; a report older than the last one applied for
// its session is ignored. It reports whether the report was applied.
func (n *Notifier) SetFocusedSeq(session string, seq float64, focused bool) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.focusSeen && session == n.focusSession && seq <= n.focusSeq {
		return false
	}
	n.focusSeen, n.focusSession, n.focusSeq = true, session, seq
	n.focused = focused
	if focused {
		n.q.clear()
	}
	return true
}

// Discard drops everything waiting (notifications were switched off).
func (n *Notifier) Discard() {
	n.mu.Lock()
	n.q.clear()
	n.mu.Unlock()
}

// Status reports the backend state.
func (n *Notifier) Status() Status {
	n.mu.Lock()
	defer n.mu.Unlock()
	return Status{State: n.state, Error: n.lastErr}
}

// Activate initialises the backend now (asking for permission where the OS
// requires it), retrying a previous failure. It waits at most wait.
func (n *Notifier) Activate(wait time.Duration) error {
	return n.call(request{activate: true}, wait)
}

// SendTest initialises the backend if needed and shows a test notification
// regardless of focus and rate limits.
func (n *Notifier) SendTest(wait time.Duration) error {
	return n.call(request{test: true}, wait)
}

func (n *Notifier) call(r request, wait time.Duration) error {
	n.mu.Lock()
	running := n.started && !n.stopped
	n.mu.Unlock()
	if !running {
		return errNotRunning
	}
	r.reply = make(chan error, 1)
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case n.reqs <- r:
	case <-t.C:
		return errBusy
	case <-n.done:
		return errNotRunning
	}
	select {
	case err := <-r.reply:
		return err
	case <-t.C:
		return errBusy
	case <-n.done:
		return errNotRunning
	}
}

func (n *Notifier) poke() {
	select {
	case n.wake <- struct{}{}:
	default:
	}
}

func (n *Notifier) run() {
	defer close(n.done)
	release := lockThread()
	defer release()
	for {
		n.mu.Lock()
		now := n.now()
		at := n.q.next(now)
		n.mu.Unlock()
		var timer *time.Timer
		var fire <-chan time.Time
		if !at.IsZero() {
			d := at.Sub(now)
			if d < 0 {
				d = 0
			}
			timer = time.NewTimer(d)
			fire = timer.C
		}
		select {
		case <-n.stop:
			if timer != nil {
				timer.Stop()
			}
			n.cleanup()
			return
		case <-n.wake:
		case <-fire:
		case r := <-n.reqs:
			r.reply <- n.handle(r)
		}
		if timer != nil {
			timer.Stop()
		}
		n.flush()
	}
}

func (n *Notifier) cleanup() {
	n.mu.Lock()
	ready := n.state == StateReady
	n.mu.Unlock()
	if ready {
		n.sender.CleanupNotifications()
	}
}

func (n *Notifier) handle(r request) error {
	if r.activate {
		n.mu.Lock()
		if n.state == StateFailed {
			n.state = StateIdle
			n.lastErr = ""
			n.sendFailures = 0
		}
		n.mu.Unlock()
	}
	if err := n.ensureInit(); err != nil {
		return err
	}
	if r.test {
		return n.send("mahpastes", "Notifications are on. You will see one when something happens while mahpastes is in the background.")
	}
	return nil
}

// flush sends whatever is due.
func (n *Notifier) flush() {
	n.mu.Lock()
	if n.focused {
		n.q.clear()
		n.mu.Unlock()
		return
	}
	now := n.now()
	at := n.q.next(now)
	n.mu.Unlock()
	if at.IsZero() || at.After(now) {
		return
	}
	// Read the setting before popping, so events that arrive while it is off
	// neither show nor use up the per-kind cooldown and the overall cap.
	if !n.enabled() {
		n.Discard()
		return
	}
	// Initialise before popping: on macOS this can wait minutes on the
	// permission prompt, and a pop charges the cooldown and the overall cap.
	n.mu.Lock()
	ready := n.state == StateReady
	n.mu.Unlock()
	if n.ensureInit() != nil {
		n.Discard()
		return
	}
	// The setting may have been switched off while the prompt was open.
	if !ready && !n.enabled() {
		n.Discard()
		return
	}
	n.mu.Lock()
	if n.focused {
		n.q.clear()
		n.mu.Unlock()
		return
	}
	due := n.q.pop(n.now())
	n.mu.Unlock()
	for _, b := range due {
		// A send can block for seconds; coming back to the window drops the
		// rest of the batch like anything else still waiting.
		n.mu.Lock()
		focused := n.focused
		n.mu.Unlock()
		if focused {
			return
		}
		title, body := render(b)
		if n.send(title, body) != nil {
			n.mu.Lock()
			failed := n.state == StateFailed
			n.mu.Unlock()
			if failed {
				n.Discard()
				return
			}
		}
	}
}

// ensureInit initialises the backend once; a failure sticks until Activate.
func (n *Notifier) ensureInit() error {
	n.mu.Lock()
	switch n.state {
	case StateReady:
		n.mu.Unlock()
		return nil
	case StateFailed:
		err := errors.New(n.lastErr)
		n.mu.Unlock()
		return err
	}
	n.state = StatePending
	n.mu.Unlock()

	err := n.sender.InitNotifications()

	n.mu.Lock()
	defer n.mu.Unlock()
	if err != nil {
		n.state = StateFailed
		n.lastErr = err.Error()
		log.Printf("notifications: unavailable: %v", err)
		return err
	}
	n.state = StateReady
	n.lastErr = ""
	n.sendFailures = 0
	return nil
}

func (n *Notifier) send(title, body string) error {
	n.mu.Lock()
	n.nextID++
	id := fmt.Sprintf("mahpastes-%d-%d", n.now().UnixNano(), n.nextID)
	n.mu.Unlock()

	err := n.sender.SendNotification(id, truncate(cleanText(title), 120), truncate(cleanText(body), 400))

	n.mu.Lock()
	defer n.mu.Unlock()
	if err == nil {
		n.sendFailures = 0
		return nil
	}
	n.sendFailures++
	log.Printf("notifications: send failed: %v", err)
	if n.sendFailures >= maxSendFailures {
		n.state = StateFailed
		n.lastErr = err.Error()
	}
	return err
}

// cleanText makes s safe for every backend. Titles and bodies carry file,
// tag and plugin text, which can be any bytes: macOS turns a string that is
// not valid UTF-8 into nil (a notification with no text), and a NUL ends the
// C string early.
func cleanText(s string) string {
	s = strings.ToValidUTF8(s, "\uFFFD")
	return strings.ReplaceAll(s, "\x00", "")
}

// truncate shortens s to at most max runes, ending with an ellipsis.
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
