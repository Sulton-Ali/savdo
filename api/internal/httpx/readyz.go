package httpx

import (
	"context"
	"time"

	"github.com/Sulton-Ali/savdo/api/gen"
)

// readyzPingTimeout bounds how long GetReadyz waits on the database before
// reporting degraded, so a slow or dead Postgres doesn't hang the health
// check itself (docs/03-ARCHITECTURE.md § Cross-cutting: Health).
const readyzPingTimeout = 1 * time.Second

// GetReadyz reports whether the API's dependencies (the database) are
// reachable.
func (s server) GetReadyz(ctx context.Context, _ gen.GetReadyzRequestObject) (gen.GetReadyzResponseObject, error) {
	pingCtx, cancel := context.WithTimeout(ctx, readyzPingTimeout)
	defer cancel()

	var readiness gen.Readiness
	if err := s.pool.Ping(pingCtx); err != nil {
		readiness.Status = gen.ReadinessStatusDegraded
		readiness.Checks.Db = gen.ReadinessChecksDbError
		return gen.GetReadyz503JSONResponse(readiness), nil
	}

	readiness.Status = gen.ReadinessStatusOk
	readiness.Checks.Db = gen.ReadinessChecksDbOk
	return gen.GetReadyz200JSONResponse(readiness), nil
}
