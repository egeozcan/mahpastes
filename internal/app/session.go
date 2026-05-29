package app

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
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
	if data, err := os.ReadFile(keyPath); err == nil && len(data) == 32 {
		return data, nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, key, 0600); err != nil {
		return nil, err
	}
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
	mu      sync.Mutex
	buckets map[string][]time.Time
}

func newLoginRateLimiter() *loginRateLimiter {
	return &loginRateLimiter{buckets: make(map[string][]time.Time)}
}

func (l *loginRateLimiter) allow(ip string, max int, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-window)
	attempts := l.buckets[ip]
	n := 0
	for _, t := range attempts {
		if t.After(cutoff) {
			attempts[n] = t
			n++
		}
	}
	attempts = attempts[:n]
	if len(attempts) >= max {
		l.buckets[ip] = attempts
		return false
	}
	l.buckets[ip] = append(attempts, now)
	return true
}

func remoteIP(remoteAddr string) string {
	ip, _, err := net.SplitHostPort(remoteAddr)
	if err != nil || ip == "" {
		return remoteAddr
	}
	return ip
}
