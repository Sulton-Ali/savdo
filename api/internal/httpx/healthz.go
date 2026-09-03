package httpx

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
)

// server implements gen.StrictServerInterface. As the API grows, each
// module's handler.go implements its slice of this interface
// (docs/03-ARCHITECTURE.md § Module map); httpx composes them — auth's
// Login/Logout/GetMe/ListSessions/RevokeSession are promoted from the
// embedded *auth.Handler, and the rest are defined directly on server
// (below, in unimplemented.go and readyz.go) until their own module lands.
type server struct {
	// pool backs GetReadyz's DB check (readyz.go). No other Phase 1
	// operation touches it yet.
	pool *pgxpool.Pool

	*auth.Handler
}

// GetHealthz reports the process is up. It does not touch the database —
// that is GET /readyz, contract-defined in Phase 1 (T1) and implemented
// once cmd/api opens a pool (T3/T4/T5).
func (server) GetHealthz(_ context.Context, _ gen.GetHealthzRequestObject) (gen.GetHealthzResponseObject, error) {
	return gen.GetHealthz200JSONResponse(gen.Healthz{Status: gen.HealthzStatusOk}), nil
}
