package plugin

import (
	"context"
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
	// HTTPTimeout is the timeout for HTTP requests (5 min for long-running AI APIs)
	HTTPTimeout = 5 * time.Minute
	// HTTPMaxResponseSize is the maximum response body size (50MB)
	HTTPMaxResponseSize = 50 * 1024 * 1024
)

// HTTPBudget is a small shared holder between a Sandbox and its HTTPAPI.
// Sandbox.CallSearch sets a deadline for the duration of one on_search call
// and clears it afterwards; every other sandbox entry point leaves it unset.
// When set, plugin HTTP requests are built with a context on that budget, so a
// search against a black-holed server gives up at MaxSearchTime instead of
// holding the sandbox for the five-minute client timeout.
//
// This is deliberately scoped to the search path. Wiring the Lua deadline into
// every request would newly abort long-running auto-uploads: event handlers
// run under MaxExecutionTime (30s) and a gopher-lua deadline only fires at VM
// instruction boundaries, so blocking HTTP currently survives past it.
type HTTPBudget struct {
	mu       sync.Mutex
	deadline time.Time
	active   bool
}

// NewHTTPBudget creates an inactive budget holder.
func NewHTTPBudget() *HTTPBudget {
	return &HTTPBudget{}
}

// Set activates the budget for the given duration.
func (b *HTTPBudget) Set(d time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.deadline = time.Now().Add(d)
	b.active = true
}

// Clear deactivates the budget.
func (b *HTTPBudget) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.active = false
}

// Remaining returns the time left on the budget, if one is active. An expired
// budget reports a zero duration with ok=true so the request fails fast.
func (b *HTTPBudget) Remaining() (time.Duration, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.active {
		return 0, false
	}
	d := time.Until(b.deadline)
	if d < 0 {
		d = 0
	}
	return d, true
}

// HTTPAPI provides restricted HTTP access to plugins
type HTTPAPI struct {
	policy *NetworkPolicy // live manifest + user-grant policy
	client *http.Client
	budget *HTTPBudget

	// Rate limiting
	mu           sync.Mutex
	requestCount int
	windowStart  time.Time
}

// NewHTTPAPI creates a new HTTP API instance governed by the given policy.
func NewHTTPAPI(policy *NetworkPolicy) *HTTPAPI {
	api := &HTTPAPI{
		policy:      policy,
		windowStart: time.Now(),
	}

	// Create client with redirect validation to prevent domain bypass
	api.client = &http.Client{
		Timeout: HTTPTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Validate redirect target against the live policy, with the
			// method the original request carries.
			domain := req.URL.Hostname()
			if err := api.policy.Allowed(domain, req.Method); err != nil {
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
	api.budget = NewHTTPBudget()

	return api
}

// Budget returns the shared HTTP deadline holder this API reacts to. Hand it
// to the plugin's sandbox so CallSearch can scope a deadline to one call.
func (h *HTTPAPI) Budget() *HTTPBudget {
	return h.budget
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

// MatchDomain checks if a domain matches an allowlist entry, supporting wildcard prefixes.
// e.g. "*.fal.media" matches "v3.fal.media", "v3b.fal.media", etc.
func MatchDomain(pattern, domain string) bool {
	if pattern == domain {
		return true
	}
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // e.g. ".fal.media"
		return strings.HasSuffix(domain, suffix) && strings.Count(domain, ".") == strings.Count(suffix, ".")
	}
	return false
}

// FindAllowedMethods returns the allowed methods for a domain, checking wildcards.
func FindAllowedMethods(allowedDomains map[string][]string, domain string) ([]string, bool) {
	// Exact match first
	if methods, ok := allowedDomains[domain]; ok {
		return methods, true
	}
	// Wildcard match
	for pattern, methods := range allowedDomains {
		if MatchDomain(pattern, domain) {
			return methods, true
		}
	}
	return nil, false
}

// checkDomainPermission validates that the URL domain is allowed by the live
// policy (manifest hosts first, then user-granted hosts) and the method is
// permitted for it.
func (h *HTTPAPI) checkDomainPermission(urlStr, method string) error {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Use url.Hostname() to correctly handle IPv6 addresses and ports
	domain := parsed.Hostname()

	return h.policy.Allowed(domain, method)
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

		// Create request. When a search budget is active the request carries a
		// context on that budget so an unresponsive server cannot hold the
		// sandbox past MaxSearchTime; otherwise behavior is unchanged.
		var reqBody io.Reader
		if body != "" {
			reqBody = strings.NewReader(body)
		}

		var req *http.Request
		var err error
		if budget, ok := h.budget.Remaining(); ok {
			ctx, cancel := context.WithTimeout(context.Background(), budget)
			defer cancel()
			req, err = http.NewRequestWithContext(ctx, method, urlStr, reqBody)
		} else {
			req, err = http.NewRequest(method, urlStr, reqBody)
		}
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
