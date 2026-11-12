package osnotify

import "go-clipboard/internal/bridgeiface"

// Classifier maps a frontend event to a notification. ok is false for events
// that should not notify. It runs on the emitting goroutine, so it must not
// block or do lookups (put those in Notice.Label).
type Classifier func(name string, data ...any) (notice Notice, ok bool)

// tappedBridge forwards every event unchanged and posts the ones the
// classifier recognises to the notifier.
type tappedBridge struct {
	bridgeiface.Bridge
	n        *Notifier
	classify Classifier
}

// Wrap returns a bridge that behaves exactly like inner and also feeds
// background events to n. Handing it to the core reaches every emitter
// (watcher, share manager, plugin manager, REST handlers) without changing
// any of them.
func Wrap(inner bridgeiface.Bridge, n *Notifier, classify Classifier) bridgeiface.Bridge {
	if inner == nil {
		inner = bridgeiface.NoOp{}
	}
	if n == nil || classify == nil {
		return inner
	}
	return &tappedBridge{Bridge: inner, n: n, classify: classify}
}

func (t *tappedBridge) Emit(name string, data ...any) {
	t.Bridge.Emit(name, data...)
	if !t.n.Accepting() {
		return
	}
	if notice, ok := t.classify(name, data...); ok {
		t.n.Post(notice)
	}
}
