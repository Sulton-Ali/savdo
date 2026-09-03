package auth

import (
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
