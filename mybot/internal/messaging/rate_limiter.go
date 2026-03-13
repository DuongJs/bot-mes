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

// Wait reserves a token and blocks until its time slot arrives or ctx is
// cancelled. Uses a reservation-based O(1) algorithm: each call reserves one
// token immediately (tokens may go negative) and sleeps for exactly the
// deficit duration. Concurrent callers receive progressively longer waits,
// automatically spacing messages at the configured rate with zero polling.
func (r *RateLimiter) Wait(ctx context.Context) error {
	r.mu.Lock()
	now := time.Now()
	elapsed := now.Sub(r.lastRefill).Seconds()
	r.lastRefill = now

	r.tokens += elapsed * r.globalRate
	if r.tokens > float64(r.globalBurst) {
		r.tokens = float64(r.globalBurst)
	}

	// Reserve one token upfront (allow negative to schedule future callers).
	r.tokens -= 1.0
	if r.tokens >= 0 {
		r.mu.Unlock()
		return nil
	}

	// Exact wait: time for the deficit to be replenished at globalRate.
	waitDur := time.Duration(float64(time.Second) * (-r.tokens) / r.globalRate)
	r.mu.Unlock()

	metrics.Global.SendRateLimited.Add(1)

	timer := time.NewTimer(waitDur)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		// Return the reserved token on cancellation.
		r.mu.Lock()
		r.tokens += 1.0
		r.mu.Unlock()
		return ctx.Err()
	case <-timer.C:
		return nil
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
