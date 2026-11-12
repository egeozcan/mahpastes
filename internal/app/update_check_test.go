package app

import (
	"context"
	"net/http"
	"net/http/httptest"
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
