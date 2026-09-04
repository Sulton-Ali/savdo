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

// idempotencyLockClassID is the first argument to the two-argument
// pg_advisory_xact_lock(classid, key) Idempotent uses (NIT 9) — a second,
// independent advisory-lock namespace from stock.Rebuild's own
// rebuildLockClassID, so a coincidental hashtext() collision between an
// idempotency key and a shop id can never make the two locks interfere.
const idempotencyLockClassID = 1

// maxIdempotencyKeyLen matches contracts/openapi.yaml's IdempotencyKey
// schema (`maxLength: 128`, NIT 10) — the strict server does not enforce
// JSON Schema constraints on header parameters at runtime, so this is
// re-checked explicitly rather than trusted from the generated type alone.
const maxIdempotencyKeyLen = 128

// RequestHash is the "same key, same request, same caller" fingerprint
// docs/05-API.md § Conventions and docs/04-DATA-MODEL.md § 3
// (idempotency_keys) describe: SHA-256 of the method, the path, the acting
// user's id and a canonical rendering of body. actorID is part of the hash
// (MINOR 7), not just the lookup key, because Idempotent's stored response
// body was already role-shaped for whoever made the first request (e.g.
// unitCost present or null per their cost.read) — Phase 4 shares this
// helper across cashiers and owners on the same shop, so two different
// actors reusing the same client-chosen key must never let the second one
// silently receive a body rendered for the first; they get 409
// IDEMPOTENCY_KEY_REUSED instead, the same as any other hash mismatch.
// "Canonical" here means json.Marshal of the request's already-decoded Go
// value (the strict server hands every handler a typed *Body, never raw
// bytes) — deterministic because it walks the struct's fields in their
// fixed declaration order, not a map, so two decodes of logically the same
// JSON always marshal back to the same bytes regardless of how the
// client's original bytes were formatted or ordered.
func RequestHash(method, path string, actorID uuid.UUID, body any) (string, error) {
	canonical, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("httpx: canonicalize request body: %w", err)
	}
	sum := sha256.Sum256([]byte(method + "|" + path + "|" + actorID.String() + "|" + string(canonical)))
	return hex.EncodeToString(sum[:]), nil
}

// errIdempotencyKeyReused is IDEMPOTENCYKEYREUSED (409): the same
// Idempotency-Key was already used for a request whose canonical hash
// differs from this one — including, since RequestHash folds the actor id
// in, the same logical request replayed by a different user (MINOR 7).
var errIdempotencyKeyReused = &apierr.Error{Status: http.StatusConflict, Code: gen.IDEMPOTENCYKEYREUSED}

// ValidateIdempotencyKey checks key against contracts/openapi.yaml's
// IdempotencyKey schema (NIT 10: `maxLength: 128`) — an empty key is valid
// (it means "no header sent", handled by Idempotent itself), only an
// over-length one is rejected, as 400 VALIDATION_FAILED naming the header.
func ValidateIdempotencyKey(key string) error {
	if len(key) > maxIdempotencyKeyLen {
		return apierr.Validation(map[string]string{"Idempotency-Key": "too_long"})
	}
	return nil
}

// Idempotent wraps fn with the Idempotency-Key replay semantics
// docs/05-API.md § Conventions and docs/04-DATA-MODEL.md § 3 describe, and
// runs fn on Idempotent's own transaction rather than a second one fn
// opens itself (BLOCKER 1 / MAJOR 2 fix — see the "single connection"
// paragraph below):
//
//   - key == "": no header was sent — fn still runs inside a transaction
//     (Move and audit.Write both need one), but with no advisory lock, no
//     idempotency_keys lookup and nothing stored: every call is a fresh
//     write.
//   - key already used with the same requestHash: fn does not run again;
//     the response stored the first time is returned unchanged.
//   - key already used with a different requestHash (including, per
//     RequestHash's own doc comment, the same body replayed by a
//     different actor): fn does not run; Idempotent returns
//     errIdempotencyKeyReused (409 IDEMPOTENCY_KEY_REUSED).
//   - key unused: fn(qtx) runs. If it returns a nil error, its
//     (status, body) is stored in the same transaction and the whole
//     thing commits together; if it returns an error, the whole
//     transaction rolls back and nothing is stored (so a genuine failure,
//     e.g. 409 STOCK_INSUFFICIENT, can always be retried with the same
//     key, and a failure that happens after fn's domain write — e.g. the
//     key-row insert itself failing — takes the domain write back out
//     with it, never leaving a durable movement with no matching key row).
//
// Single connection, single transaction: fn used to open its own,
// separate transaction (a second pooled connection) while this function's
// own transaction sat idle-in-transaction holding the advisory lock for
// fn's entire duration. Under a bounded pool, N concurrent
// Idempotency-Key'd requests could then each hold one connection for their
// own Idempotent transaction while waiting for a second, for fn — a
// connection that only becomes free once some other request's *pair*
// releases both — which can deadlock the whole pool (reproduced with
// MaxConns=4 and enough concurrent callers). Running fn(qtx) on this
// function's own transaction means one request needs exactly one
// connection, end to end, so that failure mode cannot occur; it also
// closes the earlier "movement committed, key row not" gap for free, since
// there is now only one commit.
//
// Concurrency (two requests racing the same, previously-unused key):
// guarded by pg_advisory_xact_lock(idempotencyLockClassID, hashtext(...))
// (NIT 9's two-argument form — a class id distinct from stock.Rebuild's
// own, so the two locks' key spaces can never collide) held for the
// lifetime of this transaction. A second concurrent call for the same key
// blocks in pg_advisory_xact_lock until the first call's transaction ends,
// then finds the row the first call inserted and replays it (or 409s on a
// hash mismatch) — so fn runs at most once per key, never twice.
func Idempotent(ctx context.Context, pool *pgxpool.Pool, shopID, actorID uuid.UUID, key, requestHash string, fn func(qtx *db.Queries) (status int, body []byte, err error)) (int, []byte, error) {
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

	if key == "" {
		status, body, ferr := fn(qtx)
		if ferr != nil {
			return 0, nil, ferr
		}
		if err := tx.Commit(ctx); err != nil {
			return 0, nil, fmt.Errorf("httpx: idempotent: commit: %w", err)
		}
		committed = true
		return status, body, nil
	}

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, hashtext($2))`, idempotencyLockClassID, shopID.String()+"|"+key); err != nil {
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
		// First use of this key: fall through and run fn on this same
		// transaction, still holding the advisory lock, so a concurrent
		// second caller for the same key blocks until this one commits or
		// rolls back.
	default:
		return 0, nil, fmt.Errorf("httpx: idempotent: get key: %w", err)
	}

	status, body, ferr := fn(qtx)
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
// response) to re-encode. A replay's bytes are not guaranteed byte-
// identical to the original response: response_body is a jsonb column,
// which re-serializes on the way back out (e.g. inserting a space after
// ':'), so what comes back is JSON-equal to what was stored, not
// necessarily byte-equal — never compare a replay by raw string equality,
// only by decoding both sides (see idempotency_test.go's jsonEqual).
// Each operation that uses Idempotent adds the one Visit method its own
// *ResponseObject interface needs (see stock.go's
// VisitCreateStockAdjustmentResponse).
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
