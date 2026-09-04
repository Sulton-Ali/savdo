package httpx

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/shop"
)

// server implements gen.StrictServerInterface. As the API grows, each
// module's handler.go implements its slice of this interface
// (docs/03-ARCHITECTURE.md § Module map); httpx composes them — auth's
// Login/Logout/GetMe/ListSessions/RevokeSession are promoted from the
// embedded *auth.Handler; shop's nine `/shop`, `/locations` and `/staff`
// operations are forwarded to the named *shop.Handler field (shop.go) —
// named, not embedded, because both handler types are called "Handler"
// and an anonymous field's name is its type name, so embedding both would
// collide; GetHealthz/GetReadyz are defined directly on server (below and
// in readyz.go).
type server struct {
	// pool backs GetReadyz's DB check (readyz.go). No other Phase 1
	// operation touches it yet.
	pool *pgxpool.Pool

	*auth.Handler
	shop *shop.Handler
}

// GetHealthz reports the process is up. It does not touch the database —
// that is GET /readyz, contract-defined in Phase 1 (T1) and implemented
// once cmd/api opens a pool (T3/T4/T5).
func (server) GetHealthz(_ context.Context, _ gen.GetHealthzRequestObject) (gen.GetHealthzResponseObject, error) {
	return gen.GetHealthz200JSONResponse(gen.Healthz{Status: gen.HealthzStatusOk}), nil
}
