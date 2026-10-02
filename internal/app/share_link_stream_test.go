package app

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// insertBigClip stores n random bytes as a clip, with the content hash every
// production insert path writes, and returns its id and bytes.
func insertBigClip(t *testing.T, app *App, n int, filename string) (int64, []byte) {
	t.Helper()
	data := make([]byte, n)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	res, err := app.db.Exec("INSERT INTO clips (content_type, data, filename, content_hash) VALUES (?, ?, ?, ?)",
		"application/octet-stream", data, filename, computeContentHash(data))
	if err != nil {
		t.Fatalf("insert clip: %v", err)
	}
	id, _ := res.LastInsertId()
	return id, data
}

// dialStalledShareReader sends GET /s/<token> on a raw connection, reads only
// the status line and then stops reading — the shape of a paused download or a
// deliberate slow reader. The caller owns the returned connection.
func dialStalledShareReader(t *testing.T, srv *httptest.Server, token string) (net.Conn, string) {
	t.Helper()
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetReadBuffer(4096)
	}
	if _, err := conn.Write([]byte("GET /s/" + token + " HTTP/1.1\r\nHost: x\r\n\r\n")); err != nil {
		t.Fatalf("write request: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	line, err := bufio.NewReaderSize(conn, 16).ReadString('\n')
	if err != nil {
		t.Fatalf("read status line: %v", err)
	}
	return conn, line
}

func waitFor(t *testing.T, within time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

// A share-link client that stops reading must not pin a database connection or
// a read snapshot. Before the fix the handler held a read transaction across
// every network write, so one paused download kept a pooled connection checked
// out and stopped WAL checkpoints from getting past its snapshot — forever,
// since the server runs without a write timeout.
func TestShareLinkStream_StalledReaderReleasesDatabase(t *testing.T) {
	am, app, _, cleanup := setupShareLinkTest(t)
	defer cleanup()

	bigID, _ := insertBigClip(t, app, 16<<20, "big.bin")
	link, err := am.CreateShareLink(bigID, "", 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(shareMux(am))
	defer srv.Close()

	conn, status := dialStalledShareReader(t, srv, link.Token)
	defer conn.Close()
	if !strings.Contains(status, "200") {
		t.Fatalf("status line = %q, want 200", status)
	}

	// The client is still connected and still not reading.
	if !waitFor(t, 3*time.Second, func() bool { return app.db.Stats().InUse == 0 }) {
		t.Fatalf("a stalled share reader still holds %d DB connection(s)", app.db.Stats().InUse)
	}

	small := make([]byte, 2<<20)
	_, _ = rand.Read(small)
	if _, err := app.db.Exec("INSERT INTO clips (content_type, data, filename) VALUES (?, ?, ?)",
		"application/octet-stream", small, "s.bin"); err != nil {
		t.Fatal(err)
	}
	var busy, logFrames, checkpointed int
	if err := app.db.QueryRow("PRAGMA wal_checkpoint(PASSIVE)").Scan(&busy, &logFrames, &checkpointed); err != nil {
		t.Fatal(err)
	}
	if busy != 0 || checkpointed != logFrames {
		t.Fatalf("checkpoint held back by a stalled reader: busy=%d log=%d checkpointed=%d", busy, logFrames, checkpointed)
	}
}

// One-time links stay one-time through a real HTTP server: a Range request is
// answered with the whole file (never a partial that would spend the slot on a
// byte), HEAD does not consume, and the cap is enforced.
func TestShareLinkStream_OneTimeLinkIgnoresRangeOverHTTP(t *testing.T) {
	am, app, _, cleanup := setupShareLinkTest(t)
	defer cleanup()

	bigID, want := insertBigClip(t, app, 3<<20, "one-time.bin")
	link, err := am.CreateShareLink(bigID, "", 0, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(shareMux(am))
	defer srv.Close()

	head, err := http.Head(srv.URL + "/s/" + link.Token)
	if err != nil {
		t.Fatal(err)
	}
	head.Body.Close()
	if head.StatusCode != http.StatusOK {
		t.Fatalf("HEAD status = %d", head.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/s/"+link.Token, nil)
	req.Header.Set("Range", "bytes=0-0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ranged GET status = %d, want 200", resp.StatusCode)
	}
	if resp.Header.Get("Content-Range") != "" || resp.Header.Get("Accept-Ranges") != "" {
		t.Fatalf("share link advertised ranges: Content-Range=%q Accept-Ranges=%q",
			resp.Header.Get("Content-Range"), resp.Header.Get("Accept-Ranges"))
	}
	if resp.Header.Get("Content-Length") != strconv.Itoa(len(want)) {
		t.Fatalf("Content-Length = %q, want %d", resp.Header.Get("Content-Length"), len(want))
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("body differs from the clip (%d bytes, want %d)", len(got), len(want))
	}
	for name, wantValue := range map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"Content-Security-Policy": "default-src 'none'; sandbox",
		"Cache-Control":           "no-store",
		"Content-Type":            "application/octet-stream",
	} {
		if v := resp.Header.Get(name); v != wantValue {
			t.Errorf("%s = %q, want %q", name, v, wantValue)
		}
	}
	if v := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(v, "attachment") || !strings.Contains(v, "one-time.bin") {
		t.Errorf("Content-Disposition = %q", v)
	}

	again, err := http.Get(srv.URL + "/s/" + link.Token)
	if err != nil {
		t.Fatal(err)
	}
	again.Body.Close()
	if again.StatusCode != http.StatusNotFound {
		t.Fatalf("second GET of a one-time link = %d, want 404", again.StatusCode)
	}
	var count int64
	if err := app.db.QueryRow("SELECT download_count FROM share_links WHERE id = ?", link.Info.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("download_count = %d, want 1", count)
	}
}

// setForTest overrides a package-level knob for one test.
func setForTest[T any](t *testing.T, p *T, v T) {
	t.Helper()
	old := *p
	*p = v
	t.Cleanup(func() { *p = old })
}

func fetchShare(t *testing.T, srv *httptest.Server, token string) (int, []byte) {
	t.Helper()
	resp, err := http.Get(srv.URL + "/s/" + token)
	if err != nil {
		t.Errorf("GET: %v", err)
		return 0, nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("read body: %v", err)
	}
	return resp.StatusCode, body
}

func shareDownloadCount(t *testing.T, app *App, linkID int64) int64 {
	t.Helper()
	var count int64
	if err := app.db.QueryRow("SELECT download_count FROM share_links WHERE id = ?", linkID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

// The API server has no WriteTimeout (the SSE stream must stay open), so a
// client that stops reading would otherwise keep its handler — and whatever it
// holds — parked in Write forever. A sliding per-write deadline cuts it off.
func TestShareLinkStream_StalledReaderIsCutOff(t *testing.T) {
	setForTest(t, &clipStreamWriteTimeout, 200*time.Millisecond)
	am, app, _, cleanup := setupShareLinkTest(t)
	defer cleanup()

	bigID, _ := insertBigClip(t, app, 16<<20, "big.bin")
	link, err := am.CreateShareLink(bigID, "", 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /s/{token}", func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		am.handleShareView(w, r)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	conn, _ := dialStalledShareReader(t, srv, link.Token)
	defer conn.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("share handler still blocked writing to a client that stopped reading")
	}
}

// The per-write deadline is set on the connection itself. A deadline left
// behind by one download must not fail the next response on the same
// keep-alive connection (net/http clears it after each response; this guards
// that the streaming path keeps relying on something that holds).
func TestShareLinkStream_WriteDeadlineDoesNotLeakIntoKeepAlive(t *testing.T) {
	setForTest(t, &clipStreamWriteTimeout, 100*time.Millisecond)
	am, app, _, cleanup := setupShareLinkTest(t)
	defer cleanup()

	bigID, want := insertBigClip(t, app, 3<<20, "big.bin")
	link, err := am.CreateShareLink(bigID, "", 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(shareMux(am))
	defer srv.Close()

	var conns int
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			conns++
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}}
	for i := 0; i < 2; i++ {
		resp, err := client.Get(srv.URL + "/s/" + link.Token)
		if err != nil {
			t.Fatalf("download %d: %v", i+1, err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK || !bytes.Equal(body, want) {
			t.Fatalf("download %d: status %d, %d bytes, err %v", i+1, resp.StatusCode, len(body), err)
		}
		time.Sleep(3 * clipStreamWriteTimeout)
	}
	if conns != 1 {
		t.Fatalf("downloads used %d connections, want 1 reused keep-alive connection", conns)
	}
}

// Concurrent /s/ streams are capped globally. A request over the cap is turned
// away before it claims a download slot, so it cannot spend a limited link.
func TestShareLinkStream_GlobalConcurrencyCap(t *testing.T) {
	setForTest(t, &clipStreamWriteTimeout, 10*time.Second)
	setForTest(t, &shareStreamMaxConcurrent, 2)
	setForTest(t, &shareStreamMaxPerIP, 100)
	am, app, _, cleanup := setupShareLinkTest(t)
	defer cleanup()

	bigID, want := insertBigClip(t, app, 16<<20, "big.bin")
	open, err := am.CreateShareLink(bigID, "", 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	limited, err := am.CreateShareLink(bigID, "", 0, 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(shareMux(am))
	defer srv.Close()

	c1, _ := dialStalledShareReader(t, srv, open.Token)
	c2, _ := dialStalledShareReader(t, srv, open.Token)

	status, _ := fetchShare(t, srv, limited.Token)
	if status != http.StatusServiceUnavailable {
		c1.Close()
		c2.Close()
		t.Fatalf("request over the global cap = %d, want 503", status)
	}
	if n := shareDownloadCount(t, app, limited.Info.ID); n != 0 {
		t.Fatalf("a rejected request consumed a download slot: download_count = %d", n)
	}

	// Freeing the streams frees their slots.
	c1.Close()
	c2.Close()
	var body []byte
	if !waitFor(t, 3*time.Second, func() bool {
		status, body = fetchShare(t, srv, limited.Token)
		return status == http.StatusOK
	}) {
		t.Fatalf("after the stalled readers left, status = %d, want 200", status)
	}
	if !bytes.Equal(body, want) {
		t.Fatal("body differs from the clip")
	}
}

// One client address cannot take every stream slot for itself.
func TestShareLinkStream_PerIPConcurrencyCap(t *testing.T) {
	setForTest(t, &clipStreamWriteTimeout, 10*time.Second)
	setForTest(t, &shareStreamMaxConcurrent, 100)
	setForTest(t, &shareStreamMaxPerIP, 1)
	am, app, _, cleanup := setupShareLinkTest(t)
	defer cleanup()

	bigID, _ := insertBigClip(t, app, 16<<20, "big.bin")
	link, err := am.CreateShareLink(bigID, "", 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(shareMux(am))
	defer srv.Close()

	c1, _ := dialStalledShareReader(t, srv, link.Token)
	defer c1.Close()
	if status, _ := fetchShare(t, srv, link.Token); status != http.StatusTooManyRequests {
		t.Fatalf("second concurrent stream from one address = %d, want 429", status)
	}
}

// Every download of a large clip used to re-read the blob in 1 MB SUBSTR
// chunks, and SQLite materializes the whole value for each one — quadratic CPU
// and clip-size memory per chunk in flight. The clip is now copied out once
// per revision and every download, concurrent or not, streams that copy.
func TestShareLinkStream_MaterializesOncePerRevision(t *testing.T) {
	setForTest(t, &shareStreamMaxPerIP, 100) // the burst below all comes from 127.0.0.1
	am, app, _, cleanup := setupShareLinkTest(t)
	defer cleanup()

	bigID, want := insertBigClip(t, app, 3<<20, "movie.bin")
	link, err := am.CreateShareLink(bigID, "", 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(shareMux(am))
	defer srv.Close()

	check := func(label string, want []byte) {
		t.Helper()
		status, body := fetchShare(t, srv, link.Token)
		if status != http.StatusOK || !bytes.Equal(body, want) {
			t.Fatalf("%s: status %d, %d bytes (want 200 and the %d-byte clip)", label, status, len(body), len(want))
		}
	}

	// A cold burst shares one copy.
	const burst = 8
	errs := make(chan string, burst)
	for i := 0; i < burst; i++ {
		go func() {
			resp, err := http.Get(srv.URL + "/s/" + link.Token)
			if err != nil {
				errs <- err.Error()
				return
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK || !bytes.Equal(body, want) {
				errs <- "concurrent download: status " + strconv.Itoa(resp.StatusCode) + ", " + strconv.Itoa(len(body)) + " bytes"
				return
			}
			errs <- ""
		}()
	}
	for i := 0; i < burst; i++ {
		if msg := <-errs; msg != "" {
			t.Fatal(msg)
		}
	}
	for i := 0; i < 4; i++ {
		check("sequential download", want)
	}
	if n := app.tempStore.materialized.Load(); n != 1 {
		t.Fatalf("clip materialized %d times for %d downloads, want once", n, burst+4)
	}

	// A new revision is copied once and served.
	edited := bytes.Repeat([]byte("v2"), 2<<20)
	if err := app.UpdateClipData(bigID, "application/octet-stream", base64.StdEncoding.EncodeToString(edited), "movie.bin"); err != nil {
		t.Fatal(err)
	}
	check("after UpdateClipData", edited)
	check("after UpdateClipData, again", edited)
	if n := app.tempStore.materialized.Load(); n != 2 {
		t.Fatalf("materialized = %d after one edit, want 2", n)
	}

	// A writer that changes the bytes but forgets to drop the leased file is
	// still caught: reuse is keyed on the stored content hash, not on the file
	// merely existing.
	raw := bytes.Repeat([]byte("v3"), 2<<20)
	if _, err := app.db.Exec("UPDATE clips SET data = ?, content_hash = ? WHERE id = ?", raw, computeContentHash(raw), bigID); err != nil {
		t.Fatal(err)
	}
	check("after a raw UPDATE", raw)
}

// An edit that lands after a snapshot's bytes were read but before the copy is
// published must not leave the old revision on disk as a published snapshot,
// a plaintext copy of bytes the library no longer holds. The request that
// overlapped the edit may get either revision; nothing after it may get the
// old one.
func TestShareLinkStream_EditDuringCopyIsNotPublished(t *testing.T) {
	setForTest(t, &shareStreamMaxPerIP, 100)
	am, app, _, cleanup := setupShareLinkTest(t)
	defer cleanup()

	bigID, original := insertBigClip(t, app, 3<<20, "race.bin")
	link, err := am.CreateShareLink(bigID, "", 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(shareMux(am))
	defer srv.Close()

	edited := bytes.Repeat([]byte("ed"), 2<<20)
	editOnce := false
	setForTest(t, &clipSnapshotBeforePublish, func(clipID int64) {
		if clipID != bigID || editOnce {
			return
		}
		editOnce = true
		if err := app.UpdateClipData(bigID, "application/octet-stream", base64.StdEncoding.EncodeToString(edited), "race.bin"); err != nil {
			t.Errorf("edit: %v", err)
		}
	})

	status, body := fetchShare(t, srv, link.Token)
	if status != http.StatusOK || !(bytes.Equal(body, original) || bytes.Equal(body, edited)) {
		t.Fatalf("overlapping download: status %d, %d bytes", status, len(body))
	}
	stale := filepath.Join(app.tempDir, streamSnapshotName(bigID, computeContentHash(original)))
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("the pre-edit copy was published at %s (stat: %v)", stale, err)
	}
	if status, body := fetchShare(t, srv, link.Token); status != http.StatusOK || !bytes.Equal(body, edited) {
		t.Fatalf("download after the edit: status %d, %d bytes, want the edited clip", status, len(body))
	}
}

// The authenticated data endpoint shares the streaming path and keeps ranges:
// a video seeking through a large clip reads one materialized copy rather than
// re-walking the blob for every range.
func TestClipDataEndpoint_RangesServedFromOneSnapshot(t *testing.T) {
	am, app, _, cleanup := setupShareLinkTest(t)
	defer cleanup()

	bigID, want := insertBigClip(t, app, 3<<20, "clip.mp4")
	id := strconv.FormatInt(bigID, 10)
	for _, tc := range []struct {
		rangeHeader string
		status      int
		start, end  int
	}{
		{"", http.StatusOK, 0, len(want) - 1},
		{"bytes=0-9", http.StatusPartialContent, 0, 9},
		{"bytes=1048576-1048585", http.StatusPartialContent, 1048576, 1048585},
		{"bytes=-5", http.StatusPartialContent, len(want) - 5, len(want) - 1},
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/clips/"+id+"/data", nil)
		req.SetPathValue("id", id)
		if tc.rangeHeader != "" {
			req.Header.Set("Range", tc.rangeHeader)
		}
		rec := httptest.NewRecorder()
		am.handleGetClipData(rec, withKey(req, &apiKeyContext{KeyID: 1, Role: "viewer"}))
		if rec.Code != tc.status {
			t.Fatalf("Range %q: status %d, want %d", tc.rangeHeader, rec.Code, tc.status)
		}
		if !bytes.Equal(rec.Body.Bytes(), want[tc.start:tc.end+1]) {
			t.Fatalf("Range %q: wrong bytes", tc.rangeHeader)
		}
		if rec.Header().Get("Accept-Ranges") != "bytes" {
			t.Fatalf("Range %q: Accept-Ranges = %q", tc.rangeHeader, rec.Header().Get("Accept-Ranges"))
		}
	}
	if n := app.tempStore.materialized.Load(); n != 1 {
		t.Fatalf("clip materialized %d times for 4 requests, want once", n)
	}
}

// The authenticated endpoint has the same no-write-timeout exposure as /s/.
func TestClipDataEndpoint_StalledReaderReleasesDatabaseAndIsCutOff(t *testing.T) {
	setForTest(t, &clipStreamWriteTimeout, 200*time.Millisecond)
	am, app, _, cleanup := setupShareLinkTest(t)
	defer cleanup()

	bigID, _ := insertBigClip(t, app, 16<<20, "big.bin")
	done := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/clips/{id}/data", func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		am.handleGetClipData(w, withKey(r, &apiKeyContext{KeyID: 1, Role: "viewer"}))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetReadBuffer(4096)
	}
	if _, err := conn.Write([]byte("GET /api/v1/clips/" + strconv.FormatInt(bigID, 10) + "/data HTTP/1.1\r\nHost: x\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	if line, err := bufio.NewReaderSize(conn, 16).ReadString('\n'); err != nil || !strings.Contains(line, "200") {
		t.Fatalf("status line %q, err %v", line, err)
	}
	if !waitFor(t, 3*time.Second, func() bool { return app.db.Stats().InUse == 0 }) {
		t.Fatalf("a stalled data-endpoint reader still holds %d DB connection(s)", app.db.Stats().InUse)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("data handler still blocked writing to a client that stopped reading")
	}
}

// The tag-serve JSON API rewrites clip bytes in place. It must keep
// content_hash in step — streaming snapshots and dedup both trust it — and drop
// the clip's leased temp file like UpdateClipData does.
func TestWriteJSONClipKeepsContentHashAndTempFileCurrent(t *testing.T) {
	_, app, _, cleanup := setupShareLinkTest(t)
	defer cleanup()

	orig := []byte(`{"n":1}`)
	res, err := app.db.Exec("INSERT INTO clips (content_type, data, filename, content_hash) VALUES (?, ?, ?, ?)",
		"application/json", orig, "data.json", computeContentHash(orig))
	if err != nil {
		t.Fatal(err)
	}
	clipID, _ := res.LastInsertId()
	tagRes, err := app.db.Exec("INSERT INTO tags (name, color) VALUES ('site', '#fff')")
	if err != nil {
		t.Fatal(err)
	}
	tagID, _ := tagRes.LastInsertId()
	if _, err := app.db.Exec("INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, ?)", clipID, tagID); err != nil {
		t.Fatal(err)
	}
	if _, err := app.tempStore.PrepareClipFile(clipID); err != nil {
		t.Fatal(err)
	}

	sm := &ServeManager{app: app}
	if err := sm.writeJSONClip(tagID, "data.json", map[string]int{"n": 2}); err != nil {
		t.Fatal(err)
	}
	var data []byte
	var hash string
	if err := app.db.QueryRow("SELECT data, content_hash FROM clips WHERE id = ?", clipID).Scan(&data, &hash); err != nil {
		t.Fatal(err)
	}
	if hash != computeContentHash(data) {
		t.Fatalf("content_hash is stale after writeJSONClip (data %s)", data)
	}
	if prepared, err := app.tempStore.FindExistingClipFile(clipID); err != nil || prepared != nil {
		t.Fatalf("leased temp file survived writeJSONClip: %+v, %v", prepared, err)
	}
}

// A link's snapshot is never the file drag-out hands to another app. Reuse
// checks the file's identity and size, not its bytes, so an app the clip was
// dropped into that saves in place without changing the length (VS Code, vim
// with backupcopy=yes) would otherwise change what every public link serves
// while the library still holds the original.
func TestShareLinkStream_InPlaceEditOfDragOutFileIsNotServed(t *testing.T) {
	am, app, _, cleanup := setupShareLinkTest(t)
	defer cleanup()

	bigID, want := insertBigClip(t, app, 3<<20, "doc.bin")
	link, err := am.CreateShareLink(bigID, "", 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(shareMux(am))
	defer srv.Close()
	if status, body := fetchShare(t, srv, link.Token); status != http.StatusOK || !bytes.Equal(body, want) {
		t.Fatalf("first download: status %d, %d bytes", status, len(body))
	}
	snap := filepath.Join(app.tempDir, streamSnapshotName(bigID, computeContentHash(want)))
	if info, err := os.Stat(snap); err != nil {
		t.Fatalf("no link snapshot at %s: %v", snap, err)
	} else if info.Mode().Perm()&0o222 != 0 {
		t.Fatalf("link snapshot is writable: %v", info.Mode())
	}

	// What drag-out does: reuse a leased file if there is one, else make one.
	item, err := app.lookupPreparedClipTransferItem(bigID, "drag_out")
	if err != nil {
		t.Fatal(err)
	}
	if item == nil {
		if item, err = app.prepareClipTransferItem(bigID, "drag_out"); err != nil {
			t.Fatal(err)
		}
	}
	// The receiving app saves in place, keeping the length.
	f, err := os.OpenFile(item.AbsPath, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open the drag-out file for writing: %v", err)
	}
	if _, err := f.WriteAt([]byte("TAMPERED"), 0); err != nil {
		t.Fatal(err)
	}
	f.Close()

	status, body := fetchShare(t, srv, link.Token)
	if status != http.StatusOK || !bytes.Equal(body, want) {
		t.Fatalf("after an in-place edit of the drag-out file the link served status %d, prefix %q; want the library's bytes", status, body[:min(8, len(body))])
	}
	if n := app.tempStore.materialized.Load(); n != 1 {
		t.Fatalf("materialized = %d, want 1: the link's own snapshot should still be reused", n)
	}
}

// tempDirEntries lists what the store's directory holds.
func tempDirEntries(t *testing.T, app *App) []string {
	t.Helper()
	entries, err := os.ReadDir(app.tempDir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// A link's snapshot is a plaintext copy of the clip, so it goes when the
// revision it holds does: an edit drops it, and deleting the clip leaves no
// copy behind.
func TestShareLinkStream_EditAndDeleteDropTheSnapshot(t *testing.T) {
	am, app, _, cleanup := setupShareLinkTest(t)
	defer cleanup()

	bigID, want := insertBigClip(t, app, 3<<20, "doc.bin")
	link, err := am.CreateShareLink(bigID, "", 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(shareMux(am))
	defer srv.Close()
	if status, body := fetchShare(t, srv, link.Token); status != http.StatusOK || !bytes.Equal(body, want) {
		t.Fatalf("download: status %d, %d bytes", status, len(body))
	}
	if len(tempDirEntries(t, app)) == 0 {
		t.Fatal("no snapshot was published")
	}

	edited := bytes.Repeat([]byte("v2"), 2<<20)
	if err := app.UpdateClipData(bigID, "application/octet-stream", base64.StdEncoding.EncodeToString(edited), "doc.bin"); err != nil {
		t.Fatal(err)
	}
	if left := tempDirEntries(t, app); len(left) != 0 {
		t.Fatalf("the previous revision's copy survived UpdateClipData: %v", left)
	}

	if status, body := fetchShare(t, srv, link.Token); status != http.StatusOK || !bytes.Equal(body, edited) {
		t.Fatalf("download after edit: status %d, %d bytes", status, len(body))
	}
	if err := app.DeleteClip(bigID); err != nil {
		t.Fatal(err)
	}
	if left := tempDirEntries(t, app); len(left) != 0 {
		t.Fatalf("a deleted clip left a copy behind: %v", left)
	}
}

// The pruner covers link snapshots like any leased file: one idle past its
// lease goes (and is copied again on the next download), and so does one whose
// clip is gone.
func TestTempClipStore_PruneRemovesIdleAndOrphanedSnapshots(t *testing.T) {
	am, app, _, cleanup := setupShareLinkTest(t)
	defer cleanup()

	bigID, want := insertBigClip(t, app, 3<<20, "doc.bin")
	link, err := am.CreateShareLink(bigID, "", 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(shareMux(am))
	defer srv.Close()
	if status, _ := fetchShare(t, srv, link.Token); status != http.StatusOK {
		t.Fatalf("download: status %d", status)
	}
	if len(tempDirEntries(t, app)) == 0 {
		t.Fatal("no snapshot was published")
	}

	later := time.Now().Add(3 * time.Hour)
	app.tempStore.now = func() time.Time { return later }
	if err := app.tempStore.Prune(false); err != nil {
		t.Fatal(err)
	}
	if left := tempDirEntries(t, app); len(left) != 0 {
		t.Fatalf("an idle snapshot outlived its lease: %v", left)
	}
	app.tempStore.now = time.Now // the next copy is fresh, so only orphaning can remove it
	if status, body := fetchShare(t, srv, link.Token); status != http.StatusOK || !bytes.Equal(body, want) {
		t.Fatalf("download after prune: status %d, %d bytes", status, len(body))
	}
	if n := app.tempStore.materialized.Load(); n != 2 {
		t.Fatalf("materialized = %d, want 2 (copied again after the prune)", n)
	}

	// Deleted behind the store's back, as the expiry reaper used to.
	if _, err := app.db.Exec("DELETE FROM clips WHERE id = ?", bigID); err != nil {
		t.Fatal(err)
	}
	if err := app.tempStore.Prune(true); err != nil {
		t.Fatal(err)
	}
	if left := tempDirEntries(t, app); len(left) != 0 {
		t.Fatalf("a deleted clip's snapshot survived the pruner: %v", left)
	}
}
