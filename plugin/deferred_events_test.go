package plugin

import (
	"database/sql"
	"runtime"
	"testing"
	"time"

	lua "github.com/yuin/gopher-lua"
	_ "modernc.org/sqlite"
)

// newDeferralHarness loads a plugin that records the clip_id of every
// tag:added_to_clip it receives, and a Manager subscribed to deliver it.
func newDeferralHarness(t *testing.T) (*Manager, *Sandbox) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE plugins (id INTEGER PRIMARY KEY, error_count INTEGER NOT NULL DEFAULT 0, status TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO plugins (id) VALUES (1)`); err != nil {
		t.Fatal(err)
	}

	s := NewSandbox(&Manifest{Name: "recorder"}, 1)
	t.Cleanup(s.Close)
	if err := s.LoadSource(`
seen = {}
function on_tag_added_to_clip(data) table.insert(seen, data.clip_id) end`); err != nil {
		t.Fatal(err)
	}
	m := &Manager{
		db:               db,
		plugins:          map[int64]*Plugin{1: {ID: 1, Name: "recorder", Sandbox: s}},
		eventSubscribers: map[string][]int64{"tag:added_to_clip": {1}},
	}
	return m, s
}

// seenClipIDs reads the recorder's list once its handlers have run.
func seenClipIDs(s *Sandbox) []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []int64
	if tbl, ok := s.L.GetGlobal("seen").(*lua.LTable); ok {
		tbl.ForEach(func(_, v lua.LValue) {
			ids = append(ids, int64(v.(lua.LNumber)))
		})
	}
	return ids
}

// parkInHostCall puts s in the state EmitEvent defers for: a handler holds its
// mutex and is inside a host call. The returned func is the handler returning.
func parkInHostCall(s *Sandbox) (finish func()) {
	s.mu.Lock()
	exit := s.enterHostCall()
	return func() {
		exit()
		s.mu.Unlock()
	}
}

func waitForSeen(t *testing.T, s *Sandbox, want int) []int64 {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		s.deferMu.Lock()
		idle := !s.draining
		s.deferMu.Unlock()
		if idle {
			if ids := seenClipIDs(s); len(ids) >= want {
				return ids
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("deferred events not delivered: have %d, want %d", len(seenClipIDs(s)), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A handler that tags N clips emits N events back at its own plugin while it
// holds that plugin's sandbox. Each used to get its own goroutine, parked on
// the sandbox mutex with its payload until the handler returned, and the
// mutex woke them in no particular order. They are now one queue, drained in
// order by a single goroutine.
func TestDeferredEventsUseOneDrainerInOrder(t *testing.T) {
	m, s := newDeferralHarness(t)
	const n = 500

	before := runtime.NumGoroutine()
	finish := parkInHostCall(s)
	for i := 1; i <= n; i++ {
		m.EmitEvent("tag:added_to_clip", map[string]interface{}{"clip_id": int64(i), "tag_id": int64(1)})
	}
	// Only the drainer may have been added; it is parked on the mutex.
	if grew := runtime.NumGoroutine() - before; grew > 1 {
		finish()
		t.Fatalf("%d goroutines appeared for %d deferred events, want at most one drainer", grew, n)
	}
	finish()

	ids := waitForSeen(t, s, n)
	if len(ids) != n {
		t.Fatalf("delivered %d events, want %d", len(ids), n)
	}
	for i, id := range ids {
		if id != int64(i+1) {
			t.Fatalf("delivery %d carried clip %d, want %d — deferred events arrived out of order", i, id, i+1)
		}
	}
}

// A handler whose deliveries emit more events than they consume would grow
// the queue without limit; past maxDeferredEvents further events are dropped
// rather than held. The queue keeps working afterwards.
func TestDeferredEventQueueIsBounded(t *testing.T) {
	m, s := newDeferralHarness(t)

	const emitted = maxDeferredEvents + 25
	finish := parkInHostCall(s)
	for i := 1; i <= emitted; i++ {
		m.EmitEvent("tag:added_to_clip", map[string]interface{}{"clip_id": int64(i), "tag_id": int64(1)})
	}
	s.deferMu.Lock()
	queued, dropped := len(s.deferred), s.deferredDropped
	s.deferMu.Unlock()
	finish()
	// The drainer may already hold the first event, popped and waiting on
	// the mutex, which frees one slot.
	if queued != maxDeferredEvents || dropped < 24 || dropped > 25 {
		t.Fatalf("queue held %d events and dropped %d, want %d held and 24-25 dropped", queued, dropped, maxDeferredEvents)
	}

	delivered := emitted - dropped
	ids := waitForSeen(t, s, delivered)
	if len(ids) != delivered {
		t.Fatalf("delivered %d events, want %d (%d emitted, %d dropped)", len(ids), delivered, emitted, dropped)
	}
	for i, id := range ids {
		if id != int64(i+1) {
			t.Fatalf("delivery %d carried clip %d, want %d — the queue kept the wrong events", i, id, i+1)
		}
	}

	// Drained and reset: the next deferral is accepted and delivered.
	finish = parkInHostCall(s)
	m.EmitEvent("tag:added_to_clip", map[string]interface{}{"clip_id": int64(-1), "tag_id": int64(1)})
	finish()
	ids = waitForSeen(t, s, delivered+1)
	if ids[len(ids)-1] != -1 {
		t.Fatalf("event deferred after the queue drained was not delivered (last clip %d)", ids[len(ids)-1])
	}
}

// Once anything is queued, a later event must queue behind it. An event
// emitted after the handler left its host call used to be delivered
// synchronously, racing the drainer for the mutex, and landed after the first
// queued event but before the rest — a plugin that indexes clip tags from
// these events saw an add and a remove in the wrong order.
func TestEventsAfterADeferralQueueBehindIt(t *testing.T) {
	m, s := newDeferralHarness(t)

	for trial := 0; trial < 20; trial++ {
		finish := parkInHostCall(s)
		for i := 1; i <= 3; i++ {
			m.EmitEvent("tag:added_to_clip", map[string]interface{}{"clip_id": int64(trial*10 + i), "tag_id": int64(1)})
		}
		// Let the drainer pop the first event and park on the mutex, so the
		// mutex hands off strictly in arrival order once released.
		time.Sleep(2 * time.Millisecond)
		finish()
		m.EmitEvent("tag:added_to_clip", map[string]interface{}{"clip_id": int64(trial*10 + 4), "tag_id": int64(1)})

		ids := waitForSeen(t, s, (trial+1)*4)
		got := ids[trial*4:]
		for i, id := range got {
			if want := int64(trial*10 + i + 1); id != want {
				t.Fatalf("trial %d: delivery order %v, want %d..%d in order", trial, got, trial*10+1, trial*10+4)
			}
		}
	}
}

// Events still queued when the plugin is unloaded reach a closed sandbox,
// which runs nothing. They must not count as successful runs: each one reset
// the plugin's consecutive-error count, so a plugin auto-disabled for errors
// read error_count 0, and after a reload the old copy's drainer kept zeroing
// the new copy's count.
func TestDeliveryToClosedSandboxLeavesErrorCountAlone(t *testing.T) {
	m, s := newDeferralHarness(t)
	if _, err := m.db.Exec(`UPDATE plugins SET error_count = 2 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}

	// Unloaded while one of its handlers was still parked in a host call:
	// the events that call emits are queued for a sandbox already closed.
	s.Close()
	exit := s.enterHostCall()
	for i := 1; i <= 5; i++ {
		m.EmitEvent("tag:added_to_clip", map[string]interface{}{"clip_id": int64(i), "tag_id": int64(1)})
	}
	exit()

	deadline := time.Now().Add(5 * time.Second)
	for {
		s.deferMu.Lock()
		idle := !s.draining
		s.deferMu.Unlock()
		if idle {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("drainer never finished")
		}
		time.Sleep(5 * time.Millisecond)
	}
	var n int
	if err := m.db.QueryRow(`SELECT error_count FROM plugins WHERE id = 1`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("error_count = %d after deliveries to a closed sandbox, want it left at 2", n)
	}
}

// Reaching the error limit unloads the failing copy, and Close waits for any
// other run holding its sandbox — up to MaxUIActionTime for an async action.
// status 'error' must be written before that wait, as it used to be: written
// after, a user who re-enabled the plugin meanwhile got a running plugin the
// database calls errored, which then would not load at the next start.
func TestAutoDisableRecordsErrorBeforeWaitingOnClose(t *testing.T) {
	m, s := newDeferralHarness(t)
	m.scheduler = NewScheduler()
	if _, err := m.db.Exec(`UPDATE plugins SET error_count = 2, status = 'enabled' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}

	s.mu.Lock() // another run of the plugin is in progress
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.RecordFailureForTest(1, s)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		m.mu.RLock()
		_, loaded := m.plugins[1]
		m.mu.RUnlock()
		if !loaded {
			break
		}
		if time.Now().After(deadline) {
			s.mu.Unlock()
			t.Fatal("the failing copy was never unloaded")
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond) // the unload is now waiting in Close

	var status string
	err := m.db.QueryRow(`SELECT status FROM plugins WHERE id = 1`).Scan(&status)
	s.mu.Unlock()
	<-done
	if err != nil {
		t.Fatal(err)
	}
	if status != "error" {
		t.Fatalf("status %q while the unload waited on the running handler, want error already recorded", status)
	}
}
