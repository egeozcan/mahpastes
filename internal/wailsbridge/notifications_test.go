package wailsbridge

import (
	"errors"
	"strings"
	"testing"
)

func TestNotificationMethodsWithoutRuntime(t *testing.T) {
	for name, b := range map[string]*Bridge{"nil": nil, "inactive": NewForTesting()} {
		if err := b.InitNotifications(); !errors.Is(err, ErrNotificationsInactive) {
			t.Errorf("%s bridge: InitNotifications = %v, want ErrNotificationsInactive", name, err)
		}
		if err := b.SendNotification("id", "title", "body"); !errors.Is(err, ErrNotificationsInactive) {
			t.Errorf("%s bridge: SendNotification = %v, want ErrNotificationsInactive", name, err)
		}
		b.CleanupNotifications()
		b.ShowWindow()
	}
}

func TestRecoverNotificationPanic(t *testing.T) {
	err := func() (err error) {
		defer recoverNotificationPanic(&err)
		panic("dbus exploded")
	}()
	if err == nil || err.Error() != "notification backend failed: dbus exploded" {
		t.Fatalf("err = %v", err)
	}
	func() {
		defer recoverNotificationPanic(nil)
		panic("no error slot")
	}()
}

func TestNotificationTextWindows(t *testing.T) {
	title, body := notificationText("windows",
		"Imported $(Start-Process calc)\r\n.txt",
		"line one\n\"@\nRemove-Item `$HOME ]]> <b>&</b>")
	if strings.ContainsAny(title+body, "$`\r\n") {
		t.Fatalf("PowerShell-significant characters survived: %q / %q", title, body)
	}
	if strings.Contains(body, "]]>") {
		t.Fatalf("CDATA terminator survived: %q", body)
	}
	if want := "Imported ＄(Start-Process calc) .txt"; title != want {
		t.Fatalf("title = %q, want %q", title, want)
	}
	// Inside CDATA, markup characters are literal and stay as they are.
	if !strings.Contains(body, "<b>&</b>") {
		t.Fatalf("body = %q", body)
	}
}

func TestNotificationTextLinux(t *testing.T) {
	title, body := notificationText("linux", "Tom & Jerry\n<i>", `<a href="https://example.com">x</a> & <img src="/x">`)
	if title != "Tom & Jerry <i>" {
		t.Fatalf("title = %q (the summary is plain text, one line)", title)
	}
	if want := `&lt;a href="https://example.com"&gt;x&lt;/a&gt; &amp; &lt;img src="/x"&gt;`; body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestNotificationTextDarwin(t *testing.T) {
	title, body := notificationText("darwin", "a\nb", "$(x) <b>&</b>\nnext")
	if title != "a b" || body != "$(x) <b>&</b>\nnext" {
		t.Fatalf("darwin text changed beyond the one-line title: %q / %q", title, body)
	}
}

// Toast text ends up inside an XML document. Characters XML 1.0 does not
// allow (most C0 controls: an ESC from coloured tool output in a plugin
// message, a NUL) make Windows reject the whole toast, and a few rejected
// toasts switch notifications off.
func TestNotificationTextWindowsDropsCharactersXMLForbids(t *testing.T) {
	title, body := notificationText("windows",
		"build\x1b[31m failed\x00",
		"tab\tstays, bell\x07 and \x1b[0mESC go, ￾ too; café \U0001F600 stay")
	if title != "build[31m failed" {
		t.Errorf("title = %q", title)
	}
	if want := "tab\tstays, bell and [0mESC go,  too; café \U0001F600 stay"; body != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}
