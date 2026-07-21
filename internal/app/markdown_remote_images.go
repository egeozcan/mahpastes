package app

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const markdownRemoteImageMaxRedirects = 5

type MarkdownImageProgress struct {
	RequestID string  `json:"request_id"`
	State     string  `json:"state"` // queued, downloading, complete, cancelled, error
	Bytes     int64   `json:"bytes"`
	Total     int64   `json:"total"`
	Percent   float64 `json:"percent"`
	Error     string  `json:"error,omitempty"`
}

type MarkdownRemoteImageResult struct {
	RequestID   string `json:"request_id"`
	ContentType string `json:"content_type"`
	Data        string `json:"data"`
	Size        int64  `json:"size"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	DecodedSize int64  `json:"decoded_size"`
	Cached      bool   `json:"cached"`
}

type markdownRemoteImageLoader struct {
	cache        *markdownImageCache
	emitProgress func(MarkdownImageProgress)
	client       *http.Client
	allowPrivate bool
	semaphore    chan struct{}
	mu           sync.Mutex
	active       map[string]context.CancelFunc
	cacheEpoch   atomic.Uint64
	cacheCommit  sync.RWMutex
}

func newMarkdownRemoteImageLoader(cache *markdownImageCache, emit func(MarkdownImageProgress), client *http.Client, allowPrivate bool) *markdownRemoteImageLoader {
	if client == nil {
		client = &http.Client{Transport: newMarkdownImageTransport(allowPrivate)}
	} else {
		clone := *client
		client = &clone
	}
	client.Timeout = 60 * time.Second
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= markdownRemoteImageMaxRedirects {
			return fmt.Errorf("too many redirects")
		}
		return validateMarkdownRemoteURL(req.URL, allowPrivate)
	}
	return &markdownRemoteImageLoader{
		cache: cache, emitProgress: emit, client: client, allowPrivate: allowPrivate,
		semaphore: make(chan struct{}, 3), active: map[string]context.CancelFunc{},
	}
}

func (l *markdownRemoteImageLoader) Load(parent context.Context, requestID, rawURL string) (MarkdownRemoteImageResult, error) {
	loadCacheEpoch := l.cacheEpoch.Load()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return MarkdownRemoteImageResult{}, fmt.Errorf("invalid image URL: %w", err)
	}
	if err := validateMarkdownRemoteURL(parsed, l.allowPrivate); err != nil {
		return MarkdownRemoteImageResult{}, err
	}
	if requestID == "" {
		return MarkdownRemoteImageResult{}, fmt.Errorf("request ID is required")
	}

	if l.cache != nil {
		entry, hit, err := l.cache.Get(rawURL)
		if err != nil {
			return MarkdownRemoteImageResult{}, err
		}
		if hit {
			contentType, width, height, decodedSize, err := validateMarkdownImage(entry.Data, entry.ContentType)
			if err == nil {
				result := markdownRemoteResult(requestID, entry.Data, contentType, width, height, decodedSize, true)
				l.emit(MarkdownImageProgress{RequestID: requestID, State: "complete", Bytes: result.Size, Total: result.Size, Percent: 100})
				return result, nil
			}
		}
	}

	ctx, cancel := context.WithCancel(parent)
	if err := l.register(requestID, cancel); err != nil {
		cancel()
		return MarkdownRemoteImageResult{}, err
	}
	defer func() {
		cancel()
		l.unregister(requestID)
	}()

	l.emit(MarkdownImageProgress{RequestID: requestID, State: "queued", Total: -1})
	select {
	case l.semaphore <- struct{}{}:
		defer func() { <-l.semaphore }()
	case <-ctx.Done():
		l.emit(MarkdownImageProgress{RequestID: requestID, State: "cancelled", Total: -1})
		return MarkdownRemoteImageResult{}, ctx.Err()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return MarkdownRemoteImageResult{}, err
	}
	req.Header.Set("Accept", "image/png,image/jpeg,image/gif,image/webp")
	req.Header.Set("User-Agent", "mahpastes-markdown-image/1")

	resp, err := l.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			l.emit(MarkdownImageProgress{RequestID: requestID, State: "cancelled", Total: -1})
			return MarkdownRemoteImageResult{}, ctx.Err()
		}
		l.emit(MarkdownImageProgress{RequestID: requestID, State: "error", Total: -1, Error: err.Error()})
		return MarkdownRemoteImageResult{}, fmt.Errorf("fetch remote image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := fmt.Errorf("remote image returned HTTP %d", resp.StatusCode)
		l.emit(MarkdownImageProgress{RequestID: requestID, State: "error", Total: resp.ContentLength, Error: err.Error()})
		return MarkdownRemoteImageResult{}, err
	}
	if resp.ContentLength > maxMarkdownImageBytes {
		return MarkdownRemoteImageResult{}, fmt.Errorf("remote image exceeds %d byte limit", maxMarkdownImageBytes)
	}

	total := resp.ContentLength
	if total < 0 {
		total = -1
	}
	l.emit(MarkdownImageProgress{RequestID: requestID, State: "downloading", Total: total})
	data, err := l.readWithProgress(ctx, requestID, resp.Body, total)
	if err != nil {
		return MarkdownRemoteImageResult{}, err
	}

	declaredType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || declaredType == "" {
		return MarkdownRemoteImageResult{}, fmt.Errorf("remote image has invalid content type")
	}
	contentType, width, height, decodedSize, err := validateMarkdownImage(data, declaredType)
	if err != nil {
		return MarkdownRemoteImageResult{}, err
	}

	if err := ctx.Err(); err != nil {
		return MarkdownRemoteImageResult{}, err
	}
	if l.cache != nil {
		if ttl := markdownResponseCacheTTL(resp.Header, time.Now()); ttl > 0 {
			l.cacheCommit.RLock()
			if err := ctx.Err(); err != nil || loadCacheEpoch != l.cacheEpoch.Load() {
				l.cacheCommit.RUnlock()
				if err != nil {
					return MarkdownRemoteImageResult{}, err
				}
				return MarkdownRemoteImageResult{}, context.Canceled
			}
			err := l.cache.Put(rawURL, data, contentType, ttl)
			l.cacheCommit.RUnlock()
			if err != nil {
				return MarkdownRemoteImageResult{}, err
			}
		}
	}
	result := markdownRemoteResult(requestID, data, contentType, width, height, decodedSize, false)
	l.emit(MarkdownImageProgress{RequestID: requestID, State: "complete", Bytes: result.Size, Total: result.Size, Percent: 100})
	return result, nil
}

func markdownRemoteResult(requestID string, data []byte, contentType string, width, height int, decodedSize int64, cached bool) MarkdownRemoteImageResult {
	return MarkdownRemoteImageResult{
		RequestID: requestID, ContentType: contentType, Data: base64.StdEncoding.EncodeToString(data),
		Size: int64(len(data)), Width: width, Height: height, DecodedSize: decodedSize, Cached: cached,
	}
}

func (l *markdownRemoteImageLoader) readWithProgress(ctx context.Context, requestID string, reader io.Reader, total int64) ([]byte, error) {
	limited := io.LimitReader(reader, maxMarkdownImageBytes+1)
	data := make([]byte, 0, minInt64(total, maxMarkdownImageBytes))
	buffer := make([]byte, 32*1024)
	lastEmit := time.Time{}
	for {
		count, err := limited.Read(buffer)
		if count > 0 {
			data = append(data, buffer[:count]...)
			if len(data) > maxMarkdownImageBytes {
				return nil, fmt.Errorf("remote image exceeds %d byte limit", maxMarkdownImageBytes)
			}
			now := time.Now()
			if lastEmit.IsZero() || now.Sub(lastEmit) >= 100*time.Millisecond || err == io.EOF {
				progress := MarkdownImageProgress{RequestID: requestID, State: "downloading", Bytes: int64(len(data)), Total: total}
				if total > 0 {
					progress.Percent = float64(len(data)) * 100 / float64(total)
				}
				l.emit(progress)
				lastEmit = now
			}
		}
		if err == io.EOF {
			return data, nil
		}
		if err != nil {
			if ctx.Err() != nil {
				l.emit(MarkdownImageProgress{RequestID: requestID, State: "cancelled", Bytes: int64(len(data)), Total: total})
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("read remote image: %w", err)
		}
		select {
		case <-ctx.Done():
			l.emit(MarkdownImageProgress{RequestID: requestID, State: "cancelled", Bytes: int64(len(data)), Total: total})
			return nil, ctx.Err()
		default:
		}
	}
}

func (l *markdownRemoteImageLoader) ClearCache() error {
	l.cacheEpoch.Add(1)
	l.CancelAll()
	l.cacheCommit.Lock()
	defer l.cacheCommit.Unlock()
	if l.cache == nil {
		return fmt.Errorf("Markdown image cache is not initialized")
	}
	return l.cache.Clear()
}

func (l *markdownRemoteImageLoader) Cancel(requestID string) bool {
	l.mu.Lock()
	cancel := l.active[requestID]
	l.mu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

func (l *markdownRemoteImageLoader) CancelAll() {
	l.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(l.active))
	for _, cancel := range l.active {
		cancels = append(cancels, cancel)
	}
	l.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (l *markdownRemoteImageLoader) register(requestID string, cancel context.CancelFunc) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, exists := l.active[requestID]; exists {
		return fmt.Errorf("request ID %q is already active", requestID)
	}
	l.active[requestID] = cancel
	return nil
}

func (l *markdownRemoteImageLoader) unregister(requestID string) {
	l.mu.Lock()
	delete(l.active, requestID)
	l.mu.Unlock()
}

func (l *markdownRemoteImageLoader) emit(progress MarkdownImageProgress) {
	if l.emitProgress != nil {
		l.emitProgress(progress)
	}
}

func validateMarkdownRemoteURL(parsed *url.URL, allowPrivate bool) error {
	if parsed == nil || parsed.Scheme != "https" {
		return fmt.Errorf("remote Markdown images require HTTPS")
	}
	if parsed.User != nil {
		return fmt.Errorf("remote image URLs cannot contain credentials")
	}
	if parsed.Hostname() == "" {
		return fmt.Errorf("remote image URL has no host")
	}
	if !allowPrivate {
		if ip := net.ParseIP(parsed.Hostname()); ip != nil && !isPublicMarkdownImageIP(ip) {
			return fmt.Errorf("remote image host resolves to a private or local address")
		}
	}
	return nil
}

func newMarkdownImageTransport(allowPrivate bool) *http.Transport {
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	return &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, address := range addresses {
				if !allowPrivate && !isPublicMarkdownImageIP(address.IP) {
					continue
				}
				return dialer.DialContext(ctx, network, net.JoinHostPort(address.IP.String(), port))
			}
			return nil, fmt.Errorf("remote image host resolves to a private or local address")
		},
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}
}

var markdownImageDeniedNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.31.196.0/24"),
	netip.MustParsePrefix("192.52.193.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("192.175.48.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("2620:4f:8000::/48"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("5f00::/16"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fec0::/10"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

func isPublicMarkdownImageIP(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() {
		return false
	}
	for _, denied := range markdownImageDeniedNetworks {
		if denied.Contains(address) {
			return false
		}
	}
	return true
}

func markdownResponseCacheTTL(header http.Header, now time.Time) time.Duration {
	if strings.Contains(header.Get("Vary"), "*") {
		return 0
	}
	cacheControl := strings.ToLower(header.Get("Cache-Control"))
	var maxAge *time.Duration
	for _, directive := range strings.Split(cacheControl, ",") {
		directive = strings.TrimSpace(directive)
		if strings.HasPrefix(directive, "no-store") || strings.HasPrefix(directive, "no-cache") {
			return 0
		}
		if strings.HasPrefix(directive, "max-age=") {
			seconds, err := strconv.ParseInt(strings.Trim(strings.TrimPrefix(directive, "max-age="), `"`), 10, 64)
			if err == nil {
				ttl := time.Duration(seconds) * time.Second
				maxAge = &ttl
			}
		}
	}
	if maxAge != nil {
		age := time.Duration(0)
		if seconds, err := strconv.ParseInt(strings.TrimSpace(header.Get("Age")), 10, 64); err == nil && seconds > 0 {
			age = time.Duration(seconds) * time.Second
		}
		if date, err := http.ParseTime(header.Get("Date")); err == nil && now.After(date) && now.Sub(date) > age {
			age = now.Sub(date)
		}
		ttl := *maxAge - age
		if ttl <= 0 {
			return 0
		}
		if ttl < markdownImageCacheTTL {
			return ttl
		}
		return markdownImageCacheTTL
	}
	if expires := header.Get("Expires"); expires != "" {
		if expiry, err := http.ParseTime(expires); err == nil {
			ttl := expiry.Sub(now)
			if ttl <= 0 {
				return 0
			}
			if ttl < markdownImageCacheTTL {
				return ttl
			}
		}
	}
	return markdownImageCacheTTL
}

func minInt64(value int64, maximum int) int {
	if value <= 0 || value > int64(maximum) {
		return 0
	}
	return int(value)
}
