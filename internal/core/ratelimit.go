package core

import (
	"sync"
	"time"
)

// RateLimiter implements a simple token bucket rate limiter
type RateLimiter struct {
	tokens     int
	maxTokens  int
	refillRate time.Duration
	mu         sync.Mutex
	lastRefill time.Time
}

// NewRateLimiter creates a new rate limiter
// maxTokens: maximum number of requests allowed in the bucket
// refillRate: how often to add a token back
func NewRateLimiter(maxTokens int, refillRate time.Duration) *RateLimiter {
	return &RateLimiter{
		tokens:     maxTokens,
		maxTokens:  maxTokens,
		refillRate: refillRate,
		lastRefill: time.Now(),
	}
}

// Wait blocks until a token is available
func (rl *RateLimiter) Wait() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Refill tokens based on time elapsed
	now := time.Now()
	elapsed := now.Sub(rl.lastRefill)
	tokensToAdd := int(elapsed / rl.refillRate)

	if tokensToAdd > 0 {
		rl.tokens += tokensToAdd
		if rl.tokens > rl.maxTokens {
			rl.tokens = rl.maxTokens
		}
		rl.lastRefill = now
	}

	// Wait if no tokens available
	for rl.tokens <= 0 {
		rl.mu.Unlock()
		time.Sleep(rl.refillRate)
		rl.mu.Lock()

		// Refill after sleep
		now = time.Now()
		elapsed = now.Sub(rl.lastRefill)
		tokensToAdd = int(elapsed / rl.refillRate)

		if tokensToAdd > 0 {
			rl.tokens += tokensToAdd
			if rl.tokens > rl.maxTokens {
				rl.tokens = rl.maxTokens
			}
			rl.lastRefill = now
		}
	}

	// Consume a token
	rl.tokens--
}

// Update updates the rate limiter settings
func (rl *RateLimiter) Update(maxTokens int, refillRate time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.maxTokens = maxTokens
	rl.refillRate = refillRate
	if rl.tokens > maxTokens {
		rl.tokens = maxTokens
	}
}

// ApplyRateLimitConfig applies the global rate limit settings from config to all API rate limiters
func ApplyRateLimitConfig(requestsPerSecond float64, burst int) {
	if requestsPerSecond <= 0 {
		requestsPerSecond = 1.0
	}
	if burst <= 0 {
		burst = 1
	}

	refill := time.Duration(float64(time.Second) / requestsPerSecond)

	// Access the package-level rate limiters
	gutenbergRateLimiter.Update(burst, refill)
	kiwixRateLimiter.Update(burst, refill)
}
