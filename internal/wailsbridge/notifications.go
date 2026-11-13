package wailsbridge

import (
	"errors"
	"fmt"
	"log"
	goruntime "runtime"
	"strings"

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
		// Clicking a notification brings the window back, the same way a
		// second launch or a mahpastes:// link does (it may be hidden by
		// keep-running, or minimised).
		b.ShowWindow()
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
	title, body = notificationText(goruntime.GOOS, title, body)
	return rt.SendNotification(b.ctx, rt.NotificationOptions{ID: id, Title: title, Body: body})
}

// notificationText makes event text (filenames, tag names, plugin messages:
// all untrusted) safe for the platform backend.
//
//   - Windows: go-toast puts the text unescaped into CDATA sections of the
//     toast XML, and when its COM path fails it pastes that XML into a
//     PowerShell double-quoted here-string, where "$(...)" runs code and a
//     line starting with `"@` ends the string. So line breaks become spaces,
//     "$" and the backtick escape become look-alikes, and "]]>" is split.
//   - Linux: freedesktop notification bodies are markup (links, images), so
//     the body is escaped. The summary is plain text.
//   - Every platform: the title is one line.
func notificationText(goos, title, body string) (string, string) {
	title = oneLine(title)
	switch goos {
	case "windows":
		title, body = windowsToastText(title), windowsToastText(oneLine(body))
	case "linux":
		body = linuxBodyMarkup.Replace(body)
	}
	return title, body
}

var lineBreaks = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ", "\u0085", " ", " ", " ", " ", " ")

func oneLine(s string) string { return lineBreaks.Replace(s) }

var windowsToastReplacer = strings.NewReplacer(
	"$", "＄", // FULLWIDTH DOLLAR SIGN
	"`", "ˋ", // MODIFIER LETTER GRAVE ACCENT
	"]]>", "]] >",
)

// windowsToastText makes s safe for a Windows toast, whose text is put into
// an XML document: besides the replacements above, characters XML 1.0 does
// not allow at all (C0 controls other than tab, U+FFFE, U+FFFF) are dropped.
// One of them makes Windows reject the whole toast.
func windowsToastText(s string) string {
	s = strings.Map(func(r rune) rune {
		if (r < 0x20 && r != 0x09 && r != 0x0A && r != 0x0D) || r == 0xFFFE || r == 0xFFFF {
			return -1
		}
		return r
	}, s)
	return windowsToastReplacer.Replace(s)
}

var linuxBodyMarkup = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// CleanupNotifications releases the notification backend (the Linux D-Bus
// connection). Only call it after a successful InitNotifications.
func (b *Bridge) CleanupNotifications() {
	if !b.active() {
		return
	}
	defer recoverNotificationPanic(nil)
	rt.CleanupNotifications(b.ctx)
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
