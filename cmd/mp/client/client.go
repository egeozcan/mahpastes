package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"go-clipboard/internal/buildinfo"
)

// Exit codes used by the CLI.
const (
	ExitOK         = 0
	ExitError      = 1
	ExitConnection = 2
	ExitAuth       = 3
)

// APIError represents an error returned by the mahpastes API.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("API error %d", e.StatusCode)
}

// ExitCode returns the appropriate CLI exit code for this API error.
func (e *APIError) ExitCode() int {
	switch e.StatusCode {
	case 0:
		return ExitConnection
	case http.StatusUnauthorized:
		return ExitAuth
	case http.StatusForbidden:
		return ExitAuth
	default:
		return ExitError
	}
}

// Client is an HTTP client for the mahpastes REST API.
type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

// New creates a Client from environment variables.
// MP_API_URL defaults to http://localhost:44557.
// MP_API_KEY is required.
func New() (*Client, error) {
	baseURL := os.Getenv("MP_API_URL")
	if baseURL == "" {
		baseURL = "http://localhost:44557"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	apiKey := os.Getenv("MP_API_KEY")
	if apiKey == "" {
		// A missing key is an authentication problem: exit with ExitAuth like
		// an invalid or revoked key, as documented.
		return nil, &APIError{
			StatusCode: http.StatusUnauthorized,
			Message:    "MP_API_KEY environment variable is required.\n\nTo get started:\n  1. Open mahpastes Settings > API\n  2. Create an API key\n  3. export MP_API_KEY=<your-key>",
		}
	}

	return &Client{
		BaseURL:    baseURL,
		APIKey:     apiKey,
		HTTPClient: &http.Client{Transport: newTransport(timeoutFromEnv())},
	}, nil
}

// defaultTimeout bounds connection setup and the wait for response headers.
const defaultTimeout = 30 * time.Second

// timeoutFromEnv returns MP_TIMEOUT (a Go duration such as "10s", or plain
// seconds such as "10") or defaultTimeout when unset or invalid.
func timeoutFromEnv() time.Duration {
	v := strings.TrimSpace(os.Getenv("MP_TIMEOUT"))
	if v == "" {
		return defaultTimeout
	}
	if d, err := time.ParseDuration(v); err == nil && d > 0 {
		return d
	}
	if n, err := strconv.ParseFloat(v, 64); err == nil && n > 0 {
		return time.Duration(n * float64(time.Second))
	}
	return defaultTimeout
}

// newTransport builds a transport with dial, TLS and response-header
// deadlines. There is deliberately no whole-request timeout so that large
// uploads and downloads are not cut off while data is flowing.
func newTransport(timeout time.Duration) *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DialContext = (&net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}).DialContext
	t.TLSHandshakeTimeout = timeout
	t.ResponseHeaderTimeout = timeout
	return t
}

// Long returns a client for requests whose server handler does all of its
// work (building a backup, restoring one, vacuuming, running a plugin action)
// before it writes any response header. It keeps the dial and TLS deadlines
// but drops the response-header deadline, so a slow but healthy operation is
// not reported as a timeout.
func (c *Client) Long() *Client {
	if c.HTTPClient == nil {
		return c
	}
	t, ok := c.HTTPClient.Transport.(*http.Transport)
	if !ok || t.ResponseHeaderTimeout == 0 {
		return c
	}
	lt := t.Clone()
	lt.ResponseHeaderTimeout = 0
	hc := *c.HTTPClient
	hc.Transport = lt
	cp := *c
	cp.HTTPClient = &hc
	return &cp
}

var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

func escapeQuotes(s string) string { return quoteEscaper.Replace(s) }

// do executes an HTTP request with auth headers and handles common errors.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "mp-cli/"+buildinfo.Version)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		if isConnectionRefused(err) || isDialError(err) {
			return nil, &APIError{
				StatusCode: 0,
				Message:    "Cannot connect to mahpastes. Is the app running with the API enabled?",
			}
		}
		if isTimeout(err) {
			return nil, &APIError{
				StatusCode: 0,
				Message:    fmt.Sprintf("Timed out waiting for mahpastes at %s (raise MP_TIMEOUT for slow connections).", c.BaseURL),
			}
		}
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return resp, nil
}

// Do sends a prepared request with the client's auth header, User-Agent and
// error classification, and returns an APIError for non-2xx responses. The
// caller must close the response body.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if err := checkResponse(resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// checkResponse inspects the HTTP status and returns an APIError for non-2xx responses.
func checkResponse(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return &APIError{StatusCode: 401, Message: "Authentication failed. Check MP_API_KEY."}
	case http.StatusForbidden:
		return &APIError{StatusCode: 403, Message: "Insufficient permissions."}
	}

	// Try to parse JSON error body.
	var errBody struct {
		Error string `json:"error"`
	}
	body, _ := io.ReadAll(resp.Body)
	if json.Unmarshal(body, &errBody) == nil && errBody.Error != "" {
		return &APIError{StatusCode: resp.StatusCode, Message: errBody.Error}
	}

	msg := strings.TrimSpace(string(body))
	if msg == "" {
		msg = http.StatusText(resp.StatusCode)
	}
	return &APIError{StatusCode: resp.StatusCode, Message: msg}
}

// Get performs a GET request and returns the response. Caller must close the body.
func (c *Client) Get(path string) (*http.Response, error) {
	req, err := http.NewRequest("GET", c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if err := checkResponse(resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// GetJSON performs a GET request and decodes the JSON response into v.
func (c *Client) GetJSON(path string, v interface{}) error {
	resp, err := c.Get(path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(v)
}

// GetRaw performs a GET and returns the raw response body. Caller must close it.
func (c *Client) GetRaw(path string) (io.ReadCloser, string, error) {
	resp, err := c.Get(path)
	if err != nil {
		return nil, "", err
	}
	return resp.Body, resp.Header.Get("Content-Type"), nil
}

// PostJSON performs a POST with a JSON body and decodes the response into result.
func (c *Client) PostJSON(path string, body interface{}, result interface{}) error {
	return c.jsonRequest("POST", path, body, result)
}

// PutJSON performs a PUT with a JSON body and decodes the response into result.
func (c *Client) PutJSON(path string, body interface{}, result interface{}) error {
	return c.jsonRequest("PUT", path, body, result)
}

// PatchJSON performs a PATCH with a JSON body and decodes the response into result.
func (c *Client) PatchJSON(path string, body interface{}, result interface{}) error {
	return c.jsonRequest("PATCH", path, body, result)
}

// Delete performs a DELETE request and returns any error.
func (c *Client) Delete(path string) error {
	req, err := http.NewRequest("DELETE", c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return checkResponse(resp)
}

// DeleteJSON performs a DELETE request and decodes the JSON response into result.
func (c *Client) DeleteJSON(path string, result interface{}) error {
	req, err := http.NewRequest("DELETE", c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := checkResponse(resp); err != nil {
		return err
	}
	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

// UploadFile uploads a file via multipart form POST.
// extraFields are added as form fields alongside the file.
func (c *Client) UploadFile(path string, filename string, reader io.Reader, extraFields map[string]string, result interface{}) error {
	pr, pw := io.Pipe()
	w := multipart.NewWriter(pw)

	// Write multipart body in a goroutine so it streams into the request.
	// writeErr is only read after done is closed.
	var writeErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		writeErr = writeMultipart(w, filename, reader, extraFields)
		// CloseWithError(nil) is a plain Close; a write failure aborts the
		// request instead of sending a silently truncated body.
		pw.CloseWithError(writeErr)
	}()

	req, err := http.NewRequest("POST", c.BaseURL+path, pr)
	if err != nil {
		pr.Close()
		<-done
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := c.do(req)
	if err != nil {
		// Unblock the writer in case the transport stopped reading. Do not
		// wait for it: it may be blocked reading an idle stdin. A local read
		// error has already closed done, which makes writeErr safe to read.
		pr.Close()
		select {
		case <-done:
			if writeErr != nil && !errors.Is(writeErr, io.ErrClosedPipe) {
				return writeErr // e.g. a local read error that aborted the request
			}
		default:
		}
		return err
	}
	defer resp.Body.Close()
	if err := checkResponse(resp); err != nil {
		// The server rejected the upload, possibly before consuming it; its
		// message explains more than the writer's closed-pipe error would.
		// writeErr is not needed, so do not wait for the writer.
		pr.Close()
		return err
	}
	// On success the transport finishes sending (or closes) the body, which
	// lets the writer return.
	<-done
	if writeErr != nil {
		return writeErr
	}
	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

// writeMultipart writes extraFields followed by the file part. The file part
// carries the caller's content type ("content_type" in extraFields) in its own
// header, which the server honours, instead of a separate form field.
func writeMultipart(w *multipart.Writer, filename string, reader io.Reader, extraFields map[string]string) error {
	for k, v := range extraFields {
		if k == "content_type" {
			continue
		}
		if err := w.WriteField(k, v); err != nil {
			return fmt.Errorf("write field %s: %w", k, err)
		}
	}

	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, escapeQuotes(filename)))
	ctype := "application/octet-stream"
	if ct := extraFields["content_type"]; ct != "" {
		ctype = ct
	}
	h.Set("Content-Type", ctype)
	part, err := w.CreatePart(h)
	if err != nil {
		return fmt.Errorf("create form file: %w", err)
	}
	if _, err := io.Copy(part, reader); err != nil {
		return fmt.Errorf("copy file data: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close multipart writer: %w", err)
	}
	return nil
}

// ResolveTagID resolves a tag name or numeric ID string to a tag ID.
// If nameOrID is a valid integer, it is returned directly.
// Otherwise, it fetches tags and finds a match by name.
func (c *Client) ResolveTagID(nameOrID string) (int64, error) {
	if id, err := strconv.ParseInt(nameOrID, 10, 64); err == nil {
		return id, nil
	}

	var tags []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := c.GetJSON("/api/v1/tags", &tags); err != nil {
		return 0, fmt.Errorf("fetch tags: %w", err)
	}

	nameOrID = strings.TrimSpace(nameOrID)
	for _, t := range tags {
		if strings.EqualFold(t.Name, nameOrID) {
			return t.ID, nil
		}
	}
	return 0, fmt.Errorf("tag not found: %s", nameOrID)
}

// jsonRequest sends a JSON-encoded request and decodes the response.
func (c *Client) jsonRequest(method, path string, body interface{}, result interface{}) error {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return fmt.Errorf("encode request body: %w", err)
		}
	}

	req, err := http.NewRequest(method, c.BaseURL+path, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := checkResponse(resp); err != nil {
		return err
	}
	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

// isDialError reports whether err happened while establishing the connection
// (unreachable host, unknown host, dial timeout), so the CLI exits with
// ExitConnection rather than a generic error.
func isDialError(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" {
		return true
	}
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr)
}

// isTimeout reports whether err is a transport deadline (dial, TLS handshake
// or response header) expiring.
func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// isConnectionRefused checks whether an error is a connection refused error.
func isConnectionRefused(err error) bool {
	if err == nil {
		return false
	}
	// Check for url.Error wrapping
	if uerr, ok := err.(*url.Error); ok {
		err = uerr.Err
	}
	s := err.Error()
	return strings.Contains(s, "connection refused") ||
		strings.Contains(s, "No connection could be made") ||
		strings.Contains(s, "connect: connection refused")
}
