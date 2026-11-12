package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
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
