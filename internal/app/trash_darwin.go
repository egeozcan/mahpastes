//go:build darwin

package app

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation
#import <Foundation/Foundation.h>
#include <string.h>

// trashItemAtPath moves one file to the user's Trash.
//
// Unlike startNativeFileDrag, this does NOT dispatch to the main queue.
// NSFileManager is documented as thread-safe and -trashItemAtURL: touches no
// AppKit state, so it is safe to call from the Wails call goroutine that runs
// ImportApply. Hopping to the main queue would block the UI for the duration of
// a multi-hundred-file apply.
static int trashItemAtPath(const char* cpath, char* errbuf, int errlen) {
    @autoreleasepool {
        if (cpath == NULL) {
            return 1;
        }
        NSString *path = [NSString stringWithUTF8String:cpath];
        if (path == nil || [path length] == 0) {
            return 2;
        }
        NSURL *url = [NSURL fileURLWithPath:path isDirectory:NO];
        if (url == nil) {
            return 3;
        }
        NSError *err = nil;
        if ([[NSFileManager defaultManager] trashItemAtURL:url resultingItemURL:nil error:&err]) {
            return 0;
        }
        if (err != nil && errbuf != NULL && errlen > 1) {
            const char *msg = [[err localizedDescription] UTF8String];
            if (msg != NULL) {
                strncpy(errbuf, msg, errlen - 1);
                errbuf[errlen - 1] = '\0';
            }
        }
        return 4;
    }
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// trashIsRecoverable reports whether deletions land somewhere the user can undo
// them. The wizard surfaces this so the setup pane can warn before any decision
// is made, rather than after the files are gone.
func trashIsRecoverable() bool { return true }

// moveToTrash sends a single file to the macOS Trash.
func moveToTrash(absPath string) error {
	if absPath == "" {
		return fmt.Errorf("trash: empty path")
	}

	cpath := C.CString(absPath)
	defer C.free(unsafe.Pointer(cpath))

	errbuf := make([]byte, 512)
	rc := C.trashItemAtPath(cpath, (*C.char)(unsafe.Pointer(&errbuf[0])), C.int(len(errbuf)))
	if rc == 0 {
		return nil
	}

	detail := cstringToGo(errbuf)
	switch rc {
	case 1, 2:
		return fmt.Errorf("trash: invalid path %q", absPath)
	case 3:
		return fmt.Errorf("trash: could not build a file URL for %q", absPath)
	default:
		if detail != "" {
			return fmt.Errorf("trash: %s", detail)
		}
		return fmt.Errorf("trash: NSFileManager refused to trash %q", absPath)
	}
}

// cstringToGo trims a NUL-terminated C buffer down to its Go string.
func cstringToGo(buf []byte) string {
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}
