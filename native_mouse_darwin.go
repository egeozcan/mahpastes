//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit -framework Foundation

// Defined in native_mouse_darwin.m. Installs (idempotently) a global NSEvent
// monitor for the mouse side buttons; safe to call from any goroutine.
void startMouseNavMonitor(void);
*/
import "C"

// mouseNavHandler receives "back"/"forward" when a side button is released. It
// is set once at startup via installMouseNavMonitor and read from the AppKit
// main thread inside goMouseNav.
var mouseNavHandler func(direction string)

// installMouseNavMonitor maps the mouse back (button 3 / X1) and forward
// (button 4 / X2) side buttons to in-app navigation on macOS.
//
// WKWebView reports these buttons unreliably to the DOM — `button` is always 1
// (middle) and the `buttons` bitmask is intermittently 0 — so the frontend
// cannot distinguish them. We instead read AppKit's buttonNumber directly and
// swallow the event before WebKit's own flaky history handling can race with
// ours, then forward "back"/"forward" to onNav.
func installMouseNavMonitor(onNav func(direction string)) {
	if onNav == nil {
		return
	}
	mouseNavHandler = onNav
	C.startMouseNavMonitor()
}

//export goMouseNav
func goMouseNav(direction C.int) {
	h := mouseNavHandler
	if h == nil {
		return
	}
	if direction == 0 {
		h("back")
	} else {
		h("forward")
	}
}
