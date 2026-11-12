package main

import (
	"errors"
	"sync"
	"testing"
	"time"

	coreapp "go-clipboard/internal/app"
	"go-clipboard/internal/osnotify"
)

type stubSender struct {
	mu      sync.Mutex
	initErr error
	inits   int
	sent    int
}

func (s *stubSender) InitNotifications() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inits++
	return s.initErr
}

func (s *stubSender) SendNotification(id, title, body string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent++
	return nil
}

func (s *stubSender) CleanupNotifications() {}

func newNotificationTestCore(t *testing.T) *coreapp.App {
	t.Helper()
	db := newTransferServiceTestDB(t, t.TempDir())
	if _, err := db.Exec(`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	core := coreapp.NewApp()
	core.SetDB(db)
	return core
}

func TestNotificationServiceHeadless(t *testing.T) {
	core := newNotificationTestCore(t)
	svc := NewNotificationService(core)

	st := svc.GetNotificationStatus()
	if st.Supported || st.Enabled || st.State != "unsupported" {
		t.Fatalf("headless status = %+v, want unsupported and off", st)
	}
	// The setting still persists, so the UI behaves the same in e2e runs.
	st, err := svc.SetNotificationsEnabled(true)
	if err != nil || !st.Enabled {
		t.Fatalf("enable: %+v, %v", st, err)
	}
	if !core.OSNotificationsEnabled() {
		t.Fatal("setting not persisted")
	}
	if _, err := svc.SendTestNotification(); err != nil {
		t.Fatalf("test notification without a notifier: %v", err)
	}
	svc.stop()
}

func TestNotificationServiceEnableInitialisesAndReportsFailure(t *testing.T) {
	core := newNotificationTestCore(t)
	svc := NewNotificationService(core)
	sender := &stubSender{initErr: errors.New("notifications require a valid bundle identifier")}
	n := osnotify.New(osnotify.Options{Sender: sender, Enabled: core.OSNotificationsEnabled})
	n.Start()
	svc.setNotifier(n)
	defer svc.stop()

	if st := svc.GetNotificationStatus(); !st.Supported || st.State != "idle" || st.Enabled {
		t.Fatalf("initial status = %+v", st)
	}
	st, err := svc.SetNotificationsEnabled(true)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Enabled || st.State != "failed" || st.Error == "" {
		t.Fatalf("status after a failed enable = %+v", st)
	}

	// Switching off and on again retries.
	sender.mu.Lock()
	sender.initErr = nil
	sender.mu.Unlock()
	if _, err := svc.SetNotificationsEnabled(false); err != nil {
		t.Fatal(err)
	}
	st, err = svc.SetNotificationsEnabled(true)
	if err != nil || st.State != "ready" || st.Error != "" {
		t.Fatalf("status after retry = %+v, %v", st, err)
	}
	if _, err := svc.SendTestNotification(); err != nil {
		t.Fatal(err)
	}
	sender.mu.Lock()
	inits, sent := sender.inits, sender.sent
	sender.mu.Unlock()
	if inits != 2 || sent != 1 {
		t.Fatalf("inits %d, sent %d; want 2 and 1", inits, sent)
	}
}

func TestNotificationServiceStopIsBounded(t *testing.T) {
	core := newNotificationTestCore(t)
	svc := NewNotificationService(core)
	n := osnotify.New(osnotify.Options{Sender: &stubSender{}, Enabled: core.OSNotificationsEnabled})
	n.Start()
	svc.setNotifier(n)
	done := make(chan struct{})
	go func() { svc.stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not return")
	}
}
