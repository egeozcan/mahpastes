package osnotify

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go-clipboard/internal/bridgeiface"
)

type sent struct{ id, title, body string }

type fakeSender struct {
	mu        sync.Mutex
	initErr   error
	sendErr   error
	initCalls int
	cleanups  int
	sent      []sent
	block     chan struct{} // when set, InitNotifications waits on it
}

func (f *fakeSender) InitNotifications() error {
	f.mu.Lock()
	f.initCalls++
	err := f.initErr
	block := f.block
	f.mu.Unlock()
	if block != nil {
		<-block
	}
	return err
}

func (f *fakeSender) SendNotification(id, title, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return f.sendErr
	}
	f.sent = append(f.sent, sent{id, title, body})
	return nil
}

func (f *fakeSender) CleanupNotifications() {
	f.mu.Lock()
	f.cleanups++
	f.mu.Unlock()
}

func (f *fakeSender) snapshot() (inits int, out []sent, cleanups int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.initCalls, append([]sent(nil), f.sent...), f.cleanups
}

var fastLimits = Limits{
	Debounce:    20 * time.Millisecond,
	MaxWait:     200 * time.Millisecond,
	KeyCooldown: 50 * time.Millisecond,
	RateWindow:  time.Second,
	RateMax:     10,
	MaxPending:  8,
}

func newTestNotifier(t *testing.T, s *fakeSender, enabled *atomic.Bool) *Notifier {
	t.Helper()
	n := New(Options{Sender: s, Enabled: enabled.Load, Limits: fastLimits})
	n.Start()
	t.Cleanup(func() { n.Stop(2 * time.Second) })
	return n
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestNotifierSendsCoalescedWhenUnfocused(t *testing.T) {
	s := &fakeSender{}
	var on atomic.Bool
	on.Store(true)
	n := newTestNotifier(t, s, &on)
	n.SetFocused(false)
	for i := 0; i < 5; i++ {
		n.Post(notice("import", "f"))
	}
	waitFor(t, "a notification", func() bool { _, out, _ := s.snapshot(); return len(out) == 1 })
	inits, out, _ := s.snapshot()
	if inits != 1 {
		t.Errorf("init calls = %d, want 1 (lazily, on the first notification)", inits)
	}
	if out[0].body != "5:f" || !strings.HasPrefix(out[0].id, "mahpastes-") {
		t.Errorf("sent %+v, want one combined notification", out[0])
	}
	// No second notification arrives later.
	time.Sleep(100 * time.Millisecond)
	if _, out, _ := s.snapshot(); len(out) != 1 {
		t.Errorf("sent %d notifications, want 1", len(out))
	}
}

func TestNotifierIgnoresEventsWhileFocused(t *testing.T) {
	s := &fakeSender{}
	var on atomic.Bool
	on.Store(true)
	n := newTestNotifier(t, s, &on)
	// Focused by default until the frontend reports otherwise.
	if n.Accepting() {
		t.Fatal("accepting before any focus report")
	}
	n.Post(notice("k", ""))
	// Losing focus and regaining it before the debounce ends drops the event.
	n.SetFocused(false)
	n.Post(notice("k", ""))
	n.SetFocused(true)
	time.Sleep(100 * time.Millisecond)
	inits, out, _ := s.snapshot()
	if len(out) != 0 || inits != 0 {
		t.Fatalf("sent %v (inits %d) for events seen in the window", out, inits)
	}
}

func TestNotifierRespectsDisabledSetting(t *testing.T) {
	s := &fakeSender{}
	var on atomic.Bool
	n := newTestNotifier(t, s, &on)
	n.SetFocused(false)
	n.Post(notice("k", ""))
	time.Sleep(100 * time.Millisecond)
	inits, out, _ := s.snapshot()
	if len(out) != 0 || inits != 0 {
		t.Fatalf("disabled: sent %v, init calls %d; want nothing, and no backend init", out, inits)
	}
	// Switching on later takes effect without a restart.
	on.Store(true)
	n.Post(notice("k", ""))
	waitFor(t, "a notification once enabled", func() bool { _, out, _ := s.snapshot(); return len(out) == 1 })
}

func TestNotifierInitFailureSticksUntilActivate(t *testing.T) {
	s := &fakeSender{initErr: errors.New("notifications require a valid bundle identifier")}
	var on atomic.Bool
	on.Store(true)
	n := newTestNotifier(t, s, &on)
	n.SetFocused(false)
	n.Post(notice("a", ""))
	waitFor(t, "the failed init", func() bool { return n.Status().State == StateFailed })
	if st := n.Status(); !strings.Contains(st.Error, "bundle identifier") {
		t.Fatalf("status error = %q", st.Error)
	}
	n.Post(notice("b", ""))
	time.Sleep(100 * time.Millisecond)
	if inits, out, _ := s.snapshot(); inits != 1 || len(out) != 0 {
		t.Fatalf("after a failure: %d init calls, %d sent; want 1 and 0", inits, len(out))
	}

	s.mu.Lock()
	s.initErr = nil
	s.mu.Unlock()
	if err := n.Activate(time.Second); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if st := n.Status(); st.State != StateReady || st.Error != "" {
		t.Fatalf("status after activate = %+v", st)
	}
	if inits, _, _ := s.snapshot(); inits != 2 {
		t.Fatalf("init calls = %d, want a retry on activate", inits)
	}
	// Activating again does not initialise twice.
	if err := n.Activate(time.Second); err != nil {
		t.Fatal(err)
	}
	if inits, _, _ := s.snapshot(); inits != 2 {
		t.Fatalf("init calls = %d after a second activate, want 2", inits)
	}
}

func TestNotifierRepeatedSendFailuresMarkFailed(t *testing.T) {
	s := &fakeSender{sendErr: errors.New("org.freedesktop.Notifications was not provided")}
	var on atomic.Bool
	on.Store(true)
	n := newTestNotifier(t, s, &on)
	n.SetFocused(false)
	for i := 0; i < maxSendFailures; i++ {
		if err := n.SendTest(time.Second); err == nil {
			t.Fatal("send test: want the backend error")
		}
	}
	if st := n.Status(); st.State != StateFailed {
		t.Fatalf("state = %s after %d failed sends, want failed", st.State, maxSendFailures)
	}
}

func TestNotifierSendTestIgnoresFocus(t *testing.T) {
	s := &fakeSender{}
	var on atomic.Bool
	n := newTestNotifier(t, s, &on)
	if err := n.SendTest(time.Second); err != nil {
		t.Fatal(err)
	}
	if _, out, _ := s.snapshot(); len(out) != 1 || out[0].title != "mahpastes" {
		t.Fatalf("sent %v, want the test notification", out)
	}
}

func TestNotifierActivateTimesOutWhileInitBlocks(t *testing.T) {
	block := make(chan struct{})
	s := &fakeSender{block: block}
	var on atomic.Bool
	n := newTestNotifier(t, s, &on)
	if err := n.Activate(50 * time.Millisecond); !errors.Is(err, errBusy) {
		t.Fatalf("activate = %v, want a timeout while the OS prompt is open", err)
	}
	if st := n.Status(); st.State != StatePending {
		t.Fatalf("state = %s, want pending", st.State)
	}
	// Events posted meanwhile do not block the caller.
	n.SetFocused(false)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			n.Post(notice("k", ""))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Post blocked behind a pending init")
	}
	close(block)
	waitFor(t, "ready", func() bool { return n.Status().State == StateReady })
}

func TestNotifierStopCleansUpOnlyAfterInit(t *testing.T) {
	s := &fakeSender{}
	var on atomic.Bool
	n := New(Options{Sender: s, Enabled: on.Load, Limits: fastLimits})
	n.Start()
	n.Stop(time.Second)
	if _, _, c := s.snapshot(); c != 0 {
		t.Fatalf("cleanup called %d times without an init", c)
	}

	s2 := &fakeSender{}
	n2 := New(Options{Sender: s2, Enabled: on.Load, Limits: fastLimits})
	n2.Start()
	if err := n2.Activate(time.Second); err != nil {
		t.Fatal(err)
	}
	n2.Stop(time.Second)
	n2.Stop(time.Second) // idempotent
	if _, _, c := s2.snapshot(); c != 1 {
		t.Fatalf("cleanup called %d times, want 1", c)
	}
	n2.Post(notice("k", "")) // after Stop: ignored, no panic
	if err := n2.SendTest(time.Second); !errors.Is(err, errNotRunning) {
		t.Fatalf("SendTest after Stop = %v", err)
	}
}

func TestNotifierWithoutStartIsInert(t *testing.T) {
	n := New(Options{Sender: &fakeSender{}})
	n.SetFocused(false)
	if n.Accepting() {
		t.Fatal("accepting before Start")
	}
	n.Post(notice("k", ""))
	if err := n.Activate(10 * time.Millisecond); !errors.Is(err, errNotRunning) {
		t.Fatalf("Activate = %v", err)
	}
	n.Stop(time.Second)
}

type recordingBridge struct {
	mu     sync.Mutex
	events []string
	subs   []string
}

func (r *recordingBridge) Emit(name string, data ...any) {
	r.mu.Lock()
	r.events = append(r.events, name)
	r.mu.Unlock()
}

func (r *recordingBridge) On(name string, cb func(data ...any)) {
	r.mu.Lock()
	r.subs = append(r.subs, name)
	r.mu.Unlock()
}

func TestWrapForwardsEverythingAndTapsBackgroundEvents(t *testing.T) {
	s := &fakeSender{}
	var on atomic.Bool
	on.Store(true)
	n := newTestNotifier(t, s, &on)
	inner := &recordingBridge{}
	var classified atomic.Int32
	b := Wrap(inner, n, func(name string, data ...any) (Notice, bool) {
		classified.Add(1)
		if name != "watch:import" {
			return Notice{}, false
		}
		return notice("import", "x"), true
	})

	// Focused: forwarded, not even classified.
	b.Emit("watch:import", 1)
	if classified.Load() != 0 {
		t.Fatal("classified an event while the window was focused")
	}
	n.SetFocused(false)
	b.Emit("watch:import", 1)
	b.Emit("clip:created", 1)
	b.On("plugin:modal:acked", func(...any) {})

	inner.mu.Lock()
	events, subs := append([]string(nil), inner.events...), append([]string(nil), inner.subs...)
	inner.mu.Unlock()
	if strings.Join(events, ",") != "watch:import,watch:import,clip:created" {
		t.Fatalf("forwarded %v", events)
	}
	if len(subs) != 1 || subs[0] != "plugin:modal:acked" {
		t.Fatalf("subscriptions %v", subs)
	}
	waitFor(t, "the tapped notification", func() bool { _, out, _ := s.snapshot(); return len(out) == 1 })

	// Without a notifier the inner bridge is returned as is.
	if got := Wrap(inner, nil, nil); got != bridgeiface.Bridge(inner) {
		t.Fatal("Wrap without a notifier changed the bridge")
	}
}
