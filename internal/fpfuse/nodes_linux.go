//go:build linux && !bindings

package fpfuse

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"

	"go-clipboard/internal/fileprovider"
)

// fsys is shared by every node of one mount.
type fsys struct {
	store *fileprovider.Store
	pump  *pump
	// mounted is the time folders report until their listing first changes.
	mounted time.Time
}

// pendingOp is a projection change the pump is about to replay. Only the
// pump thread may complete it, and only for this exact name.
type pendingOp struct {
	create bool
	item   fileprovider.Item
}

type dirNode struct {
	fs.Inode
	fsys *fsys
	id   string

	mu sync.Mutex
	// items is the listing the kernel has been shown. Once loaded only
	// refresh replaces it, so it always differs from the projection by exactly
	// the changes still to be announced.
	items   []fileprovider.Item
	pending map[string]pendingOp
	// changed is this folder's mtime and ctime. Projected folders carry no
	// times of their own; like a real directory, the time advances whenever
	// an entry is added or removed, so listers that compare it re-read.
	changed time.Time
}

var (
	_ fs.NodeLookuper  = (*dirNode)(nil)
	_ fs.NodeReaddirer = (*dirNode)(nil)
	_ fs.NodeGetattrer = (*dirNode)(nil)
	_ fs.NodeSetattrer = (*dirNode)(nil)
	_ fs.NodeAccesser  = (*dirNode)(nil)
	_ fs.NodeMknoder   = (*dirNode)(nil)
	_ fs.NodeMkdirer   = (*dirNode)(nil)
	_ fs.NodeUnlinker  = (*dirNode)(nil)
	_ fs.NodeRmdirer   = (*dirNode)(nil)
	_ fs.NodeCreater   = (*dirNode)(nil)
	_ fs.NodeRenamer   = (*dirNode)(nil)
	_ fs.NodeLinker    = (*dirNode)(nil)
	_ fs.NodeSymlinker = (*dirNode)(nil)
	_ fs.NodeGetattrer = (*fileNode)(nil)
	_ fs.NodeSetattrer = (*fileNode)(nil)
	_ fs.NodeAccesser  = (*fileNode)(nil)
	_ fs.NodeOpener    = (*fileNode)(nil)
	_ fs.NodeReader    = (*fileNode)(nil)
	_ fs.NodeReleaser  = (*fileNode)(nil)
)

func (d *dirNode) children(ctx context.Context) ([]fileprovider.Item, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.items != nil {
		return d.items, nil
	}
	items, err := d.fsys.store.List(ctx, d.id)
	if err != nil {
		return nil, err
	}
	d.items = items
	return items, nil
}

// refresh brings every directory the kernel has listed up to date with the
// projection, replaying each difference through the pump so watchers get
// precise events. A replay that fails falls back to plain kernel cache
// invalidation, so the listing is current even when no event is delivered.
// An error leaves the old listing in place; the caller retries.
func (d *dirNode) refresh(ctx context.Context, mountDir string) error {
	d.mu.Lock()
	old := d.items
	d.mu.Unlock()
	var firstErr error
	if old != nil {
		current, err := d.fsys.store.List(ctx, d.id)
		if errors.Is(err, fileprovider.ErrNoSuchItem) {
			current = []fileprovider.Item{}
		} else if err != nil {
			return err
		}
		d.announce(ctx, filepath.Join(mountDir, d.Path(nil)), old, current)
		d.mu.Lock()
		d.items = current
		d.mu.Unlock()
	}
	for _, child := range d.Children() {
		if sub, ok := child.Operations().(*dirNode); ok {
			if err := sub.refresh(ctx, mountDir); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (d *dirNode) announce(ctx context.Context, dir string, old, current []fileprovider.Item) {
	before := make(map[string]fileprovider.Item, len(old))
	for _, i := range old {
		before[displayName(i)] = i
	}
	var added, changed []fileprovider.Item
	contentChanged := map[string]bool{}
	for _, i := range current {
		name := displayName(i)
		prev, existed := before[name]
		switch {
		case !existed:
			added = append(added, i)
		case prev.ID != i.ID || prev.Folder != i.Folder:
			// Another item took the name: replace it.
			added = append(added, i)
		case prev.ContentVersion != i.ContentVersion || prev.Size != i.Size || prev.Modified != i.Modified:
			changed = append(changed, i)
			contentChanged[name] = prev.ContentVersion != i.ContentVersion || prev.Size != i.Size
			delete(before, name)
		default:
			delete(before, name)
		}
	}
	// What is left in before was removed (or replaced) and goes first.
	for name, i := range before {
		d.setPending(name, pendingOp{item: i})
		err := d.fsys.pump.remove(ctx, filepath.Join(dir, name), i.Folder)
		d.clearPending(name)
		if err != nil {
			log.Printf("Clips folder: replaying removal of %q: %v", name, err)
			d.dropItem(name)
			if child := d.GetChild(name); child == nil || d.NotifyDelete(name, child) != 0 {
				_ = d.NotifyEntry(name)
			}
		}
	}
	for _, i := range added {
		name := displayName(i)
		d.setPending(name, pendingOp{create: true, item: i})
		err := d.fsys.pump.create(ctx, filepath.Join(dir, name), i.Folder)
		d.clearPending(name)
		if err != nil {
			log.Printf("Clips folder: replaying creation of %q: %v", name, err)
			d.putItem(i)
			_ = d.NotifyEntry(name)
		}
	}
	d.replaceItems(changed)
	for _, i := range changed {
		name := displayName(i)
		// Drop cached pages and attributes first, then raise IN_MODIFY for new
		// content or IN_ATTRIB for metadata. The kernel looks the name up
		// itself when it has no inode for it yet.
		if child := d.GetChild(name); child != nil {
			_ = child.NotifyContent(0, 0)
		}
		if err := d.fsys.pump.touch(ctx, filepath.Join(dir, name), contentChanged[name]); err != nil {
			log.Printf("Clips folder: replaying change of %q: %v", name, err)
		}
	}
	if len(before)+len(added) > 0 {
		d.mu.Lock()
		d.changed = time.Now()
		d.mu.Unlock()
	}
	if len(before)+len(added)+len(changed) > 0 {
		_ = d.NotifyContent(0, 0)
	}
}

func (d *dirNode) setPending(name string, op pendingOp) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.pending == nil {
		d.pending = map[string]pendingOp{}
	}
	d.pending[name] = op
}

func (d *dirNode) clearPending(name string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.pending, name)
}

// takePending returns the change a trusted request completes, if any.
func (d *dirNode) takePending(ctx context.Context, name string, create, folder bool) (fileprovider.Item, bool) {
	if !d.fsys.pump.trusted(ctx) {
		return fileprovider.Item{}, false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	op, ok := d.pending[name]
	if !ok || op.create != create || op.item.Folder != folder {
		return fileprovider.Item{}, false
	}
	delete(d.pending, name)
	return op.item, true
}

func (d *dirNode) putItem(i fileprovider.Item) {
	d.mu.Lock()
	defer d.mu.Unlock()
	name := displayName(i)
	for n, existing := range d.items {
		if displayName(existing) == name {
			// Copy rather than write in place: children() hands the
			// slice to readers that walk it without the lock.
			items := append([]fileprovider.Item(nil), d.items...)
			items[n] = i
			d.items = items
			return
		}
	}
	d.items = append(d.items, i)
}

// replaceItems swaps in new versions of existing entries with one copy of
// the listing, however many changed.
func (d *dirNode) replaceItems(changed []fileprovider.Item) {
	if len(changed) == 0 {
		return
	}
	byName := make(map[string]fileprovider.Item, len(changed))
	for _, i := range changed {
		byName[displayName(i)] = i
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	items := append([]fileprovider.Item(nil), d.items...)
	for n, existing := range items {
		if i, ok := byName[displayName(existing)]; ok {
			items[n] = i
		}
	}
	d.items = items
}

func (d *dirNode) dropItem(name string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for n, existing := range d.items {
		if displayName(existing) == name {
			d.items = append(d.items[:n:n], d.items[n+1:]...)
			return
		}
	}
}

func (d *dirNode) node(ctx context.Context, i fileprovider.Item, out *fuse.EntryOut) *fs.Inode {
	fillAttr(&out.Attr, i)
	out.SetEntryTimeout(cacheWindow)
	out.SetAttrTimeout(cacheWindow)
	if i.Folder {
		return d.NewInode(ctx, &dirNode{fsys: d.fsys, id: i.ID}, fs.StableAttr{Mode: syscall.S_IFDIR, Ino: ino(i.ID)})
	}
	return d.NewInode(ctx, &fileNode{fsys: d.fsys, id: i.ID}, fs.StableAttr{Mode: syscall.S_IFREG, Ino: ino(i.ID)})
}

func (d *dirNode) Getattr(ctx context.Context, _ fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	i, err := d.fsys.store.Item(ctx, d.id)
	if err != nil {
		return errno(err)
	}
	fillAttr(&out.Attr, i)
	d.mu.Lock()
	changed := d.changed
	d.mu.Unlock()
	if changed.IsZero() {
		changed = d.fsys.mounted
	}
	out.SetTimes(&changed, &changed, &changed)
	out.SetTimeout(cacheWindow)
	return 0
}

func (d *dirNode) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	items, err := d.children(ctx)
	if err != nil {
		return nil, errno(err)
	}
	for _, i := range items {
		if displayName(i) == name {
			return d.node(ctx, i, out), 0
		}
	}
	return nil, syscall.ENOENT
}

func (d *dirNode) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	items, err := d.children(ctx)
	if err != nil {
		return nil, errno(err)
	}
	entries := make([]fuse.DirEntry, 0, len(items))
	for _, i := range items {
		mode := uint32(syscall.S_IFREG)
		if i.Folder {
			mode = syscall.S_IFDIR
		}
		entries = append(entries, fuse.DirEntry{Name: displayName(i), Mode: mode, Ino: ino(i.ID)})
	}
	return fs.NewListDirStream(entries), 0
}

// The handlers below complete replayed changes for the pump thread and
// reject every other change.

func (d *dirNode) Mknod(ctx context.Context, name string, _, _ uint32, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	i, ok := d.takePending(ctx, name, true, false)
	if !ok {
		return nil, syscall.EROFS
	}
	d.putItem(i)
	return d.node(ctx, i, out), 0
}

func (d *dirNode) Mkdir(ctx context.Context, name string, _ uint32, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	i, ok := d.takePending(ctx, name, true, true)
	if !ok {
		return nil, syscall.EROFS
	}
	d.putItem(i)
	return d.node(ctx, i, out), 0
}

func (d *dirNode) Unlink(ctx context.Context, name string) syscall.Errno {
	if _, ok := d.takePending(ctx, name, false, false); !ok {
		return syscall.EROFS
	}
	d.dropItem(name)
	return 0
}

func (d *dirNode) Rmdir(ctx context.Context, name string) syscall.Errno {
	if _, ok := d.takePending(ctx, name, false, true); !ok {
		return syscall.EROFS
	}
	d.dropItem(name)
	return 0
}

func (d *dirNode) Setattr(ctx context.Context, _ fs.FileHandle, _ *fuse.SetAttrIn, out *fuse.AttrOut) syscall.Errno {
	if !d.fsys.pump.trusted(ctx) {
		return syscall.EROFS
	}
	return d.Getattr(ctx, nil, out)
}

func (d *dirNode) Access(ctx context.Context, mask uint32) syscall.Errno {
	return access(mask)
}

func (d *dirNode) Create(context.Context, string, uint32, uint32, *fuse.EntryOut) (*fs.Inode, fs.FileHandle, uint32, syscall.Errno) {
	return nil, nil, 0, syscall.EROFS
}

func (d *dirNode) Rename(context.Context, string, fs.InodeEmbedder, string, uint32) syscall.Errno {
	return syscall.EROFS
}

func (d *dirNode) Link(context.Context, fs.InodeEmbedder, string, *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	return nil, syscall.EROFS
}

func (d *dirNode) Symlink(context.Context, string, string, *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	return nil, syscall.EROFS
}

// access keeps write access denied, so file managers and dialogs present the
// folder as read-only even though the kernel mount is not flagged "ro".
func access(mask uint32) syscall.Errno {
	if mask&2 != 0 { // W_OK
		return syscall.EROFS
	}
	return 0
}

type fileNode struct {
	fs.Inode
	fsys *fsys
	id   string

	mu    sync.Mutex
	cache *revision // shared by the open handles of the current revision
}

func (f *fileNode) Getattr(ctx context.Context, _ fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	i, err := f.fsys.store.Item(ctx, f.id)
	if err != nil {
		return errno(err)
	}
	fillAttr(&out.Attr, i)
	out.SetTimeout(cacheWindow)
	return 0
}

func (f *fileNode) Setattr(ctx context.Context, _ fs.FileHandle, _ *fuse.SetAttrIn, out *fuse.AttrOut) syscall.Errno {
	if !f.fsys.pump.trusted(ctx) {
		return syscall.EROFS
	}
	return f.Getattr(ctx, nil, out)
}

func (f *fileNode) Access(ctx context.Context, mask uint32) syscall.Errno {
	return access(mask)
}

// handle pins the content revision current at open. Reads after the clip
// changes fail with ESTALE; reopening returns the new content.
type handle struct{ rev *revision }

// revision holds one content revision for every handle that opened it. The
// first read loads it in one snapshot and later reads are served from
// memory: reading piecewise from the database would materialize the whole
// blob again for every chunk. It is dropped when its last handle closes.
type revision struct {
	version string
	refs    int // guarded by fileNode.mu
	mu      sync.Mutex
	data    []byte
	loaded  bool
}

func (r *revision) content(ctx context.Context, store *fileprovider.Store, id string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.loaded {
		return r.data, nil
	}
	var buf bytes.Buffer
	err := store.Content(ctx, id, r.version, func(i fileprovider.Item) (io.Writer, error) {
		buf.Grow(int(i.Size))
		return &buf, nil
	})
	if err != nil {
		return nil, err
	}
	r.data, r.loaded = buf.Bytes(), true
	return r.data, nil
}

func (f *fileNode) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	if flags&(syscall.O_WRONLY|syscall.O_RDWR|syscall.O_TRUNC|syscall.O_APPEND|syscall.O_CREAT) != 0 {
		return nil, 0, syscall.EROFS
	}
	i, err := f.fsys.store.Item(ctx, f.id)
	if err != nil {
		return nil, 0, errno(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cache == nil || f.cache.version != i.ContentVersion {
		f.cache = &revision{version: i.ContentVersion}
	}
	f.cache.refs++
	return &handle{rev: f.cache}, 0, 0
}

func (f *fileNode) Release(_ context.Context, fh fs.FileHandle) syscall.Errno {
	h, _ := fh.(*handle)
	if h == nil {
		return 0
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	h.rev.refs--
	if h.rev.refs == 0 && f.cache == h.rev {
		f.cache = nil
	}
	return 0
}

func (f *fileNode) Read(ctx context.Context, fh fs.FileHandle, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	h, _ := fh.(*handle)
	if h == nil {
		return nil, syscall.EBADF
	}
	data, err := h.rev.content(ctx, f.fsys.store, f.id)
	if err != nil {
		return nil, errno(err)
	}
	// The kernel caps reads at the inode's current size, which follows the
	// newest revision. Fail a handle whose clip has changed so a copy in
	// progress errors out instead of ending silently truncated.
	current, err := f.fsys.store.Item(ctx, f.id)
	if err != nil {
		return nil, errno(err)
	}
	if current.ContentVersion != h.rev.version {
		return nil, syscall.ESTALE
	}
	if off >= int64(len(data)) {
		return fuse.ReadResultData(nil), 0
	}
	return fuse.ReadResultData(data[off:min(off+int64(len(dest)), int64(len(data)))]), 0
}
