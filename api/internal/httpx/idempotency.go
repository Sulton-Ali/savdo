package httpx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// RequestHash is the "same key, same request" fingerprint docs/05-API.md §
// Conventions and docs/04-DATA-MODEL.md § 3 (idempotency_keys) describe:
// SHA-256 of the method, the path and a canonical rendering of body.
// "Canonical" here means json.Marshal of the request's already-decoded Go
// value (the strict server hands every handler a typed *Body, never raw
// bytes) — deterministic because it walks the struct's fields in their
// fixed declaration order, not a map, so two decodes of logically the same
// JSON always marshal back to the same bytes regardless of how the
// client's original bytes were formatted or ordered.
func RequestHash(method, path string, body any) (string, error) {
	canonical, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("httpx: canonicalize request body: %w", err)
	}
	sum := sha256.Sum256([]byte(method + " " + path + " " + string(canonical)))
	return hex.EncodeToString(sum[:]), nil
}

// errIdempotencyKeyReused is IDEMPOTENCYKEYREUSED (409): the same
// Idempotency-Key was already used for a request whose canonical hash
// differs from this one.
var errIdempotencyKeyReused = &apierr.Error{Status: http.StatusConflict, Code: gen.IDEMPOTENCYKEYREUSED}

// Idempotent wraps fn with the Idempotency-Key replay semantics
// docs/05-API.md § Conventions and docs/04-DATA-MODEL.md § 3 describe:
//
//   - key == "": no header was sent — run fn directly, no bookkeeping.
//   - key already used with the same requestHash: fn does not run again;
//     the response stored the first time is returned unchanged.
//   - key already used with a different requestHash: fn does not run;
//     Idempotent returns errIdempotencyKeyReused (409 IDEMPOTENCY_KEY_REUSED).
//   - key unused: fn runs. If it returns a nil error, its (status, body)
//     is stored (only ever for a 2xx status — fn is only ever asked to
//     return one on success) and returned; if it returns an error, nothing
//     is stored and the error propagates unchanged (so a genuine failure,
//     e.g. 409 STOCK_INSUFFICIENT, can always be retried with the same key).
//
// Concurrency (two requests racing the same, previously-unused key): guarded
// by pg_advisory_xact_lock(hashtext(shopID+key)) held for the lifetime of
// one Postgres transaction that Idempotent itself opens, checks the
// idempotency_keys row inside, and — only for the first caller to reach
// it — keeps open across fn's own execution before storing fn's result and
// committing. A second concurrent call for the same key blocks in
// pg_advisory_xact_lock until the first call's transaction ends, then finds
// the row the first call inserted and replays it (or 409s on a hash
// mismatch) — so fn runs at most once per key, never twice. This does mean
// fn's own writes (which run in their own, separate transaction against
// pool — Move's caller opens that transaction itself) and Idempotent's
// bookkeeping transaction are not one atomic unit: a crash between fn
// committing and Idempotent's own commit leaves the operation's effect
// applied but no idempotency_keys row recorded, so a client retry after
// such a crash would run fn a second time. Storing inside fn's own
// transaction instead (avoiding that gap entirely) would need every fn to
// accept and thread through Idempotent's *db.Queries, coupling every
// idempotent write's transaction shape to this helper; documented here as
// the accepted, narrow gap rather than taken on.
func Idempotent(ctx context.Context, pool *pgxpool.Pool, shopID uuid.UUID, key, requestHash string, fn func() (status int, body []byte, err error)) (int, []byte, error) {
	if key == "" {
		return fn()
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("httpx: idempotent: begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	qtx := db.New(tx)

	// pg_advisory_xact_lock releases automatically at commit or rollback
	// of this transaction — never held past Idempotent returning.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, shopID.String()+"|"+key); err != nil {
		return 0, nil, fmt.Errorf("httpx: idempotent: advisory lock: %w", err)
	}

	existing, err := qtx.GetIdempotencyKey(ctx, db.GetIdempotencyKeyParams{ShopID: shopID, Key: key})
	switch {
	case err == nil:
		if existing.RequestHash != requestHash {
			return 0, nil, errIdempotencyKeyReused
		}
		if err := tx.Commit(ctx); err != nil {
			return 0, nil, fmt.Errorf("httpx: idempotent: commit replay: %w", err)
		}
		committed = true
		return int(existing.ResponseStatus), existing.ResponseBody, nil
	case errors.Is(err, pgx.ErrNoRows):
		// First use of this key: fall through and run fn while still
		// holding the advisory lock, so a concurrent second caller for the
		// same key blocks until this one commits or rolls back.
	default:
		return 0, nil, fmt.Errorf("httpx: idempotent: get key: %w", err)
	}

	status, body, ferr := fn()
	if ferr != nil {
		return 0, nil, ferr
	}

	if _, err := qtx.InsertIdempotencyKey(ctx, db.InsertIdempotencyKeyParams{
		ShopID: shopID, Key: key, RequestHash: requestHash,
		ResponseStatus: int32(status), ResponseBody: body,
	}); err != nil {
		return 0, nil, fmt.Errorf("httpx: idempotent: store response: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, nil, fmt.Errorf("httpx: idempotent: commit: %w", err)
	}
	committed = true
	return status, body, nil
}

// rawJSONResponse writes a pre-serialized JSON body and status code
// verbatim — the shape Idempotent's stored-or-fresh (status, body) pair
// needs on a replay, since there is no gen.StockMovement (or other typed
// response) to re-encode: a replayed response is returned byte-for-byte
// as it was stored, not reconstructed. Each operation that uses Idempotent
// adds the one Visit method its own *ResponseObject interface needs (see
// stock.go's VisitCreateStockAdjustmentResponse).
type rawJSONResponse struct {
	status int
	body   []byte
}

func (r rawJSONResponse) write(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(r.status)
	_, err := w.Write(r.body)
	return err
}
