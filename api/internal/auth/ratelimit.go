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

// maxEntries bounds the number of addresses tracked at once.
const maxEntries = 10000

// Blocked reports whether addr has used up its failures for this window.
// It never records anything, so successful requests cost no memory.
func (l *FailureLimiter) Blocked(addr string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.entries[addr]
	return e != nil && l.Now().Sub(e.start) < l.Window && e.count >= l.Max
}

// Fail records a failed attempt from addr.
func (l *FailureLimiter) Fail(addr string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.Now()
	e := l.entries[addr]
	if e == nil || now.Sub(e.start) >= l.Window {
		if e == nil && len(l.entries) >= maxEntries {
			l.evict(now)
		}
		e = &failures{start: now}
		l.entries[addr] = e
	}
	e.count++
}

// evict makes room for one entry: expired entries go first, then the
// oldest one if every entry is still live.
func (l *FailureLimiter) evict(now time.Time) {
	var oldest string
	for k, e := range l.entries {
		if now.Sub(e.start) >= l.Window {
			delete(l.entries, k)
		} else if oldest == "" || e.start.Before(l.entries[oldest].start) {
			oldest = k
		}
	}
	if len(l.entries) >= maxEntries {
		delete(l.entries, oldest)
	}
}
