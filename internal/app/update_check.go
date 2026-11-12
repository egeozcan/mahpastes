package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-clipboard/plugin"
)

// SettingUpdateCheck switches the "new release available" check. It is on
// unless the value is "false". The check sends one anonymous GET to GitHub's
// public latest-release endpoint (no identifiers, no version in the request).
const SettingUpdateCheck = "update_check"

// settingUpdateCheckLast records (unix seconds) when the check last ran, so
// launching the app repeatedly does not repeat the request.
const settingUpdateCheckLast = "update_check_last"

// UpdateCheckInterval is the minimum time between two checks.
const UpdateCheckInterval = 24 * time.Hour

// LatestReleaseURL is GitHub's latest published (non-draft, non-prerelease)
// release of mahpastes.
const LatestReleaseURL = "https://api.github.com/repos/egeozcan/mahpastes/releases/latest"

// ReleasesPageURL is where a user downloads a new release.
const ReleasesPageURL = "https://github.com/egeozcan/mahpastes/releases"

// ReleaseInfo describes a published release that is newer than this build.
type ReleaseInfo struct {
	Version string `json:"version"`
	URL     string `json:"url"`
}

// FetchLatestRelease asks the releases endpoint for the newest release. It
// returns a non-nil ReleaseInfo only when that release is newer than
// current; a "dev" or otherwise unparsable current version never matches.
func FetchLatestRelease(ctx context.Context, client *http.Client, endpoint, current string) (*ReleaseInfo, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "mahpastes-update-check")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("release check: HTTP %d", resp.StatusCode)
	}
	var body struct {
		TagName    string `json:"tag_name"`
		HTMLURL    string `json:"html_url"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("release check: %w", err)
	}
	if body.Draft || body.Prerelease || body.TagName == "" {
		return nil, nil
	}
	if !isReleaseVersion(current) || !isReleaseVersion(body.TagName) || !plugin.IsNewerVersion(current, body.TagName) {
		return nil, nil
	}
	url := body.HTMLURL
	if !strings.HasPrefix(url, "https://github.com/") {
		url = ReleasesPageURL
	}
	return &ReleaseInfo{Version: strings.TrimPrefix(body.TagName, "v"), URL: url}, nil
}

// isReleaseVersion reports whether v looks like vX.Y.Z (optionally with a
// pre-release or build suffix) so semver comparison is meaningful.
func isReleaseVersion(v string) bool {
	parts := strings.SplitN(strings.TrimPrefix(v, "v"), ".", 3)
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		p = strings.SplitN(strings.SplitN(p, "-", 2)[0], "+", 2)[0]
		if _, err := strconv.Atoi(p); err != nil {
			return false
		}
	}
	return true
}

// UpdateCheckEnabled reports the persisted setting (default on).
func (a *App) UpdateCheckEnabled() bool {
	if a.db == nil {
		return false
	}
	v, err := a.GetSetting(SettingUpdateCheck)
	return err != nil || v != "false"
}

// SetUpdateCheckEnabled persists the setting.
func (a *App) SetUpdateCheckEnabled(enabled bool) error {
	return a.SetSetting(SettingUpdateCheck, strconv.FormatBool(enabled))
}

// UpdateCheckDue reports whether the last automatic check is older than
// UpdateCheckInterval.
func (a *App) UpdateCheckDue(now time.Time) bool {
	v, err := a.GetSetting(settingUpdateCheckLast)
	if err != nil || v == "" {
		return true
	}
	secs, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return true
	}
	return now.Sub(time.Unix(secs, 0)) >= UpdateCheckInterval
}

// MarkUpdateChecked records that a check ran at now.
func (a *App) MarkUpdateChecked(now time.Time) {
	_ = a.SetSetting(settingUpdateCheckLast, strconv.FormatInt(now.Unix(), 10))
}

// ReleaseChecker runs the opt-out release check for the desktop app. It sends
// nothing until Start, so headless runs (which never call Start) and tests
// never reach the network, and nothing after Stop.
type ReleaseChecker struct {
	app        *App
	current    string
	endpoint   string
	client     *http.Client
	startDelay time.Duration
	tick       time.Duration

	// runMu serialises checks so a toggle during the periodic check cannot
	// send a second request; Stop takes it to wait for an in-flight one.
	runMu sync.Mutex

	mu     sync.Mutex
	ctx    context.Context // nil until Start
	cancel context.CancelFunc
	notify func(ReleaseInfo)
	latest *ReleaseInfo
}

// NewReleaseChecker returns a checker comparing releases against current.
func NewReleaseChecker(a *App, current string) *ReleaseChecker {
	return &ReleaseChecker{
		app:      a,
		current:  current,
		endpoint: LatestReleaseURL,
		// Let the window finish loading so the first notice is not emitted
		// before the page listens.
		startDelay: 20 * time.Second,
		// The daily gate (UpdateCheckDue) decides; this only wakes it up.
		tick: time.Hour,
	}
}

// Start begins the periodic check; notify is called for every newer release
// found. A check that ran within UpdateCheckInterval is not repeated, and
// nothing is requested while the setting is off.
func (c *ReleaseChecker) Start(ctx context.Context, notify func(ReleaseInfo)) {
	ctx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	if c.cancel != nil {
		c.mu.Unlock()
		cancel()
		return
	}
	c.ctx, c.cancel, c.notify = ctx, cancel, notify
	c.mu.Unlock()
	go func() {
		timer := time.NewTimer(c.startDelay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return
		}
		ticker := time.NewTicker(c.tick)
		defer ticker.Stop()
		for {
			c.check(false)
			select {
			case <-ticker.C:
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Stop ends the checks and waits for a request in flight to be abandoned.
func (c *ReleaseChecker) Stop() {
	c.mu.Lock()
	cancel := c.cancel
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	// Taking runMu waits for a check in flight; the cancel above aborts its
	// request.
	c.runMu.Lock()
	defer c.runMu.Unlock()
}

// Latest returns the newer release found so far, or nil (also while the
// setting is off).
func (c *ReleaseChecker) Latest() *ReleaseInfo {
	if !c.app.UpdateCheckEnabled() {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.latest == nil {
		return nil
	}
	rel := *c.latest
	return &rel
}

// SetEnabled persists the opt-out. Turning it off forgets a release already
// found; turning it on checks right away, once Start has run.
func (c *ReleaseChecker) SetEnabled(enabled bool) error {
	if err := c.app.SetUpdateCheckEnabled(enabled); err != nil {
		return err
	}
	if !enabled {
		c.mu.Lock()
		c.latest = nil
		c.mu.Unlock()
		return nil
	}
	if c.started() {
		go c.check(true)
	}
	return nil
}

func (c *ReleaseChecker) started() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ctx != nil && c.ctx.Err() == nil
}

// check performs one release check. force skips the once-a-day gate. Every
// attempt counts towards the gate, so an offline or rate-limited machine
// retries the next day rather than every tick.
func (c *ReleaseChecker) check(force bool) {
	c.runMu.Lock()
	defer c.runMu.Unlock()
	c.mu.Lock()
	ctx, notify := c.ctx, c.notify
	c.mu.Unlock()
	if ctx == nil || ctx.Err() != nil || !c.app.UpdateCheckEnabled() {
		return
	}
	now := time.Now()
	if !force && !c.app.UpdateCheckDue(now) {
		return
	}
	c.app.MarkUpdateChecked(now)
	rctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	rel, err := FetchLatestRelease(rctx, c.client, c.endpoint, c.current)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("update check: %v", err)
		}
		return
	}
	if ctx.Err() != nil || !c.app.UpdateCheckEnabled() {
		return
	}
	c.mu.Lock()
	c.latest = rel
	c.mu.Unlock()
	if rel != nil && notify != nil {
		notify(*rel)
	}
}
