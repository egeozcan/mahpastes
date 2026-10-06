//go:build linux && !bindings

package fpfuse

import (
	"context"
	"path/filepath"
	"runtime"
	"sync/atomic"

	"github.com/hanwen/go-fuse/v2/fuse"
	"golang.org/x/sys/unix"
)

// pump runs the syscalls that replay projection changes through the VFS, so
// the kernel raises the same inotify events a real local change would:
// mknod/mkdir for IN_CREATE, unlink/rmdir for IN_DELETE and utimes for
// IN_ATTRIB. (Kernel cache invalidation alone reaches no directory watcher.)
//
// The pump owns one locked OS thread. FUSE requests carry the calling
// thread's ID, so handlers accept a change only from exactly this thread;
// plugins, other goroutines of the app and other processes get EROFS.
// mknod is used rather than open(O_CREAT) so no IN_OPEN/IN_CLOSE_WRITE makes
// thumbnailers or indexers read the clip.
type pump struct {
	// dev is the mount's device number, set once the filesystem is mounted.
	dev  uint64
	tid  atomic.Int64
	jobs chan func()
	quit chan struct{}
}

func startPump() *pump {
	p := &pump{jobs: make(chan func()), quit: make(chan struct{})}
	ready := make(chan struct{})
	go func() {
		// Never unlocked: the thread exits with the goroutine, so no other
		// goroutine is ever scheduled on a thread the handlers trust.
		runtime.LockOSThread()
		p.tid.Store(int64(unix.Gettid()))
		close(ready)
		for {
			select {
			case job := <-p.jobs:
				job()
			case <-p.quit:
				p.tid.Store(0)
				return
			}
		}
	}()
	<-ready
	return p
}

func (p *pump) stop() { close(p.quit) }

// trusted reports whether a FUSE request came from the pump thread.
func (p *pump) trusted(ctx context.Context) bool {
	caller, ok := fuse.FromContext(ctx)
	tid := p.tid.Load()
	return ok && tid != 0 && int64(caller.Pid) == tid
}

// run executes fn on the pump thread and returns its result. The caller must
// not hold any lock a FUSE handler takes: the syscall re-enters this process
// through the kernel.
func (p *pump) run(ctx context.Context, fn func() error) error {
	result := make(chan error, 1)
	select {
	case p.jobs <- func() { result <- fn() }:
	case <-p.quit:
		return context.Canceled
	case <-ctx.Done():
		return ctx.Err()
	}
	return <-result
}

// at runs fn with a descriptor for the directory holding path, once that
// directory is confirmed to be on the mount. If the filesystem was unmounted
// from outside the app, the path now names the plain directory underneath,
// and a replay must not create entries there.
func (p *pump) at(path string, fn func(dirfd int, name string) error) error {
	fd, err := unix.Open(filepath.Dir(path), unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return err
	}
	if p.dev == 0 || st.Dev != p.dev {
		return unix.ENOTCONN
	}
	return fn(fd, filepath.Base(path))
}

func (p *pump) create(ctx context.Context, path string, folder bool) error {
	return p.run(ctx, func() error {
		return p.at(path, func(dirfd int, name string) error {
			if folder {
				return unix.Mkdirat(dirfd, name, 0o555)
			}
			return unix.Mknodat(dirfd, name, unix.S_IFREG|0o444, 0)
		})
	})
}

func (p *pump) remove(ctx context.Context, path string, folder bool) error {
	return p.run(ctx, func() error {
		return p.at(path, func(dirfd int, name string) error {
			if folder {
				return unix.Unlinkat(dirfd, name, unix.AT_REMOVEDIR)
			}
			return unix.Unlinkat(dirfd, name, 0)
		})
	})
}

// touch sends a utimes request for a changed file. The handler ignores the
// requested times and answers with the projection's current attributes. The
// kernel reports an mtime-only update as IN_MODIFY, which is what watchers
// expect for new content, and an update of both times as IN_ATTRIB.
func (p *pump) touch(ctx context.Context, path string, content bool) error {
	return p.run(ctx, func() error {
		times := []unix.Timespec{{Nsec: unix.UTIME_NOW}, {Nsec: unix.UTIME_NOW}}
		if content {
			times[0].Nsec = unix.UTIME_OMIT
		}
		return p.at(path, func(dirfd int, name string) error {
			return unix.UtimesNanoAt(dirfd, name, times, unix.AT_SYMLINK_NOFOLLOW)
		})
	})
}
