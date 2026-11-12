package openrequest

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// shortDataDir returns a data dir whose socket path fits a sockaddr_un, so
// the tests exercise the in-data-dir path rather than the fallback.
func shortDataDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "mpipc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func startServer(t *testing.T, dataDir string) (*Server, <-chan Request) {
	t.Helper()
	srv, err := Listen(dataDir)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	got := make(chan Request, 8)
	go srv.Serve(func(r Request) { got <- r })
	t.Cleanup(func() { srv.Close() })
	return srv, got
}

func absPath(p string) string {
	if runtime.GOOS == "windows" {
		return `C:` + filepath.FromSlash(p)
	}
	return p
}

func TestSendDeliversRequest(t *testing.T) {
	dataDir := shortDataDir(t)
	srv, got := startServer(t, dataDir)
	if srv.Path() != filepath.Join(dataDir, socketName) {
		t.Fatalf("socket at %s, want inside the data dir", srv.Path())
	}

	req := Request{
		Files:    []string{absPath("/tmp/a.png"), absPath("/tmp/b c.txt")},
		URLs:     []string{"mahpastes://clip/7"},
		Activate: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := Send(ctx, dataDir, req); err != nil {
		t.Fatalf("Send: %v", err)
	}
	select {
	case r := <-got:
		if !reflect.DeepEqual(r, req) {
			t.Fatalf("server got %#v, want %#v", r, req)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server never saw the request")
	}
}

func TestSendPlainActivate(t *testing.T) {
	dataDir := shortDataDir(t)
	_, got := startServer(t, dataDir)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := Send(ctx, dataDir, Request{Activate: true}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if r := <-got; !r.Activate || !r.Empty() {
		t.Fatalf("got %#v, want a bare activate", r)
	}
}

func TestSendWithoutListener(t *testing.T) {
	dataDir := shortDataDir(t)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := Send(ctx, dataDir, Request{Activate: true})
	if !errors.Is(err, ErrNoListener) {
		t.Fatalf("Send with nobody listening = %v, want ErrNoListener", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("Send ignored its deadline (%s)", time.Since(start))
	}
}

func TestSendWithStaleSocketFile(t *testing.T) {
	// A crashed instance leaves its socket file behind; nothing accepts on it.
	dataDir := shortDataDir(t)
	srv, err := Listen(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if ul, ok := srv.ln.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(false)
	}
	srv.ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := Send(ctx, dataDir, Request{Activate: true}); !errors.Is(err, ErrNoListener) {
		t.Fatalf("Send to a stale socket = %v, want ErrNoListener", err)
	}

	// The next lock holder takes the path over.
	_, got := startServer(t, dataDir)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	if err := Send(ctx2, dataDir, Request{Activate: true}); err != nil {
		t.Fatalf("Send after takeover: %v", err)
	}
	<-got
}

func TestSendRetriesUntilListenerAppears(t *testing.T) {
	// Two processes started by one double-click: the loser can dial before
	// the winner has opened its socket.
	dataDir := shortDataDir(t)
	got := make(chan Request, 1)
	go func() {
		time.Sleep(300 * time.Millisecond)
		srv, err := Listen(dataDir)
		if err != nil {
			t.Error(err)
			return
		}
		t.Cleanup(func() { srv.Close() })
		srv.Serve(func(r Request) { got <- r })
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := Send(ctx, dataDir, Request{Files: []string{absPath("/x")}, Activate: true}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if r := <-got; len(r.Files) != 1 {
		t.Fatalf("got %#v", r)
	}
}

// rawExchange writes one line and returns the server's reply.
func rawExchange(t *testing.T, path, line string) wireReply {
	t.Helper()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write([]byte(line)); err != nil {
		t.Fatal(err)
	}
	if ul, ok := conn.(*net.UnixConn); ok {
		ul.CloseWrite()
	}
	b, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		t.Fatalf("read reply: %v", err)
	}
	var r wireReply
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("decode reply %q: %v", b, err)
	}
	return r
}

func TestServerRejectsMalformedRequests(t *testing.T) {
	dataDir := shortDataDir(t)
	srv, got := startServer(t, dataDir)

	tooMany := make([]string, maxRequestItems+1)
	for i := range tooMany {
		tooMany[i] = absPath("/f")
	}
	tooManyJSON, _ := json.Marshal(wireRequest{V: ProtocolVersion, Request: Request{Files: tooMany}})

	for name, line := range map[string]string{
		"wrong version": `{"v":2,"activate":true}` + "\n",
		"not json":      "hello\n",
		"empty":         "",
		"too many":      string(tooManyJSON) + "\n",
		"too large":     `{"v":1,"files":["` + strings.Repeat("a", maxMessageBytes) + `"]}` + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			if r := rawExchange(t, srv.Path(), line); r.OK || r.Error == "" {
				t.Fatalf("reply = %#v, want a refusal", r)
			}
		})
	}
	select {
	case r := <-got:
		t.Fatalf("handler ran for a malformed request: %#v", r)
	default:
	}
}

func TestServerSanitizesEntries(t *testing.T) {
	dataDir := shortDataDir(t)
	srv, got := startServer(t, dataDir)

	in := wireRequest{V: ProtocolVersion, Request: Request{
		Files:    []string{"relative.txt", "", absPath("/ok/../ok/a.png")},
		URLs:     []string{"https://example.com", "mahpastes://show"},
		Activate: true,
	}}
	b, _ := json.Marshal(in)
	if r := rawExchange(t, srv.Path(), string(b)+"\n"); !r.OK {
		t.Fatalf("valid request refused: %#v", r)
	}
	want := Request{Files: []string{absPath("/ok/a.png")}, URLs: []string{"mahpastes://show"}, Activate: true}
	if r := <-got; !reflect.DeepEqual(r, want) {
		t.Fatalf("handler got %#v, want %#v", r, want)
	}
}

func TestListenRestrictsAndCloseRemovesSocket(t *testing.T) {
	dataDir := shortDataDir(t)
	// A leftover regular file at the path must not block the new listener.
	if err := os.WriteFile(filepath.Join(dataDir, socketName), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, err := Listen(dataDir)
	if err != nil {
		t.Fatalf("Listen over a stale file: %v", err)
	}
	go srv.Serve(func(Request) {})
	if runtime.GOOS != "windows" {
		info, err := os.Stat(srv.Path())
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("socket mode %o, want 600", perm)
		}
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(srv.Path()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket still present after Close: %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestSocketPathFallsBackForLongDataDirs(t *testing.T) {
	short := shortDataDir(t)
	if got := SocketPath(short); got != filepath.Join(short, socketName) {
		t.Fatalf("SocketPath(%s) = %s, want inside the data dir", short, got)
	}

	long := filepath.Join(short, strings.Repeat("d", 120))
	p := SocketPath(long)
	if len(p) > maxSocketPathLen() {
		t.Fatalf("fallback path %s is %d bytes, max %d", p, len(p), maxSocketPathLen())
	}
	if p != SocketPath(long) {
		t.Fatal("fallback path is not deterministic")
	}
	if p == SocketPath(long+"x") {
		t.Fatal("different data dirs share a fallback socket")
	}

	// The fallback still works end to end.
	if err := os.MkdirAll(long, 0o755); err != nil {
		t.Fatal(err)
	}
	_, got := startServer(t, long)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := Send(ctx, long, Request{Activate: true}); err != nil {
		t.Fatalf("Send via fallback path: %v", err)
	}
	<-got
}
