package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
)

// jsonEqual compares a and b by decoded value, not raw bytes: the stored
// response round-trips through a jsonb column, which re-serializes it
// (e.g. inserting a space after ':'), so a byte-exact comparison of a
// replayed body against the original would fail on formatting alone even
// though the two are the same JSON value.
func jsonEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		t.Fatalf("jsonEqual: unmarshal a (%s): %v", a, err)
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		t.Fatalf("jsonEqual: unmarshal b (%s): %v", b, err)
	}
	return reflect.DeepEqual(av, bv)
}

func idempotencyTestShop(ctx context.Context, t *testing.T, pool *pgxpool.Pool, slug string) uuid.UUID {
	t.Helper()
	q := db.New(pool)
	shopRow, err := q.CreateShop(ctx, db.CreateShopParams{ID: uuid.New(), Slug: slug, Name: "Idempotency Shop " + slug})
	if err != nil {
		t.Fatalf("CreateShop(%q): %v", slug, err)
	}
	return shopRow.ID
}

func TestIdempotent_emptyKeyRunsDirectlyEveryTime(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	shopID := idempotencyTestShop(ctx, t, pool, "idem-empty-key")

	var calls int
	fn := func() (int, []byte, error) {
		calls++
		return http.StatusCreated, []byte(`{"n":1}`), nil
	}

	for i := 0; i < 3; i++ {
		status, body, err := Idempotent(ctx, pool, shopID, "", "hash", fn)
		if err != nil {
			t.Fatalf("Idempotent (call %d): %v", i, err)
		}
		if status != http.StatusCreated || string(body) != `{"n":1}` {
			t.Fatalf("Idempotent (call %d) = %d %s, want 201 {\"n\":1}", i, status, body)
		}
	}
	if calls != 3 {
		t.Fatalf("fn called %d times with an empty key, want 3 (never deduplicated)", calls)
	}
}

func TestIdempotent_replaySameHashReturnsStoredResponseWithoutRerunningFn(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	shopID := idempotencyTestShop(ctx, t, pool, "idem-replay")

	var calls int
	fn := func() (int, []byte, error) {
		calls++
		return http.StatusCreated, []byte(`{"n":1}`), nil
	}

	status1, body1, err := Idempotent(ctx, pool, shopID, "key-1", "hash-a", fn)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	status2, body2, err := Idempotent(ctx, pool, shopID, "key-1", "hash-a", fn)
	if err != nil {
		t.Fatalf("replay call: %v", err)
	}

	if calls != 1 {
		t.Fatalf("fn called %d times, want 1 (second call must replay the stored response)", calls)
	}
	if status1 != status2 || !jsonEqual(t, body1, body2) {
		t.Fatalf("replay mismatch: first=%d %s, second=%d %s", status1, body1, status2, body2)
	}
	if status1 != http.StatusCreated || !jsonEqual(t, body1, []byte(`{"n":1}`)) {
		t.Fatalf("stored response = %d %s, want 201 {\"n\":1}", status1, body1)
	}
}

func TestIdempotent_sameKeyDifferentHashReturns409WithoutRerunningFn(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	shopID := idempotencyTestShop(ctx, t, pool, "idem-reuse")

	var calls int
	fn := func() (int, []byte, error) {
		calls++
		return http.StatusCreated, []byte(`{"n":1}`), nil
	}

	if _, _, err := Idempotent(ctx, pool, shopID, "key-1", "hash-a", fn); err != nil {
		t.Fatalf("first call: %v", err)
	}

	_, _, err := Idempotent(ctx, pool, shopID, "key-1", "hash-b", fn)
	if err == nil {
		t.Fatal("want an error for a reused key with a different hash, got none")
	}
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want *apierr.Error", err)
	}
	if apiErr.Status != http.StatusConflict {
		t.Fatalf("status = %d, want 409", apiErr.Status)
	}
	if calls != 1 {
		t.Fatalf("fn called %d times, want 1 (a hash mismatch must not run fn again)", calls)
	}
}

// TestIdempotent_errorIsNotStoredAndCanBeRetried proves the "only store on
// success" rule: a caller whose write fails (e.g. 409 STOCK_INSUFFICIENT)
// can retry with the same key once the underlying condition is fixed,
// rather than being permanently stuck replaying a failure.
func TestIdempotent_errorIsNotStoredAndCanBeRetried(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	shopID := idempotencyTestShop(ctx, t, pool, "idem-error-retry")

	wantErr := errors.New("boom")
	var calls int
	failThenSucceed := func() (int, []byte, error) {
		calls++
		if calls == 1 {
			return 0, nil, wantErr
		}
		return http.StatusCreated, []byte(`{"n":2}`), nil
	}

	_, _, err := Idempotent(ctx, pool, shopID, "key-1", "hash-a", failThenSucceed)
	if !errors.Is(err, wantErr) {
		t.Fatalf("first call error = %v, want %v", err, wantErr)
	}

	status, body, err := Idempotent(ctx, pool, shopID, "key-1", "hash-a", failThenSucceed)
	if err != nil {
		t.Fatalf("retry call: %v", err)
	}
	if calls != 2 {
		t.Fatalf("fn called %d times, want 2 (a failed first call must not be replayed)", calls)
	}
	if status != http.StatusCreated || string(body) != `{"n":2}` {
		t.Fatalf("retry response = %d %s, want 201 {\"n\":2}", status, body)
	}
}

// TestIdempotent_concurrentSameKeyRunsFnExactlyOnce is the two-goroutine
// race test the task calls for: both callers use the same (shop, key,
// hash) and both must observe the same stored response, but fn — which
// simulates a slow write — must only actually run once.
func TestIdempotent_concurrentSameKeyRunsFnExactlyOnce(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	shopID := idempotencyTestShop(ctx, t, pool, "idem-race")

	var calls int32
	fn := func() (int, []byte, error) {
		n := atomic.AddInt32(&calls, 1)
		// A tiny amount of real work widens the race window so a broken
		// implementation (no lock) reliably shows two calls instead of
		// passing by luck.
		_, err := pool.Exec(ctx, `SELECT pg_sleep(0.05)`)
		if err != nil {
			return 0, nil, err
		}
		return http.StatusCreated, []byte(`{"n":` + string(rune('0'+n)) + `}`), nil
	}

	const n = 5
	results := make([]struct {
		status int
		body   string
		err    error
	}, n)

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			status, body, err := Idempotent(ctx, pool, shopID, "race-key", "race-hash", fn)
			results[i] = struct {
				status int
				body   string
				err    error
			}{status, string(body), err}
		}(i)
	}
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("fn ran %d times across %d concurrent callers, want exactly 1", got, n)
	}
	for i, r := range results {
		if r.err != nil {
			t.Fatalf("goroutine %d: %v", i, r.err)
		}
		if r.status != http.StatusCreated {
			t.Fatalf("goroutine %d: status = %d, want 201", i, r.status)
		}
		if !jsonEqual(t, []byte(r.body), []byte(results[0].body)) {
			t.Fatalf("goroutine %d: body = %s, want %s (same as goroutine 0)", i, r.body, results[0].body)
		}
	}
}

func TestRequestHash_sameLogicalBodySameHash(t *testing.T) {
	type body struct {
		Qty    string `json:"qty"`
		Reason string `json:"reason"`
	}

	h1, err := RequestHash(http.MethodPost, "/stock/adjustments", body{Qty: "1.000", Reason: "found"})
	if err != nil {
		t.Fatalf("RequestHash: %v", err)
	}
	h2, err := RequestHash(http.MethodPost, "/stock/adjustments", body{Qty: "1.000", Reason: "found"})
	if err != nil {
		t.Fatalf("RequestHash: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("RequestHash is not deterministic for the same logical body: %q != %q", h1, h2)
	}

	h3, err := RequestHash(http.MethodPost, "/stock/adjustments", body{Qty: "2.000", Reason: "found"})
	if err != nil {
		t.Fatalf("RequestHash: %v", err)
	}
	if h1 == h3 {
		t.Fatal("RequestHash produced the same hash for two different bodies")
	}
}
