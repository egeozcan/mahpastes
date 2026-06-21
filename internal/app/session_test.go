package app

import (
	"crypto/tls"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSignVerifySessionTokenRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	token := signSessionToken(key, 42, time.Now().Add(time.Hour))
	id, err := verifySessionToken(key, token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if id != 42 {
		t.Fatalf("id = %d, want 42", id)
	}
}

func TestVerifySessionTokenRejectsExpired(t *testing.T) {
	key := make([]byte, 32)
	token := signSessionToken(key, 1, time.Now().Add(-time.Second))
	if _, err := verifySessionToken(key, token); err == nil {
		t.Fatal("expected expired token to be rejected")
	}
}

func TestVerifySessionTokenRejectsTamperedSignature(t *testing.T) {
	key := make([]byte, 32)
	token := signSessionToken(key, 1, time.Now().Add(time.Hour))
	other := make([]byte, 32)
	other[0] = 0xFF
	if _, err := verifySessionToken(other, token); err == nil {
		t.Fatal("expected signature mismatch to be rejected")
	}
	if _, err := verifySessionToken(key, token+"x"); err == nil {
		t.Fatal("expected tampered token to be rejected")
	}
}

func TestLoadOrCreateSigningKeyPersistsAndIsStable(t *testing.T) {
	dir := t.TempDir()
	first, err := loadOrCreateSigningKey(dir)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	if len(first) != 32 {
		t.Fatalf("key len = %d, want 32", len(first))
	}
	second, err := loadOrCreateSigningKey(dir)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if string(first) != string(second) {
		t.Fatal("signing key must be stable across loads (else every session is invalidated)")
	}
}

func TestLoadOrCreateSigningKeyFailsLoudOnCorruption(t *testing.T) {
	dir := t.TempDir()
	// A wrong-sized key file must NOT be silently regenerated — that would
	// rotate the HMAC secret and invalidate every outstanding session.
	if err := os.WriteFile(filepath.Join(dir, "session.key"), []byte("too-short"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := loadOrCreateSigningKey(dir); err == nil {
		t.Fatal("expected corrupt key file to error instead of regenerating")
	}
	// The bad file must be left intact, not overwritten.
	data, _ := os.ReadFile(filepath.Join(dir, "session.key"))
	if string(data) != "too-short" {
		t.Fatalf("corrupt key file was overwritten: %q", data)
	}
}

func TestLoginRateLimiterLocksOutAfterMaxThenIsolatesPerIP(t *testing.T) {
	l := newLoginRateLimiter()
	for i := 0; i < 5; i++ {
		if !l.allow("1.2.3.4", 5, time.Minute) {
			t.Fatalf("attempt %d should be allowed", i+1)
		}
	}
	if l.allow("1.2.3.4", 5, time.Minute) {
		t.Fatal("6th attempt from same IP should be denied")
	}
	// A different IP must have its own bucket (no global lockout).
	if !l.allow("5.6.7.8", 5, time.Minute) {
		t.Fatal("a different IP must not be collaterally locked out")
	}
}

func TestLoginRateLimiterEvictsExpiredBuckets(t *testing.T) {
	l := newLoginRateLimiter()
	window := 30 * time.Millisecond
	for i := 0; i < 10; i++ {
		l.allow(fmt.Sprintf("10.0.0.%d", i), 5, window)
	}
	if got := l.trackedIPs(); got != 10 {
		t.Fatalf("trackedIPs = %d, want 10", got)
	}
	// Let the buckets and the last-sweep timestamp age past the window, then a
	// further call triggers the sweep that evicts the now-empty buckets.
	time.Sleep(3 * window)
	l.allow("10.0.1.1", 5, window)
	if got := l.trackedIPs(); got != 1 {
		t.Fatalf("trackedIPs after sweep = %d, want 1 (expired buckets must be evicted)", got)
	}
}

func TestLoginRateLimiterSweepDoesNotInflateBuckets(t *testing.T) {
	// Regression: the sweep prunes buckets in place; it must store the truncated
	// slice back, or surviving timestamps get duplicated and over-count the next
	// attempt, falsely locking out a legitimate client.
	l := newLoginRateLimiter()
	window := time.Minute
	now := time.Now()
	expired := now.Add(-2 * window)
	fresh1 := now.Add(-window / 2)
	fresh2 := now.Add(-window / 4)
	l.buckets["ip"] = []time.Time{expired, fresh1, fresh2}
	l.lastSweep = now.Add(-2 * window) // force the sweep to run

	l.sweepLocked(now, window)

	got := l.buckets["ip"]
	if len(got) != 2 {
		t.Fatalf("bucket after sweep = %d entries, want 2 (one expired pruned, none duplicated): %v", len(got), got)
	}
	for _, ts := range got {
		if !ts.After(now.Add(-window)) {
			t.Fatalf("sweep kept an expired/duplicated timestamp: %v", ts)
		}
	}
}

func TestClientIPHonorsTrustedProxyOnly(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	req.RemoteAddr = "10.0.0.1:5555"
	req.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.9")

	untrusted := &APIManager{trustProxy: false}
	if got := untrusted.clientIP(req); got != "10.0.0.1" {
		t.Fatalf("untrusted clientIP = %q, want TCP peer 10.0.0.1 (XFF must be ignored)", got)
	}

	trusted := &APIManager{trustProxy: true}
	if got := trusted.clientIP(req); got != "10.0.0.9" {
		t.Fatalf("trusted clientIP = %q, want right-most XFF hop 10.0.0.9", got)
	}

	req.Header.Del("X-Forwarded-For")
	if got := trusted.clientIP(req); got != "10.0.0.1" {
		t.Fatalf("trusted clientIP without XFF = %q, want TCP peer 10.0.0.1", got)
	}
}

func TestCookieSecureDecision(t *testing.T) {
	plain := httptest.NewRequest("POST", "/api/v1/auth/login", nil)

	if (&APIManager{}).cookieSecure(plain) {
		t.Fatal("plaintext request without proxy trust must not set Secure")
	}
	if !(&APIManager{secureCookies: true}).cookieSecure(plain) {
		t.Fatal("MAHPASTESD_SECURE_COOKIES override must force Secure")
	}

	tlsReq := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	tlsReq.TLS = &tls.ConnectionState{}
	if !(&APIManager{}).cookieSecure(tlsReq) {
		t.Fatal("direct TLS must set Secure")
	}

	fwd := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	fwd.Header.Set("X-Forwarded-Proto", "https")
	if (&APIManager{trustProxy: false}).cookieSecure(fwd) {
		t.Fatal("X-Forwarded-Proto must be ignored without proxy trust")
	}
	if !(&APIManager{trustProxy: true}).cookieSecure(fwd) {
		t.Fatal("trusted X-Forwarded-Proto: https must set Secure")
	}
}
