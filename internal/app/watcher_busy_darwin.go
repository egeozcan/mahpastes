//go:build darwin && cgo

package app

/*
#include <libproc.h>
#include <stdint.h>
#include <stdlib.h>
#include <sys/fcntl.h>
#include <sys/proc_info.h>

// mpWriteOpenFiles lists every file a process this user can inspect has open
// for writing, as (dev, ino) pairs in *out (2 * the returned count values; the
// caller frees it). It walks the descriptors of every process itself:
// proc_listpidspath answers for one path but also walks each process's memory
// regions, which costs ten times as much (about 100 ms against 10 ms on a
// desktop with 600 processes).
static int mpWriteOpenFiles(uint64_t **out) {
	*out = NULL;
	int n = proc_listallpids(NULL, 0);
	if (n <= 0) {
		return 0;
	}
	n += 64; // room for processes started meanwhile
	int *pids = malloc(n * sizeof(int));
	if (pids == NULL) {
		return 0;
	}
	n = proc_listallpids(pids, n * sizeof(int));
	int count = 0;
	int room = 0;
	uint64_t *files = NULL;
	int cap = 0;
	struct proc_fdinfo *fds = NULL;
	for (int i = 0; i < n; i++) {
		// Fails for a process of another user.
		int size = proc_pidinfo(pids[i], PROC_PIDLISTFDS, 0, NULL, 0);
		if (size <= 0) {
			continue;
		}
		size += 32 * sizeof(struct proc_fdinfo); // room for descriptors opened meanwhile
		if (size > cap) {
			free(fds);
			cap = 0;
			fds = malloc(size);
			if (fds == NULL) {
				continue;
			}
			cap = size;
		}
		size = proc_pidinfo(pids[i], PROC_PIDLISTFDS, 0, fds, cap);
		for (int j = 0; j < size / (int)sizeof(struct proc_fdinfo); j++) {
			if (fds[j].proc_fdtype != PROX_FDTYPE_VNODE) {
				continue;
			}
			struct vnode_fdinfo vi;
			if (proc_pidfdinfo(pids[i], fds[j].proc_fd, PROC_PIDFDVNODEINFO, &vi, sizeof(vi)) != sizeof(vi)) {
				continue;
			}
			// Event-only opens (file watchers, ours included) carry no FWRITE.
			if ((vi.pfi.fi_openflags & FWRITE) == 0) {
				continue;
			}
			if (count == room) {
				int grown = room == 0 ? 256 : room * 2;
				uint64_t *more = realloc(files, (size_t)grown * 2 * sizeof(uint64_t));
				if (more == NULL) {
					continue;
				}
				files = more;
				room = grown;
			}
			files[2 * count] = (uint64_t)vi.pvi.vi_stat.vst_dev;
			files[2 * count + 1] = vi.pvi.vi_stat.vst_ino;
			count++;
		}
	}
	free(fds);
	free(pids);
	*out = files;
	return count;
}
*/
import "C"

import (
	"os"
	"syscall"
	"time"
	"unsafe"
)

// fileBusyCheckSupported says fileOpenForWriting can tell.
const fileBusyCheckSupported = true

// writeOpenWalks is the one place the process walk runs: see
// writeOpenSnapshots for why it is never called per file.
var writeOpenWalks = newWriteOpenSnapshots(listWriteOpenFiles)

// listWriteOpenFiles walks every inspectable process once.
func listWriteOpenFiles() map[openFileID]struct{} {
	var raw *C.uint64_t
	n := int(C.mpWriteOpenFiles(&raw))
	if raw == nil {
		return nil
	}
	defer C.free(unsafe.Pointer(raw))
	pairs := unsafe.Slice((*uint64)(unsafe.Pointer(raw)), 2*n)
	files := make(map[openFileID]struct{}, n)
	for i := 0; i < n; i++ {
		files[openFileID{dev: pairs[2*i], ino: pairs[2*i+1]}] = struct{}{}
	}
	return files
}

// fileOpenForWriting reports whether a process has the file open for writing,
// going by a walk of the process table begun no more than maxAge before the
// call (0: after it). It is a veto, not proof: it sees this user's local
// processes only, so not a root process and not a writer on another machine
// (a network share).
func fileOpenForWriting(_ string, info os.FileInfo, maxAge time.Duration) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	return writeOpenWalks.openForWriting(openFileID{dev: uint64(uint32(st.Dev)), ino: st.Ino}, maxAge)
}
