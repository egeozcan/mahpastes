package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
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
		return nil, fmt.Errorf("MP_API_KEY environment variable is required.\n\nTo get started:\n  1. Open mahpastes Settings > API\n  2. Create an API key\n  3. export MP_API_KEY=<your-key>")
	}

	return &Client{
		BaseURL:    baseURL,
		APIKey:     apiKey,
		HTTPClient: &http.Client{},
	}, nil
}

// do executes an HTTP request with auth headers and handles common errors.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "mp-cli/1.0")
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		if isConnectionRefused(err) {
			return nil, &APIError{
				StatusCode: 0,
				Message:    "Cannot connect to mahpastes. Is the app running with the API enabled?",
			}
		}
		return nil, fmt.Errorf("request failed: %w", err)
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
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	for k, v := range extraFields {
		if err := w.WriteField(k, v); err != nil {
			return fmt.Errorf("write field %s: %w", k, err)
		}
	}

	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return fmt.Errorf("create form file: %w", err)
	}
	if _, err := io.Copy(part, reader); err != nil {
		return fmt.Errorf("copy file data: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequest("POST", c.BaseURL+path, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

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
	if err := c.GetJSON("/api/tags", &tags); err != nil {
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
