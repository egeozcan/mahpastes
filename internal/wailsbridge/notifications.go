package wailsbridge

import (
	"errors"
	"fmt"
	"log"
	goruntime "runtime"

	rt "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ErrNotificationsInactive is returned by the notification methods when the
// bridge has no Wails runtime context.
var ErrNotificationsInactive = errors.New("notifications need the desktop window")

// ErrNotificationsDenied is returned when the OS (macOS) did not grant
// permission to show notifications.
var ErrNotificationsDenied = errors.New("permission to show notifications was not granted; allow mahpastes under System Settings > Notifications")

// InitNotifications prepares native OS notifications and, on macOS, asks the
// user for permission (blocking until they answer, up to Wails' 3-minute
// limit). Clicking a notification brings the window forward.
//
// Call it off the UI thread and only when the user has notifications on:
// on Windows it writes HKCU registry keys for the toast activator, on Linux
// it connects to the session D-Bus. macOS needs an .app bundle with a bundle
// identifier; a bare dev binary returns an error.
func (b *Bridge) InitNotifications() (err error) {
	if !b.active() {
		return ErrNotificationsInactive
	}
	defer recoverNotificationPanic(&err)
	if err := rt.InitializeNotifications(b.ctx); err != nil {
		return err
	}
	granted, err := rt.RequestNotificationAuthorization(b.ctx)
	if err != nil {
		return err
	}
	if !granted {
		return ErrNotificationsDenied
	}
	rt.OnNotificationResponse(b.ctx, func(result rt.NotificationResult) {
		if result.Error != nil {
			log.Printf("notifications: response: %v", result.Error)
			return
		}
		b.raiseForNotification()
	})
	return nil
}

// SendNotification shows a basic notification. id should be unique per
// notification. May block briefly (a D-Bus call on Linux, up to 5s on macOS).
func (b *Bridge) SendNotification(id, title, body string) (err error) {
	if !b.active() {
		return ErrNotificationsInactive
	}
	defer recoverNotificationPanic(&err)
	return rt.SendNotification(b.ctx, rt.NotificationOptions{ID: id, Title: title, Body: body})
}

// CleanupNotifications releases the notification backend (the Linux D-Bus
// connection). Only call it after a successful InitNotifications.
func (b *Bridge) CleanupNotifications() {
	if !b.active() {
		return
	}
	defer recoverNotificationPanic(nil)
	rt.CleanupNotifications(b.ctx)
}

// raiseForNotification shows, unminimises and focuses the window after a
// notification click. Windows' WindowShow already restores a minimised
// window, and its WindowUnminimise would un-maximise a maximised one.
func (b *Bridge) raiseForNotification() {
	if !b.active() {
		return
	}
	rt.WindowShow(b.ctx)
	if goruntime.GOOS != "windows" {
		rt.WindowUnminimise(b.ctx)
	}
}

// recoverNotificationPanic turns a panic in a platform backend into an error,
// so an unusual desktop setup degrades to "notifications unavailable" rather
// than taking the app down.
func recoverNotificationPanic(err *error) {
	if r := recover(); r != nil {
		log.Printf("notifications: backend panic: %v", r)
		if err != nil {
			*err = fmt.Errorf("notification backend failed: %v", r)
		}
	}
}
