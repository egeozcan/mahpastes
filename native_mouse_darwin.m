// Objective-C side of the macOS mouse side-button navigation monitor. Kept in
// a separate .m file because the Go file declaring //export goMouseNav may not
// contain C function definitions in its cgo preamble. The _darwin filename
// suffix restricts this to macOS builds.

#import <AppKit/AppKit.h>
#import <Foundation/Foundation.h>
#import <dispatch/dispatch.h>

#include "_cgo_export.h" // declares goMouseNav (generated from the //export)

static id mouseNavMonitor = nil;

static void installMouseNavMonitorImpl(void) {
    if (mouseNavMonitor != nil) {
        return; // already installed — keep it idempotent
    }
    NSEventMask mask = NSEventMaskOtherMouseDown | NSEventMaskOtherMouseUp;
    mouseNavMonitor = [NSEvent addLocalMonitorForEventsMatchingMask:mask
        handler:^NSEvent *_Nullable(NSEvent *event) {
            NSInteger btn = [event buttonNumber];
            // Button 3 = back (X1), 4 = forward (X2). Everything else
            // (button 2 = middle click, etc.) passes straight through.
            if (btn != 3 && btn != 4) {
                return event;
            }
            if ([event type] == NSEventTypeOtherMouseUp) {
                goMouseNav(btn == 3 ? 0 : 1);
            }
            // Swallow both the down and up for the side buttons so WebKit's own
            // (flaky) history handling never races with ours.
            return nil;
        }];
}

// startMouseNavMonitor installs the monitor on the main thread (NSEvent monitor
// installation must happen there).
void startMouseNavMonitor(void) {
    if ([NSThread isMainThread]) {
        installMouseNavMonitorImpl();
    } else {
        dispatch_async(dispatch_get_main_queue(), ^{
            installMouseNavMonitorImpl();
        });
    }
}
