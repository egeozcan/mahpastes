package app

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const sessionTTL = 24 * time.Hour

func loadOrCreateSigningKey(dataDir string) ([]byte, error) {
	keyPath := filepath.Join(dataDir, "session.key")
	data, err := os.ReadFile(keyPath)
	switch {
	case err == nil:
		if len(data) == 32 {
			return data, nil
		}
		// A wrong-sized file is corruption, not "no key yet". Silently
		// regenerating here would rotate the HMAC secret and invalidate every
		// outstanding session, hiding the underlying problem — fail loudly so
		// the operator can investigate instead.
		return nil, fmt.Errorf("session key %s is %d bytes, expected 32 (corrupt?); refusing to overwrite", keyPath, len(data))
	case !os.IsNotExist(err):
		// A transient read error (permissions, I/O) is likewise indistinguishable
		// from "no key" only if we ignore it; surface it instead of rotating.
		return nil, fmt.Errorf("read session key %s: %w", keyPath, err)
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	// O_EXCL so a concurrent starter (or a race with a just-created file) cannot
	// be clobbered; if it now exists, re-read it rather than overwrite.
	f, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		if os.IsExist(err) {
			if data, rerr := os.ReadFile(keyPath); rerr == nil && len(data) == 32 {
				return data, nil
			}
		}
		return nil, err
	}
	defer f.Close()
	if _, err := f.Write(key); err != nil {
		return nil, err
	}
	log.Printf("api: generated new session signing key at %s", keyPath)
	return key, nil
}

func signSessionToken(key []byte, apiKeyID int64, expires time.Time) string {
	payload := fmt.Sprintf("%d.%d", apiKeyID, expires.Unix())
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func verifySessionToken(key []byte, token string) (int64, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0, fmt.Errorf("malformed token")
	}
	payload := parts[0] + "." + parts[1]
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return 0, err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	if !hmac.Equal(mac.Sum(nil), sig) {
		return 0, fmt.Errorf("bad signature")
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, err
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, err
	}
	if time.Now().Unix() > exp {
		return 0, fmt.Errorf("expired")
	}
	return id, nil
}

type loginRateLimiter struct {
	mu        sync.Mutex
	buckets   map[string][]time.Time
	lastSweep time.Time
}

func newLoginRateLimiter() *loginRateLimiter {
	return &loginRateLimiter{buckets: make(map[string][]time.Time), lastSweep: time.Now()}
}

func (l *loginRateLimiter) allow(ip string, max int, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-window)

	// Evict fully-expired buckets so the map cannot grow without bound from
	// distinct source IPs (a real risk on a 0.0.0.0-exposed daemon facing
	// IPv6/botnet rotation). Bounded to once per window to stay cheap.
	l.sweepLocked(now, window)

	attempts := pruneBefore(l.buckets[ip], cutoff)
	if len(attempts) >= max {
		l.buckets[ip] = attempts
		return false
	}
	l.buckets[ip] = append(attempts, now)
	return true
}

// pruneBefore drops timestamps at or before cutoff, reusing the backing array.
func pruneBefore(attempts []time.Time, cutoff time.Time) []time.Time {
	n := 0
	for _, t := range attempts {
		if t.After(cutoff) {
			attempts[n] = t
			n++
		}
	}
	return attempts[:n]
}

func (l *loginRateLimiter) sweepLocked(now time.Time, window time.Duration) {
	if now.Sub(l.lastSweep) < window {
		return
	}
	l.lastSweep = now
	cutoff := now.Add(-window)
	for ip, attempts := range l.buckets {
		// pruneBefore compacts survivors in place; we MUST store the truncated
		// result back. Keeping the original full-length header over the mutated
		// backing array would duplicate the surviving timestamps and inflate the
		// next count, prematurely locking out a legitimate client.
		pruned := pruneBefore(attempts, cutoff)
		if len(pruned) == 0 {
			delete(l.buckets, ip)
		} else {
			l.buckets[ip] = pruned
		}
	}
}

// trackedIPs reports how many source buckets are currently retained (test hook).
func (l *loginRateLimiter) trackedIPs() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

func remoteIP(remoteAddr string) string {
	ip, _, err := net.SplitHostPort(remoteAddr)
	if err != nil || ip == "" {
		return remoteAddr
	}
	return ip
}
