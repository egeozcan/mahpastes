package app

import (
	"sync"
	"time"
)

// openFileID names a file the way a descriptor does: device and inode.
type openFileID struct{ dev, ino uint64 }

// writeOpenSnapshots answers "is this file open for writing?" from a list of
// every file some process has open for writing, taken by one walk of the
// process table. The walk costs the same whether it looks for one file or
// lists them all, and each walk in progress pins an OS thread (it is a cgo
// call), so the files settling together after an unzip into a watched folder
// must not each run their own: a few thousand at once took seconds on as many
// threads, and past Go's thread limit the process aborts. Here at most one
// walk runs at a time and every caller waiting for it shares its result.
//
// A caller says how old a list it accepts. The age is measured from the start
// of the walk, so a list never claims to cover an open made after it began;
// maxAge 0 means a walk begun after the call, which is what a check that
// permits a deletion needs. Callers that arrive while a walk runs wait for it
// and, if it began too early for them, share the next one: N callers at once
// cost two walks, not N.
type writeOpenSnapshots struct {
	list func() map[openFileID]struct{}

	mu     sync.Mutex
	cur    *writeOpenSnapshot // the last finished walk
	flight *writeOpenSnapshot // the walk in progress, if any
}

type writeOpenSnapshot struct {
	started time.Time
	done    chan struct{}
	files   map[openFileID]struct{}
}

func newWriteOpenSnapshots(list func() map[openFileID]struct{}) *writeOpenSnapshots {
	return &writeOpenSnapshots{list: list}
}

// openForWriting reports whether id was open for writing in a walk that began
// no more than maxAge before this call.
func (s *writeOpenSnapshots) openForWriting(id openFileID, maxAge time.Duration) bool {
	if maxAge < 0 {
		maxAge = 0
	}
	notBefore := time.Now().Add(-maxAge)
	s.mu.Lock()
	for s.cur == nil || s.cur.started.Before(notBefore) {
		f := s.flight
		if f == nil {
			f = &writeOpenSnapshot{started: time.Now(), done: make(chan struct{})}
			s.flight = f
			s.mu.Unlock()
			f.files = s.list() // the slow part, outside the mutex
			s.mu.Lock()
			s.cur, s.flight = f, nil
			close(f.done)
			continue
		}
		s.mu.Unlock()
		<-f.done
		s.mu.Lock()
	}
	_, open := s.cur.files[id]
	s.mu.Unlock()
	return open
}
