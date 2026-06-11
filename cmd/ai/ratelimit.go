// Package main — Sprint 6 P0-9 (ODR-013, ADR-017 §2)
//
// AI service token-bucket rate limiter.
//
// Background
// ----------
// The AI service shells out to an LLM provider (OpenAI / Anthropic /
// domestic equivalents) on every `/api/research/*` call. Without a
// rate limit a single misbehaving client (or a leaked test script)
// can rack up thousands of dollars of LLM cost in a few minutes.
//
// ADR-017 §2 mandates:
//
//	"AI service 加 token bucket 限流: golang.org/x/time/rate
//	 10 req/min/user"
//
// Design choices
// --------------
//
//  1. Token bucket (not fixed window). The analysis-service
//     middleware uses a per-IP fixed-window counter; the AI service
//     uses a per-user *token bucket* because:
//       - the cost of an LLM call is bursty (a research session
//         can fire 5–10 calls in quick succession), and
//       - we want to allow short bursts but cap sustained rate.
//     `rate.NewLimiter(rate.Every(6*time.Second), 10)` gives exactly
//     10 req/min sustained with a burst of 10 tokens.
//
//  2. Per-user keyed on `X-User-ID` header, falling back to
//     `c.ClientIP()`. The AI service does NOT have JWT auth yet
//     (that's Sprint 6 P1-8, ADR-017 §2); the X-User-ID header is
//     set by the analysis-service reverse proxy after the P1-8
//     middleware runs. Until then, we rate-limit per-IP, which
//     is the next-best signal.
//
//  3. Idle visitor GC. A naive `sync.Map` of limiters would grow
//     unbounded as new clients hit the service. A background
//     goroutine evicts limiters whose last access is older than
//     `maxIdle` (default 10 minutes).
//
//  4. Skip /health. Health probes run every few seconds from the
//     load balancer and MUST NOT be rate-limited (otherwise the
//     service would self-DOS).
//
//  5. 429 + Retry-After. The standard `Retry-After` header tells
//     well-behaved clients when to come back. The body is a small
//     JSON error envelope matching the rest of the API surface.
package main

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// userLimiter pairs a per-user token bucket with the last time we
// saw a request from that user. lastSeen drives the idle GC; the
// limiter itself is the rate enforcement primitive.
type userLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// rateLimiter is the Sprint 6 P0-9 (ADR-017 §2) per-user token
// bucket. Constructed once at process start by main(); the
// returned middleware is installed on the gin Engine BEFORE the
// route group so every /api/research/* call is subject to the
// same bucket.
type rateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*userLimiter
	rate     rate.Limit
	burst    int
	maxIdle  time.Duration
}

// newRateLimiter creates a token-bucket rate limiter with the given
// sustained rate (per minute) and burst capacity. burst should be
// >= 1; values smaller than the sustained rate effectively cap the
// throughput at burst. Defaults: 10 req/min sustained, burst 10.
//
// The constructor spawns a background goroutine that evicts
// idle visitors every `maxIdle/2` so the map doesn't grow
// unboundedly under churn. The goroutine exits when the process
// exits (no Stop method is exposed; the GC loop is
// best-effort and runs at process lifetime).
func newRateLimiter(perMinute, burst int) *rateLimiter {
	// rate.Every(time.Minute / N) yields a Limit of N per minute.
	rl := &rateLimiter{
		visitors: make(map[string]*userLimiter),
		rate:     rate.Every(time.Minute / time.Duration(perMinute)),
		burst:    burst,
		maxIdle:  10 * time.Minute,
	}
	go rl.cleanupLoop()
	return rl
}

// cleanupLoop evicts visitors that haven't been seen in maxIdle.
// We do NOT lock the map for the full duration; we work on a
// snapshot of stale keys under the lock and delete them one at a
// time so concurrent allow() calls don't block on the GC sweep.
func (rl *rateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.maxIdle / 2)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-rl.maxIdle)
		rl.mu.Lock()
		var stale []string
		for k, v := range rl.visitors {
			if v.lastSeen.Before(cutoff) {
				stale = append(stale, k)
			}
		}
		rl.mu.Unlock()
		for _, k := range stale {
			rl.mu.Lock()
			// Re-check: the visitor may have been touched
			// between our snapshot and the lock acquisition.
			if v, ok := rl.visitors[k]; ok && v.lastSeen.Before(cutoff) {
				delete(rl.visitors, k)
			}
			rl.mu.Unlock()
		}
	}
}

// allow returns true if the user has tokens left, and (on success)
// records the access time. It does NOT block waiting for a token —
// the caller is expected to fail fast and return 429.
func (rl *rateLimiter) allow(user string) bool {
	rl.mu.Lock()
	v, ok := rl.visitors[user]
	if !ok {
		v = &userLimiter{
			limiter: rate.NewLimiter(rl.rate, rl.burst),
		}
		rl.visitors[user] = v
	}
	v.lastSeen = time.Now()
	limiter := v.limiter
	rl.mu.Unlock()
	// Allow() is concurrency-safe on rate.Limiter; we deliberately
	// release rl.mu before calling it so a slow Allow doesn't stall
	// other users' admission decisions.
	return limiter.Allow()
}

// reserveDelay returns the time the caller would need to wait
// before a token becomes available. Used to populate the
// Retry-After header on 429 responses. Returns 1 second minimum
// because sub-second Retry-After values are not well-defined in
// HTTP/1.1 (RFC 7231 §7.1.3).
func (rl *rateLimiter) reserveDelay(user string) time.Duration {
	rl.mu.Lock()
	v, ok := rl.visitors[user]
	if !ok {
		rl.mu.Unlock()
		return 0
	}
	limiter := v.limiter
	rl.mu.Unlock()
	r := limiter.Reserve()
	if !r.OK() {
		// Burst is zero or rate is zero — never allow.
		return time.Hour
	}
	d := r.Delay()
	r.Cancel() // we don't actually want to consume the token
	if d < time.Second {
		return time.Second
	}
	return d
}

// middleware returns the gin handler that enforces the rate
// limit. It MUST be installed on the engine (not on a route
// group) so it covers every /api/* call uniformly.
//
// `X-User-ID` header is preferred; if absent, c.ClientIP() is
// used. This means anonymous clients behind the same NAT share
// a bucket — that's intentional (it prevents a single IP from
// burning the LLM budget).
func (rl *rateLimiter) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Health probes are always exempt — see package doc §4.
		if c.Request.URL.Path == "/health" {
			c.Next()
			return
		}
		user := c.GetHeader("X-User-ID")
		if user == "" {
			user = "ip:" + c.ClientIP()
		}
		if !rl.allow(user) {
			delay := rl.reserveDelay(user)
			c.Header("Retry-After", strconv.Itoa(int(delay.Seconds())))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":       "rate limit exceeded",
				"limit_per_min": int(rl.rate * 60),
				"retry_after_seconds": int(delay.Seconds()),
			})
			return
		}
		c.Next()
	}
}
