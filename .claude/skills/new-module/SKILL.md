---
name: new-module
description: Scaffold a new Go module under api/internal/<name> in the Savdo house style (service, handler wired to the generated ServerInterface, errors, sqlc query file, integration test using testdb). Use when a phase introduces a business area that has no package yet.
---

# new-module

## Procedure (for the dispatched implementer)

1. Confirm the module is listed in `docs/03-ARCHITECTURE.md` § Module map. If not, stop:
   the orchestrator adds it (may need an ADR).
2. Create:

   ```
   api/internal/<name>/
     service.go        type Service struct{ db *db.Queries; pool *pgxpool.Pool; ... }
     handler.go        methods satisfying the module's slice of gen.ServerInterface
     errors.go         var ErrX = &apierr.Error{Code: gen.ErrorCodeX, Status: 409}
     service_test.go   uses internal/db/testdb.New(t) (testcontainers)
   api/db/queries/<name>.sql   -- name: ListX :many  (shop_id param first)
   ```

3. Register the handler in `internal/httpx/router.go` and the service in `cmd/api/main.go`
   — those two files are in scope for this task only.
4. Every query starts with `WHERE shop_id = $1`. Every service method takes `ctx` and
   reads the auth context (`auth.FromContext(ctx)`) for `shop_id`, `user_id`, `role`.
5. Add the permission checks for the module's routes per `04-DATA-MODEL.md` § 7.
6. `make generate && make verify`.

Copy the style of the nearest existing module; do not invent a new pattern.
