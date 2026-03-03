package messaging

import (
	"context"
	"sync"
	"time"

	"mybot/internal/metrics"
)

// RateLimiter implements a token-bucket rate limiter for outgoing messages.
// It supports a global rate and optional per-thread rates.
type RateLimiter struct {
	globalRate  float64
	globalBurst int
	mu          sync.Mutex
	tokens      float64
	lastRefill  time.Time
}

// NewRateLimiter creates a limiter that allows ratePerSecond messages per
// second with the given burst bucket size.
func NewRateLimiter(ratePerSecond, burst int) *RateLimiter {
	if ratePerSecond <= 0 {
		ratePerSecond = 30
	}
	if burst <= 0 {
		burst = 10
	}
	return &RateLimiter{
		globalRate:  float64(ratePerSecond),
		globalBurst: burst,
		tokens:      float64(burst),
		lastRefill:  time.Now(),
	}
}

// Wait blocks until a token is available or ctx is cancelled.
// Returns nil when a token is acquired, or the context error.
func (r *RateLimiter) Wait(ctx context.Context) error {
	for {
		if r.tryAcquire() {
			return nil
		}
		metrics.Global.SendRateLimited.Add(1)

		// Wait a short interval before retrying.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

// TryAcquire attempts to consume one token without blocking.
func (r *RateLimiter) TryAcquire() bool {
	return r.tryAcquire()
}

func (r *RateLimiter) tryAcquire() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(r.lastRefill).Seconds()
	r.lastRefill = now

	r.tokens += elapsed * r.globalRate
	if r.tokens > float64(r.globalBurst) {
		r.tokens = float64(r.globalBurst)
	}

	if r.tokens >= 1.0 {
		r.tokens -= 1.0
		return true
	}
	return false
}
