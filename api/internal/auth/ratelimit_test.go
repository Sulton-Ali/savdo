package auth

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLoginLimiterAllowsUpToTheLimit(t *testing.T) {
	l := newLoginLimiter(3)
	now := time.Now()

	for i := 0; i < 3; i++ {
		ok, _ := l.allow("1.2.3.4", now)
		if !ok {
			t.Fatalf("attempt %d: allow() = false, want true (limit is 3)", i+1)
		}
	}

	ok, retryAfter := l.allow("1.2.3.4", now)
	if ok {
		t.Fatal("4th attempt: allow() = true, want false")
	}
	if retryAfter <= 0 || retryAfter > rateWindow {
		t.Fatalf("retryAfter = %v, want a positive duration within the window", retryAfter)
	}
}

func TestLoginLimiterKeysAreIndependent(t *testing.T) {
	l := newLoginLimiter(1)
	now := time.Now()

	if ok, _ := l.allow("alice", now); !ok {
		t.Fatal("alice's first attempt should be allowed")
	}
	if ok, _ := l.allow("bob", now); !ok {
		t.Fatal("bob's first attempt should be allowed even though alice is now at her limit")
	}
	if ok, _ := l.allow("alice", now); ok {
		t.Fatal("alice's second attempt within the window should be denied")
	}
}

func TestLoginLimiterResetsAfterTheWindow(t *testing.T) {
	l := newLoginLimiter(1)
	now := time.Now()

	if ok, _ := l.allow("alice", now); !ok {
		t.Fatal("first attempt should be allowed")
	}
	if ok, _ := l.allow("alice", now.Add(30*time.Second)); ok {
		t.Fatal("second attempt inside the same window should be denied")
	}
	if ok, _ := l.allow("alice", now.Add(rateWindow+time.Second)); !ok {
		t.Fatal("attempt after the window elapsed should be allowed again")
	}
}

func TestLoginLimiterEvictsStaleKeys(t *testing.T) {
	l := newLoginLimiter(1)
	now := time.Now()

	l.allow("alice", now)
	if _, exists := l.counts["alice"]; !exists {
		t.Fatal("expected alice's window to be recorded")
	}

	// A later call, long after alice's window went stale, must evict her
	// entry rather than merely leaving a reset-but-present one.
	l.allow("bob", now.Add(staleAfter+time.Second))
	if _, exists := l.counts["alice"]; exists {
		t.Fatal("expected alice's stale window to be evicted")
	}
}

func TestLoginLimiterZeroLimitDeniesEverything(t *testing.T) {
	l := newLoginLimiter(0)
	if ok, _ := l.allow("anyone", time.Now()); ok {
		t.Fatal("allow() = true with limit 0, want fail-closed (false)")
	}
}

func TestLoginLimiterEvictionIsThrottled(t *testing.T) {
	l := newLoginLimiter(1)
	now := time.Now()

	// First call always evicts (lastEvict is the zero value, so the
	// "enough time has passed" branch is trivially true) — this also
	// seeds lastEvict.
	l.allow("alice", now)

	// A key that is already stale by the time of this second call, but
	// arriving well within evictInterval of the first — evict must NOT
	// run again yet, so the stale entry lingers a little longer than it
	// strictly needs to. That's the intended trade: bounded evict cost,
	// not perfectly instantaneous cleanup.
	justBeforeThrottle := now.Add(evictInterval - time.Millisecond)
	l.counts["alice"].start = justBeforeThrottle.Add(-staleAfter - time.Second) // force-stale, bypassing allow()
	l.allow("bob", justBeforeThrottle)
	if _, exists := l.counts["alice"]; !exists {
		t.Fatal("evict ran before evictInterval elapsed and before the size threshold was hit — eviction is not throttled")
	}

	// Once evictInterval has actually elapsed, the next call must sweep
	// it.
	l.allow("carol", now.Add(evictInterval+time.Millisecond))
	if _, exists := l.counts["alice"]; exists {
		t.Fatal("expected alice's stale window to be evicted once evictInterval elapsed")
	}
}

// TestLoginLimiterEvictionHasAMinimumGapEvenAboveSizeThreshold proves the
// size trigger alone cannot make evict re-run on every call: once the map
// is above evictSizeThreshold, two allow() calls within minEvictGap of
// each other must produce exactly one evict, not two. Without the floor,
// a sustained attack using many fresh (non-stale, so never actually
// evicted) keys would keep the map above the threshold forever, making
// every single allow() call pay for a full O(n) scan — exactly the cost
// evictInterval/evictSizeThreshold exist to amortize away.
func TestLoginLimiterEvictionHasAMinimumGapEvenAboveSizeThreshold(t *testing.T) {
	l := newLoginLimiter(1000)
	now := time.Now()

	// Populate more than evictSizeThreshold fresh (non-stale) entries
	// directly, so the size trigger is active for the whole test — evict
	// itself never removes any of them (they're all "now", not stale).
	for i := 0; i < evictSizeThreshold+10; i++ {
		l.counts[strconv.Itoa(i)] = &window{start: now, count: 1}
	}

	l.allow("first", now)
	firstEvict := l.lastEvict
	if firstEvict.IsZero() {
		t.Fatal("expected the first call (lastEvict was the zero value) to run evict")
	}

	// Second call well within minEvictGap: must NOT trigger another scan,
	// even though the map is still (and will remain) above the size
	// threshold.
	l.allow("second", now.Add(minEvictGap/2))
	if !l.lastEvict.Equal(firstEvict) {
		t.Fatalf("lastEvict changed on a 2nd call within minEvictGap (%v -> %v); the size trigger must not re-fire before the floor elapses", firstEvict, l.lastEvict)
	}

	// Once minEvictGap has elapsed, the size trigger is allowed to fire
	// again.
	l.allow("third", now.Add(minEvictGap+time.Millisecond))
	if l.lastEvict.Equal(firstEvict) {
		t.Fatal("lastEvict did not advance once minEvictGap elapsed with the map still above the size threshold")
	}
}

func TestBoundKeyPassesThroughShortKeys(t *testing.T) {
	if got := boundKey("owner1"); got != "owner1" {
		t.Fatalf("boundKey(short) = %q, want unchanged", got)
	}
}

func TestBoundKeyHashesKeysOverTheLimit(t *testing.T) {
	long := strings.Repeat("a", maxKeyLen+1)
	got := boundKey(long)

	if len(got) != 64 {
		t.Fatalf("boundKey(long) length = %d, want 64 (a sha256 hex digest)", len(got))
	}
	if got == long {
		t.Fatal("boundKey(long) returned the input unchanged, want it hashed")
	}

	// Deterministic: the same over-length input must land in the same
	// bucket every time, or it would never actually get rate limited.
	if again := boundKey(long); again != got {
		t.Fatalf("boundKey(long) = %q then %q, want stable output for the same input", got, again)
	}

	// A different over-length input must hash to something different.
	other := strings.Repeat("b", maxKeyLen+1)
	if boundKey(other) == got {
		t.Fatal("two different over-length keys hashed to the same bucket")
	}
}
