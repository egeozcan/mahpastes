package wailsbridge

import (
	"errors"
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
		b.raiseForNotification()
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
