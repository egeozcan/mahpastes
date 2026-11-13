package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func releaseServer(t *testing.T, status int, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("missing User-Agent")
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("request must be anonymous")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestFetchLatestRelease(t *testing.T) {
	newer := `{"tag_name":"v1.6.0","html_url":"https://github.com/egeozcan/mahpastes/releases/tag/v1.6.0"}`
	cases := []struct {
		name    string
		status  int
		body    string
		current string
		want    string // expected Version, "" for none
		wantErr bool
	}{
		{"newer", 200, newer, "v1.5.0", "1.6.0", false},
		{"same", 200, newer, "v1.6.0", "", false},
		{"older remote", 200, newer, "v1.7.0", "", false},
		{"describe suffix of same tag", 200, newer, "v1.6.0-3-gabc123", "", false},
		{"describe suffix of same tag, dirty", 200, newer, "1.6.0-12-gabc1234-dirty", "", false},
		{"describe suffix of older tag", 200, newer, "1.5.0-12-gabc1234", "1.6.0", false},
		{"build metadata of same tag", 200, newer, "1.6.0+dirty", "", false},
		{"release candidate of same number", 200, newer, "1.6.0-rc1", "1.6.0", false},
		{"dotted release candidate of same number", 200, newer, "1.6.0-rc.1", "1.6.0", false},
		{"describe of a release candidate tag", 200, newer, "1.6.0-rc1-3-gabc1234", "1.6.0", false},
		{"pseudo-version before same number", 200, newer, "1.6.0-0.20261112044129-d68ff3a2acc6", "1.6.0", false},
		{"dirty pseudo-version before same number", 200, newer, "1.6.0-0.20261112044129-d68ff3a2acc6+dirty", "1.6.0", false},
		{"release candidate of a later number", 200, newer, "1.6.1-rc1", "", false},
		{"remote pre-release tag of same number", 200, `{"tag_name":"v1.6.0-rc2"}`, "1.6.0-rc1", "", false},
		{"dev build never matches", 200, newer, "dev", "", false},
		{"prerelease ignored", 200, `{"tag_name":"v2.0.0","prerelease":true}`, "v1.5.0", "", false},
		{"draft ignored", 200, `{"tag_name":"v2.0.0","draft":true}`, "v1.5.0", "", false},
		{"foreign url falls back", 200, `{"tag_name":"v1.6.0","html_url":"https://evil.example/x"}`, "v1.5.0", "1.6.0", false},
		{"http error", 403, `{}`, "v1.5.0", "", true},
		{"bad json", 200, `nope`, "v1.5.0", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rel, err := FetchLatestRelease(context.Background(), nil, releaseServer(t, c.status, c.body), c.current)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			got := ""
			if rel != nil {
				got = rel.Version
			}
			if got != c.want {
				t.Fatalf("version = %q, want %q", got, c.want)
			}
			if rel != nil && rel.URL[:19] != "https://github.com/" {
				t.Fatalf("url = %q", rel.URL)
			}
		})
	}
}

// countingReleaseServer serves a v9.0.0 release and counts requests.
func countingReleaseServer(t *testing.T) (string, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"tag_name":"v9.0.0","html_url":"https://github.com/egeozcan/mahpastes/releases/tag/v9.0.0"}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &hits
}

func newTestReleaseChecker(t *testing.T) (*ReleaseChecker, *atomic.Int32) {
	t.Helper()
	url, hits := countingReleaseServer(t)
	c := NewReleaseChecker(newSettingsTestApp(t), "v1.5.0")
	c.endpoint = url
	// The periodic loop never fires on its own in these tests; they call
	// check directly.
	c.startDelay = time.Hour
	return c, hits
}

func TestUpdateCheckSettingDefaultsOnAndPersists(t *testing.T) {
	a := newSettingsTestApp(t)
	if !a.UpdateCheckEnabled() {
		t.Fatal("update check should default to on")
	}
	if err := a.SetUpdateCheckEnabled(false); err != nil {
		t.Fatal(err)
	}
	if a.UpdateCheckEnabled() {
		t.Fatal("update check still on after opting out")
	}
	if (&App{}).UpdateCheckEnabled() {
		t.Fatal("an App without a database must never check")
	}
}

func TestUpdateCheckDueGate(t *testing.T) {
	a := newSettingsTestApp(t)
	now := time.Now()
	if !a.UpdateCheckDue(now) {
		t.Fatal("a check that never ran is due")
	}
	a.MarkUpdateChecked(now)
	if a.UpdateCheckDue(now.Add(UpdateCheckInterval - time.Minute)) {
		t.Fatal("check repeated within the interval")
	}
	if !a.UpdateCheckDue(now.Add(UpdateCheckInterval)) {
		t.Fatal("check not due after the interval")
	}
}

func TestReleaseCheckerNeverRequestsBeforeStart(t *testing.T) {
	c, hits := newTestReleaseChecker(t)
	// Headless runs never call Start; toggling the setting there (as the
	// e2e suite does) must not reach the network.
	if err := c.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if err := c.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	c.check(true)
	if n := hits.Load(); n != 0 {
		t.Fatalf("%d requests before Start, want 0", n)
	}
	if c.Latest() != nil {
		t.Fatal("Latest set without a check")
	}
}

func TestReleaseCheckerDailyGateNotifyAndOptOut(t *testing.T) {
	c, hits := newTestReleaseChecker(t)
	notified := make(chan ReleaseInfo, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx, func(rel ReleaseInfo) { notified <- rel })
	defer c.Stop()

	c.check(false)
	if n := hits.Load(); n != 1 {
		t.Fatalf("first check sent %d requests, want 1", n)
	}
	select {
	case rel := <-notified:
		if rel.Version != "9.0.0" {
			t.Fatalf("notified %+v", rel)
		}
	default:
		t.Fatal("no notification for a newer release")
	}
	if rel := c.Latest(); rel == nil || rel.Version != "9.0.0" {
		t.Fatalf("Latest() = %+v", rel)
	}

	c.check(false)
	if n := hits.Load(); n != 1 {
		t.Fatalf("second check within a day sent a request (%d total)", n)
	}

	if err := c.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if c.Latest() != nil {
		t.Fatal("opting out must forget the found release")
	}
	c.check(true)
	if n := hits.Load(); n != 1 {
		t.Fatalf("check ran while opted out (%d requests)", n)
	}
}

func TestReleaseCheckerFailedAttemptCountsTowardsGate(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	c := NewReleaseChecker(newSettingsTestApp(t), "v1.5.0")
	c.endpoint = srv.URL
	c.startDelay = time.Hour
	c.Start(context.Background(), nil)
	defer c.Stop()

	c.check(false)
	c.check(false)
	if n := hits.Load(); n != 1 {
		t.Fatalf("a failing endpoint was asked %d times within a day, want 1", n)
	}
}

func TestReleaseCheckerStopsRequesting(t *testing.T) {
	c, hits := newTestReleaseChecker(t)
	c.Start(context.Background(), nil)
	c.Stop()
	c.check(true)
	if err := c.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("%d requests after Stop, want 0", n)
	}
}

func TestAPIStatusReportsVersion(t *testing.T) {
	m := NewAPIManager(newSettingsTestApp(t))
	if v := m.GetStatus().Version; v == "" {
		t.Fatal("stopped API status has no version")
	}
}

// A found release used to live only in memory while the daily gate was
// persisted, so a restart within a day showed no notice and sent no request.
func TestReleaseCheckerRemembersFoundReleaseAcrossRestart(t *testing.T) {
	url, hits := countingReleaseServer(t)
	a := newSettingsTestApp(t)
	restart := func(current string) *ReleaseChecker {
		c := NewReleaseChecker(a, current)
		c.endpoint = url
		c.startDelay = time.Hour
		return c
	}

	first := restart("v1.5.0")
	first.Start(context.Background(), nil)
	first.check(false)
	first.Stop()

	second := restart("v1.5.0")
	second.Start(context.Background(), nil)
	defer second.Stop()
	second.check(false)
	if n := hits.Load(); n != 1 {
		t.Fatalf("%d requests, want 1 (the gate still holds)", n)
	}
	rel := second.Latest()
	if rel == nil || rel.Version != "9.0.0" || rel.URL != "https://github.com/egeozcan/mahpastes/releases/tag/v9.0.0" {
		t.Fatalf("Latest() after restart = %+v, want 9.0.0", rel)
	}

	// Never started (headless): reads the stored release, sends nothing.
	if rel := restart("v1.5.0").Latest(); rel == nil || rel.Version != "9.0.0" {
		t.Fatalf("unstarted Latest() = %+v", rel)
	}
	// The update was installed, or this is an unstamped build.
	for _, current := range []string{"v9.0.0", "v9.1.0", "dev"} {
		if rel := restart(current).Latest(); rel != nil {
			t.Errorf("current %s: Latest() = %+v, want nil", current, rel)
		}
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("reading the stored release sent a request (%d total)", n)
	}

	// Opting out forgets it for good, also after opting back in.
	if err := second.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if v, _ := a.GetSetting(settingUpdateCheckFound); v != "" {
		t.Fatalf("opt-out left %s = %q", settingUpdateCheckFound, v)
	}
	if err := a.SetUpdateCheckEnabled(true); err != nil {
		t.Fatal(err)
	}
	if rel := restart("v1.5.0").Latest(); rel != nil {
		t.Fatalf("Latest() after opt-out and back = %+v, want nil", rel)
	}
}

// The URL is opened in the browser, and the row was once writable through
// REST and restores: only this project's releases page or one of its tags
// survives, not any github.com page.
func TestReleaseCheckerValidatesStoredRelease(t *testing.T) {
	a := newSettingsTestApp(t)
	c := NewReleaseChecker(a, "v1.5.0")
	for _, tc := range []struct{ stored, wantVersion, wantURL string }{
		{`{"version":"9.0.0","url":"https://evil.example/x"}`, "9.0.0", ReleasesPageURL},
		{`{"version":"v9.0.0","url":""}`, "9.0.0", ReleasesPageURL},
		{`{"version":"99.0.0","url":"https://github.com/attacker/repo/releases"}`, "99.0.0", ReleasesPageURL},
		{`{"version":"99.0.0","url":"https://github.com/egeozcan/mahpastes-evil/releases"}`, "99.0.0", ReleasesPageURL},
		{`{"version":"99.0.0","url":"https://github.com/egeozcan/mahpastes/releases/../../../attacker/repo"}`, "99.0.0", ReleasesPageURL},
		{`{"version":"99.0.0","url":"https://github.com/egeozcan/mahpastes/releases/tag/../../../../attacker"}`, "99.0.0", ReleasesPageURL},
		{`{"version":"99.0.0","url":"https://github.com/egeozcan/mahpastes/releases/tag/v99.0.0?x=1"}`, "99.0.0", ReleasesPageURL},
		{`{"version":"99.0.0","url":"https://github.com/egeozcan/mahpastes/releases/tag/v99.0.0\nhttps://evil.example"}`, "99.0.0", ReleasesPageURL},
		{`{"version":"9.0.0","url":"https://github.com/egeozcan/mahpastes/releases/tag/v9.0.0"}`, "9.0.0", ReleasesPageURL + "/tag/v9.0.0"},
		{`{"version":"9.0.0","url":"https://github.com/egeozcan/mahpastes/releases"}`, "9.0.0", ReleasesPageURL},
		{`{"version":"1.5.0","url":"https://github.com/x"}`, "", ""},
		{`{"version":"<b>new</b>","url":"https://github.com/x"}`, "", ""},
		{`not json`, "", ""},
		{``, "", ""},
	} {
		if err := a.SetSetting(settingUpdateCheckFound, tc.stored); err != nil {
			t.Fatal(err)
		}
		rel := c.Latest()
		if tc.wantVersion == "" {
			if rel != nil {
				t.Errorf("stored %q: Latest() = %+v, want nil", tc.stored, rel)
			}
			continue
		}
		if rel == nil || rel.Version != tc.wantVersion || rel.URL != tc.wantURL {
			t.Errorf("stored %q: Latest() = %+v, want %s %s", tc.stored, rel, tc.wantVersion, tc.wantURL)
		}
	}
}

// The stored release is judged like a fetched one: a release candidate still
// shows the final release of its number after a restart, and a build made
// after that release does not.
func TestReleaseCheckerStoredReleaseForPreReleaseBuild(t *testing.T) {
	a := newSettingsTestApp(t)
	if err := a.SetSetting(settingUpdateCheckFound, `{"version":"1.6.0","url":""}`); err != nil {
		t.Fatal(err)
	}
	if rel := NewReleaseChecker(a, "1.6.0-rc1").Latest(); rel == nil || rel.Version != "1.6.0" {
		t.Errorf("release candidate: Latest() = %+v, want 1.6.0", rel)
	}
	for _, current := range []string{"1.6.0", "1.6.0-12-gabc1234"} {
		if rel := NewReleaseChecker(a, current).Latest(); rel != nil {
			t.Errorf("%s: Latest() = %+v, want nil", current, rel)
		}
	}
}

// A later check that finds nothing newer (the release was pulled) clears the
// stored one.
func TestReleaseCheckerClearsStoredReleaseWhenNoneIsNewer(t *testing.T) {
	a := newSettingsTestApp(t)
	if err := a.SetSetting(settingUpdateCheckFound, `{"version":"9.0.0","url":""}`); err != nil {
		t.Fatal(err)
	}
	c := NewReleaseChecker(a, "v1.5.0")
	c.endpoint = releaseServer(t, http.StatusOK, `{"tag_name":"v1.5.0"}`)
	c.startDelay = time.Hour
	c.Start(context.Background(), nil)
	defer c.Stop()
	if c.Latest() == nil {
		t.Fatal("stored release not read")
	}
	c.check(true)
	if rel := c.Latest(); rel != nil {
		t.Fatalf("Latest() = %+v after a check found nothing newer", rel)
	}
	if v, _ := a.GetSetting(settingUpdateCheckFound); v != "" {
		t.Fatalf("%s = %q, want it cleared", settingUpdateCheckFound, v)
	}
}

// A failed check says nothing about the release found earlier.
func TestReleaseCheckerKeepsStoredReleaseOnFailedCheck(t *testing.T) {
	a := newSettingsTestApp(t)
	if err := a.SetSetting(settingUpdateCheckFound, `{"version":"9.0.0","url":""}`); err != nil {
		t.Fatal(err)
	}
	c := NewReleaseChecker(a, "v1.5.0")
	c.endpoint = releaseServer(t, http.StatusForbidden, ``)
	c.startDelay = time.Hour
	c.Start(context.Background(), nil)
	defer c.Stop()
	c.check(true)
	if rel := c.Latest(); rel == nil || rel.Version != "9.0.0" {
		t.Fatalf("Latest() = %+v after a failed check, want 9.0.0", rel)
	}
}

// A fetched release is held to the same rule: the endpoint's html_url is
// opened in the browser too.
func TestFetchLatestReleaseKeepsOnlyProjectURLs(t *testing.T) {
	for body, want := range map[string]string{
		`{"tag_name":"v9.0.0","html_url":"https://github.com/egeozcan/mahpastes/releases/tag/v9.0.0"}`: ReleasesPageURL + "/tag/v9.0.0",
		`{"tag_name":"v9.0.0","html_url":"https://github.com/attacker/repo/releases/tag/v9.0.0"}`:      ReleasesPageURL,
	} {
		rel, err := FetchLatestRelease(context.Background(), nil, releaseServer(t, http.StatusOK, body), "v1.5.0")
		if err != nil || rel == nil {
			t.Fatalf("%s: %+v, %v", body, rel, err)
		}
		if rel.URL != want {
			t.Errorf("%s: url = %q, want %q", body, rel.URL, want)
		}
	}
}

// The check's own records are this machine's: a backup neither carries nor
// sets them, and a restore leaves this machine's values alone.
func TestRestoreBackupCannotPlantUpdateNotice(t *testing.T) {
	restoreDataDir(t)
	src := newBackupTestDB(t)
	mustExecSQL(t, src, `INSERT INTO settings (key, value) VALUES ('update_check_found', '{"version":"8.0.0","url":""}')`)
	mustExecSQL(t, src, `INSERT INTO settings (key, value) VALUES ('update_check_last', '99999999999')`)
	good := filepath.Join(t.TempDir(), "good.zip")
	if err := (&App{db: src}).CreateBackup(good); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	crafted := rewriteBackupSQL(t, good,
		`INSERT INTO settings (key, value) VALUES ('update_check_found', '{"version":"99.0.0","url":"https://github.com/attacker/repo/releases"}');`+"\n"+
			`INSERT INTO settings (key, value) VALUES ('update_check_last', '99999999999');`+"\n")

	for _, backup := range []string{good, crafted} {
		dst := newBackupTestDB(t)
		app := &App{db: dst}
		if err := app.RestoreBackup(backup, "none"); err != nil {
			t.Fatalf("RestoreBackup: %v", err)
		}
		if v, ok := settingRow(t, dst, settingUpdateCheckFound); ok {
			t.Errorf("a backup set %s = %q", settingUpdateCheckFound, v)
		}
		if v, ok := settingRow(t, dst, settingUpdateCheckLast); ok {
			t.Errorf("a backup set %s = %q", settingUpdateCheckLast, v)
		}
		if rel := NewReleaseChecker(app, "v1.5.0").Latest(); rel != nil {
			t.Errorf("Latest() after restore = %+v, want nil", rel)
		}
	}

	dst := newBackupTestDB(t)
	app := &App{db: dst}
	app.storeFoundRelease(&ReleaseInfo{Version: "2.0.0", URL: ReleasesPageURL + "/tag/v2.0.0"})
	if err := app.RestoreBackup(crafted, "none"); err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}
	if rel := NewReleaseChecker(app, "v1.5.0").Latest(); rel == nil || rel.Version != "2.0.0" || rel.URL != ReleasesPageURL+"/tag/v2.0.0" {
		t.Errorf("Latest() after restore = %+v, want this machine's 2.0.0", rel)
	}
}

// PUT /api/v1/settings/{key} is how a headless admin key reaches the settings
// table; the check's own records are not its to write.
func TestUpdateCheckStateIsNotWritableOverREST(t *testing.T) {
	app := newSettingsTestApp(t)
	manager := NewAPIManager(app)
	planted := `{"version":"99.0.0","url":"https://github.com/attacker/repo/releases"}`
	for key, value := range map[string]string{settingUpdateCheckFound: planted, settingUpdateCheckLast: "99999999999"} {
		body, _ := json.Marshal(map[string]string{"value": value})
		req := httptest.NewRequest("PUT", "/api/v1/settings/"+key, bytes.NewReader(body))
		req.SetPathValue("key", key)
		rec := httptest.NewRecorder()
		manager.handleSetSetting(rec, withKey(req, &apiKeyContext{KeyID: 1, Role: "admin"}))
		if rec.Code != http.StatusForbidden {
			t.Errorf("PUT %s: code = %d, want 403", key, rec.Code)
		}
		if v, _ := app.GetSetting(key); v != "" {
			t.Errorf("PUT %s stored %q despite the refusal", key, v)
		}
	}
	if rel := NewReleaseChecker(app, "v1.5.0").Latest(); rel != nil {
		t.Errorf("Latest() = %+v after refused writes", rel)
	}
	if isDesktopOnlySetting(SettingUpdateCheck) {
		t.Errorf("%s (the user's choice) is refused over REST", SettingUpdateCheck)
	}
}
