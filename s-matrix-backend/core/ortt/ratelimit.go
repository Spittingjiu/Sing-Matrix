package ortt

import (
	"sync"
	"time"
)

// RateLimiter implements a dual-level token bucket (Global + Per-IP)
type RateLimiter struct {
	mu           sync.Mutex
	globalTokens float64
	globalMax    float64
	globalRate   float64 // tokens per second
	lastGlobal   time.Time

	ipLimiters map[string]*ipBucket
}

type ipBucket struct {
	tokens float64
	last   time.Time
}

func NewRateLimiter(globalRPS float64, perIpRPS float64) *RateLimiter {
	return &RateLimiter{
		globalTokens: globalRPS,
		globalMax:    globalRPS,
		globalRate:   globalRPS,
		lastGlobal:   time.Now(),
		ipLimiters:   make(map[string]*ipBucket),
	}
}

// Allow returns true if packet is within rate limits
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	// 1. Global limit check
	elapsed := now.Sub(rl.lastGlobal).Seconds()
	rl.lastGlobal = now
	rl.globalTokens += elapsed * rl.globalRate
	if rl.globalTokens > rl.globalMax {
		rl.globalTokens = rl.globalMax
	}
	if rl.globalTokens < 1.0 {
		return false
	}

	// 2. Per-IP limit check (max 30 tokens burst, 10 rps)
	b, exists := rl.ipLimiters[ip]
	if !exists {
		b = &ipBucket{tokens: 30.0, last: now}
		rl.ipLimiters[ip] = b
	} else {
		ipElapsed := now.Sub(b.last).Seconds()
		b.last = now
		b.tokens += ipElapsed * 10.0 // 10 tokens per sec
		if b.tokens > 30.0 {
			b.tokens = 30.0
		}
	}

	if b.tokens < 1.0 {
		return false
	}

	// Consume tokens
	rl.globalTokens -= 1.0
	b.tokens -= 1.0

	// Periodic prune of idle IP buckets if map exceeds 500 entries
	if len(rl.ipLimiters) > 500 {
		for k, v := range rl.ipLimiters {
			if now.Sub(v.last) > 60*time.Second {
				delete(rl.ipLimiters, k)
			}
		}
	}

	return true
}
