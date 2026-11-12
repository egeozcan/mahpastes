// Package osnotify turns background events (watch-folder imports, received
// shared clips, plugin task results and toasts) into native OS notifications
// while the mahpastes window is not focused.
//
// Bursts are coalesced per key ("Imported 12 clips" rather than twelve
// banners) and rate-limited per key and overall. Every call into the OS
// notification backend happens on one worker goroutine, never on the event
// path: those calls can block (macOS waits for the permission prompt, Linux
// makes a D-Bus round trip).
package osnotify

import (
	"sort"
	"strconv"
	"time"
)

// Notice is one background event to be shown, possibly combined with others
// that share its Key.
type Notice struct {
	// Key groups events into one notification, e.g. "share:12" for clips
	// received into tag 12.
	Key string
	// Detail is the per-event text (a filename, a task name, a toast
	// message). Only the most recent one is kept.
	Detail string
	// Label is resolved on the worker when the notification is sent, so the
	// event path never does lookups (a plugin or tag name). May be nil.
	Label func() string
	// Format builds the notification from the number of events combined,
	// the resolved label and the latest detail.
	Format func(count int, label, detail string) (title, body string)
}

// Limits bound how often notifications appear.
type Limits struct {
	// Debounce is the quiet period after the last event before a key is sent.
	Debounce time.Duration
	// MaxWait caps how long a steady stream of events delays a key.
	MaxWait time.Duration
	// KeyCooldown is the minimum gap between two notifications for one key.
	KeyCooldown time.Duration
	// RateWindow and RateMax cap notifications overall: at most RateMax in
	// any RateWindow.
	RateWindow time.Duration
	RateMax    int
	// MaxPending caps the number of distinct keys waiting; further keys are
	// folded into one "more background events" notification.
	MaxPending int
}

// DefaultLimits are the limits the app uses.
var DefaultLimits = Limits{
	Debounce:    2 * time.Second,
	MaxWait:     10 * time.Second,
	KeyCooldown: 30 * time.Second,
	RateWindow:  time.Minute,
	RateMax:     5,
	MaxPending:  16,
}

// overflowKey collects events once MaxPending keys are waiting.
const overflowKey = "\x00overflow"

func formatOverflow(count int, _, _ string) (string, string) {
	if count == 1 {
		return "mahpastes", "1 more background event"
	}
	return "mahpastes", strconv.Itoa(count) + " more background events"
}

type bucket struct {
	notice  Notice
	count   int
	firstAt time.Time
	lastAt  time.Time
	seq     int
}

// queue is the coalescing and rate-limiting state. It is not safe for
// concurrent use; Notifier guards it with a mutex. Time is passed in so
// tests control it.
type queue struct {
	limits   Limits
	buckets  map[string]*bucket
	lastSent map[string]time.Time
	sent     []time.Time // send times inside the rate window, oldest first
	seq      int
}

func newQueue(l Limits) *queue {
	return &queue{
		limits:   l,
		buckets:  make(map[string]*bucket),
		lastSent: make(map[string]time.Time),
	}
}

// add records one event.
func (q *queue) add(now time.Time, n Notice) {
	b, ok := q.buckets[n.Key]
	if !ok && q.limits.MaxPending > 0 && n.Key != overflowKey && q.pendingKeys() >= q.limits.MaxPending {
		n = Notice{Key: overflowKey, Format: formatOverflow}
		b, ok = q.buckets[overflowKey]
	}
	if !ok {
		q.seq++
		b = &bucket{notice: n, firstAt: now, seq: q.seq}
		q.buckets[n.Key] = b
	}
	b.count++
	b.lastAt = now
	if n.Detail != "" || b.notice.Detail == "" {
		b.notice.Detail = n.Detail
	}
	if n.Label != nil {
		b.notice.Label = n.Label
	}
}

func (q *queue) pendingKeys() int {
	n := len(q.buckets)
	if _, ok := q.buckets[overflowKey]; ok {
		n--
	}
	return n
}

// clear drops everything waiting (the window regained focus, or
// notifications were switched off).
func (q *queue) clear() {
	q.buckets = make(map[string]*bucket)
}

func (q *queue) empty() bool { return len(q.buckets) == 0 }

func (q *queue) pruneSent(now time.Time) {
	cut := 0
	for cut < len(q.sent) && !q.sent[cut].After(now.Add(-q.limits.RateWindow)) {
		cut++
	}
	q.sent = q.sent[cut:]
	for k, t := range q.lastSent {
		if now.Sub(t) >= q.limits.KeyCooldown {
			delete(q.lastSent, k)
		}
	}
}

// dueAt is when key's bucket may be sent, ignoring the overall rate cap.
func (q *queue) dueAt(key string, b *bucket) time.Time {
	due := b.lastAt.Add(q.limits.Debounce)
	if latest := b.firstAt.Add(q.limits.MaxWait); latest.Before(due) {
		due = latest
	}
	if last, ok := q.lastSent[key]; ok {
		if cool := last.Add(q.limits.KeyCooldown); cool.After(due) {
			due = cool
		}
	}
	return due
}

// rateFreeAt is when the overall cap next allows a send.
func (q *queue) rateFreeAt() time.Time {
	if q.limits.RateMax <= 0 || len(q.sent) < q.limits.RateMax {
		return time.Time{}
	}
	return q.sent[len(q.sent)-q.limits.RateMax].Add(q.limits.RateWindow)
}

// pop removes and returns the buckets that are due at now, oldest first,
// as many as the overall cap allows, and counts them as sent.
func (q *queue) pop(now time.Time) []*bucket {
	q.pruneSent(now)
	var due []*bucket
	for k, b := range q.buckets {
		if !q.dueAt(k, b).After(now) {
			due = append(due, b)
		}
	}
	sort.Slice(due, func(i, j int) bool { return due[i].seq < due[j].seq })
	var out []*bucket
	for _, b := range due {
		if free := q.rateFreeAt(); !free.IsZero() && free.After(now) {
			break
		}
		delete(q.buckets, b.notice.Key)
		q.lastSent[b.notice.Key] = now
		q.sent = append(q.sent, now)
		out = append(out, b)
	}
	return out
}

// next is when pop should run again; zero when nothing is waiting.
func (q *queue) next(now time.Time) time.Time {
	var at time.Time
	for k, b := range q.buckets {
		due := q.dueAt(k, b)
		if at.IsZero() || due.Before(at) {
			at = due
		}
	}
	if at.IsZero() {
		return at
	}
	q.pruneSent(now)
	if free := q.rateFreeAt(); free.After(at) {
		at = free
	}
	return at
}

// render builds the notification text for a popped bucket.
func render(b *bucket) (title, body string) {
	label := ""
	if b.notice.Label != nil {
		label = b.notice.Label()
	}
	return b.notice.Format(b.count, label, b.notice.Detail)
}
