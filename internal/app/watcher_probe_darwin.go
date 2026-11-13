//go:build darwin

package app

import (
	"sync"
	"syscall"
)

// evtonlyProbe holds an event-only descriptor on a watched directory, the
// kind fsnotify's kqueue backend holds. It does not keep the volume busy, and
// an unmount revokes it: fstat then fails, and keeps failing after a remount.
type evtonlyProbe struct {
	mu sync.Mutex
	fd int // -1 once closed
}

func openPlatformDirProbe(path string) dirProbe {
	fd, err := syscall.Open(path, syscall.O_EVTONLY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil
	}
	return &evtonlyProbe{fd: fd}
}

func (p *evtonlyProbe) alive() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fd < 0 {
		return false
	}
	var st syscall.Stat_t
	for {
		err := syscall.Fstat(p.fd, &st)
		if err != syscall.EINTR {
			return err == nil
		}
	}
}

func (p *evtonlyProbe) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fd >= 0 {
		syscall.Close(p.fd)
		p.fd = -1
	}
}
