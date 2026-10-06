//go:build linux && !bindings

package fpfuse

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"golang.org/x/sys/unix"

	"go-clipboard/internal/fileprovider"
)

// Kernel caches of names and attributes expire quickly so app-side edits show
// up without change notifications. Directory listings use the same window.
const cacheWindow = time.Second

func Supported() bool { return true }

// Available reports why the current system cannot mount, if it cannot. The
// mount needs the setuid fusermount helper and the FUSE device.
func Available() error {
	if fusermount() == "" {
		return errors.New("install fuse3 (it provides fusermount3) to browse clips in the file manager")
	}
	if _, err := os.Stat("/dev/fuse"); err != nil {
		return errors.New("/dev/fuse is unavailable; FUSE is not supported in this environment")
	}
	return nil
}

func fusermount() string {
	for _, name := range []string{"fusermount3", "fusermount"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

type Mount struct {
	dir     string
	created bool
	root    *dirNode
	server  *fuse.Server
	pump    *pump
	cancel  context.CancelFunc
	done    <-chan struct{}
	gone    chan struct{}
	once    sync.Once
}

// Start mounts a read-only view of the projection at dir and keeps the
// projection current until Close. A mount left behind by a crashed process is
// detached first. An existing non-empty directory that is not a mount is
// refused rather than hidden underneath the filesystem.
//
// The kernel mount is not flagged "ro". Directory change events only reach
// inotify watchers (file managers, file dialogs) for operations that pass
// through the VFS, so the app replays each projection change as a mknod,
// unlink or utimes from one dedicated thread (see pump). Every handler
// rejects changes from any other thread with EROFS, and nothing in this
// package can write to the database: the projection stays read-only by
// construction.
func Start(ctx context.Context, store *fileprovider.Store, dir string) (*Mount, error) {
	if err := Available(); err != nil {
		return nil, err
	}
	// mountinfo lists absolute paths, so Alive needs one to recognise the
	// mount.
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	created, err := prepare(dir)
	if err != nil {
		return nil, err
	}
	p := startPump()
	root := &dirNode{fsys: &fsys{store: store, pump: p, mounted: time.Now()}, id: "root"}
	timeout := cacheWindow
	server, err := fs.Mount(dir, root, &fs.Options{
		EntryTimeout:    &timeout,
		AttrTimeout:     &timeout,
		NegativeTimeout: &timeout,
		UID:             uint32(os.Getuid()),
		GID:             uint32(os.Getgid()),
		MountOptions: fuse.MountOptions{
			FsName:        "mahpastes",
			Name:          "mahpastes",
			Options:       []string{"noexec", "nosuid", "nodev"},
			MaxBackground: 4,
			// The kernel otherwise probes for xattrs on every access.
			DisableXAttrs: true,
		},
	})
	if err != nil {
		p.stop()
		if created {
			_ = os.Remove(dir)
		}
		return nil, fmt.Errorf("mount %s: %w", dir, err)
	}
	var st unix.Stat_t
	if err := unix.Stat(dir, &st); err != nil {
		if server.Unmount() != nil {
			lazyUnmount(dir)
		}
		p.stop()
		if created {
			_ = os.Remove(dir)
		}
		return nil, fmt.Errorf("mount %s: %w", dir, err)
	}
	p.dev = st.Dev
	workerCtx, cancel := context.WithCancel(ctx)
	m := &Mount{dir: dir, created: created, root: root, server: server, pump: p, cancel: cancel, gone: make(chan struct{})}
	go func() {
		// Wait returns once the kernel drops the mount, including an unmount
		// done outside the app (fusermount3 -u, or a desktop "eject").
		server.Wait()
		close(m.gone)
	}()
	m.done = fileprovider.Watch(workerCtx, store, func() error {
		if !m.Alive() {
			return nil
		}
		return m.root.refresh(workerCtx, dir)
	})
	return m, nil
}

func (m *Mount) Dir() string { return m.dir }

// Created reports whether Start created the mount directory.
func (m *Mount) Created() bool { return m.created }

// Alive reports whether the filesystem is still mounted at its directory.
// A lazy unmount (fusermount3 -u -z) detaches the path at once, but the
// server keeps running until the last open file inside it is closed.
func (m *Mount) Alive() bool {
	select {
	case <-m.gone:
		return false
	default:
		return mounted(m.dir)
	}
}

// Close unmounts. A busy mount (a shell or file manager inside it) is detached
// lazily so the app can still quit; the kernel finishes when it is released.
// The projection worker stops first, so no replayed change is in flight
// while the filesystem goes away.
func (m *Mount) Close() {
	m.once.Do(func() {
		m.cancel()
		<-m.done
		// Once the server has stopped the kernel has already dropped the
		// filesystem. A lazily detached one still answers open files and is
		// released when they close; unmounting it again fails harmlessly.
		if m.serving() {
			if err := m.server.Unmount(); err != nil {
				lazyUnmount(m.dir)
			}
		}
		m.pump.stop()
	})
}

func (m *Mount) serving() bool {
	select {
	case <-m.gone:
		return false
	default:
		return true
	}
}

func lazyUnmount(dir string) {
	if helper := fusermount(); helper != "" {
		_ = exec.Command(helper, "-u", "-z", dir).Run()
	}
}

// prepare readies dir as a mount point and reports whether it created it.
func prepare(dir string) (bool, error) {
	_, err := os.Stat(dir)
	if errors.Is(err, syscall.ENOTCONN) {
		// "Transport endpoint is not connected": the previous app instance died
		// without unmounting. Nothing else can be serving it, given the app's
		// single-instance lock on the data directory.
		lazyUnmount(dir)
		_, err = os.Stat(dir)
	}
	if errors.Is(err, os.ErrNotExist) {
		return true, os.MkdirAll(dir, 0o700)
	}
	if err != nil {
		return false, err
	}
	if mounted(dir) {
		// A dead server's mount fails the Stat above with ENOTCONN, so this
		// one is served by a live process: another instance with its own data
		// directory but the same mount location.
		return false, fmt.Errorf("%s is already in use by another Mahpastes instance; choose another location", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	if len(entries) > 0 {
		return false, fmt.Errorf("%s is not empty; move its contents or choose another location", dir)
	}
	return false, nil
}

// mounted reports whether a Mahpastes FUSE filesystem is mounted at dir.
func mounted(dir string) bool {
	// mountinfo lists canonical paths; /home is a symlink on some distros.
	// Only the parent is resolved: resolving the mount root itself would
	// send a request to the filesystem being checked.
	if real, err := filepath.EvalSymlinks(filepath.Dir(dir)); err == nil {
		dir = filepath.Join(real, filepath.Base(dir))
	}
	b, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(b), "\n") {
		// Fields after the "-" separator: filesystem type, source, options.
		pre, post, ok := strings.Cut(line, " - ")
		if !ok {
			continue
		}
		fields, tail := strings.Fields(pre), strings.Fields(post)
		if len(fields) > 4 && len(tail) > 1 && unescapeMountPath(fields[4]) == dir && tail[0] == "fuse.mahpastes" && tail[1] == "mahpastes" {
			return true
		}
	}
	return false
}

// mountinfo escapes space, tab, newline and backslash as octal.
func unescapeMountPath(s string) string {
	r := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return r.Replace(s)
}

// ino derives a stable inode number from the projection identifier, so the
// kernel and go-fuse reuse one inode for an item across lookups.
func ino(id string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	return h.Sum64() | 1
}

// Linux has no hidden-file flag; a leading dot is the file manager convention.
func displayName(i fileprovider.Item) string {
	if i.Hidden {
		return "." + i.Name
	}
	return i.Name
}

func errno(err error) syscall.Errno {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, fileprovider.ErrNoSuchItem):
		return syscall.ENOENT
	case errors.Is(err, fileprovider.ErrVersion):
		return syscall.ESTALE
	case errors.Is(err, context.Canceled):
		return syscall.EINTR
	default:
		return syscall.EIO
	}
}

func parseTime(s string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02T15:04:05.000Z"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func fillAttr(out *fuse.Attr, i fileprovider.Item) {
	out.Ino = ino(i.ID)
	if i.Folder {
		out.Mode = syscall.S_IFDIR | 0o555
		out.Nlink = 2
	} else {
		out.Mode = syscall.S_IFREG | 0o444
		out.Nlink = 1
		out.Size = uint64(i.Size)
		out.Blocks = (out.Size + 511) / 512
	}
	// The projection's Modified starts at enrollment time for every clip.
	// Content that was never rewritten since then is as old as the paste, so
	// file managers sort by when the clip arrived, not when access was enabled.
	changed := parseTime(i.Modified)
	created := parseTime(i.Created)
	content := changed
	if i.ContentVersion == "1" && !created.IsZero() || content.IsZero() {
		content = created
	}
	if changed.IsZero() {
		changed = content
	}
	if !content.IsZero() {
		out.SetTimes(&content, &content, &changed)
	}
}
