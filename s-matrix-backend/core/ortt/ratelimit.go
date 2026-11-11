package ortt

import (
	"math"
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

	perIpRate float64
	perIpMax  float64

	ipLimiters map[string]*ipBucket
}

type ipBucket struct {
	tokens float64
	last   time.Time
}

func NewRateLimiter(globalRPS float64, perIpRPS float64) *RateLimiter {
	return NewRateLimiterWithBurst(globalRPS, perIpRPS, math.Max(5.0, perIpRPS))
}

func NewRateLimiterWithBurst(globalRPS float64, perIpRPS float64, perIpBurst float64) *RateLimiter {
	if globalRPS <= 0 {
		globalRPS = 1.0
	}
	if perIpRPS <= 0 {
		perIpRPS = 1.0
	}
	if perIpBurst < 1.0 {
		perIpBurst = 1.0
	}
	return &RateLimiter{
		globalTokens: globalRPS,
		globalMax:    globalRPS,
		globalRate:   globalRPS,
		lastGlobal:   time.Now(),
		perIpRate:    perIpRPS,
		perIpMax:     perIpBurst,
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
	if elapsed < 0 {
		elapsed = 0
	}
	rl.lastGlobal = now
	rl.globalTokens += elapsed * rl.globalRate
	if rl.globalTokens > rl.globalMax {
		rl.globalTokens = rl.globalMax
	}
	if rl.globalTokens < 1.0 {
		return false
	}

	// 2. Per-IP limit check
	b, exists := rl.ipLimiters[ip]
	if !exists {
		b = &ipBucket{tokens: rl.perIpMax, last: now}
		rl.ipLimiters[ip] = b
	} else {
		ipElapsed := now.Sub(b.last).Seconds()
		if ipElapsed < 0 {
			ipElapsed = 0
		}
		b.last = now
		b.tokens += ipElapsed * rl.perIpRate
		if b.tokens > rl.perIpMax {
			b.tokens = rl.perIpMax
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
