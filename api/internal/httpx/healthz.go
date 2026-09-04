package httpx

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/catalog"
	"github.com/Sulton-Ali/savdo/api/internal/media"
	"github.com/Sulton-Ali/savdo/api/internal/shop"
	"github.com/Sulton-Ali/savdo/api/internal/stock"
)

// server implements gen.StrictServerInterface. As the API grows, each
// module's handler.go implements its slice of this interface
// (docs/03-ARCHITECTURE.md § Module map); httpx composes them — auth's
// Login/Logout/GetMe/ListSessions/RevokeSession are promoted from the
// embedded *auth.Handler; shop's nine `/shop`, `/locations` and `/staff`
// operations, media's `/media` operation, catalog's 21 catalogue/
// product-image operations and stock's four read/transfer operations are
// forwarded to their named *shop.Handler / *media.Handler /
// *catalog.Handler / *stock.Handler fields (shop.go, media.go, catalog.go,
// stock.go) — named, not embedded, because every one of these handler
// types is called "Handler" and an anonymous field's name is its type
// name, so embedding more than one would collide; GetHealthz/GetReadyz are
// defined directly on server (below and in readyz.go). CreateStockAdjustment
// is the one stock operation server implements itself rather than
// forwarding (stock.go's own doc comment): it needs pool for
// httpx.Idempotent, which stock.Handler.CreateAdjustment does not take.
type server struct {
	// pool backs GetReadyz's DB check (readyz.go) and, from T3,
	// CreateStockAdjustment's Idempotency-Key bookkeeping (stock.go).
	pool *pgxpool.Pool

	*auth.Handler
	shop    *shop.Handler
	media   *media.Handler
	catalog *catalog.Handler
	stock   *stock.Handler
}

// GetHealthz reports the process is up. It does not touch the database —
// that is GET /readyz, contract-defined in Phase 1 (T1) and implemented
// once cmd/api opens a pool (T3/T4/T5).
func (server) GetHealthz(_ context.Context, _ gen.GetHealthzRequestObject) (gen.GetHealthzResponseObject, error) {
	return gen.GetHealthz200JSONResponse(gen.Healthz{Status: gen.HealthzStatusOk}), nil
}
