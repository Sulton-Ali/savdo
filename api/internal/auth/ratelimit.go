package auth

import (
	"sync"
	"time"
)

// rateWindow is the fixed one-minute window a loginLimiter counts
// attempts in. docs/03-ARCHITECTURE.md § Cross-cutting: "Rate limiting:
// login and OTP endpoints per IP and per username".
const rateWindow = time.Minute

// staleAfter bounds how long a key's window is kept once it has expired,
// so evict actually shrinks the map instead of merely resetting counts —
// otherwise every IP or username Login ever saw would stay in memory for
// the life of the process.
const staleAfter = 2 * rateWindow

// loginLimiter is a fixed-window counter keyed by an arbitrary string (an
// IP address or a username). One process, one in-memory map: correct for
// Savdo's single-API-process deployment (D-23/one VPS); a multi-instance
// deployment would need a shared store instead (this is the same caveat
// that applies to any in-process rate limiter — see
// security-and-hardening's Rate Limiting section).
type loginLimiter struct {
	mu     sync.Mutex
	limit  int
	counts map[string]*window
}

type window struct {
	start time.Time
	count int
}

// newLoginLimiter builds a limiter allowing at most limit attempts per key
// per rateWindow. limit <= 0 is treated as "never allow" rather than
// "unlimited" — a zero-value config.Config (as a test's dummy *Service
// might use) must fail closed, not open.
func newLoginLimiter(limit int) *loginLimiter {
	return &loginLimiter{limit: limit, counts: map[string]*window{}}
}

// allow records one attempt for key at now and reports whether it is
// within the per-minute limit. When it is not, retryAfter is how long
// until the current window resets — the value Login puts in the 429's
// Retry-After (rounded up to a whole second, since that header is defined
// in seconds).
func (l *loginLimiter) allow(key string, now time.Time) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.evict(now)

	if l.limit <= 0 {
		return false, rateWindow
	}

	w, exists := l.counts[key]
	if !exists || now.Sub(w.start) >= rateWindow {
		l.counts[key] = &window{start: now, count: 1}
		return true, 0
	}

	if w.count >= l.limit {
		return false, rateWindow - now.Sub(w.start)
	}

	w.count++
	return true, 0
}

// evict drops windows old enough that they can no longer affect a future
// allow() call, so the map doesn't grow without bound under a distributed
// attack that never repeats a key. Called with l.mu already held.
func (l *loginLimiter) evict(now time.Time) {
	for key, w := range l.counts {
		if now.Sub(w.start) >= staleAfter {
			delete(l.counts, key)
		}
	}
}
