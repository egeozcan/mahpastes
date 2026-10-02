package app

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// serveTestTag tags clipID under a fresh tag and returns a tag-serve handler
// for that tag, wrapped so the test can tell when a request's handler returns.
func serveTestTag(t *testing.T, app *App, clipID int64) (http.Handler, <-chan struct{}) {
	t.Helper()
	res, err := app.db.Exec(`INSERT INTO tags (name, color) VALUES ('served', '#333333')`)
	if err != nil {
		t.Fatal(err)
	}
	tagID, _ := res.LastInsertId()
	if _, err := app.db.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, ?)`, clipID, tagID); err != nil {
		t.Fatal(err)
	}
	ts := &tagServer{tagID: tagID, tagName: "served", apiAccess: "none"}
	inner := NewServeManager(app).makeHandler(ts)
	done := make(chan struct{}, 1)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			select {
			case done <- struct{}{}:
			default: // nobody is waiting on this request
			}
		}()
		inner.ServeHTTP(w, r)
	}), done
}

// A tag-serve client that stops reading used to park its handler in one
// w.Write of the whole clip — read into memory first — for as long as it kept
// the connection open; the server had no timeouts at all. The body now comes
// from a detached snapshot under a sliding per-chunk write deadline.
func TestTagServe_StalledReaderIsCutOff(t *testing.T) {
	setForTest(t, &clipStreamWriteTimeout, 200*time.Millisecond)
	_, app, _, cleanup := setupShareLinkTest(t)
	defer cleanup()

	bigID, _ := insertBigClip(t, app, 16<<20, "big.bin")
	handler, done := serveTestTag(t, app, bigID)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetReadBuffer(4096)
	}
	if _, err := conn.Write([]byte("GET /big.bin HTTP/1.1\r\nHost: x\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	if line, err := bufio.NewReaderSize(conn, 16).ReadString('\n'); err != nil || !strings.Contains(line, "200") {
		t.Fatalf("status line %q, err %v", line, err)
	}

	// Still connected, still not reading.
	if !waitFor(t, 3*time.Second, func() bool { return app.db.Stats().InUse == 0 }) {
		t.Fatalf("a stalled tag-serve reader still holds %d DB connection(s)", app.db.Stats().InUse)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("tag-serve handler still blocked writing to a client that stopped reading")
	}
}

// The streamed path still delivers whole files — large ones from a snapshot,
// small ones from memory — with their stored type, and answers HEAD without a
// body.
func TestTagServe_ServesClipBytes(t *testing.T) {
	_, app, _, cleanup := setupShareLinkTest(t)
	defer cleanup()

	bigID, big := insertBigClip(t, app, 3<<20, "big.bin")
	handler, _ := serveTestTag(t, app, bigID)
	res, err := app.db.Exec(`INSERT INTO clips (content_type, data, filename) VALUES ('text/html', ?, 'index.html')`, []byte("<p>hi</p>"))
	if err != nil {
		t.Fatal(err)
	}
	indexID, _ := res.LastInsertId()
	if _, err := app.db.Exec(`INSERT INTO clip_tags (clip_id, tag_id) SELECT ?, id FROM tags WHERE name = 'served'`, indexID); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()

	get := func(method, path string) (*http.Response, []byte) {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return resp, body
	}

	resp, body := get(http.MethodGet, "/big.bin")
	if resp.StatusCode != http.StatusOK || !bytes.Equal(body, big) {
		t.Fatalf("GET /big.bin: status %d, %d bytes (want %d identical bytes)", resp.StatusCode, len(body), len(big))
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/octet-stream" {
		t.Fatalf("GET /big.bin Content-Type = %q", ct)
	}

	resp, body = get(http.MethodGet, "/")
	if resp.StatusCode != http.StatusOK || string(body) != "<p>hi</p>" || resp.Header.Get("Content-Type") != "text/html" {
		t.Fatalf("GET / = %d %q (%s), want the index.html clip", resp.StatusCode, body, resp.Header.Get("Content-Type"))
	}

	resp, body = get(http.MethodHead, "/big.bin")
	if resp.StatusCode != http.StatusOK || len(body) != 0 || resp.ContentLength != int64(len(big)) {
		t.Fatalf("HEAD /big.bin = %d, %d body bytes, Content-Length %d", resp.StatusCode, len(body), resp.ContentLength)
	}

	resp, _ = get(http.MethodGet, "/missing.bin")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /missing.bin = %d, want 404", resp.StatusCode)
	}
}

// A tag server can bind every interface; without header/read/idle timeouts a
// client that trickles its request headers (or never sends any) holds a
// connection and goroutine open indefinitely.
func TestTagServe_ServerHasConnectionTimeouts(t *testing.T) {
	db := newServerTestDB(t)
	sm := NewServeManager(&App{db: db})
	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (7, 'served', '#333333')`); err != nil {
		t.Fatal(err)
	}
	if _, err := sm.StartServing(7, 0, false, "none"); err != nil {
		t.Fatalf("StartServing: %v", err)
	}
	defer sm.StopAll()

	sm.mu.RLock()
	srv := sm.servers[7].server
	sm.mu.RUnlock()
	if srv.ReadHeaderTimeout <= 0 || srv.ReadTimeout <= 0 || srv.WriteTimeout <= 0 || srv.IdleTimeout <= 0 || srv.MaxHeaderBytes <= 0 {
		t.Fatalf("tag server limits: header=%v read=%v write=%v idle=%v maxHeader=%d — every one must be set",
			srv.ReadHeaderTimeout, srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout, srv.MaxHeaderBytes)
	}
	if srv.WriteTimeout <= srv.ReadTimeout {
		t.Fatalf("WriteTimeout %v <= ReadTimeout %v: a slow upload's response could never be written", srv.WriteTimeout, srv.ReadTimeout)
	}
}

// Cutting a stalled client off is what lets a paused browser video or download
// lose its connection after clipStreamWriteTimeout — so resuming has to work:
// the browser asks for the rest with a Range request.
func TestTagServe_RangeRequests(t *testing.T) {
	_, app, _, cleanup := setupShareLinkTest(t)
	defer cleanup()
	bigID, big := insertBigClip(t, app, 3<<20, "big.bin")
	handler, _ := serveTestTag(t, app, bigID)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	get := func(rangeHeader string) (*http.Response, []byte) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/big.bin", nil)
		if rangeHeader != "" {
			req.Header.Set("Range", rangeHeader)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp, body
	}

	resp, body := get("")
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Accept-Ranges") != "bytes" || !bytes.Equal(body, big) {
		t.Fatalf("plain GET: %d, Accept-Ranges %q, %d bytes", resp.StatusCode, resp.Header.Get("Accept-Ranges"), len(body))
	}

	resp, body = get("bytes=1048576-")
	if resp.StatusCode != http.StatusPartialContent || !bytes.Equal(body, big[1<<20:]) {
		t.Fatalf("resume from 1 MiB: %d, %d bytes", resp.StatusCode, len(body))
	}
	if want := "bytes 1048576-3145727/3145728"; resp.Header.Get("Content-Range") != want {
		t.Fatalf("Content-Range = %q, want %q", resp.Header.Get("Content-Range"), want)
	}

	resp, body = get("bytes=10-19")
	if resp.StatusCode != http.StatusPartialContent || !bytes.Equal(body, big[10:20]) {
		t.Fatalf("bytes=10-19: %d, %q", resp.StatusCode, body)
	}

	resp, _ = get("bytes=99999999-")
	if resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("out-of-range request: %d, want 416", resp.StatusCode)
	}
}

// A clip over clipStreamInMemoryMax is served from a snapshot in the temp
// store's dir. If that dir was removed from under the app, every such clip
// used to answer 500 until something recreated it.
func TestTagServe_ServesLargeClipWhenTempDirIsGone(t *testing.T) {
	_, app, _, cleanup := setupShareLinkTest(t)
	defer cleanup()
	bigID, big := insertBigClip(t, app, 3<<20, "big.bin")
	handler, _ := serveTestTag(t, app, bigID)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	if err := os.RemoveAll(app.tempStore.dir); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(srv.URL + "/big.bin")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !bytes.Equal(body, big) {
		t.Fatalf("GET with the temp dir gone: %d, %d bytes", resp.StatusCode, len(body))
	}
}

// countingBody counts how much of a request body the server read.
type countingBody struct {
	r io.Reader
	n int64
}

func (c *countingBody) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}
func (c *countingBody) Close() error { return nil }

// ParseMultipartForm's limit is only the in-memory threshold: a file part
// beyond it is spooled to a temp file with no cap, and the 10 MB check ran
// after the whole body was on disk. Any client the tag server lets upload
// could fill the disk with one request.
func TestTagServe_UploadStopsReadingPastTheLimit(t *testing.T) {
	db := newServerTestDB(t)
	sm := NewServeManager(&App{db: db})
	ts := &tagServer{tagID: 1, tagName: "served", apiAccess: "readwrite", serveKey: "k"}

	const total = 64 << 20
	boundary := "xyz"
	head := "--" + boundary + "\r\nContent-Disposition: form-data; name=\"file\"; filename=\"big.bin\"\r\n" +
		"Content-Type: application/octet-stream\r\n\r\n"
	body := &countingBody{r: io.MultiReader(strings.NewReader(head), io.LimitReader(zeroReader{}, total))}
	req := httptest.NewRequest(http.MethodPost, "/_api/_upload", nil)
	req.Body = body
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	req.AddCookie(&http.Cookie{Name: "_mp_serve_key", Value: "k"})
	rec := httptest.NewRecorder()

	sm.handleFileUpload(rec, req, ts)

	if rec.Code < 400 {
		t.Fatalf("a %d MB upload got %d, want a refusal", total>>20, rec.Code)
	}
	if body.n > maxUploadSize+(2<<20) {
		t.Fatalf("the server read %d MB of a %d MB upload before refusing it", body.n>>20, total>>20)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

// stalledResponseWriter is a ResponseWriter whose body writes wait for release — a
// client that has stopped reading.
type stalledResponseWriter struct {
	header  http.Header
	release chan struct{}
	writing chan struct{}
	once    sync.Once
}

func (b *stalledResponseWriter) Header() http.Header { return b.header }
func (b *stalledResponseWriter) WriteHeader(int)     {}
func (b *stalledResponseWriter) Write(p []byte) (int, error) {
	b.once.Do(func() { close(b.writing) })
	<-b.release
	return len(p), nil
}

// The JSON write handlers held the clip's mutex across writing their response
// (PUT echoes the whole body). A client that sent a write and stopped reading
// the reply blocked every other writer of that JSON clip until the write
// deadline cut it off.
func TestTagServe_JSONWriterReleasesClipLockBeforeResponding(t *testing.T) {
	db := newServerTestDB(t)
	app := &App{db: db}
	if _, err := db.Exec(`INSERT INTO tags (id, name, color) VALUES (1, 'served', '#333333')`); err != nil {
		t.Fatal(err)
	}
	res, err := db.Exec(`INSERT INTO clips (content_type, data, filename) VALUES ('application/json', '{"a":1}', 'doc.json')`)
	if err != nil {
		t.Fatal(err)
	}
	clipID, _ := res.LastInsertId()
	if _, err := db.Exec(`INSERT INTO clip_tags (clip_id, tag_id) VALUES (?, 1)`, clipID); err != nil {
		t.Fatal(err)
	}
	sm := NewServeManager(app)
	ts := &tagServer{tagID: 1, tagName: "served", apiAccess: "readwrite", serveKey: "k", clipMutexes: map[string]*sync.Mutex{}}

	request := func(method, body string) *http.Request {
		req := httptest.NewRequest(method, "/_api/doc", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "_mp_serve_key", Value: "k"})
		return req
	}

	stalled := &stalledResponseWriter{header: http.Header{}, release: make(chan struct{}), writing: make(chan struct{})}
	defer close(stalled.release)
	go sm.handleJSONAPI(stalled, request(http.MethodPut, `{"a":2}`), ts)
	select {
	case <-stalled.writing:
	case <-time.After(5 * time.Second):
		t.Fatal("the PUT never started writing its response")
	}

	done := make(chan int, 1)
	go func() {
		rec := httptest.NewRecorder()
		sm.handleJSONAPI(rec, request(http.MethodPatch, `{"b":3}`), ts)
		done <- rec.Code
	}()
	select {
	case code := <-done:
		if code != http.StatusOK {
			t.Fatalf("PATCH behind a stalled PUT answered %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("PATCH waited on the clip lock held by a PUT whose client stopped reading")
	}
}

// copyClipBody's sliding deadline is what makes the server-wide WriteTimeout
// safe for downloads: a steady reader must outlive it.
func TestTagServe_SteadyReaderOutlivesWriteTimeout(t *testing.T) {
	_, app, _, cleanup := setupShareLinkTest(t)
	defer cleanup()
	bigID, big := insertBigClip(t, app, 2<<20, "big.bin")
	handler, _ := serveTestTag(t, app, bigID)
	srv := httptest.NewUnstartedServer(handler)
	srv.Config.WriteTimeout = 300 * time.Millisecond
	srv.Config.ConnState = func(c net.Conn, state http.ConnState) {
		if tcp, ok := c.(*net.TCPConn); ok && state == http.StateNew {
			_ = tcp.SetWriteBuffer(16 << 10)
		}
	}
	srv.Start()
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetReadBuffer(16 << 10)
	}
	if _, err := conn.Write([]byte("GET /big.bin HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	start := time.Now()
	var got []byte
	buf := make([]byte, 32<<10)
	for {
		n, err := resp.Body.Read(buf)
		got = append(got, buf[:n]...)
		if err != nil {
			break
		}
		time.Sleep(5 * time.Millisecond) // ~6 MB/s: far past 300ms for 2 MB
	}
	if !bytes.Equal(got, big) {
		t.Fatalf("steady reader got %d of %d bytes after %v (server WriteTimeout 300ms)", len(got), len(big), time.Since(start))
	}
	if time.Since(start) < 300*time.Millisecond {
		t.Fatalf("download finished in %v, inside the WriteTimeout — the test is not exercising the slide", time.Since(start))
	}
}

// A browser resumes an interrupted download only with a validator: without an
// ETag (or Last-Modified) Chrome restarts it from byte zero. And a player
// sending If-Range must get the whole new revision, not a range spliced from
// an edited clip onto the old bytes it already has.
func TestTagServe_ResumesAgainstAValidator(t *testing.T) {
	_, app, _, cleanup := setupShareLinkTest(t)
	defer cleanup()
	bigID, big := insertBigClip(t, app, 3<<20, "big.bin")
	handler, _ := serveTestTag(t, app, bigID)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	get := func(headers map[string]string) (*http.Response, []byte) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/big.bin", nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp, body
	}

	resp, _ := get(nil)
	etag := resp.Header.Get("ETag")
	if etag == "" || strings.HasPrefix(etag, "W/") {
		t.Fatalf("no strong ETag on a tag-served clip (got %q)", etag)
	}

	resp, body := get(map[string]string{"Range": "bytes=10-19", "If-Range": etag})
	if resp.StatusCode != http.StatusPartialContent || !bytes.Equal(body, big[10:20]) {
		t.Fatalf("range with a matching If-Range: %d, %d bytes", resp.StatusCode, len(body))
	}
	resp, body = get(map[string]string{"Range": "bytes=10-19", "If-Range": `"an-older-revision"`})
	if resp.StatusCode != http.StatusOK || !bytes.Equal(body, big) {
		t.Fatalf("range with a stale If-Range: %d, %d bytes — want the whole current clip", resp.StatusCode, len(body))
	}

	// The ETag follows the bytes: an edited clip gets a new one.
	edited := append([]byte{}, big...)
	edited[0] ^= 0xff
	if _, err := app.db.Exec(`UPDATE clips SET data = ?, content_hash = ? WHERE id = ?`, edited, computeContentHash(edited), bigID); err != nil {
		t.Fatal(err)
	}
	if resp, _ := get(nil); resp.Header.Get("ETag") == etag {
		t.Fatal("the ETag did not change when the clip's bytes did")
	}
}

// The tag server ignored Range before it learned ranges; a multi-range request
// a client may send must not now be refused with 416.
func TestTagServe_MultiRangeIsNotRefused(t *testing.T) {
	_, app, _, cleanup := setupShareLinkTest(t)
	defer cleanup()
	bigID, _ := insertBigClip(t, app, 64<<10, "small.bin")
	handler, _ := serveTestTag(t, app, bigID)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/small.bin", nil)
	req.Header.Set("Range", "bytes=0-1,4-5")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		t.Fatal("a multi-range request was refused with 416")
	}
}

// HEAD reports what it can without loading the bytes; the stored content_hash
// it would have to use is stale for JSON clips edited by older builds, so it
// sends no validator rather than a wrong one. And the ETag covers the type as
// well as the bytes: the same bytes re-added under a new type must not be
// revalidated as unchanged and keep the browser on the old type.
func TestTagServe_ETagCoversTypeAndHEADSendsNone(t *testing.T) {
	_, app, _, cleanup := setupShareLinkTest(t)
	defer cleanup()
	clipID, _ := insertBigClip(t, app, 2048, "app.js")
	handler, _ := serveTestTag(t, app, clipID)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	do := func(method string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+"/app.js", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return resp
	}
	if etag := do(http.MethodHead).Header.Get("ETag"); etag != "" {
		t.Fatalf("HEAD sent ETag %q", etag)
	}
	before := do(http.MethodGet).Header.Get("ETag")
	if _, err := app.db.Exec(`UPDATE clips SET content_type = 'text/javascript' WHERE id = ?`, clipID); err != nil {
		t.Fatal(err)
	}
	if after := do(http.MethodGet).Header.Get("ETag"); after == before {
		t.Fatalf("the ETag stayed %s when the clip's type changed", after)
	}
}

// Thousands of tiny ranges turn a small clip into a response many times its
// size; a tag server can listen on every interface. Past a modest count the
// Range header is ignored and the clip is served whole.
func TestTagServe_ManyRangesAreIgnored(t *testing.T) {
	_, app, _, cleanup := setupShareLinkTest(t)
	defer cleanup()
	clipID, data := insertBigClip(t, app, 64<<10, "small.bin")
	handler, _ := serveTestTag(t, app, clipID)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	var ranges []string
	for i := 0; i < 500; i++ {
		ranges = append(ranges, strconv.Itoa(i)+"-"+strconv.Itoa(i))
	}
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/small.bin", nil)
	req.Header.Set("Range", "bytes="+strings.Join(ranges, ","))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !bytes.Equal(body, data) {
		t.Fatalf("500-range request: %d with %d bytes, want the whole %d-byte clip", resp.StatusCode, len(body), len(data))
	}
}
