package main

import (
	"errors"
	"log"
	"runtime"
	"sync"
	"time"

	coreapp "go-clipboard/internal/app"
	"go-clipboard/internal/osnotify"
	"go-clipboard/internal/wailsbridge"
)

// activateWait bounds SetNotificationsEnabled(true): macOS keeps the
// permission prompt open for up to three minutes.
const activateWait = 190 * time.Second

var errNotificationsOff = errors.New("turn on system notifications first")

// NotificationService exposes the "System notifications" setting to the
// frontend. The notifier is nil in headless runs (e2e, worker self-test),
// where nobody would see a notification; the setting still persists there.
type NotificationService struct {
	core *coreapp.App

	mu       sync.Mutex
	notifier *osnotify.Notifier
}

// NotificationStatus is what the settings section shows.
type NotificationStatus struct {
	// Supported is false when this run cannot show notifications at all.
	Supported bool `json:"supported"`
	Enabled   bool `json:"enabled"`
	// State is idle (not initialised yet), pending (waiting for the OS, e.g.
	// the macOS permission prompt), ready, failed or unsupported.
	State    string `json:"state"`
	Error    string `json:"error"`
	Platform string `json:"platform"`
}

// NewNotificationService creates the bound service; the notifier is attached
// in OnStartup once the Wails runtime exists.
func NewNotificationService(core *coreapp.App) *NotificationService {
	return &NotificationService{core: core}
}

// newDesktopNotifier builds the notifier for the desktop window. It is
// started after Bootstrap; the frontend's focus reports reach it through
// ReportWindowFocus.
func newDesktopNotifier(bridge *wailsbridge.Bridge, core *coreapp.App) *osnotify.Notifier {
	return osnotify.New(osnotify.Options{Sender: bridge, Enabled: core.OSNotificationsEnabled})
}

// ReportWindowFocus is called by the frontend (os-notifications.js) whenever
// the window gains or loses focus. It is a bound call, not a Wails event: in
// `wails dev` a page in an external browser can load before the native window
// exists, and Wails forwards a browser's event to that window without a nil
// check, which ends the process. Calls run on their own goroutines, so two
// reports sent close together can arrive swapped; seq (increasing within one
// page load, named by session) decides, not the arrival.
func (s *NotificationService) ReportWindowFocus(focused bool, seq float64, session string) {
	if n := s.current(); n != nil {
		n.SetFocusedSeq(session, seq, focused)
	}
}

func (s *NotificationService) setNotifier(n *osnotify.Notifier) {
	s.mu.Lock()
	s.notifier = n
	s.mu.Unlock()
}

func (s *NotificationService) current() *osnotify.Notifier {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.notifier
}

// stop ends the notifier at shutdown.
func (s *NotificationService) stop() {
	if n := s.current(); n != nil {
		n.Stop(2 * time.Second)
	}
}

func (s *NotificationService) status() NotificationStatus {
	st := NotificationStatus{Enabled: s.core.OSNotificationsEnabled(), Platform: runtime.GOOS}
	n := s.current()
	if n == nil {
		st.State = "unsupported"
		st.Error = "System notifications are not available in this run."
		return st
	}
	ns := n.Status()
	st.Supported = true
	st.State = string(ns.State)
	st.Error = ns.Error
	return st
}

// GetNotificationStatus reports the setting and whether the OS backend works.
func (s *NotificationService) GetNotificationStatus() NotificationStatus {
	return s.status()
}

// SetNotificationsEnabled saves the setting. Switching it on initialises the
// OS backend right away, so the macOS permission prompt appears now rather
// than at some later background event, and any failure shows in Settings.
func (s *NotificationService) SetNotificationsEnabled(enabled bool) (NotificationStatus, error) {
	if err := s.core.SetOSNotificationsEnabled(enabled); err != nil {
		return s.status(), err
	}
	if n := s.current(); n != nil {
		if enabled {
			if err := n.Activate(activateWait); err != nil {
				log.Printf("notifications: enable: %v", err)
			}
		} else {
			n.Discard()
		}
	}
	return s.status(), nil
}

// SendTestNotification shows a notification now, whatever the focus, so the
// user can check that the OS (Focus, Do Not Disturb) lets it through. It is
// refused while the setting is off: initialising the backend prompts on macOS
// and writes registry keys on Windows, which only switching it on may do.
func (s *NotificationService) SendTestNotification() (NotificationStatus, error) {
	n := s.current()
	if n == nil {
		return s.status(), nil
	}
	if !s.core.OSNotificationsEnabled() {
		return s.status(), errNotificationsOff
	}
	err := n.SendTest(activateWait)
	return s.status(), err
}
