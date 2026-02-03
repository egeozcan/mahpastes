package plugin

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"
)

const (
	// HTTPRequestsPerMinute is the rate limit for HTTP requests
	HTTPRequestsPerMinute = 100
	// HTTPTimeout is the timeout for HTTP requests
	HTTPTimeout = 30 * time.Second
	// HTTPMaxResponseSize is the maximum response body size (10MB)
	HTTPMaxResponseSize = 10 * 1024 * 1024
)

// HTTPAPI provides restricted HTTP access to plugins
type HTTPAPI struct {
	allowedDomains map[string][]string // domain -> allowed methods
	client         *http.Client

	// Rate limiting
	mu           sync.Mutex
	requestCount int
	windowStart  time.Time
}

// NewHTTPAPI creates a new HTTP API instance with the given allowed domains
func NewHTTPAPI(allowedDomains map[string][]string) *HTTPAPI {
	api := &HTTPAPI{
		allowedDomains: allowedDomains,
		windowStart:    time.Now(),
	}

	// Create client with redirect validation to prevent domain bypass
	api.client = &http.Client{
		Timeout: HTTPTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Validate redirect URL against allowlist
			domain := req.URL.Hostname()
			if _, ok := api.allowedDomains[domain]; !ok {
				return fmt.Errorf("redirect to unauthorized domain: %s", domain)
			}
			// Prevent downgrade to non-HTTPS
			if req.URL.Scheme != "https" {
				return fmt.Errorf("redirect to non-HTTPS URL not allowed: %s", req.URL.String())
			}
			// Limit redirects to 10
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	return api
}

// Register adds the http module to the Lua state
func (h *HTTPAPI) Register(L *lua.LState) {
	httpMod := L.NewTable()

	httpMod.RawSetString("get", L.NewFunction(h.makeRequest("GET")))
	httpMod.RawSetString("post", L.NewFunction(h.makeRequest("POST")))
	httpMod.RawSetString("put", L.NewFunction(h.makeRequest("PUT")))
	httpMod.RawSetString("patch", L.NewFunction(h.makeRequest("PATCH")))
	httpMod.RawSetString("delete", L.NewFunction(h.makeRequest("DELETE")))

	L.SetGlobal("http", httpMod)
}

// checkDomainPermission validates that the URL domain is in the allowlist and method is allowed
func (h *HTTPAPI) checkDomainPermission(urlStr, method string) error {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Use url.Hostname() to correctly handle IPv6 addresses and ports
	domain := parsed.Hostname()

	allowedMethods, ok := h.allowedDomains[domain]
	if !ok {
		return fmt.Errorf("domain not in allowlist: %s", domain)
	}

	// Check if method is allowed for this domain
	methodAllowed := false
	for _, m := range allowedMethods {
		if strings.EqualFold(m, method) {
			methodAllowed = true
			break
		}
	}

	if !methodAllowed {
		return fmt.Errorf("%s not allowed for domain %s (allowed: [%s])", method, domain, strings.Join(allowedMethods, ", "))
	}

	return nil
}

// checkRateLimit enforces rate limiting
func (h *HTTPAPI) checkRateLimit() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now()

	// Reset window if a minute has passed
	if now.Sub(h.windowStart) >= time.Minute {
		h.requestCount = 0
		h.windowStart = now
	}

	if h.requestCount >= HTTPRequestsPerMinute {
		return fmt.Errorf("rate limit exceeded: %d requests per minute", HTTPRequestsPerMinute)
	}

	h.requestCount++
	return nil
}

// makeRequest returns a Lua function that handles HTTP requests for the given method
func (h *HTTPAPI) makeRequest(method string) lua.LGFunction {
	return func(L *lua.LState) int {
		urlStr := L.CheckString(1)

		// Check domain permission
		if err := h.checkDomainPermission(urlStr, method); err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}

		// Check rate limit
		if err := h.checkRateLimit(); err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}

		// Parse options
		var body string
		headers := make(map[string]string)

		if L.GetTop() >= 2 {
			if opts, ok := L.Get(2).(*lua.LTable); ok {
				// Get body
				if bodyVal := opts.RawGetString("body"); bodyVal != lua.LNil {
					body = bodyVal.String()
				}

				// Get headers
				if headersVal := opts.RawGetString("headers"); headersVal != lua.LNil {
					if headersTable, ok := headersVal.(*lua.LTable); ok {
						headersTable.ForEach(func(k, v lua.LValue) {
							headers[k.String()] = v.String()
						})
					}
				}
			}
		}

		// Create request
		var reqBody io.Reader
		if body != "" {
			reqBody = strings.NewReader(body)
		}

		req, err := http.NewRequest(method, urlStr, reqBody)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}

		// Set headers
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		// Execute request
		resp, err := h.client.Do(req)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		defer resp.Body.Close()

		// Read response body with size limit
		limitedReader := io.LimitReader(resp.Body, HTTPMaxResponseSize)
		respBody, err := io.ReadAll(limitedReader)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}

		// Build response table
		result := L.NewTable()
		result.RawSetString("status", lua.LNumber(resp.StatusCode))
		result.RawSetString("body", lua.LString(string(respBody)))

		// Build headers table
		respHeaders := L.NewTable()
		for k, v := range resp.Header {
			if len(v) > 0 {
				respHeaders.RawSetString(k, lua.LString(v[0]))
			}
		}
		result.RawSetString("headers", respHeaders)

		L.Push(result)
		return 1
	}
}
