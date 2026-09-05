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

// GetSale and ListSales below satisfy gen.StrictServerInterface's two read
// sales operations by forwarding to server.sales (*sales.Handler) — same
// reasoning as stock.go's own doc comment.
//
// CreateSale is the exception, the same way ReceivePurchase and
// CreateStockAdjustment are (purchases.go's/stock.go's own doc comments):
// it accepts an Idempotency-Key (docs/05-API.md § Conventions), and
// httpx.Idempotent — the helper that implements that replay semantics —
// lives in this package rather than internal/sales, because internal/httpx
// already imports internal/sales to wire the router and the reverse
// import would cycle. So this method does the request-hash/replay
// orchestration itself and calls sales.Handler.CreateSaleTx (a plain
// (gen.Sale, error) method taking the transaction Idempotent opened, not
// one of gen.StrictServerInterface's) for the actual write.

// ListSales lists the shop's sales.
func (s server) ListSales(ctx context.Context, req gen.ListSalesRequestObject) (gen.ListSalesResponseObject, error) {
	return s.sales.ListSales(ctx, req)
}

// GetSale gets a sale.
func (s server) GetSale(ctx context.Context, req gen.GetSaleRequestObject) (gen.GetSaleResponseObject, error) {
	return s.sales.GetSale(ctx, req)
}

// salesPath is the RequestHash canonicalization path for POST /sales —
// like stockAdjustmentPath (stock.go), this operation's path never varies,
// so there is nothing to read off the request for it beyond the body
// itself.
const salesPath = "/sales"

// CreateSale completes a quick sale, replaying a previous response for a
// repeated Idempotency-Key (docs/05-API.md § Conventions) instead of
// running the write again. The whole operation — the sale_out movements,
// the sale/items/payment inserts and (if a key was sent) the
// idempotency_keys row — runs on the one transaction Idempotent opens; see
// sales.Handler.CreateSaleTx's and Idempotent's own doc comments for why
// that matters (the same BLOCKER 1/MAJOR 2 fix CreateStockAdjustment's and
// ReceivePurchase's own doc comments describe).
func (s server) CreateSale(ctx context.Context, req gen.CreateSaleRequestObject) (gen.CreateSaleResponseObject, error) {
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
	hash, err := RequestHash(http.MethodPost, salesPath, authCtx.UserID, authCtx.Role, *req.Body)
	if err != nil {
		return nil, err
	}

	status, body, err := Idempotent(ctx, s.pool, authCtx.ShopID, key, hash, func(qtx *db.Queries) (int, []byte, error) {
		sale, err := s.sales.CreateSaleTx(ctx, qtx, req.Body)
		if err != nil {
			return 0, nil, err
		}
		b, err := json.Marshal(sale)
		if err != nil {
			return 0, nil, fmt.Errorf("httpx: marshal create sale response: %w", err)
		}
		return http.StatusCreated, b, nil
	})
	if err != nil {
		return nil, err
	}
	return rawJSONResponse{status: status, body: body}, nil
}

// VisitCreateSaleResponse lets rawJSONResponse (idempotency.go) stand in
// for CreateSale201JSONResponse on a replay, where the stored bytes are
// written back verbatim rather than re-encoded from a gen.Sale.
func (r rawJSONResponse) VisitCreateSaleResponse(w http.ResponseWriter) error {
	return r.write(w)
}
