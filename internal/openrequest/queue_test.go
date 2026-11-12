package openrequest

import (
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueueBuffersUntilTaken(t *testing.T) {
	q := NewQueue(time.Millisecond)
	q.Submit(Request{Files: []string{"/a", "/b"}, Activate: true})
	q.Submit(Request{Files: []string{"/b", "/c"}, URLs: []string{"mahpastes://show"}})

	got := q.Take()
	want := Request{Files: []string{"/a", "/b", "/c"}, URLs: []string{"mahpastes://show"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Take() = %#v, want %#v", got, want)
	}
	if again := q.Take(); !again.Empty() {
		t.Fatalf("second Take() = %#v, want empty", again)
	}
	// A file taken once may be opened again later.
	q.Submit(Request{Files: []string{"/a"}})
	if got := q.Take(); !reflect.DeepEqual(got.Files, []string{"/a"}) {
		t.Fatalf("resubmitted file lost: %#v", got)
	}
}

func TestQueueNotifiesOnceAfterBurst(t *testing.T) {
	q := NewQueue(30 * time.Millisecond)
	var notified atomic.Int32
	q.Attach(nil, func() { notified.Add(1) })

	for i := 0; i < 5; i++ {
		q.Submit(Request{Files: []string{"/f" + string(rune('0'+i))}})
		time.Sleep(5 * time.Millisecond)
	}
	waitFor(t, func() bool { return notified.Load() == 1 })
	time.Sleep(60 * time.Millisecond)
	if n := notified.Load(); n != 1 {
		t.Fatalf("burst produced %d notifications, want 1", n)
	}
	if got := q.Take(); len(got.Files) != 5 {
		t.Fatalf("Take() files = %v, want 5", got.Files)
	}
}

func TestQueueAnnouncesBacklogOnAttach(t *testing.T) {
	q := NewQueue(time.Millisecond)
	q.Submit(Request{Files: []string{"/cold-launch.png"}, Activate: true})

	var notified atomic.Int32
	q.Attach(nil, func() { notified.Add(1) })
	waitFor(t, func() bool { return notified.Load() == 1 })
}

func TestQueueActivatesImmediatelyOnlyWhenAsked(t *testing.T) {
	q := NewQueue(time.Hour)
	var activated atomic.Int32
	q.Attach(func() { activated.Add(1) }, func() {})

	q.Submit(Request{Activate: true}) // plain relaunch
	q.Submit(Request{Files: []string{"/x"}})
	q.Submit(Request{URLs: []string{"mahpastes://show"}, Activate: true})
	if n := activated.Load(); n != 2 {
		t.Fatalf("activated %d times, want 2", n)
	}
}

func TestQueueSkipsNotifyWhenTakenFirst(t *testing.T) {
	q := NewQueue(20 * time.Millisecond)
	var notified atomic.Int32
	q.Attach(nil, func() { notified.Add(1) })
	q.Submit(Request{Files: []string{"/x"}})
	q.Take()
	time.Sleep(50 * time.Millisecond)
	if n := notified.Load(); n != 0 {
		t.Fatalf("notified %d times for an already drained queue", n)
	}
}

func TestQueueCaps(t *testing.T) {
	q := NewQueue(time.Hour)
	files := make([]string, maxQueuedFiles+5)
	for i := range files {
		files[i] = "/f/" + time.Duration(i).String()
	}
	urls := make([]string, maxQueuedURLs+5)
	for i := range urls {
		urls[i] = "mahpastes://show"
	}
	q.Submit(Request{Files: files, URLs: urls})
	got := q.Take()
	if len(got.Files) != maxQueuedFiles || len(got.URLs) != maxQueuedURLs {
		t.Fatalf("caps not applied: %d files, %d urls", len(got.Files), len(got.URLs))
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within 2s")
		}
		time.Sleep(2 * time.Millisecond)
	}
}
