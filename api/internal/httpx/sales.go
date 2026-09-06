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

// GetSale and ListSales below satisfy gen.StrictServerInterface's two read
// sales operations by forwarding to server.sales (*sales.Handler) — same
// reasoning as stock.go's own doc comment.
//
// CreateSale, VoidSale and CreateSaleReturn are all exceptions, the same
// way ReceivePurchase and CreateStockAdjustment are
// (purchases.go's/stock.go's own doc comments): sales.Handler has no pool
// of its own, so each one's own transaction is opened here rather than in
// internal/sales — CreateSale/CreateSaleReturn accept an Idempotency-Key
// (docs/05-API.md § Conventions) and go through httpx.Idempotent, the
// helper that implements that replay semantics; VoidSale has no
// Idempotency-Key in the contract, so it opens and commits a plain
// transaction itself instead. All three live in this package rather than
// internal/sales because internal/httpx already imports internal/sales to
// wire the router and the reverse import would cycle. Each one calls the
// matching sales.Handler.___Tx method (a plain (gen.Sale, error) method
// taking the transaction this package opened, not one of
// gen.StrictServerInterface's) for the actual write.

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

// VoidSale voids a completed sale. Unlike CreateSale/CreateSaleReturn,
// POST /sales/{id}/void carries no Idempotency-Key
// (contracts/openapi.yaml has none for this operation — a replayed void
// simply answers 409 SALE_ALREADY_VOIDED the second time, which is
// itself safe to retry), so this method does not go through
// httpx.Idempotent. sales.Handler has no pool of its own (service.go's
// own doc comment) — void/return turned out not to need one, since the
// only write path here is "run once", not "replay-or-run-once" — so this
// method opens, commits and rolls back the one transaction
// sales.Handler.VoidSaleTx (a plain (gen.Sale, error) method, not one of
// gen.StrictServerInterface's) runs on, the same "single connection"
// shape CreateSale gives CreateSaleTx via Idempotent, just without the
// advisory lock and replay bookkeeping this operation does not need.
func (s server) VoidSale(ctx context.Context, req gen.VoidSaleRequestObject) (gen.VoidSaleResponseObject, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("httpx: void sale: begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	qtx := db.New(tx)

	sale, err := s.sales.VoidSaleTx(ctx, qtx, req.Id, req.Body)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		if isDeadlock(err) {
			return nil, errDeadlock
		}
		return nil, fmt.Errorf("httpx: void sale: commit: %w", err)
	}
	committed = true
	return gen.VoidSale200JSONResponse(sale), nil
}

// saleReturnPath is the RequestHash canonicalization path for POST
// /sales/{id}/return — like receivePurchasePath (purchases.go), this
// operation's path has an {id} segment, so it must be part of what the
// hash covers: two different sales returned with the same
// Idempotency-Key by the same actor must not be treated as the same
// request.
func saleReturnPath(id uuid.UUID) string {
	return fmt.Sprintf("/sales/%s/return", id)
}

// CreateSaleReturn completes a partial or full return against a
// completed sale, replaying a previous response for a repeated
// Idempotency-Key (docs/05-API.md § Conventions) instead of running the
// write again — the same split as CreateSale (this method's own doc
// comment above): the whole operation — the return_in movements, the
// return sale/items/payment inserts, the audit row and (if a key was
// sent) the idempotency_keys row — runs on the one transaction
// Idempotent opens; see sales.Handler.CreateSaleReturnTx's and
// Idempotent's own doc comments for why that matters.
func (s server) CreateSaleReturn(ctx context.Context, req gen.CreateSaleReturnRequestObject) (gen.CreateSaleReturnResponseObject, error) {
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
	hash, err := RequestHash(http.MethodPost, saleReturnPath(req.Id), authCtx.UserID, authCtx.Role, *req.Body)
	if err != nil {
		return nil, err
	}

	status, body, err := Idempotent(ctx, s.pool, authCtx.ShopID, key, hash, func(qtx *db.Queries) (int, []byte, error) {
		sale, err := s.sales.CreateSaleReturnTx(ctx, qtx, req.Id, req.Body)
		if err != nil {
			return 0, nil, err
		}
		b, err := json.Marshal(sale)
		if err != nil {
			return 0, nil, fmt.Errorf("httpx: marshal create sale return response: %w", err)
		}
		return http.StatusCreated, b, nil
	})
	if err != nil {
		return nil, err
	}
	return rawJSONResponse{status: status, body: body}, nil
}

// VisitCreateSaleReturnResponse lets rawJSONResponse (idempotency.go)
// stand in for CreateSaleReturn201JSONResponse on a replay, where the
// stored bytes are written back verbatim rather than re-encoded from a
// gen.Sale.
func (r rawJSONResponse) VisitCreateSaleReturnResponse(w http.ResponseWriter) error {
	return r.write(w)
}

// ListSaleDrafts and GetSaleDraft below satisfy gen.StrictServerInterface's
// two read draft operations by forwarding to server.sales
// (*sales.Handler) — same reasoning as ListSales/GetSale above.
//
// CreateSaleDraft, UpdateSaleDraft and DeleteSaleDraft are exceptions,
// the same way VoidSale is: sales.Handler has no pool of its own, so each
// one opens, commits and rolls back its own plain transaction here rather
// than in internal/sales, and none of them carries an Idempotency-Key
// (contracts/openapi.yaml has none for a draft's own create/edit/delete —
// docs/00-DECISIONS.md D-87/D-89: a duplicate draft, edit or delete is
// not a ledger event, unlike a duplicate sale). CompleteSaleDraft is the
// one draft operation that does carry a key — it creates a real Sale —
// so it goes through httpx.Idempotent exactly like CreateSale/
// CreateSaleReturn. Each of the four calls the matching
// sales.Handler.___Tx method (a plain method taking the transaction this
// package opened) for the actual write.

// ListSaleDrafts lists the shop's draft sales.
func (s server) ListSaleDrafts(ctx context.Context, req gen.ListSaleDraftsRequestObject) (gen.ListSaleDraftsResponseObject, error) {
	return s.sales.ListSaleDrafts(ctx, req)
}

// GetSaleDraft gets a draft sale.
func (s server) GetSaleDraft(ctx context.Context, req gen.GetSaleDraftRequestObject) (gen.GetSaleDraftResponseObject, error) {
	return s.sales.GetSaleDraft(ctx, req)
}

// CreateSaleDraft saves a new draft sale.
func (s server) CreateSaleDraft(ctx context.Context, req gen.CreateSaleDraftRequestObject) (gen.CreateSaleDraftResponseObject, error) {
	if req.Body == nil {
		return nil, apierr.Validation(map[string]string{"body": "required"})
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("httpx: create sale draft: begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	qtx := db.New(tx)

	draft, err := s.sales.CreateSaleDraftTx(ctx, qtx, req.Body)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		if isDeadlock(err) {
			return nil, errDeadlock
		}
		return nil, fmt.Errorf("httpx: create sale draft: commit: %w", err)
	}
	committed = true
	return gen.CreateSaleDraft201JSONResponse(draft), nil
}

// UpdateSaleDraft edits a draft sale — same "plain transaction, no
// Idempotency-Key" shape as CreateSaleDraft above.
func (s server) UpdateSaleDraft(ctx context.Context, req gen.UpdateSaleDraftRequestObject) (gen.UpdateSaleDraftResponseObject, error) {
	if req.Body == nil {
		return nil, apierr.Validation(map[string]string{"body": "required"})
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("httpx: update sale draft: begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	qtx := db.New(tx)

	draft, err := s.sales.UpdateSaleDraftTx(ctx, qtx, req.Id, req.Body)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		if isDeadlock(err) {
			return nil, errDeadlock
		}
		return nil, fmt.Errorf("httpx: update sale draft: commit: %w", err)
	}
	committed = true
	return gen.UpdateSaleDraft200JSONResponse(draft), nil
}

// DeleteSaleDraft deletes a draft sale — same "plain transaction, no
// Idempotency-Key" shape as CreateSaleDraft above.
func (s server) DeleteSaleDraft(ctx context.Context, req gen.DeleteSaleDraftRequestObject) (gen.DeleteSaleDraftResponseObject, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("httpx: delete sale draft: begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	qtx := db.New(tx)

	if err := s.sales.DeleteSaleDraftTx(ctx, qtx, req.Id); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		if isDeadlock(err) {
			return nil, errDeadlock
		}
		return nil, fmt.Errorf("httpx: delete sale draft: commit: %w", err)
	}
	committed = true
	return gen.DeleteSaleDraft204Response{}, nil
}

// completeSaleDraftPath is the RequestHash canonicalization path for
// POST /sales/drafts/{id}/complete — mirrors saleReturnPath: the
// operation's path has an {id} segment, so it must be part of what the
// hash covers.
func completeSaleDraftPath(id uuid.UUID) string {
	return fmt.Sprintf("/sales/drafts/%s/complete", id)
}

// CompleteSaleDraft completes a draft sale, replaying a previous response
// for a repeated Idempotency-Key instead of running the write again —
// the same split as CreateSale/CreateSaleReturn (this file's own doc
// comment above): the whole operation — CreateSaleTx's own sale_out
// movements and sale/items/payment inserts, the draft's own deletion and
// (if a key was sent) the idempotency_keys row — runs on the one
// transaction Idempotent opens.
func (s server) CompleteSaleDraft(ctx context.Context, req gen.CompleteSaleDraftRequestObject) (gen.CompleteSaleDraftResponseObject, error) {
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
	hash, err := RequestHash(http.MethodPost, completeSaleDraftPath(req.Id), authCtx.UserID, authCtx.Role, *req.Body)
	if err != nil {
		return nil, err
	}

	status, body, err := Idempotent(ctx, s.pool, authCtx.ShopID, key, hash, func(qtx *db.Queries) (int, []byte, error) {
		sale, err := s.sales.CompleteSaleDraftTx(ctx, qtx, req.Id, req.Body)
		if err != nil {
			return 0, nil, err
		}
		b, err := json.Marshal(sale)
		if err != nil {
			return 0, nil, fmt.Errorf("httpx: marshal complete sale draft response: %w", err)
		}
		return http.StatusCreated, b, nil
	})
	if err != nil {
		return nil, err
	}
	return rawJSONResponse{status: status, body: body}, nil
}

// VisitCompleteSaleDraftResponse lets rawJSONResponse (idempotency.go)
// stand in for CompleteSaleDraft201JSONResponse on a replay.
func (r rawJSONResponse) VisitCompleteSaleDraftResponse(w http.ResponseWriter) error {
	return r.write(w)
}
