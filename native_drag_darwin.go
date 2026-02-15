//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit -framework Foundation
#import <AppKit/AppKit.h>
#import <Foundation/Foundation.h>
#import <dispatch/dispatch.h>

static int startNativeFileDrag(const char* cpath) {
    if (cpath == NULL) {
        return 1;
    }
    NSString *path = [NSString stringWithUTF8String:cpath];
    if (path == nil || [path length] == 0) {
        return 2;
    }
    if (![[NSFileManager defaultManager] fileExistsAtPath:path]) {
        return 3;
    }

    __block int result = 4;
    void (^dragBlock)(void) = ^{
        NSWindow *window = [NSApp keyWindow];
        if (window == nil) {
            result = 5;
            return;
        }
        NSView *view = [window contentView];
        if (view == nil) {
            result = 6;
            return;
        }

        NSEvent *event = [NSApp currentEvent];
        if (event == nil) {
            result = 7;
            return;
        }

        NSPoint location = [view convertPoint:[event locationInWindow] fromView:nil];
        NSRect dragRect = NSMakeRect(location.x, location.y, 1, 1);

#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
        BOOL ok = [view dragFile:path fromRect:dragRect slideBack:YES event:event];
#pragma clang diagnostic pop
        result = ok ? 0 : 8;
    };

    if ([NSThread isMainThread]) {
        dragBlock();
    } else {
        dispatch_sync(dispatch_get_main_queue(), dragBlock);
    }

    return result;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

func startNativeFileDrag(absPath string) error {
	cPath := C.CString(absPath)
	defer C.free(unsafe.Pointer(cPath))

	code := int(C.startNativeFileDrag(cPath))
	if code != 0 {
		return fmt.Errorf("native drag failed (code %d)", code)
	}
	return nil
}
