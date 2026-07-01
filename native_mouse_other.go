//go:build !darwin

package main

// installMouseNavMonitor is a no-op outside macOS. Windows (WebView2) and Linux
// deliver the mouse side buttons to the DOM correctly, so the frontend handles
// them directly there (see the mouse-nav block in frontend/js/app.js).
func installMouseNavMonitor(_ func(direction string)) {}
