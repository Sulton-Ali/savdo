package httpx

import (
	"context"

	"github.com/Sulton-Ali/savdo/api/gen"
)

// server implements gen.StrictServerInterface. As the API grows, each
// module's handler.go implements its slice of this interface
// (docs/03-ARCHITECTURE.md § Module map); httpx composes them.
type server struct{}

// GetHealthz reports the process is up. It does not touch the database —
// that is GET /readyz, contract-defined in Phase 1 (T1) and implemented
// once cmd/api opens a pool (T3/T4/T5).
func (server) GetHealthz(_ context.Context, _ gen.GetHealthzRequestObject) (gen.GetHealthzResponseObject, error) {
	return gen.GetHealthz200JSONResponse(gen.Healthz{Status: gen.HealthzStatusOk}), nil
}
