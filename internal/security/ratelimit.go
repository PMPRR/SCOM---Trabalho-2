package security

import (
	"sync"
	"time"
)

// RateLimiter allows a number of events per key in a time window. It lives
// in memory: enough for one server, and a restart only forgives the counts.
type RateLimiter struct {
	limit  int
	window time.Duration
	now    func() time.Time

	mu      sync.Mutex
	entries map[string]*rateEntry
}

type rateEntry struct {
	count int
	reset time.Time
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{limit: limit, window: window, now: time.Now, entries: make(map[string]*rateEntry)}
}

// Allow counts one event for key. It reports false, and how long to wait,
// once the key has used up its limit for the current window.
func (l *RateLimiter) Allow(key string) (bool, time.Duration) {
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	entry, ok := l.entries[key]
	if !ok || !now.Before(entry.reset) {
		// Expired entries of other keys are dropped along the way, so the
		// map does not grow with every address that ever tried to log in.
		for other, e := range l.entries {
			if !now.Before(e.reset) {
				delete(l.entries, other)
			}
		}
		entry = &rateEntry{reset: now.Add(l.window)}
		l.entries[key] = entry
	}

	if entry.count >= l.limit {
		return false, entry.reset.Sub(now)
	}
	entry.count++
	return true, 0
}
