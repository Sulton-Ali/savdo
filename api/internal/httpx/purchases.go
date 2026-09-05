package httpx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// The four methods below satisfy gen.StrictServerInterface's list/get/
// create/update purchase operations by forwarding to server.stock
// (*stock.Handler) — same reasoning as stock.go's own doc comment.
//
// ReceivePurchase is the exception, the same way CreateStockAdjustment is
// (stock.go's own doc comment): it accepts an Idempotency-Key
// (docs/05-API.md § Conventions), and httpx.Idempotent — the helper that
// implements that replay semantics — lives in this package rather than
// internal/stock, because internal/httpx already imports internal/stock to
// wire the router and the reverse import would cycle. So this method does
// the request-hash/replay orchestration itself and calls
// stock.Handler.ReceivePurchaseTx (a plain (gen.Purchase, error) method
// taking the transaction Idempotent opened, not one of
// gen.StrictServerInterface's) for the actual write. CancelPurchase has no
// Idempotency-Key in the contract, so it forwards directly like
// CreateStockTransfer does.

// ListPurchases lists the shop's purchases.
func (s server) ListPurchases(ctx context.Context, req gen.ListPurchasesRequestObject) (gen.ListPurchasesResponseObject, error) {
	return s.stock.ListPurchases(ctx, req)
}

// CreatePurchase creates a draft purchase.
func (s server) CreatePurchase(ctx context.Context, req gen.CreatePurchaseRequestObject) (gen.CreatePurchaseResponseObject, error) {
	return s.stock.CreatePurchase(ctx, req)
}

// GetPurchase gets a purchase.
func (s server) GetPurchase(ctx context.Context, req gen.GetPurchaseRequestObject) (gen.GetPurchaseResponseObject, error) {
	return s.stock.GetPurchase(ctx, req)
}

// UpdatePurchase updates a draft purchase.
func (s server) UpdatePurchase(ctx context.Context, req gen.UpdatePurchaseRequestObject) (gen.UpdatePurchaseResponseObject, error) {
	return s.stock.UpdatePurchase(ctx, req)
}

// CancelPurchase cancels a purchase.
func (s server) CancelPurchase(ctx context.Context, req gen.CancelPurchaseRequestObject) (gen.CancelPurchaseResponseObject, error) {
	return s.stock.CancelPurchase(ctx, req)
}

// receivePurchasePath is the RequestHash canonicalization path for
// POST /purchases/{id}/receive — unlike stockAdjustmentPath (stock.go),
// this operation's path has an {id} segment, so it must be part of what
// the hash covers (stockAdjustmentPath's own doc comment already flagged
// this as "a future idempotent operation would need to include" it): two
// different purchases replayed with the same Idempotency-Key by the same
// actor must not be treated as the same request.
func receivePurchasePath(id uuid.UUID) string {
	return fmt.Sprintf("/purchases/%s/receive", id)
}

// ReceivePurchase receives a draft purchase, replaying a previous response
// for a repeated Idempotency-Key (docs/05-API.md § Conventions) instead of
// running the write again. The whole operation — the movements, the
// cost_override updates, the audit row and (if a key was sent) the
// idempotency_keys row — runs on the one transaction Idempotent opens; see
// stock.Handler.ReceivePurchaseTx's and Idempotent's own doc comments for
// why that matters. POST /purchases/{id}/receive has no request body, so
// RequestHash's body argument is the path parameter id (via
// receivePurchasePath rolled into path) plus a nil body — actorID and the
// id together already uniquely identify "this receive, by this actor".
func (s server) ReceivePurchase(ctx context.Context, req gen.ReceivePurchaseRequestObject) (gen.ReceivePurchaseResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}

	var key string
	if req.Params.IdempotencyKey != nil {
		key = *req.Params.IdempotencyKey
	}
	if err := ValidateIdempotencyKey(key); err != nil {
		return nil, err
	}
	hash, err := RequestHash(http.MethodPost, receivePurchasePath(req.Id), authCtx.UserID, authCtx.Role, nil)
	if err != nil {
		return nil, err
	}

	status, body, err := Idempotent(ctx, s.pool, authCtx.ShopID, key, hash, func(qtx *db.Queries) (int, []byte, error) {
		p, err := s.stock.ReceivePurchaseTx(ctx, qtx, req.Id)
		if err != nil {
			return 0, nil, err
		}
		b, err := json.Marshal(p)
		if err != nil {
			return 0, nil, fmt.Errorf("httpx: marshal receive purchase response: %w", err)
		}
		return http.StatusOK, b, nil
	})
	if err != nil {
		return nil, err
	}
	return rawJSONResponse{status: status, body: body}, nil
}

// VisitReceivePurchaseResponse lets rawJSONResponse (idempotency.go) stand
// in for ReceivePurchase200JSONResponse on a replay, where the stored
// bytes are written back verbatim rather than re-encoded from a
// gen.Purchase.
func (r rawJSONResponse) VisitReceivePurchaseResponse(w http.ResponseWriter) error {
	return r.write(w)
}
