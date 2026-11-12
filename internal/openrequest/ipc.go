package openrequest

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// The instance socket.
//
// The desktop app holds a per-data-dir advisory lock (internal/app
// AcquireInstanceLock). The process that wins it listens on a Unix domain
// socket tied to the same data directory; a later launch that loses the lock
// dials that socket, sends its Request as one line of JSON, waits for a one
// line JSON ack and exits. Unix domain sockets exist on Linux, macOS and
// Windows 10 1803+, so one implementation covers all three, and nothing here
// depends on D-Bus or on Wails internals.
//
// Wire format (one request per connection, newline-terminated JSON each way):
//
//	client -> server  {"v":1,"files":["/abs/a.png"],"urls":["mahpastes://x"],"activate":true}
//	server -> client  {"ok":true}            or {"ok":false,"error":"..."}
//
// Only the user who owns the data directory can reach the socket (the file is
// created 0600 inside it). The payload is still treated as untrusted: it is
// size-capped here, and every file is checked again before it is imported.

// ProtocolVersion is the "v" field of the wire format.
const ProtocolVersion = 1

const (
	socketName = "instance.sock"
	// maxMessageBytes caps one request line. 32K UTF-16 characters of command
	// line (Explorer's limit) is at most 128 KiB of UTF-8 plus JSON escaping.
	maxMessageBytes = 1 << 20
	// maxRequestItems caps the files+URLs of one request.
	maxRequestItems = maxQueuedFiles
	// ioTimeout bounds each side's read/write of a single exchange.
	ioTimeout = 5 * time.Second
)

// maxSocketPathLen is the longest path a sockaddr_un holds on this OS
// (104 bytes with a terminating NUL on macOS, 108 on Linux and Windows).
func maxSocketPathLen() int {
	if runtime.GOOS == "darwin" {
		return 103
	}
	return 107
}

// SocketPath is where the instance owning dataDir listens. It is inside the
// data directory unless that path is too long for a socket address (deep
// MAHPASTES_DATA_DIR overrides, long macOS temp dirs); then it is a name
// derived from the data directory in the per-user runtime directory. Both
// sides compute it the same way.
func SocketPath(dataDir string) string {
	p := filepath.Join(dataDir, socketName)
	if len(p) <= maxSocketPathLen() {
		return p
	}
	sum := sha256.Sum256([]byte(filepath.Clean(dataDir)))
	return filepath.Join(runtimeDir(), "mahpastes-"+hex.EncodeToString(sum[:8])+".sock")
}

// runtimeDir is a short, per-user directory for the fallback socket path.
func runtimeDir() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" && runtime.GOOS == "linux" {
		return dir
	}
	return os.TempDir()
}

type wireRequest struct {
	V int `json:"v"`
	Request
}

type wireReply struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// Server accepts requests from later launches. Only the holder of the
// instance lock may run one: Listen removes whatever is at the socket path
// first, which is only safe because no one else can be listening there.
type Server struct {
	ln   net.Listener
	path string

	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup
}

// Listen opens the instance socket for dataDir. The caller must hold the
// data directory's instance lock.
func Listen(dataDir string) (*Server, error) {
	path := SocketPath(dataDir)
	// A socket file survives a crash. Nobody can be serving it (we hold the
	// lock), so remove it; Listen would otherwise fail with "address in use".
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("remove stale instance socket: %w", err)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen on instance socket %s: %w", path, err)
	}
	if runtime.GOOS != "windows" {
		// Keep other local users out even where the directory is readable.
		if err := os.Chmod(path, 0o600); err != nil {
			ln.Close()
			return nil, fmt.Errorf("restrict instance socket: %w", err)
		}
	}
	return &Server{ln: ln, path: path}, nil
}

// Path is the socket path the server listens on.
func (s *Server) Path() string { return s.path }

// Serve accepts connections until Close, calling handle for every valid
// request. handle runs on the connection's goroutine and must not block for
// long: the sender waits for the ack.
func (s *Server) Serve(handle func(Request)) {
	for {
		conn, err := s.ln.Accept()
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			if conn != nil {
				conn.Close()
			}
			return
		}
		if err != nil {
			s.mu.Unlock()
			if errors.Is(err, net.ErrClosed) {
				return
			}
			// Transient (out of file descriptors, an aborted connection):
			// keep serving, or every later launch would lose its handoff.
			log.Printf("instance socket: accept: %v", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		// Added under the lock so Close's Wait never races a new connection.
		s.wg.Add(1)
		s.mu.Unlock()
		go func() {
			defer s.wg.Done()
			serveConn(conn, handle)
		}()
	}
}

// Close stops accepting, waits for in-flight connections and removes the
// socket file.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	err := s.ln.Close()
	s.wg.Wait()
	// Closing a Go unix listener unlinks the socket on Unix; remove it
	// explicitly for Windows and in case unlinking was disabled.
	if rmErr := os.Remove(s.path); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) && err == nil {
		err = rmErr
	}
	return err
}

func serveConn(conn net.Conn, handle func(Request)) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(ioTimeout))

	req, err := readRequest(conn)
	reply := wireReply{OK: err == nil}
	if err != nil {
		reply.Error = err.Error()
		log.Printf("instance socket: rejected request: %v", err)
	}
	if err == nil {
		handle(req)
	}
	_ = json.NewEncoder(conn).Encode(reply)
}

// readRequest reads and validates one request line.
func readRequest(r io.Reader) (Request, error) {
	line, err := bufio.NewReader(io.LimitReader(r, maxMessageBytes+1)).ReadBytes('\n')
	if len(line) > maxMessageBytes {
		return Request{}, errors.New("request too large")
	}
	// A sender may close its write side instead of ending the line.
	if err != nil && (!errors.Is(err, io.EOF) || len(line) == 0) {
		return Request{}, fmt.Errorf("read request: %w", err)
	}
	var w wireRequest
	if err := json.Unmarshal(line, &w); err != nil {
		return Request{}, fmt.Errorf("decode request: %w", err)
	}
	if w.V != ProtocolVersion {
		return Request{}, fmt.Errorf("unsupported protocol version %d", w.V)
	}
	return sanitize(w.Request)
}

// sanitize keeps only well-formed entries: absolute file paths and mahpastes
// URLs. A sender that is not FromArgs (anything that can reach the socket)
// gets no further than this.
func sanitize(in Request) (Request, error) {
	if len(in.Files)+len(in.URLs) > maxRequestItems {
		return Request{}, fmt.Errorf("too many items: %d (max %d)", len(in.Files)+len(in.URLs), maxRequestItems)
	}
	out := Request{Activate: in.Activate}
	for _, f := range in.Files {
		if f == "" || !filepath.IsAbs(f) {
			continue
		}
		out.Files = append(out.Files, filepath.Clean(f))
	}
	for _, u := range in.URLs {
		if s, ok := ParseURL(u); ok {
			out.URLs = append(out.URLs, s)
		}
	}
	return out, nil
}

// ErrNoListener means nothing accepted the connection: the lock holder is not
// a desktop app (mahpastesd), is still starting, or is gone.
var ErrNoListener = errors.New("no running mahpastes window is listening")

// ErrRefused means the request will not be delivered however often it is
// retried: the running instance rejected it, or the socket belongs to another
// user.
var ErrRefused = errors.New("open request refused")

// Send delivers req to the instance that owns dataDir. It keeps retrying the
// connection until ctx expires, because a launch that lost the lock race may
// get here before the winner has opened its socket (a double-click starts two
// processes almost together). A refused or missing socket after that is
// ErrNoListener; a request the instance rejects, or a socket owned by another
// user, is ErrRefused.
func Send(ctx context.Context, dataDir string, req Request) error {
	path := SocketPath(dataDir)
	var d net.Dialer
	var conn net.Conn
	for {
		if err := checkSocketOwner(path); err != nil {
			return fmt.Errorf("%w: %v", ErrRefused, err)
		}
		var err error
		conn, err = d.DialContext(ctx, "unix", path)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: %v", ErrNoListener, err)
		case <-time.After(100 * time.Millisecond):
		}
	}
	defer conn.Close()

	deadline := time.Now().Add(ioTimeout)
	if dl, ok := ctx.Deadline(); ok && dl.After(deadline) {
		deadline = dl
	}
	_ = conn.SetDeadline(deadline)

	if err := json.NewEncoder(conn).Encode(wireRequest{V: ProtocolVersion, Request: req}); err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	line, err := bufio.NewReader(io.LimitReader(conn, 64*1024)).ReadBytes('\n')
	if err != nil && (!errors.Is(err, io.EOF) || len(line) == 0) {
		return fmt.Errorf("read reply: %w", err)
	}
	var reply wireReply
	if err := json.Unmarshal(line, &reply); err != nil {
		return fmt.Errorf("decode reply: %w", err)
	}
	if !reply.OK {
		return fmt.Errorf("%w by the running instance: %s", ErrRefused, reply.Error)
	}
	return nil
}
