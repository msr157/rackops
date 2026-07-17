package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// tokenBucket is a classic refill-on-demand token bucket.
type tokenBucket struct {
	tokens   float64
	last     time.Time
	rate     float64 // tokens per second
	capacity float64
}

func (b *tokenBucket) allow(now time.Time) bool {
	elapsed := now.Sub(b.last).Seconds()
	b.last = now
	b.tokens = min(b.capacity, b.tokens+elapsed*b.rate)
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// rateLimiter tracks token buckets per key (an IP or an email address).
// Idle buckets are evicted opportunistically so memory stays bounded.
type rateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*tokenBucket
	rate     float64
	burst    float64
	lastSwep time.Time
	now      func() time.Time
}

// newRateLimiter allows `perMinute` sustained requests with `burst` headroom.
func newRateLimiter(perMinute float64, burst int) *rateLimiter {
	return &rateLimiter{
		buckets: map[string]*tokenBucket{},
		rate:    perMinute / 60.0,
		burst:   float64(burst),
		now:     time.Now,
	}
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := rl.now()
	// Sweep stale buckets at most once a minute.
	if now.Sub(rl.lastSwep) > time.Minute {
		rl.lastSwep = now
		idle := max(time.Duration(rl.burst/rl.rate)*time.Second, 10*time.Minute)
		for k, b := range rl.buckets {
			if now.Sub(b.last) > idle {
				delete(rl.buckets, k)
			}
		}
	}
	b, ok := rl.buckets[key]
	if !ok {
		b = &tokenBucket{tokens: rl.burst, last: now, rate: rl.rate, capacity: rl.burst}
		rl.buckets[key] = b
	}
	return b.allow(now)
}

// clientIP extracts the caller's IP. X-Forwarded-For is honored (first hop)
// because the CRM is expected to sit behind a reverse proxy in production;
// direct deployments simply see the socket address.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if first, _, ok := strings.Cut(xff, ","); ok {
			return strings.TrimSpace(first)
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
