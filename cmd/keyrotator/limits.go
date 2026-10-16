package main

import (
	"math"
	"sync"
	"time"
)

// bucket is a plain token bucket: tokens accrue at `rate` per second, up to
// `burst`. It is used for both provider-wide and per-key rate limits.
type bucket struct {
	tokens float64
	last   time.Time
}

// limiter is a registry of token buckets keyed by an opaque string
// ("p:<providerID>" or "k:<keyID>"). Entries are created on demand and evicted
// once they have been idle for a while, so the map does not grow forever.
type limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	lastGC  time.Time
}

func newLimiter() *limiter {
	return &limiter{buckets: map[string]*bucket{}, lastGC: time.Now()}
}

// burstFor allows a small burst so a limit of N/s behaves like N requests in
// the first second rather than one every 1/N seconds.
func burstFor(rps float64) float64 {
	b := math.Ceil(rps)
	if b < 1 {
		b = 1
	}
	return b
}

// take consumes one token. rps <= 0 means unlimited. When the bucket is empty
// it reports how many seconds until a token is available.
func (l *limiter) take(key string, rps float64) (bool, float64) {
	if rps <= 0 {
		return true, 0
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.gcLocked(now)

	b := l.buckets[key]
	burst := burstFor(rps)
	if b == nil {
		b = &bucket{tokens: burst, last: now}
		l.buckets[key] = b
	}
	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens = math.Min(burst, b.tokens+elapsed*rps)
		b.last = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, (1 - b.tokens) / rps
}

// allow is take for callers that only need the yes/no answer.
func (l *limiter) allow(key string, rps float64) bool {
	ok, _ := l.take(key, rps)
	return ok
}

// wait reports how long until the bucket has a free token, without consuming.
func (l *limiter) wait(key string, rps float64) float64 {
	if rps <= 0 {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[key]
	if b == nil {
		return 0
	}
	tokens := math.Min(burstFor(rps), b.tokens+time.Since(b.last).Seconds()*rps)
	if tokens >= 1 {
		return 0
	}
	return (1 - tokens) / rps
}

func (l *limiter) gcLocked(now time.Time) {
	if now.Sub(l.lastGC) < 5*time.Minute {
		return
	}
	l.lastGC = now
	for k, b := range l.buckets {
		if now.Sub(b.last) > 10*time.Minute {
			delete(l.buckets, k)
		}
	}
}
