package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// rateWindow is the fixed one-minute window newLoginLimiter's default
// (login's own) limiter counts attempts in. docs/03-ARCHITECTURE.md §
// Cross-cutting: "Rate limiting: login and OTP endpoints per IP and per
// username" — OTP's own limiters (otp.go's otpRateWindow, 15 minutes) use
// newWindowedLimiter instead, the same type with a longer window.
const rateWindow = time.Minute

// staleAfter bounds how long a key's window is kept once it has expired,
// so evict actually shrinks the map instead of merely resetting counts —
// otherwise every IP or username Login ever saw would stay in memory for
// the life of the process. This is login's own default; a loginLimiter
// built with newWindowedLimiter scales the same 2x factor off its own
// window instead (the stale field below).
const staleAfter = 2 * rateWindow

// maxKeyLen bounds how much of a caller-supplied key (a username; an IP
// is already short and fixed-shape) is ever stored verbatim. Login already
// rejects a username over 64 bytes at the API boundary
// (handler.go's maxLoginUsernameLength, contracts/openapi.yaml's
// LoginRequest.username maxLength), but that validation lives one layer
// up — this bound holds even if a future caller (Service.Login is not
// itself length-limited; the bot's own login path, or a test) skips it,
// so a single request can never grow one map entry without bound.
const maxKeyLen = 64

// evictInterval and evictSizeThreshold amortize evict's O(n) full-map scan:
// running it on every allow() call would itself become the bottleneck
// under a distributed attack hammering many distinct keys. evict only
// actually runs when the map has grown large enough to matter or enough
// time has passed since the last sweep — whichever comes first — subject
// to minEvictGap below.
//
// minEvictGap is a hard floor under both of those triggers. Without it,
// once the map is sitting above evictSizeThreshold, the size disjunct
// alone would re-fire on *every single* allow() call for as long as the
// map stays that large: eviction only removes keys older than staleAfter,
// so a sustained attack using many fresh (non-stale) keys keeps the map
// above the threshold indefinitely, and "size > threshold" would
// otherwise re-trigger the O(n) scan on every call — defeating the whole
// point of amortizing it. minEvictGap guarantees at least this long
// between two scans regardless of which condition tripped.
const (
	evictInterval      = 10 * time.Second
	evictSizeThreshold = 10_000
	minEvictGap        = 1 * time.Second
)

// loginLimiter is a fixed-window counter keyed by an arbitrary string (an
// IP address or a username). One process, one in-memory map: correct for
// Savdo's single-API-process deployment (D-23/one VPS); a multi-instance
// deployment would need a shared store instead (this is the same caveat
// that applies to any in-process rate limiter — see
// security-and-hardening's Rate Limiting section).
type loginLimiter struct {
	mu        sync.Mutex
	limit     int
	perWindow time.Duration // the fixed window this instance counts attempts in
	stale     time.Duration // how long a key's window is kept once expired (2x perWindow)
	counts    map[string]*window
	lastEvict time.Time
}

type window struct {
	start time.Time
	count int
}

// newLoginLimiter builds a limiter allowing at most limit attempts per key
// per rateWindow (login's own one-minute window). limit <= 0 is treated as
// "never allow" rather than "unlimited" — a zero-value config.Config (as a
// test's dummy *Service might use) must fail closed, not open.
func newLoginLimiter(limit int) *loginLimiter {
	return newWindowedLimiter(limit, rateWindow)
}

// newWindowedLimiter is newLoginLimiter generalized to an arbitrary
// window: otp.go's RequestOtp/VerifyOtp rate limits
// (docs/03-ARCHITECTURE.md § Cross-cutting: "login and OTP endpoints per
// IP and per username") need a longer window (15 minutes) than login's
// own one-minute rateWindow, but share every other mechanic — fixed-window
// counting, throttled eviction, key bounding — with loginLimiter, so this
// is the same type parameterized by window rather than a second copy of
// it. stale scales the same 2x factor staleAfter uses for login's default
// window: long enough that evict never drops a key while it could still
// matter, short enough the map doesn't hold every key forever.
func newWindowedLimiter(limit int, win time.Duration) *loginLimiter {
	return &loginLimiter{limit: limit, perWindow: win, stale: 2 * win, counts: map[string]*window{}}
}

// allow records one attempt for key at now and reports whether it is
// within the per-minute limit. When it is not, retryAfter is how long
// until the current window resets — the value Login puts in the 429's
// Retry-After (rounded up to a whole second, since that header is defined
// in seconds).
func (l *loginLimiter) allow(key string, now time.Time) (ok bool, retryAfter time.Duration) {
	key = boundKey(key)

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.shouldEvict(now) {
		l.evict(now)
		l.lastEvict = now
	}

	if l.limit <= 0 {
		return false, l.perWindow
	}

	w, exists := l.counts[key]
	if !exists || now.Sub(w.start) >= l.perWindow {
		l.counts[key] = &window{start: now, count: 1}
		return true, 0
	}

	if w.count >= l.limit {
		return false, l.perWindow - now.Sub(w.start)
	}

	w.count++
	return true, 0
}

// shouldEvict decides whether evict should run now, given the two triggers
// (map size, elapsed time) and the minEvictGap floor under both of them.
// Called with l.mu already held.
func (l *loginLimiter) shouldEvict(now time.Time) bool {
	sinceLastEvict := now.Sub(l.lastEvict)
	if sinceLastEvict < minEvictGap {
		return false
	}
	return len(l.counts) > evictSizeThreshold || sinceLastEvict >= evictInterval
}

// evict drops windows old enough that they can no longer affect a future
// allow() call, so the map doesn't grow without bound under a distributed
// attack that never repeats a key. Called with l.mu already held; callers
// throttle how often this runs (see evictInterval/evictSizeThreshold)
// since it is an O(n) scan of the whole map.
func (l *loginLimiter) evict(now time.Time) {
	for key, w := range l.counts {
		if now.Sub(w.start) >= l.stale {
			delete(l.counts, key)
		}
	}
}

// boundKey caps a rate-limiter key at maxKeyLen bytes, hashing it down to
// a fixed 64-character hex digest when it is longer, so no single caller
// can grow the limiter's map by an unbounded amount with one long key.
// Hashing (not truncating) keeps the mapping collision-resistant and
// still deterministic — the same over-length input always lands in the
// same bucket, which is what actually rate-limits it.
func boundKey(key string) string {
	if len(key) <= maxKeyLen {
		return key
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}
