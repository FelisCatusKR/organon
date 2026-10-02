package auth

import (
	"sync"
	"time"
)

// FailureLimiter blocks a client address after too many failed
// authentication attempts within a fixed window.
type FailureLimiter struct {
	Max    int           // failures allowed per window (blocked from Max+1)
	Window time.Duration // window length
	Now    func() time.Time

	mu      sync.Mutex
	entries map[string]*failures
}

type failures struct {
	count int
	start time.Time
}

// NewFailureLimiter allows max failures per window.
func NewFailureLimiter(max int, window time.Duration) *FailureLimiter {
	return &FailureLimiter{Max: max, Window: window, Now: time.Now, entries: map[string]*failures{}}
}

func (l *FailureLimiter) current(addr string) *failures {
	now := l.Now()
	e := l.entries[addr]
	if e == nil || now.Sub(e.start) >= l.Window {
		e = &failures{start: now}
		l.entries[addr] = e
	}
	return e
}

// Blocked reports whether addr has used up its failures for this window.
func (l *FailureLimiter) Blocked(addr string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.current(addr).count >= l.Max
}

// Fail records a failed attempt from addr.
func (l *FailureLimiter) Fail(addr string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.current(addr).count++
	// Keep the map from growing without bound.
	if len(l.entries) > 10000 {
		now := l.Now()
		for k, e := range l.entries {
			if now.Sub(e.start) >= l.Window {
				delete(l.entries, k)
			}
		}
	}
}
