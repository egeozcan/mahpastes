package main

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"go-clipboard/internal/wailsbridge"
)

// The headless worker self-test.
//
// The design's milestone 1 requires proving that the committed static validator
// worker can load, exchange a request, time out, terminate and restart *on each
// supported desktop surface* — WKWebView on macOS, WebView2 on Windows,
// WebKitGTK on Linux. The e2e suites cannot answer that: Playwright drives
// Chromium over http://localhost, which is neither the production custom-scheme
// origin nor any of those engines. `OP_SELFTEST` exists in the wire protocol for
// exactly this purpose and performs no I/O and touches no clip data.
//
// So this is a real product surface, deliberately kept as small as possible:
//
//   - It is opt-in via a single environment variable. Without it, nothing here
//     runs and the service below is never bound, so the normal app gains no
//     method.
//
//     An env var rather than a CLI flag on purpose: Wails v2 parses os.Args itself
//     and an unrecognized flag drops it into dev-mode argument handling, which
//     fails with an unrelated "unable to infer the AssetDir" error. The app already
//     uses MAHPASTES_START_HIDDEN and MAHPASTES_DATA_DIR, so this matches house
//     convention and cannot collide with Wails' own flags.
//   - The frontend detects the mode by the *presence* of the bound service
//     rather than by a flag it would have to be told about.
//   - It reports JSON on stdout and sets the exit code, which is what makes it
//     usable from CI on all three OSes without a human reading a window.
const selfTestWorkerEnv = "MAHPASTES_SELFTEST_WORKER"

// How long the frontend has to load the bundle, probe the worker and report.
// Generous: a cold CI runner starting a webview is slow, and a false timeout here
// would read as a platform failure.
const selfTestTimeout = 90 * time.Second

func wantsWorkerSelfTest() bool {
	return os.Getenv(selfTestWorkerEnv) == "1"
}

// SelfTestService is bound only in self-test mode. The frontend checks for it and
// stays silent when it is absent, so a normal run is untouched.
type SelfTestService struct {
	mu     sync.Mutex
	bridge *wailsbridge.Bridge
	result string
	done   chan struct{}
	once   sync.Once
}

func NewSelfTestService() *SelfTestService {
	return &SelfTestService{done: make(chan struct{})}
}

func (s *SelfTestService) setBridge(bridge *wailsbridge.Bridge) {
	s.mu.Lock()
	s.bridge = bridge
	s.mu.Unlock()
}

// ReportWorkerSelfTest receives the frontend's JSON result and ends the run.
//
// Bound to the frontend, so it must tolerate being called more than once and must
// not block the caller.
func (s *SelfTestService) ReportWorkerSelfTest(payload string) {
	s.mu.Lock()
	if s.result == "" {
		s.result = payload
	}
	bridge := s.bridge
	s.mu.Unlock()

	s.once.Do(func() {
		close(s.done)
		// Quitting from the bound call would deadlock the runtime on some
		// platforms, so hand it to the scheduler.
		go func() {
			time.Sleep(50 * time.Millisecond)
			bridge.Quit()
		}()
	})
}

// startWatchdog quits the app if the frontend never reports, so CI fails with a
// timeout result rather than hanging until the job is killed.
func (s *SelfTestService) startWatchdog() {
	go func() {
		select {
		case <-s.done:
		case <-time.After(selfTestTimeout):
			s.mu.Lock()
			if s.result == "" {
				s.result = `{"ok":false,"reason":"timeout","detail":"the frontend did not report a worker self-test result"}`
			}
			bridge := s.bridge
			s.mu.Unlock()
			bridge.Quit()
		}
	}()
}

// finish prints the result and returns the process exit code. Called after
// wails.Run returns, because that is the only point at which the webview is
// guaranteed to be gone.
func (s *SelfTestService) finish() int {
	s.mu.Lock()
	raw := s.result
	s.mu.Unlock()

	if raw == "" {
		raw = `{"ok":false,"reason":"no-result","detail":"the app exited before reporting"}`
	}

	// Echoed verbatim when it does not parse: a malformed payload is itself the
	// finding, and rewriting it would hide what the surface actually said.
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		os.Stdout.WriteString(raw + "\n")
		return 1
	}
	pretty, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		os.Stdout.WriteString(raw + "\n")
		return 1
	}
	os.Stdout.Write(append(pretty, '\n'))

	if ok, _ := parsed["ok"].(bool); ok {
		return 0
	}
	return 1
}
