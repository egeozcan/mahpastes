package osnotify

import (
	"fmt"
	"testing"
	"time"
)

var testLimits = Limits{
	Debounce:    2 * time.Second,
	MaxWait:     10 * time.Second,
	KeyCooldown: 30 * time.Second,
	RateWindow:  time.Minute,
	RateMax:     3,
	MaxPending:  4,
}

func countFormat(count int, label, detail string) (string, string) {
	return label, fmt.Sprintf("%d:%s", count, detail)
}

func notice(key, detail string) Notice {
	return Notice{Key: key, Detail: detail, Format: countFormat}
}

func bodies(bs []*bucket) []string {
	var out []string
	for _, b := range bs {
		_, body := render(b)
		out = append(out, b.notice.Key+"="+body)
	}
	return out
}

func TestQueueCoalescesABurst(t *testing.T) {
	q := newQueue(testLimits)
	t0 := time.Unix(1000, 0)
	for i := 0; i < 12; i++ {
		q.add(t0.Add(time.Duration(i)*100*time.Millisecond), notice("import", fmt.Sprintf("f%d", i)))
	}
	last := t0.Add(1100 * time.Millisecond)
	if got := q.pop(last.Add(time.Second)); len(got) != 0 {
		t.Fatalf("sent during the debounce: %v", bodies(got))
	}
	if at := q.next(last); !at.Equal(last.Add(2 * time.Second)) {
		t.Fatalf("next = %v, want debounce end %v", at, last.Add(2*time.Second))
	}
	got := bodies(q.pop(last.Add(2 * time.Second)))
	if len(got) != 1 || got[0] != "import=12:f11" {
		t.Fatalf("got %v, want one notification for 12 events with the latest detail", got)
	}
	if !q.empty() {
		t.Fatal("queue not empty after sending")
	}
}

func TestQueueMaxWaitBoundsASteadyStream(t *testing.T) {
	q := newQueue(testLimits)
	t0 := time.Unix(1000, 0)
	// One event per second never leaves a 2s quiet gap.
	var sent []string
	for i := 0; i <= 10; i++ {
		now := t0.Add(time.Duration(i) * time.Second)
		q.add(now, notice("k", ""))
		sent = append(sent, bodies(q.pop(now))...)
	}
	if len(sent) != 1 || sent[0] != "k=11:" {
		t.Fatalf("got %v, want one notification at the 10s max wait", sent)
	}
}

func TestQueueKeyCooldown(t *testing.T) {
	q := newQueue(testLimits)
	t0 := time.Unix(1000, 0)
	q.add(t0, notice("k", "a"))
	if got := q.pop(t0.Add(2 * time.Second)); len(got) != 1 {
		t.Fatalf("first: got %d, want 1", len(got))
	}
	q.add(t0.Add(3*time.Second), notice("k", "b"))
	q.add(t0.Add(4*time.Second), notice("k", "c"))
	if got := q.pop(t0.Add(20 * time.Second)); len(got) != 0 {
		t.Fatalf("sent inside the per-key cooldown: %v", bodies(got))
	}
	if at := q.next(t0.Add(20 * time.Second)); !at.Equal(t0.Add(32 * time.Second)) {
		t.Fatalf("next = %v, want cooldown end", at)
	}
	got := bodies(q.pop(t0.Add(32 * time.Second)))
	if len(got) != 1 || got[0] != "k=2:c" {
		t.Fatalf("got %v, want the two held events combined", got)
	}
}

func TestQueueGlobalRateCap(t *testing.T) {
	q := newQueue(testLimits)
	t0 := time.Unix(1000, 0)
	for i := 0; i < 4; i++ {
		q.add(t0, notice(fmt.Sprintf("k%d", i), ""))
	}
	got := q.pop(t0.Add(2 * time.Second))
	if len(got) != 3 {
		t.Fatalf("sent %d, want the cap of 3", len(got))
	}
	// Oldest first: k3 was added last and waits.
	if got[0].notice.Key != "k0" || got[2].notice.Key != "k2" {
		t.Fatalf("order = %v", bodies(got))
	}
	if at := q.next(t0.Add(3 * time.Second)); !at.Equal(t0.Add(62 * time.Second)) {
		t.Fatalf("next = %v, want when the window frees a slot", at)
	}
	if got := q.pop(t0.Add(61 * time.Second)); len(got) != 0 {
		t.Fatalf("sent over the cap: %v", bodies(got))
	}
	if got := q.pop(t0.Add(62 * time.Second)); len(got) != 1 || got[0].notice.Key != "k3" {
		t.Fatalf("got %v, want k3 once a slot frees", bodies(got))
	}
}

func TestQueueFoldsExtraKeysIntoOverflow(t *testing.T) {
	q := newQueue(testLimits)
	t0 := time.Unix(1000, 0)
	for i := 0; i < 7; i++ {
		q.add(t0, notice(fmt.Sprintf("k%d", i), ""))
	}
	if n := len(q.buckets); n != 5 {
		t.Fatalf("buckets = %d, want 4 keys plus overflow", n)
	}
	b := q.buckets[overflowKey]
	if b == nil || b.count != 3 {
		t.Fatalf("overflow = %+v, want 3 folded events", b)
	}
	if _, body := render(b); body != "3 more background events" {
		t.Fatalf("overflow body = %q", body)
	}
	// An existing key still collects its own events.
	q.add(t0, notice("k0", "x"))
	if q.buckets["k0"].count != 2 {
		t.Fatal("existing key was folded into overflow")
	}
}

func TestQueueClear(t *testing.T) {
	q := newQueue(testLimits)
	q.add(time.Unix(1000, 0), notice("k", ""))
	q.clear()
	if !q.empty() || !q.next(time.Unix(1000, 0)).IsZero() {
		t.Fatal("clear left work behind")
	}
}

func TestQueueResolvesLabelAtSendTime(t *testing.T) {
	q := newQueue(testLimits)
	t0 := time.Unix(1000, 0)
	calls := 0
	n := notice("k", "d")
	n.Label = func() string { calls++; return "Plugin X" }
	q.add(t0, n)
	q.add(t0, n)
	if calls != 0 {
		t.Fatal("label resolved while recording")
	}
	got := q.pop(t0.Add(2 * time.Second))
	title, _ := render(got[0])
	if title != "Plugin X" || calls != 1 {
		t.Fatalf("title %q after %d lookups, want one lookup", title, calls)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("héllo wörld", 5); got != "héll…" {
		t.Fatalf("truncate = %q", got)
	}
	if got := truncate("short", 10); got != "short" {
		t.Fatalf("truncate = %q", got)
	}
}
