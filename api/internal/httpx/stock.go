package httpx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// The four methods below satisfy gen.StrictServerInterface's read and
// transfer stock operations by forwarding to server.stock (*stock.Handler)
// — named, not embedded, for the same reason server.shop/server.catalog
// are (healthz.go's doc comment on the server struct). Replaces what
// unimplemented.go stubbed out before the stock module existed
// (docs/06-ROADMAP.md Phase 3 T3).
//
// CreateStockAdjustment is the exception: it accepts an Idempotency-Key
// (docs/05-API.md § Conventions), and httpx.Idempotent — the helper that
// implements that replay semantics — lives in this package rather than
// internal/stock, because internal/httpx already imports internal/stock to
// wire the router and the reverse import would cycle. So this method does
// the request-hash/replay orchestration itself and calls
// stock.Handler.CreateAdjustmentTx (a plain (gen.StockMovement, error)
// method taking the transaction Idempotent opened, not one of
// gen.StrictServerInterface's) for the actual write.

// ListStockLevels lists stock levels per variant and location.
func (s server) ListStockLevels(ctx context.Context, req gen.ListStockLevelsRequestObject) (gen.ListStockLevelsResponseObject, error) {
	return s.stock.ListStockLevels(ctx, req)
}

// ListStockMovements lists the append-only stock movement ledger.
func (s server) ListStockMovements(ctx context.Context, req gen.ListStockMovementsRequestObject) (gen.ListStockMovementsResponseObject, error) {
	return s.stock.ListStockMovements(ctx, req)
}

// CreateStockTransfer transfers stock between two locations.
func (s server) CreateStockTransfer(ctx context.Context, req gen.CreateStockTransferRequestObject) (gen.CreateStockTransferResponseObject, error) {
	return s.stock.CreateStockTransfer(ctx, req)
}

// ListLowStock lists variants at or below their effective low-stock
// threshold.
func (s server) ListLowStock(ctx context.Context, req gen.ListLowStockRequestObject) (gen.ListLowStockResponseObject, error) {
	return s.stock.ListLowStock(ctx, req)
}

// stockAdjustmentPath is the fixed path RequestHash canonicalizes against
// — POST /stock/adjustments never varies, so there is nothing to read off
// the request for it (contrast a path with an {id} segment, which a
// future idempotent operation would need to include).
const stockAdjustmentPath = "/stock/adjustments"

// CreateStockAdjustment records a manual stock adjustment, replaying a
// previous response for a repeated Idempotency-Key (docs/05-API.md §
// Conventions) instead of running the write again. The whole operation —
// the movement, the audit row and (if a key was sent) the idempotency_keys
// row — runs on the one transaction Idempotent opens; see
// stock.Handler.CreateAdjustmentTx's and Idempotent's own doc comments for
// why that matters (BLOCKER 1 / MAJOR 2).
func (s server) CreateStockAdjustment(ctx context.Context, req gen.CreateStockAdjustmentRequestObject) (gen.CreateStockAdjustmentResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}
	if req.Body == nil {
		return nil, apierr.Validation(map[string]string{"body": "required"})
	}

	var key string
	if req.Params.IdempotencyKey != nil {
		key = *req.Params.IdempotencyKey
	}
	if err := ValidateIdempotencyKey(key); err != nil {
		return nil, err
	}
	hash, err := RequestHash(http.MethodPost, stockAdjustmentPath, authCtx.UserID, authCtx.Role, *req.Body)
	if err != nil {
		return nil, err
	}

	status, body, err := Idempotent(ctx, s.pool, authCtx.ShopID, key, hash, func(qtx *db.Queries) (int, []byte, error) {
		mv, err := s.stock.CreateAdjustmentTx(ctx, qtx, *req.Body)
		if err != nil {
			return 0, nil, err
		}
		b, err := json.Marshal(mv)
		if err != nil {
			return 0, nil, fmt.Errorf("httpx: marshal stock adjustment response: %w", err)
		}
		return http.StatusCreated, b, nil
	})
	if err != nil {
		return nil, err
	}
	return rawJSONResponse{status: status, body: body}, nil
}

// VisitCreateStockAdjustmentResponse lets rawJSONResponse (idempotency.go)
// stand in for CreateStockAdjustment201JSONResponse on a replay, where the
// stored bytes are written back verbatim rather than re-encoded from a
// gen.StockMovement.
func (r rawJSONResponse) VisitCreateStockAdjustmentResponse(w http.ResponseWriter) error {
	return r.write(w)
}
