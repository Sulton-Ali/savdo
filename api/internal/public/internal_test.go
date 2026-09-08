package public

// White-box unit tests for pure-Go helpers that need no database: the
// cache bound (T3 review round 2, CRITICAL 1), If-None-Match parsing
// (MINOR 7), pagination clamping and Invalidate's shop-memo clearing
// (MINOR 8). Every other test in this package lives in public_test
// (external, testcontainers-backed); these few need direct field access
// this file's package public gives them.

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCacheSet_boundsEntryCountAndTotalBytes(t *testing.T) {
	svc := &Service{}
	body := []byte(`{"junk":"response body"}`)

	// 3 000 distinct keys (standing in for 3 000 distinct `q` values a
	// caller could send, each already bounded in length by
	// cacheParamsKey/searchParam before reaching cacheSet) must never
	// leave more than maxCacheEntries live at once.
	for i := 0; i < 3000; i++ {
		key := fmt.Sprintf("shop-a|/v1/public/products|q=junk%d|uz", i)
		svc.cacheSet(key, cacheEntry{
			shopID: uuid.New(), status: 200, contentType: "application/json",
			body: body, etag: strongETag(body), expiresAt: time.Now().Add(cacheTTL),
		})
	}

	if got := len(svc.resp); got > maxCacheEntries {
		t.Fatalf("entries = %d, want <= maxCacheEntries (%d)", got, maxCacheEntries)
	}
	if svc.respBytes > maxCacheBytes {
		t.Fatalf("respBytes = %d, want <= maxCacheBytes (%d)", svc.respBytes, maxCacheBytes)
	}
	// respBytes must still describe what's actually in the map (the two
	// are updated together everywhere) — a real accounting bug (drift
	// between the two) would otherwise hide behind an early return.
	// entrySize (T3 review round 3, MAJOR (a)) counts the key too, not
	// just the body, so the reconciliation sum must match.
	var sum int
	for k, e := range svc.resp {
		sum += entrySize(k, e)
	}
	if sum != svc.respBytes {
		t.Fatalf("respBytes = %d, want %d (sum of entrySize over live entries)", svc.respBytes, sum)
	}
}

func TestCacheSet_neverStoresAnOversizedEntry(t *testing.T) {
	svc := &Service{}
	huge := make([]byte, maxCacheEntryBytes+1)
	svc.cacheSet("k", cacheEntry{body: huge, etag: strongETag(huge), expiresAt: time.Now().Add(cacheTTL)})
	if _, ok := svc.resp["k"]; ok {
		t.Fatal("an entry above maxCacheEntryBytes was cached, want it refused")
	}
}

func TestIfNoneMatch(t *testing.T) {
	const etag = `"abc123"`
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"empty header never matches", "", false},
		{"exact strong match", `"abc123"`, true},
		{"weak-prefixed match", `W/"abc123"`, true},
		{"star always matches", "*", true},
		{"comma list, match is the second entry", `"zzz", "abc123"`, true},
		{"comma list, no entry matches", `"zzz", "yyy"`, false},
		{"unrelated tag does not match", `"other-etag"`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ifNoneMatch(tc.raw, etag); got != tc.want {
				t.Errorf("ifNoneMatch(%q, %q) = %v, want %v", tc.raw, etag, got, tc.want)
			}
		})
	}
}

func TestClampLimit(t *testing.T) {
	intPtr := func(n int) *int { return &n }
	cases := []struct {
		name string
		in   *int
		want int32
	}{
		{"nil uses the default", nil, defaultLimit},
		{"zero uses the default", intPtr(0), defaultLimit},
		{"negative uses the default", intPtr(-1), defaultLimit},
		{"in range passes through", intPtr(5), 5},
		{"exactly maxLimit passes through", intPtr(maxLimit), maxLimit},
		{"above maxLimit is capped", intPtr(9999), maxLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clampLimit(tc.in); got != tc.want {
				t.Errorf("clampLimit(%v) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

// TestInvalidate_clearsMemoisedShopRow pins MINOR 8: Invalidate must drop
// resolveShop's own memo for the same shop (so a shop-identity write is
// visible on the very next request, not after resolveShop's own
// leftover cacheTTL), but must leave a *different* shop's memo alone.
func TestInvalidate_clearsMemoisedShopRow(t *testing.T) {
	shopID := uuid.New()
	otherShopID := uuid.New()

	svc := &Service{}
	svc.shopResolved = true
	svc.shopExpiresAt = time.Now().Add(cacheTTL)
	svc.shop.ID = shopID

	svc.Invalidate(otherShopID)
	if !svc.shopResolved {
		t.Fatal("Invalidate(otherShopID) cleared the memo for a different shop")
	}

	svc.Invalidate(shopID)
	if svc.shopResolved {
		t.Fatal("Invalidate(shopID) left the memoised shop row in place, want it cleared")
	}
}
