//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit -framework Foundation
#import <AppKit/AppKit.h>

int copyFilesToPasteboard(const char** paths, int count) {
    NSPasteboard *pb = [NSPasteboard generalPasteboard];
    [pb clearContents];

    NSMutableArray *urls = [NSMutableArray arrayWithCapacity:count];
    for (int i = 0; i < count; i++) {
        NSString *path = [NSString stringWithUTF8String:paths[i]];
        NSURL *url = [NSURL fileURLWithPath:path];
        if (url) {
            [urls addObject:url];
        }
    }
    BOOL ok = [pb writeObjects:urls];
    return ok ? 0 : 1;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

func copyFilesToClipboard(paths []string) error {
	if len(paths) == 0 {
		return fmt.Errorf("no paths provided")
	}
	cPaths := make([]*C.char, len(paths))
	for i, p := range paths {
		cPaths[i] = C.CString(p)
	}
	defer func() {
		for _, cp := range cPaths {
			C.free(unsafe.Pointer(cp))
		}
	}()

	result := C.copyFilesToPasteboard(&cPaths[0], C.int(len(paths)))
	if result != 0 {
		return fmt.Errorf("failed to write files to pasteboard")
	}
	return nil
}
