package plugin

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	maxPluginSize = 1 << 20 // 1MB
	fetchTimeout  = 30 * time.Second
)

// FetchPluginSource downloads a plugin .lua file from a URL.
// Validates URL scheme (HTTP/HTTPS only) and enforces a 1MB size limit.
func FetchPluginSource(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return "", fmt.Errorf("unsupported URL scheme %q: only http and https are allowed", parsed.Scheme)
	}

	client := &http.Client{Timeout: fetchTimeout}
	resp, err := client.Get(rawURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch plugin: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server returned HTTP %d", resp.StatusCode)
	}

	limited := io.LimitReader(resp.Body, maxPluginSize+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}
	if len(body) > maxPluginSize {
		return "", fmt.Errorf("plugin source exceeds maximum size of %d bytes", maxPluginSize)
	}

	return string(body), nil
}
